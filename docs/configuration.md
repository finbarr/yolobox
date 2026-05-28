# Configuration

## Interactive setup

Run `yolobox setup` to write global defaults to `~/.config/yolobox/config.toml`.

## Config files

### Global config

Path: `~/.config/yolobox/config.toml`

Applies to all projects:

```toml
# mode = "remote"
# remote_name = "foo"
default_harness = "codex"
git_config = true
opencode_config = true
pi_config = true
gh_token = true
rtk = true
ssh_agent = true
docker = true
clipboard = true
open_bridge = true
network = "my_compose_network"
# no_network = true # incompatible with network, pod, docker, clipboard, and open_bridge
no_env_passthrough = true
no_yolo = true
cpus = "4"
memory = "8g"
cap_add = ["SYS_PTRACE"]
devices = ["/dev/kvm:/dev/kvm"]
runtime_args = ["--security-opt", "seccomp=unconfined"]

[remote]
backend_url = "https://api.yolobox.dev"
# Browser-granted Better Auth session token written by yolobox login. Prefer YOLOBOX_TOKEN in scripts.
token = "your-session-token"
ssh_user = "root"
setup = ["docker compose pull"]
```

### Project config

Path: `.yolobox.toml`

Place in your project root for project-specific settings:

```toml
default_harness = "none"
mounts = ["../shared-libs:/libs:ro"]
env = ["DEBUG=1"]
readonly_project = true
container_name = "project-yolobox"
exclude = [".env*", "secrets/**"]
copy_as = [".env.sandbox:.env"]
no_network = true
no_env_passthrough = true
shm_size = "2g"

[customize]
packages = ["default-jdk", "maven"]
```

### Precedence

CLI flags > project config > global config > defaults

Use `container_name` or `--name` only when you need a stable runtime container name for inspection or integration. Fixed names cannot run concurrently; Docker, Podman, or Apple container will reject a second live container with the same name.

## Default harness

Set `default_harness` to one AI shortcut name to make bare `yolobox` launch that tool:

```toml
default_harness = "codex"
```

Valid values are `claude`, `codex`, `gemini`, `opencode`, `copilot`, and `none`. Use `none` in project config to override a global default harness and keep bare `yolobox` as an interactive shell. `yolobox shell` always opens a shell regardless of this setting.

## Remote defaults

Set `mode = "remote"` and `remote_name` when bare `yolobox` should use a named remote machine instead of starting a local container.

```toml
mode = "remote"
remote_name = "foo"
default_harness = "codex"

[remote]
# Defaults to https://api.yolobox.dev when omitted.
backend_url = "https://remote.example.com"
# Browser-granted Better Auth session token written by yolobox login. Prefer YOLOBOX_TOKEN in scripts.
token = "your-session-token"
ssh_user = "root"
setup = ["docker compose pull"]
```

With that config, bare `yolobox` behaves like `yolobox remote run foo codex`.

Remote mode requires a Better Auth session from `remote.token`, `YOLOBOX_TOKEN`, or `yolobox login`. Plain `yolobox login` opens a browser approval flow, prints the URL for copy/paste, and stores the granted session token. The CLI asks the hosted or self-hosted backend for a bootstrapped host owned by the authenticated user, fetches backend-issued tunnel SSH credentials, and mirrors the current folder to `/opt/yolobox/project` with `rsync`. The backend/VM agent maps the original local source path, runs setup commands, resolves command wrappers, and manages the single tmux session. Remote commands run directly on the VM with yolobox wrappers on `PATH`; they do not start a nested yolobox container. The backend is the source of truth for machine state; the CLI stores auth/config only and asks the backend for list, status, create, destroy, and connect metadata. The hosted browser app is intended to run at `https://app.yolobox.dev` against `https://api.yolobox.dev`.

Remote sync copies the whole current folder on `sync up`. That includes `.git` if present, uncommitted files, ignored files, `.env` files, dependency folders, build output, and local caches. `sync down` copies the remote project back to the local folder and requires `--force` because it can overwrite local files.

## Project file filtering

Use project config when you want a repo to carry its own sandboxed view:

