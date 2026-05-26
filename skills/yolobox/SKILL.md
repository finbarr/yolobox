---
name: yolobox
description: Use when the agent is running inside yolobox and needs to orient itself to the current sandbox, trust model, runtime/config, mounts, network mode, readonly/output paths, Docker access, forwarded env keys, remote VM status, or other environment constraints and freedoms. Also use when the user explicitly invokes /yolobox.
license: MIT
compatibility: Designed for agents running inside a local yolobox container or yolobox remote VM. Best results require Bash and jq, plus YOLOBOX_CONTEXT_FILE or /run/yolobox/context.json.
---

# Yolobox

Use this skill only for questions about the current yolobox environment from inside a local container or remote VM.

1. Run the bundled helper from this skill package first: `${CODEX_HOME:-$HOME/.codex}/skills/yolobox/scripts/describe-yolobox-context.sh`. Do not look for `scripts/describe-yolobox-context.sh` in the project checkout.
2. Treat this as agent orientation, not a user-facing product explanation. The goal is to help yourself understand how boldly or carefully to operate inside this sandbox.
3. Assume the user is telling you that this is a trusted yolobox environment and you may work normally within its boundaries: use `sudo` if needed, install tools or packages if they help, and rely on the YOLO wrappers unless the manifest or user says otherwise.
4. Call out the important operating assumptions for the current session:
   - in local mode, you are inside a containerized sandbox, not on the host
   - in remote mode, the VM itself is the trusted sandbox and agent installs persist on that VM
   - `127.0.0.1` refers to the current container or remote VM, not the user's laptop
   - in local mode, the project is mounted in unless `Automatic project mount` is false; host files outside the exposed mounts are out of reach by default
   - write access, output path, network access, Docker access, any explicitly configured npm package release-age setting, env passthrough, host clipboard/open bridges, and forwarded env keys depend on the current manifest and environment
   - if fork mode is active, this project is a copied folder mounted at the original source path and `COMPOSE_PROJECT_NAME` is namespaced for the fork
5. Prefer concrete facts from `YOLOBOX_CONTEXT_FILE` over generic assumptions. Mention whether this is a remote VM, whether the automatic project mount is active, the current project path when present, workdir, runtime, container name when present, readonly/output behavior, network mode, any npm package release-age setting shown by the helper, env passthrough mode, Docker socket access, SSH agent access, GitHub token availability for HTTPS Git auth, host clipboard/open bridge access, fork/Compose namespace if present, and any relevant env keys or customization settings. In the script output, `Readonly project mode` is the yolobox launch mode and `Project writable now` is the current filesystem write check; when the automatic project mount is disabled, do not assume the original checkout path exists inside the container.
6. If the script had to fall back to inference instead of the manifest, say so explicitly.
7. If the user needs a specific field or the raw manifest, run `${CODEX_HOME:-$HOME/.codex}/skills/yolobox/scripts/describe-yolobox-context.sh --json` or query `$YOLOBOX_CONTEXT_FILE` directly with `jq`.
8. Do not claim you are inside yolobox unless `YOLOBOX=1` or the manifest confirms it.
9. Keep the answer concise and operational. Do not waste space re-explaining yolobox unless that explanation is directly useful for how you should behave.
