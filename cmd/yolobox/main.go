package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var Version = "dev"

const (
	logo = `
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
	"CLAUDE_CODE_OAUTH_TOKEN",
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

var sizePattern = regexp.MustCompile(`^\d+(?:\.\d+)?(?:[kKmMgGtTpP](?:i?[bB]?)?|[bB])?$`)
var packageNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.+\-:]*$`)
var versionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

var errHelp = errors.New("help requested")

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

	return runCmdArgs(args, projectDir, nil)
}

func runCmdArgs(args []string, projectDir string, fork *ForkConfig) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		cfg, rest, err := parseBaseFlags("yolobox", args, projectDir)
		if err != nil {
			return err
		}
		applyForkConfig(&cfg, fork)
		if len(rest) != 0 {
			return fmt.Errorf("unexpected args: %v\n  Hint: flags go after the subcommand: yolobox run --flag cmd (not yolobox --flag run cmd)", rest)
		}
		return runShell(cfg)
	}

	switch args[0] {
	case "run":
		cfg, rest, err := parseBaseFlags("run", args[1:], projectDir)
		if err != nil {
			return err
		}
		applyForkConfig(&cfg, fork)
		if len(rest) == 0 {
			return fmt.Errorf("run requires a command")
		}
		return runCommand(cfg, rest, false)
	case "fork":
		return runFork(args[1:], projectDir)
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
		applyForkConfig(&cfg, fork)
		if len(rest) != 0 {
			return fmt.Errorf("unexpected args: %v", rest)
		}
		if err := validateRuntimeConstraints(cfg); err != nil {
			return err
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
			toolName := args[0]
			// Split args so tool-specific flags (like --resume) pass through
			yoloboxArgs, toolArgs := splitToolArgs(args[1:])

			cfg, rest, err := parseBaseFlags(toolName, yoloboxArgs, projectDir)
			if err != nil {
				return err
			}
			applyForkConfig(&cfg, fork)

			// Combine any remaining args from flag parsing with tool args
			allToolArgs := append(rest, toolArgs...)

			// Print logo before running tool
			fmt.Fprint(os.Stderr, colorCyan+logo+colorReset)
			// Build command: tool name + any tool args
			command := append([]string{toolName}, allToolArgs...)
			return runCommand(cfg, command, false)
		}
		return fmt.Errorf("unknown command: %s (try 'yolobox help')\n  Hint: if using flags, put them after the subcommand: yolobox run --flag cmd", args[0])
	}
}

func applyForkConfig(cfg *Config, fork *ForkConfig) {
	if fork == nil || fork.Name == "" {
		return
	}
	cfg.Fork = *fork
	cfg.Env = append(cfg.Env,
		"YOLOBOX_FORK_NAME="+fork.Name,
		"YOLOBOX_FORK_SOURCE="+fork.Source,
		"YOLOBOX_FORK_COPY="+fork.Copy,
		"COMPOSE_PROJECT_NAME="+fork.ComposeProject,
	)
}

func wrapCommaList(items []string, maxWidth int) []string {
	if len(items) == 0 {
		return nil
	}

	var lines []string
	current := items[0]
	for _, item := range items[1:] {
		candidate := current + ", " + item
		if len(candidate) > maxWidth {
			lines = append(lines, current)
			current = item
			continue
		}
		current = candidate
	}
	lines = append(lines, current)
	return lines
}

