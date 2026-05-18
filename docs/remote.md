# Remote Mode

Remote mode gives yolobox a machine that keeps running after your laptop disconnects. It is designed for Claude, Codex, and other terminal agents that benefit from real Linux compute, persistent sessions, and preview ports without turning yolobox into a hosted IDE.

This page is the client and backend contract. The yolobox CLI should not know whether a machine came from a hosted yolobox service, a self-hosted pool, Hetzner, AWS, bare metal, or anything else. Provider-specific provisioning belongs behind the backend API.

## Status

Implemented in the client:

- backend-only machine leasing over HTTP
- named machines and named workspaces
- persistent `tmux` sessions
- full-folder sync up and forced sync down
- local SSH preview forwarding
- local registry state for machines, workspaces, sessions, and exposures
- host bootstrap when the backend returns an unprepared machine

Not implemented in this repo:

- hosted backend service
- self-hosted backend server
- provider-specific provisioners
- public preview URLs
- managed workspace containers with per-workspace volumes
- secret/config staging for Claude, Codex, Git, or GitHub auth

## Goals

- Make `yolobox remote --name foo codex` feel like the normal local yolobox workflow, but on durable Linux compute.
- Keep the CLI provider-agnostic. The client leases SSH hosts from a backend and then handles sync, attach, and forwarding.
- Support both hosted and self-hosted backends with the same client contract.
- Keep remote access explicit. Starting a remote command must not imply public port exposure.
- Preserve local escape hatches: users can inspect local registry state, sync back with `--force`, and destroy a lease.

## Non-goals

- The yolobox client does not create cloud VMs directly.
- The yolobox client does not contain provider SDKs or provider-specific CLI wrappers.
- The open-source client does not create public preview URLs.
- The current client does not yet run a long-lived managed workspace container on the remote host.

## Mental Model

Remote support has four separate concepts:

- **Machine:** the remote compute target returned by the backend.
- **Workspace:** a durable project copy on a machine. A machine can host more than one named workspace.
- **Session:** a persistent `tmux` session inside a workspace. Closing the local terminal does not stop it.
- **Exposure:** explicit port access. The open-source client supports local SSH forwarding; public URLs belong to a backend or hosted service.

Keeping these separate matters. A machine can outlive a project, a workspace can outlive an attached terminal, and preview access should be opt-in rather than implied by starting a web server.

## CLI Contract

```bash
yolobox remote --name foo codex
yolobox remote --name foo --workspace app codex
yolobox remote resume foo/app codex
yolobox remote attach foo/app codex
yolobox remote sync up foo/app
yolobox remote sync down foo/app --force
yolobox remote forward foo/app 3000
yolobox remote forward foo/app 3000 --local-port 13000
yolobox remote stop foo/app
yolobox remote list
yolobox remote status foo/app
yolobox remote destroy foo --force
```

`foo` is the machine name. `app` is the workspace name. If the workspace is omitted, yolobox uses `default`, so `foo` and `foo/default` refer to the same workspace.

Names are lowercased and must use lowercase letters, numbers, and hyphens. They must start with a letter or number and are capped at 63 characters.

If `remote_name` is configured, commands that take a remote target can omit `foo/app`; `remote_workspace` selects the workspace and defaults to `default`.

`remote expose` is intentionally reserved for future managed preview URLs. Today it returns an error that points users to `remote forward`.

## Configuration

Remote defaults live in normal yolobox config:

```toml
mode = "remote"
remote_name = "foo"
remote_workspace = "app"
default_harness = "codex"

[remote]
backend_url = "https://remote.example.com"
# Prefer YOLOBOX_REMOTE_TOKEN for local testing instead of committing this.
backend_token = "your-backend-token"
ssh_user = "root"
setup = [
  "docker compose pull",
  "docker compose up -d db redis"
]
```

`backend_url` is required for remote mode unless `--backend-url` is passed. `backend_token` may be omitted when `YOLOBOX_REMOTE_TOKEN` is set.

`ssh_user` defaults to `root` and is used when the backend response does not include `ssh_user`.

`remote.setup` commands run after `sync up` finishes. They run from the remote workspace project directory with `set -euo pipefail`.

With this config, bare `yolobox` behaves like:

```bash
yolobox remote resume foo/app codex
```

## Backend Responsibilities

The backend is the control plane. It owns allocation and release of hosts. It may be hosted or self-hosted. It may lease from a static pool, warm pool, cloud provider, on-prem cluster, or any other substrate.

The backend must:

- authenticate every non-health request
- lease one host for a logical machine name
- make `POST /v1/machines/ensure` idempotent for the same name
- return an SSH-reachable host address in `machine.public_ipv4`
- return `ssh_user` when the host should not be reached as `root`
- release or mark the lease idle on `DELETE /v1/machines/{name}`
- keep provider-specific details behind the API

The backend may:

