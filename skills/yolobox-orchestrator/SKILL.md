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
   - If `mode = "remote"` and `remote_name` are set, bare `yolobox` runs `yolobox remote run <name> <default_harness>`.
3. Prefer explicit isolation and safety flags:
   - `--scratch` for disposable or concurrent sessions that must not share `/home/yolo`.
   - `--readonly-project` when the agent only needs read access to the project tree.
   - `--no-env-passthrough` when host API/token environment variables should not enter the box automatically.
   - `--open-bridge` only when the agent needs to open HTTP(S) URLs in the host browser.
   - `--docker` only when the agent needs Docker access or sibling containers.
4. Use `yolobox login`, `yolobox remote create <env>`, and `yolobox remote run <env> <cmd...>` when the user wants a named remote machine that keeps running after the local machine disconnects. Remote mode is backend-only: the hosted or self-hosted backend leases a bootstrapped host, and the CLI uses local `ssh` and `rsync` through the backend WebSocket tunnel for terminal I/O and file copy. Setup commands, command wrapping, and tmux lifecycle are backend-authorized VM agent actions. There is no direct-SSH fallback; if the machine lacks backend tunnel credentials, the VM agent is disconnected, or the backend cannot open SSH through the tunnel, remote operations fail. The CLI pins remote SSH host keys in `~/.yolobox/remote_known_hosts`. `remote create` accepts `--tier small`, `--tier medium`, or `--tier large` when a newly provisioned VM needs a specific size, and it fails when the name already exists. Hosted DigitalOcean backends should create remotes from a golden snapshot id in `YOLOBOX_REMOTE_IMAGE`; build or rotate that snapshot with `deploy/digitalocean/build-remote-image.sh`. Machine-agent identity is token-only: the backend creates a high-entropy per-machine token, stores only its hash, passes the plaintext token to the VM, and never trusts a machine name claimed by the VM. `remote run` and `remote connect` operate on existing machines. `remote connect` opens or attaches to the managed tmux session without syncing or bootstrapping, and it fails if backend metadata says bootstrap has not completed. `sync up` mirrors the whole current folder to `/opt/yolobox/project`; remote commands run from that path; and `sync down` requires `--force`. Backend restarts drop active tunnel connections, but the VM and managed tmux session remain; rerun the CLI command after the backend and VM agent reconnect. The CLI prints progress while backend provisioning and tunneled SSH startup are pending, then prints the ready state and any generated preview URL on separate lines. Each remote machine has exactly one managed tmux session; if it already exists, terminal `run` and `connect` attach instead of starting a second session. The remote VM is the sandbox; commands do not run inside a nested yolobox container.
5. When you need exact command patterns or edge-case reminders, read [references/commands.md](references/commands.md).
6. If you launch a box for another agent, point it at `yolobox` and `YOLOBOX_CONTEXT_FILE` for inside-the-box introspection.
7. When discussing concurrency, distinguish isolated per-run manifests from shared persistent state: manifests are per-run, but `/home/yolo` and `/var/cache` are shared unless `--scratch` is used.
