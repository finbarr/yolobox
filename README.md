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

Run [Claude Code](https://claude.ai/code), [Codex](https://openai.com/codex/), or any AI coding agent in "yolo mode" without nuking your home directory.

## The Problem

AI coding agents are incredibly powerful when you let them run commands without asking permission. But one misinterpreted prompt and `rm -rf ~` later, you're restoring from backup (yea right, as if you have backups lol).

## The Solution

`yolobox` runs your AI agent inside a container where:
- ✅ Your **project directory** is mounted at its real path (e.g., `/Users/you/project`)
- ✅ The agent has **full permissions** and **sudo** inside the container
- ✅ Your **home directory is NOT mounted** (unless you explicitly opt in)
- ✅ Persistent volumes keep tools and configs across sessions
- ✅ **Session continuity** - AI sessions can be resumed across host/container boundary

The AI can go absolutely wild inside the sandbox. Your actual home directory? Untouchable.

## Quick Start

```bash
# Install via Homebrew (macOS/Linux)
brew install finbarr/tap/yolobox

# Or install via script (requires Go)
curl -fsSL https://raw.githubusercontent.com/finbarr/yolobox/master/install.sh | bash
```

Then from any project:

```bash
cd /path/to/your/project
yolobox claude    # Let it rip
```

Or use any other AI tool: `yolobox codex`, `yolobox gemini`, `yolobox copilot`.

Non-interactive invocations keep stdout and stderr separate, so shell redirection works as expected:

```bash
yolobox claude -- -p "Hello" 2>/dev/null
```

## What's in the Box?

The base image comes batteries-included:
- **AI CLIs**: Claude Code, Gemini CLI, OpenAI Codex, OpenCode, Copilot (all pre-configured for full-auto mode!)
- **Runtimes**: Node.js 22, Python 3, Go, Bun
- **Build tools**: make, cmake, gcc
- **Git** + **GitHub CLI**
- **Common utilities**: ripgrep, fd, fzf, jq, vim

Need something else? The AI has sudo.

### AI CLIs Run in YOLO Mode

Inside yolobox, the AI CLIs are aliased to skip all permission prompts:

| Command | Expands to |
|---------|------------|
| `claude` | `claude --dangerously-skip-permissions` |
| `codex` | `codex --ask-for-approval never --sandbox danger-full-access` |
| `gemini` | `gemini --yolo` |
| `opencode` | `opencode` (no yolo flag available yet) |
| `copilot` | `copilot --yolo` |

No confirmations, no guardrails—just pure unfiltered AI, the way nature intended.

For Codex, yolobox pins approval and sandbox mode explicitly so upstream trust defaults and Linux sandbox backend changes do not change the wrapper behavior.

## Project-Level Container Customization

Need extra tools for one project without bloating the base image? yolobox can build and cache a derived image from your project config.

Simple case:

```toml
# .yolobox.toml
[customize]
packages = ["default-jdk", "maven"]
```

Then run normally:

```bash
yolobox run mvn --version
```

For more advanced setups, add a Dockerfile fragment:

```toml
# .yolobox.toml
[customize]
dockerfile = ".yolobox.Dockerfile"
```

```dockerfile
# .yolobox.Dockerfile
USER root
RUN curl -fsSL https://get.sdkman.io | bash
USER yolo
```

You can combine both. `packages` install first, then the fragment runs on top.

For one-offs, use flags:

```bash
yolobox run --packages default-jdk,maven mvn --version
yolobox run --customize-file .yolobox.Dockerfile bash
yolobox run --packages default-jdk --rebuild-image java --version
```

The first run builds a derived image. Later runs reuse it until the base image or customization inputs change. When you use a Dockerfile fragment, yolobox asks Docker/Podman to build again so context changes are noticed, but cached layers are reused when nothing changed.

This also keeps `yolobox upgrade` relatively painless:

- `yolobox upgrade` still updates the binary and pulls the latest base image
- your project-level customization stays in `.yolobox.toml` / `.yolobox.Dockerfile`
- the next run rebuilds the derived image only if the new base image or your customization inputs changed

You still pay one rebuild after a base-image upgrade, but you do not need to manually rebase a forked Dockerfile just to keep using the feature.

> **Note:** Derived-image customization requires a runtime that can build images (`docker` or `podman`). Apple's `container` runtime can run yolobox, but it cannot build custom images.

## Runtime Support

- **macOS**: Docker Desktop, OrbStack, Colima, or [Apple container](https://github.com/apple/container) (macOS Tahoe+)
- **Linux**: Docker or Podman

yolobox auto-detects available runtimes. To use a specific runtime:
```bash
yolobox claude --runtime container   # Apple container
yolobox claude --runtime docker      # Docker
yolobox claude --runtime podman      # Podman
```

> **Memory:** Claude Code needs **4GB+ RAM** allocated to Docker. Colima defaults to 2GB which will cause OOM kills. Increase with: `colima stop && colima start --memory 8`

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
yolobox fork --name <env> <cmd...> # Run in a named copied folder with a Compose namespace
yolobox setup               # Configure yolobox settings
yolobox upgrade             # Update binary and pull latest image
yolobox config              # Show resolved configuration
yolobox reset --force       # Delete volumes (fresh start)
yolobox version             # Show version
yolobox help                # Show help
```

## Fork Mode

Use fork mode when you want several agents working beside you without competing for the same folder. Each fork gets its own complete copy of the current project folder, like another developer working on their own machine. If the folder contains a Git checkout, your Git remote becomes the sync point, just like it would for a team.

```bash
yolobox fork --name bruno codex
yolobox fork --name diane claude
yolobox fork resume bruno codex
yolobox fork discard bruno --force
```

Fork mode copies the entire current folder to `../.yolobox-forks/<folder>/<env>`. That includes `.git` if present, ignored files, untracked files, env files, dependencies, local caches, and anything else in the folder. Inside the container, the copied folder is mounted at the original source path, while host filesystem writes land in the copy. Yolobox also sets `COMPOSE_PROJECT_NAME` to a unique value for the source folder and fork.

On exit, yolobox runs best-effort `docker compose -p "$COMPOSE_PROJECT_NAME" down --volumes --remove-orphans` when it finds a Compose file. The copied folder is preserved until you explicitly discard it.

Compose namespacing covers default Compose-created containers, networks, and named volumes. Fixed host ports, explicit `container_name`, external networks or volumes, and absolute bind mounts can still collide.

See the [recipes](docs/recipes.md) for common fork workflows, including parallel agents on one project and webapp routing.

## Configuration

Run `yolobox setup` to configure your preferences with an interactive wizard.

Settings are saved to `~/.config/yolobox/config.toml`:

```toml
git_config = true
opencode_config = true
gh_token = true
ssh_agent = true
docker = true
clipboard = true
network = "my_compose_network"
# no_network = true # incompatible with network, pod, docker, and clipboard
no_yolo = true
cpus = "4"
memory = "8g"
cap_add = ["SYS_PTRACE"]
devices = ["/dev/kvm:/dev/kvm"]
runtime_args = ["--security-opt", "seccomp=unconfined"]
```

You can also create `.yolobox.toml` in your project for project-specific settings:

```toml
mounts = ["../shared-libs:/libs:ro"]
env = ["DEBUG=1"]
readonly_project = true
exclude = [".env*", "secrets/**"]
copy_as = [".env.sandbox:.env"]
no_network = true
shm_size = "2g"
```

Priority: CLI flags > project config > global config > defaults.

Each `runtime_args` entry is a single CLI argument. For flags that take a value, add them as separate entries so `--security-opt seccomp=unconfined` becomes `["--security-opt", "seccomp=unconfined"]`.

> **Note:** Setting `claude_config = true`, `codex_config = true`, `gemini_config = true`, or `opencode_config = true` in your config will copy your host config on **every** container start. Claude, Gemini, and OpenCode config sync replaces the matching in-container config directory, overwriting changes made inside the container. Codex config sync merges host files into `~/.codex` and preserves a valid in-container `auth.json` when the host copy has no usable auth file. Prefer using `--claude-config`, `--codex-config`, `--gemini-config`, or `--opencode-config` for one-time syncs.

yolobox removes a zero-byte `/home/yolo/.codex/auth.json` during startup. Recent Codex versions fail with `EOF while parsing a value` when that stale file exists; removing it lets Codex recreate auth normally or show the sign-in flow.

If Codex auth fails with `No space left on device`, the Docker or Podman storage backing `/home/yolo` or `/tmp` is full. Check `docker system df` or the equivalent for your runtime, then reclaim runtime storage or increase the VM disk size. yolobox warns at container startup when those paths are nearly full, but it does not automatically prune unrelated images, volumes, or build cache.

### Project File Filtering

Use `--exclude` to hide matching project paths from the container by staging an empty readonly project view:

```bash
yolobox claude --readonly-project --exclude ".env*" --exclude "secrets/**"
```

Use `--copy-as` to copy one file into another project path inside that staged readonly view:

```bash
yolobox claude --readonly-project --exclude ".env*" --copy-as ".env.sandbox:.env"
```

- `exclude` patterns are relative to the project root and support `**`
- `copy_as` destinations must stay inside the project and must already exist as files
- `copy_as` wins if it targets the same path as an `exclude`
- `--exclude` and `--copy-as` currently require `--readonly-project`
- `--exclude` and `--copy-as` are currently supported on Docker and Podman, not Apple's `container` runtime

### Skipping the Automatic Project Mount

Use `--no-project` when yolobox is running in an environment where its current working directory is not also visible to the Docker or Podman daemon, such as some Docker-in-Docker and remote-daemon setups. This skips the default project mount, default workdir, and `$YOLOBOX_PROJECT_PATH`; provide your own mount and workdir explicitly:

```bash
yolobox run --no-project \
  --mount /host/path/to/project:/workspace \
  --runtime-arg=--workdir=/workspace \
  bash
```

`--no-project` is incompatible with `--readonly-project`, `--exclude`, and `--copy-as` because those options operate on the automatic project mount.

### Copying Global Agent Instructions

The `--copy-agent-instructions` flag copies your **global/user-level** agent instruction files and skills into the container. This is useful when you have custom rules, preferences, or reusable skills defined globally that you want available inside yolobox.

Files copied (if they exist on your host):

| Tool | Source | Destination |
|------|--------|-------------|
| Claude | `~/.claude/CLAUDE.md` | `/home/yolo/.claude/CLAUDE.md` |
| Claude skills | `~/.claude/skills/` | `/home/yolo/.claude/skills/` |
| Gemini | `~/.gemini/GEMINI.md` | `/home/yolo/.gemini/GEMINI.md` |
| Codex | `~/.codex/AGENTS.md` | `/home/yolo/.codex/AGENTS.md` |
| Codex skills | `~/.codex/skills/` | `/home/yolo/.codex/skills/` |
| Copilot | `~/.copilot/agents/` | `/home/yolo/.copilot/agents/` |

**Note:** This copies instructions and skills, not full configs (credentials, settings, history). For full tool configs, use `--claude-config`, `--codex-config`, `--gemini-config`, or `--opencode-config`.

You can also set `copy_agent_instructions = true` in your config file for persistent use.

### Auto-Forwarded Environment Variables

These are automatically passed into the container if set:
- `ANTHROPIC_API_KEY`
- `CLAUDE_CODE_OAUTH_TOKEN`
- `OPENAI_API_KEY`
- `COPILOT_GITHUB_TOKEN` / `GH_TOKEN` / `GITHUB_TOKEN`
- `OPENROUTER_API_KEY`
- `GEMINI_API_KEY`

### Runtime Context Manifest

Every yolobox session also provides a machine-readable context manifest at `/run/yolobox/context.json` and exports its path as `YOLOBOX_CONTEXT_FILE`.

This gives agents and scripts a stable way to confirm they are inside yolobox and inspect the resolved launch context without scraping CLI output. The manifest includes:

- yolobox version, schema version, and `inside_yolobox` confirmation
- configured and selected runtime
- project, home, and output paths inside the container
- launch command, working directory, and interactive mode
- developer/environment name, copied folder path, source folder, and Compose project name when `yolobox fork` is active
- resolved config fields such as network, mounts, readonly mode, Docker access, and customization settings
- forwarded environment variable keys, without exposing their values

The repo's [`skills/`](skills) directory is also the canonical Agent Skills source for yolobox:

- [`skills/yolobox`](skills/yolobox) is the inside-the-box skill. It orients the agent to the trusted yolobox sandbox it is running in, then reads the manifest and summarizes the current constraints and freedoms. Its `Readonly project mode` line reports the launch mode; its `Project writable now` line is a live filesystem check. yolobox currently installs this built-in for Claude and Codex sessions inside the container.
- [`skills/yolobox-orchestrator`](skills/yolobox-orchestrator) is the host-side skill for agents that need to launch or control yolobox sessions from outside the container.

For Claude and Codex, yolobox also injects a managed instruction block into `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md` so the agent knows to use the `yolobox` skill when sandbox assumptions matter.

Both skills follow the standard Agent Skills layout so they can be validated and published from this repo instead of being maintained as image-only one-offs.

> **Note:** On macOS, `gh` CLI stores tokens in Keychain, not environment variables. Use `--gh-token` (or `gh_token = true` in config) to extract and forward your GitHub token. When a token is present, yolobox also configures HTTPS Git auth for `github.com` remotes.

## Flags

> **Note:** Flags go **after** the subcommand: `yolobox run --flag cmd` or `yolobox claude --flag`, not `yolobox --flag run cmd`.

| Flag | Description | Incompatible with |
|------|-------------|-------------------|
| `--runtime <name>` | Use `docker`, `podman`, or `container` (Apple) | |
| `--image <name>` | Custom base image | |
| `--mount <src:dst>` | Extra mount (repeatable) | |
| `--exclude <glob>` | Hide matching project paths from the container (repeatable) | Apple `container`, `--no-project`, without `--readonly-project` |
| `--copy-as <src:dst>` | Mount a file at another project path inside the container (repeatable) | Apple `container`, `--no-project`, without `--readonly-project` |
| `--env <KEY=val>` | Set environment variable (repeatable) | |
| `--setup` | Run interactive setup before starting | |
| `--ssh-agent` | Forward SSH agent socket | |
| `--no-network` | Disable network access | `--network`, `--pod`, `--docker`, `--clipboard` |
| `--network <name>` | Join specific network (e.g., docker compose) | `--no-network`, `--pod` |
| `--pod <name>` | Join existing Podman pod (shares its network) | `--no-network`, `--network`, `--docker` |
| `--no-yolo` | Disable auto-confirmations (mindful mode) | |
| `--scratch` | Start with a fresh home/cache (nothing persists) | |
| `--readonly-project` | Mount project read-only (outputs go to `/output`) | `--no-project` |
| `--no-project` | Skip automatic project mount (caller provides `--mount` and `--runtime-arg=--workdir`) | `--readonly-project`, `--exclude`, `--copy-as` |
| `--claude-config` | Copy host `~/.claude` config into container | |
| `--codex-config` | Copy host `~/.codex` config into container | |
| `--gemini-config` | Copy host `~/.gemini` config into container | |
| `--opencode-config` | Copy host `~/.config/opencode` config into container | |
| `--git-config` | Copy host `~/.gitconfig` into container | |
| `--gh-token` | Forward GitHub token for `gh` and HTTPS Git auth (extracts from keychain via `gh auth token`) | |
| `--copy-agent-instructions` | Copy global agent instruction files and skills (see configuration below) | |
| `--docker` | Mount Docker socket and join shared network (see notes below) | `--no-network`, `--pod` |
| `--clipboard` | Bridge text clipboard copy/paste between the container and host | `--no-network` |
| `--cpus <num>` | Limit CPUs available to the container (accepts fractions like `3.5`) | |
| `--memory <limit>` | Hard memory limit (e.g., `8g`, `1024m`) | |
| `--shm-size <size>` | Size of `/dev/shm` tmpfs (useful for browsers/playwright) | |
| `--gpus <spec>` | Pass GPUs (Docker/Podman notation, e.g., `all`, `device=0`) | |
| `--device <src:dest>` | Add host devices in the container (repeatable) | |
| `--cap-add <cap>` | Add Linux capabilities (repeatable) | |
| `--cap-drop <cap>` | Drop Linux capabilities (repeatable) | |
| `--runtime-arg <flag>` | Pass raw runtime flags directly to Docker/Podman (repeatable) | |
| `--packages <list>` | Comma-separated apt packages for a derived custom image | Apple `container` |
| `--customize-file <path>` | Dockerfile fragment for a derived custom image | Apple `container` |
| `--rebuild-image` | Force rebuild of the derived custom image | Apple `container` |

> **Resource & security controls:** The table lists the common knobs baked into yolobox. Anything else (e.g., `--ulimit nofile=4096:8192`, `--security-opt seccomp=unconfined`) can be forwarded verbatim with `--runtime-arg <flag>` as many times as needed. Docker and Podman accept the passthrough flags unchanged; Apple's `container` runtime ignores options it doesn't understand.

> **SSH agent (macOS):** On macOS, `--ssh-agent` requires the Docker VM to forward the SSH agent. For **Colima**: edit `~/.colima/default/colima.yaml`, set `forwardAgent: true`, then restart (`colima stop && colima start`). **Docker Desktop** forwards the agent automatically.

> **Networking:** By default, yolobox uses Docker's bridge network (internet access, no container DNS). Use `--network <name>` to join a docker compose network and access services by name. Use `--no-network` for complete isolation.

> **Docker access:** The `--docker` flag mounts the host Docker socket into the container and joins a shared `yolobox-net` network. This lets the AI agent run Docker commands (build images, start containers, use docker compose) that create sibling containers on the same network. The agent and any services it creates can communicate by container name. The network name is available inside the container as `$YOLOBOX_NETWORK`.

> **Clipboard bridge:** `--clipboard` starts a short-lived host proxy and exposes text clipboard command shims (`pbcopy`, `pbpaste`, `xclip`, `xsel`, `wl-copy`, `wl-paste`) inside the container.

> **Project filtering:** `--exclude` globs are evaluated relative to the project root. `--copy-as` destinations must already exist as files in the project. Both flags currently require `--readonly-project` and are incompatible with `--no-project`. Apple's `container` runtime does not support them yet.

## Philosophy: It's the AI's Box, Not Yours

yolobox is designed for AI agents, not humans. The typical workflow is:

```bash
yolobox claude    # Launch Claude Code in the sandbox
yolobox codex     # Or Codex, or any other AI tool
```

That's it. You launch the AI and let it work. You're not meant to manually enter the box and set things up—the AI does that itself.

**Why?** The AI agent has full sudo access inside the container. If it needs a compiler, database, or framework—it just installs it. Named volumes persist these installations across sessions, so setup happens once. You point it at your project and let it cook.

## Security Model

### How It Works

yolobox uses **container isolation** (Docker or Podman) as its security boundary. When you run `yolobox`, it:

1. Starts a container with your project mounted at its real path
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
- Network access (use `--no-network` to disable, or `--network <name>` for specific networks)
- The container itself (the AI has root via sudo)
- Against kernel exploits or container escape vulnerabilities

If you want a narrower view of the project, use `--exclude` and `--copy-as` to hide or replace selected files before the agent sees them.

### Hardening Options

**Level 1: Basic (default)**
```bash
yolobox  # Standard container isolation
```

**Level 2: Reduced attack surface**
```bash
yolobox claude --no-network --readonly-project --exclude ".env*" --exclude "secrets/**"
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

Need more control than `packages` or a small Dockerfile fragment? You can still build and use a fully custom image:

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

Using a custom image name means `yolobox upgrade` won't overwrite your customization. That is the upside.

The downside is that you now own the drift:

- upstream Dockerfile changes do not automatically flow into your custom image
- `yolobox upgrade` will update the binary, but it will not rebuild or migrate your custom image for you
- when the base image changes upstream, you need to pull those changes into your fork and rebuild manually
- the farther your custom Dockerfile drifts, the more upgrade work you take on

If you mostly need "add a few tools for this project", prefer project-level customization above. Use a fully custom image only when you need full control over the entire base image.

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
