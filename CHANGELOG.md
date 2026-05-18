# Changelog

All notable changes to yolobox are documented here.

This changelog was backfilled from Git tags, annotated tag messages, and commit
history. Versions are listed newest first. There is no `v0.6.0` entry because
there is no `v0.6.0` tag in this repository.

## Unreleased

### Added

- Expanded remote mode around explicit machine, workspace, session, and exposure metadata.
- Added named remote workspaces, directional `remote sync up` / `remote sync down --force`, `remote stop`, and local SSH preview forwarding with `remote forward`.
- Added `remote_workspace` config and setup support for the default remote workspace.
- Added token-authenticated remote backend client support for `remote.backend_url` and a documented backend API contract.

### Changed

- Scoped npm package release-age gating to yolobox's own base-image build installs; runtime npm/npx commands and CLI self-updates are unrestricted by default.
- Added `--name` / `container_name` support for assigning a runtime container name.

## v0.18.0 - 2026-05-18

### Added

- Added opt-in RTK command-output compression for supported AI CLIs with `--rtk` or `rtk = true`.
- Added concise release summaries to update prompts and post-upgrade output.
- Added `yolobox upgrade --check` to inspect the latest release without changing the binary or image.

### Changed

- GitHub releases now use the curated `CHANGELOG.md` section for the release body so CLI update prompts can show human-written notes.
- Documented that releases must be tagged only from a clean, up-to-date `master` branch and pushed with the specific release tag.
- Updated remote mode docs into a fuller backend/client specification covering API contracts, lifecycle, workspace sync, registry state, preview forwarding, failure behavior, security boundaries, and hosted roadmap.
- Remote workspaces now sync under `/opt/yolobox-workspaces` and non-interactive remote commands run directly over SSH instead of requiring a terminal-backed tmux attach.

## v0.17.1 - 2026-05-17

### Added

- Added this changelog.
- Added `YOLOBOX_TIMING=1` startup diagnostics for host-side and container entrypoint timing.

### Fixed

- Made Codex config sync incremental, installed `rsync` in the base image, skipped volatile log/state/cache/temp files, and live-mounted host Codex sessions so large host `~/.codex` trees are not recopied and rechowned on every yolobox start.

## v0.17.0 - 2026-05-16

### Added

- Added Pi agent support to the base image and CLI shortcuts.
- Added an option to disable automatic environment variable passthrough.
- Added a host URL open bridge for opening URLs from inside yolobox.
- Added configurable default harness support.
- Added npm package release-age gating for base image builds and runtime npm installs.

### Fixed

- Preserved Codex session timestamps when importing host Codex config, so resume lists keep their original ordering.

## v0.16.0 - 2026-05-08

### Added

- Added `--no-project` to skip the automatic project mount, workdir, `YOLOBOX_PROJECT_PATH`, and host UID/GID detection.
- Supported Docker-in-Docker and similar environments where the container CWD is not visible to the outer runtime host.

## v0.15.0 - 2026-05-04

### Added

- Added OpenCode config sync support.

## v0.14.2 - 2026-05-03

### Fixed

- Fixed a CI lint failure after the v0.14 release.

## v0.14.1 - 2026-05-03

### Fixed

- Fixed GitHub HTTPS authentication inside yolobox.

## v0.14.0 - 2026-05-03

### Added

- Added `yolobox fork`, a named full-folder copy workflow for running multiple agents against isolated copies of the same project.
- Added fork resume and discard workflows.
- Added fork metadata through environment variables and the runtime context manifest.
- Added fork-specific `COMPOSE_PROJECT_NAME` namespacing for Docker Compose resources.
- Added fork documentation and recipes, including local HTTPS routing guidance for web apps.
- Added host text clipboard bridging with `--clipboard`.

### Fixed

- Clarified the bundled yolobox skill helper path so agents do not look for helper scripts in the project checkout.

## v0.13.3 - 2026-04-26

### Fixed

- Fixed context script tests when Go is built with `-trimpath`.

## v0.13.2 - 2026-04-24

### Fixed

- Avoided creating yolobox runtime context manifest temp directories inside the project checkout.

## v0.13.1 - 2026-04-24

### Fixed

- Preserved a valid existing container Codex auth file when the imported host Codex config has no usable auth file.

## v0.13.0 - 2026-04-24

### Fixed

- Repaired zero-byte Codex auth files during startup so Codex does not abort on invalid JSON.
- Fixed docs site links for bundled skill source files.

## v0.12.1 - 2026-04-24

### Fixed

- Improved bundled yolobox skill project-access reporting.
- Trimmed the skill access report and clarified access field names.

## v0.12.0 - 2026-04-23

### Added

- Added built-in yolobox agent skills.

### Changed

- Updated Docker release GitHub Actions.

## v0.11.0 - 2026-04-14

