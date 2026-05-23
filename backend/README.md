# yolobox Backend

This is the open-source remote control plane. The CLI always talks to a backend;
it does not provision cloud machines locally. The hosted backend can offer a free
account/control-plane layer, let users attach their own cloud credentials, and
gate yolobox-owned VMs behind paid plans. Self-hosters can run this package with
their own provider credentials.

## Run Locally

```bash
npm ci
YOLOBOX_BACKEND_TOKEN=change-me \
DIGITALOCEAN_ACCESS_TOKEN=dop_v1_example \
npm run dev
```

By default the server listens on `127.0.0.1:8787` and stores state at
`~/.local/state/yolobox/backend.json`.

Environment:

- `YOLOBOX_BACKEND_TOKEN`: bearer token accepted by the API.
- `YOLOBOX_BACKEND_LISTEN`: listen address, default `127.0.0.1:8787`.
- `YOLOBOX_BACKEND_STATE`: JSON state file path.
- `YOLOBOX_BACKEND_PROVIDER`: provider adapter, default `digitalocean`.
- `DIGITALOCEAN_ACCESS_TOKEN`: DigitalOcean token for self-hosted provisioning.
- `DIGITALOCEAN_REGION`: default `nyc3`.
- `DIGITALOCEAN_SIZE`: default `s-2vcpu-4gb`.
- `DIGITALOCEAN_IMAGE`: default `ubuntu-24-04-x64`.
- `DIGITALOCEAN_SSH_KEYS`: comma-separated SSH key ids or fingerprints.
- `DIGITALOCEAN_TAGS`: comma-separated tags, default `yolobox`.
- `DIGITALOCEAN_VPC_UUID`: optional VPC UUID.

## API

All non-health requests require:

```http
Authorization: Bearer <token>
```

Routes:

- `GET /healthz`
- `GET /v1/auth/whoami`
- `POST /v1/machines/ensure`
- `GET /v1/machines`
- `GET /v1/machines/:name`
- `PATCH /v1/machines/:name`
- `DELETE /v1/machines/:name`

Machines are one-to-one with a remote VM. There are no backend workspaces or
multiple named sessions per machine; the CLI uses one project path and one tmux
session on the VM.
