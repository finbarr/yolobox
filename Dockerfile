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

# Install system packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Essentials
    bash \
    fish \
    zsh \
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
    jq \
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
    # For native node modules
    libssl-dev \
    # For terminfo compilation (Ghostty support)
    ncurses-bin \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js 22 LTS
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update \
    && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Install Bun (from official image)
COPY --from=bun-source /usr/local/bin/bun /usr/local/bin/bun
RUN ln -s /usr/local/bin/bun /usr/local/bin/bunx

# Create symlinks for bat/fd (Debian/Ubuntu rename these binaries)
RUN ln -s /usr/bin/batcat /usr/local/bin/bat && \
    ln -s /usr/bin/fdfind /usr/local/bin/fd

# Install global npm packages and AI CLIs
RUN npm install -g \
    typescript \
    ts-node \
    yarn \
    pnpm \
    @google/gemini-cli \
    @openai/codex \
    opencode-ai \
    @github/copilot

# Install Go (from official image)
COPY --from=go-source /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:$PATH"

# Install uv (fast Python package manager)
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/

# Create yolo user with passwordless sudo
RUN useradd -m -s /bin/bash yolo \
    && echo "yolo ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/yolo \
    && chmod 0440 /etc/sudoers.d/yolo

# Set up directories
RUN mkdir -p /workspace /output /secrets \
    && chown yolo:yolo /workspace /output

# Copy Claude Code from installer stage
COPY --from=claude-installer /root/.local/bin/claude /usr/local/bin/claude

USER yolo

# Create symlink for Claude at ~/.local/bin (host config expects it there)
# Then run `claude install` to register installation metadata so `claude update` works
RUN mkdir -p /home/yolo/.local/bin && \
    ln -s /usr/local/bin/claude /home/yolo/.local/bin/claude && \
    claude install || true
WORKDIR /home/yolo

# Set up a fun prompt and aliases
RUN echo 'PS1="\\[\\033[35m\\]yolo\\[\\033[0m\\]:\\[\\033[36m\\]\\w\\[\\033[0m\\] 🎲 "' >> ~/.bashrc \
    && echo 'alias ll="ls -la"' >> ~/.bashrc \
    && echo 'alias la="ls -A"' >> ~/.bashrc \
    && echo 'alias l="ls -CF"' >> ~/.bashrc \
    && echo 'alias yeet="rm -rf"' >> ~/.bashrc

# Fish config (in /etc/ since fish doesn't have the skeleton PS1 issue)
USER root
RUN mkdir -p /etc/fish/conf.d && \
    printf '%s\n' \
    '# yolobox fish configuration' \
    '# Disable default greeting' \
    'set -g fish_greeting ""' \
    '' \
    '# Custom prompt matching yolo theme' \
    'function fish_prompt' \
    '    set_color magenta' \
    '    echo -n "yolo"' \
    '    set_color normal' \
    '    echo -n ":"' \
    '    set_color cyan' \
    '    echo -n (prompt_pwd)' \
    '    set_color normal' \
    '    echo -n " 🎲 "' \
    'end' \
    '' \
    '# Aliases (same as bash)' \
    'alias ll "ls -la"' \
    'alias la "ls -A"' \
    'alias l "ls -CF"' \
    'alias yeet "rm -rf"' \
    '' \
    '# Welcome message (interactive only)' \
    'if status is-interactive' \
    '    echo ""' \
    '    set_color --bold magenta' \
    '    echo "  Welcome to yolobox!"' \
    '    set_color normal' \
    '    set_color yellow' \
    '    echo "  Your home directory is safe. Go wild."' \
    '    set_color normal' \
    '    echo ""' \
    'end' \
    > /etc/fish/conf.d/yolobox.fish

# Verify fish configuration is valid
RUN fish -c 'source /etc/fish/conf.d/yolobox.fish'

# Zsh config (in /etc/zsh/zshrc for system-wide sourcing)
RUN mkdir -p /etc/zsh && \
    printf '%s\n' \
    '# yolobox zsh configuration' \
    '' \
    '# Custom prompt matching yolo theme' \
    'PROMPT="%F{magenta}yolo%f:%F{cyan}%~%f 🎲 "' \
    '' \
    '# Aliases (same as bash/fish)' \
    'alias ll="ls -la"' \
    'alias la="ls -A"' \
    'alias l="ls -CF"' \
    'alias yeet="rm -rf"' \
    '' \
    '# Welcome message (interactive only)' \
    'if [[ -o interactive ]]; then' \
    '    echo ""' \
    '    print -P "%B%F{magenta}  Welcome to yolobox!%f%b"' \
    '    print -P "%F{yellow}  Your home directory is safe. Go wild.%f"' \
    '    echo ""' \
    'fi' \
    > /etc/zsh/zshrc

