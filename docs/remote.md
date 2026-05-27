# Remote Mode

Remote mode gives Claude, Codex, and other harnesses a named Linux machine that keeps running after your laptop disconnects. The CLI is backend-first: it always asks a hosted or self-hosted backend for the machine lease, then uses SSH and rsync for the developer workflow.

The CLI no longer provisions cloud machines directly and no longer maintains a local remote registry. The backend is the source of truth for machine state.

## Status

Implemented in the CLI:

- default hosted backend URL: `https://api.yolobox.dev`
- `yolobox login` and `yolobox logout` for backend auth
- backend-backed machine lease, status, list, update, and destroy calls
- one named VM per remote name
- one project storage path per VM: `/opt/yolobox/project`
- one source-path workdir alias per VM, matching the original local project path
- one persistent tmux session per VM: `yolobox`
- VM-native agent execution with Docker Engine and Compose on the host
- full-folder `remote sync up` and forced `remote sync down --force`
- backend-generated preview URLs for deployments with a preview base domain
- yolobox VM runtime bootstrap fallback when the backend returns an unprepared machine

Implemented in the open-source backend package under `backend/`:

- TypeScript/Fastify web service
- Better Auth email/password signup, logout, bearer sessions, and browser-approved CLI device login
- SQLite auth database plus JSON state file for leased machines
- per-user machine ownership and isolation
- DigitalOcean provider adapter for self-hosted provisioning
- machine metadata updates from the CLI
- preview hostname generation plus backend preview proxy endpoints
- TanStack browser console for signup, signin, machine list, create, destroy, and CLI grant approval

Not implemented in this repo:

- managed billing, quotas, team roles, and audit logs
- yolobox-owned paid VM pools
- custom preview hostnames
- provider adapters beyond DigitalOcean

## Mental Model

Remote mode has one main concept: a machine. A named remote maps to one VM, one project storage directory, one source-path workdir alias, and one tmux session.

Project bytes live at `/opt/yolobox/project` on the remote VM. The CLI also creates the original local source path on the VM as a symlink to that storage directory, then starts the requested agent command directly on the VM from that source path. That means a local checkout at `/Users/example/project` also appears at `/Users/example/project` on the remote machine, which keeps Codex and Claude session paths stable.

That is intentional. Multiple workspaces and multiple named sessions on one VM replicated fork-mode concepts remotely and made state ownership unclear. If you want another remote environment, create another named remote machine.

`remote run` and `remote connect` never create a second managed session. If the
`yolobox` tmux session already exists, terminal invocations attach to it; a
non-terminal interactive invocation fails clearly instead of silently ignoring
the requested command.

Preview access is backend-owned. When `YOLOBOX_PREVIEW_BASE_DOMAIN` is configured,
each machine receives a stable generated hostname under that domain. Hosted
deployments proxy that hostname to the machine's standard preview service; custom
names are a separate product layer.

## CLI Contract

```bash
yolobox login
yolobox login --backend-url https://remote.example.com
yolobox login --backend-url https://remote.example.com --no-open
yolobox login --backend-url https://remote.example.com --token <existing-session-token>
yolobox logout

yolobox remote create foo
yolobox remote create foo --tier medium
yolobox remote create foo --no-sync
yolobox remote run foo codex
yolobox remote connect foo
yolobox remote sync up foo
yolobox remote sync down foo --force
yolobox remote list
yolobox remote status foo
yolobox remote destroy foo --force
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
# Browser-granted Better Auth session token written by yolobox login. Prefer YOLOBOX_TOKEN in scripts.
token = "your-session-token"
ssh_user = "root"
setup = [
  "docker compose pull",
  "docker compose up -d db redis"
]
```

With this config, bare `yolobox` behaves like:

```bash
yolobox remote run foo codex
```

`YOLOBOX_BACKEND_URL` overrides `remote.backend_url`. `YOLOBOX_TOKEN` overrides `remote.token`.