func printUsage() {
	fmt.Fprint(os.Stderr, colorCyan+logo+colorReset)
	fmt.Fprintf(os.Stderr, "  %sFull-power AI agents, host-safe by default.%s\n\n", colorYellow, colorReset)
	fmt.Fprintf(os.Stderr, "  %sVersion:%s %s\n\n", colorBold, colorReset, Version)
	fmt.Fprintf(os.Stderr, "%sUSAGE:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  yolobox                     Start interactive shell in sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox run <cmd...>        Run a command in sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox fork --name <env> <cmd>  Run in a named copied folder with Compose namespace")
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
	fmt.Fprintln(os.Stderr, "  --runtime <name>      Container runtime: docker, podman, or container")
	fmt.Fprintln(os.Stderr, "  --image <name>        Base image to use")
	fmt.Fprintln(os.Stderr, "  --pod <name>          Join existing Podman pod (shares its network)")
	fmt.Fprintln(os.Stderr, "  --setup               Run interactive setup before starting")
	fmt.Fprintln(os.Stderr, "  --mount <src:dst>     Extra mount (repeatable)")
	fmt.Fprintln(os.Stderr, "  --exclude <glob>      Hide project paths from the container (repeatable)")
	fmt.Fprintln(os.Stderr, "  --copy-as <src:dst>   Mount a file at a project path inside the container")
	fmt.Fprintln(os.Stderr, "  --env <KEY=val>       Set environment variable (repeatable)")
	fmt.Fprintln(os.Stderr, "  --ssh-agent           Forward SSH agent socket")
	fmt.Fprintln(os.Stderr, "  --no-network          Disable network access (default: network enabled)")
	fmt.Fprintln(os.Stderr, "  --network <name>      Join container network (e.g., docker compose network)")
	fmt.Fprintln(os.Stderr, "  --no-yolo             Disable AI CLIs YOLO mode")
	fmt.Fprintln(os.Stderr, "  --scratch             Fresh environment, no persistent volumes")
	fmt.Fprintln(os.Stderr, "  --readonly-project    Mount project directory read-only")
	fmt.Fprintln(os.Stderr, "  --claude-config       Copy host Claude config to container")
	fmt.Fprintln(os.Stderr, "  --codex-config        Copy host Codex config to container")
	fmt.Fprintln(os.Stderr, "  --gemini-config       Copy host Gemini config to container")
	fmt.Fprintln(os.Stderr, "  --opencode-config     Copy host OpenCode config to container")
	fmt.Fprintln(os.Stderr, "  --git-config          Copy host git config to container")
	fmt.Fprintln(os.Stderr, "  --gh-token            Forward GitHub token for gh and HTTPS git")
	fmt.Fprintln(os.Stderr, "  --copy-agent-instructions  Copy global agent instructions and skills")
	fmt.Fprintln(os.Stderr, "  --docker              Mount Docker socket and join shared network")
	fmt.Fprintln(os.Stderr, "  --clipboard           Bridge text clipboard copy/paste to the host")
	fmt.Fprintln(os.Stderr, "  --cpus <count>        Limit number of CPUs (supports fractions)")
	fmt.Fprintln(os.Stderr, "  --memory <size>       Cap memory usage (e.g., 4g, 512m)")
	fmt.Fprintln(os.Stderr, "  --shm-size <size>     Size of /dev/shm (e.g., 1g for Playwright)")
	fmt.Fprintln(os.Stderr, "  --gpus <spec>         GPU devices to add (e.g., all, device=0)")
	fmt.Fprintln(os.Stderr, "  --device <spec>       Pass a host device through (repeatable)")
	fmt.Fprintln(os.Stderr, "  --cap-add <name>      Add a Linux capability (repeatable)")
	fmt.Fprintln(os.Stderr, "  --cap-drop <name>     Drop a Linux capability (repeatable)")
	fmt.Fprintln(os.Stderr, "  --runtime-arg <flag>  Raw runtime flag passthrough (repeatable)")
	fmt.Fprintln(os.Stderr, "  --packages <list>     Comma-separated apt packages for a custom image")
	fmt.Fprintln(os.Stderr, "  --customize-file <path> Dockerfile fragment for a custom image")
	fmt.Fprintln(os.Stderr, "  --rebuild-image       Force rebuild of the custom image")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sCONFIG:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  Global:  ~/.config/yolobox/config.toml")
	fmt.Fprintln(os.Stderr, "  Project: .yolobox.toml")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sAUTO-FORWARDED ENV VARS:%s\n", colorBold, colorReset)
	for _, line := range wrapCommaList(autoPassthroughEnvVars, 76) {
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sEXAMPLES:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  yolobox                     # Drop into a shell")
	fmt.Fprintln(os.Stderr, "  yolobox run make build      # Run make inside sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox fork --name bruno codex  # Developer env + Compose namespace")
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
		runtimeFlag           string
		imageFlag             string
		podFlag               string
		networkFlag           string
		sshAgent              bool
		readonlyProject       bool
		noNetwork             bool
		noYolo                bool
		scratch               bool
		claudeConfig          bool
		codexConfig           bool
		geminiConfig          bool
		opencodeConfig        bool
		gitConfig             bool
		ghToken               bool
		copyAgentInstructions bool
		docker                bool
		clipboard             bool
		setup                 bool
		mounts                stringSliceFlag
		excludes              stringSliceFlag
		copyAs                stringSliceFlag
		envVars               stringSliceFlag

		// Resource limits & security
		cpus          string
		memoryLimit   string
		shmSize       string
		gpus          string
		devices       stringSliceFlag
		capAdd        stringSliceFlag
		capDrop       stringSliceFlag
		runtimeArgs   stringSliceFlag
		packages      string
		customizeFile string
		rebuildImage  bool
	)

	fs.StringVar(&runtimeFlag, "runtime", "", "container runtime")
	fs.StringVar(&imageFlag, "image", "", "container image")
	fs.StringVar(&podFlag, "pod", "", "join existing podman pod")
	fs.StringVar(&networkFlag, "network", "", "container network to join")
	fs.BoolVar(&sshAgent, "ssh-agent", false, "mount SSH agent socket")
	fs.BoolVar(&readonlyProject, "readonly-project", false, "mount project read-only")
	fs.BoolVar(&noNetwork, "no-network", false, "disable network")
	fs.BoolVar(&noYolo, "no-yolo", false, "disable AI CLIs YOLO mode")
	fs.BoolVar(&scratch, "scratch", false, "fresh environment, no persistent volumes")
	fs.BoolVar(&claudeConfig, "claude-config", false, "copy host Claude config to container")
	fs.BoolVar(&codexConfig, "codex-config", false, "copy host Codex config to container")
	fs.BoolVar(&geminiConfig, "gemini-config", false, "copy host Gemini config to container")
	fs.BoolVar(&opencodeConfig, "opencode-config", false, "copy host OpenCode config to container")
	fs.BoolVar(&gitConfig, "git-config", false, "copy host git config to container")
	fs.BoolVar(&ghToken, "gh-token", false, "forward GitHub CLI token (from gh auth token)")
	fs.BoolVar(&copyAgentInstructions, "copy-agent-instructions", false, "copy agent instruction files and skills (CLAUDE.md, ~/.claude/skills, GEMINI.md, AGENTS.md, ~/.codex/skills)")
	fs.BoolVar(&docker, "docker", false, "mount Docker socket and join shared network")
	fs.BoolVar(&clipboard, "clipboard", false, "bridge text clipboard copy/paste to the host")
	fs.BoolVar(&setup, "setup", false, "run interactive setup before starting")
	fs.Var(&mounts, "mount", "extra mount src:dst")
	fs.Var(&excludes, "exclude", "hide matching project paths from the container")
	fs.Var(&copyAs, "copy-as", "mount a file at another project path inside the container")
	fs.Var(&envVars, "env", "environment variable KEY=value")

	// Resource limits & security
	fs.StringVar(&cpus, "cpus", "", "limit number of CPUs (supports fractions)")
	fs.StringVar(&memoryLimit, "memory", "", "memory limit (e.g., 8g)")
	fs.StringVar(&shmSize, "shm-size", "", "size of /dev/shm (e.g., 1g)")
	fs.StringVar(&gpus, "gpus", "", "GPU devices to add (e.g., all or device IDs)")
	fs.Var(&devices, "device", "add host device inside the container (repeatable)")
	fs.Var(&capAdd, "cap-add", "add Linux capability (repeatable)")
	fs.Var(&capDrop, "cap-drop", "drop Linux capability (repeatable)")
	fs.Var(&runtimeArgs, "runtime-arg", "raw runtime flag to pass through to the container engine (repeatable)")
	fs.StringVar(&packages, "packages", "", "comma-separated apt packages for a custom image")
	fs.StringVar(&customizeFile, "customize-file", "", "path to a Dockerfile fragment for a custom image")
	fs.BoolVar(&rebuildImage, "rebuild-image", false, "force rebuild of the custom image")

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
	if podFlag != "" {
		cfg.Pod = podFlag
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
	if networkFlag != "" {
		cfg.Network = networkFlag
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
	if codexConfig {
		cfg.CodexConfig = true
	}
	if geminiConfig {
		cfg.GeminiConfig = true
	}
	if opencodeConfig {
		cfg.OpencodeConfig = true
	}
	if gitConfig {
		cfg.GitConfig = true
	}
	if ghToken {
		cfg.GhToken = true
	}
	if copyAgentInstructions {
		cfg.CopyAgentInstructions = true
	}
	if docker {
		cfg.Docker = true
	}
	if clipboard {
		cfg.Clipboard = true
	}
	if setup {
		cfg.Setup = true
	}
	if len(mounts) > 0 {
		cfg.Mounts = append(cfg.Mounts, mounts...)
	}
	if len(excludes) > 0 {
		cfg.Exclude = append(cfg.Exclude, excludes...)
	}
	if len(copyAs) > 0 {
		cfg.CopyAs = append(cfg.CopyAs, copyAs...)
	}
	if len(envVars) > 0 {
		cfg.Env = append(cfg.Env, envVars...)
	}

	if cpus != "" {
		cfg.CPUs = cpus
	}
	if memoryLimit != "" {
		cfg.Memory = memoryLimit
	}
	if shmSize != "" {
		cfg.ShmSize = shmSize
	}
	if gpus != "" {
		cfg.GPUs = gpus
	}

	if len(devices) > 0 {
		cfg.Devices = append(cfg.Devices, devices...)
	}
	if len(capAdd) > 0 {
		cfg.CapAdd = append(cfg.CapAdd, capAdd...)
	}
	if len(capDrop) > 0 {
		cfg.CapDrop = append(cfg.CapDrop, capDrop...)
	}
	if len(runtimeArgs) > 0 {
		cfg.RuntimeArgs = append(cfg.RuntimeArgs, runtimeArgs...)
	}
	if packages != "" {
		cfg.Customize.Packages = append(cfg.Customize.Packages, parseCommaSeparatedValues(packages)...)
	}
	if customizeFile != "" {
		cfg.Customize.Dockerfile = customizeFile
	}
	if rebuildImage {
		cfg.RebuildImage = true
	}

	// Validate conflicting options after config + CLI values have been merged.
	if err := validateConfigConflicts(cfg); err != nil {
		return cfg, nil, err
	}
	if err := validateRuntimeOptions(cfg); err != nil {
		return cfg, nil, err
	}
	if err := validateCustomizeConfig(cfg.Customize); err != nil {
		return cfg, nil, err
	}
	if err := validateProjectFilteringConfig(cfg, projectDir); err != nil {
		return cfg, nil, err
	}

	return cfg, fs.Args(), nil
}

func validateConfigConflicts(cfg Config) error {
	if cfg.Network != "" && cfg.NoNetwork {
		return fmt.Errorf("cannot use --network with --no-network")
	}
	if cfg.Docker && cfg.NoNetwork {
		return fmt.Errorf("cannot use --docker with --no-network")
	}
	if cfg.Clipboard && cfg.NoNetwork {
		return fmt.Errorf("cannot use --clipboard with --no-network")
	}
	if cfg.Pod != "" {
		if cfg.Network != "" {
			return fmt.Errorf("cannot use --pod with --network")
		}
		if cfg.NoNetwork {
			return fmt.Errorf("cannot use --pod with --no-network")
		}
		if cfg.Docker {
			return fmt.Errorf("cannot use --pod with --docker")
		}
	}
	return nil
}

func validateRuntimeConstraints(cfg Config) error {
	if cfg.Pod == "" {
		return nil
	}

	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}
	if filepath.Base(runtimePath) != "podman" {
		return fmt.Errorf("--pod requires the podman runtime (set --runtime podman)")
	}
	return nil
}

func validateRuntimeOptions(cfg Config) error {
	if cfg.CPUs != "" {
		cpuVal, err := strconv.ParseFloat(cfg.CPUs, 64)
		if err != nil || cpuVal <= 0 {
			return fmt.Errorf("invalid --cpus value %q: must be a positive number", cfg.CPUs)
		}
	}

	for _, field := range []struct {
		name  string
		value string
	}{
		{"memory", cfg.Memory},
		{"shm-size", cfg.ShmSize},
	} {
		if field.value == "" {
			continue
		}
		if !sizePattern.MatchString(field.value) {
			return fmt.Errorf("invalid --%s value %q: expected a number optionally followed by k/m/g/t", field.name, field.value)
		}
	}

	for _, arg := range cfg.RuntimeArgs {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("--runtime-arg entries cannot be blank")
		}
	}

	return nil
}

