package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var Version = "dev"

const (
	logo    = `
  ██╗   ██╗ ██████╗ ██╗      ██████╗ ██████╗  ██████╗ ██╗  ██╗
  ╚██╗ ██╔╝██╔═══██╗██║     ██╔═══██╗██╔══██╗██╔═══██╗╚██╗██╔╝
   ╚████╔╝ ██║   ██║██║     ██║   ██║██████╔╝██║   ██║ ╚███╔╝
    ╚██╔╝  ██║   ██║██║     ██║   ██║██╔══██╗██║   ██║ ██╔██╗
     ██║   ╚██████╔╝███████╗╚██████╔╝██████╔╝╚██████╔╝██╔╝ ██╗
     ╚═╝    ╚═════╝ ╚══════╝ ╚═════╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═╝
`
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

// Common API keys to auto-passthrough
var autoPassthroughEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"COPILOT_GITHUB_TOKEN",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"OPENROUTER_API_KEY",
	"GEMINI_API_KEY",
}

// Tool shortcuts - these become direct subcommands (e.g., "yolobox claude")
var toolShortcuts = []string{
	"claude",
	"codex",
	"gemini",
	"opencode",
	"copilot",
}

type Config struct {
	Runtime         string   `toml:"runtime"`
	Image           string   `toml:"image"`
	Mounts          []string `toml:"mounts"`
	Env             []string `toml:"env"`
	SSHAgent        bool     `toml:"ssh_agent"`
	ReadonlyProject bool     `toml:"readonly_project"`
	NoNetwork       bool     `toml:"no_network"`
	NoYolo          bool     `toml:"no_yolo"`
	Scratch         bool     `toml:"scratch"`
	ClaudeConfig    bool     `toml:"claude_config"`
	GitConfig       bool     `toml:"git_config"`

	// Runtime-only fields (not persisted to config file)
	Setup bool `toml:"-"` // Run interactive setup before starting
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

var errHelp = errors.New("help requested")

// Version check cache
type versionCache struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
}

const versionCheckInterval = 24 * time.Hour

func versionCachePath() string {
	configDir, _ := os.UserConfigDir()
	return filepath.Join(configDir, "yolobox", "version-check.json")
}

func checkForUpdates() {
	// Don't block on version check - run with a short timeout
	done := make(chan struct{})
	go func() {
		defer close(done)
		doVersionCheck()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// Timeout - skip update check
	}
}

