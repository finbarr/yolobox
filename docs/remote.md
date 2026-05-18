# Remote Mode

Remote mode gives yolobox a machine that keeps running after your laptop disconnects. It is designed for Claude, Codex, and other terminal agents that benefit from real Linux compute, persistent sessions, and preview ports without turning yolobox into a hosted IDE.

This page is the client, backend, and provider contract. The yolobox CLI normally should not care whether a machine came from a hosted yolobox service, a self-hosted pool, Hetzner, AWS, bare metal, or anything else. For local experimentation, the CLI can also use the same provider adapter directly when no backend is configured.

## Status

Implemented in the client:

- backend machine leasing over HTTP
- direct local machine provisioning through provider adapters
- DigitalOcean provider adapter
- named machines and named workspaces
- persistent `tmux` sessions
- full-folder sync up and forced sync down
- local SSH preview forwarding
- local registry state for machines, workspaces, sessions, and exposures
- host bootstrap when the backend or provider returns an unprepared machine

Implemented in the backend service:

- `yolobox remote backend serve`
- bearer-token API auth
- shared machine lease state in a JSON state file
- shared session metadata in the backend state file
- the same provider adapter interface used by the direct client path

Not implemented in this repo:

- hosted backend service
- multi-user identity, team roles, billing, quotas, and audit logs
- provider adapters beyond DigitalOcean
- public preview URLs
- managed workspace containers with per-workspace volumes
- secret/config staging for Claude, Codex, Git, or GitHub auth

## Goals

- Make `yolobox remote --name foo codex` feel like the normal local yolobox workflow, but on durable Linux compute.
- Keep the sync and execution path provider-agnostic. The client leases SSH hosts from either a backend or a local provider adapter, then handles sync, attach, and forwarding.
- Support both hosted and self-hosted backends with the same client contract.
- Let users try remote mode without running a backend by using the direct DigitalOcean adapter.
- Keep remote access explicit. Starting a remote command must not imply public port exposure.
- Preserve local escape hatches: users can inspect local registry state, sync back with `--force`, and destroy a lease.

## Non-goals

- The yolobox client does not use provider-specific CLIs such as `doctl`.
- The built-in backend is not the hosted yolobox product. It is a deployable control-plane baseline.
- The open-source client does not create public preview URLs.
- The current client does not yet run a long-lived managed workspace container on the remote host.

## Mental Model

Remote support has four separate concepts:

- **Machine:** the remote compute target returned by the backend or direct provider.
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
yolobox remote backend serve --provider digitalocean --token "$YOLOBOX_BACKEND_TOKEN"
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

`backend_url` is required for backend-backed remote mode unless `--backend-url` is passed. `backend_token` may be omitted when `YOLOBOX_REMOTE_TOKEN` is set.

For direct local provisioning without a backend:

```toml
mode = "remote"
remote_name = "foo"
remote_workspace = "app"
default_harness = "codex"

[remote]
provider = "digitalocean"
ssh_user = "root"

[remote.digitalocean]
# Prefer DIGITALOCEAN_TOKEN instead of committing this.
token = "dop_v1_example"
region = "nyc3"
size = "s-2vcpu-4gb"
image = "ubuntu-24-04-x64"
# Optional. If omitted, yolobox uploads your default public SSH key.
ssh_keys = ["123456", "aa:bb:cc:fingerprint"]
tags = ["yolobox"]
```

The equivalent one-off command is:

```bash
DIGITALOCEAN_TOKEN=... yolobox remote --provider digitalocean --name foo codex
```

`ssh_user` defaults to `root` and is used when the backend response does not include `ssh_user`.

`remote.setup` commands run after `sync up` finishes. They run from the remote workspace project directory with `set -euo pipefail`.

With this config, bare `yolobox` behaves like:

```bash
yolobox remote resume foo/app codex
```

## Backend Responsibilities

The backend is the control plane. It owns allocation and release of hosts. It may be hosted or self-hosted. It may lease from a static pool, warm pool, cloud provider, on-prem cluster, or any other substrate. The built-in server starts with one provider at a time:

```bash
YOLOBOX_BACKEND_TOKEN=change-me \
DIGITALOCEAN_TOKEN=dop_v1_example \
yolobox remote backend serve --provider digitalocean --listen 0.0.0.0:8787
```

Clients then point at it:

```toml
[remote]
backend_url = "https://remote.example.com"
backend_token = "change-me"
```

The backend must:

- authenticate every non-health request
- lease one host for a logical machine name
- make `POST /v1/machines/ensure` idempotent for the same name
- return an SSH-reachable host address in `machine.public_ipv4`
- return `ssh_user` when the host should not be reached as `root`
- release or mark the lease idle on `DELETE /v1/machines/{name}`
- keep provider-specific details behind the API
- accept shared session metadata from clients so teammates can inspect active sessions

The backend may:

- return prewarmed hosts with `bootstrap_complete = true`
- return fresh hosts with `bootstrap_complete = false` or omitted
- include informational metadata such as `provider_id`, `region`, `size`, and `image`
- implement billing, policy, idle shutdown, snapshots, or audit logs without changing the client workflow

## Client Responsibilities

The yolobox client owns the developer workflow after a host is leased. It:

- sends the desired machine name, workspace name, repo URL, and branch to the backend or provider
- waits for SSH when the backend or provider does not mark the host bootstrapped
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
  "ssh_user": "root",
  "repo_url": "git@github.com:example/project.git",
  "branch": "feature/remote-work"
}
```

Fields:

- `name` is required and is the logical machine name.
- `workspace` is optional and defaults to `default` in the client.
- `ssh_user` is optional and lets the client send its preferred SSH user.
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

### `GET /v1/machines`

Return the machine leases known to the backend state file.

Successful response:

```json
{
  "machines": [
    {
      "name": "foo",
      "public_ipv4": "203.0.113.10",
      "provider_id": "host-a"
    }
  ]
}
```

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

### `GET /v1/sessions`

Return shared session metadata known to the backend. `?machine=foo` filters to one machine.

Successful response:

```json
{
  "sessions": [
    {
      "id": "foo-app-main",
      "name": "main",
      "machine": "foo",
      "workspace": "foo-app",
      "tmux_session": "yolobox-foo-app-main",
      "last_command": ["codex"],
      "updated_at": "2026-05-18T17:10:00Z"
    }
  ]
}
```

### `PUT /v1/sessions/{id}`

Create or update shared session metadata. Clients call this after starting or attaching to a persistent remote session.

### `DELETE /v1/sessions/{id}`

Remove shared session metadata. Clients call this after `remote stop`.

## Provider Adapters

Provider adapters implement host leasing, status refresh, and release. The direct client path and the backend service use the same adapter interface.

The DigitalOcean adapter:

- reads credentials from `remote.digitalocean.token`, `DIGITALOCEAN_TOKEN`, or `DO_API_TOKEN`
- creates Droplets through the DigitalOcean API, not `doctl`
- uses `remote.digitalocean.region`, `size`, `image`, `ssh_keys`, `tags`, and `vpc_uuid`
- tags created Droplets with `yolobox` and `yolobox-machine-<name>`
- reuses a matching tagged Droplet for idempotent `ensure`
- uploads the user's default public SSH key when no `ssh_keys` are configured
- deletes the Droplet on release

## Machine Lifecycle

1. User runs `yolobox remote --name foo --workspace app codex`.
2. Client loads config and validates either backend settings or direct provider settings.
3. Client checks for an existing local registry machine.
4. If none exists and `backend_url` is configured, client calls `POST /v1/machines/ensure`.
5. If none exists and `provider = "digitalocean"` is configured, client provisions through the DigitalOcean adapter.
6. Backend or provider leases or returns an SSH host.
7. Client saves the machine in local registry.
8. If `bootstrap_complete` is false, client waits up to five minutes for SSH and bootstraps the host.
9. Client ensures the workspace path exists.
10. Client syncs the local project folder to the remote workspace.
11. Client runs any configured setup commands.
12. Client starts or attaches to the requested command/session and publishes session metadata to the backend when one is configured.

Later runs reuse the local machine and workspace registry entries. `remote status` can refresh backend or provider state. `remote destroy --force` releases the backend lease or direct provider machine and removes local machine, workspace, session, and exposure records for that machine.

## Host Bootstrap

When a backend or provider returns `bootstrap_complete = false` or omits it, the client assumes an Ubuntu-like host reachable over SSH with enough privilege for `apt-get` and Docker installation.

The bootstrap script:

- waits for `cloud-init` when present
- installs `ca-certificates`, `curl`, `git`, `rsync`, and `tmux`
- installs Docker through `get.docker.com` when Docker is missing
- installs yolobox through the repository install script when yolobox is missing
- pulls `ghcr.io/finbarr/yolobox:latest` best-effort
- creates `/opt/yolobox-workspaces`

Backends or providers that do not want client-side bootstrap should return hosts that already have these pieces and set `bootstrap_complete = true`.

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

- `machines`: backend leases or direct-provider machines known to this client
- `workspaces`: local-to-remote project mappings
- `sessions`: known tmux sessions started by this client
- `exposures`: local forwarding records

The registry is local cache and workflow state, not the backend source of truth. `remote status` refreshes what it can from the configured backend or provider.

Registry files are written with normal user-readable config permissions. They should not contain backend tokens.

## Failure Behavior

- Missing both `remote.backend_url` and `remote.provider` fails before machine creation.
- Missing backend token fails before any backend request.
- Missing DigitalOcean token fails before direct DigitalOcean provisioning.
- Missing local `ssh` or `rsync` fails before sync or SSH work.
- Backend non-2xx responses fail the command and surface the response body when present.
- Missing `machine.public_ipv4` in the backend response fails the command.
- Missing `machine.public_ipv4` in a provider response fails the command.
- SSH readiness waits up to five minutes during bootstrap.
- `sync down` refuses to run without `--force`.
- `destroy --force` removes local registry state only after the backend or direct provider release succeeds.

## Security

Treat a remote machine like another trusted development machine:

- Anyone with SSH access to the remote host can inspect synced files, tmux sessions, running containers, and forwarded preview traffic.
- `sync up` copies project-local secrets if they are in the folder.
- `sync down --force` can overwrite local files.
- Remote backend tokens authorize host leasing, release, and session metadata updates.
- DigitalOcean tokens authorize Droplet and SSH key management.
- A self-hosted backend should listen behind TLS or on a private network.
- If backend-provided hosts expose Docker access to agents, dedicate those hosts to one user or workspace because Docker access is effectively host-level access.

## Backend Implementation Boundary

The built-in backend is a deployable baseline for one shared provider-backed machine pool. It stores state in a JSON file and is suitable for early self-hosted testing behind TLS or a private network.

A production hosted service should still grow into a dedicated backend project that owns:

- persistent lease database
- user/team auth
- token issuance and rotation
- pool membership and health checks
- provider adapters beyond the shared built-in interface
- idle shutdown and cleanup
- billing or quota policy
- audit events
- public preview routing and TLS

The built-in backend starts with:

- token auth
- a JSON lease and session state file
- one configured provider adapter at a time
- the machine and session endpoints in this spec
- health check endpoint

## Roadmap

Planned client-side improvements:

- better dirty-state warnings before `sync up` and `sync down`
- local port-forward lifecycle listing and cleanup
- explicit secret/config staging for Claude, Codex, Git, and GitHub auth
- remote agent subcommands for workspace management over SSH
- managed workspace containers with per-workspace home, cache, output volumes, and networks

Backend or hosted-service improvements:

- durable database storage for the self-hosted backend
- provider adapters beyond DigitalOcean
- managed machines and warm pools
- managed preview URLs with TLS and auth
- team sharing roles for viewer, commenter, controller, and owner
- audit logs for leases, attaches, commands, syncs, and exposures
- snapshots, clone, restore, idle shutdown, budget controls, and policy controls