func warnSecurityRelaxations(cfg Config) {
	var categories []string
	if len(cfg.CapAdd) > 0 {
		categories = append(categories, "--cap-add")
	}
	if len(cfg.Devices) > 0 {
		categories = append(categories, "--device")
	}
	if runtimeArgsContainUnconfined(cfg.RuntimeArgs) {
		categories = append(categories, "--security-opt seccomp=unconfined")
	}
	if len(categories) == 0 {
		return
	}
	warn("Security-impacting runtime flags active (%s). Ensure you trust the workload.", strings.Join(categories, ", "))
}

func runtimeArgsContainUnconfined(args []string) bool {
	for _, arg := range args {
		if strings.Contains(strings.ToLower(arg), "seccomp=unconfined") {
			return true
		}
	}
	return false
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
			// Merge setup results into config (preserving any CLI overrides).
			// mergeConfig applies cfg's non-zero values on top of newCfg,
			// so CLI/config-file settings win and setup fills the gaps.
			mergeConfig(&newCfg, cfg)
			newCfg.Fork = cfg.Fork
			cfg = newCfg
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

	if err := validateRuntimeConstraints(cfg); err != nil {
		return err
	}
	if hasCustomization(cfg) {
		customImage, err := prepareCustomImage(&cfg, projectDir)
		if err != nil {
			return err
		}
		cfg.Image = customImage
	}
	warnSecurityRelaxations(cfg)

	// Warn if Docker has low memory (can cause OOM with Claude)
	checkDockerMemory(cfg.Runtime)

	var clipboard *clipboardBridge
	if cfg.Clipboard {
		clipboard, err = startClipboardBridge(cfg.Runtime)
		if err != nil {
			return err
		}
		defer clipboard.Close()
		cfg.ClipboardEndpoint = clipboard.Endpoint
		cfg.ClipboardToken = clipboard.Token
		info("Host clipboard bridge enabled")
	}

	// Ensure Docker network exists before starting container
	if cfg.Docker {
		networkName := cfg.Network
		if networkName == "" {
			networkName = "yolobox-net"
		}
		if err := ensureDockerNetwork(cfg.Runtime, networkName); err != nil {
			return err
		}
	}

	args, cleanupPaths, err := buildRunArgs(cfg, projectDir, command, interactive)
	if err != nil {
		return err
	}
	// Clean up temp files after the container exits, regardless of outcome
	defer func() {
		for _, p := range cleanupPaths {
			_ = os.RemoveAll(p)
		}
	}()
	return execRuntime(cfg.Runtime, args)
}