func doVersionCheck() {
	cachePath := versionCachePath()

	// Try to read cache
	var cache versionCache
	if data, err := os.ReadFile(cachePath); err == nil {
		if err := json.Unmarshal(data, &cache); err == nil {
			// Cache is valid, check if it's fresh enough
			if time.Since(cache.CheckedAt) < versionCheckInterval {
				// Use cached version
				showUpdateMessage(cache.LatestVersion)
				return
			}
		}
	}

	// Fetch latest version from GitHub
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/finbarr/yolobox/releases/latest")
	if err != nil {
		return // Silently fail
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	// Update cache
	cache = versionCache{
		LatestVersion: latestVersion,
		CheckedAt:     time.Now(),
	}
	if data, err := json.Marshal(cache); err == nil {
		os.MkdirAll(filepath.Dir(cachePath), 0755)
		os.WriteFile(cachePath, data, 0644)
	}

	showUpdateMessage(latestVersion)
}

func showUpdateMessage(latestVersion string) {
	currentVersion := strings.TrimPrefix(Version, "v")
	if latestVersion != "" && latestVersion != currentVersion && latestVersion > currentVersion {
		fmt.Fprintf(os.Stderr, "\n%s💡 yolobox v%s available:%s https://github.com/finbarr/yolobox/releases/tag/v%s\n",
			colorYellow, latestVersion, colorReset, latestVersion)
		fmt.Fprintf(os.Stderr, "   Run %syolobox upgrade%s to update\n\n", colorBold, colorReset)
	}
}

func main() {
	os.Exit(run())
}

func run() int {
	if err := runCmd(); err != nil {
		if errors.Is(err, errHelp) {
			return 0
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		errorf("%v", err)
		return 1
	}
	return 0
}

func runCmd() error {
	args := os.Args[1:]

	// Check for updates (skip for version/help/upgrade commands)
	skipCheck := len(args) > 0 && (args[0] == "version" || args[0] == "help" || args[0] == "upgrade")
	if !skipCheck {
		checkForUpdates()
	}

	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		cfg, rest, err := parseBaseFlags("yolobox", args, projectDir)
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return fmt.Errorf("unexpected args: %v", rest)
		}
		return runShell(cfg)
	}

	switch args[0] {
	case "run":
		cfg, rest, err := parseBaseFlags("run", args[1:], projectDir)
		if err != nil {
			return err
		}
		if len(rest) == 0 {
			return fmt.Errorf("run requires a command")
		}
		return runCommand(cfg, rest, false)
	case "setup":
		_, err := runSetup()
		return err
	case "upgrade":
		return upgradeYolobox()
	case "config":
		cfg, rest, err := parseBaseFlags("config", args[1:], projectDir)
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return fmt.Errorf("unexpected args: %v", rest)
		}
		return printConfig(cfg)
	case "reset":
		return resetVolumes(args[1:])
	case "uninstall":
		return uninstallYolobox(args[1:])
	case "version":
		printVersion()
		return nil
	case "help":
		printUsage()
		return errHelp
	default:
		// Check if it's a tool shortcut (e.g., "yolobox claude", "yolobox codex")
		if isToolShortcut(args[0]) {
			cfg, rest, err := parseBaseFlags(args[0], args[1:], projectDir)
			if err != nil {
				return err
			}
			// Print logo before running tool
			fmt.Fprint(os.Stderr, colorCyan+logo+colorReset)
			// Build command: tool name + any remaining args
			command := append([]string{args[0]}, rest...)
			return runCommand(cfg, command, false)
		}
		return fmt.Errorf("unknown command: %s (try 'yolobox help')", args[0])
	}
}

func printVersion() {
	fmt.Printf("%syolobox%s %s%s%s (%s/%s)\n", colorBold, colorReset, colorCyan, Version, colorReset, runtime.GOOS, runtime.GOARCH)
}

