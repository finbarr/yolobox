# Configuration

## Interactive setup

Run `yolobox setup` to write global defaults to `~/.config/yolobox/config.toml`.

## Config files

### Global config

Path: `~/.config/yolobox/config.toml`

Applies to all projects:

```toml
git_config = true
gh_token = true
ssh_agent = true
docker = true
no_network = true
network = "my_compose_network"
no_yolo = true
cpus = "4"
memory = "8g"
cap_add = ["SYS_PTRACE"]
devices = ["/dev/kvm:/dev/kvm"]
runtime_args = ["--security-opt", "seccomp=unconfined"]
```

### Project config

Path: `.yolobox.toml`

Place in your project root for project-specific settings:

```toml
mounts = ["../shared-libs:/libs:ro"]
env = ["DEBUG=1"]
readonly_project = true
exclude = [".env*", "secrets/**"]
copy_as = [".env.sandbox:.env"]
no_network = true
shm_size = "2g"

[customize]
packages = ["default-jdk", "maven"]
```

### Precedence

CLI flags > project config > global config > defaults

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
- Apple's `container` runtime does not support this feature yet

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
| Copilot | `~/.copilot/agents/` | `/home/yolo/.copilot/agents/` |

This copies instruction files and skills, not full configs, credentials, settings, or history. For full tool configs, use `--claude-config`, `--codex-config`, or `--gemini-config`.

## Auto-forwarded environment variables

These are automatically passed into the container if they are set on the host:

- `ANTHROPIC_API_KEY`
- `CLAUDE_CODE_OAUTH_TOKEN`
- `OPENAI_API_KEY`
- `COPILOT_GITHUB_TOKEN` / `GH_TOKEN` / `GITHUB_TOKEN`
- `OPENROUTER_API_KEY`
- `GEMINI_API_KEY`

::: tip macOS and GitHub tokens
On macOS, `gh` stores tokens in Keychain, not environment variables. Use `--gh-token` or `gh_token = true` if you want yolobox to extract and forward the GitHub CLI token.
:::

## Runtime context manifest

Every yolobox session mounts a runtime manifest at `/run/yolobox/context.json` and sets `YOLOBOX_CONTEXT_FILE` to that path.

The manifest is intended for agents and scripts running inside the container. It exposes the resolved runtime and launch context in JSON, including an `inside_yolobox` confirmation, the effective config, container paths, launch command, and the keys of forwarded environment variables without copying their values into the manifest.

The canonical skill packages live under [`skills/`](https://github.com/finbarr/yolobox/tree/master/skills):

- [`skills/yolobox`](https://github.com/finbarr/yolobox/tree/master/skills/yolobox) is the inside-the-box skill that orients the agent to the trusted yolobox sandbox it is running in, then uses this manifest to explain the current sandbox accurately. Its `Readonly project mode` line reports the launch mode; its `Project writable now` line is a live filesystem check. yolobox currently installs it for Claude and Codex sessions inside the container.
- [`skills/yolobox-orchestrator`](https://github.com/finbarr/yolobox/tree/master/skills/yolobox-orchestrator) is the host-side skill for agents that need to launch or control yolobox itself.

yolobox also injects a managed guidance block into `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md` so those agents know to use the `yolobox` skill when current sandbox assumptions matter.

## Config sync warning

::: warning
Setting `claude_config = true`, `codex_config = true`, or `gemini_config = true` in config copies your host config on every container start. That can overwrite changes made inside the container, including auth and history. Prefer `--claude-config`, `--codex-config`, or `--gemini-config` for one-time syncs.
:::

yolobox removes a zero-byte `/home/yolo/.codex/auth.json` during startup. Recent Codex versions fail with `EOF while parsing a value` when that stale file exists; removing it lets Codex recreate auth normally or show the sign-in flow.