func formatTomlStringSlice(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		quoted = append(quoted, fmt.Sprintf("%q", v))
	}
	if len(quoted) == 0 {
		return "[]"
	}
	return fmt.Sprintf("[%s]", strings.Join(quoted, ", "))
}

func parseMultilineInput(input string) []string {
	if input == "" {
		return nil
	}
	input = strings.ReplaceAll(input, "\r\n", "\n")
	lines := strings.Split(input, "\n")
	var values []string
	for _, line := range lines {
		val := strings.TrimSpace(line)
		if val == "" {
			continue
		}
		values = append(values, val)
	}
	return values
}

// yoloboxTheme returns a custom huh theme matching the yolobox brand
func yoloboxTheme() *huh.Theme {
	t := huh.ThemeBase()

	purple := lipgloss.Color("35") // magenta/purple
	cyan := lipgloss.Color("36")   // cyan
	yellow := lipgloss.Color("33") // yellow
	white := lipgloss.Color("15")  // bright white

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
	cfg, err := loadSetupDefaults()
	if err != nil {
		return Config{}, err
	}

	// Form fields
	var selectedOptions []string
	podName := cfg.Pod
	cpuLimit := cfg.CPUs
	memoryLimit := cfg.Memory
	shmLimit := cfg.ShmSize
	gpuSetting := cfg.GPUs
	deviceList := strings.Join(cfg.Devices, "\n")
	capAddList := strings.Join(cfg.CapAdd, "\n")
	capDropList := strings.Join(cfg.CapDrop, "\n")
	runtimeArgList := strings.Join(cfg.RuntimeArgs, "\n")

	// Initialize from current config
	if cfg.GitConfig {
		selectedOptions = append(selectedOptions, "git_config")
	}
	if cfg.ClaudeConfig {
		selectedOptions = append(selectedOptions, "claude_config")
	}
	if cfg.CodexConfig {
		selectedOptions = append(selectedOptions, "codex_config")
	}
	if cfg.GeminiConfig {
		selectedOptions = append(selectedOptions, "gemini_config")
	}
	if cfg.OpencodeConfig {
		selectedOptions = append(selectedOptions, "opencode_config")
	}
	if cfg.GhToken {
		selectedOptions = append(selectedOptions, "gh_token")
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
	if cfg.Docker {
		selectedOptions = append(selectedOptions, "docker")
	}
	if cfg.Clipboard {
		selectedOptions = append(selectedOptions, "clipboard")
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
					huh.NewOption("Claude config (copy ~/.claude and ~/.claude.json)", "claude_config"),
					huh.NewOption("Codex config (copy ~/.codex)", "codex_config"),
					huh.NewOption("Gemini config (copy ~/.gemini)", "gemini_config"),
					huh.NewOption("OpenCode config (copy ~/.config/opencode)", "opencode_config"),
					huh.NewOption("GitHub token (gh + HTTPS git auth)", "gh_token"),
					huh.NewOption("SSH agent (for git over SSH)", "ssh_agent"),
					huh.NewOption("Docker socket (run containers from sandbox)", "docker"),
					huh.NewOption("Host clipboard (text copy/paste bridge; requires network)", "clipboard"),
					huh.NewOption("No network (disables network, pod, Docker, and clipboard)", "no_network"),
					huh.NewOption("No YOLO (disable auto-confirm in AI CLIs)", "no_yolo"),
				).
				Value(&selectedOptions),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Podman pod (optional)").
				Description("Join an existing Podman pod by name (shares its network)").
				Placeholder("e.g. mypod").
				Value(&podName),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Default CPU limit (--cpus)").
				Description("Optional, supports fractions like 1.5").
				Placeholder("e.g. 2").
				Value(&cpuLimit),
			huh.NewInput().
				Title("Default memory limit (--memory)").
				Description("Optional, e.g., 4g or 512m").
				Placeholder("e.g. 4g").
				Value(&memoryLimit),
			huh.NewInput().
				Title("Default shm size (--shm-size)").
				Description("Optional, e.g., 1g for Playwright").
				Placeholder("e.g. 1g").
				Value(&shmLimit),
			huh.NewInput().
				Title("Default GPUs (--gpus)").
				Description("Optional, e.g., all or device=0").
				Placeholder("e.g. all").
				Value(&gpuSetting),
		),
		huh.NewGroup(
			huh.NewText().
				Title("Devices (--device)").
				Description("One per line, e.g., /dev/kvm:/dev/kvm").
				Lines(3).
				Placeholder("/dev/kvm:/dev/kvm").
				Value(&deviceList),
			huh.NewText().
				Title("Added capabilities (--cap-add)").
				Description("One per line, e.g., SYS_PTRACE").
				Lines(3).
				Placeholder("SYS_PTRACE").
				Value(&capAddList),
			huh.NewText().
				Title("Dropped capabilities (--cap-drop)").
				Description("One per line").
				Lines(3).
				Value(&capDropList),
			huh.NewText().
				Title("Raw runtime args (--runtime-arg)").
				Description("One per line, passed directly to Docker/Podman").
				Lines(4).
				Placeholder("--security-opt seccomp=unconfined").
				Value(&runtimeArgList),
		),
	).WithTheme(yoloboxTheme())

	err = form.Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return Config{}, fmt.Errorf("setup cancelled")
		}
		return Config{}, err
	}

	// Build config from form values
	cfg.GitConfig = contains(selectedOptions, "git_config")
	cfg.ClaudeConfig = contains(selectedOptions, "claude_config")
	cfg.CodexConfig = contains(selectedOptions, "codex_config")
	cfg.GeminiConfig = contains(selectedOptions, "gemini_config")
	cfg.OpencodeConfig = contains(selectedOptions, "opencode_config")
	cfg.GhToken = contains(selectedOptions, "gh_token")
	cfg.SSHAgent = contains(selectedOptions, "ssh_agent")
	cfg.Docker = contains(selectedOptions, "docker")
	cfg.Clipboard = contains(selectedOptions, "clipboard")
	cfg.NoNetwork = contains(selectedOptions, "no_network")
	cfg.NoYolo = contains(selectedOptions, "no_yolo")
	cfg.Pod = strings.TrimSpace(podName)
	cfg.CPUs = strings.TrimSpace(cpuLimit)
	cfg.Memory = strings.TrimSpace(memoryLimit)
	cfg.ShmSize = strings.TrimSpace(shmLimit)
	cfg.GPUs = strings.TrimSpace(gpuSetting)
	cfg.Devices = parseMultilineInput(deviceList)
	cfg.CapAdd = parseMultilineInput(capAddList)
	cfg.CapDrop = parseMultilineInput(capDropList)
	cfg.RuntimeArgs = parseMultilineInput(runtimeArgList)

	if err := validateConfigConflicts(cfg); err != nil {
		return cfg, err
	}
	if err := validateRuntimeOptions(cfg); err != nil {
		return cfg, err
	}

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