- return prewarmed hosts with `bootstrap_complete = true`
- return fresh hosts with `bootstrap_complete = false` or omitted
- include informational metadata such as `provider_id`, `region`, `size`, and `image`
- implement billing, policy, idle shutdown, snapshots, or audit logs without changing the client workflow

## Client Responsibilities

The yolobox client owns the developer workflow after a host is leased. It:

- sends the desired machine name, workspace name, repo URL, and branch to the backend
- waits for SSH when the backend does not mark the host bootstrapped
- bootstraps Docker, `tmux`, `git`, `rsync`, and yolobox when needed
- mirrors the local folder with `rsync`
- runs setup commands after upward sync
- starts or attaches to `tmux` sessions
- runs noninteractive commands directly over SSH
- records local registry state
- forwards local preview ports with SSH

The client requires local `ssh` and `rsync`.

## Backend HTTP API

All non-health requests use:

```http
Authorization: Bearer <token>
Content-Type: application/json
```

The client times out backend requests after 30 seconds. Any non-2xx response is treated as an error and the response body is surfaced when present.

### `GET /healthz`

Health check endpoint for operators and load balancers. The current client does not require it during normal commands.

Expected successful response:

```http
200 OK
```

### `POST /v1/machines/ensure`

Lease or return the host for a logical machine name. This operation must be idempotent for the same authenticated owner and machine name.

Request:

```json
{
  "name": "foo",
  "workspace": "app",
  "repo_url": "git@github.com:example/project.git",
  "branch": "feature/remote-work"
}
```

Fields:

- `name` is required and is the logical machine name.
- `workspace` is optional and defaults to `default` in the client.
- `repo_url` is optional Git metadata from the local checkout.
- `branch` is optional Git metadata from the local checkout.

Successful response:

```json
{
  "machine": {
    "name": "foo",
    "public_ipv4": "203.0.113.10",
    "ssh_user": "root",
    "provider_id": "host-a",
    "region": "fsn1",
    "size": "cx22",
    "image": "ubuntu-24.04",
    "created_at": "2026-05-18T17:00:00Z",
    "updated_at": "2026-05-18T17:00:00Z",
    "bootstrap_complete": true
  },
  "status": "leased"
}
```

Required response fields:

- `machine.public_ipv4`

Optional response fields:

- `machine.name`; the client overwrites it with the requested name.
- `machine.ssh_user`; defaults to config `remote.ssh_user`, then `root`.
- `machine.provider_id`, `region`, `size`, and `image`; informational.
- `machine.backend_url`; the client stores the configured backend URL if omitted.
- `machine.bootstrap_complete`; false when omitted.
- `status`; displayed by `remote status`.

Recommended error responses:

- `400` for invalid names or malformed JSON
- `401` for missing or invalid token
- `409` when no host is available
- `500` for backend failures

### `GET /v1/machines/{name}`

Return the current lease for a logical machine name. `yolobox remote status` uses this to refresh address and metadata.

Successful response:

```json
{
  "machine": {
    "name": "foo",
    "public_ipv4": "203.0.113.10",
    "ssh_user": "root",
    "provider_id": "host-a",
    "bootstrap_complete": true
  },
  "status": "running"
}
```

Recommended error responses:

- `401` for missing or invalid token
- `404` when the machine name is not leased

### `DELETE /v1/machines/{name}`

Release the backend lease. The backend decides whether release means destroy, stop, return to pool, or mark idle.

Successful response:

```http
204 No Content
```

Backends should prefer idempotent success when a lease is already gone if that is safe for their ownership model. The current client treats any non-2xx response as an error.

## Machine Lifecycle

1. User runs `yolobox remote --name foo --workspace app codex`.
2. Client loads config and validates `remote.backend_url` plus token.
3. Client checks for an existing local registry machine.
4. If none exists, client calls `POST /v1/machines/ensure`.
5. Backend leases or returns an SSH host.
6. Client saves the machine in local registry.
7. If `bootstrap_complete` is false, client waits up to five minutes for SSH and bootstraps the host.
8. Client ensures the workspace path exists.
9. Client syncs the local project folder to the remote workspace.
10. Client runs any configured setup commands.
11. Client starts or attaches to the requested command/session.

Later runs reuse the local machine and workspace registry entries. `remote status` can refresh backend state. `remote destroy --force` releases the backend lease and removes local machine, workspace, session, and exposure records for that machine.

## Host Bootstrap

When a backend returns `bootstrap_complete = false` or omits it, the client assumes an Ubuntu-like host reachable over SSH with enough privilege for `apt-get` and Docker installation.

The bootstrap script:

- waits for `cloud-init` when present
- installs `ca-certificates`, `curl`, `git`, `rsync`, and `tmux`
- installs Docker through `get.docker.com` when Docker is missing
- installs yolobox through the repository install script when yolobox is missing
- pulls `ghcr.io/finbarr/yolobox:latest` best-effort
- creates `/opt/yolobox-workspaces`

