# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
make build          # Build the yolobox binary
make test           # Run tests
make lint           # Run go vet and golangci-lint
make image          # Build the Docker base image
make install        # Build and install to ~/.local/bin
make clean          # Remove built binary
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

### Container Behavior

- Project mounted at `/workspace` (read-write by default, read-only with `--readonly-project`)
- `/output` volume created when using `--readonly-project`
- Sets `YOLOBOX=1` env var inside container
- Runs as `yolo` user with full sudo access

### Testing

```bash
make test                           # Run all tests
go test -v ./cmd/yolobox -run TestName  # Run specific test
```