### Added

- Added Codex config sync support.

## v0.10.6 - 2026-03-30

### Fixed

- Fixed rootless Podman named volume ownership.
- Fixed `/output` ownership so readonly-project runs can still write output files.

### Changed

- Improved yolobox.dev social-card metadata and SEO metadata.

## v0.10.5 - 2026-03-24

### Added

- Added the VitePress documentation site for yolobox.dev.
- Added GitHub Pages deployment for the docs site.
- Added generated brand assets and social-card assets.
- Added readonly project file filtering.

### Changed

- Refactored the yolobox CLI into logical files.
- Linked the README to the docs site.

## v0.10.4 - 2026-03-21

### Fixed

- Preserved `PATH` in the Docker socket regroup re-exec path.

## v0.10.3 - 2026-03-21

### Fixed

- Installed `bubblewrap` in the base image to suppress Codex startup warnings.

## v0.10.2 - 2026-03-21

### Changed

- Pinned Codex yolo-mode flags explicitly.

### Fixed

- Refreshed process groups after adding the container user to the Docker socket group.

## v0.10.1 - 2026-03-19

### Fixed

- Fixed TTY handling so non-interactive commands keep stdout and stderr separate.
- Preserved host socket permissions instead of mutating bind-mounted socket files.
- Kept setup defaults sourced from global config only.
- Stopped `yolobox upgrade` from pruning unrelated host images.
- Compared versions semantically instead of lexically.
- Aligned auto-forwarded environment variable documentation with the source list.

### Changed

- Replaced the Claude-specific repo guide with `AGENTS.md`.
- Refreshed GitHub Actions versions and CI settings.

## v0.10.0 - 2026-03-14

### Added

- Added build-time container customization for project-specific packages and Dockerfile fragments.

## v0.9.4 - 2026-03-12

### Fixed

- Added `--userns=keep-id` for rootless Podman bind mount permissions.
- Added SELinux labels to named volumes for Podman persistence on SELinux-enabled hosts.

## v0.9.3 - 2026-03-11

### Fixed

- Removed the default Ubuntu user from the base image to avoid UID 1000 collisions during UID remapping.

## v0.9.2 - 2026-03-08

### Fixed

- Preserved `PATH` through the UID-fix sudo call so yolo-mode wrappers remain active.

## v0.9.1 - 2026-03-07

### Fixed

- Matched the container user UID/GID to the host project directory owner for Colima and virtiofs bind mount access.

## v0.9.0 - 2026-03-01

### Added

- Added advanced container resource controls.
- Added issue templates and a pull request template.

### Changed

- Simplified runtime flags and added passthrough support.
- Synced runtime flag documentation with CLI behavior.
- Reused shared config merge logic in shell setup.

## v0.8.3 - 2026-02-27

### Fixed

- Used unique temp files for `--claude-config` so concurrent yolobox instances do not overwrite each other's mounted config.
- Collected and cleaned up temp paths created during run argument construction.

## v0.8.2 - 2026-02-27

### Fixed

- Pinned Claude Code to the image version during container startup.
- Added a smoke test for Claude version pinning.

## v0.8.1 - 2026-02-25

### Fixed

- Resolved symlinks in host config directories before mounting them into the container.

## v0.8.0 - 2026-02-25

### Added

- Added `--pod` support for joining a Podman pod.
- Added persistent Podman pod support.

### Fixed

- Isolated tests from host config so they do not depend on the developer machine's environment.

## v0.7.3 - 2026-02-24

### Fixed

- Resolved SSH agent socket paths inside the Docker VM on macOS.
- Fixed SSH agent socket permissions in the entrypoint.

## v0.7.2 - 2026-02-23

### Fixed

- Pruned dangling images after upgrade to reduce disk exhaustion from old image layers.

### Changed

- Switched Docker image builds to registry cache and optimized Dockerfile layer ordering.

## v0.7.1 - 2026-02-23

### Fixed

- Used VM-internal Docker socket paths on macOS.

### Changed

- Added stricter verification-before-commit guidance to the repo instructions.

## v0.7.0 - 2026-02-23

### Added

- Added automatic `CLAUDE_CODE_OAUTH_TOKEN` passthrough.
- Added host timezone auto-detection and forwarding.
- Added `--docker` for Docker socket forwarding and shared-network behavior.

### Fixed

- Waited for Docker image builds before updating the Homebrew formula.
- Gated releases on Docker image build completion.
- Preferred `/var/run/docker.sock` over runtime-specific host paths.
- Filtered Docker build cache artifacts out of release binary downloads.

### Changed

- Enabled GitHub Actions cache for Docker image builds.
- Added test gating and `go-version-file` usage in release CI.

## v0.6.1 - 2026-02-05

### Added

- Added `--gemini-config` to copy host Gemini CLI config into the container.

