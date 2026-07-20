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

## Updating agent CLIs

Update all bundled AI CLIs in the persistent yolobox home:

```bash
yolobox update-agents
```

Target one or more tools when you do not want the full update path:

```bash
yolobox update-agents codex
yolobox update-agents kimi
yolobox update-agents claude antigravity
```

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

Set project-specific environment variables in `.yolobox.toml` when a tool needs a per-project home or account:

```toml
env = ["CODEX_HOME=/home/yolo/.codex-account"]
```

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