func printUsage() {
	fmt.Fprint(os.Stderr, colorCyan+logo+colorReset)
	fmt.Fprintf(os.Stderr, "  %sFull-power AI agents, host-safe by default.%s\n\n", colorYellow, colorReset)
	fmt.Fprintf(os.Stderr, "  %sVersion:%s %s\n\n", colorBold, colorReset, Version)
	fmt.Fprintf(os.Stderr, "%sUSAGE:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  yolobox                     Start interactive shell in sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox run <cmd...>        Run a command in sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox setup               Configure yolobox settings")
	fmt.Fprintln(os.Stderr, "  yolobox upgrade             Upgrade binary and pull latest image")
	fmt.Fprintln(os.Stderr, "  yolobox config              Print resolved configuration")
	fmt.Fprintln(os.Stderr, "  yolobox reset --force       Remove named volumes (fresh start)")
	fmt.Fprintln(os.Stderr, "  yolobox uninstall --force   Uninstall yolobox completely")
	fmt.Fprintln(os.Stderr, "  yolobox version             Show version info")
	fmt.Fprintln(os.Stderr, "  yolobox help                Show this help")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sTOOL SHORTCUTS:%s\n", colorBold, colorReset)
	for _, tool := range toolShortcuts {
		fmt.Fprintf(os.Stderr, "  yolobox %-20sRun %s in sandbox\n", tool, tool)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sFLAGS:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  --runtime <name>      Container runtime: docker or podman")
	fmt.Fprintln(os.Stderr, "  --image <name>        Base image to use")
	fmt.Fprintln(os.Stderr, "  --setup               Run interactive setup before starting")
	fmt.Fprintln(os.Stderr, "  --mount <src:dst>     Extra mount (repeatable)")
	fmt.Fprintln(os.Stderr, "  --env <KEY=val>       Set environment variable (repeatable)")
	fmt.Fprintln(os.Stderr, "  --ssh-agent           Forward SSH agent socket")
	fmt.Fprintln(os.Stderr, "  --no-network          Disable network access")
	fmt.Fprintln(os.Stderr, "  --no-yolo             Disable AI CLIs YOLO mode")
	fmt.Fprintln(os.Stderr, "  --scratch             Fresh environment, no persistent volumes")
	fmt.Fprintln(os.Stderr, "  --readonly-project    Mount project directory read-only")
	fmt.Fprintln(os.Stderr, "  --claude-config       Copy host Claude config to container")
	fmt.Fprintln(os.Stderr, "  --git-config          Copy host git config to container")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sCONFIG:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  Global:  ~/.config/yolobox/config.toml")
	fmt.Fprintln(os.Stderr, "  Project: .yolobox.toml")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sAUTO-FORWARDED ENV VARS:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  ANTHROPIC_API_KEY, OPENAI_API_KEY, COPILOT_GITHUB_TOKEN, GH_TOKEN, GITHUB_TOKEN")
	fmt.Fprintln(os.Stderr, "  OPENROUTER_API_KEY, GEMINI_API_KEY, GEMINI_MODEL, GOOGLE_API_KEY")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sEXAMPLES:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  yolobox                     # Drop into a shell")
	fmt.Fprintln(os.Stderr, "  yolobox run make build      # Run make inside sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox run claude          # Run Claude Code in sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox --no-network        # Paranoid mode: no internet")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "  %sLet your AI go full send. Your home directory stays home.%s\n\n", colorPurple, colorReset)
}

// parseBaseFlags parses CLI flags and merges them with config files.
// projectDir is passed as a parameter (rather than calling os.Getwd() inside)
// to enable testing without mutating global working directory state.
func parseBaseFlags(name string, args []string, projectDir string) (Config, []string, error) {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return Config{}, nil, err
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = printUsage

	var (
		runtimeFlag     string
		imageFlag       string
		sshAgent        bool
		readonlyProject bool
		noNetwork       bool
		noYolo          bool
		scratch         bool
		claudeConfig    bool
		gitConfig       bool
		setup           bool
		mounts          stringSliceFlag
		envVars         stringSliceFlag
	)

	fs.StringVar(&runtimeFlag, "runtime", "", "container runtime")
	fs.StringVar(&imageFlag, "image", "", "container image")
	fs.BoolVar(&sshAgent, "ssh-agent", false, "mount SSH agent socket")
	fs.BoolVar(&readonlyProject, "readonly-project", false, "mount project read-only")
	fs.BoolVar(&noNetwork, "no-network", false, "disable network")
	fs.BoolVar(&noYolo, "no-yolo", false, "disable AI CLIs YOLO mode")
	fs.BoolVar(&scratch, "scratch", false, "fresh environment, no persistent volumes")
	fs.BoolVar(&claudeConfig, "claude-config", false, "copy host Claude config to container")
	fs.BoolVar(&gitConfig, "git-config", false, "copy host git config to container")
	fs.BoolVar(&setup, "setup", false, "run interactive setup before starting")
	fs.Var(&mounts, "mount", "extra mount src:dst")
	fs.Var(&envVars, "env", "environment variable KEY=value")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return Config{}, nil, errHelp
		}
		return Config{}, nil, err
	}

	if runtimeFlag != "" {
		cfg.Runtime = runtimeFlag
	}
	if imageFlag != "" {
		cfg.Image = imageFlag
	}
	if sshAgent {
		cfg.SSHAgent = true
	}
	if readonlyProject {
		cfg.ReadonlyProject = true
	}
	if noNetwork {
		cfg.NoNetwork = true
	}
	if noYolo {
		cfg.NoYolo = true
	}
	if scratch {
		cfg.Scratch = true
	}
	if claudeConfig {
		cfg.ClaudeConfig = true
	}
	if gitConfig {
		cfg.GitConfig = true
	}
	if setup {
		cfg.Setup = true
	}
	if len(mounts) > 0 {
		cfg.Mounts = append(cfg.Mounts, mounts...)
	}
	if len(envVars) > 0 {
		cfg.Env = append(cfg.Env, envVars...)
	}

	return cfg, fs.Args(), nil
}