// splitToolArgs separates yolobox flags from tool flags for shortcuts.
// This allows `yolobox claude --resume` to pass --resume to claude instead of
// failing because --resume is not a known yolobox flag.
func splitToolArgs(args []string) (yoloboxArgs, toolArgs []string) {
	knownFlags := map[string]bool{
		"runtime": true, "image": true, "network": true, "pod": true,
		"ssh-agent": true, "readonly-project": true, "no-network": true,
		"no-yolo": true, "scratch": true, "claude-config": true,
		"codex-config": true, "gemini-config": true, "opencode-config": true, "git-config": true, "gh-token": true,
		"copy-agent-instructions": true, "docker": true, "setup": true, "mount": true,
		"clipboard": true,
		"exclude":   true, "copy-as": true,
		"env": true, "h": true, "help": true,
		"cpus": true, "memory": true, "shm-size": true, "gpus": true,
		"device": true, "cap-add": true, "cap-drop": true, "runtime-arg": true,
		"packages": true, "customize-file": true, "rebuild-image": true,
	}

	flagsWithValues := map[string]bool{
		"runtime": true, "image": true, "network": true, "pod": true,
		"mount": true, "exclude": true, "copy-as": true, "env": true, "cpus": true, "memory": true,
		"shm-size": true, "device": true, "cap-add": true, "cap-drop": true,
		"gpus": true, "runtime-arg": true, "packages": true, "customize-file": true,
	}

	i := 0
	for i < len(args) {
		arg := args[i]

		if arg == "--" {
			// Everything after -- goes to the tool
			return yoloboxArgs, args[i+1:]
		}

		if !strings.HasPrefix(arg, "-") {
			// Non-flag argument - this and rest go to tool
			return yoloboxArgs, args[i:]
		}

		// It's a flag, extract the name
		flagName := strings.TrimLeft(arg, "-")
		hasValue := false
		if idx := strings.Index(flagName, "="); idx != -1 {
			flagName = flagName[:idx]
			hasValue = true
		}

		if !knownFlags[flagName] {
			// Unknown flag - this and rest go to tool
			return yoloboxArgs, args[i:]
		}

		// Known yolobox flag
		yoloboxArgs = append(yoloboxArgs, arg)
		i++

		// If it's a flag that takes a value and doesn't have =, consume next arg
		if flagsWithValues[flagName] && !hasValue && i < len(args) && !strings.HasPrefix(args[i], "-") {
			yoloboxArgs = append(yoloboxArgs, args[i])
			i++
		}
	}

	return yoloboxArgs, nil
}

