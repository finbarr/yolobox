# DigitalOcean deployment

This bundle runs the yolobox remote console and API on a single DigitalOcean
Droplet. Caddy terminates TLS for `app.yolobox.dev`, `api.yolobox.dev`, and
generated preview hostnames under `hosted.yolobox.dev`, then proxies those
hostnames to the backend container. Backend state is stored on the host under
`/opt/yolobox/data/backend` so it can be backed up outside Docker.

## Provision

Create an Ubuntu 24.04 Droplet with `cloud-init.yml`. Enable DigitalOcean
Droplet backups for machine-level restore coverage.

Required DNS records:

```text
app.yolobox.dev.  A  <droplet-ip>
api.yolobox.dev.  A  <droplet-ip>
*.hosted.yolobox.dev.  A  <droplet-ip>
```

## Configure

Copy `env.production.example` to `.env.production` on the server and fill in the
secret values:

```bash
cp deploy/digitalocean/env.production.example deploy/digitalocean/.env.production
```

`BETTER_AUTH_SECRET` must be a long random value. The backend also needs
`DIGITALOCEAN_ACCESS_TOKEN` so it can create remote VMs for users. Normal user
VMs do not receive reusable DigitalOcean account SSH keys; the backend uses a
one-time no-login key during create only to prevent provider password emails,
then deletes the account key. Cloud-init configures VMs to trust short-lived
yolobox SSH certificates instead. The backend SSH user CA is stored at
`/opt/yolobox/data/backend/ssh_ca_ed25519` and is included in the backend data
backups.
Set `YOLOBOX_PREVIEW_BASE_DOMAIN` to the wildcard domain above. The default
preview target is port `80` on each remote machine; change
`YOLOBOX_PREVIEW_TARGET_PORT` if the machine runtime should receive preview
traffic somewhere else.

## Deploy

From the repository root on the Droplet:

```bash
docker compose --env-file deploy/digitalocean/.env.production \
  -f deploy/digitalocean/docker-compose.yml up -d --build
```

Install and start the backup timer:

```bash
sudo deploy/digitalocean/install-backups.sh
```

## Build the Remote VM Image

New remote machines should come from a prebuilt yolobox snapshot instead of a
plain Ubuntu image. The image builder creates a temporary Droplet, installs the
remote VM runtime, cleans cloud-init and SSH host identity, powers it off,
snapshots it, deletes the builder Droplet, and prints the snapshot id.

From a clean, committed checkout:

```bash
deploy/digitalocean/build-remote-image.sh \
  --env-file deploy/digitalocean/.env.production \
  --set-active
```

The builder Droplet still needs an SSH key for the temporary image build. Use
`DIGITALOCEAN_SSH_KEYS` only when the matching private key is available to the
host running the script. Otherwise set `YOLOBOX_IMAGE_BUILDER_SSH_PUBLIC_KEY`
and pass the matching private key with `--ssh-key`. Quote
`YOLOBOX_IMAGE_BUILDER_SSH_PUBLIC_KEY` in `.env.production` because OpenSSH
public keys contain spaces.

`--set-active` writes `YOLOBOX_REMOTE_IMAGE=<snapshot-id>` plus metadata back to
the env file. Restart the backend after that so future creates use the snapshot:

```bash
docker compose --env-file deploy/digitalocean/.env.production \
  -f deploy/digitalocean/docker-compose.yml up -d --build --force-recreate backend
```

For a release-grade image, build from a pushed tag or commit:

```bash
deploy/digitalocean/build-remote-image.sh \
  --env-file deploy/digitalocean/.env.production \
  --ref v0.19.0 \
  --set-active
```

When running the builder on the production Droplet, use SSH agent forwarding if
the matching private key lives on your laptop:

```bash
ssh -A root@<control-plane-ip> \
  'cd /opt/yolobox/app && deploy/digitalocean/build-remote-image.sh --env-file deploy/digitalocean/.env.production --set-active'
```

Keep at least the previous snapshot id until a production smoke create succeeds.
Rollback is just setting `YOLOBOX_REMOTE_IMAGE` back to the previous snapshot id
and recreating the backend container.

## Verify

```bash
docker compose --env-file deploy/digitalocean/.env.production \
  -f deploy/digitalocean/docker-compose.yml ps
curl -fsS https://api.yolobox.dev/healthz
curl -fsS https://app.yolobox.dev/healthz
sudo systemctl status yolobox-backend-backup.timer --no-pager
sudo systemctl start yolobox-backend-backup.service
ls -lh /opt/yolobox/backups
```
