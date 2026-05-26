#!/usr/bin/env bash
set -euo pipefail

backup_root="${YOLOBOX_BACKUP_ROOT:-/opt/yolobox/backups}"
data_root="${YOLOBOX_DATA_ROOT:-/opt/yolobox/data}"
retention_days="${YOLOBOX_BACKUP_RETENTION_DAYS:-14}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
archive="${backup_root}/yolobox-backend-${timestamp}.tar.gz"

mkdir -p "${backup_root}"

tmpdir="$(mktemp -d "${backup_root}/.tmp-yolobox-backend.XXXXXX")"

cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

backend_dir="${data_root}/backend"
caddy_dir="${data_root}/caddy"

if [ -f "${backend_dir}/auth.sqlite" ]; then
  sqlite3 "${backend_dir}/auth.sqlite" ".backup '${tmpdir}/auth.sqlite'"
fi

for file in backend.json auth.sqlite-wal auth.sqlite-shm; do
  if [ -f "${backend_dir}/${file}" ]; then
    cp -a "${backend_dir}/${file}" "${tmpdir}/${file}"
  fi
done

if [ -d "${caddy_dir}" ]; then
  tar -C "${data_root}" -czf "${tmpdir}/caddy-data.tar.gz" caddy
fi

tar -C "${tmpdir}" -czf "${archive}" .
chmod 0600 "${archive}"

find "${backup_root}" -maxdepth 1 -type f -name 'yolobox-backend-*.tar.gz' -mtime "+${retention_days}" -delete

echo "${archive}"
