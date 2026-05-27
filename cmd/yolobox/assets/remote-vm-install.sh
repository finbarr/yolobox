#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

ready_marker="${YOLOBOX_REMOTE_READY_MARKER:-/opt/yolobox/remote/ready}"

if [ -f "$ready_marker" ]; then
  exit 0
fi

if command -v cloud-init >/dev/null 2>&1; then
  echo "remote setup: waiting for cloud-init"
  cloud-init status --wait >/dev/null 2>&1 || true
fi

install_log="${YOLOBOX_REMOTE_INSTALL_LOG:-/var/log/yolobox-remote-install.log}"
mkdir -p "$(dirname "$install_log")"
: > "$install_log"
exec 3>&1
trap 'status=$?; if [ "$status" -ne 0 ]; then echo "yolobox remote installer failed; recent log follows (${install_log})" >&3; tail -n 80 "$install_log" >&3 || true; fi; exit "$status"' EXIT
exec >>"$install_log" 2>&1

step() {
  echo "remote setup: $*" >&3
}

apt_install() {
  step "installing base packages"
  apt-get update
  apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    curl \
    wget \
    git \
    sudo \
    build-essential \
    make \
    cmake \
    pkg-config \
    python3 \
    python3-pip \
    python3-venv \
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
    libssl-dev \
    ncurses-bin \
    tmux
  ln -sf /usr/bin/batcat /usr/local/bin/bat 2>/dev/null || true
  ln -sf /usr/bin/fdfind /usr/local/bin/fd 2>/dev/null || true
}

install_node() {
  step "checking Node.js"
  local major
  major="$(node -v 2>/dev/null | sed -E 's/^v([0-9]+).*/\1/' || true)"
  if [ "${major:-0}" -ge 22 ] 2>/dev/null; then
    return 0
  fi
  curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
  apt-get install -y nodejs
}

install_gh() {
  step "checking GitHub CLI"
  if command -v gh >/dev/null 2>&1; then
    return 0
  fi
  install -m 0755 -d /usr/share/keyrings
  curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /usr/share/keyrings/githubcli-archive-keyring.gpg
  chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list
  apt-get update
  apt-get install -y gh
}

install_docker() {
  step "checking Docker Engine"
  if ! command -v docker >/dev/null 2>&1; then
    curl -fsSL https://get.docker.com | sh
  fi
  systemctl enable docker >/dev/null 2>&1 || true
  systemctl start docker >/dev/null 2>&1 || service docker start >/dev/null 2>&1 || true
  docker network create yolobox-net >/dev/null 2>&1 || true
}

install_go() {
  step "checking Go"
  if command -v go >/dev/null 2>&1; then
    return 0
  fi
  local go_version arch tarball tmp
  go_version="${YOLOBOX_REMOTE_GO_VERSION:-1.25.6}"
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) return 0 ;;
  esac
  tarball="go${go_version}.linux-${arch}.tar.gz"
  tmp="$(mktemp -d)"
  curl -fsSL "https://go.dev/dl/${tarball}" -o "${tmp}/${tarball}"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${tmp}/${tarball}"
  rm -rf "$tmp"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
}

install_bun() {
  step "checking Bun"
  if command -v bun >/dev/null 2>&1; then
    return 0
  fi
  curl -fsSL https://bun.sh/install | BUN_INSTALL=/opt/bun bash
  ln -sf /opt/bun/bin/bun /usr/local/bin/bun
  ln -sf /opt/bun/bin/bun /usr/local/bin/bunx
}

install_uv() {
  step "checking uv"
  if command -v uv >/dev/null 2>&1 && command -v uvx >/dev/null 2>&1; then
    return 0
  fi
  curl -LsSf https://astral.sh/uv/install.sh | env UV_INSTALL_DIR=/usr/local/bin sh
}

install_ai_clis() {
  step "installing AI CLIs"
  NPM_CONFIG_PREFIX="" npm install -g --no-audit --no-fund \
    @google/gemini-cli \
    @openai/codex \
    opencode-ai \
    @github/copilot \
    @earendil-works/pi-coding-agent
  NPM_CONFIG_PREFIX="" npm cache clean --force >/dev/null 2>&1 || true
}