Backends that do not want client-side bootstrap should return hosts that already have these pieces and set `bootstrap_complete = true`.

## Workspace Sync

`sync up` mirrors the current local folder to the remote workspace with:

```text
rsync -az --delete --human-readable --info=stats1
```

The remote project path is:

```text
/opt/yolobox-workspaces/<machine>-<workspace>/<folder>
```

The sync is intentionally closer to fork mode than a Git checkout. `.git`, untracked files, ignored files, `.env` files, dependency folders, build output, and local caches are copied if they live under the current folder.

`sync down` copies the remote workspace back into the current local folder and requires `--force`:

```bash
yolobox remote sync down foo/app --force
```

Use `sync down` only when the remote copy should overwrite local files. For Git projects, committing changes in the remote session and pushing a branch is usually a cleaner handoff.

## Session Execution

Remote sessions use `tmux`. The default logical session name is `main`; the concrete tmux session name is:

```text
yolobox-<machine>-<workspace>-main
```

Interactive commands run through `tmux`:

- if stdin and stdout are terminals, the client attaches with `ssh -t`
- otherwise, the client starts the tmux session detached and tells the user to reattach later

Noninteractive commands run directly over SSH without requiring a terminal. This keeps host-side stdout and stderr usable for command output and redirection.

The client forwards the SSH agent only when the recorded repo URL looks like SSH (`git@...` or `ssh://...`).

## Preview Ports

Remote mode does not expose public ports by default. For local preview access:

```bash
yolobox remote forward foo/app 3000
yolobox remote forward foo/app 3000 --local-port 13000
```

The first command opens `http://127.0.0.1:3000` on the local machine and forwards it to `127.0.0.1:3000` on the remote machine. Press `Ctrl+C` to stop forwarding.

Forwarding records a local exposure entry with:

- `kind = "ssh-forward"`
- `visibility = "local"`
- `target_host = "127.0.0.1"`
- local and remote ports

Public preview URLs such as `*.yolobox.dev`, team-private previews, and public links are reserved for backend or hosted-service work.

## Local Registry

The local registry lives at:

```text
~/.local/state/yolobox/remotes.json
```

It records:

- `machines`: backend leases known to this client
- `workspaces`: local-to-remote project mappings
- `sessions`: known tmux sessions started by this client
- `exposures`: local forwarding records

The registry is local cache and workflow state, not the backend source of truth. `remote status` refreshes what it can from the configured backend. If a local registry entry points at a legacy non-backend provider, most remote commands reject it and tell the user to recreate through a backend; `destroy --force` removes local state for legacy entries.

Registry files are written with normal user-readable config permissions. They should not contain backend tokens.

## Failure Behavior

- Missing `remote.backend_url` fails before any backend request.
- Missing backend token fails before any backend request.
- Missing local `ssh` or `rsync` fails before sync or SSH work.
- Backend non-2xx responses fail the command and surface the response body when present.
- Missing `machine.public_ipv4` in the backend response fails the command.
- SSH readiness waits up to five minutes during bootstrap.
- `sync down` refuses to run without `--force`.
- `destroy --force` removes local registry state only after a backend release succeeds for backend machines.

## Security

Treat a remote machine like another trusted development machine:

- Anyone with SSH access to the remote host can inspect synced files, tmux sessions, running containers, and forwarded preview traffic.
- `sync up` copies project-local secrets if they are in the folder.
- `sync down --force` can overwrite local files.
- Remote backend tokens authorize host leasing and release.
- A self-hosted backend should listen behind TLS or on a private network.
- If backend-provided hosts expose Docker access to agents, dedicate those hosts to one user or workspace because Docker access is effectively host-level access.

## Backend Implementation Boundary

The self-hosted backend should live outside the yolobox client repo unless there is a strong reason to merge it later. A separate backend project should own:

- persistent lease database
- user/team auth
- token issuance and rotation
- pool membership and health checks
- provider adapters
- idle shutdown and cleanup
- billing or quota policy
- audit events
- public preview routing and TLS

A minimal self-hosted backend can start with:

- static SSH host pool
- token auth
- lease table keyed by owner plus machine name
- the three machine endpoints in this spec
- health check endpoint

## Roadmap

Planned client-side improvements:

- better dirty-state warnings before `sync up` and `sync down`
- local port-forward lifecycle listing and cleanup
- explicit secret/config staging for Claude, Codex, Git, and GitHub auth
- remote agent subcommands for workspace management over SSH
- managed workspace containers with per-workspace home, cache, output volumes, and networks

Backend or hosted-service improvements:

- self-hosted backend implementation
- provider provisioners beyond static SSH pools
- managed machines and warm pools
- managed preview URLs with TLS and auth
- team sharing roles for viewer, commenter, controller, and owner
- audit logs for leases, attaches, commands, syncs, and exposures
- snapshots, clone, restore, idle shutdown, budget controls, and policy controls