```toml
exclude = [".env*", "secrets/**"]
copy_as = [".env.sandbox:.env"]
```

- `exclude` globs are relative to the project root and support `**`
- `copy_as` sources can be relative or absolute host paths
- `copy_as` destinations must stay inside the project and already exist as files
- `copy_as` takes precedence if it targets the same path as `exclude`
- both options currently require `readonly_project = true` or `--readonly-project`
- both options are incompatible with `no_project = true` or `--no-project`
- Apple's `container` runtime does not support this feature yet

## Skipping the automatic project mount

Set `no_project = true` only in advanced environments where yolobox's current working directory is not visible to the Docker or Podman daemon. In that mode, provide the mount and workdir explicitly:

```toml
no_project = true
mounts = ["/host/path/to/project:/workspace"]
runtime_args = ["--workdir=/workspace"]
```

`no_project = true` cannot be combined with `readonly_project`, `exclude`, or `copy_as`.

## Customization config

Project-level image customization lives under `[customize]`:

```toml
[customize]
packages = ["default-jdk", "maven"]
dockerfile = ".yolobox.Dockerfile"
```

Use `packages` for apt installs. Use `dockerfile` when you need extra build logic on top of that.

## Runtime args format

Each `runtime_args` entry is a single CLI argument. For flags that take a value, add them as separate entries:

```toml
runtime_args = ["--security-opt", "seccomp=unconfined"]
```

## Host clipboard

Set `clipboard = true` or pass `--clipboard` to bridge text clipboard copy/paste between the container and the host. yolobox starts a short-lived host proxy for the session and exposes clipboard command shims inside the container: `pbcopy`, `pbpaste`, `xclip`, `xsel`, `wl-copy`, and `wl-paste`.

`clipboard = true` cannot be combined with `no_network = true`.

## Host URL open bridge

Set `open_bridge = true` or pass `--open-bridge` to bridge URL opening from the container to the host. yolobox starts a short-lived host proxy for the session and exposes `open` and `xdg-open` shims inside the container.

The bridge only accepts `http://` and `https://` URLs and asks the host OS to open them in the default browser. `open_bridge = true` cannot be combined with `no_network = true`.

## RTK command compression

Set `rtk = true` or pass `--rtk` to enable RTK command-output compression for supported AI shortcuts. yolobox installs the latest RTK release available when the base image is built, then runs RTK init inside the container for Claude, Codex, Gemini, or OpenCode after any host config sync.

yolobox does not auto-update RTK at startup. To get a newer RTK release, rebuild or pull a newer yolobox image. Copilot and Pi are not auto-initialized because RTK does not currently provide a matching non-project config hook for them.

## Global agent instructions {#global-agent-instructions}

The `--copy-agent-instructions` flag copies your global or user-level instruction files and skills into the container.

Files copied if they exist on your host:

| Tool | Source | Destination |
|------|--------|-------------|
| Claude | `~/.claude/CLAUDE.md` | `/home/yolo/.claude/CLAUDE.md` |
| Claude skills | `~/.claude/skills/` | `/home/yolo/.claude/skills/` |
| Gemini | `~/.gemini/GEMINI.md` | `/home/yolo/.gemini/GEMINI.md` |
| Codex | `~/.codex/AGENTS.md` | `/home/yolo/.codex/AGENTS.md` |
| Codex skills | `~/.codex/skills/` | `/home/yolo/.codex/skills/` |
| Pi | `~/.pi/agent/AGENTS.md` | `/home/yolo/.pi/agent/AGENTS.md` |
| Pi skills | `~/.pi/agent/skills/` | `/home/yolo/.pi/agent/skills/` |
| Copilot | `~/.copilot/agents/` | `/home/yolo/.copilot/agents/` |

This copies instruction files and skills, not full configs, credentials, settings, or history. For full tool configs, use `--claude-config`, `--codex-config`, `--gemini-config`, `--opencode-config`, or `--pi-config`.

## Auto-forwarded environment variables

These are automatically passed into the container if they are set on the host:

- `ANTHROPIC_API_KEY`
- `CLAUDE_CODE_OAUTH_TOKEN`
- `OPENAI_API_KEY`
- `COPILOT_GITHUB_TOKEN` / `GH_TOKEN` / `GITHUB_TOKEN`
- `OPENROUTER_API_KEY`
- `GEMINI_API_KEY`
- `AZURE_OPENAI_API_KEY`
- `CEREBRAS_API_KEY`
- `DEEPSEEK_API_KEY`
- `FIREWORKS_API_KEY`
- `GROQ_API_KEY`
- `KIMI_API_KEY`
- `MINIMAX_API_KEY`
- `MISTRAL_API_KEY`
- `XAI_API_KEY`
- `ZAI_API_KEY`
- `AI_GATEWAY_API_KEY`

Set `no_env_passthrough = true` or pass `--no-env-passthrough` to disable all automatic host environment passthrough. This suppresses the API/token list above plus `TERM`, `LANG`, and detected `TZ`; explicit `env = [...]` config and `--env KEY=value` still pass through, and `gh_token = true` or `--gh-token` still forwards a GitHub token when requested.

::: tip macOS and GitHub tokens
On macOS, `gh` stores tokens in Keychain, not environment variables. Use `--gh-token` or `gh_token = true` if you want yolobox to extract and forward the GitHub token. When a token is present, yolobox also configures HTTPS Git auth for `github.com` remotes.
:::

## Runtime context manifest

Every yolobox session provides a runtime manifest at `/run/yolobox/context.json` and sets `YOLOBOX_CONTEXT_FILE` to that path.

The manifest is intended for agents and scripts running inside the container. It exposes the resolved runtime and launch context in JSON, including an `inside_yolobox` confirmation, the effective config, container paths, launch command, fork metadata when `yolobox fork` is active, and the keys of forwarded environment variables without copying their values into the manifest.

The canonical skill packages live under [`skills/`](https://github.com/finbarr/yolobox/tree/master/skills):

- [`skills/yolobox`](https://github.com/finbarr/yolobox/tree/master/skills/yolobox) is the inside-the-box skill that orients the agent to the trusted yolobox sandbox it is running in, then uses this manifest to explain the current sandbox accurately. Its `Readonly project mode` line reports the launch mode; its `Project writable now` line is a live filesystem check. yolobox currently installs it for Claude and Codex sessions inside the container.
- [`skills/yolobox-orchestrator`](https://github.com/finbarr/yolobox/tree/master/skills/yolobox-orchestrator) is the host-side skill for agents that need to launch or control yolobox itself.

yolobox also injects a managed guidance block into `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md` so those agents know to use the `yolobox` skill when current sandbox assumptions matter.

## Config sync warning

::: warning
Setting `claude_config = true`, `codex_config = true`, `gemini_config = true`, `opencode_config = true`, or `pi_config = true` in config syncs your host config on every container start. Claude, Gemini, OpenCode, and Pi config sync replaces the matching in-container config directory, overwriting changes made inside the container. Codex config sync incrementally merges durable host files into `~/.codex`, skips volatile Codex log, state, cache, and temp files, preserves a valid in-container `auth.json` when the host copy has no usable auth file, and live-mounts host Codex sessions so resume history stays current without copying it. Prefer `--claude-config`, `--codex-config`, `--gemini-config`, `--opencode-config`, or `--pi-config` for one-time syncs.
:::

## Startup timing diagnostics

Set `YOLOBOX_TIMING=1` to print host-side and container-entrypoint timing markers:

```bash
YOLOBOX_TIMING=1 yolobox run true
```

This is useful when diagnosing slow config sync, Docker startup, update checks, or runtime argument construction.

yolobox removes a zero-byte `/home/yolo/.codex/auth.json` during startup. Recent Codex versions fail with `EOF while parsing a value` when that stale file exists; removing it lets Codex recreate auth normally or show the sign-in flow.

If Codex auth fails with `No space left on device`, the Docker or Podman storage backing `/home/yolo` or `/tmp` is full. Check `docker system df` or the equivalent for your runtime, then reclaim runtime storage or increase the VM disk size. yolobox warns at container startup when those paths are nearly full, but it does not automatically prune unrelated images, volumes, or build cache.