install_claude() {
  step "checking Claude Code"
  if ! command -v claude >/dev/null 2>&1; then
    curl -fsSL https://claude.ai/install.sh | bash
  fi
  if [ -x /root/.local/bin/claude ]; then
    ln -sf /root/.local/bin/claude /usr/local/bin/claude
  fi
}

install_rtk() {
  step "checking RTK"
  if command -v rtk >/dev/null 2>&1; then
    return 0
  fi
  curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/develop/install.sh | RTK_INSTALL_DIR=/usr/local/bin sh
}

install_yolo_user() {
  step "configuring yolo user"
  if ! id -u yolo >/dev/null 2>&1; then
    useradd -m -s /bin/bash yolo
  fi
  echo "yolo ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/yolo
  chmod 0440 /etc/sudoers.d/yolo
}

install_wrappers() {
  step "writing yolobox command wrappers"
  mkdir -p /opt/yolobox/bin
  cat > /opt/yolobox/wrapper-template <<'EOF'
#!/bin/bash
WRAPPER_DIR=/opt/yolobox/bin
CMD=$(basename "$0")
CLEAN_PATH=$(printf "%s" "$PATH" | tr ":" "\n" | grep -v "^$WRAPPER_DIR$" | tr "\n" ":" | sed 's/:$//')
REAL_BIN=$(PATH="$CLEAN_PATH" command -v "$CMD" 2>/dev/null || true)
if [ -z "$REAL_BIN" ]; then
  echo "Error: $CMD not found" >&2
  exit 1
fi
if [ "${NO_YOLO:-}" = "1" ]; then
  exec "$REAL_BIN" "$@"
fi
EOF

  cp /opt/yolobox/wrapper-template /opt/yolobox/bin/claude
  echo 'exec "$REAL_BIN" --dangerously-skip-permissions "$@"' >> /opt/yolobox/bin/claude

  cp /opt/yolobox/wrapper-template /opt/yolobox/bin/codex
  echo 'exec "$REAL_BIN" --ask-for-approval never --sandbox danger-full-access "$@"' >> /opt/yolobox/bin/codex

  cp /opt/yolobox/wrapper-template /opt/yolobox/bin/gemini
  echo 'exec "$REAL_BIN" --yolo "$@"' >> /opt/yolobox/bin/gemini

  cp /opt/yolobox/wrapper-template /opt/yolobox/bin/opencode
  echo 'exec "$REAL_BIN" "$@"' >> /opt/yolobox/bin/opencode

  cp /opt/yolobox/wrapper-template /opt/yolobox/bin/copilot
  echo 'exec "$REAL_BIN" --yolo "$@"' >> /opt/yolobox/bin/copilot

  cp /opt/yolobox/wrapper-template /opt/yolobox/bin/pi
  echo 'exec "$REAL_BIN" "$@"' >> /opt/yolobox/bin/pi

  cat > /opt/yolobox/bin/open <<'EOF'
#!/bin/bash
if [ "$#" -ne 1 ]; then
  echo "usage: open <url>" >&2
  exit 2
fi
echo "Open this URL in your browser: $1" >&2
EOF
  ln -sf open /opt/yolobox/bin/xdg-open

  chmod +x /opt/yolobox/bin/*
}

install_git_credential_helper() {
  step "writing Git credential helper"
  cat > /opt/yolobox/bin/git-credential-github-token <<'EOF'
#!/bin/sh
case "${1:-}" in
  get) ;;
  *) exit 0 ;;
esac
protocol=""
host=""
while IFS= read -r line; do
  [ -z "$line" ] && break
  case "$line" in
    protocol=*) protocol=${line#protocol=} ;;
    host=*) host=${line#host=} ;;
  esac
done
[ "$protocol" = "https" ] || exit 0
[ "$host" = "github.com" ] || exit 0
token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
[ -n "$token" ] || exit 0
printf "username=x-access-token\n"
printf "password=%s\n" "$token"
EOF
  chmod +x /opt/yolobox/bin/git-credential-github-token
  git config --system --add credential.https://github.com.helper "" || true
  git config --system --add credential.https://github.com.helper "!/opt/yolobox/bin/git-credential-github-token" || true
}

install_upsert_block() {
  step "writing managed instruction updater"
  cat > /usr/local/bin/yolobox-upsert-block <<'EOF'
#!/usr/bin/env python3
import pathlib
import sys

if len(sys.argv) != 5:
    raise SystemExit("usage: yolobox-upsert-block <target> <source> <start> <end>")

target = pathlib.Path(sys.argv[1])
source = pathlib.Path(sys.argv[2])
start_marker = sys.argv[3]
end_marker = sys.argv[4]

target.parent.mkdir(parents=True, exist_ok=True)
existing_lines = []
if target.exists():
    skip = False
    for line in target.read_text().splitlines():
        if line == start_marker:
            skip = True
            continue
        if line == end_marker:
            skip = False
            continue
        if not skip:
            existing_lines.append(line)

while existing_lines and existing_lines[-1] == "":
    existing_lines.pop()

payload_lines = source.read_text().rstrip("\n").splitlines()
output_lines = []
if existing_lines:
    output_lines.extend(existing_lines)
    output_lines.append("")
output_lines.append(start_marker)
output_lines.extend(payload_lines)
output_lines.append(end_marker)
target.write_text("\n".join(output_lines) + "\n")
EOF
  chmod +x /usr/local/bin/yolobox-upsert-block
}

install_remote_session() {
  step "writing remote session launcher"
  cat > /usr/local/bin/yolobox-remote-session <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

workdir="${1:-${YOLOBOX_PROJECT_PATH:-$(pwd)}}"
home_dir="${HOME:-/root}"

export YOLOBOX=1
export YOLOBOX_REMOTE=1
export YOLOBOX_PROJECT_PATH="$workdir"
export YOLOBOX_CONTEXT_FILE="${YOLOBOX_CONTEXT_FILE:-/run/yolobox/context.json}"
export NPM_CONFIG_PREFIX="${NPM_CONFIG_PREFIX:-${home_dir}/.npm-global}"

mkdir -p "$workdir" "$home_dir/.npm-global" "$home_dir/.codex/skills" "$home_dir/.claude/skills" /run/yolobox
ln -sfn "$workdir" /workspace
git config --global --add safe.directory "$workdir" >/dev/null 2>&1 || true
docker network create yolobox-net >/dev/null 2>&1 || true

if command -v jq >/dev/null 2>&1; then
  jq -n \
    --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg workdir "$workdir" \
    --arg home "$home_dir" \
    '{
      schema_version: 1,
      inside_yolobox: true,
      remote: true,
      yolobox_version: "remote-vm",
      generated_at: $generated_at,
      runtime: {
        configured: "remote-vm",
        selected: "remote-vm",
        apple_container: false,
        rootless_podman: false
      },
      launch: {
        interactive: true,
        command: [],
        working_dir: $workdir,
        context_file: "/run/yolobox/context.json",
        auto_passthrough_env_keys: [],
        gh_token_forwarded: false
      },
      paths: {
        project: $workdir,
        home: $home
      },
      config: {
        runtime: "remote-vm",
        image: "yolobox-vm",
        container_name: "",
        default_harness: "none",
        mounts: [],
        env_keys: [],
        exclude: [],
        copy_as: [],
        ssh_agent: false,
        readonly_project: false,
        no_project: false,
        no_network: false,
        no_env_passthrough: false,
        network: "host",
        pod: "",
        no_yolo: false,
        scratch: false,
        claude_config: false,
        codex_config: false,
        gemini_config: false,
        opencode_config: false,
        pi_config: false,
        git_config: false,
        gh_token: false,
        rtk: false,
        copy_agent_instructions: false,
        docker: true,
        clipboard: false,
        open_bridge: false,
        cpus: "",
        memory: "",
        shm_size: "",
        gpus: "",
        devices: [],
        cap_add: [],
        cap_drop: [],
        runtime_args: [],
        customize: {
          packages: [],
          dockerfile: ""
        }
      }
    }' > "$YOLOBOX_CONTEXT_FILE"
fi

if [ -d /opt/yolobox/skills/yolobox ]; then
  rm -rf "$home_dir/.codex/skills/yolobox-context" "$home_dir/.codex/skills/yolobox" "$home_dir/.claude/skills/yolobox"
  cp -a /opt/yolobox/skills/yolobox "$home_dir/.codex/skills/yolobox"
  cp -a /opt/yolobox/skills/yolobox "$home_dir/.claude/skills/yolobox"
fi

if [ -f /opt/yolobox/agent-instructions/codex/yolobox.md ]; then
  /usr/local/bin/yolobox-upsert-block "$home_dir/.codex/AGENTS.md" /opt/yolobox/agent-instructions/codex/yolobox.md "# BEGIN YOLOBOX MANAGED BLOCK" "# END YOLOBOX MANAGED BLOCK" || true
fi
if [ -f /opt/yolobox/agent-instructions/claude/yolobox.md ]; then
  /usr/local/bin/yolobox-upsert-block "$home_dir/.claude/CLAUDE.md" /opt/yolobox/agent-instructions/claude/yolobox.md "<!-- BEGIN YOLOBOX MANAGED BLOCK -->" "<!-- END YOLOBOX MANAGED BLOCK -->" || true
fi

if [ -f "$home_dir/.claude.json" ] || command -v jq >/dev/null 2>&1; then
  claude_json="$home_dir/.claude.json"
  if [ ! -f "$claude_json" ]; then
    echo '{"projects":{}}' > "$claude_json"
  fi
  tmp="$(mktemp)"
  jq --arg path "$workdir" '.projects[$path] = (.projects[$path] // {}) + {"hasTrustDialogAccepted": true}' "$claude_json" > "$tmp" && mv "$tmp" "$claude_json" || rm -f "$tmp"
fi
EOF
  chmod +x /usr/local/bin/yolobox-remote-session
}

install_yolobox_agent() {
  step "writing yolobox machine agent"
  install -d -m 0755 /etc/yolobox
  if [ ! -f /etc/yolobox/agent.env ]; then
    install -m 0600 /dev/null /etc/yolobox/agent.env
  fi
  install -d -m 0755 /usr/local/lib/yolobox
  cat > /usr/local/lib/yolobox/agent.mjs <<'EOF'
#!/usr/bin/env node
import net from "node:net";

const backendURL = (process.env.YOLOBOX_AGENT_BACKEND_URL || "").replace(/\/+$/, "");
const token = process.env.YOLOBOX_AGENT_TOKEN || "";
const heartbeatInterval = Number(process.env.YOLOBOX_AGENT_HEARTBEAT_INTERVAL || 30_000);

if (!backendURL || !token) {
  console.error("yolobox agent token/backend URL is not configured");
  process.exit(0);
}

const streams = new Map();
let socket;

connect();

function tunnelURL() {
  const url = new URL(backendURL);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = "/v1/agent/tunnel";
  url.search = "";
  return url;
}

function connect(delay = 1000) {
  socket = new WebSocket(tunnelURL(), {
    headers: { Authorization: `Bearer ${token}` },
  });

  let heartbeat;
  socket.addEventListener("open", () => {
    heartbeat = setInterval(() => send({ type: "ping" }), heartbeatInterval);
    send({ type: "ping" });
  });

  socket.addEventListener("message", (event) => {
    handleMessage(event.data).catch((error) => {
      console.error(`yolobox agent message failed: ${error.message}`);
    });
  });

  socket.addEventListener("close", () => {
    if (heartbeat) clearInterval(heartbeat);
    for (const stream of streams.values()) stream.destroy();
    streams.clear();
    setTimeout(() => connect(Math.min(delay * 2, 15000)), delay);
  });

  socket.addEventListener("error", () => {});
}

async function handleMessage(data) {
  const message = JSON.parse(typeof data === "string" ? data : Buffer.from(await data.arrayBuffer()).toString("utf8"));
  const id = message.stream_id || "";
  switch (message.type) {
  case "open":
    openStream(id, message.host || "127.0.0.1", Number(message.port || 22));
    return;
  case "data": {
    const stream = streams.get(id);
    if (stream) stream.write(Buffer.from(message.data || "", "base64"));
    return;
  }
  case "eof": {
    const stream = streams.get(id);
    if (stream) stream.end();
    return;
  }
  case "close": {
    const stream = streams.get(id);
    if (stream) stream.destroy();
    streams.delete(id);
    return;
  }
  }
}

function openStream(id, host, port) {
  if (!id) return;
  const stream = net.connect({ host, port });
  streams.set(id, stream);
  stream.on("connect", () => send({ type: "opened", stream_id: id }));
  stream.on("data", (chunk) => send({ type: "data", stream_id: id, data: chunk.toString("base64") }));
  stream.on("end", () => send({ type: "eof", stream_id: id }));
  stream.on("close", () => {
    send({ type: "close", stream_id: id });
    streams.delete(id);
  });
  stream.on("error", (error) => {
    send({ type: "error", stream_id: id, message: error.message });
    streams.delete(id);
  });
}

function send(message) {
  if (socket?.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(message));
  }
}
EOF
  chmod +x /usr/local/lib/yolobox/agent.mjs

  cat > /etc/systemd/system/yolobox-agent.service <<'EOF'
[Unit]
Description=Yolobox machine agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/yolobox/agent.env
ExecStart=/usr/bin/env node /usr/local/lib/yolobox/agent.mjs
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl enable yolobox-agent >/dev/null 2>&1 || true
  systemctl restart yolobox-agent >/dev/null 2>&1 || true
}

copy_repo_assets_if_present() {
  step "copying bundled assets"
  local source_dir="${YOLOBOX_SOURCE_DIR:-}"
  local tmp_dir=""
  if [ -z "$source_dir" ] || [ ! -d "$source_dir" ]; then
    local ref archive
    ref="${YOLOBOX_REMOTE_REPO_REF:-master}"
    tmp_dir="$(mktemp -d)"
    archive="${tmp_dir}/yolobox.tgz"
    if curl -fsSL "https://github.com/finbarr/yolobox/archive/${ref}.tar.gz" -o "$archive"; then
      mkdir -p "${tmp_dir}/src"
      tar -xzf "$archive" -C "${tmp_dir}/src" --strip-components=1
      source_dir="${tmp_dir}/src"
    else
      rm -rf "$tmp_dir"
      return 0
    fi
  fi
  mkdir -p /opt/yolobox
  if [ -d "$source_dir/skills" ]; then
    rm -rf /opt/yolobox/skills
    cp -a "$source_dir/skills" /opt/yolobox/skills
  fi
  if [ -d "$source_dir/agent-instructions" ]; then
    rm -rf /opt/yolobox/agent-instructions
    cp -a "$source_dir/agent-instructions" /opt/yolobox/agent-instructions
  fi
  if [ -n "$tmp_dir" ]; then
    rm -rf "$tmp_dir"
  fi
}

write_profile() {
  step "writing shell profile"
  cat > /etc/profile.d/yolobox-remote.sh <<'EOF'
export PATH="/opt/yolobox/bin:/root/.npm-global/bin:/home/yolo/.npm-global/bin:/usr/local/go/bin:$PATH"
export YOLOBOX=1
export YOLOBOX_REMOTE=1
export NPM_CONFIG_PREFIX="${NPM_CONFIG_PREFIX:-$HOME/.npm-global}"
EOF
}

apt_install
install_node
install_gh
install_docker
install_go
install_bun
install_uv
install_ai_clis
install_claude
install_rtk
install_yolo_user
install_wrappers
install_git_credential_helper
install_upsert_block
install_remote_session
install_yolobox_agent
copy_repo_assets_if_present
write_profile

step "marking remote runtime ready"
mkdir -p "$(dirname "$ready_marker")" /opt/yolobox/project
chmod 755 /opt /opt/yolobox /opt/yolobox/project
date -u +%Y-%m-%dT%H:%M:%SZ > "$ready_marker"
step "complete"
