# Setting Container Env Vars From Differently Named Host Vars

**Date:** 2026-07-19 (revised 2026-07-21 after two review rounds on PR #59)
**Status:** Approved

## Problem

There is no way to give the container a variable whose value comes from a
*differently named* host variable. `env = ["GH_TOKEN=$RO_TOKEN"]` passes the
literal string `$RO_TOKEN` into the container: yolobox invokes the container
runtime via `exec` (no shell), and Docker does not expand `$VAR` in
`-e KEY=value` either.

The motivating case: the host shell has a read-write `GH_TOKEN`, and the
sandbox should get a read-only token instead — without hard-coding the token
into `.yolobox.toml`.

## Design

A dedicated, opt-in surface: `env_from_host = ["KEY=HOST_VAR"]` in config and
`--env-from-host KEY=HOST_VAR` on the CLI. Each entry sets the container
variable `KEY` to the host's value of `HOST_VAR`.

Semantics:

- Both sides are plain environment variable names matching
  `^[a-zA-Z_][a-zA-Z0-9_]*$`. No `$`, no `${}`, no substrings.
- An alias **owns** its container variable. Nothing else may set the same key,
  and the guarantee never depends on the runtime's duplicate-`-e` precedence.
  Concretely:
  - Automatic passthrough (`autoPassthroughEnvVars`) skips an aliased key.
  - `--gh-token` is skipped, with a warning, when `GH_TOKEN` is aliased.
  - Setting the same key in both `env` and `env_from_host` is a validation
    error.
- If `HOST_VAR` is unset on the host, the run **fails closed** with an error
  naming the missing variable. Skipping the entry was the original behavior and
  was rejected in the second review round: the more privileged variable the
  alias exists to replace is forwarded earlier in `buildRunArgs`, so a skipped
  alias silently degraded to exactly the value the user was trying to avoid.
  A host variable that is set but empty is forwarded as empty.
- Malformed entries are rejected at validation time, before the runtime is
  invoked. `KEY=$HOST_VAR` gets a targeted error pointing at the leading `$`,
  since that is the natural thing to try first. Host-variable *presence* is
  checked later, in `buildRunArgs`, so `yolobox config` and `yolobox setup` do
  not require the source variable to exist.
- `env` / `--env` values are unaffected and still pass through verbatim.
- The container-side keys are added to the context manifest's `env_keys`;
  values are never included.

The logic lives in `hostEnvAliasArgs`, `envFromHostKeys`, `envFromHostKeySet`,
`validateEnvFromHost`, and `validateEnvFromHostConflicts` in
`cmd/yolobox/env_from_host.go`, applied in `buildRunArgs`
(`cmd/yolobox/main.go`) and `validateRuntimeOptions`.

## Alternatives considered

- **Expand `$VAR`/`${VAR}` inside `env` values** (the original implementation
  on this branch): rejected in review. Passing every `KEY=value` through
  `os.Expand` silently reinterprets values that already work today —
  `--env 'HASH=$2b$12$example'` reached the container as `HASH=b2`, because Go
  treats `$2` as a shell positional parameter. Passwords, bcrypt hashes, jq
  filters, and shell fragments would break on upgrade.
- **Expand only `${VAR}`, treat bare `$` as literal:** smaller blast radius,
  but still changes the meaning of any existing value containing `${...}`, so
  it is not strictly backward-safe. A new config key has no such risk.
- **A global `expand_env = true` toggle:** rejected — turning it on to alias
  one token silently arms the same footgun for every other entry.
- **Full interpolation (`PATH_EXT=$HOME/bin`):** not implemented. The use case
  is aliasing, and composition can already be done on the host, where the
  user's shell expands the value before yolobox sees it.

## Testing

- Table test for `hostEnvAliasArgs`: aliasing to a different name and the same
  name, multiple entries, unset host var erroring, set-but-empty forwarded.
- Table test for `validateEnvFromHost` via `validateRuntimeOptions`: missing
  `=`, empty key, empty host var, `$`-prefixed and `${}`-wrapped host vars,
  invalid characters on either side, plus the `env` / `env_from_host` key
  conflict.
- `buildRunArgs` test asserting the alias is emitted and that literal `env`
  entries (including `HASH=$2b$12$example`) and key-only entries pass through
  untouched.
- `buildRunArgs` least-privilege test: host `GH_TOKEN=write-token`, `--gh-token`
  set, and `env_from_host = ["GH_TOKEN=..."]` must produce exactly one
  `GH_TOKEN` arg carrying the aliased value, with `write-token` absent from the
  full arg list.
- `buildRunArgs` test asserting an unset alias source is an error naming the
  missing host variable.
- Context manifest test asserting aliased keys appear in `env_keys` without
  their values.
- End-to-end `runCmdArgs` tests against a fake `docker` on PATH, covering a
  `.yolobox.toml` `env_from_host` entry plus a `--env-from-host` flag while a
  privileged host `GH_TOKEN` is set, the rejection of `$`-prefixed host variable
  names, and an unset alias source aborting before the runtime is invoked.
