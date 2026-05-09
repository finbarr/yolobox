# Flags

::: tip
Flags go after the subcommand: `yolobox run --flag cmd` or `yolobox claude --flag`, not `yolobox --flag run cmd`.
:::

## Runtime & image

| Flag | Description | Incompatible with |
|------|-------------|-------------------|
| `--runtime <name>` | Use `docker`, `podman`, or `container` | |
| `--image <name>` | Override the base image | |
| `--packages <list>` | Comma-separated apt packages for a derived custom image | Apple `container` |
| `--customize-file <path>` | Dockerfile fragment for a derived custom image | Apple `container` |
| `--rebuild-image` | Force rebuild of the derived custom image | Apple `container` |

## Filesystem, config, and identity

| Flag | Description | Incompatible with |
|------|-------------|-------------------|
| `--mount <src:dst>` | Extra mount, repeatable | |
| `--exclude <glob>` | Hide matching project paths from the container, repeatable | Apple `container`, `--no-project`, without `--readonly-project` |
| `--copy-as <src:dst>` | Mount a file at another project path inside the container, repeatable | Apple `container`, `--no-project`, without `--readonly-project` |
| `--env <KEY=val>` | Extra environment variable, repeatable | |
| `--setup` | Run interactive setup before starting | |
| `--ssh-agent` | Forward SSH agent socket | |
| `--readonly-project` | Mount the project read-only and write outputs to `/output` | `--no-project` |
| `--no-project` | Skip the automatic project mount; caller provides `--mount` and `--runtime-arg=--workdir` | `--readonly-project`, `--exclude`, `--copy-as` |
| `--claude-config` | Copy host `~/.claude` config into the container | |
| `--codex-config` | Copy host `~/.codex` config into the container | |
| `--gemini-config` | Copy host `~/.gemini` config into the container | |
| `--opencode-config` | Copy host `~/.config/opencode` config into the container | |
| `--git-config` | Copy host `~/.gitconfig` into the container | |
| `--gh-token` | Forward GitHub token for `gh` and HTTPS Git auth from `gh auth token` | |
| `--copy-agent-instructions` | Copy global instruction files and skills into the container | |
| `--clipboard` | Bridge text clipboard copy/paste between the container and host | `--no-network` |

## Networking and behavior

| Flag | Description | Incompatible with |
|------|-------------|-------------------|
| `--no-network` | Disable network access | `--network`, `--pod`, `--docker`, `--clipboard` |
| `--network <name>` | Join a specific network | `--no-network`, `--pod` |
| `--pod <name>` | Join an existing Podman pod | `--no-network`, `--network`, `--docker` |
| `--no-yolo` | Disable auto-confirmations | |
| `--scratch` | Start with a fresh home and cache | |
| `--docker` | Mount the Docker socket and join the shared `yolobox-net` network | `--no-network`, `--pod` |

## Resources and low-level runtime control

| Flag | Description | Incompatible with |
|------|-------------|-------------------|
| `--cpus <num>` | Limit CPUs, including fractional values like `3.5` | |
| `--memory <limit>` | Hard memory limit like `8g` or `1024m` | |
| `--shm-size <size>` | Size of `/dev/shm` | |
| `--gpus <spec>` | Pass GPUs, for example `all` or `device=0` | |
| `--device <src:dest>` | Add host devices, repeatable | |
| `--cap-add <cap>` | Add Linux capabilities, repeatable | |
| `--cap-drop <cap>` | Drop Linux capabilities, repeatable | |
| `--runtime-arg <flag>` | Pass raw runtime flags directly to Docker or Podman | |

## SSH agent on macOS

On macOS, `--ssh-agent` depends on the VM forwarding the agent:

- Docker Desktop forwards it automatically
- Colima needs `forwardAgent: true` in `~/.colima/default/colima.yaml`, then a restart

## Networking

By default, yolobox uses the runtime's normal bridged network.

- use `--network <name>` when you need container-name DNS on a compose network
- use `--no-network` when you want complete network isolation

## Docker access {#docker-access}

The `--docker` flag mounts the host Docker socket into the container and joins a shared `yolobox-net` network. That lets the agent:

- run Docker commands
- build images
- start sibling containers
- communicate with services by container name on the shared network

The network name is available inside the container as `$YOLOBOX_NETWORK`.

::: warning
`--docker` cannot be combined with `--no-network`.
:::

## Host clipboard

The `--clipboard` flag starts a short-lived host proxy and exposes text clipboard command shims inside the container: `pbcopy`, `pbpaste`, `xclip`, `xsel`, `wl-copy`, and `wl-paste`.

This makes text copy/paste operations from tools such as Codex and Claude Code reach the host clipboard.

::: warning
`--clipboard` cannot be combined with `--no-network`, and it intentionally creates a host-write channel from inside the container.
:::

## Project file filtering

Use `--exclude` when you want the container to see an empty placeholder instead of the real project file or directory:

```bash
yolobox claude --readonly-project --exclude ".env*" --exclude "secrets/**"
```

Use `--copy-as` when you want to substitute one file for another project path inside the staged readonly project view:

```bash
yolobox claude --readonly-project --exclude ".env*" --copy-as ".env.sandbox:.env"
```

- exclude globs are relative to the project root
- `**` matches recursively
- `copy-as` destinations must stay inside the project and already exist as files
- if both flags target the same path, `copy-as` wins
- both flags currently require `--readonly-project`
- both flags are incompatible with `--no-project`

::: warning
`--exclude` and `--copy-as` are currently supported on Docker and Podman only. Apple's `container` runtime does not support them yet.
:::

## Skipping the automatic project mount

Use `--no-project` when yolobox is running somewhere its current working directory is not visible to the Docker or Podman daemon, such as some Docker-in-Docker and remote-daemon setups.

```bash
yolobox run --no-project \
  --mount /host/path/to/project:/workspace \
  --runtime-arg=--workdir=/workspace \
  bash
```

This disables the default project mount, default workdir, and `$YOLOBOX_PROJECT_PATH`. The caller is responsible for providing any mounts and workdir the command needs.

## Derived image customization

These flags map to the same model described in [Project-Level Customization](/customizing):

```bash
yolobox run --packages default-jdk,maven mvn --version
yolobox run --customize-file .yolobox.Dockerfile bash
yolobox run --packages default-jdk --rebuild-image java --version
```

Use them when you want a one-off customization without writing config first.

## Raw runtime passthrough {#advanced}

Anything not covered by a dedicated flag can still be forwarded with `--runtime-arg`:

```bash
yolobox run \
  --runtime-arg "--ulimit" \
  --runtime-arg "nofile=4096:8192" \
  --runtime-arg "--security-opt" \
  --runtime-arg "seccomp=unconfined" \
  claude
```

Docker and Podman accept these passthrough flags unchanged. Apple's `container` runtime ignores options it does not understand.
