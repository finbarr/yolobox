#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

usage() {
  cat <<'EOF'
Usage:
  deploy/digitalocean/build-remote-image.sh [options]

Build a DigitalOcean snapshot that already contains the yolobox remote VM
runtime. By default the script builds from the committed HEAD of this checkout.

Options:
  --env-file <path>       Env file with DigitalOcean settings
                           (default: deploy/digitalocean/.env.production)
  --ref <git-ref>         Build from a GitHub ref instead of this checkout
  --source-dir <path>     Build from a local Git checkout (default: repo root)
  --name <snapshot-name>  Snapshot name (default includes commit/ref + timestamp)
  --size <slug>           Builder Droplet size (default: DIGITALOCEAN_SIZE or small tier)
  --region <slug>         Builder Droplet region (default: DIGITALOCEAN_REGION or nyc3)
  --base-image <slug>     Builder base image (default: DIGITALOCEAN_IMAGE or Ubuntu 24.04)
  --ssh-key <path>        Private key for SSH to the builder Droplet
  --set-active            Write YOLOBOX_REMOTE_IMAGE=<snapshot-id> to the env file
  --keep-builder          Keep the builder Droplet instead of deleting it
  --allow-dirty           Allow a dirty local checkout; only committed HEAD is archived
  -h, --help              Show this help

Required env:
  DIGITALOCEAN_ACCESS_TOKEN or DIGITALOCEAN_TOKEN or DO_API_TOKEN

SSH key selection:
  The Droplet is created with DIGITALOCEAN_SSH_KEYS when set. Otherwise the
  script registers YOLOBOX_REMOTE_SSH_PUBLIC_KEY, the first ssh-agent key, or a
  common local ~/.ssh/*.pub key. The matching private key must be available to
  ssh, either from an agent or --ssh-key.
EOF
}

log() {
  printf '==> %s\n' "$*" >&2
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

shell_quote() {
  printf '%q' "$1"
}

env_file="${YOLOBOX_REMOTE_IMAGE_ENV_FILE:-${repo_root}/deploy/digitalocean/.env.production}"
arg_ref=""
source_dir="${repo_root}"
snapshot_name=""
builder_size=""
builder_region=""
builder_base_image=""
ssh_private_key=""
set_active=0
keep_builder=0
allow_dirty=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      [ "$#" -ge 2 ] || die "--env-file requires a path"
      env_file="$2"
      shift 2
      ;;
    --env-file=*)
      env_file="${1#--env-file=}"
      shift
      ;;
    --ref)
      [ "$#" -ge 2 ] || die "--ref requires a value"
      arg_ref="$2"
      shift 2
      ;;
    --ref=*)
      arg_ref="${1#--ref=}"
      shift
      ;;
    --source-dir)
      [ "$#" -ge 2 ] || die "--source-dir requires a path"
      source_dir="$2"
      shift 2
      ;;
    --source-dir=*)
      source_dir="${1#--source-dir=}"
      shift
      ;;
    --name)
      [ "$#" -ge 2 ] || die "--name requires a value"
      snapshot_name="$2"
      shift 2
      ;;
    --name=*)
      snapshot_name="${1#--name=}"
      shift
      ;;
    --size)
      [ "$#" -ge 2 ] || die "--size requires a value"
      builder_size="$2"
      shift 2
      ;;
    --size=*)
      builder_size="${1#--size=}"
      shift
      ;;
    --region)
      [ "$#" -ge 2 ] || die "--region requires a value"
      builder_region="$2"
      shift 2
      ;;
    --region=*)
      builder_region="${1#--region=}"
      shift
      ;;
    --base-image)
      [ "$#" -ge 2 ] || die "--base-image requires a value"
      builder_base_image="$2"
      shift 2
      ;;
    --base-image=*)
      builder_base_image="${1#--base-image=}"
      shift
      ;;
    --ssh-key)
      [ "$#" -ge 2 ] || die "--ssh-key requires a path"
      ssh_private_key="$2"
      shift 2
      ;;
    --ssh-key=*)
      ssh_private_key="${1#--ssh-key=}"
      shift
      ;;
    --set-active)
      set_active=1
      shift
      ;;
    --keep-builder)
      keep_builder=1
      shift
      ;;
    --allow-dirty)
      allow_dirty=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

if [ -f "$env_file" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
fi

require_command curl
require_command jq
require_command ssh
require_command rsync
require_command git

token="${DIGITALOCEAN_ACCESS_TOKEN:-${DIGITALOCEAN_TOKEN:-${DO_API_TOKEN:-}}}"
[ -n "$token" ] || die "set DIGITALOCEAN_ACCESS_TOKEN, DIGITALOCEAN_TOKEN, or DO_API_TOKEN"

api_url="${YOLOBOX_DIGITALOCEAN_API_URL:-https://api.digitalocean.com}"
api_url="${api_url%/}"
builder_region="${builder_region:-${DIGITALOCEAN_REGION:-nyc3}}"
builder_size="${builder_size:-${DIGITALOCEAN_SIZE:-s-2vcpu-4gb-amd}}"
builder_base_image="${builder_base_image:-${DIGITALOCEAN_IMAGE:-ubuntu-24-04-x64}}"
image_ref="${arg_ref:-${YOLOBOX_REMOTE_IMAGE_REF:-}}"
source_dir="$(cd "$source_dir" && pwd)"
timestamp="$(date -u +%Y%m%d%H%M%S)"
builder_name="yolobox-image-builder-${timestamp}"
builder_name="${builder_name:0:63}"
snapshot_prefix="${YOLOBOX_REMOTE_IMAGE_PREFIX:-yolobox-remote}"
snapshot_done=0
builder_id=""

tmp_dir="$(mktemp -d)"
cleanup() {
  if [ -n "$builder_id" ] && [ "$keep_builder" -eq 0 ]; then
    log "deleting builder Droplet ${builder_id}"
    do_api DELETE "/v2/droplets/${builder_id}" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

do_api() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local output http_code
  output="$(mktemp "${tmp_dir}/do-api.XXXXXX")"
  if [ -n "$body" ]; then
    http_code="$(curl -sS -X "$method" \
      -H "Authorization: Bearer ${token}" \
      -H "Content-Type: application/json" \
      -d "$body" \
      -o "$output" \
      -w '%{http_code}' \
      "${api_url}${path}")"
  else
    http_code="$(curl -sS -X "$method" \
      -H "Authorization: Bearer ${token}" \
      -o "$output" \
      -w '%{http_code}' \
      "${api_url}${path}")"
  fi
  if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ]; then
    printf 'DigitalOcean %s %s failed (%s): %s\n' "$method" "$path" "$http_code" "$(cat "$output")" >&2
    return 1
  fi
  cat "$output"
}

json_csv_array() {
  jq -cn --arg value "$1" '
    $value
    | split(",")
    | map(gsub("^\\s+|\\s+$"; ""))
    | map(select(length > 0))
  '
}

json_ssh_keys() {
  jq -cn --arg value "$1" '
    $value
    | split(",")
    | map(gsub("^\\s+|\\s+$"; ""))
    | map(select(length > 0))
    | map(if test("^[0-9]+$") then tonumber else . end)
  '
}

first_public_key() {
  if [ -n "${YOLOBOX_REMOTE_SSH_PUBLIC_KEY:-}" ]; then
    printf '%s\n' "$YOLOBOX_REMOTE_SSH_PUBLIC_KEY"
    return 0
  fi
  if ssh-add -L >/dev/null 2>&1; then
    ssh-add -L | sed -n '1p'
    return 0
  fi
  for key in "$HOME/.ssh/id_ed25519.pub" "$HOME/.ssh/id_rsa.pub"; do
    if [ -s "$key" ]; then
      sed -n '1p' "$key"
      return 0
    fi
  done
  return 1
}

hash_public_key() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s' "$1" | sha256sum | awk '{print substr($1, 1, 12)}'
  else
    printf '%s' "$1" | shasum -a 256 | awk '{print substr($1, 1, 12)}'
  fi
}

resolve_ssh_keys_json() {
  if [ -n "${DIGITALOCEAN_SSH_KEYS:-}" ]; then
    json_ssh_keys "$DIGITALOCEAN_SSH_KEYS"
    return 0
  fi

  local public_key key_id key_name created
  public_key="$(first_public_key)" || die "set DIGITALOCEAN_SSH_KEYS or provide an SSH public key via YOLOBOX_REMOTE_SSH_PUBLIC_KEY, ssh-agent, or ~/.ssh/*.pub"
  key_id="$(do_api GET "/v2/account/keys?per_page=200" | jq -r --arg public_key "$public_key" '.ssh_keys[] | select(.public_key == $public_key) | .id' | sed -n '1p')"
  if [ -z "$key_id" ]; then
    key_name="yolobox-image-builder-$(hash_public_key "$public_key")"
    created="$(do_api POST "/v2/account/keys" "$(jq -cn --arg name "$key_name" --arg public_key "$public_key" '{name: $name, public_key: $public_key}')")"
    key_id="$(printf '%s' "$created" | jq -r '.ssh_key.id')"
  fi
  jq -cn --argjson key_id "$key_id" '[$key_id]'
}

archive_path=""
build_label=""
build_source=""

if [ -n "$image_ref" ]; then
  [[ "$image_ref" =~ ^[A-Za-z0-9._/-]+$ ]] || die "--ref contains unsupported characters"
  build_label="$image_ref"
  build_source="github ref ${image_ref}"
else
  [ -d "${source_dir}/.git" ] || die "--source-dir must be a Git checkout when --ref is not set"
  if [ "$allow_dirty" -eq 0 ] && [ -n "$(git -C "$source_dir" status --porcelain)" ]; then
    die "local checkout is dirty; commit first or pass --allow-dirty to build committed HEAD anyway"
  fi
  build_label="$(git -C "$source_dir" rev-parse --short=12 HEAD)"
  build_source="local checkout HEAD $(git -C "$source_dir" rev-parse HEAD)"
  archive_path="${tmp_dir}/yolobox-source.tgz"
  git -C "$source_dir" archive --format=tar HEAD | gzip -9 > "$archive_path"
fi

safe_label="$(printf '%s' "$build_label" | tr -cs 'A-Za-z0-9._-' '-' | sed -E 's/^-+|-+$//g' | cut -c1-40)"
[ -n "$safe_label" ] || safe_label="image"
snapshot_name="${snapshot_name:-${snapshot_prefix}-${safe_label}-${timestamp}}"
snapshot_name="${snapshot_name:0:255}"

log "building ${snapshot_name} from ${build_source}"
log "creating builder Droplet in ${builder_region} (${builder_size}, ${builder_base_image})"

ssh_keys_json="$(resolve_ssh_keys_json)"
tags_json="$(json_csv_array "${DIGITALOCEAN_TAGS:-yolobox}")"
tags_json="$(jq -cn --argjson configured "$tags_json" '$configured + ["yolobox-image-builder"] | unique')"

droplet_body="$(jq -cn \
  --arg name "$builder_name" \
  --arg region "$builder_region" \
  --arg size "$builder_size" \
  --arg image "$builder_base_image" \
  --argjson ssh_keys "$ssh_keys_json" \
  --argjson tags "$tags_json" \
  --arg vpc_uuid "${DIGITALOCEAN_VPC_UUID:-}" \
  '{
    name: $name,
    region: $region,
    size: $size,
    image: $image,
    ssh_keys: $ssh_keys,
    tags: $tags,
    monitoring: true
  } + (if $vpc_uuid == "" then {} else {vpc_uuid: $vpc_uuid} end)')"

builder_id="$(do_api POST "/v2/droplets" "$droplet_body" | jq -r '.droplet.id')"
[ -n "$builder_id" ] && [ "$builder_id" != "null" ] || die "DigitalOcean did not return a builder Droplet id"
log "builder Droplet id: ${builder_id}"

wait_for_public_ipv4() {
  local ip=""
  for _ in $(seq 1 120); do
    ip="$(do_api GET "/v2/droplets/${builder_id}" | jq -r '.droplet.networks.v4[]? | select(.type == "public") | .ip_address' | sed -n '1p')"
    if [ -n "$ip" ]; then
      printf '%s\n' "$ip"
      return 0
    fi
    sleep 5
  done
  return 1
}

builder_ip="$(wait_for_public_ipv4)" || die "timed out waiting for builder public IPv4"
log "builder IPv4: ${builder_ip}"

ssh_opts=(-o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=15)
if [ -n "$ssh_private_key" ]; then
  ssh_opts+=(-i "$ssh_private_key")
fi
ssh_target="root@${builder_ip}"
rsync_ssh="ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=15"
if [ -n "$ssh_private_key" ]; then
  rsync_ssh="${rsync_ssh} -i $(shell_quote "$ssh_private_key")"
fi

log "waiting for SSH"
for _ in $(seq 1 120); do
  if ssh "${ssh_opts[@]}" -o ConnectTimeout=5 "$ssh_target" "true" >/dev/null 2>&1; then
    break
  fi
  sleep 5
done
ssh "${ssh_opts[@]}" -o ConnectTimeout=5 "$ssh_target" "true" >/dev/null 2>&1 || die "timed out waiting for SSH on ${ssh_target}"

if [ -n "$archive_path" ]; then
  log "uploading committed source archive"
  rsync -az -e "$rsync_ssh" "$archive_path" "${ssh_target}:/root/yolobox-source.tgz"
  log "installing remote VM runtime from source archive"
  ssh "${ssh_opts[@]}" "$ssh_target" \
    "rm -rf /root/yolobox-source && mkdir -p /root/yolobox-source && tar -xzf /root/yolobox-source.tgz -C /root/yolobox-source && env YOLOBOX_SOURCE_DIR=/root/yolobox-source /root/yolobox-source/cmd/yolobox/assets/remote-vm-install.sh"
else
  log "installing remote VM runtime from GitHub ref ${image_ref}"
  install_url="https://raw.githubusercontent.com/finbarr/yolobox/${image_ref}/cmd/yolobox/assets/remote-vm-install.sh"
  ssh "${ssh_opts[@]}" "$ssh_target" \
    "curl -fsSL $(shell_quote "$install_url") -o /root/yolobox-remote-vm-install.sh && chmod +x /root/yolobox-remote-vm-install.sh && env YOLOBOX_REMOTE_REPO_REF=$(shell_quote "$image_ref") /root/yolobox-remote-vm-install.sh"
fi

log "verifying remote runtime"
ssh "${ssh_opts[@]}" "$ssh_target" \
  "test -f /opt/yolobox/remote/ready && command -v codex >/dev/null && command -v claude >/dev/null && command -v docker >/dev/null && command -v rsync >/dev/null && test -x /usr/local/bin/yolobox-remote-session && test -x /usr/local/bin/yolobox-agent && systemctl list-unit-files yolobox-agent.service >/dev/null"

log "cleaning instance identity for reusable snapshot"
ssh "${ssh_opts[@]}" "$ssh_target" 'bash -s' <<'REMOTE_CLEAN'
set -euo pipefail

if [ -d /etc/cloud/cloud.cfg.d ]; then
  cat > /etc/cloud/cloud.cfg.d/99-yolobox-golden-image.cfg <<'EOF_CLOUD'
ssh_deletekeys: true
EOF_CLOUD
fi

rm -f /root/.bash_history /home/yolo/.bash_history
rm -f /root/.ssh/authorized_keys /home/yolo/.ssh/authorized_keys
rm -f /etc/ssh/ssh_host_*
find /tmp /var/tmp -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true

if command -v cloud-init >/dev/null 2>&1; then
  cloud-init clean --logs --machine-id
else
  truncate -s 0 /etc/machine-id || true
  rm -f /var/lib/dbus/machine-id || true
  ln -sf /etc/machine-id /var/lib/dbus/machine-id || true
fi

sync
REMOTE_CLEAN

log "shutting down builder before snapshot"
ssh "${ssh_opts[@]}" "$ssh_target" "sync && shutdown -h now" >/dev/null 2>&1 || true

wait_for_status() {
  local want="$1"
  local status=""
  for _ in $(seq 1 120); do
    status="$(do_api GET "/v2/droplets/${builder_id}" | jq -r '.droplet.status')"
    if [ "$status" = "$want" ]; then
      return 0
    fi
    sleep 5
  done
  return 1
}

if ! wait_for_status "off"; then
  log "builder did not shut down cleanly; requesting power_off"
  power_action_id="$(do_api POST "/v2/droplets/${builder_id}/actions" '{"type":"power_off"}' | jq -r '.action.id')"
  for _ in $(seq 1 120); do
    power_status="$(do_api GET "/v2/droplets/${builder_id}/actions/${power_action_id}" | jq -r '.action.status')"
    [ "$power_status" = "completed" ] && break
    [ "$power_status" = "errored" ] && die "power_off action failed"
    sleep 5
  done
  wait_for_status "off" || die "builder did not reach off status"
fi

log "snapshotting builder Droplet"
snapshot_action_id="$(do_api POST "/v2/droplets/${builder_id}/actions" "$(jq -cn --arg name "$snapshot_name" '{type: "snapshot", name: $name}')" | jq -r '.action.id')"
[ -n "$snapshot_action_id" ] && [ "$snapshot_action_id" != "null" ] || die "DigitalOcean did not return a snapshot action id"

for _ in $(seq 1 360); do
  snapshot_status="$(do_api GET "/v2/droplets/${builder_id}/actions/${snapshot_action_id}" | jq -r '.action.status')"
  case "$snapshot_status" in
    completed)
      snapshot_done=1
      break
      ;;
    errored)
      die "snapshot action failed"
      ;;
  esac
  sleep 10
done
[ "$snapshot_done" -eq 1 ] || die "timed out waiting for snapshot action ${snapshot_action_id}"

snapshot_id="$(do_api GET "/v2/snapshots?resource_type=droplet&per_page=200" | jq -r --arg name "$snapshot_name" '.snapshots[] | select(.name == $name) | .id' | sed -n '1p')"
[ -n "$snapshot_id" ] && [ "$snapshot_id" != "null" ] || die "snapshot completed, but snapshot id was not found by name"

if [ "$keep_builder" -eq 0 ]; then
  log "deleting builder Droplet ${builder_id}"
  do_api DELETE "/v2/droplets/${builder_id}" >/dev/null
  builder_id=""
fi

set_env_var() {
  local file="$1"
  local key="$2"
  local value="$3"
  local tmp
  [ -f "$file" ] || die "cannot update missing env file: $file"
  tmp="$(mktemp "${tmp_dir}/env.XXXXXX")"
  awk -v key="$key" -v value="$value" '
    BEGIN { done = 0 }
    $0 ~ "^" key "=" {
      print key "=" value
      done = 1
      next
    }
    { print }
    END {
      if (!done) {
        print key "=" value
      }
    }
  ' "$file" > "$tmp"
  mv "$tmp" "$file"
}

if [ "$set_active" -eq 1 ]; then
  log "writing active image metadata to ${env_file}"
  set_env_var "$env_file" "YOLOBOX_REMOTE_IMAGE" "$snapshot_id"
  set_env_var "$env_file" "YOLOBOX_REMOTE_IMAGE_REF" "$build_label"
  set_env_var "$env_file" "YOLOBOX_REMOTE_IMAGE_NAME" "$snapshot_name"
  set_env_var "$env_file" "YOLOBOX_REMOTE_IMAGE_BUILT_AT" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi

cat <<EOF
Snapshot ready.
  id:   ${snapshot_id}
  name: ${snapshot_name}
  ref:  ${build_label}

Set YOLOBOX_REMOTE_IMAGE=${snapshot_id} in the backend environment, then restart
the backend so new remote machines are created from this snapshot.
EOF
