# AGENTS.md

This file is the working agreement for changes in this repository.

## Workflow

Default workflow for this repo:

1. Make the requested code or docs change directly.
2. Verify it thoroughly.
3. Commit the change before finishing.

Whenever behavior, UX, defaults, flags, docs, or built-in agent guidance change, audit the matching user-facing surfaces in this repo and update them too:

- [README.md](README.md)
- the docs site under [docs/](docs/)
- the bundled skills under [skills/](skills/)
- the setup command/wizard in [cmd/yolobox/main.go](cmd/yolobox/main.go)

Do not stop at unit tests when behavior can be exercised for real. If a change affects runtime behavior, flags, mounts, image builds, config loading, or release automation, run the actual path and verify the output.

If full end-to-end verification is blocked by the environment, state exactly what was run, what was not run, and why.

Any hard-earned lesson that changes how future work should be done belongs in `AGENTS.md`.

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

## Verification Standard

For non-trivial code changes, start here:

```bash
make clean && make build && make test
./yolobox version
./yolobox help
./yolobox config
```

Then run the feature you changed end-to-end. Examples:

- Runtime or mount behavior: `./yolobox run ...`
- Docker integration: `./yolobox run --docker docker version`
- Custom image flow: `./yolobox run --packages cowsay /usr/games/cowsay hello`
- Config loading: `./yolobox config`

For Dockerfile or image changes, rebuild the image and run container smoke tests:

```bash
make image
./yolobox run echo hello
./yolobox run whoami
./yolobox run pwd
```

For docs-only changes, inspect the diff carefully. Tests are optional unless the docs describe behavior that also changed.

## Code Map

Most logic lives in [cmd/yolobox/main.go](cmd/yolobox/main.go).

Areas to check when adding flags or config:

- `Config`
- `parseBaseFlags`
- `mergeConfig`
- `printConfig`
- `saveGlobalConfig`
- `runSetup`
- the runtime path that consumes the option

Also update [README.md](README.md), the docs site under [docs/](docs/), and the bundled skills under [skills/](skills/) when user-facing behavior or agent behavior changes.

## Hard Learnings

