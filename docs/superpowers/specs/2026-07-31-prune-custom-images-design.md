# Pruning stale custom images

## Problem

Every derived image yolobox builds is tagged `yolobox-custom:<12 hex chars>`, where the
hex is the first 12 characters of a SHA-256 over the base image ID, the normalized
package list, and the generated Dockerfile (`customImageTag`, `cmd/yolobox/custom_image.go`).

The tag is content-addressed, so editing a project's `customize.dockerfile` produces a new
tag while the previous image stays behind under its old tag. A user running several
customized projects accumulates images that all share the `yolobox-custom` repository and
differ only by an opaque hash.

`buildCustomImage` passes no `--label`, so the built image carries no provenance. Nothing
in Docker's metadata records which project or Dockerfile produced a given image, and the
hash is not reversible. A user cannot tell which images are superseded, and no script can
group them by project, because the grouping information does not exist on disk.

## Goals

- Record which project produced each custom image.
- Provide a command that reports and deletes superseded images.
- Report, but never delete, images built before the label existed.

## Non-goals

- Tracking image last-used time. See "Known limitation".
- Pruning the base image or any non-`yolobox-custom` image.
- Automatic pruning on a schedule or as a side effect of `yolobox run`.

## Design

### Provenance label

`buildCustomImage` adds one label to every build:

```
--label io.yolobox.project=<absolute project dir>
```

`prepareCustomImage` already receives `projectDir` and threads it through.

Exactly one label, deliberately. A second label holding a build timestamp would change the
image's content hash on every build, so each rebuild would orphan its predecessor as a
dangling image. A label whose value is constant for a given project keeps repeat builds
byte-identical, so a no-op rebuild still resolves to the same image ID. Build time is
already available as Docker's `Created` field, which is what the prune command reads.

The label applies only to images built after this change. Images built before it stay
unlabeled; the prune command reports them but never deletes them, as described below.

### `yolobox prune-images`

A new subcommand in `cmd/yolobox/maintenance.go`, dispatched from the `switch` in
`main.go` alongside `reset` and `uninstall`.

It enumerates images in the `yolobox-custom` repository, reads each one's `Created`,
`Size`, and `io.yolobox.project` label, and collects the set of images referenced by any
container (`docker ps -a`).

Classification, in order:

| Case | Action |
| --- | --- |
| Referenced by any container | Keep. Checked first; overrides every rule below. |
| Labeled, project directory exists | Keep the newest by `Created`; delete the rest. |
| Labeled, project directory missing | Delete all images for that project. |
| Unlabeled | Keep. Reported separately for manual deletion. |

Unlabeled images predate the label and cannot be attributed to a project, so the command
has no basis for deciding which are superseded. Deleting them would be a guess. They are
listed under their own heading, with their combined size and the `docker image rm` command
to remove them, leaving the decision to the user.

Output otherwise groups images by project, showing each image's tag, age, and size, with a
total reclaimable size.

### Flags

`reset` and `uninstall` both require `--force` to do anything destructive. `prune-images`
follows that convention: it runs as a dry run by default and only deletes with `--force`.
The dry run is useful on its own, since it answers the "which of these can I delete"
question that motivated this work.

## Structure

Classification is a pure function:

```go
func classifyCustomImages(
    images []customImage,
    inUse map[string]bool,
    dirExists func(string) bool,
) (keep, remove, unlabeled []customImage)
```

`unlabeled` is returned as its own set rather than folded into `keep`, so the command can
report it under a separate heading without re-deriving which images lack the label.

It takes no dependency on `exec` or the filesystem, so it unit-tests without a Docker
daemon, matching how `matchYoloboxVolumes` is tested today. The surrounding command
handles runtime resolution, image enumeration, and deletion.

## Error handling

- Failure to resolve the container runtime, or to list images, aborts with the error.
- An image whose `inspect` fails is reported and skipped, never deleted; one unreadable
  image must not abort the run.
- A failed `image rm` for one image is reported and the run continues to the next. The
  command exits non-zero if any deletion failed.
- No images matching `yolobox-custom` is a success with a "nothing to prune" message, not
  an error.

## Testing

Unit tests for `classifyCustomImages` covering:

- Two images for one existing project: newest kept, older removed.
- Images for a project whose directory no longer exists: all removed.
- Unlabeled images: never in the remove set, returned as `unlabeled`.
- An image referenced by a container: kept, even when its project directory is gone and
  even when it is not the newest for its project.
- No images: all three sets empty.

Plus a test that `buildCustomImage` includes the `io.yolobox.project` label in its
argument list.

## Known limitation

"Newest per project" is wrong in one scenario. If a user adds a package, builds, then
reverts that change, the reverted config hashes back to the earlier tag. `prepareCustomImage`
short-circuits on the existing image rather than rebuilding, so the live image is the older
one, and prune keeps the abandoned newer image while deleting the live one.

A second case has the same shape. Two projects with identical customization produce the
same content hash, so the second project reuses the first project's image rather than
building its own. The image carries only the first project's label. If that project's
directory is later removed, prune deletes an image the second project still uses.

Both consequences are a rebuild on the next run, not data loss or breakage. Fixing either
correctly requires tracking last-used time in a state file outside the image, which is not
worth the added state management and staleness handling for these cases.

## Documentation

- `printUsage` in `main.go` gains a `prune-images` line.
- `docs/commands.md` documents the command and its classification rules.
- `CHANGELOG.md` records the new command and the new label.