# Verify zsh configuration is valid
RUN zsh -c 'source /etc/zsh/zshrc'

# Install Ghostty terminfo (not in Ubuntu's ncurses yet, needs 6.5+)
# Prevents "Could not set up terminal" warnings when TERM=xterm-ghostty
RUN cat <<'EOF' | tic -x -
xterm-ghostty|ghostty|Ghostty,
    am, bce, ccc, hs, km, mc5i, mir, msgr, npc, xenl, AX, Su, Tc, XT, fullkbd,
    colors#256, cols#80, it#8, lines#24, pairs#32767,
    acsc=++\,\,--..00``aaffgghhiijjkkllmmnnooppqqrrssttuuvvwwxxyyzz{{||}}~~,
    bel=^G, blink=\E[5m, bold=\E[1m, cbt=\E[Z, civis=\E[?25l,
    clear=\E[H\E[2J, cnorm=\E[?12l\E[?25h, cr=^M,
    csr=\E[%i%p1%d;%p2%dr, cub=\E[%p1%dD, cub1=^H,
    cud=\E[%p1%dB, cud1=^J, cuf=\E[%p1%dC, cuf1=\E[C,
    cup=\E[%i%p1%d;%p2%dH, cuu=\E[%p1%dA, cuu1=\E[A,
    cvvis=\E[?12;25h, dch=\E[%p1%dP, dch1=\E[P, dim=\E[2m,
    dl=\E[%p1%dM, dl1=\E[M, dsl=\E]2;\007, ech=\E[%p1%dX,
    ed=\E[J, el=\E[K, el1=\E[1K, flash=\E[?5h$<100/>\E[?5l,
    fsl=^G, home=\E[H, hpa=\E[%i%p1%dG, ht=^I, hts=\EH,
    ich=\E[%p1%d@, ich1=\E[@, il=\E[%p1%dL, il1=\E[L, ind=^J,
    indn=\E[%p1%dS,
    initc=\E]4;%p1%d;rgb\:%p2%{255}%*%{1000}%/%2.2X/%p3%{255}%*%{1000}%/%2.2X/%p4%{255}%*%{1000}%/%2.2X\E\\,
    invis=\E[8m, kDC=\E[3;2~, kEND=\E[1;2F, kHOM=\E[1;2H,
    kIC=\E[2;2~, kLFT=\E[1;2D, kNXT=\E[6;2~, kPRV=\E[5;2~,
    kRIT=\E[1;2C, kbs=\177, kcbt=\E[Z, kcub1=\EOD, kcud1=\EOB,
    kcuf1=\EOC, kcuu1=\EOA, kdch1=\E[3~, kend=\EOF, kent=\EOM,
    kf1=\EOP, kf10=\E[21~, kf11=\E[23~, kf12=\E[24~,
    kf13=\E[1;2P, kf14=\E[1;2Q, kf15=\E[1;2R, kf16=\E[1;2S,
    kf17=\E[15;2~, kf18=\E[17;2~, kf19=\E[18;2~, kf2=\EOQ,
    kf20=\E[19;2~, kf21=\E[20;2~, kf22=\E[21;2~,
    kf23=\E[23;2~, kf24=\E[24;2~, kf25=\E[1;5P, kf26=\E[1;5Q,
    kf27=\E[1;5R, kf28=\E[1;5S, kf29=\E[15;5~, kf3=\EOR,
    kf30=\E[17;5~, kf31=\E[18;5~, kf32=\E[19;5~,
    kf33=\E[20;5~, kf34=\E[21;5~, kf35=\E[23;5~,
    kf36=\E[24;5~, kf37=\E[1;6P, kf38=\E[1;6Q, kf39=\E[1;6R,
    kf4=\EOS, kf40=\E[1;6S, kf41=\E[15;6~, kf42=\E[17;6~,
    kf43=\E[18;6~, kf44=\E[19;6~, kf45=\E[20;6~,
    kf46=\E[21;6~, kf47=\E[23;6~, kf48=\E[24;6~,
    kf49=\E[1;3P, kf5=\E[15~, kf50=\E[1;3Q, kf51=\E[1;3R,
    kf52=\E[1;3S, kf53=\E[15;3~, kf54=\E[17;3~,
    kf55=\E[18;3~, kf56=\E[19;3~, kf57=\E[20;3~,
    kf58=\E[21;3~, kf59=\E[23;3~, kf6=\E[17~, kf60=\E[24;3~,
    kf61=\E[1;4P, kf62=\E[1;4Q, kf63=\E[1;4R, kf7=\E[18~,
    kf8=\E[19~, kf9=\E[20~, khome=\EOH, kich1=\E[2~,
    kind=\E[1;2B, kmous=\E[<, knp=\E[6~, kpp=\E[5~,
    kri=\E[1;2A, oc=\E]104\007, op=\E[39;49m, rc=\E8,
    rep=%p1%c\E[%p2%{1}%-%db, rev=\E[7m, ri=\EM,
    rin=\E[%p1%dT, ritm=\E[23m, rmacs=\E(B, rmam=\E[?7l,
    rmcup=\E[?1049l, rmir=\E[4l, rmkx=\E[?1l\E>, rmso=\E[27m,
    rmul=\E[24m, rs1=\E]\E\\\Ec, sc=\E7,
    setab=\E[%?%p1%{8}%<%t4%p1%d%e%p1%{16}%<%t10%p1%{8}%-%d%e48;5;%p1%d%;m,
    setaf=\E[%?%p1%{8}%<%t3%p1%d%e%p1%{16}%<%t9%p1%{8}%-%d%e38;5;%p1%d%;m,
    sgr=%?%p9%t\E(0%e\E(B%;\E[0%?%p6%t;1%;%?%p2%t;4%;%?%p1%p3%|%t;7%;%?%p4%t;5%;%?%p7%t;8%;m,
    sgr0=\E(B\E[m, sitm=\E[3m, smacs=\E(0, smam=\E[?7h,
    smcup=\E[?1049h, smir=\E[4h, smkx=\E[?1h\E=, smso=\E[7m,
    smul=\E[4m, tbc=\E[3g, tsl=\E]2;, u6=\E[%i%d;%dR, u7=\E[6n,
    u8=\E[?%[;0123456789]c, u9=\E[c, vpa=\E[%i%p1%dd,
    BD=\E[?2004l, BE=\E[?2004h, Clmg=\E[s,
    Cmg=\E[%i%p1%d;%p2%ds, Dsmg=\E[?69l, E3=\E[3J,
    Enmg=\E[?69h, Ms=\E]52;%p1%s;%p2%s\007, PE=\E[201~,
    PS=\E[200~, RV=\E[>c, Se=\E[2 q,
    Setulc=\E[58\:2\:\:%p1%{65536}%/%d\:%p1%{256}%/%{255}%&%d\:%p1%{255}%&%d%;m,
    Smulx=\E[4\:%p1%dm, Ss=\E[%p1%d q,
    Sync=\E[?2026%?%p1%{1}%-%tl%eh%;,
    XM=\E[?1006;1000%?%p1%{1}%=%th%el%;, XR=\E[>0q,
    fd=\E[?1004l, fe=\E[?1004h, kDC3=\E[3;3~, kDC4=\E[3;4~,
    kDC5=\E[3;5~, kDC6=\E[3;6~, kDC7=\E[3;7~, kDN=\E[1;2B,
    kDN3=\E[1;3B, kDN4=\E[1;4B, kDN5=\E[1;5B, kDN6=\E[1;6B,
    kDN7=\E[1;7B, kEND3=\E[1;3F, kEND4=\E[1;4F,
    kEND5=\E[1;5F, kEND6=\E[1;6F, kEND7=\E[1;7F,
    kHOM3=\E[1;3H, kHOM4=\E[1;4H, kHOM5=\E[1;5H,
    kHOM6=\E[1;6H, kHOM7=\E[1;7H, kIC3=\E[2;3~, kIC4=\E[2;4~,
    kIC5=\E[2;5~, kIC6=\E[2;6~, kIC7=\E[2;7~, kLFT3=\E[1;3D,
    kLFT4=\E[1;4D, kLFT5=\E[1;5D, kLFT6=\E[1;6D,
    kLFT7=\E[1;7D, kNXT3=\E[6;3~, kNXT4=\E[6;4~,
    kNXT5=\E[6;5~, kNXT6=\E[6;6~, kNXT7=\E[6;7~,
    kPRV3=\E[5;3~, kPRV4=\E[5;4~, kPRV5=\E[5;5~,
    kPRV6=\E[5;6~, kPRV7=\E[5;7~, kRIT3=\E[1;3C,
    kRIT4=\E[1;4C, kRIT5=\E[1;5C, kRIT6=\E[1;6C,
    kRIT7=\E[1;7C, kUP=\E[1;2A, kUP3=\E[1;3A, kUP4=\E[1;4A,
    kUP5=\E[1;5A, kUP6=\E[1;6A, kUP7=\E[1;7A, kxIN=\E[I,
    kxOUT=\E[O, rmxx=\E[29m, rv=\E\\[[0-9]+;[0-9]+;[0-9]+c,
    setrgbb=\E[48\:2\:%p1%d\:%p2%d\:%p3%dm,
    setrgbf=\E[38\:2\:%p1%d\:%p2%d\:%p3%dm, smxx=\E[9m,
    xm=\E[<%i%p3%d;%p1%d;%p2%d;%?%p4%tM%em%;,
    xr=\EP>\\|[ -~]+a\E\\,
EOF

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

# Claude wrapper
RUN cp /opt/yolobox/wrapper-template /opt/yolobox/bin/claude \
    && echo 'exec "$REAL_BIN" --dangerously-skip-permissions "$@"' >> /opt/yolobox/bin/claude \
    && chmod +x /opt/yolobox/bin/claude

# Codex wrapper
RUN cp /opt/yolobox/wrapper-template /opt/yolobox/bin/codex \
    && echo 'exec "$REAL_BIN" --dangerously-bypass-approvals-and-sandbox "$@"' >> /opt/yolobox/bin/codex \
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


# Add wrapper dir and ~/.local/bin to PATH (wrappers take priority)
ENV PATH="/opt/yolobox/bin:/home/yolo/.local/bin:$PATH"

USER yolo

# Welcome message
RUN echo 'echo ""' >> ~/.bashrc \
    && echo 'echo -e "\\033[1;35m  Welcome to yolobox!\\033[0m"' >> ~/.bashrc \
    && echo 'echo -e "\\033[33m  Your home directory is safe. Go wild.\\033[0m"' >> ~/.bashrc \
    && echo 'echo ""' >> ~/.bashrc

# Create entrypoint script
USER root
RUN mkdir -p /host-claude /host-git && \
    printf '%s\n' \
    '#!/bin/bash' \
    '' \
    '# Copy Claude config from host staging area if present' \
    'if [ -d /host-claude/.claude ] || [ -f /host-claude/.claude.json ]; then' \
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
    'fi' \
    '' \
    '# Copy git config from host staging area if present' \
    'if [ -f /host-git/.gitconfig ]; then' \
    '    echo -e "\033[33m→ Copying host git config to container\033[0m" >&2' \
    '    sudo rm -f /home/yolo/.gitconfig' \
    '    sudo cp -a /host-git/.gitconfig /home/yolo/.gitconfig' \
    '    sudo chown yolo:yolo /home/yolo/.gitconfig' \
    'fi' \
    '' \
    '# Auto-trust /workspace for Claude Code (this is yolobox after all)' \
    'CLAUDE_JSON="/home/yolo/.claude.json"' \
    'if [ ! -f "$CLAUDE_JSON" ]; then' \
    '    echo '"'"'{"projects":{}}'"'"' > "$CLAUDE_JSON"' \
    'fi' \
    '# Add /workspace as trusted project' \
    'if command -v jq &> /dev/null; then' \
    '    TMP=$(mktemp)' \
    '    jq '"'"'.projects["/workspace"] = (.projects["/workspace"] // {}) + {"hasTrustDialogAccepted": true}'"'"' "$CLAUDE_JSON" > "$TMP" && mv "$TMP" "$CLAUDE_JSON"' \
    '    chown yolo:yolo "$CLAUDE_JSON"' \
    'fi' \
    '' \
    '# Create minimal .zshrc if missing (suppresses zsh-newuser-install wizard)' \
    'if [ ! -f /home/yolo/.zshrc ]; then' \
    '    echo "# yolobox zshrc - sources system config" > /home/yolo/.zshrc' \
    '    echo "source /etc/zsh/zshrc" >> /home/yolo/.zshrc' \
    '    chown yolo:yolo /home/yolo/.zshrc' \
    'fi' \
    '' \
    'exec "$@"' \
    > /usr/local/bin/yolobox-entrypoint.sh && \
    chmod +x /usr/local/bin/yolobox-entrypoint.sh
USER yolo

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/yolobox-entrypoint.sh"]
CMD ["bash"]
