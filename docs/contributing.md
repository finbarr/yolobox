# Contributing

## Getting started

```bash
git clone https://github.com/finbarr/yolobox.git
cd yolobox
make build
make test
```

## Requirements

- Go 1.23+
- Docker or Podman for runtime and image testing

If you are working on the docs site branch, you also need Node.js to build the VitePress site.

yolobox release image builds run their npm installs with npm's `min-release-age` set to 7 days. If you install npm dependencies while developing locally and want to mirror the release-image build behavior, use:

```bash
npm install --min-release-age=7
```

## Development commands

```bash
make build
make test
make lint
make image
make smoke-test
make smoke-acp
make install
```

After image changes, build and test the same image, for example `make image IMAGE=yolobox:dev` followed by `make smoke-test IMAGE=yolobox:dev`. The full smoke test includes both ACP adapters. For a focused check, `make smoke-acp IMAGE=yolobox:dev` checks adapter versions and ACP protocol initialization through piped yolobox stdio without credentials or network access. Use `SMOKE_ARGS` to pass runtime options, for example `SMOKE_ARGS="--runtime podman --image yolobox:dev"` when that image is available in Podman.

For docs site work:

```bash
cd docs
npm install
npm run docs:build
```

## Expectations

- follow the repo guidance in `AGENTS.md`
- add tests for code changes
- run the relevant verification before committing
- keep documentation aligned with shipped behavior
- update the [changelog](https://github.com/finbarr/yolobox/blob/master/CHANGELOG.md) for release-worthy changes

## Pull requests

1. create a branch
2. make the change
3. run the relevant verification
4. if you changed docs, build the docs site
5. open a PR with a clear description

## Reporting issues

Include:

- operating system and version
- container runtime and version
- reproduction steps
- expected vs actual behavior

## Versioning

Version comes from `git describe`:

- tagged commit: `v0.1.1`
- later commit: `v0.1.1-3-gead833b`
- local changes add `-dirty`

The Makefile handles version stamping automatically. The version string does not require a source edit.

## Releasing

Before tagging, update the [changelog](https://github.com/finbarr/yolobox/blob/master/CHANGELOG.md) with the release's user-facing changes and commit it. The release workflow uses that tag's changelog section as the GitHub release body, and yolobox uses that release body for update prompts, `yolobox upgrade --check`, and post-upgrade notes.

Only tag releases from `master`. Before creating the tag, switch to `master`, fast-forward it from `origin/master`, and verify the working tree is clean. Do not tag feature branches, and do not use `git push --tags` from a non-release branch.

```bash
git switch master
git pull --ff-only origin master
git status --short --branch
git tag v0.1.2
git push origin master refs/tags/v0.1.2
```

GitHub Actions builds release binaries, creates the GitHub release, and publishes the container image.