func buildRunArgs(cfg Config, projectDir string, command []string, interactive bool) ([]string, []string, error) {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, nil, err
	}
	projectMountTarget := absProject
	containerWorkingDir := absProject
	if cfg.Fork.Name != "" {
		absProject = cfg.Fork.Copy
		projectMountTarget = cfg.Fork.Source
		containerWorkingDir = cfg.Fork.Source
	}

	// cleanupPaths collects temp files/dirs created during arg building
	// that should be removed after the container exits
	var cleanupPaths []string

	// Check if we're using Apple container (doesn't support file mounts)
	appleContainer := isAppleContainer(cfg.Runtime)
	if appleContainer && (len(cfg.Exclude) > 0 || len(cfg.CopyAs) > 0) {
		return nil, nil, fmt.Errorf("--exclude and --copy-as are not supported with Apple container runtime")
	}

	// Check if we're using rootless Podman (needs --userns=keep-id for bind mount permissions)
	rootlessPodman := isRootlessPodman(cfg.Runtime)

	args := []string{"run", "--rm"}

	// Rootless Podman: map the host user to container UID 1000 (yolo) so
	// bind-mounted files are accessible. Without this, the host user maps to
	// root inside the user namespace and the yolo user cannot write to mounts.
	if rootlessPodman {
		args = append(args, "--userns=keep-id:uid=1000,gid=1000")
	}

	// Docker/Podman PTYs merge stdout/stderr, so only attach a TTY when the
	// command is actually interactive.
	stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	if shouldAttachTTY(command, interactive, stdinTTY, stdoutTTY) {
		args = append(args, "-it")
	} else if !stdinTTY {
		args = append(args, "-i")
	}

	args = append(args, "-w", containerWorkingDir)
	args = append(args, "-e", "YOLOBOX=1")
	args = append(args, "-e", "YOLOBOX_PROJECT_PATH="+containerWorkingDir)

	// Pass host UID/GID so the entrypoint can match the yolo user to the
	// project directory owner (fixes virtiofs permission issues on Colima 0.10+).
	// Skip for rootless Podman where --userns=keep-id handles UID mapping.
	if !rootlessPodman {
		if info, err := os.Stat(absProject); err == nil {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				args = append(args, "-e", fmt.Sprintf("YOLOBOX_HOST_UID=%d", stat.Uid))
				args = append(args, "-e", fmt.Sprintf("YOLOBOX_HOST_GID=%d", stat.Gid))
			}
		}
	}

	if cfg.NoYolo {
		args = append(args, "-e", "NO_YOLO=1")
	}
	if cfg.Clipboard {
		args = append(args,
			"-e", "YOLOBOX_CLIPBOARD=1",
			"-e", "YOLOBOX_CLIPBOARD_ENDPOINT="+cfg.ClipboardEndpoint,
			"-e", "YOLOBOX_CLIPBOARD_TOKEN="+cfg.ClipboardToken,
		)
		args = append(args, clipboardRuntimeArgs(cfg.Runtime)...)
	}
	if termEnv := os.Getenv("TERM"); termEnv != "" {
		args = append(args, "-e", "TERM="+termEnv)
	}
	if lang := os.Getenv("LANG"); lang != "" {
		args = append(args, "-e", "LANG="+lang)
	}
	if tz := detectTimezone(); tz != "" {
		args = append(args, "-e", "TZ="+tz)
	}

	// Auto-passthrough common API keys
	autoPassthroughEnvKeys := make([]string, 0, len(autoPassthroughEnvVars))
	for _, key := range autoPassthroughEnvVars {
		if val := os.Getenv(key); val != "" {
			autoPassthroughEnvKeys = append(autoPassthroughEnvKeys, key)
			args = append(args, "-e", key+"="+val)
		}
	}

	// Forward GitHub CLI token (extracted from keychain/credential store)
	ghTokenForwarded := false
	if cfg.GhToken {
		if token := getGhToken(); token != "" {
			args = append(args, "-e", "GH_TOKEN="+token)
			ghTokenForwarded = true
		}
	}

	// User-specified env vars
	for _, env := range cfg.Env {
		args = append(args, "-e", env)
	}

	projectMountSource, overlayCleanupPaths, err := buildProjectFilterMounts(cfg, absProject)
	if err != nil {
		return nil, nil, err
	}
	cleanupPaths = append(cleanupPaths, overlayCleanupPaths...)

	// Project mount at its real container path (for session continuity).
	// A symlink /workspace -> real path is created by the entrypoint
	projectMount := projectMountSource + ":" + projectMountTarget
	if cfg.ReadonlyProject {
		projectMount += ":ro"
		// Create a writable output directory
		if cfg.Scratch {
			args = append(args, "-v", "/output") // anonymous volume, deleted with container
		} else {
			args = append(args, "-v", persistentVolumeMount("yolobox-output", "/output", rootlessPodman))
		}
	}
	args = append(args, "-v", projectMount)

	// Named volumes for persistence (skip if --scratch).
	// Rootless Podman on SELinux-enabled hosts assigns per-container MCS
	// labels; without :Z, files created in one run are inaccessible to the
	// next. :U also repairs older keep-id volumes that were created with
	// subordinate-ID ownership and now appear as uid/gid 999 in-container.
	if !cfg.Scratch {
		args = append(args, "-v", persistentVolumeMount("yolobox-home", "/home/yolo", rootlessPodman))
		args = append(args, "-v", persistentVolumeMount("yolobox-cache", "/var/cache", rootlessPodman))
	}

	// For Apple container, we need to collect files and mount via a temp directory
	// (Apple container only supports directory mounts, not file mounts)
	var appleContainerFiles map[string]string
	if appleContainer {
		appleContainerFiles = make(map[string]string)
	}

	// Mount Claude config from host to staging area (copied to /home/yolo by entrypoint)
	if cfg.ClaudeConfig {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		claudeConfigDir := filepath.Join(home, ".claude")
		if _, err := os.Stat(claudeConfigDir); err == nil {
			mountSrc := claudeConfigDir
			if dirContainsSymlinks(claudeConfigDir) {
				staged, err := stageDirResolvingSymlinks(claudeConfigDir)
				if err != nil {
					warn("Failed to resolve symlinks in %s: %s", claudeConfigDir, err)
				} else {
					mountSrc = staged
					cleanupPaths = append(cleanupPaths, staged)
				}
			}
			args = append(args, "-v", mountSrc+":/host-claude/.claude:ro")
		}
		claudeConfigFile := filepath.Join(home, ".claude.json")
		if _, err := os.Stat(claudeConfigFile); err == nil {
			// Preprocess to remove installMethod (host install method doesn't apply in container)
			if processedPath := preprocessClaudeConfig(claudeConfigFile); processedPath != "" {
				cleanupPaths = append(cleanupPaths, processedPath)
				if appleContainer {
					appleContainerFiles[processedPath] = "claude/.claude.json"
				} else {
					args = append(args, "-v", processedPath+":/host-claude/.claude.json:ro")
				}
			}
		}
		// On macOS, extract OAuth credentials from Keychain and mount as .credentials.json
		// Write to unique temp file in ~/.yolobox/tmp/ (unique per invocation to
		// avoid conflicts when multiple yolobox instances run concurrently)
		if creds := getClaudeCredentials(); creds != "" {
			tmpDir := filepath.Join(home, ".yolobox", "tmp")
			if err := os.MkdirAll(tmpDir, 0700); err == nil {
				f, err := os.CreateTemp(tmpDir, "claude-credentials-*.json")
				if err == nil {
					if _, writeErr := f.Write([]byte(creds)); writeErr == nil {
						if closeErr := f.Close(); closeErr == nil {
							if chmodErr := os.Chmod(f.Name(), 0600); chmodErr == nil {
								credsPath := f.Name()
								cleanupPaths = append(cleanupPaths, credsPath)
								if appleContainer {
									appleContainerFiles[credsPath] = "claude/.credentials.json"
								} else {
									args = append(args, "-v", credsPath+":/host-claude/.credentials.json:ro")
								}
							} else {
								_ = os.Remove(f.Name())
							}
						} else {
							_ = os.Remove(f.Name())
						}
					} else {
						_ = f.Close()
						_ = os.Remove(f.Name())
					}
				}
			}
		}
	}

	// Mount Gemini config from host to staging area (copied to /home/yolo by entrypoint)
	if cfg.GeminiConfig {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		geminiConfigDir := filepath.Join(home, ".gemini")
		if _, err := os.Stat(geminiConfigDir); err == nil {
			mountSrc := geminiConfigDir
			if dirContainsSymlinks(geminiConfigDir) {
				staged, err := stageDirResolvingSymlinks(geminiConfigDir)
				if err != nil {
					warn("Failed to resolve symlinks in %s: %s", geminiConfigDir, err)
				} else {
					mountSrc = staged
					cleanupPaths = append(cleanupPaths, staged)
				}
			}
			args = append(args, "-v", mountSrc+":/host-gemini/.gemini:ro")
		}
	}

	// Mount OpenCode config from host to staging area (copied to /home/yolo by entrypoint)
	if cfg.OpencodeConfig {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		opencodeConfigDir := filepath.Join(home, ".config", "opencode")
		if _, err := os.Stat(opencodeConfigDir); err == nil {
			mountSrc := opencodeConfigDir
			if dirContainsSymlinks(opencodeConfigDir) {
				staged, err := stageDirResolvingSymlinks(opencodeConfigDir)
				if err != nil {
					warn("Failed to resolve symlinks in %s: %s", opencodeConfigDir, err)
				} else {
					mountSrc = staged
					cleanupPaths = append(cleanupPaths, staged)
				}
			}
			args = append(args, "-v", mountSrc+":/host-opencode/.config/opencode:ro")
		}
	}

	// Mount Codex config from host to staging area (copied to /home/yolo by entrypoint)
	if cfg.CodexConfig {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		codexConfigDir := filepath.Join(home, ".codex")
		if _, err := os.Stat(codexConfigDir); err == nil {
			mountSrc := codexConfigDir
			if dirContainsSymlinks(codexConfigDir) {
				staged, err := stageDirResolvingSymlinks(codexConfigDir)
				if err != nil {
					warn("Failed to resolve symlinks in %s: %s", codexConfigDir, err)
				} else {
					mountSrc = staged
					cleanupPaths = append(cleanupPaths, staged)
				}
			}
			args = append(args, "-v", mountSrc+":/host-codex/.codex:ro")
		}
	}

	// Mount git config from host to staging area (copied to /home/yolo by entrypoint)
	if cfg.GitConfig {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		gitConfigFile := filepath.Join(home, ".gitconfig")
		if _, err := os.Stat(gitConfigFile); err == nil {
			if appleContainer {
				appleContainerFiles[gitConfigFile] = "git/.gitconfig"
			} else {
				args = append(args, "-v", gitConfigFile+":/host-git/.gitconfig:ro")
			}
		}
	}

	// Mount global agent instruction files from host to staging area (copied by entrypoint)
	// These are the global/user-level instruction files, not project-level ones
	if cfg.CopyAgentInstructions {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		// Claude: ~/.claude/CLAUDE.md
		claudeMd := filepath.Join(home, ".claude", "CLAUDE.md")
		if _, err := os.Stat(claudeMd); err == nil {
			if appleContainer {
				appleContainerFiles[claudeMd] = "agent-instructions/claude/CLAUDE.md"
			} else {
				args = append(args, "-v", claudeMd+":/host-agent-instructions/claude/CLAUDE.md:ro")
			}
		}
		// Claude: ~/.claude/skills/ directory
		claudeSkills := filepath.Join(home, ".claude", "skills")
		if info, err := os.Stat(claudeSkills); err == nil && info.IsDir() {
			mountSrc := claudeSkills
			if dirContainsSymlinks(claudeSkills) {
				staged, err := stageDirResolvingSymlinks(claudeSkills)
				if err != nil {
					warn("Failed to resolve symlinks in %s: %s", claudeSkills, err)
				} else {
					mountSrc = staged
					cleanupPaths = append(cleanupPaths, staged)
				}
			}
			args = append(args, "-v", mountSrc+":/host-agent-instructions/claude/skills:ro")
		}
		// Gemini: ~/.gemini/GEMINI.md
		geminiMd := filepath.Join(home, ".gemini", "GEMINI.md")
		if _, err := os.Stat(geminiMd); err == nil {
			if appleContainer {
				appleContainerFiles[geminiMd] = "agent-instructions/gemini/GEMINI.md"
			} else {
				args = append(args, "-v", geminiMd+":/host-agent-instructions/gemini/GEMINI.md:ro")
			}
		}
		// Codex: ~/.codex/AGENTS.md
		codexMd := filepath.Join(home, ".codex", "AGENTS.md")
		if _, err := os.Stat(codexMd); err == nil {
			if appleContainer {
				appleContainerFiles[codexMd] = "agent-instructions/codex/AGENTS.md"
			} else {
				args = append(args, "-v", codexMd+":/host-agent-instructions/codex/AGENTS.md:ro")
			}
		}
		// Codex: ~/.codex/skills/ directory
		codexSkills := filepath.Join(home, ".codex", "skills")
		if info, err := os.Stat(codexSkills); err == nil && info.IsDir() {
			mountSrc := codexSkills
			if dirContainsSymlinks(codexSkills) {
				staged, err := stageDirResolvingSymlinks(codexSkills)
				if err != nil {
					warn("Failed to resolve symlinks in %s: %s", codexSkills, err)
				} else {
					mountSrc = staged
					cleanupPaths = append(cleanupPaths, staged)
				}
			}
			args = append(args, "-v", mountSrc+":/host-agent-instructions/codex/skills:ro")
		}
		// Copilot: ~/.copilot/agents/ directory (this is already a directory, works with Apple container)
		copilotAgents := filepath.Join(home, ".copilot", "agents")
		if info, err := os.Stat(copilotAgents); err == nil && info.IsDir() {
			mountSrc := copilotAgents
			if dirContainsSymlinks(copilotAgents) {
				staged, err := stageDirResolvingSymlinks(copilotAgents)
				if err != nil {
					warn("Failed to resolve symlinks in %s: %s", copilotAgents, err)
				} else {
					mountSrc = staged
					cleanupPaths = append(cleanupPaths, staged)
				}
			}
			args = append(args, "-v", mountSrc+":/host-agent-instructions/copilot/agents:ro")
		}
	}

	// For Apple container: create temp dir with collected files and mount it
	if appleContainer && len(appleContainerFiles) > 0 {
		tmpDir, err := prepareFileMountDir(appleContainerFiles)
		if err != nil {
			return nil, nil, err
		}
		cleanupPaths = append(cleanupPaths, tmpDir)
		// Mount the temp dir; entrypoint will need to handle the different paths
		args = append(args, "-v", tmpDir+":/host-files:ro")
		args = append(args, "-e", "YOLOBOX_HOST_FILES=/host-files")
	}

	// Extra mounts
	for _, mount := range cfg.Mounts {
		resolved, err := resolveMount(mount, absProject)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, "-v", resolved)
	}

	// Resource & security controls
	args = appendRunFlag(args, "cpus", cfg.CPUs)
	args = appendRunFlag(args, "memory", cfg.Memory)
	args = appendRunFlag(args, "shm-size", cfg.ShmSize)
	args = appendRunFlag(args, "gpus", cfg.GPUs)
	for _, d := range cfg.Devices {
		args = append(args, "--device", d)
	}
	for _, c := range cfg.CapAdd {
		args = append(args, "--cap-add", c)
	}
	for _, c := range cfg.CapDrop {
		args = append(args, "--cap-drop", c)
	}

	// SSH agent forwarding
	if cfg.SSHAgent {
		if appleContainer {
			// Apple container uses --ssh flag instead of socket mounts
			args = append(args, "--ssh")
		} else {
			sock, err := findSSHAgentSocket()
			if err != nil {
				warn("%s", err)
			} else {
				args = append(args, "-v", sock+":/ssh-agent")
				args = append(args, "-e", "SSH_AUTH_SOCK=/ssh-agent")
			}
		}
	}

	// Docker socket forwarding
	if cfg.Docker {
		sock, err := findDockerSocket()
		if err != nil {
			return nil, nil, err
		}
		args = append(args, "-v", sock+":/var/run/docker.sock")
		// Default to yolobox-net if no explicit network is set
		if cfg.Network == "" {
			cfg.Network = "yolobox-net"
		}
		args = append(args, "-e", "YOLOBOX_NETWORK="+cfg.Network)
	}

	// Network configuration
	if cfg.Pod != "" {
		args = append(args, "--pod", cfg.Pod)
	} else {
		if cfg.NoNetwork {
			args = append(args, "--network", "none")
		} else if cfg.Network != "" {
			args = append(args, "--network", cfg.Network)
		}
	}

	contextPayload, err := encodeContextManifest(cfg, containerWorkingDir, command, interactive, autoPassthroughEnvKeys, ghTokenForwarded)
	if err != nil {
		return nil, nil, err
	}
	args = append(args, "-e", yoloboxContextPayloadEnv+"="+contextPayload)
	args = append(args, "-e", "YOLOBOX_CONTEXT_FILE="+yoloboxContextFile)

	if len(cfg.RuntimeArgs) > 0 {
		args = append(args, cfg.RuntimeArgs...)
	}

	args = append(args, cfg.Image)
	args = append(args, command...)
	return args, cleanupPaths, nil
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

