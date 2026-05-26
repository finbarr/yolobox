# yolobox Backend

This is the open-source remote control plane. The CLI always talks to a backend;
it does not provision cloud machines locally. The hosted backend can offer a free
account/control-plane layer, let users attach their own cloud credentials, and
gate yolobox-owned VMs behind paid plans. Self-hosters can run this package with
their own provider credentials.

The browser app is built with TanStack Router and TanStack Query. In production
the intended split is `app.yolobox.dev` for the console and `api.yolobox.dev`
for this API. The API also serves the built app from `dist-app` for simple
self-hosted deployments.

## Run Locally

```bash
npm ci
BETTER_AUTH_SECRET=replace-with-a-random-secret-at-least-32-bytes \
DIGITALOCEAN_ACCESS_TOKEN=dop_v1_example \
npm run dev
```

Run the web app during development from a second shell:

```bash
npm run dev:app
```

By default the server listens on `127.0.0.1:8787` and stores state at
`~/.local/state/yolobox/backend.json`. Better Auth users and sessions are stored
in SQLite at `~/.local/state/yolobox/auth.sqlite`.

## Run With Docker Compose

From the repository root:

```bash
BETTER_AUTH_SECRET="$(openssl rand -hex 32)" \
DIGITALOCEAN_ACCESS_TOKEN=dop_v1_example \
docker compose -f docker-compose.backend.yml up --build
```

The Compose service publishes `127.0.0.1:8787` by default and persists backend
state in the `yolobox-backend-data` Docker volume. Override the host bind with
`YOLOBOX_BACKEND_PORT`, for example `YOLOBOX_BACKEND_PORT=127.0.0.1:8877`.
When the public URL changes, also set `BETTER_AUTH_URL`, `YOLOBOX_APP_URL`, and
`YOLOBOX_API_URL` so browser login links point at the reachable host.

## Production DigitalOcean Deployment

The repository includes a production bundle in `deploy/digitalocean/` for the
hosted split:

- `app.yolobox.dev` for the browser console
- `api.yolobox.dev` for API and Better Auth routes
- Caddy-managed TLS in front of the backend container
- host-mounted backend and Caddy data under `/opt/yolobox/data`
- a systemd timer that writes daily archives to `/opt/yolobox/backups`

Enable DigitalOcean Droplet backups for machine-level recovery, then install the
repo backup timer for application state recovery. The backend data backup uses
SQLite's online backup command for `auth.sqlite` and includes `backend.json` plus
Caddy data.

Then sign up or sign in from another shell. The CLI prints a browser URL, tries
to open it, and waits for the web app to grant access:

```bash
yolobox login --backend-url http://127.0.0.1:8787
```

Environment:

- `BETTER_AUTH_SECRET`: required signing secret for Better Auth sessions.
- `BETTER_AUTH_URL`: public auth base URL, default `http://<listen>/v1/auth`.
- `BETTER_AUTH_TRUSTED_ORIGINS`: comma-separated trusted browser origins.
- `YOLOBOX_APP_URL`: public app URL, default derived from `BETTER_AUTH_URL` in direct runs and `http://127.0.0.1:8787` in Compose.
- `YOLOBOX_API_URL`: public API URL, default derived from `BETTER_AUTH_URL` in direct runs and `http://127.0.0.1:8787` in Compose.
- `YOLOBOX_BACKEND_CORS_ORIGINS`: comma-separated browser origins allowed to call the API.
- `YOLOBOX_BACKEND_AUTH_DB`: SQLite auth database path.
- `YOLOBOX_BACKEND_LISTEN`: listen address, default `127.0.0.1:8787`.
- `YOLOBOX_BACKEND_STATE`: JSON state file path.
- `YOLOBOX_BACKEND_PROVIDER`: provider adapter, default `digitalocean`.
- `DIGITALOCEAN_ACCESS_TOKEN`: DigitalOcean token for self-hosted provisioning.
- `DIGITALOCEAN_REGION`: default `nyc3`.
- `DIGITALOCEAN_SIZE`: default `s-2vcpu-4gb`.
- `YOLOBOX_REMOTE_IMAGE`: provider image id or slug for a prebuilt yolobox VM image. When unset, DigitalOcean falls back to `DIGITALOCEAN_IMAGE` and then `ubuntu-24-04-x64`.
- `DIGITALOCEAN_IMAGE`: DigitalOcean image fallback, default `ubuntu-24-04-x64`.
- `DIGITALOCEAN_SSH_KEYS`: comma-separated SSH key ids or fingerprints.
- `DIGITALOCEAN_TAGS`: comma-separated tags, default `yolobox`.
- `DIGITALOCEAN_VPC_UUID`: optional VPC UUID.
- `YOLOBOX_REMOTE_SSH_PUBLIC_KEY`: public key to register when no `DIGITALOCEAN_SSH_KEYS` are set.

## API

Machine endpoints and `GET /v1/auth/whoami` require a Better Auth bearer
session:

```http
Authorization: Bearer <token>
```

Routes:

- `GET /healthz`
- `POST /v1/auth/sign-up/email`
- `POST /v1/auth/sign-in/email`
- `POST /v1/auth/sign-out`
- `POST /v1/auth/device/code`
- `GET /v1/auth/device`
- `POST /v1/auth/device/approve`
- `POST /v1/auth/device/deny`
- `POST /v1/auth/device/token`
- `GET /v1/auth/whoami`
- `GET /v1/providers`
- `POST /v1/machines`
- `POST /v1/machines/ensure`
- `GET /v1/machines`
- `GET /v1/machines/:name`
- `GET /v1/machines/:name/connect`
- `PATCH /v1/machines/:name`
- `DELETE /v1/machines/:name`

Machines are scoped to the authenticated Better Auth user and are one-to-one with
a remote VM. The backend imports provider-owned machines when listing, so the UI
and CLI can see machines already present in the authenticated account. There are
no backend workspaces or multiple named sessions per machine; the CLI uses one
project path and one tmux session on the VM.
