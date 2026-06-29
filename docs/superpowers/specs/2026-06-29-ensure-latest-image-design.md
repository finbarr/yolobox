# Design: `--ensure-latest` flag

**Date:** 2026-06-29
**Status:** Draft — pending user review

## Problem

A user starting yolobox in a project folder wants a simple way to guarantee the
image they are about to run is current. The existing controls don't serve this:

- **Nothing refreshes a present-but-stale base image on a normal run.** The base
  image is referenced as `ghcr.io/finbarr/yolobox:latest` (`config.go:78`).
  `inspectImageID` (`custom_image.go:111`) only pulls when the image is *missing*
  locally, so a local `:latest` pulled weeks ago is reused indefinitely. Only
  `yolobox upgrade` force-pulls it (`maintenance.go:242`), and that also updates
  the binary.
- **`--rebuild-image` doesn't fill the gap.** It rebuilds only the *derived*
  custom layer, and only when the project has customization (`[customize]`
  packages or a Dockerfile fragment). In a folder with no customization it does
  nothing. It never refreshes the base image.

There is no per-run, intent-level control that means "make the image I'm about to
run as current as possible." `--ensure-latest` adds it.

## Goal

- Give users a single opt-in flag that makes the about-to-run image current.

## Non-goals

- Changing the meaning of `--rebuild-image` (it stays "rebuild the derived
  custom layer"). Its no-op-without-customization behavior is left as-is.
- Folding binary updates into the new flag (`yolobox upgrade` remains the
  binary+image command).
- Making any image refresh happen by default — the new behavior is opt-in.
- **Accepting a global flag *before* a subcommand** (e.g.
  `yolobox --ensure-latest shell`). yolobox's documented convention is
  subcommand-first (`yolobox shell --ensure-latest`), and the no-harness path
  already prints a "flags go after the subcommand" hint. The wrong ordering is
  classified as user error and is intentionally not addressed here. (Note: with a
  `default_harness` set, that wrong ordering currently misfires silently — it
  forwards the subcommand token to the harness as a prompt rather than printing
  the hint. Left as-is per this decision; could be revisited separately.)

## Background: the two-layer image model

"The image" is really two layers:

- **Base image** — `ghcr.io/finbarr/yolobox:latest`, pulled from the registry.
- **Derived image** — built locally `FROM` the base when a project has
  customization; tagged by a content hash of (base image ID + packages +
  fragment) via `customImageTag` (`custom_image.go:102`).

Today's controls are split *by layer* (`--rebuild-image` = derived;
`yolobox upgrade` = base) rather than *by intent*. The new flag is intent-level:
"make the image I'm about to run as current as possible."

Layer-caching facts that make this cheap:

- `docker pull` on an already-current local `:latest` is a no-op aside from a
  digest check; a stale local copy downloads only changed layers.
- A derived build `FROM` an already-present base reuses the base layers and only
  runs the customization delta; unchanged inputs reuse the build cache.

## `--ensure-latest` flag

A new opt-in boolean flag, kept **alongside** `--rebuild-image` (not a
replacement, not an alias).

### Behavior

When set, before launching the container `runCommand` will:

1. **Force-pull the base image** (`<runtime> pull cfg.Image`) unconditionally —
   even when a local copy exists — via a new `pullLatestBaseImage(runtime, image)`
   helper. This is the "don't take the local-copy shortcut" step that refreshes a
   stale-but-present base.
2. **Then** run the existing customization path. Because the freshly-pulled base
   may have a new image ID, `customImageTag` changes, so `prepareCustomImage`
   rebuilds the derived layer automatically — no need to also pass
   `--rebuild-image`. If the base did not change, the tag is unchanged and the
   derived image is reused as-is.
3. With **no customization**, step 2 is a no-op and the run proceeds on the fresh
   base. This is the "ensure I'm current" outcome.

### Wiring

- Add `EnsureLatest bool` to `Config` (`config.go`), `toml:"-"` like `RebuildImage`.
- Register `--ensure-latest` in `parseBaseFlags` (`main.go`), set `cfg.EnsureLatest`.
- Add `"ensure-latest": true` to `splitToolArgs`' `knownFlags` so
  `yolobox claude --ensure-latest` works, and to the fork known-flag set
  (the map near `main.go:1221`).
- Add the force-pull call in `runCommand` *before* the `hasCustomization` block,
  so the base is fresh before any derived prep:
  ```go
  if cfg.EnsureLatest {
      if err := pullLatestBaseImage(cfg.Runtime, cfg.Image); err != nil {
          return err
      }
  }
  if hasCustomization(cfg) { /* existing */ }
  ```
- Add to CLI help (`printUsage`) and docs.

### `--no-network` is *not* a concern

`--no-network` only injects `--network none` into the **container run** args
(`main.go:1824`); it constrains the final running container, not the host. The
base-image pull and derived build are host-side `exec.Command` calls
(`docker pull` / `docker build`) that run before the container exists and use the
host's networking — unaffected by `cfg.NoNetwork`. (The same is already true of
`apt` installs in a custom build today.) So `--ensure-latest` works normally under
`--no-network`. The only failure mode is the **host** being offline, in which case
`docker pull` fails and `pullLatestBaseImage` surfaces that error naturally — no
special-casing required.

## Testing

- **`--ensure-latest` pulls** — logging fake runtime (appends every invocation)
  asserts a `pull` of the base image occurs.
- **With customization** — a `build` follows the `pull`.
- **No customization** — the run proceeds on the base image after the `pull` (no
  build), and the final container command is the expected one (e.g. `bash` for
  `shell`).
- **`--no-network`** — the `pull` still happens (host-side), confirming no
  special-casing.
- **Tool shortcut** — `yolobox claude --ensure-latest` parses the flag as a
  yolobox flag (not forwarded to claude) and triggers the pull.

## Docs to update

- `docs/flags.md` — flag table + a note on `--ensure-latest` vs `--rebuild-image`
  vs `yolobox upgrade`.
- `docs/customizing.md` — "Rebuild behavior" / "Upgrade behavior" sections.
- `README.md` — flag table (`README.md:420`).
- CLI help (`printUsage`, `main.go`).

## Open questions for reviewer

None outstanding.