func defaultConfig() Config {
	return Config{
		Image: "ghcr.io/finbarr/yolobox:latest",
	}
}

func loadConfig(projectDir string) (Config, error) {
	cfg := defaultConfig()

	globalPath, err := globalConfigPath()
	if err != nil {
		return Config{}, err
	}
	if err := mergeConfigFile(globalPath, &cfg); err != nil {
		return Config{}, err
	}

	projectPath := filepath.Join(projectDir, ".yolobox.toml")
	if err := mergeConfigFile(projectPath, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func globalConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "yolobox", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "yolobox", "config.toml"), nil
}

func mergeConfigFile(path string, cfg *Config) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var fileCfg Config
	if _, err := toml.DecodeFile(path, &fileCfg); err != nil {
		return err
	}

	mergeConfig(cfg, fileCfg)
	return nil
}

func mergeConfig(dst *Config, src Config) {
	if src.Runtime != "" {
		dst.Runtime = src.Runtime
	}
	if src.Image != "" {
		dst.Image = src.Image
	}
	if len(src.Mounts) > 0 {
		dst.Mounts = append([]string{}, src.Mounts...)
	}
	if len(src.Env) > 0 {
		dst.Env = append([]string{}, src.Env...)
	}
	if src.SSHAgent {
		dst.SSHAgent = true
	}
	if src.ReadonlyProject {
		dst.ReadonlyProject = true
	}
	if src.NoNetwork {
		dst.NoNetwork = true
	}
	if src.NoYolo {
		dst.NoYolo = true
	}
	if src.Scratch {
		dst.Scratch = true
	}
	if src.ClaudeConfig {
		dst.ClaudeConfig = true
	}
	if src.GitConfig {
		dst.GitConfig = true
	}
}

func runShell(cfg Config) error {
	// Run setup if explicitly requested via --setup flag
	if cfg.Setup {
		newCfg, err := runSetup()
		if err != nil {
			// If setup was cancelled, continue with defaults
			if err.Error() == "setup cancelled" {
				info("Using default settings")
			} else {
				return err
			}
		} else {
			// Merge setup results into config (preserving any CLI overrides)
			if !cfg.GitConfig {
				cfg.GitConfig = newCfg.GitConfig
			}
			if !cfg.SSHAgent {
				cfg.SSHAgent = newCfg.SSHAgent
			}
			if !cfg.NoNetwork {
				cfg.NoNetwork = newCfg.NoNetwork
			}
			if !cfg.NoYolo {
				cfg.NoYolo = newCfg.NoYolo
			}
		}
	}

	// Print logo before entering container
	fmt.Fprint(os.Stderr, colorCyan+logo+colorReset)

	err := runCommand(cfg, []string{"bash"}, true)
	if err != nil {
		return fmt.Errorf("failed to start shell: %w", err)
	}
	return nil
}

func runCommand(cfg Config, command []string, interactive bool) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Warn about scratch mode implications
	if cfg.Scratch {
		warn("Scratch mode: /home/yolo and /var/cache are ephemeral (data will not persist)")
		if cfg.ReadonlyProject {
			warn("Scratch mode with readonly-project: /output is ephemeral (copy files out before exiting)")
		}
	}

	// Warn if Docker has low memory (can cause OOM with Claude)
	checkDockerMemory(cfg.Runtime)

	args, err := buildRunArgs(cfg, projectDir, command, interactive)
	if err != nil {
		return err
	}
	return execRuntime(cfg.Runtime, args)
}

