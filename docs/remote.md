# Remote Mode

Remote mode gives Claude, Codex, and other harnesses a named Linux machine that keeps running after your laptop disconnects. The CLI is backend-first: it always asks a hosted or self-hosted backend for the machine lease, then uses SSH and rsync for the developer workflow.

The CLI no longer provisions cloud machines directly and no longer maintains a local remote registry. The backend is the source of truth for machine state.

## Status

Implemented in the CLI:

- default hosted backend URL: `https://api.yolobox.dev`
- `yolobox login` and `yolobox logout` for backend auth
- backend-backed machine lease, status, list, update, and destroy calls
- one named VM per remote name
- one project path per VM: `/opt/yolobox/project`
- one persistent tmux session per VM: `yolobox`
- full-folder `remote sync up` and forced `remote sync down --force`
- local SSH preview forwarding with `remote forward`
- host bootstrap when the backend returns an unprepared machine

Implemented in the open-source backend package under `backend/`:

- TypeScript/Fastify web service
- Better Auth email/password signup, login, logout, and bearer sessions
- SQLite auth database plus JSON state file for leased machines
- per-user machine ownership and isolation
- DigitalOcean provider adapter for self-hosted provisioning
- machine metadata updates from the CLI

Not implemented in this repo:

- the hosted account UI
- managed billing, quotas, team roles, and audit logs
- yolobox-owned paid VM pools
- public preview URLs
- provider adapters beyond DigitalOcean

## Mental Model

Remote mode has one main concept: a machine. A named remote maps to one VM, one project directory, and one tmux session.

That is intentional. Multiple workspaces and multiple named sessions on one VM replicated fork-mode concepts remotely and made state ownership unclear. If you want another remote environment, create another named remote machine.

Port access remains explicit. The open-source CLI supports local SSH forwarding; public preview URLs belong behind a hosted backend.

## CLI Contract

```bash
yolobox login --email you@example.com
yolobox login --signup --email you@example.com
yolobox login --backend-url https://remote.example.com --email you@example.com
yolobox login --backend-url https://remote.example.com --token <existing-session-token>
yolobox logout

yolobox remote --name foo codex
yolobox remote resume foo codex
yolobox remote attach foo codex
yolobox remote sync up foo
yolobox remote sync down foo --force
yolobox remote forward foo 3000
yolobox remote forward foo 3000 --local-port 13000
yolobox remote stop foo
yolobox remote list
yolobox remote status foo
yolobox remote destroy foo --force
```

If `remote_name` is configured, commands that take a remote name can omit it:

```bash
yolobox remote forward 3000
```

Names are lowercased and must use lowercase letters, numbers, and hyphens. They must start with a letter or number and are capped at 63 characters.

## Configuration

Remote defaults live in normal yolobox config:

```toml
mode = "remote"
remote_name = "foo"
default_harness = "codex"

[remote]
# Defaults to https://api.yolobox.dev when omitted.
backend_url = "https://remote.example.com"
# Better Auth session token written by yolobox login. Prefer YOLOBOX_TOKEN in scripts.
token = "your-session-token"
ssh_user = "root"
setup = [
  "docker compose pull",
  "docker compose up -d db redis"
]
```

With this config, bare `yolobox` behaves like:

```bash
yolobox remote --name foo codex
```

`YOLOBOX_BACKEND_URL` overrides `remote.backend_url`. `YOLOBOX_TOKEN` overrides `remote.token`.

## Hosted And Self-Hosted Backends

The CLI defaults to the hosted backend URL. That lets the product offer a free account/control-plane layer, a bring-your-own-infra path where users connect their own provider credentials, and paid yolobox-owned VMs when users want managed compute.

The backend remains open source. Self-hosters run the TypeScript backend with their own provider credentials:

```bash
cd backend
npm ci
BETTER_AUTH_SECRET=replace-with-a-random-secret-at-least-32-bytes \
DIGITALOCEAN_ACCESS_TOKEN=dop_v1_example \
npm run dev
```

Then point the CLI at it:

```bash
yolobox login --signup --backend-url http://127.0.0.1:8787 --email you@example.com
yolobox remote --name foo codex
```

Or run the backend with Docker Compose from the repository root:

```bash
BETTER_AUTH_SECRET="$(openssl rand -hex 32)" \
DIGITALOCEAN_ACCESS_TOKEN=dop_v1_example \
docker compose -f docker-compose.backend.yml up --build
```