func parseCommaSeparatedValues(input string) []string {
	parts := strings.Split(input, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}
	return values
}

func resolveRuntime(name string) (string, error) {
	if name == "" {
		if path, err := exec.LookPath("docker"); err == nil {
			return path, nil
		}
		if path, err := exec.LookPath("podman"); err == nil {
			return path, nil
		}
		if path, err := exec.LookPath("container"); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("no container runtime found. Install docker, podman, or Apple container and try again")
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

// getGhToken extracts the GitHub CLI token from the host's credential store
// Returns empty string if gh is not installed or not logged in
func getGhToken() string {
	cmd := exec.Command("gh", "auth", "token")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// getClaudeCredentials extracts Claude Code OAuth credentials from macOS Keychain
// Returns empty string on non-macOS or if not logged in
func getClaudeCredentials() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	cmd := exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// preprocessClaudeConfig reads ~/.claude.json, removes installMethod (which is
// host-specific and causes issues in the container), and writes to a temp file.
// Returns the temp file path, or empty string on error.
func preprocessClaudeConfig(srcPath string) string {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return ""
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}

	// Remove installMethod - let Claude detect it fresh in the container
	// The host's installMethod (e.g., "native") doesn't apply inside the container
	delete(config, "installMethod")

	processed, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return ""
	}

	// Write to unique temp file in ~/.yolobox/tmp/ (unique per invocation to
	// avoid conflicts when multiple yolobox instances run concurrently)
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	tmpDir := filepath.Join(home, ".yolobox", "tmp")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return ""
	}
	f, err := os.CreateTemp(tmpDir, "claude-config-*.json")
	if err != nil {
		return ""
	}

	if _, err := f.Write(processed); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return ""
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return ""
	}

	return f.Name()
}

// detectTimezone returns the host's IANA timezone (e.g., "America/New_York").
// Returns empty string if detection fails.
func detectTimezone() string {
	// Check TZ env var first
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}

	// Try reading /etc/localtime symlink (works on macOS and Linux)
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}

	// Extract timezone from path like /var/db/timezone/zoneinfo/America/New_York
	// or /usr/share/zoneinfo/America/New_York
	const marker = "zoneinfo/"
	if idx := strings.LastIndex(target, marker); idx != -1 {
		return target[idx+len(marker):]
	}

	return ""
}

// checkDockerMemory warns if Docker has less than 4GB RAM available
func checkDockerMemory(runtime string) {
	runtimePath, err := resolveRuntime(runtime)
	if err != nil {
		return
	}

	// Skip memory check for Apple container (uses native VM with dynamic memory)
	if strings.HasSuffix(runtimePath, "/container") {
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
