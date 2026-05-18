# Remote Mode

Remote mode gives yolobox a machine that keeps running after your laptop disconnects. It is designed for Claude, Codex, and other terminal agents that benefit from real Linux compute, persistent sessions, and preview ports without turning yolobox into a hosted IDE.

The open-source path deliberately starts without a hosted control plane. The local CLI uses your DigitalOcean CLI credentials, connects over SSH, stores registry metadata locally, and keeps the core workflow usable for bring-your-own infrastructure. Managed machines, public preview URLs, team auth, billing, and dashboards belong to a later hosted layer.

## Mental model

Remote support has four separate concepts:

- **Machine:** the remote compute target, currently a DigitalOcean Droplet created through `doctl`.
- **Workspace:** a durable project copy on a machine. A machine can host more than one named workspace.
- **Session:** a persistent `tmux` session inside a workspace. Closing the local terminal does not stop it.
- **Exposure:** explicit port access. The MVP supports local SSH forwarding; managed public URLs are not part of the open-source MVP.

Keeping these separate matters. A machine can outlive a project, a workspace can outlive an attached terminal, and preview access should be opt-in rather than implied by starting a web server.

## Commands

```bash
yolobox remote --name foo codex
yolobox remote --name foo --workspace app codex
yolobox remote resume foo/app codex
yolobox remote attach foo/app codex
yolobox remote sync up foo/app
yolobox remote sync down foo/app --force
yolobox remote forward foo/app 3000
yolobox remote stop foo/app
yolobox remote list
yolobox remote status foo/app
yolobox remote destroy foo --force
```

`foo` is the machine name. `app` is the workspace name. If you omit the workspace, yolobox uses `default`, so `foo` and `foo/default` refer to the same default workspace.

If `remote_name` is configured, commands that take a remote target can omit `foo/app`; `remote_workspace` selects the workspace, defaulting to `default`.

## Prerequisites

Install and authenticate the DigitalOcean CLI before provisioning:

```bash
doctl auth init
```

Remote provisioning also requires a DigitalOcean SSH key ID or fingerprint. Configure it with `remote.ssh_key` or pass `--ssh-key`.

The local machine must have `ssh` and `rsync`. yolobox checks for `doctl`, `ssh`, and `rsync` before creating a Droplet so a missing sync tool does not leave a half-provisioned machine behind.

## Configuration

Remote defaults live in normal yolobox config:

```toml
mode = "remote"
remote_name = "foo"
remote_workspace = "app"
default_harness = "codex"

[remote]
provider = "digitalocean"
region = "nyc3"
size = "s-2vcpu-4gb"
image = "ubuntu-24-04-x64"
ssh_key = "your-digitalocean-ssh-key-id-or-fingerprint"
ssh_user = "root"
setup = [
  "docker compose pull",
  "docker compose up -d db redis"
]
```

With that config, bare `yolobox` behaves like:

```bash
yolobox remote resume foo/app codex
```

`remote.setup` commands run after `sync up` finishes. They are useful for pulling images, starting databases, or refreshing dependencies on the remote machine.

## Provisioning

The first `yolobox remote --name foo ...` call creates a Droplet named `yolobox-foo`, waits for SSH, installs Docker, `tmux`, `git`, `rsync`, and yolobox, pulls the base image, creates the requested workspace, syncs the project, and attaches to the session.

Later calls reuse the registered machine and workspace. `yolobox remote status foo/app` refreshes the Droplet IP from DigitalOcean when possible.

## Sync

`sync up` mirrors the current local folder to the remote workspace with `rsync -az --delete`:

```bash
yolobox remote sync up foo/app
```

The remote project path is:

```text
/root/yolobox-workspaces/<machine>-<workspace>/<folder>
```

The sync is intentionally closer to fork mode than a Git checkout. `.git`, untracked files, ignored files, `.env` files, dependency folders, build output, and local caches are copied if they live under the current folder. That makes the remote feel like the local workspace, but it also means secrets in the project folder leave your laptop.

`sync down` copies the remote workspace back into the current local folder and requires `--force`:

```bash
yolobox remote sync down foo/app --force
```

Use `sync down` only when the remote copy should overwrite local files. For Git projects, committing changes in the remote session and pushing a branch is usually a cleaner handoff.

## Sessions

Remote sessions use `tmux`. The default session name is `main`, and the underlying tmux session is namespaced by machine and workspace. Reattach with:

```bash
yolobox remote resume foo/app codex
```

Stop the default session with:

```bash
yolobox remote stop foo/app
```

The open-source MVP runs the requested harness through the remote yolobox CLI inside the synced workspace. It does not yet run a managed long-lived workspace container with per-workspace volumes; the registry already records workspace container and volume names so the implementation can grow in that direction without changing the CLI contract.

## Preview ports

Remote mode does not expose public ports by default. For local preview access, forward a remote port over SSH:

```bash
yolobox remote forward foo/app 3000
yolobox remote forward foo/app 3000 --local-port 13000
```

The first command opens `http://127.0.0.1:3000` on your laptop and forwards it to `127.0.0.1:3000` on the remote machine. Press `Ctrl+C` to stop forwarding.

Managed URLs such as `*.yolobox.dev`, team-private previews, and public links are intentionally separate from terminal attach and are not part of the open-source MVP.

## Local registry

The local registry lives at:

```text
~/.local/state/yolobox/remotes.json
```

It records machines, workspaces, sessions, and exposures. It is local state, not the source of truth for the cloud provider. If the registry drifts, `remote status` refreshes what it can from DigitalOcean.

## Security

Treat a remote machine like another trusted development machine:

- Anyone with SSH access to the VM can inspect synced files, tmux sessions, running containers, and forwarded preview traffic.
- `sync up` copies project-local secrets if they are in the folder.
- `sync down --force` can overwrite local files.
- Public preview URLs are not created by the MVP.
- If a future hosted mode allows Docker socket access, the machine should be dedicated to one user or workspace because Docker access is effectively host-level access.

## Roadmap

The current CLI contract is intended to survive future hosted work.

Planned open-source improvements:

- remote agent subcommands for workspace management over SSH
- long-lived workspace containers with per-workspace home, cache, output volumes, and networks
- explicit secret/config staging for Claude, Codex, Git, and GitHub auth
- better dirty-state checks before `sync up` and `sync down`
- local port-forward lifecycle listing and cleanup
- bring-your-own SSH host support beyond DigitalOcean provisioning

Hosted or team features:

- managed machines and warm pools
- managed preview URLs with TLS and auth
- team sharing roles for viewer, commenter, controller, and owner
- audit logs for attaches, commands, syncs, and exposures
- snapshots, clone, restore, idle shutdown, budget controls, and policy controls
