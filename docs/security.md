# Security Model

## How it works

yolobox uses container isolation as its safety boundary. When you run it, yolobox:

1. starts a container with your project mounted at its real path
2. runs as user `yolo` with sudo access inside the container
3. keeps your home directory unmounted unless you explicitly opt in
4. relies on the container runtime to isolate filesystem, process tree, and network

The AI has full root-equivalent power inside the container, but only over what the container can actually see.

## Trust boundary

The trust boundary is the container runtime itself.

That means yolobox is good at protecting against accidents like:

- deleting your home directory
- reading your SSH keys by default
- rummaging through unrelated projects

It is not a promise against:

- kernel exploits
- container escape vulnerabilities
- a deliberately hostile agent trying to break isolation

If you are defending against hostile code rather than careless code, move up to stronger isolation.

## What yolobox protects

- your home directory
- your SSH keys, dotfiles, and usual workstation credentials
- unrelated projects and most host filesystem state
- the host from accidental destructive commands aimed at `~`

## What yolobox does not protect

- your project directory, which is mounted read-write by default
- network access unless you turn it off
- the container's own filesystem and state
- the host from runtime or kernel escape vulnerabilities

If you want to narrow the container's view of the project itself, use `--exclude` and `--copy-as` to hide or replace selected files before the agent sees them.

## Important trust-expanding flags

Some flags deliberately widen the trust boundary:

- `--docker` mounts the host Docker socket into the container
- `--clipboard` lets clipboard commands in the container read and write the host text clipboard through a short-lived proxy
- `--claude-config`, `--codex-config`, `--gemini-config`, `--opencode-config`, `--pi-config`, and `--git-config` copy selected host config into the container; `--codex-config` also live-mounts host Codex sessions read/write for resume continuity
- `--open-bridge` lets `open` and `xdg-open` commands in the container ask the host to open HTTP(S) URLs
- `--gh-token` forwards a GitHub token for `gh` and HTTPS Git authentication
- `--rtk` initializes RTK inside the container for supported AI CLIs, which means RTK can inspect and compress command output for those sessions
- automatic environment passthrough forwards common API/token variables when they are set; use `--no-env-passthrough` to suppress it
- `--mount`, `--device`, and `--runtime-arg` expose extra host paths, devices, and low-level runtime capabilities

These are useful, but they are explicit trust decisions.

## Remote mode

`yolobox remote` runs on a VM or host returned by a configured backend. That remote host is outside the local container trust boundary. Anyone with SSH access to it can inspect synced workspace folders, running containers, tmux sessions, forwarded preview traffic, and any files or secrets you place there.

Remote backend tokens authorize host leasing and release. Treat `remote.backend_token` and `YOLOBOX_REMOTE_TOKEN` like cloud credentials. A self-hosted backend should listen behind TLS or on a private network, and its machine pool should contain hosts you are comfortable dedicating to yolobox workloads.

The MVP mirrors the entire current folder with `rsync` on `remote sync up`. That includes `.git` if present, uncommitted files, ignored files, `.env` files, dependency folders, build output, and local caches. Treat the remote as a trusted development machine, and move secrets out of the project folder before syncing when they should not leave your laptop. `remote sync down ... --force` copies remote files back into the local folder and can overwrite local changes.

Remote preview access is explicit. The open-source MVP supports local SSH forwarding with `remote forward`; it does not create public preview URLs. Stop the forwarding process when you are done.

## Hardening options

### Level 1: default

```bash
yolobox claude
```

Good for protection from accidental damage.

### Level 2: reduced attack surface

```bash
yolobox claude --no-network --no-env-passthrough --readonly-project --exclude ".env*" --exclude "secrets/**"
```

Good when you want a tighter box for inspection or untrusted code.

### Level 3: rootless Podman

```bash
yolobox claude --runtime podman
```

Rootless Podman maps container root to your unprivileged host user, which reduces the blast radius of runtime escapes.

### Level 4: VM isolation

Use a VM if you are worried about malicious-container risk rather than simple accidents.

- macOS: UTM, Parallels, Lima, or similar
- Linux: a dedicated VM or Podman machine

## Podman network isolation

Rootless Podman commonly uses `slirp4netns`, which helps isolate containers from the host network while still allowing outbound internet access.

That makes rootless Podman a strong default if security matters more than convenience.

## Quick recommendations

- Use Docker or Podman defaults when your goal is protection from accidents.
- Add `--no-network`, `--no-env-passthrough`, and `--readonly-project` when you want a tighter box.
- Use rootless Podman when you want stronger host hardening.
- Use a VM when you care about hostile workloads, not just accidental damage.

## npm package freshness

The base image build runs yolobox's own npm/npx installs with `NPM_CONFIG_MIN_RELEASE_AGE=7`, so image contents avoid package versions published in the last week. The Dockerfile upgrades npm first using npm's date-based `--before` filter so release builds can keep npm current without trusting a just-published npm package. The finished box does not keep that npm config, so runtime npm/npx installs, CLI upgrades, and derived Dockerfile fragments are unrestricted by default unless you explicitly set npm's release-age config yourself.
