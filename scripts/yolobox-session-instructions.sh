#!/bin/bash
set -euo pipefail

session_dir=/run/yolobox/session
instructions_file="$session_dir/instructions.md"

if [[ ${1:-} == --codex-hook ]]; then
    # Only the first startup hook in this container receives the instructions.
    [[ -f "$instructions_file" ]] || exit 0
    consumed_file=$(mktemp "$session_dir/consumed.XXXXXX")
    trap 'rm -f "$consumed_file"' EXIT
    if mv "$instructions_file" "$consumed_file" 2>/dev/null; then
        jq -n --rawfile instructions "$consumed_file" \
            '{hookSpecificOutput: {hookEventName: "SessionStart", additionalContext: $instructions}}'
    fi
    exit
fi

if [[ -z ${YOLOBOX_SESSION_INSTRUCTIONS_B64:-} ]]; then
    echo 'yolobox: missing session instructions' >&2
    exit 1
fi

# Neither the Markdown nor the system hook may be written to a volume or a
# host bind mount, even when it was supplied through raw runtime arguments.
require_container_path() {
    local original=$1 path
    path=$(realpath -m -- "$original")
    while [[ ! -e "$path" ]]; do
        path=${path%/*}
    done
    if [[ $(findmnt -n -o TARGET -T "$path") != / ]]; then
        echo "yolobox: --instructions requires container-local storage at $original" >&2
        exit 1
    fi
}

system_hooks=/etc/codex/hooks.json
require_container_path "$session_dir"
require_container_path "$instructions_file"
if [[ $1 == codex ]]; then
    require_container_path /etc/codex
    require_container_path "$system_hooks"
fi

sudo install -d -m 0700 -o "$(id -u)" -g "$(id -g)" "$session_dir"
umask 077
printf '%s' "$YOLOBOX_SESSION_INSTRUCTIONS_B64" | base64 -d > "$instructions_file"
unset YOLOBOX_SESSION_INSTRUCTIONS_B64

if [[ $1 == claude ]]; then
    shift
    exec claude --append-system-prompt-file "$instructions_file" "$@"
fi

# Install the hook for this container only. Keep any existing system hooks,
# including other SessionStart handlers, and let Codex layer user hooks normally.
session_hooks=/opt/yolobox/codex-session-hooks.json
if [[ -e "$system_hooks" ]]; then
    jq -s '.[1].hooks.SessionStart as $session | .[0] | .hooks.SessionStart += $session' \
        "$system_hooks" "$session_hooks" > "$session_dir/hooks.json"
    session_hooks="$session_dir/hooks.json"
fi
staged_hooks=$(sudo mktemp /etc/codex/.yolobox-session-hooks.XXXXXX)
trap 'sudo rm -f "$staged_hooks"' EXIT
sudo install -m 0644 "$session_hooks" "$staged_hooks"
sudo mv -f "$staged_hooks" "$system_hooks"
trap - EXIT
exec "$@"
