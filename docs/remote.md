# Remote Mode

Remote mode is the roadmap for named yolobox machines that run away from your laptop. It should feel like fork mode in the sense that each name owns an isolated working environment, but the unit of isolation is a remote VM instead of a copied local folder.

## MVP

The first implementation deliberately avoids a hosted control plane. The local CLI manages DigitalOcean Droplets through `doctl`, stores machine metadata in a local registry, and connects over SSH.

Target commands:

```bash
yolobox remote --name foo codex
yolobox remote resume foo codex
yolobox remote sync foo
yolobox remote list
yolobox remote status foo
yolobox remote destroy foo --force
```

The MVP should:

- create or reuse a named DigitalOcean Droplet
- install Docker, tmux, git, rsync, and yolobox on the remote host
- mirror the current folder into the remote host with `rsync`
- refresh that full folder mirror on `remote sync`
- run the requested harness inside a persistent tmux session
- let the user reattach after closing their laptop
- keep enough local registry state to list, resume, sync, and destroy machines later

The MVP does not include team sharing, hosted DNS, a dashboard, automatic billing, or a remote clipboard/open bridge. Those belong to later phases.

## Prerequisites

Install and authenticate the DigitalOcean CLI before using the MVP:

```bash
doctl auth init
```

Remote provisioning also requires a DigitalOcean SSH key ID or fingerprint. Configure it with `remote.ssh_key` or pass `--ssh-key`.

The local machine also needs `ssh` and `rsync`. yolobox checks for those before creating a Droplet so a missing sync tool does not leave a half-provisioned VM behind.

## Configuration

Remote defaults live in the normal yolobox config files:

```toml
mode = "remote"
remote_name = "foo"
default_harness = "codex"

[remote]
provider = "digitalocean"
region = "nyc3"
size = "s-2vcpu-4gb"
image = "ubuntu-24-04-x64"
ssh_key = "your-digitalocean-ssh-key-id-or-fingerprint"
ssh_user = "root"
```

With that config, bare `yolobox` should resolve to:

```bash
yolobox remote resume foo codex
```

Project config can add setup commands that run after the folder is copied or updated:

```toml
[remote]
setup = [
  "docker compose pull",
  "docker compose up -d db redis"
]
```

## Sync Model

The MVP mirrors the whole current folder with `rsync -az --delete`. The remote project path is `/root/yolobox-projects/<folder>`, and the registry records the last local source path, Git remote, and branch when those are available.

This is intentionally closer to fork mode than a Git checkout: `.git`, untracked files, ignored files, `.env` files, dependency folders, build output, and local caches are all copied if they live under the current folder. That makes the remote feel like the local workspace, but it also means secrets in the project folder leave your laptop. Remove or relocate anything you do not want on the VM before syncing.

## Local Registry

The local registry records the user-visible remote name, provider, Droplet ID, public IP, repo URL, branch, remote project path, SSH user, and timestamps. It is local state, not the source of truth for the cloud provider. If the registry drifts, `yolobox remote status` should refresh from the provider where possible.

## Later Phases

Future hosted mode can replace the local-only control flow with a managed control plane while preserving the same CLI contract.

- Hosted control plane for account login, teams, DNS, TLS, lifecycle management, idle cleanup, and billing.
- Provider abstraction for Hetzner, AWS, existing SSH hosts, and other VM backends.
- Team sharing with owner, collaborator, and viewer roles.
- Authenticated app URLs such as `web.foo.hosted.yolobox.dev`.
- Remote clipboard and URL-open bridge routed through an attached local CLI or browser session.
- Dashboard for team machines, owners, exposed ports, costs, and activity.
