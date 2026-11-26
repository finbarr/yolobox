# Recipes

## Add instructions to a fresh Claude session

Claude Code's native `--append-system-prompt-file` option also works through yolobox. Run from the project directory with the file in the mounted project.

```bash
yolobox claude -- --append-system-prompt-file ./review.md
```

For the same Yolobox syntax with Claude and Codex, use [`--instructions FILE`](/commands#session-instructions).

## Parallel agents on one project

Use `fork` when you want each agent to work like another developer sitting beside you with their own machine. Name forks like developer environments, not necessarily like features.

```bash
yolobox fork --name bruno codex
yolobox fork --name diane claude
yolobox fork --name mike codex
```

Each fork gets a complete copy of the current project folder under `../.yolobox-forks/<folder>/<env>`. That includes `.git` if present, ignored files, untracked files, env files, dependencies, local caches, and anything else in the folder. Inside the container, the copied folder is mounted at the original source path, so path-based agent state still lines up while writes go to that copy.

For a Git project, treat your Git remote as the synchronization point. Have each agent commit and push from its fork, then review or merge those branches the same way you would with teammates on separate machines.

```bash
git status
git add <files>
git commit -m "Implement feature"
git push -u origin HEAD
```

When you are done with a fork:

```bash
yolobox fork discard bruno --force
```

## Webapps with local HTTPS routing

For webapps, keep routing setup in the project instead of baking a proxy into yolobox. A good pattern is:

- run one shared host-side Traefik or Caddy router on ports `80` and `443`
- use random host ports for forked app services
- attach forked app services to a shared external proxy network
- route friendly names from `YOLOBOX_FORK_NAME`, such as `https://bruno.myapp.localhost`
- keep `COMPOSE_PROJECT_NAME` for Compose resource names, not user-facing URLs
- use `.localhost` hostnames so the host needs no DNS setup
- use `mkcert` for a trusted local wildcard certificate

Run the shared router from the host project folder. Inside yolobox, `*.localhost` points at the yolobox container, not the host router, so validate routed URLs from the host or from sibling containers on the shared proxy network.

Compose namespacing covers default Compose-created containers, networks, and named volumes. Fixed host ports, explicit `container_name`, external networks or volumes, and absolute bind mounts can still collide, so route through service labels or per-fork environment variables where possible.
