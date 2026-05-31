# What's in the Box

The base image is meant to be useful immediately without turning into a giant kitchen-sink image.

## Preinstalled tools

### AI CLIs

- Claude Code
- Gemini CLI
- OpenAI Codex
- OpenCode
- GitHub Copilot
- Pi

Claude and Codex sessions also get a built-in `yolobox` skill that helps the agent orient itself to the trusted sandbox it is running in, then reads `/run/yolobox/context.json` and describes the active environment. yolobox also injects managed guidance into their user instruction files so they know when to use that skill. The host-side `yolobox-orchestrator` skill lives in the repo's `skills/` directory but is not auto-installed inside the container because it is meant for agents running outside yolobox.

RTK is also preinstalled for opt-in command-output compression. Pass `--rtk` or set `rtk = true` to initialize it for Claude, Codex, Gemini, or OpenCode inside the container.

### Runtimes

- Node.js 22
- Python 3
- Go
- Bun

npm is upgraded during the image build using npm's date-based `--before` filter. yolobox's own later npm/npx installs in that image build run with `NPM_CONFIG_MIN_RELEASE_AGE=7`, but the finished box does not keep the release-age setting at runtime.

### Build tools

- make
- cmake
- gcc

### Utilities

- git
- GitHub CLI
- ripgrep
- fd
- fzf
- jq
- vim
- RTK

Need something else? The agent has sudo inside the container. If it needs a package manager, runtime, database client, or build dependency, it can install it.

## YOLO mode

Inside yolobox, AI CLIs are wrapped to skip approval prompts where the upstream tool supports it:

| Command | Expands to |
|---------|------------|
| `claude` | `claude --dangerously-skip-permissions` |
| `codex` | `codex --ask-for-approval never --sandbox danger-full-access` |
| `gemini` | `gemini --yolo` |
| `opencode` | `opencode` |
| `copilot` | `copilot --yolo` |
| `pi` | `pi` |

No confirmations, no guardrails. That is the product.

OpenCode and Pi do not have dedicated yolo flags yet, but they still run inside the yolobox sandbox.

## Why the base image stays lean

The base image includes common tools nearly everyone needs. Project-specific stacks should usually be layered on with [project-level customization](/customizing):

- `packages = [...]` for apt packages
- `dockerfile = ".yolobox.Dockerfile"` for more advanced setup

That keeps upgrades cheaper than maintaining a fully custom forked image.

::: tip Why is this safe?
The AI is running inside a container. It can `rm -rf /` and the only thing destroyed is the container itself. Your home directory, your SSH keys, your other projects, and the rest of your host stay out of reach unless you explicitly expose them.
:::
