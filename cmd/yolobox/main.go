package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	Version = "0.1.0"
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
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"OPENROUTER_API_KEY",
	"GEMINI_API_KEY",
}

type Config struct {
	Runtime         string   `toml:"runtime"`
	Image           string   `toml:"image"`
	Mounts          []string `toml:"mounts"`
	Secrets         []string `toml:"secrets"`
	Env             []string `toml:"env"`
	SSHAgent        bool     `toml:"ssh_agent"`
	UnsafeHost      bool     `toml:"unsafe_host"`
	ReadonlyProject bool     `toml:"readonly_project"`
	NoNetwork       bool     `toml:"no_network"`
	Memory          string   `toml:"memory"`
	CPUs            string   `toml:"cpus"`
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
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		cfg, rest, err := parseBaseFlags("yolobox", args)
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
		cfg, rest, err := parseBaseFlags("run", args[1:])
		if err != nil {
			return err
		}
		if len(rest) == 0 {
			return fmt.Errorf("run requires a command")
		}
		return runCommand(cfg, rest, false)
	case "update":
		cfg, rest, err := parseBaseFlags("update", args[1:])
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return fmt.Errorf("unexpected args: %v", rest)
		}
		return updateImage(cfg)
	case "config":
		cfg, rest, err := parseBaseFlags("config", args[1:])
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return fmt.Errorf("unexpected args: %v", rest)
		}
		return printConfig(cfg)
	case "reset":
		return resetVolumes(args[1:])
	case "version":
		printVersion()
		return nil
	case "help":
		printUsage()
		return errHelp
	default:
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
	fmt.Fprintln(os.Stderr, "  yolobox update              Pull the latest base image")
	fmt.Fprintln(os.Stderr, "  yolobox config              Print resolved configuration")
	fmt.Fprintln(os.Stderr, "  yolobox reset --force       Remove named volumes (fresh start)")
	fmt.Fprintln(os.Stderr, "  yolobox version             Show version info")
	fmt.Fprintln(os.Stderr, "  yolobox help                Show this help")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sFLAGS:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  --runtime <name>      Container runtime: docker or podman")
	fmt.Fprintln(os.Stderr, "  --image <name>        Base image to use")
	fmt.Fprintln(os.Stderr, "  --mount <src:dst>     Extra mount (repeatable)")
	fmt.Fprintln(os.Stderr, "  --secret <path>       Mount secret dir into /secrets (repeatable)")
	fmt.Fprintln(os.Stderr, "  --env <KEY=val>       Set environment variable (repeatable)")
	fmt.Fprintln(os.Stderr, "  --ssh-agent           Forward SSH agent socket")
	fmt.Fprintln(os.Stderr, "  --memory <limit>      Memory limit (e.g., 4g, 512m)")
	fmt.Fprintln(os.Stderr, "  --cpus <limit>        CPU limit (e.g., 2, 0.5)")
	fmt.Fprintln(os.Stderr, "  --no-network          Disable network access")
	fmt.Fprintln(os.Stderr, "  --readonly-project    Mount project directory read-only")
	fmt.Fprintln(os.Stderr, "  --unsafe-host         Mount host home to /host-home (danger!)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sCONFIG:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  Global:  ~/.config/yolobox/config.toml")
	fmt.Fprintln(os.Stderr, "  Project: .yolobox.toml")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sAUTO-FORWARDED ENV VARS:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  ANTHROPIC_API_KEY, OPENAI_API_KEY, GITHUB_TOKEN, GH_TOKEN,")
	fmt.Fprintln(os.Stderr, "  OPENROUTER_API_KEY, GEMINI_API_KEY")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%sEXAMPLES:%s\n", colorBold, colorReset)
	fmt.Fprintln(os.Stderr, "  yolobox                     # Drop into a shell")
	fmt.Fprintln(os.Stderr, "  yolobox run make build      # Run make inside sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox run claude          # Run Claude Code in sandbox")
	fmt.Fprintln(os.Stderr, "  yolobox --no-network        # Paranoid mode: no internet")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "  %sLet your AI go full send. Your home directory stays home.%s\n\n", colorPurple, colorReset)
}

func parseBaseFlags(name string, args []string) (Config, []string, error) {
	projectDir, err := os.Getwd()
	if err != nil {
		return Config{}, nil, err
	}
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
		memoryFlag      string
		cpusFlag        string
		sshAgent        bool
		unsafeHost      bool
		readonlyProject bool
		noNetwork       bool
		mounts          stringSliceFlag
		secrets         stringSliceFlag
		envVars         stringSliceFlag
	)

	fs.StringVar(&runtimeFlag, "runtime", "", "container runtime")
	fs.StringVar(&imageFlag, "image", "", "container image")
	fs.StringVar(&memoryFlag, "memory", "", "memory limit")
	fs.StringVar(&cpusFlag, "cpus", "", "cpu limit")
	fs.BoolVar(&sshAgent, "ssh-agent", false, "mount SSH agent socket")
	fs.BoolVar(&unsafeHost, "unsafe-host", false, "mount host home to /host-home")
	fs.BoolVar(&readonlyProject, "readonly-project", false, "mount project read-only")
	fs.BoolVar(&noNetwork, "no-network", false, "disable network")
	fs.Var(&mounts, "mount", "extra mount src:dst")
	fs.Var(&secrets, "secret", "secret directory")
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
	if memoryFlag != "" {
		cfg.Memory = memoryFlag
	}
	if cpusFlag != "" {
		cfg.CPUs = cpusFlag
	}
	if sshAgent {
		cfg.SSHAgent = true
	}
	if unsafeHost {
		cfg.UnsafeHost = true
	}
	if readonlyProject {
		cfg.ReadonlyProject = true
	}
	if noNetwork {
		cfg.NoNetwork = true
	}
	if len(mounts) > 0 {
		cfg.Mounts = append(cfg.Mounts, mounts...)
	}
	if len(secrets) > 0 {
		cfg.Secrets = append(cfg.Secrets, secrets...)
	}
	if len(envVars) > 0 {
		cfg.Env = append(cfg.Env, envVars...)
	}

	return cfg, fs.Args(), nil
}