### Fixed

- Allowed global npm installs as the `yolo` user without `sudo`.

## v0.5.1 - 2026-01-31

### Added

- Extracted Claude OAuth credentials from macOS Keychain when copying Claude config.

### Fixed

- Clarified that yolobox flags must come after the subcommand.
- Stripped `installMethod` from copied `.claude.json` so Claude detects its container install correctly.

## v0.5.0 - 2026-01-27

### Fixed

- Passed tool-specific flags through shortcut commands such as `yolobox claude --resume`.

## v0.4.0 - 2026-01-27

### Added

- Added `--gh-token` for forwarding a GitHub CLI token.
- Added `--network` for joining explicit container networks.
- Documented network modes and the new GitHub token flag.

## v0.3.0 - 2026-01-23

### Added

- Added Apple `container` runtime support for macOS Tahoe and newer.
- Added a host-file staging workaround for Apple container file mount limitations.
- Added Apple container SSH agent forwarding support.

## v0.2.2 - 2026-01-22

### Changed

- Updated the Homebrew formula to use pre-built signed binaries instead of building from source.

## v0.2.1 - 2026-01-22

### Added

- Added Apple code signing and notarization for macOS release binaries.

## v0.2.0 - 2026-01-21

### Changed

- Mounted projects at their real host paths instead of `/workspace` for agent session continuity.
- Set `YOLOBOX_PROJECT_PATH` inside the container.
- Configured Git `safe.directory` and Claude trust for the real project path.

### Breaking Changes

- Existing sessions tied to `/workspace` no longer map to the project path used by new runs.

## v0.1.9 - 2026-01-21

### Added

- Added `--copy-agent-instructions` to copy global AI agent instruction files without copying full credentials or history.

## v0.1.8 - 2026-01-20

### Added

- Added Homebrew install instructions.
- Added automated Homebrew formula updates to the release workflow.

### Fixed

- Ran terminfo and wrapper setup as root during image builds.

### Changed

- Reordered README sections for a clearer user flow.
- Removed the initial GoReleaser/Homebrew implementation before adding the automated formula workflow.

## v0.1.7 - 2026-01-19

### Added

- Added Bun and modern CLI utilities to the base image.
- Added zsh shell support.
- Added the interactive setup wizard.
- Added tool shortcut commands.

### Fixed

- Suppressed the zsh first-run wizard.
- Showed the logo when using tool shortcuts.

### Changed

- Simplified and deduplicated README positioning copy.

## v0.1.6 - 2026-01-18

### Added

- Added `--git-config` to copy host Git config into the container.
- Added `--scratch` for ephemeral environments.
- Added fish shell support with auto-detection and Ghostty terminfo.
- Added documentation for customizing the Docker image.

### Fixed

- Aligned the Makefile image name with the CLI default.

### Changed

- Removed project config restrictions that had treated project-level config as a host security boundary.

## v0.1.5 - 2026-01-14

### Added

- Added Go and `uv` to the container image.
- Added GitHub Copilot CLI to the container image.
- Added `COPILOT_GITHUB_TOKEN` to auto-passthrough environment variables.
- Added `--no-yolo` to disable AI auto-confirmation wrappers.
- Added TTY auto-detection for interactive container sessions.

### Fixed

- Ran `claude install` during image build so Claude update support works.

## v0.1.4 - 2026-01-12

### Fixed

- Blocked sensitive project config fields for runtime, image argument injection, SSH agent forwarding, and Claude config sharing.

## v0.1.3 - 2026-01-12

### Fixed

- Blocked project-level runtime overrides that could execute host commands.
- Changed the hardcoded version fallback to `dev` for source builds.

## v0.1.2 - 2026-01-12

### Fixed

- Detected symlinks in project config mount validation and required their targets to stay inside the project directory.

### Changed

- Documented versioning and release process.

## v0.1.1 - 2026-01-12

### Added

- Added OpenCode CLI to the container image.

### Fixed

- Fixed release workflow Docker tags to include the `v` prefix.
- Fixed project config mount escape validation.
- Fixed version stamping by making the version variable linker-settable.

### Changed

- Expanded README security model documentation.

## v0.1.0 - 2026-01-11

### Added

- Initial yolobox release.
- Added a container sandbox for running AI coding agents without mounting the host home directory by default.
- Added persistent volumes for tools and config state.
- Added Claude Code, Gemini CLI, and OpenAI Codex support in the base image.
- Added yolo-mode wrappers for AI CLIs.
- Added host Claude config sharing with opt-in config copy support.
- Added upgrade, uninstall, reset, config, version, shell, and run workflows.
- Added update checks with a 24-hour cache.
- Added project mounting, Git trust setup, and Claude trust setup.
- Added low-memory warnings and initial workflow documentation.
