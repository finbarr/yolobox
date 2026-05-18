---
name: yolobox-orchestrator
description: Use when the agent is outside yolobox and needs to start, inspect, or control local or remote yolobox sessions, choose the right yolobox subcommand or flags, read merged config, decide between scratch, readonly, docker, network, fork, or remote options, or debug how a yolobox launch should be configured.
license: MIT
compatibility: Requires the yolobox CLI on the host. Runtime behavior depends on the configured container runtime and local Docker, Podman, or Apple container access.
---

# Yolobox Orchestrator

Use this skill for host-side orchestration of yolobox sessions.

Do not use it for questions about the current environment from inside a running box. Inside the container, use `yolobox` instead.

1. Start from the user's intent, then choose the smallest `yolobox` command or flag set that accomplishes it.
2. Check `yolobox config` when defaults, merged config, or flag precedence matter.
   - If `default_harness` is set, bare `yolobox` launches that shortcut; use `yolobox shell` for an explicit shell.
   - If `mode = "remote"` and `remote_name` are set, bare `yolobox` attaches to `yolobox remote resume <name>/<remote_workspace> <default_harness>`, with `remote_workspace` defaulting to `default`.
3. Prefer explicit isolation and safety flags:
   - `--scratch` for disposable or concurrent sessions that must not share `/home/yolo`.
   - `--readonly-project` when the agent only needs read access to the project tree.
   - `--no-env-passthrough` when host API/token environment variables should not enter the box automatically.
   - `--open-bridge` only when the agent needs to open HTTP(S) URLs in the host browser.
   - `--docker` only when the agent needs Docker access or sibling containers.
4. Use `yolobox remote --name <env> --workspace <name> <cmd...>` when the user wants a named remote machine and workspace that keep running after the local machine disconnects. Remote mode requires either `remote.backend_url` / `--backend-url` plus a backend token, or `remote.provider` / `--provider` such as `digitalocean` plus provider credentials. The backend or direct provider leases an SSH host and the client uses `ssh` and `rsync` for sync, attach, and forwarding. `sync up` mirrors the whole current folder to the VM, `sync down` requires `--force`, and `remote forward` is the supported local preview path.
5. When you need exact command patterns or edge-case reminders, read [references/commands.md](references/commands.md).
6. If you launch a box for another agent, point it at `yolobox` and `YOLOBOX_CONTEXT_FILE` for inside-the-box introspection.
7. When discussing concurrency, distinguish isolated per-run manifests from shared persistent state: manifests are per-run, but `/home/yolo` and `/var/cache` are shared unless `--scratch` is used.