func printConfig(cfg Config) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Printf("%sruntime:%s %s\n", colorBold, colorReset, resolvedRuntimeName(cfg.Runtime))
	fmt.Printf("%simage:%s %s\n", colorBold, colorReset, cfg.Image)
	fmt.Printf("%sproject:%s %s\n", colorBold, colorReset, projectDir)
	fmt.Printf("%sssh_agent:%s %t\n", colorBold, colorReset, cfg.SSHAgent)
	fmt.Printf("%sreadonly_project:%s %t\n", colorBold, colorReset, cfg.ReadonlyProject)
	fmt.Printf("%sno_network:%s %t\n", colorBold, colorReset, cfg.NoNetwork)
	fmt.Printf("%sno_yolo:%s %t\n", colorBold, colorReset, cfg.NoYolo)
	fmt.Printf("%sscratch:%s %t\n", colorBold, colorReset, cfg.Scratch)
	fmt.Printf("%sclaude_config:%s %t\n", colorBold, colorReset, cfg.ClaudeConfig)
	fmt.Printf("%sgit_config:%s %t\n", colorBold, colorReset, cfg.GitConfig)
	if len(cfg.Mounts) > 0 {
		fmt.Printf("%smounts:%s\n", colorBold, colorReset)
		for _, m := range cfg.Mounts {
			fmt.Printf("  - %s\n", m)
		}
	}
	if len(cfg.Env) > 0 {
		fmt.Printf("%senv:%s\n", colorBold, colorReset)
		for _, e := range cfg.Env {
			fmt.Printf("  - %s\n", e)
		}
	}
	return nil
}

// globalConfigExists checks if the global config file exists
func globalConfigExists() bool {
	path, err := globalConfigPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// saveGlobalConfig writes config to the global config file
func saveGlobalConfig(cfg Config) error {
	path, err := globalConfigPath()
	if err != nil {
		return err
	}

	// Create config directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Build TOML content (only non-default values)
	var lines []string
	if cfg.GitConfig {
		lines = append(lines, "git_config = true")
	}
	if cfg.SSHAgent {
		lines = append(lines, "ssh_agent = true")
	}
	if cfg.NoNetwork {
		lines = append(lines, "no_network = true")
	}
	if cfg.NoYolo {
		lines = append(lines, "no_yolo = true")
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// yoloboxTheme returns a custom huh theme matching the yolobox brand
func yoloboxTheme() *huh.Theme {
	t := huh.ThemeBase()

	purple := lipgloss.Color("35")  // magenta/purple
	cyan := lipgloss.Color("36")    // cyan
	yellow := lipgloss.Color("33")  // yellow
	white := lipgloss.Color("15")   // bright white

	// Title styling - purple and bold
	t.Focused.Title = t.Focused.Title.Foreground(purple).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(white)

	// Selection styling
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(yellow)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(cyan)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(white)

	// Multi-select styling
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(yellow)
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(cyan).SetString("[x] ")
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(white).SetString("[ ] ")

	return t
}

// runSetup runs the interactive setup wizard
func runSetup() (Config, error) {
	cfg := Config{}

	// Load existing config as defaults
	if globalConfigExists() {
		projectDir, _ := os.Getwd()
		existing, err := loadConfig(projectDir)
		if err == nil {
			cfg = existing
		}
	}

	// Form fields
	var selectedOptions []string

	// Initialize from current config
	if cfg.GitConfig {
		selectedOptions = append(selectedOptions, "git_config")
	}
	if cfg.SSHAgent {
		selectedOptions = append(selectedOptions, "ssh_agent")
	}
	if cfg.NoNetwork {
		selectedOptions = append(selectedOptions, "no_network")
	}
	if cfg.NoYolo {
		selectedOptions = append(selectedOptions, "no_yolo")
	}

	// Print header with box
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("35")). // purple
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("35")).
		Padding(0, 2)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, headerStyle.Render("yolobox setup"))
	fmt.Fprintln(os.Stderr)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("What do you want inside the box?").
				Options(
					huh.NewOption("Git identity (copy ~/.gitconfig)", "git_config"),
					huh.NewOption("SSH agent (for git over SSH)", "ssh_agent"),
					huh.NewOption("No network (disable internet access)", "no_network"),
					huh.NewOption("No YOLO (disable auto-confirm in AI CLIs)", "no_yolo"),
				).
				Value(&selectedOptions),
		),
	).WithTheme(yoloboxTheme())

	err := form.Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return Config{}, fmt.Errorf("setup cancelled")
		}
		return Config{}, err
	}

	// Build config from form values
	cfg.GitConfig = contains(selectedOptions, "git_config")
	cfg.SSHAgent = contains(selectedOptions, "ssh_agent")
	cfg.NoNetwork = contains(selectedOptions, "no_network")
	cfg.NoYolo = contains(selectedOptions, "no_yolo")

	// Save to global config
	if err := saveGlobalConfig(cfg); err != nil {
		return cfg, err
	}

	path, _ := globalConfigPath()
	success("Locked in! Config saved to %s", path)
	fmt.Fprintf(os.Stderr, "  %sRun %syolobox setup%s%s anytime to change these settings.%s\n\n", colorCyan, colorBold, colorReset, colorCyan, colorReset)

	return cfg, nil
}

