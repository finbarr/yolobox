# Yolobox Command Patterns

Use this file when you need concrete host-side `yolobox` command shapes.

## Inspecting config

Check the effective merged configuration before changing behavior:

```bash
yolobox config
```

## Basic launches

Run a one-shot command:

```bash
yolobox run echo hello
```

Launch an AI CLI in the box:

```bash
yolobox codex
yolobox claude
yolobox gemini
yolobox kimi
yolobox agy
yolobox antigravity
yolobox opencode
yolobox copilot
yolobox pi
```

If `default_harness` is set to a shortcut such as `codex`, bare `yolobox` launches that tool. Use an explicit shell when you need manual access:

```bash
yolobox shell
```

## ACP clients

Configure an ACP client or editor to spawn `yolobox` from the project directory, using one of these stdio agent commands:

```bash
yolobox run codex-acp
yolobox run claude-agent-acp
```

The client must pipe ACP protocol messages into stdin and read responses from stdout. These are protocol servers, not interactive CLI shortcuts.

Existing persistent-box authentication and config sync options apply:

```bash
yolobox run --codex-config codex-acp
yolobox run --claude-config claude-agent-acp
yolobox run --claude-config --no-claude-auth claude-agent-acp
```

Use `--no-claude-auth` to preserve the box's independent login while syncing Claude settings. API keys use the normal environment passthrough controls; add `--env` or `--env-from-host` when needed.

Each adapter bundles a compatible engine and bypasses yolobox's `codex` / `claude` wrappers, so its engine version may differ from the interactive CLI. Select permissions through the ACP client. `--no-yolo` and `NO_YOLO` do not control adapter permissions.

## Updating agent CLIs

Update all bundled AI CLIs and ACP adapters in the persistent yolobox home:

```bash
yolobox update-agents
```

Target one or more tools when you do not want the full update path:

```bash
yolobox update-agents codex
yolobox update-agents kimi
yolobox update-agents claude antigravity
```

The `codex` target updates Codex and `codex-acp`; the `claude` target updates Claude Code and `claude-agent-acp`. Adapter command names are not separate update targets.

This is a global maintenance command: it ignores `.yolobox.toml` and does not mount the current project. Do not add `--scratch`; agent updates need the persistent `yolobox-home` volume.

## Isolation controls

Use a fresh home/cache state:

```bash
yolobox run --scratch sh -lc 'pwd && whoami'
```

Mount the project read-only and write outputs to `/output`:

```bash
yolobox run --readonly-project sh -lc 'pwd && ls /output'
```

Disable automatic host environment passthrough for untrusted work:

```bash
yolobox run --no-env-passthrough env
```

Sync host Claude settings while keeping the box login independent:

```bash
yolobox claude --claude-config --no-claude-auth
```

Run `/login` once inside the persistent box. This mode does not extract the macOS Keychain credential, import host `.credentials.json`, or auto-forward `CLAUDE_CODE_OAUTH_TOKEN`. It still live-mounts host `~/.claude/projects` read/write so resume history stays current.

Sync host Copilot CLI settings and session history, optionally keeping the box login independent:

```bash
yolobox copilot --copilot-config
yolobox copilot --copilot-config --no-copilot-auth
```

Without `--no-copilot-auth`, the host Copilot login is forwarded as `COPILOT_GITHUB_TOKEN` unless `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN` is already supplied. With it, host login keys are stripped and token passthrough is suppressed; run `/login` once inside the box.

Set project-specific environment variables in `.yolobox.toml` when a tool needs a per-project home or account:

```toml
env = ["CODEX_HOME=/home/yolo/.codex-account"]
```

`env` values are passed to the runtime verbatim; nothing in them is interpreted. To give the container a different value than the host uses under the same name, alias it from a host variable:

```bash
yolobox run --env-from-host GH_TOKEN=YOLOBOX_READONLY_GH_TOKEN claude
```

```toml
env_from_host = ["GH_TOKEN=YOLOBOX_READONLY_GH_TOKEN"]
```

An alias owns that container variable: automatic passthrough and `--gh-token` are skipped for the same key, setting the key in both `env` and `env_from_host` is an error, and an unset host variable aborts the run rather than falling back to the variable the alias replaces.

## Docker and network access

Allow Docker commands inside the box:

```bash
yolobox run --docker docker version
```

Join an existing Docker network:

```bash
yolobox run --network my-compose_default sh -lc 'getent hosts db'
```

Bridge text clipboard copy/paste to the host:

```bash
yolobox codex --clipboard
```

Bridge URL opening to the host browser:

```bash
yolobox codex --open-bridge
```

## Context handoff to the inside agent

Every session provides a manifest at `/run/yolobox/context.json` and exports `YOLOBOX_CONTEXT_FILE`.

If an agent inside the box needs to orient itself to the environment, direct it to use `yolobox`.

## Concurrency reminder

Concurrent `yolobox` runs each get their own manifest, even with different args.

Persistent state is still shared unless `--scratch` is used:

- `/home/yolo`
- `/var/cache`
- the mounted project tree

## Nested yolobox reminder

When `yolobox` runs inside another `yolobox`, temp mount sources must live under an existing host-visible bind mount such as the project path. An inner-container `/tmp` is not visible to the outer Docker daemon.