func defaultConfig() Config {
	return Config{
		Image: "yolobox/base:latest",
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
	if src.Memory != "" {
		dst.Memory = src.Memory
	}
	if src.CPUs != "" {
		dst.CPUs = src.CPUs
	}
	if len(src.Mounts) > 0 {
		dst.Mounts = append([]string{}, src.Mounts...)
	}
	if len(src.Secrets) > 0 {
		dst.Secrets = append([]string{}, src.Secrets...)
	}
	if len(src.Env) > 0 {
		dst.Env = append([]string{}, src.Env...)
	}
	if src.SSHAgent {
		dst.SSHAgent = true
	}
	if src.UnsafeHost {
		dst.UnsafeHost = true
	}
	if src.ReadonlyProject {
		dst.ReadonlyProject = true
	}
	if src.NoNetwork {
		dst.NoNetwork = true
	}
}

func runShell(cfg Config) error {
	success("Spinning up your sandbox...")
	return runCommand(cfg, []string{"bash"}, true)
}

func runCommand(cfg Config, command []string, interactive bool) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}
	args, err := buildRunArgs(cfg, projectDir, command, interactive)
	if err != nil {
		return err
	}
	return execRuntime(cfg.Runtime, args)
}

func updateImage(cfg Config) error {
	info("Pulling latest image: %s", cfg.Image)
	runtime, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}
	if err := execCommand(runtime, []string{"pull", cfg.Image}); err != nil {
		return err
	}
	success("Image updated!")
	return nil
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
	fmt.Printf("%sunsafe_host:%s %t\n", colorBold, colorReset, cfg.UnsafeHost)
	fmt.Printf("%sreadonly_project:%s %t\n", colorBold, colorReset, cfg.ReadonlyProject)
	fmt.Printf("%sno_network:%s %t\n", colorBold, colorReset, cfg.NoNetwork)
	if cfg.Memory != "" {
		fmt.Printf("%smemory:%s %s\n", colorBold, colorReset, cfg.Memory)
	}
	if cfg.CPUs != "" {
		fmt.Printf("%scpus:%s %s\n", colorBold, colorReset, cfg.CPUs)
	}
	if len(cfg.Mounts) > 0 {
		fmt.Printf("%smounts:%s\n", colorBold, colorReset)
		for _, m := range cfg.Mounts {
			fmt.Printf("  - %s\n", m)
		}
	}
	if len(cfg.Secrets) > 0 {
		fmt.Printf("%ssecrets:%s\n", colorBold, colorReset)
		for _, s := range cfg.Secrets {
			fmt.Printf("  - %s\n", s)
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
	volumes := []string{"yolobox-home", "yolobox-cache", "yolobox-tools"}
	args = append([]string{"volume", "rm"}, volumes...)
	if err := execCommand(runtime, args); err != nil {
		return err
	}
	success("Fresh start! All volumes removed.")
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
	if interactive {
		args = append(args, "-it")
	}

	args = append(args, "-w", "/workspace")
	args = append(args, "-e", "YOLOBOX=1")
	if term := os.Getenv("TERM"); term != "" {
		args = append(args, "-e", "TERM="+term)
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
		args = append(args, "-v", "yolobox-output:/output")
	}
	args = append(args, "-v", projectMount)

	// Named volumes for persistence
	args = append(args, "-v", "yolobox-home:/home/yolo")
	args = append(args, "-v", "yolobox-cache:/var/cache")
	args = append(args, "-v", "yolobox-tools:/opt/tools")

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

	// Secrets
	for _, secret := range cfg.Secrets {
		resolved, err := resolveSecretPath(secret, absProject)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(resolved)
		args = append(args, "-v", resolved+":/secrets/"+name+":ro")
	}

	// Unsafe host home mount
	if cfg.UnsafeHost {
		warn("--unsafe-host enabled: your home directory is accessible at /host-home")
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		args = append(args, "-v", home+":/host-home")
	}

	// Resource limits
	if cfg.Memory != "" {
		args = append(args, "--memory", cfg.Memory)
	}
	if cfg.CPUs != "" {
		args = append(args, "--cpus", cfg.CPUs)
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

func resolveSecretPath(path string, projectDir string) (string, error) {
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
	if !filepath.IsAbs(path) {
		path = filepath.Join(projectDir, path)
	}
	return filepath.Clean(path), nil
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

func execCommand(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
