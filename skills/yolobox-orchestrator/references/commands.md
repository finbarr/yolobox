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
yolobox opencode
yolobox copilot
yolobox pi
```

If `default_harness` is set to a shortcut such as `codex`, bare `yolobox` launches that tool. Use an explicit shell when you need manual access:

```bash
yolobox shell
```

If `mode = "remote"`, `remote_name = "foo"`, and `default_harness = "codex"` are set, bare `yolobox` runs:

```bash
yolobox remote --name foo codex
```

## Remote machines

Log in, then create or reuse a named remote machine. The hosted or self-hosted backend leases the SSH host:

```bash
yolobox login --token <token>
yolobox login --backend-url http://127.0.0.1:8787 --token change-me
yolobox remote --name foo codex
```

Run the self-hosted backend package for a shared machine pool:

```bash
cd backend
YOLOBOX_BACKEND_TOKEN=change-me DIGITALOCEAN_ACCESS_TOKEN=... npm run dev
```

Reattach later:

```bash
yolobox remote resume foo codex
```

Sync the current folder to the remote machine:

```bash
yolobox remote sync up foo
```

Copy the remote project back to the local folder, overwriting local files:

```bash
yolobox remote sync down foo --force
```

Forward a remote preview port to localhost:

```bash
yolobox remote forward foo 3000
yolobox remote forward 3000 # uses configured remote_name
```

Inspect and clean up backend state:

```bash
yolobox remote list
yolobox remote status foo
yolobox remote destroy foo --force
```

The remote path depends on backend auth plus `ssh`, `rsync`, and SSH access to the returned host. `sync up` mirrors the whole current folder to `/opt/yolobox/project`, including `.git`, untracked files, ignored files, env files, dependencies, build output, and local caches. `sync down` requires `--force` because it can overwrite local files.

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
