```
██╗   ██╗ ██████╗ ██╗      ██████╗ ██████╗  ██████╗ ██╗  ██╗
╚██╗ ██╔╝██╔═══██╗██║     ██╔═══██╗██╔══██╗██╔═══██╗╚██╗██╔╝
 ╚████╔╝ ██║   ██║██║     ██║   ██║██████╔╝██║   ██║ ╚███╔╝
  ╚██╔╝  ██║   ██║██║     ██║   ██║██╔══██╗██║   ██║ ██╔██╗
   ██║   ╚██████╔╝███████╗╚██████╔╝██████╔╝╚██████╔╝██╔╝ ██╗
   ╚═╝    ╚═════╝ ╚══════╝ ╚═════╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═╝
```

**Let your AI go full send. Your home directory stays home.**

Docs: [yolobox.dev](https://yolobox.dev)

Changelog: [CHANGELOG.md](CHANGELOG.md)

Run [Claude Code](https://claude.ai/code), [Codex](https://openai.com/codex/), [Kimi Code](https://github.com/MoonshotAI/kimi-code), Gemini, Antigravity, OpenCode, Copilot, Pi, or any AI coding agent in "yolo mode" without nuking your home directory.

## The Problem

AI coding agents are incredibly powerful when you let them run commands without asking permission. But one misinterpreted prompt and `rm -rf ~` later, you're restoring from backup (yea right, as if you have backups lol).

## The Solution

`yolobox` runs your AI agent inside a container where:

- your project directory is mounted at its real path, such as `/Users/you/project`
- the agent has full permissions and sudo inside the container
- your home directory is not mounted unless you explicitly opt in
- persistent volumes keep tools, configs, and sessions across runs
- Claude, Codex, and Kimi Code get built-in yolobox guidance so they can understand the sandbox they are running in

The AI can go absolutely wild inside the sandbox. Your actual home directory? Untouchable.

## Quick Start

```bash
# Install via Homebrew
brew install finbarr/tap/yolobox

# Or install via script
curl -fsSL https://raw.githubusercontent.com/finbarr/yolobox/master/install.sh | bash
```

Then from any project:

```bash
cd /path/to/your/project
yolobox claude    # Let it rip
```

Other AI shortcuts work the same way:

```bash
yolobox codex
yolobox gemini
yolobox kimi
yolobox agy
yolobox antigravity
yolobox opencode
yolobox copilot
yolobox pi
```

Set `default_harness = "codex"` to make bare `yolobox` launch Codex. Use `yolobox shell` when you want a manual shell, and `yolobox run <cmd...>` when you want one command in the sandbox.

Full install and runtime details live in [Installation & Setup](https://yolobox.dev/getting-started). Command examples live in [Commands](https://yolobox.dev/commands).

## What's in the Box?

The base image comes with AI CLIs, Node.js, Python, Go, Bun, build tools, Git, GitHub CLI, ripgrep, fd, fzf, jq, vim, RTK, and the usual practical bits.

Need something else? The agent has sudo.

Inside yolobox, supported AI CLIs are wrapped to skip permission prompts. No confirmations, no guardrails. Just pure unfiltered AI, the way nature intended.

For the full tool list, YOLO-mode wrapper table, RTK notes, npm package freshness policy, and bundled CLI upgrade behavior, see [What's in the Box](https://yolobox.dev/whats-in-the-box).

## Project Customization

If one project needs extra tools or environment variables, add a small project config instead of forking the whole base image:

```toml
# .yolobox.toml
env = ["CODEX_HOME=/home/yolo/.codex-account"]

[customize]
packages = ["default-jdk", "maven"]
```

Then run normally:

```bash
yolobox run mvn --version
```

Project-level customization can also layer a Dockerfile fragment on top of the base image. The first run builds a derived image; later runs reuse it until the base image or customization inputs change.

Use container paths for env values because they are passed directly to the process inside yolobox. See [Configuration](https://yolobox.dev/configuration) for project env settings, and [Project-Level Customization](https://yolobox.dev/customizing) for package installs, Dockerfile fragments, rebuild behavior, upgrade behavior, and fully custom images.

## Common Workflows

```bash
yolobox setup                         # Configure global defaults
yolobox config                        # Show resolved config for this project
yolobox claude --docker --gh-token    # Give the agent Docker and GitHub access
yolobox codex --rtk                   # Enable RTK command-output compression
yolobox run --no-network make test    # Run one command with no network
yolobox fork --name bruno codex       # Give an agent its own project copy
yolobox upgrade                       # Update binary and pull the latest image
yolobox update-agents                 # Update AI CLIs in the persistent box
```

The detailed references are intentionally in the docs site:

- [Commands](https://yolobox.dev/commands): shortcuts, maintenance commands, `fork`, and examples
- [Configuration](https://yolobox.dev/configuration): global config, project config, copied instructions, env passthrough, and context manifests
- [Flags](https://yolobox.dev/flags): every flag, compatibility note, and runtime passthrough detail
- [Recipes](https://yolobox.dev/recipes): parallel agents and webapp routing

## Philosophy: It's the AI's Box, Not Yours

yolobox is designed for AI agents, not humans. You launch the AI and let it work.

The agent has sudo inside the container. If it needs a compiler, database, package, or framework, it can install one. Named volumes preserve that setup across sessions, so you do not have to turn the README into a hundred-line package matrix. Point it at your project and let it cook.

## Security Model

yolobox is protection from accidents, not a magic anti-container-escape theorem.

It helps protect your home directory, SSH keys, dotfiles, unrelated projects, and most host filesystem state from careless destructive commands. It does not protect the project directory you mounted, secrets you explicitly forward, host actions you explicitly bridge, or the host kernel from runtime escape vulnerabilities.

For a tighter box, combine flags such as:

```bash
yolobox claude --no-network --no-env-passthrough --readonly-project --exclude ".env*" --exclude "secrets/**"
```

If you are worried about hostile code rather than careless code, use stronger isolation such as rootless Podman or a VM. The full threat model and hardening options are in [Security Model](https://yolobox.dev/security).

## Development

```bash
make build
make test
make lint
make image
```

Contributor workflow, docs-site commands, versioning, and release rules live in [Contributing](https://yolobox.dev/contributing).

## License

MIT