// contains checks if a string slice contains a value
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// isToolShortcut checks if a command is a tool shortcut
func isToolShortcut(cmd string) bool {
	return contains(toolShortcuts, cmd)
}

func resetVolumes(args []string) error {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "remove volumes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return errHelp
		}
		return err
	}
	if !*force {
		return fmt.Errorf("reset requires --force (this will delete all cached data)")
	}

	cfg, err := loadConfigFromEnv()
	if err != nil {
		return err
	}
	runtime, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}

	warn("Removing yolobox volumes...")
	volumes := []string{"yolobox-home", "yolobox-cache"}
	args = append([]string{"volume", "rm"}, volumes...)
	if err := execCommand(runtime, args); err != nil {
		return err
	}
	success("Fresh start! All volumes removed.")
	return nil
}

func uninstallYolobox(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "confirm uninstall")
	keepVolumes := fs.Bool("keep-volumes", false, "keep Docker volumes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return errHelp
		}
		return err
	}
	if !*force {
		fmt.Println("This will remove:")
		fmt.Println("  - yolobox binary")
		fmt.Println("  - ~/.config/yolobox/ (config and cache)")
		if !*keepVolumes {
			fmt.Println("  - Docker volumes (yolobox-home, yolobox-cache)")
		}
		fmt.Println("")
		return fmt.Errorf("run with --force to confirm (use --keep-volumes to preserve Docker data)")
	}

	// Remove config directory
	configDir, err := os.UserConfigDir()
	if err == nil {
		yoloboxConfig := filepath.Join(configDir, "yolobox")
		if _, err := os.Stat(yoloboxConfig); err == nil {
			info("Removing %s...", yoloboxConfig)
			os.RemoveAll(yoloboxConfig)
		}
	}

	// Remove Docker volumes unless --keep-volumes
	if !*keepVolumes {
		cfg, err := loadConfigFromEnv()
		if err == nil {
			runtime, err := resolveRuntime(cfg.Runtime)
			if err == nil {
				info("Removing Docker volumes...")
				volumes := []string{"yolobox-home", "yolobox-cache", "yolobox-output"}
				execCommand(runtime, append([]string{"volume", "rm", "-f"}, volumes...))
			}
		}
	}

	// Remove binary (do this last)
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	info("Removing %s...", execPath)
	if err := os.Remove(execPath); err != nil {
		return fmt.Errorf("failed to remove binary: %w (try: sudo rm %s)", err, execPath)
	}

	success("yolobox has been uninstalled. Goodbye!")
	return nil
}

func loadConfigFromEnv() (Config, error) {
	projectDir, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	return loadConfig(projectDir)
}

