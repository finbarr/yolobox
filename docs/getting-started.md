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

Use `claude`, `codex`, `gemini`, `opencode`, `copilot`, or `pi` depending on the tool you want to run.

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
yolobox copilot
yolobox pi
yolobox run make test
yolobox shell
```

Use `yolobox shell` when you want a shell. Use `yolobox run ...` when you want one command. Use the AI shortcuts when you want the intended workflow.

You can also set a default harness in config, such as `default_harness = "codex"`, so bare `yolobox` launches that AI shortcut. Set it to `none` or leave it unset to keep bare `yolobox` as the shell shortcut.

For remote work, `yolobox login` opens a browser approval flow and stores backend auth, then `yolobox remote create foo` creates a named machine from the hosted or self-hosted backend and syncs the current folder. Use `--tier small`, `--tier medium`, or `--tier large` on create when a new VM needs a specific size. Create fails when the name already exists. `yolobox remote run foo codex` syncs again and runs from the source-path workdir on the VM, while `yolobox remote connect foo` opens a shell without syncing. Remote commands print progress while backend provisioning and SSH startup are pending. Backends with a preview base domain give each machine a generated HTTPS preview URL and the CLI prints it on its own line when the machine is ready. See [Remote Mode](/remote) for the workflow.

## Runtime support

yolobox auto-detects the first supported runtime it can use.

| Platform | Supported runtimes |
|---|---|
| macOS | Docker Desktop, OrbStack, Colima, Apple container (macOS Tahoe+) |
| Linux | Docker, Podman |

Force a runtime explicitly:

```bash
yolobox claude --runtime docker
yolobox claude --runtime podman
yolobox claude --runtime container
```

## Next pages

- [Commands](/commands): shortcut commands, shell usage, and maintenance commands
- [Recipes](/recipes): named agent environments, folder synchronization, and webapp routing
- [Remote Mode](/remote): backend-backed machines, persistent sessions, and generated preview URLs
- [What's in the Box](/whats-in-the-box): preinstalled tools and YOLO-mode wrappers
- [Project-Level Customization](/customizing): add packages or a Dockerfile fragment per project
- [Configuration](/configuration): global defaults, project config, copied instructions, and auto-forwarded env vars

::: warning Memory requirements
Claude Code needs at least 4 GB of RAM allocated to Docker. Colima defaults to 2 GB, which often leads to OOM kills.

```bash
colima stop && colima start --memory 8
```
:::
