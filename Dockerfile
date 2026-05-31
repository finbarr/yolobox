# Stage: Go source
FROM golang:1.25.6 AS go-source

# Stage: Bun runtime
FROM oven/bun:1.3 AS bun-source

# Stage: Claude Code installer
FROM ubuntu:24.04 AS claude-installer

RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://claude.ai/install.sh | bash

# Main image
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ARG NPM_MIN_RELEASE_AGE_DAYS=7

# =============================================================================
# STABLE LAYERS — large, rarely change (ordered first to minimize re-downloads)
# =============================================================================

# Install system packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Essentials
    bash \
    ca-certificates \
    curl \
    wget \
    git \
    sudo \
    # Build tools
    build-essential \
    make \
    cmake \
    pkg-config \
    # Python
    python3 \
    python3-pip \
    python3-venv \
    # Common utilities
    bubblewrap \
    jq \
    rsync \
    ripgrep \
    fd-find \
    bat \
    eza \
    fzf \
    tree \
    htop \
    vim \
    nano \
    less \
    openssh-client \
    gnupg \
    unzip \
    zip \
    tzdata \
    # For native node modules
    libssl-dev \
    # For terminfo compilation (Ghostty support)
    ncurses-bin \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js 22 LTS
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# Keep npm current enough to support min-release-age, but do not bootstrap from
# an npm package published inside the same age gate we enforce below.
RUN set -eux; \
    tmp="$(mktemp -d)"; \
    npm pack "npm@latest" \
        --silent \
        --before="$(date -u -d "${NPM_MIN_RELEASE_AGE_DAYS} days ago" +%Y-%m-%dT%H:%M:%SZ)" \
        --pack-destination "$tmp"; \
    tar -xzf "$tmp"/npm-*.tgz -C "$tmp"; \
    rm -rf /usr/lib/node_modules/npm; \
    mv "$tmp/package" /usr/lib/node_modules/npm; \
    ln -sf ../lib/node_modules/npm/bin/npm-cli.js /usr/bin/npm; \
    ln -sf ../lib/node_modules/npm/bin/npx-cli.js /usr/bin/npx; \
    npm cache clean --force; \
    rm -rf "$tmp"

# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update \
    && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Install Docker CLI + Compose (for --docker flag; no daemon, uses host socket)
