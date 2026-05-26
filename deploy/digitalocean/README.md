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
`DIGITALOCEAN_ACCESS_TOKEN` and either `DIGITALOCEAN_SSH_KEYS` or
`YOLOBOX_REMOTE_SSH_PUBLIC_KEY` so it can create remote VMs for users.
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
