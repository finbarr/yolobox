package main

import (
	"fmt"
	"strconv"
	"strings"
)

var agentUpdateTargets = []string{
	"claude",
	"codex",
	"gemini",
	"kimi",
	"agy",
	"opencode",
	"copilot",
	"pi",
}

var agentUpdateAliases = map[string]string{
	"claude":      "claude",
	"codex":       "codex",
	"gemini":      "gemini",
	"kimi":        "kimi",
	"agy":         "agy",
	"antigravity": "agy",
	"opencode":    "opencode",
	"copilot":     "copilot",
	"pi":          "pi",
}

func updateAgents(args []string, projectDir string, fork *ForkConfig) error {
	defaults, err := loadSetupDefaults()
	if err != nil {
		return err
	}
	defaults = updateAgentsDefaults(defaults)

	cfg, rest, err := parseBaseFlagsWithConfig("update-agents", args, projectDir, defaults)
	if err != nil {
		return err
	}
	applyForkConfig(&cfg, fork)

	targets, err := parseUpdateAgentTargets(rest)
	if err != nil {
		return err
	}

	cfg, err = prepareUpdateAgentsConfig(cfg)
	if err != nil {
		return err
	}

	return runCommand(cfg, []string{"bash", "-lc", updateAgentsShellScript(targets)}, false)
}

func updateAgentsDefaults(cfg Config) Config {
	cfg.NoProject = false
	cfg.ReadonlyProject = false
	cfg.Exclude = nil
	cfg.CopyAs = nil
	cfg.ClaudeConfig = false
	cfg.CodexConfig = false
	cfg.GeminiConfig = false
	cfg.KimiConfig = false
	cfg.OpencodeConfig = false
	cfg.PiConfig = false
	cfg.GitConfig = false
	cfg.GhToken = false
	cfg.RTK = false
	cfg.CopyAgentInstructions = false
	cfg.Docker = false
	cfg.Clipboard = false
	cfg.OpenBridge = false
	cfg.Customize = CustomizeConfig{}
	cfg.RebuildImage = false
	return cfg
}

func parseUpdateAgentTargets(args []string) ([]string, error) {
	if len(args) == 0 {
		return allAgentUpdateTargets(), nil
	}

	seen := make(map[string]bool)
	var targets []string
	add := func(target string) {
		if seen[target] {
			return
		}
		seen[target] = true
		targets = append(targets, target)
	}

	for _, arg := range args {
		for _, part := range strings.Split(arg, ",") {
			name := strings.ToLower(strings.TrimSpace(part))
			if name == "" {
				continue
			}
			if name == "all" {
				for _, target := range agentUpdateTargets {
					add(target)
				}
				continue
			}
			target, ok := agentUpdateAliases[name]
			if !ok {
				return nil, fmt.Errorf("unknown update-agents target %q; expected one of: %s", name, updateAgentTargetHelp())
			}
			add(target)
		}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("update-agents requires a target, or no targets to update all agents")
	}
	return targets, nil
}

func prepareUpdateAgentsConfig(cfg Config) (Config, error) {
	if cfg.Scratch {
		return Config{}, fmt.Errorf("update-agents cannot use --scratch because agent updates must persist in yolobox-home")
	}
	if cfg.NoNetwork {
		return Config{}, fmt.Errorf("update-agents requires network access; remove --no-network")
	}

	cfg = updateAgentsDefaults(cfg)
	cfg.NoProject = true
	return cfg, nil
}

func allAgentUpdateTargets() []string {
	targets := make([]string, len(agentUpdateTargets))
	copy(targets, agentUpdateTargets)
	return targets
}

func updateAgentTargetHelp() string {
	names := append([]string{"all"}, agentUpdateTargets...)
	names = append(names, "antigravity")
	return strings.Join(names, ", ")
}

