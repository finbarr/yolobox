FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8

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
    jq \
    ripgrep \
    fd-find \
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

# Create yolo user with passwordless sudo
RUN useradd -m -s /bin/bash yolo \
    && echo "yolo ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/yolo \
    && chmod 0440 /etc/sudoers.d/yolo

# Set up directories
RUN mkdir -p /workspace /output /secrets \
    && chown yolo:yolo /workspace /output

# Install global npm packages as yolo user
USER yolo
WORKDIR /home/yolo

# Install Claude Code and other useful global packages
RUN npm install -g \
    @anthropic-ai/claude-code \
    typescript \
    ts-node \
    yarn \
    pnpm

# Set up a nice prompt
RUN echo 'PS1="\\[\\033[1;35m\\]yolobox\\[\\033[0m\\]:\\[\\033[1;34m\\]\\w\\[\\033[0m\\]\\$ "' >> ~/.bashrc \
    && echo 'alias ll="ls -la"' >> ~/.bashrc \
    && echo 'alias la="ls -A"' >> ~/.bashrc \
    && echo 'alias l="ls -CF"' >> ~/.bashrc

# Welcome message
RUN echo 'echo ""' >> ~/.bashrc \
    && echo 'echo -e "\\033[1;35m  Welcome to yolobox!\\033[0m"' >> ~/.bashrc \
    && echo 'echo -e "\\033[33m  Your home directory is safe. Go wild.\\033[0m"' >> ~/.bashrc \
    && echo 'echo ""' >> ~/.bashrc

WORKDIR /workspace

# Default to bash
CMD ["bash"]