func buildRunArgs(cfg Config, projectDir string, command []string, interactive bool) ([]string, error) {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}

	args := []string{"run", "--rm"}

	// Add -it if explicitly interactive, or if stdin/stdout are both terminals
	// This allows "yolobox run claude" to work interactively while still
	// supporting piping (e.g., "echo foo | yolobox run cat")
	isTTY := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	if interactive || isTTY {
		args = append(args, "-it")
	}

	args = append(args, "-w", "/workspace")
	args = append(args, "-e", "YOLOBOX=1")
	if cfg.NoYolo {
		args = append(args, "-e", "NO_YOLO=1")
	}
	if termEnv := os.Getenv("TERM"); termEnv != "" {
		args = append(args, "-e", "TERM="+termEnv)
	}
	if lang := os.Getenv("LANG"); lang != "" {
		args = append(args, "-e", "LANG="+lang)
	}

	// Auto-passthrough common API keys
	for _, key := range autoPassthroughEnvVars {
		if val := os.Getenv(key); val != "" {
			args = append(args, "-e", key+"="+val)
		}
	}

	// User-specified env vars
	for _, env := range cfg.Env {
		args = append(args, "-e", env)
	}

	// Project mount (optionally read-only)
	projectMount := absProject + ":/workspace"
	if cfg.ReadonlyProject {
		projectMount += ":ro"
		// Create a writable output directory
		if cfg.Scratch {
			args = append(args, "-v", "/output") // anonymous volume, deleted with container
		} else {
			args = append(args, "-v", "yolobox-output:/output")
		}
	}
	args = append(args, "-v", projectMount)

	// Named volumes for persistence (skip if --scratch)
	if !cfg.Scratch {
		args = append(args, "-v", "yolobox-home:/home/yolo")
		args = append(args, "-v", "yolobox-cache:/var/cache")
	}

	// Mount Claude config from host to staging area (copied to /home/yolo by entrypoint)
	if cfg.ClaudeConfig {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		claudeConfigDir := filepath.Join(home, ".claude")
		if _, err := os.Stat(claudeConfigDir); err == nil {
			args = append(args, "-v", claudeConfigDir+":/host-claude/.claude:ro")
		}
		claudeConfigFile := filepath.Join(home, ".claude.json")
		if _, err := os.Stat(claudeConfigFile); err == nil {
			args = append(args, "-v", claudeConfigFile+":/host-claude/.claude.json:ro")
		}
	}

	// Mount git config from host to staging area (copied to /home/yolo by entrypoint)
	if cfg.GitConfig {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		gitConfigFile := filepath.Join(home, ".gitconfig")
		if _, err := os.Stat(gitConfigFile); err == nil {
			args = append(args, "-v", gitConfigFile+":/host-git/.gitconfig:ro")
		}
	}

	// Extra mounts
	for _, mount := range cfg.Mounts {
		resolved, err := resolveMount(mount, absProject)
		if err != nil {
			return nil, err
		}
		args = append(args, "-v", resolved)
	}

	// SSH agent forwarding
	if cfg.SSHAgent {
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			warn("SSH_AUTH_SOCK not set; skipping ssh-agent mount")
		} else {
			args = append(args, "-v", sock+":/ssh-agent")
			args = append(args, "-e", "SSH_AUTH_SOCK=/ssh-agent")
		}
	}

	// Network isolation
	if cfg.NoNetwork {
		args = append(args, "--network", "none")
	}

	args = append(args, cfg.Image)
	args = append(args, command...)
	return args, nil
}

func resolveMount(mount string, projectDir string) (string, error) {
	parts := strings.SplitN(mount, ":", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid mount %q; expected src:dst", mount)
	}
	src := parts[0]
	dst := parts[1]
	var opts string
	if len(parts) == 3 {
		opts = parts[2]
	}

	resolved, err := resolvePath(src, projectDir)
	if err != nil {
		return "", err
	}
	if opts != "" {
		return fmt.Sprintf("%s:%s:%s", resolved, dst, opts), nil
	}
	return fmt.Sprintf("%s:%s", resolved, dst), nil
}