Plain `yolobox login` uses Better Auth's device authorization flow: the CLI
creates a short-lived login request, prints the verification URL, tries to open
it in your browser, and polls until the web app grants or denies CLI access.
The CLI always prints the URL so SSH and headless sessions can copy/paste it;
`--no-open` skips the automatic browser attempt. `--token` stores an existing
backend session token for scripts and local testing.

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
yolobox login --backend-url http://127.0.0.1:8787
yolobox remote run foo codex
```

Or run the backend with Docker Compose from the repository root:

```bash
BETTER_AUTH_SECRET="$(openssl rand -hex 32)" \
DIGITALOCEAN_ACCESS_TOKEN=dop_v1_example \
docker compose -f docker-compose.backend.yml up --build
```

The Compose service publishes `127.0.0.1:8787` and stores auth plus machine
state in the `yolobox-backend-data` Docker volume.
When you publish it on a different host or port, set `BETTER_AUTH_URL`,
`YOLOBOX_APP_URL`, and `YOLOBOX_API_URL` to the reachable public URLs so
browser login links are correct.

For the hosted production split, use `deploy/digitalocean/`. It runs the API and
frontend on one Droplet behind Caddy, publishes `app.yolobox.dev`,
`api.yolobox.dev`, and wildcard preview hosts such as
`*.hosted.yolobox.dev`, stores backend state in `/opt/yolobox/data/backend`, and
installs a daily backup timer under systemd. Pair that with DigitalOcean Droplet
backups so both the machine and app state have recovery coverage.

The backend stores Better Auth users and sessions in SQLite at `~/.local/state/yolobox/auth.sqlite` by default. Override that with `YOLOBOX_BACKEND_AUTH_DB`. `BETTER_AUTH_URL` should point at the auth base URL, for example `https://api.example.com/v1/auth`, when running behind a public hostname.

The browser console is built into the backend package with TanStack Router and TanStack Query. The hosted split is `https://app.yolobox.dev` for the app and `https://api.yolobox.dev` for the API. For self-hosting, set `YOLOBOX_APP_URL`, `YOLOBOX_API_URL`, `BETTER_AUTH_TRUSTED_ORIGINS`, and `YOLOBOX_BACKEND_CORS_ORIGINS` to match the public hostnames. Set `YOLOBOX_PREVIEW_BASE_DOMAIN` when the deployment has wildcard DNS for generated preview hosts, and `YOLOBOX_PREVIEW_TARGET_PORT` when machines should receive preview traffic on a port other than `80`.

The backend reads provider settings from environment variables. The current provider adapter is DigitalOcean, configured with `DIGITALOCEAN_REGION`, `DIGITALOCEAN_SIZE`, `YOLOBOX_REMOTE_IMAGE`, `DIGITALOCEAN_IMAGE`, `DIGITALOCEAN_SSH_KEYS`, `DIGITALOCEAN_TAGS`, and `DIGITALOCEAN_VPC_UUID`. `DIGITALOCEAN_SIZE` is the default size for creates without an explicit tier. Create-time tiers map to DigitalOcean AMD sizes: `small` uses 2 vCPU / 4 GB, `medium` uses 4 vCPU / 8 GB, and `large` uses 8 vCPU / 16 GB. Set `YOLOBOX_REMOTE_IMAGE` to a prebuilt yolobox VM image or provider snapshot id so new machines start with the remote runtime already installed. When it is unset, the DigitalOcean adapter falls back to `DIGITALOCEAN_IMAGE` and then `ubuntu-24-04-x64`; the CLI can prepare that plain host over SSH. The backend provider interface owns create, destroy, list/import, and connect metadata so other platforms can be added without changing the CLI protocol.

## Client Responsibilities

After the backend leases a host, the CLI:

- sends the machine name, requested size tier, preferred SSH user, local source path, repo URL, and branch to the backend
- waits for SSH when the backend returns an unbootstrapped host
- prepares the VM-native yolobox runtime when the image does not already contain it
- mirrors the local folder to `/opt/yolobox/project`
- maps the original local source path to that storage directory on the VM
- runs setup commands from the source-path workdir after upward sync
- starts or connects to the single `yolobox` tmux session for interactive VM-native commands
- runs noninteractive commands directly over SSH
- exposes `YOLOBOX_PREVIEW_URL` and `YOLOBOX_PREVIEW_HOSTNAME` inside remote sessions when the backend returned them
- patches backend machine metadata after bootstrap, sync, and command execution

Remote create, run, connect, status, and destroy require local `ssh` when
they need to reach a machine. Commands that copy project files also require
local `rsync`.

The CLI does not store remote machine state locally. It stores auth/config only,
then asks the backend for list, status, create, destroy, and connect metadata.
`yolobox remote create foo` creates one machine, prepares the VM runtime, and
syncs the current folder by default. Pass `--no-sync` to skip the initial copy,
or `--tier small`, `--tier medium`, or `--tier large` to choose the VM size.
Create fails when the name already exists; use `remote run`, `remote connect`,
or `remote status` for existing machines. `yolobox remote run foo ...` syncs
the folder and then runs the command on an existing machine. Remote commands
print progress while backend provisioning and SSH startup are pending; when a
machine is ready, any generated preview URL is shown on its own line.
`yolobox remote connect foo` prepares a backend-known machine and opens a shell
without syncing the local folder. If a machine has no stored source path yet,
connect records the current folder path and uses it for the remote workdir alias.

## Backend HTTP API

Auth endpoints are mounted under `/v1/auth/*` and are handled by Better Auth. The CLI uses:

- `POST /v1/auth/device/code`
- `POST /v1/auth/device/token`
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

### `GET /v1/providers`

List configured provider adapters and their capabilities.

### `POST /v1/machines`

Create the host for a logical machine name. The backend returns `409` when that authenticated owner already has a machine with the same name. Different users can use the same logical machine name; the backend derives a provider-specific machine name per user.

Request:

```json
{
  "name": "foo",
  "tier": "medium",
  "ssh_user": "root",
  "source_path": "/Users/example/project",
  "repo_url": "git@github.com:example/project.git",
  "branch": "feature/remote-work"
}
```

Successful `201` response:

```json
{
  "machine": {
    "name": "foo",
    "provider": "digitalocean",
    "provider_id": "123456",
    "public_ipv4": "203.0.113.10",
    "ssh_user": "root",
    "preview_hostname": "amber-bridge-a1b2c3.hosted.yolobox.dev",
    "preview_url": "https://amber-bridge-a1b2c3.hosted.yolobox.dev",
    "source_path": "/Users/example/project",
    "project_path": "/opt/yolobox/project",
    "bootstrap_complete": false
  },
  "status": "created"
}
```

### `GET /v1/machines`

List the authenticated user's machines. The backend also asks the configured
provider for account machines and imports matching resources before responding,
so a fresh CLI or UI session can see machines that already exist in the account.

### `GET /v1/machines/{name}`

Return and refresh one leased machine.

### `GET /v1/machines/{name}/connect`

Return refreshed machine state plus SSH and CLI connect commands for the UI.

### `PATCH /v1/machines/{name}`

Accept CLI-owned metadata updates such as source path, repo URL, branch, last command, sync timestamp, project storage path, and bootstrap status.

### `DELETE /v1/machines/{name}`

Release the backend lease and delete backend state for the machine.

### `GET /v1/preview/tls-check?domain={hostname}`

Unauthenticated Caddy on-demand TLS allow-list endpoint. It returns success only
when the requested hostname matches a generated preview hostname in backend
state.

### `ANY /v1/preview/proxy/{hostname}/{path}`

