# Project-Level Customization

## The default path

If one project needs extra tooling, do not fork the whole base image first. Start with project-level customization:

```toml
# .yolobox.toml
[customize]
packages = ["default-jdk", "maven"]
```

Then run normally:

```bash
yolobox run mvn --version
```

The first run builds a derived image. Later runs reuse it until the base image or customization inputs change.

Derived images inherit the base image's npm release-age gate: `NPM_CONFIG_MIN_RELEASE_AGE=7`. npm/npx commands in Dockerfile fragments skip package versions published in the last week unless the fragment explicitly overrides npm config.

## Add apt packages

Use `packages = [...]` when you only need system packages:

```toml
[customize]
packages = ["default-jdk", "maven", "postgresql-client"]
```

For one-offs, you can also do it from the CLI:

```bash
yolobox run --packages default-jdk,maven mvn --version
```

## Add a Dockerfile fragment

Use a fragment when packages are not enough:

```toml
[customize]
dockerfile = ".yolobox.Dockerfile"
```

```dockerfile
USER root
RUN curl -fsSL https://get.sdkman.io | bash
USER yolo
```

CLI equivalent:

```bash
yolobox run --customize-file .yolobox.Dockerfile bash
```

You can combine both. `packages` install first, then the fragment runs on top.

## Rebuild behavior

Use this flag when you want to force a rebuild:

```bash
yolobox run --packages default-jdk --rebuild-image java --version
```

Normal behavior:

- package-only customizations reuse a matching derived image directly
- Dockerfile-fragment customizations ask the runtime to build again so context changes are noticed
- cached layers are reused when inputs have not materially changed

## Upgrade behavior

This approach is designed to keep `yolobox upgrade` relatively painless:

- `yolobox upgrade` still updates the binary and pulls the latest base image
- your customization stays in `.yolobox.toml` and optional fragment files
- the next run rebuilds the derived image only when the base image or customization inputs changed

You still pay one rebuild after a base-image update, but you do not have to manually rebase a full Dockerfile fork just to keep using extra project tools.

## Runtime requirement

Derived-image customization requires a runtime that can build images:

- Docker
- Podman

Apple's `container` runtime can run yolobox, but it cannot build custom images.

## Fully custom images

If you need full control over the base image, you can still build and point yolobox at your own image:

```bash
git clone https://github.com/finbarr/yolobox.git
cd yolobox
make image IMAGE=my-yolobox:latest
```

Then set:

```toml
image = "my-yolobox:latest"
```

That is the escape hatch, not the default recommendation.

### The upside

- total control over the image contents
- stable custom image name that `yolobox upgrade` will not overwrite

### The downside

- upstream Dockerfile changes do not flow into your custom image automatically
- `yolobox upgrade` updates the binary, but it does not rebuild or migrate your forked image
- when upstream changes, you own the rebase and rebuild work
- the farther your image drifts, the more upgrade friction you take on

If you mostly need “add a few tools for this repo”, prefer project-level customization.