The Compose service publishes `127.0.0.1:8787` and stores auth plus machine state
in the `yolobox-backend-data` Docker volume.

The backend stores Better Auth users and sessions in SQLite at `~/.local/state/yolobox/auth.sqlite` by default. Override that with `YOLOBOX_BACKEND_AUTH_DB`. `BETTER_AUTH_URL` should point at the auth base URL, for example `https://api.example.com/v1/auth`, when running behind a public hostname.

The backend reads DigitalOcean settings from environment variables such as `DIGITALOCEAN_REGION`, `DIGITALOCEAN_SIZE`, `DIGITALOCEAN_IMAGE`, `DIGITALOCEAN_SSH_KEYS`, `DIGITALOCEAN_TAGS`, and `DIGITALOCEAN_VPC_UUID`.

## Client Responsibilities

After the backend leases a host, the CLI:

- sends the machine name, preferred SSH user, local source path, repo URL, and branch to the backend
- waits for SSH when the backend returns an unbootstrapped host
- bootstraps Docker, `tmux`, `git`, `rsync`, and yolobox when needed
- mirrors the local folder to `/opt/yolobox/project`
- runs setup commands after upward sync
- starts or attaches to the single `yolobox` tmux session for interactive commands
- runs noninteractive commands directly over SSH
- forwards local preview ports with SSH
- patches backend machine metadata after bootstrap, sync, and command execution

The client requires local `ssh` and `rsync`.

## Backend HTTP API

Auth endpoints are mounted under `/v1/auth/*` and are handled by Better Auth. The CLI uses:

- `POST /v1/auth/sign-up/email`
- `POST /v1/auth/sign-in/email`
- `POST /v1/auth/sign-out`

Machine endpoints require a Better Auth bearer session:

```http
Authorization: Bearer <token>
Content-Type: application/json
```

The client times out backend requests after 30 seconds. Any non-2xx response is treated as an error and the response body is surfaced when present.

### `GET /healthz`

Health check endpoint for operators and load balancers.

### `GET /v1/auth/whoami`

Returns basic authenticated account/backend information for the current Better Auth session.

### `POST /v1/machines/ensure`

Lease or return the host for a logical machine name. This operation is idempotent for the same authenticated owner and machine name. Different users can use the same logical machine name; the backend derives a provider-specific machine name per user.

Request:

```json
{
  "name": "foo",
  "ssh_user": "root",
  "source_path": "/Users/example/project",
  "repo_url": "git@github.com:example/project.git",
  "branch": "feature/remote-work"
}
```

Successful response:

```json
{
  "machine": {
    "name": "foo",
    "provider": "digitalocean",
    "provider_id": "123456",
    "public_ipv4": "203.0.113.10",
    "ssh_user": "root",
    "project_path": "/opt/yolobox/project",
    "bootstrap_complete": false
  },
  "status": "leased"
}
```

### `GET /v1/machines`

List the authenticated user's leased machines.

### `GET /v1/machines/{name}`

Return and refresh one leased machine.

### `PATCH /v1/machines/{name}`

Accept CLI-owned metadata updates such as source path, repo URL, branch, last command, sync timestamp, project path, and bootstrap status.

### `DELETE /v1/machines/{name}`

Release the backend lease and delete backend state for the machine.

## Sync Semantics

`sync up` mirrors the current local folder to the remote machine:

```bash
rsync -az --delete --human-readable --info=stats1 ./ root@host:/opt/yolobox/project/
```

This includes `.git` if present, untracked files, ignored files, `.env` files, dependency folders, build output, and local caches. Treat the remote as a trusted development machine.

`sync down` copies the remote project back into the current local folder and requires `--force`:

```bash
yolobox remote sync down foo --force
```

Use `sync down` only when the remote copy should overwrite local files. For Git projects, committing changes in the remote session and pushing a branch is usually a cleaner handoff.

## Security

Remote machines are outside the local container trust boundary. Anyone with SSH access can inspect synced files, running containers, tmux contents, forwarded preview traffic, and any secrets you place there.

Backend session tokens authorize machine leasing, release, and metadata updates for one Better Auth user. Treat `remote.token` and `YOLOBOX_TOKEN` like cloud credentials. A self-hosted backend should listen behind TLS or on a private network, and `BETTER_AUTH_SECRET` must be a long random secret.

DigitalOcean credentials now belong to the backend, not the CLI. Hosted bring-your-own-infra flows should store provider credentials server-side and scope them to the smallest permissions practical.
