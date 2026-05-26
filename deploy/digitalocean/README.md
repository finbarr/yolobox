# DigitalOcean deployment

This bundle runs the yolobox remote console and API on a single DigitalOcean
Droplet. Caddy terminates TLS for `app.yolobox.dev` and `api.yolobox.dev`, then
proxies both hostnames to the backend container. Backend state is stored on the
host under `/opt/yolobox/data/backend` so it can be backed up outside Docker.

## Provision

Create an Ubuntu 24.04 Droplet with `cloud-init.yml`. Enable DigitalOcean
Droplet backups for machine-level restore coverage.

Required DNS records:

```text
app.yolobox.dev.  A  <droplet-ip>
api.yolobox.dev.  A  <droplet-ip>
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
