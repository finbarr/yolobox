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

If you set `mode = "remote"`, `remote_name = "foo"`, and `default_harness = "codex"`, bare `yolobox` behaves like `yolobox remote run foo codex`.

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
yolobox login [--backend-url <url>] # Open browser login for remote backend
yolobox login [--backend-url <url>] --no-open # Print browser URL without opening it
yolobox logout              # Revoke and clear remote backend auth
yolobox remote create <env> [--tier small|medium|large] [--no-sync] # Create a named remote machine
yolobox remote run <env> <cmd...> # Sync, then run on an existing remote machine
yolobox remote connect <env> # Open a shell on a backend-known machine without syncing
yolobox remote sync up <env> # Copy the current folder to the remote machine
yolobox remote sync down <env> --force # Copy the remote project back locally
yolobox remote list             # List backend machines
yolobox remote status <env>     # Show backend machine state
yolobox remote destroy <env> --force # Release the backend machine
yolobox setup               # Write global defaults to ~/.config/yolobox/config.toml
yolobox config              # Print the resolved config for the current project
yolobox upgrade             # Update the binary and pull the latest base image
yolobox upgrade --check     # Show latest release notes without upgrading
yolobox reset --force       # Remove yolobox named volumes
yolobox uninstall --force   # Remove yolobox binary, image, and volumes
yolobox version             # Print version and platform
yolobox help                # Show CLI help
```

Remote subcommands require an explicit machine name. `remote_name` is used only by bare `yolobox` when `mode = "remote"`.

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
yolobox login
yolobox remote create foo
yolobox remote create foo --tier medium
yolobox remote run foo codex
yolobox remote connect foo
yolobox remote sync up foo
yolobox remote sync down foo --force
yolobox remote status foo # shows the generated preview URL when the backend has one
```

Remote mode requires a Better Auth session from `yolobox login` or `YOLOBOX_TOKEN`. Plain `yolobox login` prints a browser URL, tries to open it, and waits while the web app grants CLI access; use `--no-open` on SSH/headless hosts. The CLI defaults to the hosted backend at `https://api.yolobox.dev`; set `remote.backend_url`, `YOLOBOX_BACKEND_URL`, or `yolobox login --backend-url` for a self-hosted backend. The hosted browser console is intended for `https://app.yolobox.dev`. The backend leases a bootstrapped SSH host for the authenticated user; yolobox mirrors the current folder to `/opt/yolobox/project` with `rsync`, maps the original local source path to that storage directory on the VM, then starts requested commands over SSH from the source-path workdir. Hosted DigitalOcean backends should set `YOLOBOX_REMOTE_IMAGE` to a golden snapshot id built by `deploy/digitalocean/build-remote-image.sh` so creates return bootstrapped machines. Remote commands run directly on the VM with yolobox wrappers on `PATH`, not inside a nested yolobox container, so Docker Compose and installed packages persist on the machine. The CLI stores no local machine registry; list, status, create, destroy, and connect all come from the backend. `yolobox remote create foo` creates a machine and syncs the current folder by default; pass `--no-sync` to skip that copy, or `--tier small`, `--tier medium`, or `--tier large` to choose the VM size. Create fails when the name already exists. `yolobox remote run foo ...` syncs the folder and then runs the command on an existing machine. `yolobox remote connect foo` opens or attaches to the managed tmux session without syncing, bootstrapping, or changing the remote workdir alias; it fails if backend metadata says bootstrap has not completed. Remote commands print progress while backend provisioning and SSH startup are pending; when a machine is ready, any generated preview URL is shown on its own line. Use `yolobox remote sync up foo` when you want to copy without running a command. Backends with a preview base domain assign a generated HTTPS URL to each machine and export it inside remote sessions as `YOLOBOX_PREVIEW_URL`. Use `yolobox remote sync down foo --force` only when the remote copy should overwrite local files.

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

Use `remote` when you want a named machine that can keep running after your laptop disconnects.

Use `yolobox shell` when you are debugging or exploring manually, not as the main path.
