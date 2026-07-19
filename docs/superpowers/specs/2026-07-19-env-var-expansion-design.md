# Host Environment Variable Expansion in `env` Entries

**Date:** 2026-07-19
**Status:** Approved

## Problem

`env = ["A=$B"]` in `.yolobox.toml` passes the literal string `$B` into the
container. Yolobox invokes the container runtime via `exec` (no shell), and
Docker does not expand `$VAR` in `-e KEY=value` either, so there was no way to
forward a host variable's value under a different name from config.

## Design

Expand `$VAR` and `${VAR}` references in the **value** part of each `KEY=value`
entry in `cfg.Env` against the host environment, at the point where run args
are built (`buildRunArgs` in `cmd/yolobox/main.go`). This applies uniformly to
config-file entries and CLI `--env` entries, matching docker-compose's
env-file expansion behavior.

Semantics (shell-like, implemented with `os.Expand`):

- `$VAR` and `${VAR}` in the value are replaced with the host's value.
- Unset variables expand to an empty string.
- `$$` produces a literal `$` (escape hatch).
- Key-only entries (no `=`, e.g. `env = ["B"]`) are passed to the runtime
  unchanged so the runtime's own host passthrough (`-e NAME`) still applies.
- Keys are never expanded.

The logic lives in `expandEnvEntry` in `cmd/yolobox/env_expand.go`, applied in
the user-specified env loop in `buildRunArgs`.

## Alternatives considered

- **Expand at config-load time:** rejected — `yolobox config` should display
  raw configured values, and CLI entries merge after load, so point-of-use
  expansion is both simpler and uniform.
- **Expand only config-file entries, not CLI:** rejected — inconsistent
  semantics for the same `cfg.Env` list; the CLI shell normally expands
  unquoted `$VAR` anyway, so double expansion only affects deliberately
  quoted values, and `$$` covers the literal case.

## Testing

- Unit table test for `expandEnvEntry` covering both syntaxes, mixed
  literals, multiple refs, unset vars, `$$` escape, key-only entries,
  trailing `$`, and empty values.
- Integration test asserting `buildRunArgs` emits expanded `-e` args and
  leaves key-only entries untouched.
- End-to-end run of the real binary against a fake `docker` on PATH,
  verifying `--env 'A=$HOST_SECRET'`, `--env 'LIT=$$HOST_SECRET'`, key-only
  forwarding, and a `.yolobox.toml` `env` entry with `${VAR}` syntax.