func resolvePath(path string, projectDir string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
	}
	if strings.HasPrefix(path, ".") || strings.HasPrefix(path, "/") {
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectDir, path)
		}
		return filepath.Clean(path), nil
	}
	return path, nil
}

func resolvedRuntimeName(name string) string {
	if name == "" {
		return "auto"
	}
	if name == "colima" {
		return "docker"
	}
	return name
}

func resolveRuntime(name string) (string, error) {
	if name == "" {
		if path, err := exec.LookPath("docker"); err == nil {
			return path, nil
		}
		if path, err := exec.LookPath("podman"); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("no container runtime found. Install docker or podman and try again")
	}
	if name == "colima" {
		name = "docker"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("runtime %q not found in PATH", name)
	}
	return path, nil
}

func execRuntime(runtime string, args []string) error {
	runtimePath, err := resolveRuntime(runtime)
	if err != nil {
		return err
	}
	return execCommand(runtimePath, args)
}

// checkDockerMemory warns if Docker has less than 4GB RAM available
func checkDockerMemory(runtime string) {
	runtimePath, err := resolveRuntime(runtime)
	if err != nil {
		return
	}

	cmd := exec.Command(runtimePath, "info", "--format", "{{.MemTotal}}")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	memBytes, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return
	}

	memGB := float64(memBytes) / (1024 * 1024 * 1024)
	if memGB < 3.5 {
		warn("Docker has only %.1fGB RAM. Claude Code may get OOM killed.", memGB)
		warn("Increase Docker/Colima memory to 4GB+ for best results.")
	}
}

func execCommand(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// GitHub release info
type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func upgradeYolobox() error {
	info("Checking for updates...")

	// Get latest release from GitHub
	resp, err := http.Get("https://api.github.com/repos/finbarr/yolobox/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to check for updates: HTTP %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("failed to parse release info: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(Version, "v")

	if latestVersion == currentVersion {
		success("Already at latest version (%s)", Version)
	} else {
		info("New version available: %s (current: %s)", latestVersion, currentVersion)

		// Find the right binary for this platform
		binaryName := fmt.Sprintf("yolobox-%s-%s", runtime.GOOS, runtime.GOARCH)
		var downloadURL string
		for _, asset := range release.Assets {
			if asset.Name == binaryName {
				downloadURL = asset.BrowserDownloadURL
				break
			}
		}

		if downloadURL == "" {
			return fmt.Errorf("no binary available for %s/%s", runtime.GOOS, runtime.GOARCH)
		}

		// Download new binary
		info("Downloading %s...", binaryName)
		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("failed to download: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return fmt.Errorf("failed to download: HTTP %d", resp.StatusCode)
		}

		// Get current executable path
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return fmt.Errorf("failed to resolve executable path: %w", err)
		}

		// Write to temp file first
		tmpFile, err := os.CreateTemp(filepath.Dir(execPath), "yolobox-upgrade-*")
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()

		_, err = io.Copy(tmpFile, resp.Body)
		tmpFile.Close()
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to write binary: %w", err)
		}

		// Make executable
		if err := os.Chmod(tmpPath, 0755); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to chmod: %w", err)
		}

		// Replace current binary
		if err := os.Rename(tmpPath, execPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to replace binary: %w", err)
		}

		success("Binary upgraded to %s", latestVersion)
	}

	// Also pull latest Docker image
	info("Pulling latest Docker image...")
	cfg := defaultConfig()
	runtime, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}
	if err := execCommand(runtime, []string{"pull", cfg.Image}); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	success("Upgrade complete!")
	return nil
}

// Output helpers with colors
func success(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, colorGreen+"✓ "+colorReset+format+"\n", args...)
}

func info(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, colorBlue+"→ "+colorReset+format+"\n", args...)
}

func warn(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, colorYellow+"⚠ "+colorReset+format+"\n", args...)
}

func errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, colorRed+"✗ "+colorReset+format+"\n", args...)
}
