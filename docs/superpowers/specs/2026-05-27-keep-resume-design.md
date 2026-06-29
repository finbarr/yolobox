# Keep & Resume Yoloboxes — Design

**Date:** 2026-05-27
**Status:** Draft

## Problem

Today every yolobox runs with `docker run --rm` (cmd/yolobox/main.go:1313), so the
container is destroyed on exit. Cross-run persistence is limited to:

- The project directory (bind-mounted from host)
- Two named volumes: `yolobox-home` (`/home/yolo`) and `yolobox-cache` (`/var/cache`)

Anything else inside the container rootfs — `/tmp`, ad-hoc installed packages
outside `$HOME` or `/var/cache`, files written to system paths — is gone. There
is no way to exit a yolobox and later resume it with the same filesystem state.

## Goals

- Let users exit a yolobox and resume it later with the container's filesystem
  intact (everything inside the rootfs, not just bind mounts and the two existing
  named volumes).
- Keep the change minimal: opt-in flag, no new top-level subcommands in v1.
- Reuse Docker/Podman's native `start` for resume so the implementation stays
  small and the behavior matches existing container-runtime semantics.

## Non-goals

- **Running-process persistence.** The agent process (claude, codex, etc.) is
  killed on exit and re-launched on resume. In-tool session continuity continues
  to flow through each tool's own `--resume` flag.
- **New subcommands** for listing, stopping, or removing kept containers in v1.
  Users use `docker ps -a` / `docker rm` directly.
- **Garbage collection.** Kept containers accumulate until the user removes
  them.
- **Reconciling flag changes on resume.** Create-time flags (mounts, env,
  network, capabilities) are baked into the container at `docker run` time and
  cannot change on resume.
- **Fork integration beyond what falls out for free.** `fork --name X` already
  produces a stable container name, so `--keep` works there too; no fork-specific
  code is added.

## UX

```bash
# Create a persistent container.
yolobox claude --name foo --keep

# … work in the session, exit normally …
# Container is now in "stopped" state, not removed.

# Resume.
yolobox claude --name foo
# yolobox detects an existing container named `foo` and runs `docker start -ai foo`
# instead of `docker run`. The yolobox entrypoint re-executes (UID/GID fixups,
# config sync), then the original command (claude) launches fresh.

# Clean up when done.
docker rm foo
```

### Resume detection

Resume is triggered by the presence of an existing **stopped** container with
the requested `--name`, regardless of whether the user passes `--keep` on the
resume invocation. Three cases:

- **Container does not exist:** create a new one (with or without `--rm`
  depending on `--keep`). Current behavior.
- **Container exists and is stopped:** resume it via `docker start -ai <name>`.
- **Container exists and is running:** error out with "container `foo` is
  already running; attach with `docker attach foo` or exit the other session
  first". v1 does not silently multiplex.

This means `--keep` is only meaningful on **creation**. On resume it's a no-op,
and yolobox prints a notice that create-time flags are preserved.

### Required conditions

- `--name` must be set. Without a stable name there is no way to identify a
  container to resume.
