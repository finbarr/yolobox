#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

install -m 0755 "${script_dir}/backup-backend.sh" /usr/local/bin/yolobox-backend-backup
install -m 0644 "${script_dir}/yolobox-backend-backup.service" /etc/systemd/system/yolobox-backend-backup.service
install -m 0644 "${script_dir}/yolobox-backend-backup.timer" /etc/systemd/system/yolobox-backend-backup.timer

systemctl daemon-reload
systemctl enable --now yolobox-backend-backup.timer
systemctl start yolobox-backend-backup.service
