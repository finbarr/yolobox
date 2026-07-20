# Flags

::: tip
Flags go after the subcommand: `yolobox run --flag cmd` or `yolobox claude --flag`, not `yolobox --flag run cmd`.
:::

## Runtime & image

| Flag | Description | Incompatible with |
|------|-------------|-------------------|
| `--runtime <name>` | Use `docker`, `podman`, or `container` | |
| `--image <name>` | Override the base image | |
| `--platform <value>` | Container platform, e.g. `linux/amd64`; persistent volumes are kept per architecture | Apple `container` |
| `--name <name>` | Assign a runtime container name | |
| `--packages <list>` | Comma-separated apt packages for a derived custom image | Apple `container` |
| `--customize-file <path>` | Dockerfile fragment for a derived custom image | Apple `container` |
| `--rebuild-image` | Force rebuild of the derived custom image | Apple `container` |
| `--ensure-latest` | Force-pull the configured base image before running, then rebuild any derived image on top | |

### `--ensure-latest` vs `--rebuild-image` vs `yolobox upgrade`

These target different layers:

- `--ensure-latest` — force-pulls the **configured base** image from its registry
  for this run (even if a copy already exists locally), then rebuilds the derived
  custom image on top if your project has customization. The base it pulls is
  whatever `--image` / `image` resolves to (default `ghcr.io/finbarr/yolobox:latest`):
  with the default tag this fetches the newest image; with a pinned tag it re-pulls
  that exact tag (a no-op unless it was re-pushed, so it never moves past the pin);
  with a fully custom `--image` it pulls that image rather than the yolobox one. It
  does not touch the yolobox binary.
- `--rebuild-image` — rebuilds only the **derived** custom image (packages /
  Dockerfile fragment). It does not refresh the base image, and has no effect
  without customization.
- `yolobox upgrade` — updates the yolobox **binary** and pulls the latest base
  image. This is the full update path; `--ensure-latest` is the per-run,
  image-only subset.

## Filesystem, config, and identity

| Flag | Description | Incompatible with |
|------|-------------|-------------------|
| `--mount <src:dst>` | Extra mount, repeatable | |
| `--exclude <glob>` | Hide matching project paths from the container, repeatable | Apple `container`, `--no-project`, without `--readonly-project` |
| `--copy-as <src:dst>` | Mount a file at another project path inside the container, repeatable | Apple `container`, `--no-project`, without `--readonly-project` |
| `--env <KEY=val>` | Extra environment variable, repeatable | |
| `--no-env-passthrough` | Disable automatic host environment passthrough | |
| `--setup` | Run interactive setup before starting | |
| `--ssh-agent` | Forward SSH agent socket | |
| `--readonly-project` | Mount the project read-only and write outputs to `/output` | `--no-project` |
| `--no-project` | Skip the automatic project mount; caller provides `--mount` and `--runtime-arg=--workdir` | `--readonly-project`, `--exclude`, `--copy-as` |
| `--claude-config` | Copy host `~/.claude` config into the container | |
| `--codex-config` | Sync host `~/.codex` config and live-mount sessions | |
| `--gemini-config` | Copy host `~/.gemini` Gemini/Antigravity config into the container | |
| `--kimi-config` | Sync host `~/.kimi-code` config, credentials, skills, and sessions into the container | |
| `--opencode-config` | Copy host `~/.config/opencode` config into the container | |
| `--pi-config` | Copy host `~/.pi/agent` config into the container | |
| `--git-config` | Copy host `~/.gitconfig` into the container | |
| `--gh-token` | Forward GitHub token for `gh` and HTTPS Git auth from `gh auth token` | |
| `--rtk` | Enable RTK command-output compression for supported AI CLIs | |
| `--copy-agent-instructions` | Copy global instruction files and skills into the container | |
| `--clipboard` | Bridge text clipboard copy/paste between the container and host | `--no-network` |
| `--open-bridge` | Bridge `open`/`xdg-open` HTTP(S) URLs to the host browser | `--no-network` |

## Networking and behavior

| Flag | Description | Incompatible with |
|------|-------------|-------------------|
| `--no-network` | Disable network access | `--network`, `--pod`, `--docker`, `--clipboard`, `--open-bridge` |
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

## Environment passthrough

yolobox automatically passes a short list of common API/token environment variables when they exist on the host, plus `TERM`, `LANG`, and detected `TZ` for terminal usability.

Use `--no-env-passthrough` to disable those automatic host-derived environment variables. Explicit `--env KEY=value` entries still pass through, and `--gh-token` still forwards a GitHub token when requested.

For project-specific variables, put the same values in `.yolobox.toml`:

```toml
env = ["CODEX_HOME=/home/yolo/.codex-account"]
```

The value is passed directly into the container process. Use container paths rather than host-only paths or shell shortcuts such as `~`.

## RTK command compression

The `--rtk` flag enables RTK command-output compression for supported AI shortcuts. yolobox runs RTK init inside the container for Claude, Codex, Gemini, or OpenCode after any host config sync, so copied host config does not overwrite the RTK hooks.

RTK is installed in the base image at image build time using the latest available RTK release. yolobox does not auto-update RTK during container startup; pull or rebuild the yolobox image to pick up newer RTK releases.

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

## Host URL open bridge

The `--open-bridge` flag starts a short-lived host proxy and exposes `open` and `xdg-open` command shims inside the container.

The bridge only accepts `http://` and `https://` URLs and asks the host OS to open them in the default browser.

::: warning
`--open-bridge` cannot be combined with `--no-network`, and it intentionally creates a host browser action channel from inside the container.
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
