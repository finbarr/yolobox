# Commands

## Default workflow

yolobox is built around AI shortcut commands:

```bash
yolobox claude
yolobox codex
yolobox gemini
yolobox agy
yolobox antigravity
yolobox opencode
yolobox copilot
yolobox pi
```

That is the intended path. You point the agent at a project and let it work inside the sandbox.

If you use one tool most of the time, set `default_harness = "codex"` or another shortcut name in config. Then bare `yolobox` launches that tool. Set `default_harness = "none"` or leave it unset to keep bare `yolobox` as an interactive shell.

## Command reference

### AI shortcuts

```bash
yolobox claude
yolobox codex
yolobox gemini
yolobox agy
yolobox antigravity
yolobox opencode
yolobox copilot
yolobox pi
```

These launch the matching tool inside yolobox and apply the tool-specific YOLO-mode wrapper when one exists.

### General commands

```bash
yolobox                     # Run configured default harness, or shell if none
yolobox shell               # Open an interactive shell
yolobox run <cmd...>        # Run a single command in the sandbox
yolobox fork --name <env> <cmd...> # Run in a named copied folder with a Compose namespace
yolobox fork resume <env> [cmd...] # Reopen an existing copied folder
yolobox fork discard <env> --force # Delete a copied folder
yolobox setup               # Write global defaults to ~/.config/yolobox/config.toml
yolobox config              # Print the resolved config for the current project
yolobox upgrade             # Update the binary and pull the latest base image
yolobox upgrade --check     # Show latest release notes without upgrading
yolobox reset --force       # Remove yolobox named volumes
yolobox uninstall --force   # Remove yolobox binary, image, and volumes
yolobox version             # Print version and platform
yolobox help                # Show CLI help
```

## Common examples

### Start an agent with Docker access

```bash
yolobox claude --docker --git-config --gh-token
```

### Start an agent that can open host browser URLs

```bash
yolobox codex --open-bridge
```

### Start an agent with RTK compression

```bash
yolobox codex --rtk
```

### Run one command in isolation

```bash
yolobox run --no-network --no-env-passthrough --readonly-project python3 untrusted_script.py
```

### Name the runtime container

```bash
yolobox run --name yolobox-dev sleep 60
```

### Run parallel agents on one project

```bash
yolobox fork --name bruno codex
yolobox fork --name diane claude
```

Fork mode gives each agent its own complete copy of the current project folder, like another developer working on their own machine. Instead of many agents competing on one machine and one folder, you get many named agent environments, each with its own folder and Docker Compose namespace. If the folder contains a Git checkout, use your Git remote as the sync point, just like you would with teammates.

The copy lives at `../.yolobox-forks/<folder>/<env>` and is mounted inside the container at the original source path. Yolobox also sets a unique `COMPOSE_PROJECT_NAME`, so default Docker Compose containers, networks, and named volumes are namespaced by fork.

When the fork exits, yolobox runs best-effort Compose cleanup if it finds a Compose file. The copied folder is preserved until you explicitly discard it:

```bash
yolobox fork resume bruno codex
yolobox fork discard bruno --force
```

See [Recipes](/recipes) for common fork workflows, including webapp routing.

### Hide secrets from the sandboxed view

```bash
yolobox claude --readonly-project --exclude ".env*" --exclude "secrets/**" --copy-as ".env.sandbox:.env"
```

### Build with extra packages for one project

```bash
yolobox run --packages default-jdk,maven mvn --version
```

### Inspect the resolved configuration

```bash
yolobox config
```

### Trace startup timing

```bash
YOLOBOX_TIMING=1 yolobox run true
```

### Inspect the latest release before upgrading

```bash
yolobox upgrade --check
```

The check prints the current version, latest version, and a short summary from the release notes without downloading a binary or pulling the image.

### Reset persistent state

```bash
yolobox reset --force
```

## Mental model

Use shortcut commands when you want an AI agent session.

Use `run` when you want one exact command in the same sandbox model.

Use `fork` when you want concurrent sessions on the same project folder without sharing files or the default Compose project namespace.

Use `yolobox shell` when you are debugging or exploring manually, not as the main path.