RUN install -m 0755 -d /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc && \
    chmod a+r /etc/apt/keyrings/docker.asc && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null && \
    apt-get update && \
    apt-get install -y docker-ce-cli docker-compose-plugin docker-buildx-plugin && \
    rm -rf /var/lib/apt/lists/*

# Install Go (from official image)
COPY --from=go-source /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:$PATH"

# Install Bun (from official image)
COPY --from=bun-source /usr/local/bin/bun /usr/local/bin/bun
RUN ln -s /usr/local/bin/bun /usr/local/bin/bunx

# Install uv (fast Python package manager)
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/

# Install Ghostty terminfo (not in Ubuntu's ncurses yet, needs 6.5+)
# Prevents "Could not set up terminal" warnings when TERM=xterm-ghostty
# Must be done as root to install to system terminfo directory
COPY ghostty.terminfo /tmp/ghostty.terminfo
RUN tic -x -o /usr/share/terminfo /tmp/ghostty.terminfo && rm /tmp/ghostty.terminfo

# Create symlinks for bat/fd (Debian/Ubuntu rename these binaries)
RUN ln -s /usr/bin/batcat /usr/local/bin/bat && \
    ln -s /usr/bin/fdfind /usr/local/bin/fd

# Install stable dev tools (change rarely, separated from AI CLIs)
RUN NPM_CONFIG_MIN_RELEASE_AGE="${NPM_MIN_RELEASE_AGE_DAYS}" npm install -g --no-audit --no-fund \
    typescript \
    ts-node \
    yarn \
    pnpm \
    && npm cache clean --force

# =============================================================================
# USER SETUP — small layers, stable
# =============================================================================

# Remove default ubuntu user (UID 1000) to avoid collision when the entrypoint
# remaps yolo's UID to match the host project directory owner
RUN userdel -r ubuntu 2>/dev/null || true

# Create yolo user with passwordless sudo
RUN useradd -m -s /bin/bash yolo \
    && echo "yolo ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/yolo \
    && chmod 0440 /etc/sudoers.d/yolo

# Set up directories
RUN mkdir -p /output /secrets \
    && chown yolo:yolo /output

# AI CLI wrappers in yolo mode - these find the real binary dynamically,
# so they survive updates (npm update -g, claude upgrade, etc.)
RUN mkdir -p /opt/yolobox/bin

# Generic wrapper template that finds real binary by excluding wrapper dir from PATH
RUN echo '#!/bin/bash' > /opt/yolobox/wrapper-template \
    && echo 'WRAPPER_DIR=/opt/yolobox/bin' >> /opt/yolobox/wrapper-template \
    && echo 'CMD=$(basename "$0")' >> /opt/yolobox/wrapper-template \
    && echo 'CLEAN_PATH=$(echo "$PATH" | tr ":" "\n" | grep -v "^$WRAPPER_DIR$" | tr "\n" ":" | sed "s/:$//" )' >> /opt/yolobox/wrapper-template \
    && echo 'REAL_BIN=$(PATH="$CLEAN_PATH" which "$CMD" 2>/dev/null)' >> /opt/yolobox/wrapper-template \
    && echo 'if [ -z "$REAL_BIN" ]; then echo "Error: $CMD not found" >&2; exit 1; fi' >> /opt/yolobox/wrapper-template \
    && echo 'if [ "$NO_YOLO" = "1" ]; then exec "$REAL_BIN" "$@"; fi' >> /opt/yolobox/wrapper-template

# Clipboard command shims used by --clipboard. These cover the command names
# used by common terminal clipboard libraries on Linux and macOS.
RUN printf '%s\n' \
    '#!/bin/bash' \
    'set -euo pipefail' \
    'endpoint="${YOLOBOX_CLIPBOARD_ENDPOINT:-}"' \
    'token="${YOLOBOX_CLIPBOARD_TOKEN:-}"' \
    'if [ -z "$endpoint" ] || [ -z "$token" ]; then' \
    '    echo "yolobox clipboard bridge is not enabled; start with --clipboard" >&2' \
    '    exit 1' \
    'fi' \
    'copy_to_host() {' \
    '    curl -fsS -X POST -H "X-Yolobox-Clipboard-Token: $token" --data-binary @- "$endpoint/copy" >/dev/null' \
    '}' \
    'paste_from_host() {' \
    '    curl -fsS -H "X-Yolobox-Clipboard-Token: $token" "$endpoint/paste"' \
    '}' \
    'cmd="$(basename "$0")"' \
    'case "$cmd" in' \
    '    pbcopy|wl-copy)' \
    '        copy_to_host' \
    '        ;;' \
    '    pbpaste|wl-paste)' \
    '        paste_from_host' \
    '        ;;' \
    '    xclip)' \
    '        for arg in "$@"; do' \
    '            case "$arg" in -o|-out) paste_from_host; exit $? ;; esac' \
    '        done' \
    '        copy_to_host' \
    '        ;;' \
    '    xsel)' \
    '        for arg in "$@"; do' \
    '            case "$arg" in -o|--output) paste_from_host; exit $? ;; esac' \
    '        done' \
    '        copy_to_host' \
    '        ;;' \
    '    *)' \
    '        echo "unsupported yolobox clipboard command: $cmd" >&2' \
    '        exit 1' \
    '        ;;' \
    'esac' \
    > /opt/yolobox/bin/yolobox-clipboard \
    && chmod +x /opt/yolobox/bin/yolobox-clipboard \
    && ln -s yolobox-clipboard /opt/yolobox/bin/pbcopy \
    && ln -s yolobox-clipboard /opt/yolobox/bin/pbpaste \
    && ln -s yolobox-clipboard /opt/yolobox/bin/wl-copy \
    && ln -s yolobox-clipboard /opt/yolobox/bin/wl-paste \
    && ln -s yolobox-clipboard /opt/yolobox/bin/xclip \
    && ln -s yolobox-clipboard /opt/yolobox/bin/xsel

# URL open shims used by --open-bridge. These intentionally accept only a
# single URL argument; the host bridge validates that it is http or https.
RUN printf '%s\n' \
    '#!/bin/bash' \
    'set -euo pipefail' \
    'endpoint="${YOLOBOX_OPEN_BRIDGE_ENDPOINT:-}"' \
    'token="${YOLOBOX_OPEN_BRIDGE_TOKEN:-}"' \
    'if [ -z "$endpoint" ] || [ -z "$token" ]; then' \
    '    echo "yolobox open bridge is not enabled; start with --open-bridge" >&2' \
    '    exit 1' \
    'fi' \
    'if [ "$#" -ne 1 ]; then' \
    '    echo "usage: $(basename "$0") <http-or-https-url>" >&2' \
    '    exit 2' \
    'fi' \
    'printf "%s" "$1" | curl -fsS -X POST -H "X-Yolobox-Open-Token: $token" --data-binary @- "$endpoint/open" >/dev/null' \
    > /opt/yolobox/bin/yolobox-open \
    && chmod +x /opt/yolobox/bin/yolobox-open \
    && ln -s yolobox-open /opt/yolobox/bin/open \
    && ln -s yolobox-open /opt/yolobox/bin/xdg-open

# Claude wrapper
RUN cp /opt/yolobox/wrapper-template /opt/yolobox/bin/claude \
    && echo 'exec "$REAL_BIN" --dangerously-skip-permissions "$@"' >> /opt/yolobox/bin/claude \
    && chmod +x /opt/yolobox/bin/claude

# Codex wrapper
RUN cp /opt/yolobox/wrapper-template /opt/yolobox/bin/codex \
    && echo 'exec "$REAL_BIN" --ask-for-approval never --sandbox danger-full-access "$@"' >> /opt/yolobox/bin/codex \
    && chmod +x /opt/yolobox/bin/codex

# Gemini wrapper
RUN cp /opt/yolobox/wrapper-template /opt/yolobox/bin/gemini \
    && echo 'exec "$REAL_BIN" --yolo "$@"' >> /opt/yolobox/bin/gemini \
    && chmod +x /opt/yolobox/bin/gemini

# OpenCode wrapper (no yolo flag yet, passthrough for now)
RUN cp /opt/yolobox/wrapper-template /opt/yolobox/bin/opencode \
    && echo 'exec "$REAL_BIN" "$@"' >> /opt/yolobox/bin/opencode \
    && chmod +x /opt/yolobox/bin/opencode

# Copilot wrapper
RUN cp /opt/yolobox/wrapper-template /opt/yolobox/bin/copilot \
    && echo 'exec "$REAL_BIN" --yolo "$@"' >> /opt/yolobox/bin/copilot \
    && chmod +x /opt/yolobox/bin/copilot

# Pi wrapper (no yolo flag; Pi runs with its own tool controls)
RUN cp /opt/yolobox/wrapper-template /opt/yolobox/bin/pi \
    && echo 'exec "$REAL_BIN" "$@"' >> /opt/yolobox/bin/pi \
    && chmod +x /opt/yolobox/bin/pi

# GitHub HTTPS credential helper. When a GitHub token is forwarded into the
# container, this lets ordinary Git commands authenticate to https://github.com
# without depending on host-specific helpers such as macOS Keychain.
RUN printf '%s\n' \
    '#!/bin/sh' \
    'case "${1:-}" in' \
    '    get) ;;' \
    '    *) exit 0 ;;' \
    'esac' \
    'protocol=""' \
    'host=""' \
    'while IFS= read -r line; do' \
    '    [ -z "$line" ] && break' \
    '    case "$line" in' \
    '        protocol=*) protocol=${line#protocol=} ;;' \
    '        host=*) host=${line#host=} ;;' \
    '    esac' \
    'done' \
    '[ "$protocol" = "https" ] || exit 0' \
    '[ "$host" = "github.com" ] || exit 0' \
    'token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"' \
    '[ -n "$token" ] || exit 0' \
    'printf "username=x-access-token\n"' \
    'printf "password=%s\n" "$token"' \
    > /opt/yolobox/bin/git-credential-github-token \
    && chmod +x /opt/yolobox/bin/git-credential-github-token \
    && git config --system --add credential.https://github.com.helper "" \
    && git config --system --add credential.https://github.com.helper "!/opt/yolobox/bin/git-credential-github-token"

# Built-in agent skills live outside /home/yolo so named volumes cannot hide them.
COPY skills /opt/yolobox/skills
COPY agent-instructions /opt/yolobox/agent-instructions

# Configure npm to use a user-writable prefix so yolo can `npm install -g` without sudo.
ENV NPM_CONFIG_PREFIX=/home/yolo/.npm-global

# Add wrapper dir, npm-global bin, and ~/.local/bin to PATH (wrappers take priority)
ENV PATH="/opt/yolobox/bin:/home/yolo/.npm-global/bin:/home/yolo/.local/bin:$PATH"

# Managed-block helper: merges built-in agent guidance into user instruction files
RUN printf '%s\n' \
    '#!/usr/bin/env python3' \
    'import pathlib' \
    'import sys' \
    '' \
    'if len(sys.argv) != 5:' \
    '    raise SystemExit("usage: yolobox-upsert-block <target> <source> <start> <end>")' \
    '' \
    'target = pathlib.Path(sys.argv[1])' \
    'source = pathlib.Path(sys.argv[2])' \
    'start_marker = sys.argv[3]' \
    'end_marker = sys.argv[4]' \
    '' \
    'target.parent.mkdir(parents=True, exist_ok=True)' \
    '' \
    'existing_lines = []' \
    'if target.exists():' \
    '    skip = False' \
    '    for line in target.read_text().splitlines():' \
    '        if line == start_marker:' \
    '            skip = True' \
    '            continue' \
    '        if line == end_marker:' \
    '            skip = False' \
    '            continue' \
    '        if not skip:' \
    '            existing_lines.append(line)' \
    '' \
    'while existing_lines and existing_lines[-1] == "":' \
    '    existing_lines.pop()' \
    '' \
    'payload_lines = source.read_text().rstrip("\n").splitlines()' \
    'output_lines = []' \
    'if existing_lines:' \
    '    output_lines.extend(existing_lines)' \
    '    output_lines.append("")' \
    'output_lines.append(start_marker)' \
    'output_lines.extend(payload_lines)' \
    'output_lines.append(end_marker)' \
    '' \
    'target.write_text("\n".join(output_lines) + "\n")' \
    > /usr/local/bin/yolobox-upsert-block && \
    chmod +x /usr/local/bin/yolobox-upsert-block

# UID-fix helper: runs as root to change yolo UID/GID and exec as the new user.
# Called by the entrypoint when the host project dir owner differs from yolo's UID.
RUN printf '%s\n' \
    '#!/bin/bash' \
    'HOST_UID=$1; HOST_GID=$2; shift 2; shift' \
    'usermod -u "$HOST_UID" -o yolo 2>/dev/null' \
    'groupmod -g "$HOST_GID" -o yolo 2>/dev/null' \
    'chown -R "$HOST_UID:$HOST_GID" /home/yolo 2>/dev/null' \
    'chown -R "$HOST_UID:$HOST_GID" /output 2>/dev/null' \
    '[ -n "$YOLOBOX_SAVED_PATH" ] && export PATH="$YOLOBOX_SAVED_PATH" && unset YOLOBOX_SAVED_PATH' \
    'exec setpriv --reuid="$HOST_UID" --regid="$HOST_GID" --init-groups -- "$@"' \
    > /usr/local/bin/yolobox-uid-fix.sh && \
    chmod +x /usr/local/bin/yolobox-uid-fix.sh

# Create entrypoint script
RUN mkdir -p /host-claude /host-codex /host-codex-sessions /host-gemini /host-opencode /host-pi /host-git /host-agent-instructions /host-files && \
    printf '%s\n' \
    '#!/bin/bash' \
    '' \
    '# Match yolo UID/GID to host project owner (fixes virtiofs on Colima 0.10+)' \
    '# Must run FIRST: after remapping, the named volume is owned by the new UID,' \
    '# so subsequent runs cannot access /home/yolo until the fix re-execs.' \
    'if [ -n "$YOLOBOX_HOST_UID" ] && [ "$YOLOBOX_HOST_UID" != "$(id -u)" ] && [ "$YOLOBOX_HOST_UID" != "0" ]; then' \
    '    export YOLOBOX_SAVED_PATH="$PATH"' \
    '    exec sudo -E /usr/local/bin/yolobox-uid-fix.sh "$YOLOBOX_HOST_UID" "${YOLOBOX_HOST_GID:-$(id -g)}" -- "$0" "$@"' \
    'fi' \
    '' \
    '# Apple container workaround: files are in /host-files/ instead of separate mounts' \
    '# Check YOLOBOX_HOST_FILES env var for the mount location' \
    'HF="${YOLOBOX_HOST_FILES:-}"' \
    '' \
    'yolobox_timing_mark() {' \
    '    [ -n "${YOLOBOX_TIMING:-}" ] || return 0' \
    '    local label="$1" now delta total' \
    '    now="$(date +%s%3N 2>/dev/null || true)"' \
    '    [ -n "$now" ] || return 0' \
    '    [ -n "${YOLOBOX_TIMING_START_MS:-}" ] || YOLOBOX_TIMING_START_MS="$now"' \
    '    [ -n "${YOLOBOX_TIMING_LAST_MS:-}" ] || YOLOBOX_TIMING_LAST_MS="$YOLOBOX_TIMING_START_MS"' \
    '    delta=$((now - YOLOBOX_TIMING_LAST_MS))' \
    '    total=$((now - YOLOBOX_TIMING_START_MS))' \
    '    YOLOBOX_TIMING_LAST_MS="$now"' \
    '    printf "\033[35m[timing]\033[0m +%d.%03ds total %d.%03ds entrypoint: %s\n" "$((delta / 1000))" "$((delta % 1000))" "$((total / 1000))" "$((total % 1000))" "$label" >&2' \
    '}' \
    'if [ -n "${YOLOBOX_TIMING:-}" ]; then' \
    '    YOLOBOX_TIMING_START_MS="$(date +%s%3N 2>/dev/null || true)"' \
    '    YOLOBOX_TIMING_LAST_MS="$YOLOBOX_TIMING_START_MS"' \
    '    yolobox_timing_mark "start"' \
    'fi' \
    '' \
    'inject_agent_guidance() {' \
    '    local target="$1"' \
    '    local source_file="$2"' \
    '    local start_marker="$3"' \
    '    local end_marker="$4"' \
    '    /usr/local/bin/yolobox-upsert-block "$target" "$source_file" "$start_marker" "$end_marker"' \
    '    sudo chown yolo:yolo "$target"' \
    '}' \
    'warn_low_space() {' \
    '    local path="$1"' \
    '    local label="$2"' \
    '    local min_kb="${3:-65536}"' \
    '    local available_kb' \
    '    available_kb=$(df -Pk "$path" 2>/dev/null | awk "NR==2 {print \$4}")' \
    '    if [ -n "$available_kb" ] && [ "$available_kb" -lt "$min_kb" ]; then' \
    '        echo -e "\033[33m→ Low free space for $label (${available_kb}KB available); Codex auth and other CLI writes may fail with '\''No space left on device'\''\033[0m" >&2' \
    '    fi' \
    '}' \
    'enable_rtk() {' \
    '    [ "${YOLOBOX_RTK:-}" = "1" ] || return 0' \
    '    if ! command -v rtk >/dev/null 2>&1; then' \
    '        echo -e "\033[33m→ RTK requested but rtk is not installed in this image\033[0m" >&2' \
    '        return 0' \
    '    fi' \
    '    local target="${YOLOBOX_RTK_TARGET:-}"' \
    '    if [ -z "$target" ]; then' \
    '        echo -e "\033[33m→ RTK enabled; no supported AI shortcut target detected\033[0m" >&2' \
    '        return 0' \
    '    fi' \
    '    local output status=0' \
    '    case "$target" in' \
    '        claude)' \
    '            output="$(rtk init -g --auto-patch 2>&1)" || status=$?' \
    '            ;;' \
    '        codex)' \
    '            output="$(rtk init -g --codex 2>&1)" || status=$?' \
    '            ;;' \
    '        gemini)' \
    '            output="$(rtk init -g --gemini --auto-patch 2>&1)" || status=$?' \
    '            ;;' \
    '        opencode)' \
    '            output="$(rtk init -g --opencode 2>&1)" || status=$?' \
    '            ;;' \
    '        *)' \
    '            echo -e "\033[33m→ RTK enabled, but $target is not supported by yolobox auto-init\033[0m" >&2' \
    '            return 0' \
    '            ;;' \
    '    esac' \
    '    if [ "$status" = "0" ]; then' \
    '        echo -e "\033[33m→ RTK command compression enabled for $target\033[0m" >&2' \
    '    else' \
    '        echo -e "\033[33m→ Failed to enable RTK for $target\033[0m" >&2' \
    '        [ -n "$output" ] && printf "%s\n" "$output" >&2' \
    '    fi' \
    '}' \
    '' \
    'warn_low_space /home/yolo /home/yolo' \
    'warn_low_space /tmp /tmp' \
    '' \
    '# Materialize the runtime context manifest without a host-side temp bind mount' \
    'if [ -n "${YOLOBOX_CONTEXT_JSON_B64:-}" ]; then' \
    '    sudo mkdir -p /run/yolobox' \
    '    if printf "%s" "$YOLOBOX_CONTEXT_JSON_B64" | base64 -d | sudo tee /run/yolobox/context.json >/dev/null; then' \
    '        sudo chmod 0444 /run/yolobox/context.json' \
    '    else' \
    '        echo -e "\033[33m→ Failed to write yolobox context manifest\033[0m" >&2' \
    '    fi' \
    '    unset YOLOBOX_CONTEXT_JSON_B64' \
    'fi' \
    '' \
    '# Copy Claude config from host staging area if present' \
    'if [ -d /host-claude/.claude ] || [ -f /host-claude/.claude.json ] || [ -f "$HF/claude/.claude.json" ]; then' \
    '    echo -e "\033[33m→ Copying host Claude config to container\033[0m" >&2' \
    'fi' \
    'if [ -d /host-claude/.claude ]; then' \
    '    sudo rm -rf /home/yolo/.claude' \
    '    sudo cp -a /host-claude/.claude /home/yolo/.claude' \
    '    sudo chown -R yolo:yolo /home/yolo/.claude' \
    'fi' \
    'if [ -f /host-claude/.claude.json ]; then' \
    '    sudo rm -f /home/yolo/.claude.json' \
    '    sudo cp -a /host-claude/.claude.json /home/yolo/.claude.json' \
    '    sudo chown yolo:yolo /home/yolo/.claude.json' \
    'elif [ -f "$HF/claude/.claude.json" ]; then' \
    '    sudo rm -f /home/yolo/.claude.json' \
    '    sudo cp -a "$HF/claude/.claude.json" /home/yolo/.claude.json' \
    '    sudo chown yolo:yolo /home/yolo/.claude.json' \
    'fi' \
    '# Copy Claude credentials from macOS Keychain (extracted by yolobox)' \
    'CREDS_FILE="/host-claude/.credentials.json"' \
    '[ ! -f "$CREDS_FILE" ] && [ -f "$HF/claude/.credentials.json" ] && CREDS_FILE="$HF/claude/.credentials.json"' \
    'if [ -f "$CREDS_FILE" ]; then' \
    '    mkdir -p /home/yolo/.claude' \
    '    sudo cp -a "$CREDS_FILE" /home/yolo/.claude/.credentials.json' \
    '    sudo chown yolo:yolo /home/yolo/.claude/.credentials.json' \
    '    sudo chmod 600 /home/yolo/.claude/.credentials.json' \
    'fi' \
    '' \
    '# Copy Gemini config from host staging area if present' \
    'if [ -d /host-gemini/.gemini ]; then' \
    '    echo -e "\033[33m→ Copying host Gemini config to container\033[0m" >&2' \
    '    sudo rm -rf /home/yolo/.gemini' \
    '    sudo cp -a /host-gemini/.gemini /home/yolo/.gemini' \
    '    sudo chown -R yolo:yolo /home/yolo/.gemini' \
    'fi' \
    '' \
    '# Copy OpenCode config from host staging area if present' \
    'if [ -d /host-opencode/.config/opencode ]; then' \
    '    echo -e "\033[33m→ Copying host OpenCode config to container\033[0m" >&2' \
    '    sudo mkdir -p /home/yolo/.config' \
    '    sudo rm -rf /home/yolo/.config/opencode' \
    '    sudo cp -a /host-opencode/.config/opencode /home/yolo/.config/opencode' \
    '    sudo chown -R yolo:yolo /home/yolo/.config/opencode' \
    'fi' \
    '' \
    '# Copy Pi config from host staging area if present' \
    'if [ -d /host-pi/.pi/agent ]; then' \
    '    echo -e "\033[33m→ Copying host Pi config to container\033[0m" >&2' \
    '    sudo mkdir -p /home/yolo/.pi' \
    '    sudo rm -rf /home/yolo/.pi/agent' \
    '    sudo cp -a /host-pi/.pi/agent /home/yolo/.pi/agent' \
    '    sudo chown -R yolo:yolo /home/yolo/.pi' \
    'fi' \
    '' \
    '# Copy Codex config from host staging area if present' \
    'if [ -d /host-codex/.codex ]; then' \
    '    echo -e "\033[33m→ Copying host Codex config to container\033[0m" >&2' \
    '    yolobox_timing_mark "codex config sync start"' \
    '    CODEX_AUTH_BACKUP=""' \
    '    if [ -s /home/yolo/.codex/auth.json ] && { [ ! -f /host-codex/.codex/auth.json ] || [ ! -s /host-codex/.codex/auth.json ]; }; then' \
    '        CODEX_AUTH_BACKUP="/tmp/codex-auth.json.yolobox-backup.$$"' \
    '        sudo cp -a /home/yolo/.codex/auth.json "$CODEX_AUTH_BACKUP"' \
    '    fi' \
    '    mkdir -p /home/yolo/.codex' \
    '    sudo rm -rf /home/yolo/.codex/log /home/yolo/.codex/sqlite /home/yolo/.codex/.tmp /home/yolo/.codex/tmp /home/yolo/.codex/cache /home/yolo/.codex/generated_images /home/yolo/.codex/computer-use /home/yolo/.codex/logs_*.sqlite* /home/yolo/.codex/state_*.sqlite*' \
    '    if command -v rsync >/dev/null 2>&1; then' \
    '        sudo rsync -a --chown=yolo:yolo --exclude=sessions/ --exclude=log/ --exclude=logs_*.sqlite* --exclude=state_*.sqlite* --exclude=sqlite/ --exclude=.tmp/ --exclude=tmp/ --exclude=cache/ --exclude=generated_images/ --exclude=computer-use/ /host-codex/.codex/ /home/yolo/.codex/' \
    '    else' \
    '        sudo cp -a /host-codex/.codex/. /home/yolo/.codex/' \
    '        sudo chown -R yolo:yolo /home/yolo/.codex' \
    '    fi' \
    '    if [ -n "$CODEX_AUTH_BACKUP" ] && [ -s "$CODEX_AUTH_BACKUP" ]; then' \
    '        sudo mv -f "$CODEX_AUTH_BACKUP" /home/yolo/.codex/auth.json' \
    '        sudo chown yolo:yolo /home/yolo/.codex/auth.json' \
    '    fi' \
    '    if [ -d /host-codex-sessions ]; then' \
    '        sudo rm -rf /home/yolo/.codex/sessions' \
    '        ln -s /host-codex-sessions /home/yolo/.codex/sessions' \
    '    fi' \
    '    yolobox_timing_mark "codex config sync done"' \
    'fi' \
    'if [ -f /home/yolo/.codex/auth.json ] && [ ! -s /home/yolo/.codex/auth.json ]; then' \
    '    echo -e "\033[33m→ Removing empty Codex auth file\033[0m" >&2' \
    '    rm -f /home/yolo/.codex/auth.json' \
    'fi' \
    '' \
    '# Copy git config from host staging area if present' \
    'if [ -f /host-git/.gitconfig ]; then' \
    '    echo -e "\033[33m→ Copying host git config to container\033[0m" >&2' \
    '    sudo rm -f /home/yolo/.gitconfig' \
    '    sudo cp -a /host-git/.gitconfig /home/yolo/.gitconfig' \
    '    sudo chown yolo:yolo /home/yolo/.gitconfig' \
    'elif [ -f "$HF/git/.gitconfig" ]; then' \
    '    echo -e "\033[33m→ Copying host git config to container\033[0m" >&2' \
    '    sudo rm -f /home/yolo/.gitconfig' \
    '    sudo cp -a "$HF/git/.gitconfig" /home/yolo/.gitconfig' \
    '    sudo chown yolo:yolo /home/yolo/.gitconfig' \
    'fi' \
    '' \
    '# Mark project directory as safe for git (ownership differs from container user)' \
    'if [ -n "$YOLOBOX_PROJECT_PATH" ]; then' \
    '    git config --global --add safe.directory "$YOLOBOX_PROJECT_PATH"' \
    'fi' \
    '' \
    '# Copy global agent instruction files from host staging area if present' \
    'COPIED_AGENT_INSTRUCTIONS=0' \
    '# Claude: CLAUDE.md' \
    'CLAUDE_MD="/host-agent-instructions/claude/CLAUDE.md"' \
    '[ ! -f "$CLAUDE_MD" ] && [ -f "$HF/agent-instructions/claude/CLAUDE.md" ] && CLAUDE_MD="$HF/agent-instructions/claude/CLAUDE.md"' \
    'if [ -f "$CLAUDE_MD" ]; then' \
    '    mkdir -p /home/yolo/.claude' \
    '    sudo cp -a "$CLAUDE_MD" /home/yolo/.claude/CLAUDE.md' \
    '    sudo chown yolo:yolo /home/yolo/.claude/CLAUDE.md' \
    '    COPIED_AGENT_INSTRUCTIONS=1' \
    'fi' \
    '# Claude: skills/ directory' \
    'CLAUDE_SKILLS_DIR="/host-agent-instructions/claude/skills"' \
    '[ ! -d "$CLAUDE_SKILLS_DIR" ] && [ -d "$HF/agent-instructions/claude/skills" ] && CLAUDE_SKILLS_DIR="$HF/agent-instructions/claude/skills"' \
    'if [ -d "$CLAUDE_SKILLS_DIR" ]; then' \
    '    mkdir -p /home/yolo/.claude' \
    '    sudo rm -rf /home/yolo/.claude/skills' \
    '    sudo cp -a "$CLAUDE_SKILLS_DIR" /home/yolo/.claude/skills' \
    '    sudo chown -R yolo:yolo /home/yolo/.claude' \
    '    COPIED_AGENT_INSTRUCTIONS=1' \
    'fi' \
    '# Gemini: GEMINI.md' \
    'GEMINI_MD="/host-agent-instructions/gemini/GEMINI.md"' \
    '[ ! -f "$GEMINI_MD" ] && [ -f "$HF/agent-instructions/gemini/GEMINI.md" ] && GEMINI_MD="$HF/agent-instructions/gemini/GEMINI.md"' \
    'if [ -f "$GEMINI_MD" ]; then' \
    '    mkdir -p /home/yolo/.gemini' \
    '    sudo cp -a "$GEMINI_MD" /home/yolo/.gemini/GEMINI.md' \
    '    sudo chown -R yolo:yolo /home/yolo/.gemini' \
    '    COPIED_AGENT_INSTRUCTIONS=1' \
    'fi' \
    '# Codex: AGENTS.md' \
    'CODEX_MD="/host-agent-instructions/codex/AGENTS.md"' \
    '[ ! -f "$CODEX_MD" ] && [ -f "$HF/agent-instructions/codex/AGENTS.md" ] && CODEX_MD="$HF/agent-instructions/codex/AGENTS.md"' \
    'if [ -f "$CODEX_MD" ]; then' \
    '    mkdir -p /home/yolo/.codex' \
    '    sudo cp -a "$CODEX_MD" /home/yolo/.codex/AGENTS.md' \
    '    sudo chown yolo:yolo /home/yolo/.codex/AGENTS.md' \
    '    COPIED_AGENT_INSTRUCTIONS=1' \
    'fi' \
    '# Codex: skills/ directory' \
    'CODEX_SKILLS_DIR="/host-agent-instructions/codex/skills"' \
    '[ ! -d "$CODEX_SKILLS_DIR" ] && [ -d "$HF/agent-instructions/codex/skills" ] && CODEX_SKILLS_DIR="$HF/agent-instructions/codex/skills"' \
    'if [ -d "$CODEX_SKILLS_DIR" ]; then' \
    '    mkdir -p /home/yolo/.codex' \
    '    sudo rm -rf /home/yolo/.codex/skills' \
    '    sudo cp -a "$CODEX_SKILLS_DIR" /home/yolo/.codex/skills' \
    '    sudo chown -R yolo:yolo /home/yolo/.codex/skills' \
    '    COPIED_AGENT_INSTRUCTIONS=1' \
    'fi' \
    '# Pi: AGENTS.md' \
    'PI_MD="/host-agent-instructions/pi/AGENTS.md"' \
    '[ ! -f "$PI_MD" ] && [ -f "$HF/agent-instructions/pi/AGENTS.md" ] && PI_MD="$HF/agent-instructions/pi/AGENTS.md"' \
    'if [ -f "$PI_MD" ]; then' \
    '    mkdir -p /home/yolo/.pi/agent' \
    '    sudo cp -a "$PI_MD" /home/yolo/.pi/agent/AGENTS.md' \
    '    sudo chown -R yolo:yolo /home/yolo/.pi' \
    '    COPIED_AGENT_INSTRUCTIONS=1' \
    'fi' \
    '# Pi: skills/ directory' \
    'PI_SKILLS_DIR="/host-agent-instructions/pi/skills"' \
    '[ ! -d "$PI_SKILLS_DIR" ] && [ -d "$HF/agent-instructions/pi/skills" ] && PI_SKILLS_DIR="$HF/agent-instructions/pi/skills"' \
    'if [ -d "$PI_SKILLS_DIR" ]; then' \
    '    mkdir -p /home/yolo/.pi/agent' \
    '    sudo rm -rf /home/yolo/.pi/agent/skills' \
    '    sudo cp -a "$PI_SKILLS_DIR" /home/yolo/.pi/agent/skills' \
    '    sudo chown -R yolo:yolo /home/yolo/.pi' \
    '    COPIED_AGENT_INSTRUCTIONS=1' \
    'fi' \
    '# Copilot: agents/ directory' \
    'if [ -d /host-agent-instructions/copilot/agents ]; then' \
    '    mkdir -p /home/yolo/.copilot' \
    '    sudo rm -rf /home/yolo/.copilot/agents' \
    '    sudo cp -a /host-agent-instructions/copilot/agents /home/yolo/.copilot/agents' \
    '    sudo chown -R yolo:yolo /home/yolo/.copilot' \
    '    COPIED_AGENT_INSTRUCTIONS=1' \
    'fi' \
    'if [ "$COPIED_AGENT_INSTRUCTIONS" = "1" ]; then' \
    '    echo -e "\033[33m→ Copying global agent instructions and skills to container\033[0m" >&2' \
    'fi' \
    '' \
    '# Enable RTK after host config sync so its in-container hooks are not overwritten' \
    'enable_rtk' \
    '' \
    '# Install built-in yolobox skill from the image (named volume may shadow /home/yolo)' \
    'if [ -d /opt/yolobox/skills/yolobox ]; then' \
    '    mkdir -p /home/yolo/.codex/skills /home/yolo/.claude/skills' \
    '    sudo rm -rf /home/yolo/.codex/skills/yolobox-context' \
    '    sudo rm -rf /home/yolo/.codex/skills/yolobox' \
    '    sudo cp -a /opt/yolobox/skills/yolobox /home/yolo/.codex/skills/yolobox' \
    '    sudo rm -rf /home/yolo/.claude/skills/yolobox' \
    '    sudo cp -a /opt/yolobox/skills/yolobox /home/yolo/.claude/skills/yolobox' \
    '    sudo chown -R yolo:yolo /home/yolo/.codex/skills/yolobox /home/yolo/.claude/skills/yolobox' \
    'fi' \
    '' \
    '# Inject built-in agent guidance so Claude and Codex use the yolobox skill when it matters' \
    'inject_agent_guidance /home/yolo/.claude/CLAUDE.md /opt/yolobox/agent-instructions/claude/yolobox.md "<!-- BEGIN YOLOBOX MANAGED BLOCK -->" "<!-- END YOLOBOX MANAGED BLOCK -->"' \
    'inject_agent_guidance /home/yolo/.codex/AGENTS.md /opt/yolobox/agent-instructions/codex/yolobox.md "# BEGIN YOLOBOX MANAGED BLOCK" "# END YOLOBOX MANAGED BLOCK"' \
    '' \
    '# Handle Docker socket access without mutating host socket permissions' \
    'if [ -S /var/run/docker.sock ]; then' \
    '    DOCKER_GID=$(stat -c %g /var/run/docker.sock 2>/dev/null || true)' \
    '    if [ -n "$DOCKER_GID" ] && ! id -G yolo | tr " " "\n" | grep -qx "$DOCKER_GID"; then' \
    '        DOCKER_GROUP=$(getent group "$DOCKER_GID" | cut -d: -f1 | head -1)' \
    '        if [ -z "$DOCKER_GROUP" ]; then' \
    '            DOCKER_GROUP=yolobox-docker' \
    '            sudo groupadd -g "$DOCKER_GID" "$DOCKER_GROUP" >/dev/null 2>&1 || true' \
    '        fi' \
    '        sudo usermod -aG "$DOCKER_GROUP" yolo >/dev/null 2>&1 || true' \
    '        _YOLOBOX_NEED_REGROUP=1' \
    '    fi' \
    'fi' \
    '' \
    '# Ensure npm-global prefix dir exists (named volume may shadow /home/yolo)' \
    'mkdir -p /home/yolo/.npm-global' \
    '' \
    '# Pin Claude to image version (named volume may contain an older install)' \
    'mkdir -p /home/yolo/.local/bin' \
    'ln -sf /usr/local/bin/claude /home/yolo/.local/bin/claude' \
    '' \
    '# Auto-trust project directory for Claude Code (this is yolobox after all)' \
    'if [ -n "$YOLOBOX_PROJECT_PATH" ]; then' \
    '    CLAUDE_JSON="/home/yolo/.claude.json"' \
    '    if [ ! -f "$CLAUDE_JSON" ]; then' \
    '        echo '"'"'{"projects":{}}'"'"' > "$CLAUDE_JSON"' \
    '    fi' \
    '    if command -v jq &> /dev/null; then' \
    '        TMP=$(mktemp)' \
    '        jq --arg path "$YOLOBOX_PROJECT_PATH" '"'"'.projects[$path] = (.projects[$path] // {}) + {"hasTrustDialogAccepted": true}'"'"' "$CLAUDE_JSON" > "$TMP" && mv "$TMP" "$CLAUDE_JSON"' \
    '        chown yolo:yolo "$CLAUDE_JSON"' \
    '    fi' \
    'fi' \
    '' \
    '# Re-exec with refreshed groups if we added docker group above' \
    'if [ "$_YOLOBOX_NEED_REGROUP" = "1" ]; then' \
    '    exec sudo -E --preserve-env=PATH setpriv --reuid="$(id -u)" --regid="$(id -g)" --init-groups -- "$@"' \
    'fi' \
    'yolobox_timing_mark "exec command"' \
    'exec "$@"' \
    > /usr/local/bin/yolobox-entrypoint.sh && \
    chmod +x /usr/local/bin/yolobox-entrypoint.sh

USER yolo

# Create npm-global prefix dir (also created in entrypoint for existing named volumes)
RUN mkdir -p /home/yolo/.npm-global

# Set up a fun prompt and aliases
RUN echo 'PS1="\\[\\033[35m\\]yolo\\[\\033[0m\\]:\\[\\033[36m\\]\\w\\[\\033[0m\\] 🎲 "' >> ~/.bashrc \
    && echo 'alias ll="ls -la"' >> ~/.bashrc \
    && echo 'alias la="ls -A"' >> ~/.bashrc \
    && echo 'alias l="ls -CF"' >> ~/.bashrc \
    && echo 'alias yeet="rm -rf"' >> ~/.bashrc

# Welcome message
RUN echo 'echo ""' >> ~/.bashrc \
    && echo 'echo -e "\\033[1;35m  Welcome to yolobox!\\033[0m"' >> ~/.bashrc \
    && echo 'echo -e "\\033[33m  Your home directory is safe. Go wild.\\033[0m"' >> ~/.bashrc \
    && echo 'echo ""' >> ~/.bashrc

# =============================================================================
# VOLATILE LAYERS — change when bumping AI CLI versions
# Placed last so upgrades only re-download these layers, not the stable base.
# =============================================================================

# AI coding CLIs (updated more frequently than dev tools above)
# NPM_CONFIG_PREFIX is set above for runtime user installs; unset it here
# so these install to the default system location like the dev tools.
USER root
RUN NPM_CONFIG_PREFIX="" NPM_CONFIG_MIN_RELEASE_AGE="${NPM_MIN_RELEASE_AGE_DAYS}" npm install -g --no-audit --no-fund \
    @google/gemini-cli \
    @openai/codex \
    opencode-ai \
    @github/copilot \
    @earendil-works/pi-coding-agent \
    && NPM_CONFIG_PREFIX="" npm cache clean --force

# RTK command-output compression proxy. Intentionally not pinned: the base image
# captures the latest RTK release available when yolobox is built.
RUN curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/develop/install.sh | RTK_INSTALL_DIR=/usr/local/bin sh
USER yolo

# Copy Claude Code from installer stage
USER root
COPY --from=claude-installer /root/.local/bin/claude /usr/local/bin/claude
USER yolo

# Create symlink for Claude at ~/.local/bin (host config expects it there)
# Then run `claude install` to register installation metadata so `claude update` works
RUN mkdir -p /home/yolo/.local/bin && \
    ln -s /usr/local/bin/claude /home/yolo/.local/bin/claude && \
    claude install || true

WORKDIR /home/yolo

# Working directory is set by yolobox CLI to the actual project path

ENTRYPOINT ["/usr/local/bin/yolobox-entrypoint.sh"]
CMD ["bash"]
