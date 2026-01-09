```
██╗   ██╗ ██████╗ ██╗      ██████╗ ██████╗  ██████╗ ██╗  ██╗
╚██╗ ██╔╝██╔═══██╗██║     ██╔═══██╗██╔══██╗██╔═══██╗╚██╗██╔╝
 ╚████╔╝ ██║   ██║██║     ██║   ██║██████╔╝██║   ██║ ╚███╔╝
  ╚██╔╝  ██║   ██║██║     ██║   ██║██╔══██╗██║   ██║ ██╔██╗
   ██║   ╚██████╔╝███████╗╚██████╔╝██████╔╝╚██████╔╝██╔╝ ██╗
   ╚═╝    ╚═════╝ ╚══════╝ ╚═════╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═╝
```

**Let your AI go full send. Your home directory stays home.**

Run [Claude Code](https://claude.ai/code), [Codex](https://openai.com/index/codex/), or any AI coding agent in "yolo mode" without nuking your home directory.

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
curl -fsSL https://raw.githubusercontent.com/finbarr/yolobox/main/install.sh | bash

# Or clone and build
git clone https://github.com/finbarr/yolobox.git
cd yolobox
make install
```

Then from any project:

```bash
cd /path/to/your/project
yolobox
```

You're now in a sandboxed shell. Run `claude` and let it rip.

## What's in the Box?

The base image comes batteries-included:
- **AI CLIs**: Claude Code, Gemini CLI, OpenAI Codex (all aliased to run in full-auto mode!)
- **Node.js 22** + npm/yarn/pnpm
- **Python 3** + pip + venv
- **Build tools**: make, cmake, gcc
- **Git** + **GitHub CLI**
- **Common utilities**: ripgrep, fd, fzf, jq, vim

Need something else? You have sudo.

## AI CLIs Run in YOLO Mode

Inside yolobox, the AI CLIs are aliased to skip all permission prompts:

| Command | Expands to |
|---------|------------|
| `claude` | `claude --dangerously-skip-permissions` |
| `codex` | `codex --dangerously-bypass-approvals-and-sandbox` |
| `gemini` | `gemini --yolo` |

No confirmations, no guardrails—just pure unfiltered AI, the way nature intended.

## Commands

```bash
yolobox                     # Drop into interactive shell
yolobox run <cmd...>        # Run a single command
yolobox run claude          # Run Claude Code in sandbox
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
| `--ssh-agent` | Forward SSH agent socket |
| `--no-network` | Disable network access |
| `--readonly-project` | Mount project read-only (outputs go to `/output`) |
| `--claude-config` | Copy host `~/.claude` config into container |

## Auto-Forwarded Environment Variables

These are automatically passed into the container if set:
- `ANTHROPIC_API_KEY`
- `OPENAI_API_KEY`
- `GITHUB_TOKEN` / `GH_TOKEN`
- `OPENROUTER_API_KEY`
- `GEMINI_API_KEY`

## Configuration

Create `~/.config/yolobox/config.toml` for global defaults:

```toml
runtime = "docker"
image = "ghcr.io/finbarr/yolobox:latest"
ssh_agent = true
```

Or `.yolobox.toml` in your project for project-specific settings:

```toml
mounts = ["../shared-libs:/libs:ro"]
env = ["DEBUG=1"]
no_network = true
```

Priority: CLI flags > project config > global config > defaults.

> **Note:** Setting `claude_config = true` in your config will copy your host's Claude config on **every** container start, overwriting any changes made inside the container. Use the CLI flag `--claude-config` for one-time syncs.

## Runtime Support

- **macOS**: Docker Desktop, OrbStack, or Colima
- **Linux**: Docker or Podman

## Threat Model

**What yolobox protects:**
- Your home directory from accidental deletion
- Your SSH keys, credentials, and dotfiles
- Other projects on your machine

**What yolobox does NOT protect:**
- Your project directory (it's mounted read-write by default)
- Network access (use `--no-network` for paranoid mode)
- The container itself (the AI has root via sudo)

For extra paranoia, use `--readonly-project` to mount your project read-only. Outputs go to `/output`.

## Building the Base Image

```bash
make image
```

This builds `yolobox/base:latest` locally.

## Why "yolobox"?

Because you want to tell your AI agent "just do it" without consequences. YOLO, but in a box.

## License

MIT
