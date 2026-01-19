# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
make build          # Build the yolobox binary
make test           # Run unit tests
make lint           # Run go vet (and golangci-lint if installed)
make image          # Build the Docker base image
make smoke-test     # Run smoke tests on container tools
make install        # Build and install to ~/.local/bin
make clean          # Remove built binary
```

## Versioning & Releases

See the [Development section in README.md](README.md#development) for versioning policy and release process.

**TL;DR:** Tag and push. No files to edit.
```bash
git tag v0.1.2
git push origin master --tags
```

## Workflow

**Always commit changes after completing work.** Don't leave uncommitted changes - if you modified files, commit them before finishing.

## Verification

After making changes, run this verification sequence:

### Quick verification (after code changes)
```bash
make clean && make build && make test && ./yolobox version
```

### Full verification (after Dockerfile or significant changes)
```bash
# 1. Build and unit tests
make clean && make build && make test

# 2. CLI smoke tests
./yolobox version
./yolobox help
./yolobox config

# 3. Rebuild image (if Dockerfile changed)
make image

# 4. Container functionality tests
./yolobox run echo "hello"              # Basic execution
./yolobox run whoami                    # Should output: yolo
./yolobox run pwd                       # Should output: /workspace

# 5. Security tests
./yolobox run ls /host-home             # Should fail (not mounted)
./yolobox run env | grep YOLOBOX        # Should show: YOLOBOX=1

# 6. Pre-installed tools
./yolobox run node --version            # Node.js
./yolobox run bun --version             # Bun
./yolobox run python3 --version         # Python
./yolobox run go version                # Go
./yolobox run uv --version              # uv (Python package manager)
./yolobox run claude --version          # Claude Code
./yolobox run gemini --version          # Gemini CLI
./yolobox run codex --version           # OpenAI Codex
./yolobox run opencode --version        # OpenCode
./yolobox run copilot --version         # GitHub Copilot CLI
./yolobox run gh --version              # GitHub CLI
./yolobox run fd --version              # fd (find replacement)
./yolobox run rg --version              # rg (ripgrep, grep replacement)
./yolobox run bat --version             # bat (cat with syntax highlighting)
./yolobox run eza --version             # eza (modern ls replacement)

# 7. Flag tests (flags go AFTER subcommand)
./yolobox run --env FOO=bar bash -c 'echo $FOO'           # Should output: bar
./yolobox run --no-network curl https://google.com        # Should fail
./yolobox run --readonly-project touch /workspace/x       # Should fail

# 8. API key passthrough
ANTHROPIC_API_KEY=test ./yolobox run printenv ANTHROPIC_API_KEY  # Should output: test

# 9. Claude config sharing (opt-in with --claude-config)
./yolobox run --claude-config ls /home/yolo/.claude   # Should show copied host claude config

# 10. Git config sharing (opt-in with --git-config)
./yolobox run --git-config cat /home/yolo/.gitconfig  # Should show copied host git config
```

## Architecture

yolobox is a single-binary Go CLI that runs AI coding agents (Claude Code, Codex, etc.) inside a container sandbox. The host home directory is protected by default.

### Code Structure

All code lives in `cmd/yolobox/main.go` (~700 lines):

- **Config struct** - TOML config with runtime, image, mounts, secrets, env, resource limits, network/readonly flags
- **loadConfig()** - Merges global (`~/.config/yolobox/config.toml`) + project (`.yolobox.toml`) + CLI flags
- **buildRunArgs()** - Constructs docker/podman run arguments
- **resolveRuntime()** - Auto-detects docker or podman
- **Color helpers** - `success()`, `info()`, `warn()`, `errorf()` for colorful output

### Key Design Decisions

- Single file keeps it auditable and simple
- Named volumes (`yolobox-home`, `yolobox-cache`, `yolobox-tools`) persist across runs
- Auto-passthrough of common API keys (ANTHROPIC_API_KEY, OPENAI_API_KEY, etc.)
- Container user is `yolo` with passwordless sudo
- Flags are parsed per-subcommand (e.g., `yolobox run --env FOO=bar cmd`, not `yolobox --env FOO=bar run cmd`)

### Container Behavior

- Project mounted at `/workspace` (read-write by default, read-only with `--readonly-project`)
- `/output` volume created when using `--readonly-project`
- Sets `YOLOBOX=1` env var inside container
- Runs as `yolo` user with full sudo access
- Host home is NOT mounted (use `--mount ~:/host-home` if you really need it)
- Host `~/.claude` can be copied to container with `--claude-config` flag (or `claude_config = true` in config)

## Hard-Won Learnings

Document solutions here when something takes multiple attempts to figure out.

- **SIGKILL in docker build but not docker run?** Use multi-stage build. Memory accumulates across layers; isolate heavy installers in a separate stage and COPY the result.
- **Claude Code config lives in TWO places**: `~/.claude/` (settings, history) AND `~/.claude.json` (onboarding state, preferences). Mount both.
- **Claude Code needs writable config**: Can't mount `~/.claude` read-only; Claude writes to it at runtime. Solution: mount to staging area (`/host-claude/`) and copy on container start via entrypoint.
- **OAuth tokens on macOS are in Keychain**: Can't copy them to container. On Linux, Claude stores creds in `~/.claude/.credentials.json`. Users must either use API key or `/login` inside container.
- **Colima defaults to 2GB RAM**: Claude Code gets OOM killed. Need 4GB+. yolobox now warns if Docker has < 4GB.
- **Named volumes shadow image contents**: The `yolobox-home` volume mounts over `/home/yolo`, so new files added to the image's `/home/yolo` won't appear for existing users. Solution: put configs in `/etc/` if they must be visible without volume deletion.
