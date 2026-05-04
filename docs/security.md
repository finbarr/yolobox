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
- `--claude-config`, `--codex-config`, `--gemini-config`, `--opencode-config`, and `--git-config` copy selected host config into the container
- `--gh-token` forwards a GitHub token for `gh` and HTTPS Git authentication
- `--mount`, `--device`, and `--runtime-arg` expose extra host paths, devices, and low-level runtime capabilities

These are useful, but they are explicit trust decisions.

## Hardening options

### Level 1: default

```bash
yolobox claude
```

Good for protection from accidental damage.

### Level 2: reduced attack surface

```bash
yolobox claude --no-network --readonly-project --exclude ".env*" --exclude "secrets/**"
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
- Add `--no-network` and `--readonly-project` when you want a tighter box.
- Use rootless Podman when you want stronger host hardening.
- Use a VM when you care about hostile workloads, not just accidental damage.