- `--keep` is incompatible with `--scratch` (which exists to mean "fresh
  environment, no persistent volumes"). Error out at flag-parse time.
- `--keep` does not apply to `yolobox run <cmd>` (one-shot semantics). Error out
  if combined.

### Notice on resume

When yolobox resumes an existing container, it prints a single line to stderr:

```
yolobox: resuming existing container `foo` (create-time flags are preserved; flags on this invocation are ignored)
```

This is the v1 mitigation for the silent-flag-change footgun (see Caveats).

## Implementation sketch

Approximate diff, ~40 lines of Go plus a small test.

### Flag

In the shortcut/run flag parsing (around cmd/yolobox/main.go:444):

```go
fs.BoolVar(&keep, "keep", false, "preserve container on exit; resume with the same --name")
```

Add `"keep": true` to the `knownFlags` map in `splitToolArgs`
(cmd/yolobox/main.go:1210) so it isn't forwarded to the inner tool.

Wire `keep` into the config struct as `cfg.Keep`.

### Resume branch

Before the existing `docker run` construction (cmd/yolobox/main.go:1313), add:

```go
if cfg.ContainerName != "" {
    exists, err := containerExists(ctx, cfg.Runtime, cfg.ContainerName)
    if err != nil {
        return err
    }
    if exists {
        running, err := containerRunning(ctx, cfg.Runtime, cfg.ContainerName)
        if err != nil {
            return err
        }
        if running {
            return fmt.Errorf("container `%s` is already running; attach with `%s attach %s` or exit the other session first",
                cfg.ContainerName, cfg.Runtime, cfg.ContainerName)
        }
        fmt.Fprintf(os.Stderr,
            "yolobox: resuming existing container `%s` (create-time flags preserved)\n",
            cfg.ContainerName)
        return execStart(cfg.Runtime, cfg.ContainerName)
    }
}
```

Where:

- `containerExists` runs `<runtime> container inspect <name>` and returns true on
  exit code 0, false on non-zero with no stderr noise, error otherwise.
- `containerRunning` runs `<runtime> container inspect -f '{{.State.Running}}'
  <name>` and parses the boolean output.
- `execStart` runs `<runtime> start -ai <name>` with stdio wired through, same
  pattern as the existing `docker run` exec.

### Conditional `--rm`

At cmd/yolobox/main.go:1313:

```go
args := []string{"run"}
if !cfg.Keep {
    args = append(args, "--rm")
}
```

### Validation

In config validation (around cmd/yolobox/main.go:709 where `--name` is
validated):

```go
if cfg.Keep && cfg.ContainerName == "" {
    return errors.New("--keep requires --name")
}
if cfg.Keep && cfg.Scratch {
    return errors.New("--keep and --scratch are incompatible")
}
```

Reject `--keep` in the `run` subcommand path with a clear error.

## Behavior on resume — what actually persists

- **Container rootfs:** everything written outside bind mounts and named volumes
  (e.g., `/tmp`, packages installed via `apt-get` into system paths, files
  written to `/srv` or `/opt`). This is the new capability.
- **Bind-mounted project:** unchanged; already persisted today.
- **Named volumes** (`yolobox-home`, `yolobox-cache`): unchanged; already
  persisted today.
- **Environment variables set by the entrypoint:** re-applied, because the
  entrypoint re-runs on `docker start`.
- **Config sync** (`--claude-config` et al.): re-runs on `docker start` (the
  entrypoint is idempotent).

What does **not** persist:

- Running processes. The agent re-launches fresh on resume.
- Shell environment / aliases set interactively in the previous session.
- Background jobs.

## Caveats and known limitations

- **Create-time flags win.** `docker start` re-uses the mounts, env, network,
  capabilities, and command baked into the container at creation. If the user
  runs `yolobox claude --name foo --docker` once, then `yolobox claude --name
  foo` later, the second session still has docker socket access. The notice on
  resume is the v1 mitigation; structured validation is deferred.
- **Image upgrades don't reach kept containers.** After `yolobox upgrade`,
  existing kept containers stay on the older image until the user removes them
  and creates fresh ones.
- **No automatic cleanup.** Users must `docker rm` kept containers explicitly.
- **Apple `container` runtime parity unverified.** The existing code already
  branches on `isAppleContainer` for some features; we should verify `container
  start -ai` works equivalently before claiming support. If not, document the
  limitation and gate `--keep` on supported runtimes for v1.

## Testing

Unit tests (extending main_test.go patterns):

- `--keep` without `--name` → validation error.
- `--keep` with `--scratch` → validation error.
- `--keep` in `run` subcommand → validation error.
- `--keep` set → emitted `docker run` args omit `--rm`.
- `--keep` unset → `--rm` is present (regression guard).

Integration tests (manual for v1, document in CONTRIBUTING):

1. `yolobox shell --name keeptest --keep`, `touch /tmp/marker`, `exit`.
2. `yolobox shell --name keeptest`, verify `/tmp/marker` exists, verify resume
   notice was printed.
3. `docker rm keeptest`, `yolobox shell --name keeptest`, verify a fresh
   container is created (no marker).

## Out of scope (future work)

- `yolobox stop <name>` / `yolobox resume <name>` / `yolobox ls` / `yolobox rm
  <name>` wrappers for runtime-agnostic management.
- Validation/diffing of flags between create and resume.
- Auto-removal of stopped containers after a TTL.
- Running-process persistence via `docker pause`/`unpause` (separate feature, if
  ever).
- Detection of image drift and prompting for re-creation after `yolobox
  upgrade`.

## Estimated size

~50 lines of Go in cmd/yolobox/main.go, plus a ~30-line test addition, plus a
short paragraph in docs/commands.md and a CHANGELOG entry. Single PR.
