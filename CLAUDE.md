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

**When adding new flags or config options:**
1. Add the flag to `cmd/yolobox/main.go` (Config struct, parseBaseFlags, mergeConfig, buildRunArgs, printConfig, saveGlobalConfig, runSetup)
2. Update `README.md`:
   - Add to the Flags table
   - Add to the example config.toml if it's a persistent option
   - Add any relevant notes (e.g., in Auto-Forwarded Environment Variables section)
3. Update verification tests in this file if needed

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
./yolobox run pwd                       # Should output: actual project path (e.g., /Users/foo/project)

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
./yolobox run --readonly-project touch testfile           # Should fail (project is read-only)

# 8. API key passthrough
ANTHROPIC_API_KEY=test ./yolobox run printenv ANTHROPIC_API_KEY  # Should output: test

# 9. Claude config sharing (opt-in with --claude-config)
./yolobox run --claude-config ls /home/yolo/.claude   # Should show copied host claude config

# 10. Gemini config sharing (opt-in with --gemini-config)
./yolobox run --gemini-config ls /home/yolo/.gemini   # Should show copied host gemini config

# 11. Git config sharing (opt-in with --git-config)
./yolobox run --git-config cat /home/yolo/.gitconfig  # Should show copied host git config

# 12. Global agent instructions (opt-in with --copy-agent-instructions)
# Creates test file, copies it, then cleans up
mkdir -p ~/.claude && echo "test" > ~/.claude/CLAUDE.md
./yolobox run --copy-agent-instructions cat /home/yolo/.claude/CLAUDE.md  # Should output: test
rm ~/.claude/CLAUDE.md
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

- Project mounted at its **real host path** (e.g., `/Users/foo/project`) for session continuity
- `/output` volume created when using `--readonly-project`
- Sets `YOLOBOX=1` and `YOLOBOX_PROJECT_PATH` env vars inside container
- Runs as `yolo` user with full sudo access
- Host home is NOT mounted (use `--mount ~:/host-home` if you really need it)
- Host `~/.claude` can be copied to container with `--claude-config` flag (or `claude_config = true` in config)

## Hard-Won Learnings

Document solutions here when something takes multiple attempts to figure out.

- **SIGKILL in docker build but not docker run?** Use multi-stage build. Memory accumulates across layers; isolate heavy installers in a separate stage and COPY the result.
- **Claude Code config lives in TWO places**: `~/.claude/` (settings, history) AND `~/.claude.json` (onboarding state, preferences). Mount both.
- **Claude Code needs writable config**: Can't mount `~/.claude` read-only; Claude writes to it at runtime. Solution: mount to staging area (`/host-claude/`) and copy on container start via entrypoint.
- **OAuth tokens on macOS are in Keychain**: Claude stores OAuth creds in Keychain on macOS, not in `~/.claude/`. Solution: `--claude-config` now extracts credentials from Keychain via `security find-generic-password -s "Claude Code-credentials" -w` and mounts them to `/host-claude/.credentials.json`, which the entrypoint copies to `/home/yolo/.claude/.credentials.json`.
- **GitHub CLI tokens on macOS are in Keychain**: `~/.config/gh/hosts.yml` only has user metadata, not the token. Use `gh auth token` to extract it, then pass as `GH_TOKEN` env var. This is what `--gh-token` does.
- **Colima defaults to 2GB RAM**: Claude Code gets OOM killed. Need 4GB+. yolobox now warns if Docker has < 4GB.
- **Named volumes shadow image contents**: The `yolobox-home` volume mounts over `/home/yolo`, so new files added to the image's `/home/yolo` won't appear for existing users. Solution: put configs in `/etc/` if they must be visible without volume deletion.
- **npm install -g fails as yolo user**: Global npm packages install to `/usr/lib/node_modules/` (root-owned). Solution: set `NPM_CONFIG_PREFIX=/home/yolo/.npm-global` and add its `bin/` to PATH. The entrypoint creates the dir for existing named volumes.
