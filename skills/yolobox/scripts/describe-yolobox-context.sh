#!/bin/bash
set -euo pipefail

context_file="${YOLOBOX_CONTEXT_FILE:-/run/yolobox/context.json}"
output_dir="${YOLOBOX_OUTPUT_PATH:-/output}"

path_access_state() {
    local mode="$1"
    local path="$2"

    if [[ -z "$path" || ! -e "$path" ]]; then
        printf 'unknown'
        return
    fi

    case "$mode" in
        readable)
            if [[ -r "$path" ]]; then
                printf 'true'
            else
                printf 'false'
            fi
            ;;
        writable)
            if [[ -w "$path" ]]; then
                printf 'true'
            else
                printf 'false'
            fi
            ;;
        *)
            printf 'unknown'
            ;;
    esac
}

usage() {
    cat <<'EOF'
Usage: describe-yolobox-context.sh [--json]

Describe the current yolobox session.

Options:
  --json    Print the raw yolobox context manifest when available
  --help    Show this help text
EOF
}

case "${1:-}" in
    --help)
        usage
        exit 0
        ;;
    --json)
        ;;
    "")
        ;;
    *)
        printf 'Error: unknown option: %s\n\n' "${1:-}" >&2
        usage >&2
        exit 1
        ;;
esac

if [[ "${1:-}" == "--json" ]]; then
    if [[ -f "$context_file" ]]; then
        cat "$context_file"
    else
        inside_json="false"
        if [[ "${YOLOBOX:-}" == "1" ]]; then
            inside_json="true"
        fi
        printf '{\n  "inside_yolobox": %s,\n  "manifest_present": false\n}\n' "$inside_json"
    fi
    exit 0
fi

if [[ -f "$context_file" ]] && command -v jq >/dev/null 2>&1; then
    project_path="$(jq -r '.paths.project // empty' "$context_file" 2>/dev/null || true)"
    project_writable="$(path_access_state writable "$project_path")"

    jq -r \
        --arg project_writable "$project_writable" \
        '
        (.config.no_project // false) as $no_project |
        [
            "Inside yolobox: yes",
            "Source: manifest",
            "Automatic project mount: " + (($no_project | not) | tostring),
            (if $no_project then "Project: (automatic mount disabled)" else "Project: " + (.paths.project // "") end),
            (if $no_project then empty else "Project writable now: " + $project_writable end),
            "Workdir: " + (.launch.working_dir // ""),
            "Home: " + .paths.home,
            (if .paths.output != null and .paths.output != "" then "Output: " + .paths.output else empty end),
            (if .fork != null then "Fork: " + .fork.name else empty end),
            (if .fork != null then "Fork source: " + .fork.source else empty end),
            (if .fork != null then "Fork copied folder: " + .fork.copy else empty end),
            (if .fork != null then "Compose project: " + .fork.compose_project else empty end),
            "Runtime: configured=" + .runtime.configured + " selected=" + .runtime.selected,
            "Default harness: " + ((.config.default_harness // "none") | tostring),
            "Interactive: " + (.launch.interactive | tostring),
            "Readonly project mode: " + (.config.readonly_project | tostring),
            "Scratch: " + (.config.scratch | tostring),
            "No network: " + (.config.no_network | tostring),
            "No env passthrough: " + ((.config.no_env_passthrough // false) | tostring),
            (if .config.network != "" then "Network: " + .config.network else empty end),
            (if .config.pod != "" then "Pod: " + .config.pod else empty end),
            "Docker socket: " + (.config.docker | tostring),
            "SSH agent: " + (.config.ssh_agent | tostring),
            "GitHub token available: " + ((.launch.gh_token_forwarded or (((.launch.auto_passthrough_env_keys // []) | index("GH_TOKEN")) != null) or (((.launch.auto_passthrough_env_keys // []) | index("GITHUB_TOKEN")) != null)) | tostring),
            "Host clipboard: " + (.config.clipboard | tostring),
            "Host open bridge: " + ((.config.open_bridge // false) | tostring),
            "YOLO wrappers disabled: " + (.config.no_yolo | tostring),
            (if (.config.customize.packages | length) > 0 then "Customize packages: " + (.config.customize.packages | join(", ")) else empty end),
            (if .config.customize.dockerfile != "" then "Customize dockerfile: " + .config.customize.dockerfile else empty end),
            (if (.launch.auto_passthrough_env_keys | length) > 0 then "Auto-forwarded env keys: " + (.launch.auto_passthrough_env_keys | join(", ")) else empty end),
            (if (.config.env_keys | length) > 0 then "Explicit env keys: " + (.config.env_keys | join(", ")) else empty end)
        ]
        | .[]
    ' "$context_file"
    exit 0
fi

inside="no"
if [[ "${YOLOBOX:-}" == "1" ]]; then
    inside="yes"
fi

project="${YOLOBOX_PROJECT_PATH:-$(pwd)}"
workdir="$(pwd)"
home_dir="${HOME:-/home/yolo}"
project_writable="$(path_access_state writable "$project")"
readonly_project="unknown"
output_path=""
docker_socket="false"
ssh_agent="false"
github_token="false"
clipboard="false"
open_bridge="false"

if [[ "$project_writable" == "true" ]]; then
    readonly_project="false"
elif [[ -d "$output_dir" ]]; then
    readonly_project="true"
    output_path="$output_dir"
fi
if [[ -S /var/run/docker.sock ]]; then
    docker_socket="true"
fi
if [[ -n "${SSH_AUTH_SOCK:-}" && -S "${SSH_AUTH_SOCK}" ]]; then
    ssh_agent="true"
fi
if [[ -n "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]]; then
    github_token="true"
fi
if [[ "${YOLOBOX_CLIPBOARD:-}" == "1" && -n "${YOLOBOX_CLIPBOARD_ENDPOINT:-}" ]]; then
    clipboard="true"
fi
if [[ "${YOLOBOX_OPEN_BRIDGE:-}" == "1" && -n "${YOLOBOX_OPEN_BRIDGE_ENDPOINT:-}" ]]; then
    open_bridge="true"
fi

printf 'Inside yolobox: %s\n' "$inside"
printf 'Source: inferred (manifest unavailable)\n'
printf 'Automatic project mount: unknown\n'
printf 'Project: %s\n' "$project"
printf 'Project writable now: %s\n' "$project_writable"
printf 'Workdir: %s\n' "$workdir"
printf 'Home: %s\n' "$home_dir"
if [[ -n "$output_path" ]]; then
    printf 'Output: %s\n' "$output_path"
fi
printf 'Readonly project mode: %s\n' "$readonly_project"
printf 'Docker socket: %s\n' "$docker_socket"
printf 'SSH agent: %s\n' "$ssh_agent"
printf 'GitHub token available: %s\n' "$github_token"
printf 'Host clipboard: %s\n' "$clipboard"
printf 'Host open bridge: %s\n' "$open_bridge"
