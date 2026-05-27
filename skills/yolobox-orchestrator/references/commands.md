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
yolobox remote run foo codex
```

## Remote machines

Log in through the browser, then create a named remote machine. The hosted or self-hosted backend leases the host, and the CLI reaches it only through the backend tunnel:

```bash
yolobox login
yolobox login --backend-url http://127.0.0.1:8787
yolobox remote create foo
yolobox remote create foo --tier medium
yolobox remote run foo codex
```

`remote create` fails if `foo` already exists; use `remote run`, `remote connect`, or `remote status` for existing machines. The backend creates a per-machine agent token, stores only its hash, and authenticates VM agent calls by that token only, not by any machine name claimed by the VM. The backend also creates per-machine tunnel SSH credentials. `remote connect` opens or attaches to the managed tmux session without syncing, bootstrapping, or changing the remote workdir alias, and it fails if backend metadata says bootstrap has not completed. Remote commands print progress while backend provisioning and tunneled SSH startup are pending, then print the ready state and any generated preview URL on separate lines.

Run the self-hosted backend package for a shared machine pool:

```bash
cd backend
BETTER_AUTH_SECRET=replace-with-a-random-secret-at-least-32-bytes DIGITALOCEAN_ACCESS_TOKEN=... npm run dev
```

Attach to the existing managed session without syncing:

```bash
yolobox remote connect foo
```

Sync the current folder to the remote machine:

```bash
yolobox remote sync up foo
```

Copy the remote project back to the local folder, overwriting local files:

```bash
yolobox remote sync down foo --force
```

Inspect and clean up backend state:

```bash
yolobox remote list
yolobox remote status foo
yolobox remote destroy foo --force
```

The remote path depends on backend auth plus local `ssh` and `rsync`; SSH access is always through the backend WebSocket tunnel and fails if the VM agent is not connected. `sync up` mirrors the whole current folder to `/opt/yolobox/project`, then runs VM-native sessions from a source-path alias matching the local project path. Each remote machine has one managed tmux session named `yolobox`; if it already exists, terminal `run` and `connect` attach instead of starting another session. The remote VM is the sandbox: commands do not run inside a nested yolobox container, and Docker Compose talks to the VM's Docker daemon. The mirrored folder includes `.git`, untracked files, ignored files, env files, dependencies, build output, and local caches. `sync down` requires `--force` because it can overwrite local files.

For hosted DigitalOcean backends, build or rotate the remote VM golden snapshot with `deploy/digitalocean/build-remote-image.sh`, set `YOLOBOX_REMOTE_IMAGE` to the snapshot id, and restart the backend before testing new remote creates.

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
