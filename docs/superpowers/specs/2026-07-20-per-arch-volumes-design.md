# Per-architecture persistent volumes

Date: 2026-07-20
Status: approved

## Problem

All yolobox runs share the named volumes `yolobox-home` (`/home/yolo`),
`yolobox-cache` (`/var/cache`), and `yolobox-output` (`/output`, readonly-project
mode) regardless of container architecture. On the same Docker daemon, an
x86-emulated run (e.g. on Apple Silicon) and a native arm64 run therefore share
`/home/yolo`: tools installed under one architecture are broken binaries under
the other.

## Goal

Runs of different container architectures on the same daemon get separate
persistent volumes, without losing existing users' data on upgrade.

## Design

### New `--platform` flag and `platform` config key

- CLI flag `--platform <value>` and TOML key `platform` (global and
  per-project config, merged like other string options; flag wins).
- Accepts Docker platform syntax (`linux/amd64`) or a bare architecture
  (`amd64`).
- When set, the value is passed through as `--platform <value>` to:
  - `docker run` / `podman run`
  - image pulls (`--ensure-latest`, `upgrade`)
  - custom-image builds (`docker build --platform`)
- The Apple `container` runtime does not accept `--platform`; setting the
  option with that runtime is an error.

### Architecture resolution (for volume naming)

A single effective platform is resolved and used consistently for the run,
image pulls, custom-image builds, and volume selection:

1. `--platform` flag / `platform` config, or a `--platform` value already
   present in `runtime_args`. If both are set they must agree (after
   normalization); conflicting values are an error.
2. `DOCKER_DEFAULT_PLATFORM` environment variable
3. Native host architecture (`runtime.GOARCH`)

The architecture component is extracted from the platform string (`linux/amd64`
→ `amd64`) and normalized: `x86_64` → `amd64`, `aarch64` → `arm64`. Variant
suffixes (`linux/arm/v7`) keep the arch component only (`arm`).

### Volume naming

- Resolved arch equals the native host arch (`runtime.GOARCH`): keep the
  legacy unsuffixed names (`yolobox-home`, `yolobox-cache`, `yolobox-output`).
  Existing data survives the upgrade.
- Any other arch: suffix with the normalized arch — `yolobox-home-amd64`,
  `yolobox-cache-amd64`, `yolobox-output-amd64`.

Resolution is deterministic per host: the same command always maps to the same
volumes.

### `reset`

- `yolobox reset --force` (default): wipes **all** yolobox persistent volumes
  across every architecture. Volumes are discovered by querying the runtime's
  volume list (`volume ls`, names only) and matching
  `^yolobox-(home|cache|output)(-[a-z0-9]+)?$`. If the query fails, fall back
  to removing the legacy fixed list. This also fixes `reset` previously not
  removing `yolobox-output`.
- New `yolobox reset --force --platform <value>`: narrows the wipe to the
  volumes that a run with the same `--platform` value would use (same parsing
  and normalization). `--platform arm64` on an arm64 host therefore targets
  the legacy unsuffixed names.

### `uninstall`

Unchanged semantics: removes all yolobox volumes unless `--keep-volumes`.
Uses the same discovery-by-matching as `reset` so arch-suffixed volumes are
cleaned up too.

## Components

- `runtime_support.go` (or a new `platform.go`): platform parsing,
  normalization, arch resolution, `volumeNameForArch(base, arch)`.
- `config.go`: `Platform` field, merge + print + save support.
- `main.go`: flag parsing, `--platform` on the run command line, suffixed
  volume names in `buildRunArgs`, Apple-container error.
- `custom_image.go`: `--platform` on pull and build.
- `maintenance.go`: volume discovery + matching for `reset`/`uninstall`,
  `reset --platform`.

## Error handling

- Unparseable platform values (empty arch component) are an error at startup.
- Apple `container` runtime + `platform` set: error with a clear message.
- Volume discovery failure during `reset`/`uninstall`: fall back to the legacy
  fixed name list; `uninstall` continues best-effort as today.

## Testing

Unit tests following existing `main_test.go` patterns:

- Platform parsing/normalization (`linux/amd64`, `amd64`, `x86_64`,
  `aarch64`, `linux/arm/v7`, invalid values).
- Arch resolution precedence (flag > runtime_args > env > native).
- Volume naming: native → legacy names, non-native → suffixed.
- `buildRunArgs`: emits `--platform` and the correct `-v` volume names.
- Reset volume matching: which discovered names are removed, with and without
  `--platform`.

## Docs

- `docs/commands.md` (reset examples), configuration docs, `--help` text,
  CHANGELOG entry.