Unauthenticated preview proxy endpoint used by the production Caddy wildcard
route. It validates that `{hostname}` belongs to a leased machine and forwards
the request to that machine on `YOLOBOX_PREVIEW_TARGET_PORT`.

## Sync Semantics

`sync up` mirrors the current local folder to the remote machine:

```bash
rsync -az --delete --human-readable ./ root@host:/opt/yolobox/project/
```

This includes `.git` if present, untracked files, ignored files, `.env` files, dependency folders, build output, and local caches. Treat the remote as a trusted development machine.

The remote storage path remains `/opt/yolobox/project`, but commands run from a source-path alias. For example, syncing from `/Users/example/project` creates `/Users/example/project` on the VM as a symlink to `/opt/yolobox/project`, so Codex, Claude, shells, and Docker Compose work from `/Users/example/project`.

## VM Runtime

Remote mode does not run a nested yolobox container on the VM. A yolobox remote machine is the sandbox: it runs the requested command directly on the VM, with `/opt/yolobox/bin` wrappers first on `PATH`, Docker Engine available on the host, and persistent installs written to the VM disk.

Prebuilt provider images should run the embedded VM installer at image-build time. The installer lives at `cmd/yolobox/assets/remote-vm-install.sh` and writes `/opt/yolobox/remote/ready` when the runtime is ready. It installs the AI CLIs, Docker Engine and Compose, `tmux`, `rsync`, common build tools, the YOLO wrappers, GitHub HTTPS token helper, and `/usr/local/bin/yolobox-remote-session`.

When building an image from this repository checkout, run:

```bash
sudo env YOLOBOX_SOURCE_DIR="$PWD" ./cmd/yolobox/assets/remote-vm-install.sh
```

For the hosted DigitalOcean deployment, use the image builder:

```bash
deploy/digitalocean/build-remote-image.sh \
  --env-file deploy/digitalocean/.env.production \
  --set-active
```

The builder creates a temporary Droplet, installs the remote VM runtime from the
committed checkout, cleans cloud-init and SSH host identity so cloned machines
receive fresh instance metadata, powers the Droplet off, snapshots it, deletes
the builder, and writes the new snapshot id to `YOLOBOX_REMOTE_IMAGE` when
`--set-active` is passed. For release-grade images, build from a pushed tag or
commit:

```bash
deploy/digitalocean/build-remote-image.sh \
  --env-file deploy/digitalocean/.env.production \
  --ref v0.19.0 \
  --set-active
```

After changing `YOLOBOX_REMOTE_IMAGE`, restart the backend so future remote
creates use the new snapshot. Keep the previous snapshot id until a smoke create
passes; rollback is setting `YOLOBOX_REMOTE_IMAGE` back to that id and
recreating the backend container.

The CLI still sends the installer over SSH when a backend returns a plain or older host. If `/opt/yolobox/remote/ready` already exists, the installer exits immediately; otherwise it upgrades the host in place before syncing or connecting. Installer command output is written on the VM to `/var/log/yolobox-remote-install.log`; the CLI prints only high-level setup steps unless installation fails.

`sync down` copies the remote project back into the current local folder and requires `--force`:

```bash
yolobox remote sync down foo --force
```

Use `sync down` only when the remote copy should overwrite local files. For Git projects, committing changes in the remote session and pushing a branch is usually a cleaner handoff.

## Security

Remote machines are outside the local container trust boundary. Anyone with SSH access can inspect synced files, running containers, tmux contents, preview traffic, and any secrets you place there.

Backend session tokens authorize machine leasing, release, and metadata updates for one Better Auth user. Treat `remote.token` and `YOLOBOX_TOKEN` like cloud credentials. A self-hosted backend should listen behind TLS or on a private network, and `BETTER_AUTH_SECRET` must be a long random secret.

DigitalOcean credentials now belong to the backend, not the CLI. Hosted bring-your-own-infra flows should store provider credentials server-side and scope them to the smallest permissions practical.