- Named volumes shadow image contents. Anything baked into `/home/yolo` disappears behind `yolobox-home` for existing users.
- On macOS, Docker socket source paths are resolved inside the Docker VM. Use `/var/run/docker.sock` as the mount source, not host-side paths like `~/.colima/default/docker.sock`.
- On macOS, `SSH_AUTH_SOCK` from the host is not directly mountable into Docker. Docker Desktop uses `/run/host-services/ssh-auth.sock`; Colima requires `forwardAgent: true` and a VM-side socket path from `colima ssh -- printenv SSH_AUTH_SOCK`.
- Claude config is split across `~/.claude/` and `~/.claude.json`, and the config directory must be writable inside the container.
- Claude OAuth creds on macOS live in Keychain, not just on disk.
- `gh` tokens on macOS may also live in Keychain; `gh auth token` is the reliable extraction path.
- Colima often defaults to 2GB RAM, which is too small for heavier agent workflows. 4GB+ is the practical floor.
- Global npm installs as `yolo` need a user-writable prefix such as `/home/yolo/.npm-global`.
- If a Docker build gets SIGKILL while equivalent runtime commands succeed, split heavy installers into a separate stage to reduce layer memory pressure.
- Never `chmod` bind-mounted host sockets from inside the container. Fix access by matching the socket's group inside the container instead of mutating host permissions.
- Setup defaults must come from global config only. Never seed a global-writing flow from merged project config, or repo-local settings will leak into every future run.
- `yolobox upgrade` must not perform host-wide Docker cleanup. Pull the image you own; do not prune unrelated user images or caches as a side effect.
- Version comparisons must be semantic, not lexical. Also stamp source-built binaries with a real version string, or update checks and support output become misleading.
- `install.sh` runs under `set -euo pipefail`, so any best-effort network probe must explicitly tolerate failure. Otherwise the release lookup exits the script before the source-build fallback can run.
- Help text for auto-forwarded env vars must be generated from `autoPassthroughEnvVars`. Hardcoded copies drift and create auth debugging noise.
- Allocating a container TTY (`-t`) merges stdout and stderr at the PTY boundary. Only enable TTY for genuinely interactive commands, or host-side redirection and piping will behave incorrectly.
- Host-side stdin pipes need `docker run -i`/`podman run -i` even when no TTY is allocated. Keep `-i` separate from `-t` so piped data works without merging stdout and stderr.
- Codex trust is separate from execution mode. `--ask-for-approval never` plus `--sandbox danger-full-access` still shows the trust prompt for a new directory, so verify trusted-project startup separately when changing Codex launch flags.
- Any `sudo` re-exec path in the entrypoint must preserve `PATH` (for example `--preserve-env=PATH`) or `/opt/yolobox/bin` wrappers get bypassed and AI CLIs lose pinned yolo flags.
- Avoid parallel Git commands in this repo while another Git operation is active. We have repeatedly hit misleading `.git/index.lock` failures from overlapping status/checkout/rebase calls.
- Parallel `yolobox run` sessions against the same repo can also race on the entrypoint's `git config --global --add safe.directory ...` update and trip `/home/yolo/.gitconfig` lock errors. Run end-to-end runtime verifications sequentially when they share the same persistent home volume.
- GitHub Pages deployments that use a custom Actions workflow should set the custom domain in the repository Pages settings. A checked-in `CNAME` file is ignored in that flow and only adds confusion.
- For the VitePress docs site, stop the live dev container before running `npm run docs:build`. The dev server and build both write `docs/.vitepress/dist`, and the shared bind mount causes flaky build conflicts if both are active.
- Social-card support is not just `og:image`. Ship a real `robots.txt`, generate a sitemap, and include explicit `og:image:*` plus `twitter:image:alt` metadata so crawlers and card parsers have stable image hints.
- For VitePress SEO, do not hardcode canonical URLs or `og:` / `twitter:` titles in global `head`. Generate them per page from `pageData`, or every doc page will look like the homepage to crawlers.
- Docker file bind mounts targeting a path inside an already bind-mounted project tree degrade into directories. Project file filtering must use a staged readonly project view rather than nested file mounts.
- When yolobox itself runs inside another yolobox, temp mount sources must live under an existing host-visible bind mount like the project path. Inner-container `/tmp` is not visible to the outer Docker daemon.
- On macOS/Colima, host temp paths under `/var/folders` may mount into containers as empty directories because the Docker VM cannot see them. Put temp mount sources under a Docker-visible path such as `~/.yolobox/tmp` when verifying bind-mounted config directories.
- Brand and social assets must not depend on runtime font rendering for the ASCII wordmark. Generate committed image assets from deterministic shapes, or social-card rasterization can drop the text and the site logo can drift from the share image.
- Rootless Podman named volumes need `:U` alongside `:Z` when using `--userns=keep-id`, or older subordinate-ID ownership on `yolobox-home` reappears in-container as uid/gid 999 and breaks `/home/yolo`.
- If the entrypoint remaps `yolo` to the host project UID/GID, it must also re-own `/output`; otherwise `--readonly-project` leaves the writable output volume stuck at the image's original 1000:1000 ownership.
- Built-in skills live under `skills/` as standard Agent Skills packages. When editing them, keep `SKILL.md` spec-compliant and validate with `uvx --from git+https://github.com/agentskills/agentskills#subdirectory=skills-ref skills-ref validate ./skills/<name>`.
- VitePress docs links cannot point from `docs/` to repo-root paths such as `../skills`; use GitHub source links or add real docs pages, or the Pages build fails dead-link checking.
- When a built-in skill is renamed, the entrypoint must remove the old skill directory from `/home/yolo/.codex/skills` during startup. The named home volume preserves stale skill folders across image upgrades.
- Built-in agent guidance should be injected into `CLAUDE.md` and `AGENTS.md` as a managed block. Do not replace user instruction files outright just to teach agents about yolobox behavior.
- The built-in yolobox skill must not infer readonly project state from `/output` existing. `/output` is present in the base image; check the manifest and actual project access instead.
- A zero-byte `/home/yolo/.codex/auth.json` makes recent Codex CLIs abort with `EOF while parsing a value`. Repair that stale persistent-home state during entrypoint startup instead of requiring users to reset the volume.
- Host Codex config import must preserve a valid existing container `/home/yolo/.codex/auth.json` when the host config has no usable auth file. Do not `rm -rf /home/yolo/.codex` before importing host Codex config.
- Codex auth can report `No space left on device` when the Docker/Podman storage backing `/home/yolo` or `/tmp` is full after image pulls or builds. Warn clearly, but do not auto-prune unrelated runtime storage.
- Codex resume ordering is sensitive to session file mtimes. Host config import must restore imported `~/.codex/sessions` mtimes from session timestamps, or old host sessions appear as freshly updated in resume lists.
- Do not create yolobox runtime metadata directories inside the project checkout. For small container handoff data like the context manifest, pass it through the entrypoint and materialize it in-container instead of bind-mounting a host temp directory from the repo.
- Local `make lint` skips `golangci-lint` when it is not installed, but CI runs `golangci-lint-action` with the latest linter. Before release pushes, run the CI linter explicitly or install `golangci-lint` locally.
