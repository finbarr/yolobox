# Installation & Setup

## What yolobox does

yolobox runs an AI coding agent inside a container where:

- your project is mounted at its real path
- the agent has full permissions and sudo inside the box
- your home directory is not mounted unless you explicitly opt in
- named volumes preserve tools and session state across runs

The default workflow is simple:

```bash
cd /path/to/your/project
yolobox claude
```

Use `claude`, `codex`, `gemini`, `agy`, `opencode`, `copilot`, or `pi` depending on the tool you want to run.

## Install

### Homebrew

```bash
brew install finbarr/tap/yolobox
```

### Install script

```bash
curl -fsSL https://raw.githubusercontent.com/finbarr/yolobox/master/install.sh | bash
```

The install script downloads a release binary when one is available for your platform. If it cannot, it falls back to building from source.

## First run

Start from any project:

```bash
cd /path/to/your/project
yolobox claude
```

Other common entry points:

```bash
yolobox codex
yolobox gemini
yolobox agy
yolobox copilot
yolobox pi
yolobox run make test
yolobox shell
```

Use `yolobox shell` when you want a shell. Use `yolobox run ...` when you want one command. Use the AI shortcuts when you want the intended workflow.

You can also set a default harness in config, such as `default_harness = "codex"`, so bare `yolobox` launches that AI shortcut. Set it to `none` or leave it unset to keep bare `yolobox` as the shell shortcut.

## Runtime support

yolobox auto-detects the first supported runtime it can use. On macOS the
order is Apple container → Docker → Podman; on Linux it is Docker → Podman.

| Platform | Supported runtimes |
|---|---|
| macOS | Apple container (macOS 26+, Apple silicon, container >= 1.0), Docker Desktop, OrbStack, Colima |
| Linux | Docker, Podman |

Apple's [container](https://github.com/apple/container) runs each sandbox in
its own lightweight VM with no Docker daemon required:

```bash
brew install container
container system start
```

yolobox requires container >= 1.0 and skips older installs during
auto-detection (it starts the system service automatically when needed).

Force a runtime explicitly:

```bash
yolobox claude --runtime container
yolobox claude --runtime docker
yolobox claude --runtime podman
```

## Next pages

- [Commands](/commands): shortcut commands, shell usage, and maintenance commands
- [Recipes](/recipes): named agent environments, Git remote synchronization, and webapp routing
- [What's in the Box](/whats-in-the-box): preinstalled tools and YOLO-mode wrappers
- [Project-Level Customization](/customizing): add packages or a Dockerfile fragment per project
- [Configuration](/configuration): global defaults, project config, copied instructions, and auto-forwarded env vars

::: warning Memory requirements
Claude Code needs at least 4 GB of RAM allocated to Docker. Colima defaults to 2 GB, which often leads to OOM kills.

```bash
colima stop && colima start --memory 8
```

Apple container allocates memory per container VM, so yolobox defaults it to
4 GB there. Raise it with `--memory 8g` or `memory = "8g"` in config if needed.
:::
