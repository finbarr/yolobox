---
name: yolobox-orchestrator
description: Use when the agent is outside yolobox and needs to start, inspect, or control yolobox sessions, choose the right yolobox subcommand or flags, read merged config, decide between scratch, readonly, docker, or network options, or debug how a yolobox launch should be configured.
license: MIT
compatibility: Requires the yolobox CLI on the host. Runtime behavior depends on the configured container runtime and local Docker, Podman, or Apple container access.
---

# Yolobox Orchestrator

Use this skill for host-side orchestration of yolobox sessions.

Do not use it for questions about the current environment from inside a running box. Inside the container, use `yolobox` instead.

1. Start from the user's intent, then choose the smallest `yolobox` command or flag set that accomplishes it.
   - For an ACP client, use `yolobox run codex-acp` or `yolobox run claude-agent-acp` with piped stdin/stdout. The adapters bundle compatible engines and use ACP permission modes; `--no-yolo` does not control them. See [references/commands.md](references/commands.md) for config, authentication, and updates.
   - For a fresh Claude or Codex session with extra Markdown instructions, pass `--instructions FILE` to the shortcut.
2. Check `yolobox config` when defaults, merged config, or flag precedence matter.
   - If `default_harness` is set, bare `yolobox` launches that shortcut; use `yolobox shell` for an explicit shell.
3. Prefer explicit isolation and safety flags:
   - `--scratch` for disposable or concurrent sessions that must not share `/home/yolo`.
   - Use `--scratch --reuse-login` only when the user wants the current regular-box Claude or Codex access token available in that scratch session.
   - Use `volume backup` for a stopped home volume and `volume restore` to create a separate copy. Select the new volume with `--home-volume`.
   - Use `--gitlab-proxy HOST/GROUP/PROJECT` for read-only GitLab access scoped to one project.
   - `--readonly-project` when the agent only needs read access to the project tree.
   - `--no-env-passthrough` when host API/token environment variables should not enter the box automatically.
   - `--claude-config --no-claude-auth` when host Claude settings should be synced but the persistent box must keep its own `/login` credentials. This still live-mounts host Claude project history read/write.
   - `--copilot-config` to share host Copilot CLI settings, login, and session history; add `--no-copilot-auth` when the box must keep its own Copilot `/login`.
   - `--open-bridge` only when the agent needs to open HTTP(S) URLs in the host browser.
   - `--docker` only when the agent needs Docker access or sibling containers.
4. When you need exact command patterns or edge-case reminders, read [references/commands.md](references/commands.md).
5. If you launch a box for another agent, point it at `yolobox` and `YOLOBOX_CONTEXT_FILE` for inside-the-box introspection.
6. When discussing concurrency, use the per-run manifest to identify shared state. Persistent boxes share `/var/cache` by runtime and architecture. They share `/home/yolo` when they use the same home volume.