func updateAgentsShellScript(targets []string) string {
	quotedTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		quotedTargets = append(quotedTargets, strconv.Quote(target))
	}

	var b strings.Builder
	b.WriteString("set -euo pipefail\n")
	b.WriteString("export NPM_CONFIG_PREFIX=\"${NPM_CONFIG_PREFIX:-$HOME/.npm-global}\"\n")
	b.WriteString("mkdir -p \"$NPM_CONFIG_PREFIX/bin\" \"$HOME/.local/bin\"\n")
	b.WriteString("export PATH=\"/opt/yolobox/bin:$NPM_CONFIG_PREFIX/bin:$HOME/.local/bin:$PATH\"\n")
	b.WriteString("targets=(")
	b.WriteString(strings.Join(quotedTargets, " "))
	b.WriteString(")\n")
	b.WriteString(`
print_version() {
    local label="$1"
    local bin="$2"
    local version=""
    if command -v "$bin" >/dev/null 2>&1 && version="$(NO_YOLO=1 "$bin" --version 2>&1 | head -n 1)"; then
        printf 'ok %s: %s\n' "$label" "$version"
    else
        printf 'ok %s updated\n' "$label"
    fi
}

require_command() {
    local bin="$1"
    local label="$2"
    if ! command -v "$bin" >/dev/null 2>&1; then
        printf '%s is required to update %s but is not installed in this image\n' "$bin" "$label" >&2
        exit 1
    fi
}

npm_update() {
    local label="$1"
    local bin="$2"
    local package="$3"
    require_command npm "$label"
    printf 'updating %s (%s)\n' "$label" "$package"
    npm install -g --no-audit --no-fund "$package@latest"
    print_version "$label" "$bin"
}

update_claude() {
    require_command claude "Claude Code"
    printf 'updating Claude Code\n'
    NO_YOLO=1 claude update
    print_version "Claude Code" claude
}

update_antigravity() {
    require_command curl "Antigravity CLI"
    require_command bash "Antigravity CLI"
    printf 'updating Antigravity CLI\n'
    local tmp_home=""
    local installer=""
    local install_dir="$HOME/.local/bin"
    tmp_home="$(mktemp -d)"
    installer="$(mktemp)"
    if ! curl -fsSL https://antigravity.google/cli/install.sh -o "$installer"; then
        rm -rf "$tmp_home" "$installer"
        return 1
    fi
    if ! HOME="$tmp_home" bash "$installer" --dir "$install_dir"; then
        rm -rf "$tmp_home" "$installer"
        return 1
    fi
    rm -rf "$tmp_home" "$installer"
    print_version "Antigravity CLI" agy
}

update_kimi() {
    require_command curl "Kimi Code"
    require_command bash "Kimi Code"
    printf 'updating Kimi Code\n'
    local curl_home=""
    local installer=""
    curl_home="$(mktemp -d)"
    printf '%s\n' \
        'retry = 5' \
        'retry-all-errors' \
        'retry-delay = 2' \
        'connect-timeout = 20' \
        'max-time = 300' \
        > "$curl_home/.curlrc"
    installer="$(mktemp)"
    if ! CURL_HOME="$curl_home" curl -fsSL https://code.kimi.com/kimi-code/install.sh -o "$installer"; then
        rm -rf "$curl_home" "$installer"
        return 1
    fi
    if ! CURL_HOME="$curl_home" KIMI_INSTALL_DIR="$HOME/.local" KIMI_NO_MODIFY_PATH=1 bash "$installer"; then
        rm -rf "$curl_home" "$installer"
        return 1
    fi
    rm -rf "$curl_home" "$installer"
    print_version "Kimi Code" kimi
}

for target in "${targets[@]}"; do
    case "$target" in
        claude)
            update_claude
            ;;
        codex)
            npm_update "OpenAI Codex" codex "@openai/codex"
            ;;
        gemini)
            npm_update "Gemini CLI" gemini "@google/gemini-cli"
            ;;
        kimi)
            update_kimi
            ;;
        agy)
            update_antigravity
            ;;
        opencode)
            npm_update "OpenCode" opencode "opencode-ai"
            ;;
        copilot)
            npm_update "GitHub Copilot" copilot "@github/copilot"
            ;;
        pi)
            npm_update "Pi" pi "@earendil-works/pi-coding-agent"
            ;;
        *)
            printf 'unsupported update target: %s\n' "$target" >&2
            exit 1
            ;;
    esac
done

printf 'agent updates complete\n'
`)
	return b.String()
}
