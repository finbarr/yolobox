# Commands

## Default workflow

yolobox is built around AI shortcut commands:

```bash
yolobox claude
yolobox codex
yolobox gemini
yolobox opencode
yolobox copilot
yolobox pi
```

That is the intended path. You point the agent at a project and let it work inside the sandbox.

If you use one tool most of the time, set `default_harness = "codex"` or another shortcut name in config. Then bare `yolobox` launches that tool. Set `default_harness = "none"` or leave it unset to keep bare `yolobox` as an interactive shell.

If you set `mode = "remote"`, `remote_name = "foo"`, `remote_workspace = "app"`, and `default_harness = "codex"`, bare `yolobox` attaches to `yolobox remote resume foo/app codex`.

## Command reference

### AI shortcuts

```bash
yolobox claude
yolobox codex
yolobox gemini
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
yolobox remote --name <env> [cmd...] # Create or reuse a named remote machine and default workspace
yolobox remote --name <env> --workspace <name> [cmd...] # Use a named remote workspace
yolobox remote resume [<env>[/<workspace>]] [cmd...] # Reattach to a remote tmux session
yolobox remote sync up [<env>[/<workspace>]] # Copy the current folder to the remote workspace
yolobox remote sync down [<env>[/<workspace>]] --force # Copy the remote workspace back locally
yolobox remote forward [<env>[/<workspace>]] <port> # Forward a remote preview port to localhost
yolobox remote stop [<env>[/<workspace>]] # Stop the remote tmux session
yolobox remote list             # List locally registered remote machines and workspaces
yolobox remote status [<env>[/<workspace>]] # Show local and backend state
yolobox remote destroy <env> --force # Release the backend host and local registry entry
yolobox remote backend serve --provider digitalocean # Run a self-hosted machine backend
yolobox setup               # Write global defaults to ~/.config/yolobox/config.toml
yolobox config              # Print the resolved config for the current project
yolobox upgrade             # Update the binary and pull the latest base image
yolobox upgrade --check     # Show latest release notes without upgrading
yolobox reset --force       # Remove yolobox named volumes
yolobox uninstall --force   # Remove yolobox binary, image, and volumes
yolobox version             # Print version and platform
yolobox help                # Show CLI help
```

The optional remote target can be omitted only when `remote_name` is configured. If `remote_workspace` is also configured, that workspace is used; otherwise yolobox uses `default`.

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

### Work on a remote machine

```bash
yolobox remote --name foo codex
yolobox remote --name foo --workspace app codex
yolobox remote --provider digitalocean --name foo codex
yolobox remote resume foo/app codex
yolobox remote forward foo/app 3000
yolobox remote forward 3000 # uses configured remote_name and remote_workspace
yolobox remote backend serve --provider digitalocean --listen 0.0.0.0:8787
```

Remote mode requires a configured backend URL and token, or a direct provider such as `--provider digitalocean` with `DIGITALOCEAN_ACCESS_TOKEN`. The backend or direct provider leases an SSH host; yolobox mirrors the current folder to a named workspace on that host with `rsync`, then starts the requested command over SSH. Use `yolobox remote sync up foo/app` when you want the remote host to get the latest local folder contents. Use `yolobox remote sync down foo/app --force` only when the remote copy should overwrite local files.

The MVP copies the whole current folder. That includes `.git` if present, uncommitted files, ignored files, `.env` files, dependency folders, build output, and local caches.

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

Use `remote` when you want a named machine and workspace that can keep running after your laptop disconnects.

Use `yolobox shell` when you are debugging or exploring manually, not as the main path.
