```
██╗   ██╗ ██████╗ ██╗      ██████╗ ██████╗  ██████╗ ██╗  ██╗
╚██╗ ██╔╝██╔═══██╗██║     ██╔═══██╗██╔══██╗██╔═══██╗╚██╗██╔╝
 ╚████╔╝ ██║   ██║██║     ██║   ██║██████╔╝██║   ██║ ╚███╔╝
  ╚██╔╝  ██║   ██║██║     ██║   ██║██╔══██╗██║   ██║ ██╔██╗
   ██║   ╚██████╔╝███████╗╚██████╔╝██████╔╝╚██████╔╝██╔╝ ██╗
   ╚═╝    ╚═════╝ ╚══════╝ ╚═════╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═╝
```

**Let your AI go full send. Your home directory stays home.**

Run [Claude Code](https://claude.ai/code), [Codex](https://openai.com/codex/), or any AI coding agent in "yolo mode" without nuking your home directory.

## The Problem

AI coding agents are incredibly powerful when you let them run commands without asking permission. But one misinterpreted prompt and `rm -rf ~` later, you're restoring from backup (yea right, as if you have backups lol).

## The Solution

`yolobox` runs your AI agent inside a container where:
- ✅ Your **project directory** is mounted at `/workspace`
- ✅ The agent has **full permissions** and **sudo** inside the container
- ✅ Your **home directory is NOT mounted** (unless you explicitly opt in)
- ✅ Persistent volumes keep tools and configs across sessions

The AI can go absolutely wild inside the sandbox. Your actual home directory? Untouchable.

## Quick Start

```bash
# Install (requires Go)
curl -fsSL https://raw.githubusercontent.com/finbarr/yolobox/master/install.sh | bash

# Or clone and build
git clone https://github.com/finbarr/yolobox.git
cd yolobox
make install
```

Then from any project:

```bash
cd /path/to/your/project
yolobox claude    # Let it rip
```

Or use any other AI tool: `yolobox codex`, `yolobox gemini`, `yolobox copilot`.

## Philosophy: It's the AI's Box, Not Yours

yolobox is designed for AI agents, not humans. The typical workflow is:

```bash
yolobox claude    # Launch Claude Code in the sandbox
yolobox codex     # Or Codex, or any other AI tool
```

That's it. You launch the AI and let it work. You're not meant to manually enter the box and set things up—the AI does that itself.

**Why?** The AI agent has full sudo access inside the container. If it needs a compiler, database, or framework—it just installs it. Named volumes persist these installations across sessions, so setup happens once. You point it at your project and let it cook.

## What's in the Box?

The base image comes batteries-included:
- **AI CLIs**: Claude Code, Gemini CLI, OpenAI Codex, OpenCode, Copilot (all pre-configured for full-auto mode!)
- **Runtimes**: Node.js 22, Python 3, Go, Bun
- **Build tools**: make, cmake, gcc
- **Git** + **GitHub CLI**
- **Common utilities**: ripgrep, fd, fzf, jq, vim

Need something else? The AI has sudo.

## AI CLIs Run in YOLO Mode

Inside yolobox, the AI CLIs are aliased to skip all permission prompts:

| Command | Expands to |
|---------|------------|
| `claude` | `claude --dangerously-skip-permissions` |
| `codex` | `codex --dangerously-bypass-approvals-and-sandbox` |
| `gemini` | `gemini --yolo` |
| `opencode` | `opencode` (no yolo flag available yet) |
| `copilot` | `copilot --yolo` |

No confirmations, no guardrails—just pure unfiltered AI, the way nature intended.

## Commands

```bash
# AI tool shortcuts (recommended)
yolobox claude              # Run Claude Code
yolobox codex               # Run OpenAI Codex
yolobox gemini              # Run Gemini CLI
yolobox opencode            # Run OpenCode
yolobox copilot             # Run GitHub Copilot

# General commands
yolobox                     # Drop into interactive shell (for manual use)
yolobox run <cmd...>        # Run any command in sandbox
yolobox setup               # Configure yolobox settings
yolobox upgrade             # Update binary and pull latest image
yolobox config              # Show resolved configuration
yolobox reset --force       # Delete volumes (fresh start)
yolobox version             # Show version
yolobox help                # Show help
```

## Flags

| Flag | Description |
|------|-------------|
| `--runtime <name>` | Use `docker` or `podman` |
| `--image <name>` | Custom base image |
| `--mount <src:dst>` | Extra mount (repeatable) |
| `--env <KEY=val>` | Set environment variable (repeatable) |
| `--setup` | Run interactive setup before starting |
| `--ssh-agent` | Forward SSH agent socket |
| `--no-network` | Disable network access |
| `--no-yolo` | Disable auto-confirmations (mindful mode) |
| `--readonly-project` | Mount project read-only (outputs go to `/output`) |
| `--claude-config` | Copy host `~/.claude` config into container |
| `--git-config` | Copy host `~/.gitconfig` into container |

## Auto-Forwarded Environment Variables

These are automatically passed into the container if set:
- `ANTHROPIC_API_KEY`
- `OPENAI_API_KEY`
- `COPILOT_GITHUB_TOKEN` / `GH_TOKEN` / `GITHUB_TOKEN`
- `OPENROUTER_API_KEY`
- `GEMINI_API_KEY`

## Configuration

Run `yolobox setup` to configure your preferences with an interactive wizard.

Settings are saved to `~/.config/yolobox/config.toml`:

```toml
git_config = true
ssh_agent = true
no_network = true
no_yolo = true
```

You can also create `.yolobox.toml` in your project for project-specific settings:

```toml
mounts = ["../shared-libs:/libs:ro"]
env = ["DEBUG=1"]
no_network = true
```

Priority: CLI flags > project config > global config > defaults.

> **Note:** Setting `claude_config = true` in your config will copy your host Claude config on **every** container start, overwriting any changes made inside the container (including auth and history). Prefer using `--claude-config` for one-time syncs.

## Runtime Support

- **macOS**: Docker Desktop, OrbStack, or Colima
- **Linux**: Docker or Podman

> **Memory:** Claude Code needs **4GB+ RAM** allocated to Docker. Colima defaults to 2GB which will cause OOM kills. Increase with: `colima stop && colima start --memory 8`

## Security Model

### How It Works

yolobox uses **container isolation** (Docker or Podman) as its security boundary. When you run `yolobox`, it:

1. Starts a container with your project mounted at `/workspace`
2. Runs as user `yolo` with sudo access *inside* the container
3. Does NOT mount your home directory (unless explicitly requested)
4. Uses Linux namespaces to isolate the container's filesystem, process tree, and network

The AI agent has full root access *inside the container*, but the container's view of the filesystem is restricted to what yolobox explicitly mounts.

### Trust Boundary

**The trust boundary is the container runtime** (Docker/Podman). This means:

- ✅ Protection against accidental `rm -rf ~` or credential theft
- ✅ Protection against most filesystem-based attacks
- ⚠️ **NOT protection against container escapes** — a sufficiently advanced exploit targeting kernel vulnerabilities could break out
- ⚠️ **NOT protection against a malicious AI** deliberately trying to escape — this is defense against accidents, not adversarial attacks

If you're worried about an AI actively trying to escape containment, you need VM-level isolation (see "Hardening Options" below).

### Threat Model

**What yolobox protects:**
- Your home directory from accidental deletion
- Your SSH keys, credentials, and dotfiles
- Other projects on your machine
- Host system files and configurations

**What yolobox does NOT protect:**
- Your project directory (it's mounted read-write by default)
- Network access (use `--no-network` to disable)
- The container itself (the AI has root via sudo)
- Against kernel exploits or container escape vulnerabilities

### Hardening Options

**Level 1: Basic (default)**
```bash
yolobox  # Standard container isolation
```

**Level 2: Reduced attack surface**
```bash
yolobox run --no-network --readonly-project claude
```

**Level 3: Rootless Podman** (recommended for security-conscious users)
```bash
# Install podman and run rootless
yolobox --runtime podman
```

Rootless Podman runs the container without root privileges on the host, using user namespaces. This significantly reduces the impact of container escapes since the container's "root" maps to your unprivileged user on the host.

**Level 4: VM isolation** (maximum security)

For true isolation with no shared kernel, consider running yolobox inside a VM:
- **macOS**: Use a Linux VM via UTM, Parallels, or Lima
- **Linux**: Use a Podman machine or dedicated VM

This adds significant overhead but eliminates kernel-level attack surface.

### Network Isolation with Podman

For users who want to prevent container access to the local network while preserving internet access:

```bash
# Rootless podman uses slirp4netns by default, which provides
# network isolation from the host network
podman run --network=slirp4netns:allow_host_loopback=false ...
```

yolobox doesn't currently expose this as a flag, but you can achieve it by running rootless Podman (the default network mode for rootless is slirp4netns).

## Building the Base Image

```bash
make image
```

This builds `ghcr.io/finbarr/yolobox:latest` locally, overriding the remote image.

## Customizing the Image

Want to pre-install additional packages or tools? Create your own image:

**1. Clone and modify:**
```bash
git clone https://github.com/finbarr/yolobox.git
cd yolobox
# Edit the Dockerfile to add your packages
```

**2. Build with a custom name:**
```bash
make image IMAGE=my-yolobox:latest
```

**3. Configure yolobox to use it:**
```bash
mkdir -p ~/.config/yolobox
echo 'image = "my-yolobox:latest"' > ~/.config/yolobox/config.toml
```

Using a custom image name means `yolobox upgrade` won't overwrite your customization. When you update your Dockerfile, just rebuild with the same command.

## Why "yolobox"?

Because you want to tell your AI agent "just do it" without consequences. YOLO, but in a box.

## Development

### Building

```bash
make build          # Build binary
make test           # Run tests
make lint           # Run linters
make image          # Build Docker image
make install        # Install to ~/.local/bin
```

### Versioning

Version is derived automatically from git tags via `git describe`:
- Tagged commit: `v0.1.1`
- After tag: `v0.1.1-3-gead833b` (3 commits after tag)
- Uncommitted changes: adds `-dirty`

**No files to edit for releases.** The Makefile handles it.

### Releasing

To release a new version:

```bash
git tag v0.1.2
git push origin master --tags
```

That's it. GitHub Actions will automatically:
1. Build binaries for linux/darwin × amd64/arm64
2. Create a GitHub release with binaries and checksums
3. Build and push Docker image to `ghcr.io/finbarr/yolobox`

**Version policy:**
- Patch bump (`0.1.x`): Bug fixes, security fixes
- Minor bump (`0.x.0`): New features
- Major bump (`x.0.0`): Breaking changes

## License

MIT
