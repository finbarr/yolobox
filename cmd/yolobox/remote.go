package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	remoteDefaultSessionName = "yolobox"
	remoteProjectRoot        = "/opt/yolobox/project"
)

type remoteMachine struct {
	Name              string    `json:"name"`
	Provider          string    `json:"provider,omitempty"`
	ProviderID        string    `json:"provider_id,omitempty"`
	PublicIPv4        string    `json:"public_ipv4,omitempty"`
	Region            string    `json:"region,omitempty"`
	Size              string    `json:"size,omitempty"`
	Image             string    `json:"image,omitempty"`
	SSHUser           string    `json:"ssh_user,omitempty"`
	SourcePath        string    `json:"source_path,omitempty"`
	ProjectPath       string    `json:"project_path,omitempty"`
	RepoURL           string    `json:"repo_url,omitempty"`
	Branch            string    `json:"branch,omitempty"`
	LastCommand       []string  `json:"last_command,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	LastSyncedAt      time.Time `json:"last_synced_at,omitempty"`
	BootstrapComplete bool      `json:"bootstrap_complete,omitempty"`
}

type remoteProvisionOptions struct {
	Name       string
	SSHUser    string
	BackendURL string
}

func runRemote(args []string, projectDir string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printRemoteUsage()
		return errHelp
	}

	switch args[0] {
	case "attach", "resume":
		return runRemoteResume(args[1:], projectDir)
	case "sync":
		return runRemoteSync(args[1:], projectDir)
	case "stop":
		return runRemoteStop(args[1:], projectDir)
	case "forward":
		return runRemoteForward(args[1:], projectDir)
	case "list":
		return runRemoteList(args[1:], projectDir)
	case "status":
		return runRemoteStatus(args[1:], projectDir)
	case "destroy":
		return runRemoteDestroy(args[1:], projectDir)
	default:
		return runRemoteCreate(args, projectDir)
	}
}

func printRemoteUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox remote --name <env> [cmd...]       Create or reuse a named remote machine")
	fmt.Fprintln(os.Stderr, "  yolobox remote resume [<env>] [cmd...]     Reattach to the remote machine session")
	fmt.Fprintln(os.Stderr, "  yolobox remote attach [<env>] [cmd...]     Alias for resume")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync [up] [<env>]           Copy the current folder to the remote machine")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync down [<env>] --force   Copy the remote project back locally")
	fmt.Fprintln(os.Stderr, "  yolobox remote forward [<env>] <port>      Forward a remote preview port to localhost")
	fmt.Fprintln(os.Stderr, "  yolobox remote stop [<env>]                Stop the remote tmux session")
	fmt.Fprintln(os.Stderr, "  yolobox remote list                        List backend machines")
	fmt.Fprintln(os.Stderr, "  yolobox remote status [<env>]              Show backend machine state")
	fmt.Fprintln(os.Stderr, "  yolobox remote destroy <env> --force       Release/delete remote machine")
	fmt.Fprintln(os.Stderr, "  Omit <env> only when remote_name is configured.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "OPTIONS:")
	fmt.Fprintln(os.Stderr, "  --name <env>         Remote machine name")
	fmt.Fprintln(os.Stderr, "  --ssh-user <user>    SSH user for the remote host")
	fmt.Fprintln(os.Stderr, "  --backend-url <url>  Remote backend API URL")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "EXAMPLES:")
	fmt.Fprintln(os.Stderr, "  yolobox login --email you@example.com")
	fmt.Fprintln(os.Stderr, "  yolobox remote --name foo codex")
	fmt.Fprintln(os.Stderr, "  yolobox remote resume foo codex")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync up foo")
	fmt.Fprintln(os.Stderr, "  yolobox remote forward foo 3000")
}

func runRemoteDefault(cfg Config, projectDir string) error {
	if err := validateRemoteDefaults(cfg); err != nil {
		return err
	}
	name := strings.TrimSpace(cfg.RemoteName)
	machine, err := ensureRemoteMachine(cfg, projectDir, remoteProvisionOptions{Name: name})
	if err != nil {
		return err
	}
	return attachRemoteMachine(cfg, machine, remoteDefaultCommand(cfg))
}

func runRemoteCreate(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}

	opts, commandArgs, err := parseRemoteCreateFlags(args, cfg)
	if err != nil {
		return err
	}
	if opts.Name == "" {
		return fmt.Errorf("yolobox remote requires --name")
	}
	if err := validateRemoteName(opts.Name); err != nil {
		return err
	}
	cfg, err = remoteConfigForProvision(cfg, opts)
	if err != nil {
		return err
	}
	if len(commandArgs) == 0 {
		commandArgs = remoteDefaultCommand(cfg)
	}

	machine, err := ensureRemoteMachine(cfg, projectDir, opts)
	if err != nil {
		return err
	}
	return attachRemoteMachine(cfg, machine, commandArgs)
}

func parseRemoteCreateFlags(args []string, cfg Config) (remoteProvisionOptions, []string, error) {
	fs := flag.NewFlagSet("remote", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printRemoteUsage

	opts := remoteProvisionOptions{
		Name:       strings.TrimSpace(cfg.RemoteName),
		SSHUser:    cfg.Remote.SSHUser,
		BackendURL: remoteBackendURL(cfg),
	}
	fs.StringVar(&opts.Name, "name", opts.Name, "remote machine name")
	fs.StringVar(&opts.SSHUser, "ssh-user", opts.SSHUser, "remote SSH user")
	fs.StringVar(&opts.BackendURL, "backend-url", opts.BackendURL, "remote backend API URL")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printRemoteUsage()
			return opts, nil, errHelp
		}
		return opts, nil, err
	}

	opts.Name = strings.ToLower(strings.TrimSpace(opts.Name))
	opts.SSHUser = strings.TrimSpace(opts.SSHUser)
	opts.BackendURL = strings.TrimRight(strings.TrimSpace(opts.BackendURL), "/")
	return opts, fs.Args(), nil
}

func remoteConfigForProvision(cfg Config, opts remoteProvisionOptions) (Config, error) {
	if opts.BackendURL != "" {
		if err := validateRemoteBackendURL(opts.BackendURL); err != nil {
			return cfg, fmt.Errorf("invalid --backend-url: %w", err)
		}
		cfg.Remote.BackendURL = opts.BackendURL
	}
	if opts.SSHUser != "" {
		cfg.Remote.SSHUser = opts.SSHUser
	}
	return cfg, nil
}

func runRemoteResume(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	name, commandArgs, err := parseRemoteNameAndCommand("resume", args, cfg)
	if err != nil {
		return err
	}
	if len(commandArgs) == 0 {
		commandArgs = remoteDefaultCommand(cfg)
	}
	machine, _, err := getRemoteBackendMachine(cfg, name)
	if err != nil {
		return err
	}
	return attachRemoteMachine(cfg, machine, commandArgs)
}

func runRemoteSync(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}

	direction := "up"
	if len(args) > 0 && (args[0] == "up" || args[0] == "down") {
		direction = args[0]
		args = args[1:]
	}
	force := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--force" {
			force = true
			continue
		}
		filtered = append(filtered, arg)
	}

	name, rest, err := parseRemoteNameAndCommand("sync", filtered, cfg)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected remote sync args: %v", rest)
	}
	if direction == "down" && !force {
		return fmt.Errorf("remote sync down overwrites the local folder; pass --force to continue")
	}
	machine, _, err := getRemoteBackendMachine(cfg, name)
	if err != nil {
		return err
	}

	switch direction {
	case "up":
		if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
			return err
		}
		if err := updateRemoteBackendMachine(cfg, machine); err != nil {
			return err
		}
	case "down":
		if err := syncRemoteProjectDown(machine, projectDir); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown remote sync direction %q", direction)
	}

	success("Synced remote %s %s", machine.Name, direction)
	return nil
}

func runRemoteList(args []string, projectDir string) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected remote list args: %v", args)
	}
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	machines, err := listRemoteBackendMachines(cfg)
	if err != nil {
		return err
	}
	if len(machines) == 0 {
		if _, err := fmt.Fprintln(os.Stdout, "No remote machines."); err != nil {
			return err
		}
		return nil
	}
	sort.Slice(machines, func(i, j int) bool {
		return machines[i].Name < machines[j].Name
	})
	if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %s\n", "NAME", "PROVIDER", "IP", "REGION", "PROJECT"); err != nil {
		return err
	}
	for _, m := range machines {
		if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %s\n", m.Name, configValueOrNotSet(m.Provider), m.PublicIPv4, m.Region, m.ProjectPath); err != nil {
			return err
		}
	}
	return nil
}

func runRemoteStatus(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	name, rest, err := parseRemoteNameAndCommand("status", args, cfg)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected remote status args: %v", rest)
	}
	machine, status, err := getRemoteBackendMachine(cfg, name)
	if err != nil {
		return err
	}

	fmt.Printf("%sname:%s %s\n", colorBold, colorReset, machine.Name)
	fmt.Printf("%sbackend_url:%s %s\n", colorBold, colorReset, remoteBackendURL(cfg))
	fmt.Printf("%sbackend_status:%s %s\n", colorBold, colorReset, configValueOrNotSet(status))
	fmt.Printf("%sprovider:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.Provider))
	fmt.Printf("%sprovider_id:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.ProviderID))
	fmt.Printf("%spublic_ipv4:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.PublicIPv4))
	fmt.Printf("%sssh_user:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.SSHUser))
	fmt.Printf("%ssource_path:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.SourcePath))
	fmt.Printf("%srepo:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.RepoURL))
	fmt.Printf("%sbranch:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.Branch))
	fmt.Printf("%sproject_path:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.ProjectPath))
	fmt.Printf("%slast_synced_at:%s %s\n", colorBold, colorReset, displayTime(machine.LastSyncedAt))
	fmt.Printf("%sbootstrap_complete:%s %t\n", colorBold, colorReset, machine.BootstrapComplete)
	return nil
}

func runRemoteDestroy(args []string, projectDir string) error {
	if len(args) == 0 {
		return fmt.Errorf("yolobox remote destroy requires a remote name")
	}
	name := ""
	force := false
	for _, arg := range args {
		switch arg {
		case "--force":
			force = true
		default:
			if name != "" {
				return fmt.Errorf("unexpected remote destroy argument: %s", arg)
			}
			name = strings.ToLower(strings.TrimSpace(arg))
		}
	}
	if name == "" {
		return fmt.Errorf("yolobox remote destroy requires a remote name")
	}
	if err := validateRemoteName(name); err != nil {
		return err
	}
	if !force {
		return fmt.Errorf("yolobox remote destroy requires --force")
	}
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	if err := releaseRemoteBackendMachine(cfg, name); err != nil {
		return err
	}
	success("Destroyed remote %s", name)
	return nil
}

func runRemoteStop(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	name, rest, err := parseRemoteNameAndCommand("stop", args, cfg)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected remote stop args: %v", rest)
	}
	machine, _, err := getRemoteBackendMachine(cfg, name)
	if err != nil {
		return err
	}
	script := "tmux has-session -t " + shellQuote(remoteDefaultSessionName) + " 2>/dev/null && tmux kill-session -t " + shellQuote(remoteDefaultSessionName) + " || true"
	if err := runSSHCommand(machine, script, false, false); err != nil {
		return err
	}
	success("Stopped remote session %s", machine.Name)
	return nil
}

func runRemoteForward(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	name, localPort, remotePort, err := parseRemoteForwardArgs(args, cfg)
	if err != nil {
		return err
	}
	machine, _, err := getRemoteBackendMachine(cfg, name)
	if err != nil {
		return err
	}
	info("Forwarding http://127.0.0.1:%d to %s port %d. Press Ctrl+C to stop.", localPort, machine.Name, remotePort)
	return runSSHForward(machine, localPort, remotePort)
}

func parseRemoteNameAndCommand(command string, args []string, cfg Config) (string, []string, error) {
	if len(args) == 0 {
		name := strings.TrimSpace(cfg.RemoteName)
		if name == "" {
			return "", nil, fmt.Errorf("yolobox remote %s requires a remote name", command)
		}
		name = strings.ToLower(name)
		return name, nil, validateRemoteName(name)
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if err := validateRemoteName(name); err != nil {
		return "", nil, err
	}
	return name, args[1:], nil
}

func parseRemoteForwardArgs(args []string, cfg Config) (string, int, int, error) {
	localPort := 0
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--local-port":
			if i+1 >= len(args) {
				return "", 0, 0, fmt.Errorf("--local-port requires a value")
			}
			port, err := parsePort(args[i+1])
			if err != nil {
				return "", 0, 0, fmt.Errorf("invalid local port: %w", err)
			}
			localPort = port
			i++
		default:
			positionals = append(positionals, args[i])
		}
	}

	if len(positionals) == 1 && strings.TrimSpace(cfg.RemoteName) != "" {
		if remotePort, err := parsePort(positionals[0]); err == nil {
			if localPort == 0 {
				localPort = remotePort
			}
			name := strings.ToLower(strings.TrimSpace(cfg.RemoteName))
			return name, localPort, remotePort, validateRemoteName(name)
		}
	}

	name, rest, err := parseRemoteNameAndCommand("forward", positionals, cfg)
	if err != nil {
		return "", 0, 0, err
	}
	if len(rest) == 0 {
		return "", 0, 0, fmt.Errorf("yolobox remote forward requires a remote port")
	}
	if len(rest) > 1 {
		return "", 0, 0, fmt.Errorf("unexpected remote forward args: %v", rest[1:])
	}
	remotePort, err := parsePort(rest[0])
	if err != nil {
		return "", 0, 0, err
	}
	if localPort == 0 {
		localPort = remotePort
	}
	return name, localPort, remotePort, nil
}

func parsePort(value string) (int, error) {
	value = strings.TrimSpace(value)
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q", value)
	}
	return port, nil
}

func ensureRemoteMachine(cfg Config, projectDir string, opts remoteProvisionOptions) (remoteMachine, error) {
	opts.Name = strings.ToLower(strings.TrimSpace(opts.Name))
	if err := validateRemoteName(opts.Name); err != nil {
		return remoteMachine{}, err
	}
	if opts.BackendURL != "" {
		cfg.Remote.BackendURL = strings.TrimRight(strings.TrimSpace(opts.BackendURL), "/")
	}
	machine, err := ensureRemoteBackendMachine(cfg, projectDir, opts)
	if err != nil {
		return remoteMachine{}, err
	}
	if !machine.BootstrapComplete {
		if err := waitForRemoteSSH(machine, 5*time.Minute); err != nil {
			return machine, err
		}
		if err := bootstrapRemoteHost(machine); err != nil {
			return machine, err
		}
		machine.BootstrapComplete = true
		machine.UpdatedAt = time.Now().UTC()
		if err := updateRemoteBackendMachine(cfg, machine); err != nil {
			return machine, err
		}
	}
	if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
		return machine, err
	}
	if err := updateRemoteBackendMachine(cfg, machine); err != nil {
		return machine, err
	}
	success("Remote %s is ready at %s via backend", machine.Name, machine.PublicIPv4)
	return machine, nil
}

func validateRemoteDefaults(cfg Config) error {
	if strings.TrimSpace(cfg.RemoteName) == "" {
		return fmt.Errorf(`mode = "remote" requires remote_name`)
	}
	if err := validateRemoteName(cfg.RemoteName); err != nil {
		return err
	}
	if err := validateRemoteBackendURL(remoteBackendURL(cfg)); err != nil {
		return fmt.Errorf("invalid remote backend URL: %w", err)
	}
	if remoteAuthToken(cfg) == "" {
		return fmt.Errorf("remote session token is required; run `yolobox login` or set %s", remoteAuthTokenEnv)
	}
	return nil
}

func validateRemoteName(name string) error {
	return validateForkName(name)
}

type gitRepoInfo struct {
	URL    string
	Branch string
}

func currentGitRepo(projectDir string) gitRepoInfo {
	url, err := gitOutput(projectDir, "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(url) == "" {
		return gitRepoInfo{}
	}
	branch, err := gitOutput(projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		branch = ""
	}
	branch = strings.TrimSpace(branch)
	if branch == "HEAD" {
		branch = ""
	}
	return gitRepoInfo{URL: strings.TrimSpace(url), Branch: branch}
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func waitForRemoteSSH(machine remoteMachine, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		cmd := exec.Command("ssh", append(remoteSSHOptions(false), "-o", "ConnectTimeout=5", machine.sshTarget(), "true")...)
		if err := cmd.Run(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for SSH on %s", machine.sshTarget())
		}
		time.Sleep(5 * time.Second)
	}
}

func bootstrapRemoteHost(machine remoteMachine) error {
	info("Bootstrapping remote host %s...", machine.Name)
	script := `set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
if command -v cloud-init >/dev/null 2>&1; then
  cloud-init status --wait >/dev/null 2>&1 || true
fi
apt-get update
apt-get install -y ca-certificates curl git rsync tmux
if ! command -v docker >/dev/null 2>&1; then
  curl -fsSL https://get.docker.com | sh
fi
if ! command -v yolobox >/dev/null 2>&1; then
  curl -fsSL https://raw.githubusercontent.com/finbarr/yolobox/master/install.sh | bash
fi
docker pull ghcr.io/finbarr/yolobox:latest || true
mkdir -p /opt/yolobox
chmod 755 /opt /opt/yolobox
`
	return runRemoteScript(machine, script, false)
}

func syncRemoteProject(machine *remoteMachine, cfg Config, projectDir string) error {
	if err := requireRemoteClientTools("ssh", "rsync"); err != nil {
		return err
	}
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return err
	}
	repo := currentGitRepo(sourcePath)
	machine.SourcePath = sourcePath
	machine.RepoURL = repo.URL
	machine.Branch = repo.Branch
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}

	info("Copying %s to %s:%s...", sourcePath, machine.Name, machine.ProjectPath)
	if err := ensureRemoteProjectPath(*machine); err != nil {
		return err
	}
	if err := rsyncPathToRemote(*machine, machine.ProjectPath, sourcePath); err != nil {
		return err
	}
	if len(cfg.Remote.Setup) > 0 {
		if err := runRemoteScript(*machine, buildRemoteSetupScript(*machine, cfg.Remote.Setup), false); err != nil {
			return err
		}
	}
	machine.LastSyncedAt = time.Now().UTC()
	machine.UpdatedAt = machine.LastSyncedAt
	return nil
}

func syncRemoteProjectDown(machine remoteMachine, projectDir string) error {
	if err := requireRemoteClientTools("ssh", "rsync"); err != nil {
		return err
	}
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return err
	}
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}
	info("Copying %s:%s back to %s...", machine.Name, machine.ProjectPath, sourcePath)
	return rsyncPathFromRemote(machine, machine.ProjectPath, sourcePath)
}

func normalizedProjectPath(projectDir string) (string, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

func remoteProjectPath() string {
	return remoteProjectRoot
}

func ensureRemoteProjectPath(machine remoteMachine) error {
	parent := filepath.Dir(machine.ProjectPath)
	script := "set -euo pipefail\n" +
		"if ! command -v rsync >/dev/null 2>&1; then\n" +
		"  apt-get update\n" +
		"  apt-get install -y rsync\n" +
		"fi\n" +
		"mkdir -p " + shellQuote(parent) + " " + shellQuote(machine.ProjectPath) + "\n"
	return runRemoteScript(machine, script, false)
}

func rsyncPathToRemote(machine remoteMachine, projectPath string, sourcePath string) error {
	source := sourcePath + string(os.PathSeparator)
	target := machine.sshTarget() + ":" + projectPath + "/"
	args := []string{
		"-az",
		"--delete",
		"--human-readable",
		"--info=stats1",
		"-e", remoteSSHCommand(false),
		source,
		target,
	}
	cmd := exec.Command("rsync", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func rsyncPathFromRemote(machine remoteMachine, projectPath string, destinationPath string) error {
	source := machine.sshTarget() + ":" + projectPath + "/"
	target := destinationPath + string(os.PathSeparator)
	args := []string{
		"-az",
		"--delete",
		"--human-readable",
		"--info=stats1",
		"-e", remoteSSHCommand(false),
		source,
		target,
	}
	cmd := exec.Command("rsync", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildRemoteSetupScript(machine remoteMachine, setup []string) string {
	var b strings.Builder
	b.WriteString("set -euo pipefail\n")
	b.WriteString("cd " + shellQuote(machine.ProjectPath) + "\n")
	for _, command := range setup {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		b.WriteString(command + "\n")
	}
	return b.String()
}

func attachRemoteMachine(cfg Config, machine remoteMachine, commandArgs []string) error {
	if machine.PublicIPv4 == "" {
		return fmt.Errorf("remote %s has no public IPv4; run yolobox remote status %s", machine.Name, machine.Name)
	}
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}
	if len(commandArgs) == 0 {
		commandArgs = []string{"shell"}
	}

	stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	interactive := remoteCommandNeedsTTY(commandArgs)
	sshTTY := false
	var remoteCommand string

	if interactive {
		if stdinTTY && stdoutTTY {
			remoteCommand = remoteTmuxCommand(machine, append([]string{"yolobox"}, commandArgs...), true)
			sshTTY = true
			info("Attaching to remote %s (%s)", machine.Name, machine.PublicIPv4)
		} else {
			remoteCommand = remoteTmuxCommand(machine, append([]string{"yolobox"}, commandArgs...), false)
			info("Starting detached remote session %s (%s); run from a terminal to attach", machine.Name, machine.PublicIPv4)
		}
	} else {
		remoteCommand = remoteDirectCommand(machine, append([]string{"yolobox"}, commandArgs...))
		info("Running on remote %s (%s)", machine.Name, machine.PublicIPv4)
	}

	if err := runSSHCommand(machine, remoteCommand, sshTTY, shouldForwardSSHAgent(machine.RepoURL)); err != nil {
		return err
	}

	machine.LastCommand = commandArgs
	machine.UpdatedAt = time.Now().UTC()
	if err := updateRemoteBackendMachine(cfg, machine); err != nil {
		warn("Could not update remote backend machine state: %v", err)
	}
	return nil
}

func remoteDefaultCommand(cfg Config) []string {
	if harness := normalizeDefaultHarness(cfg.DefaultHarness); harness != "" {
		return []string{harness}
	}
	return []string{"shell"}
}

func remoteCommandNeedsTTY(command []string) bool {
	if len(command) == 0 {
		return true
	}
	cmd := filepath.Base(command[0])
	switch cmd {
	case "shell":
		return true
	case "run":
		return shouldAttachTTY(command[1:], false, true, true)
	default:
		return shouldAttachTTY(command, false, true, true)
	}
}

func remoteCommandPrefix(machine remoteMachine) string {
	return "export PATH=\"/root/.local/bin:$PATH\"; cd " + shellQuote(machine.ProjectPath) + "; "
}

func remoteDirectCommand(machine remoteMachine, command []string) string {
	return remoteCommandPrefix(machine) + "exec " + shellJoin(command)
}

func remoteTmuxCommand(machine remoteMachine, command []string, attach bool) string {
	if attach {
		return remoteCommandPrefix(machine) + "tmux new-session -A -s " + shellQuote(remoteDefaultSessionName) + " -c " + shellQuote(machine.ProjectPath) + " " + shellQuote(shellJoin(command))
	}
	return remoteCommandPrefix(machine) + "tmux has-session -t " + shellQuote(remoteDefaultSessionName) + " 2>/dev/null || tmux new-session -d -s " + shellQuote(remoteDefaultSessionName) + " -c " + shellQuote(machine.ProjectPath) + " " + shellQuote(shellJoin(command))
}

func runRemoteScript(machine remoteMachine, script string, forwardAgent bool) error {
	return runSSHCommand(machine, "bash -s", false, forwardAgent, strings.NewReader(script))
}

func runSSHCommand(machine remoteMachine, remoteCommand string, tty bool, forwardAgent bool, stdin ...*strings.Reader) error {
	if err := requireRemoteClientTools("ssh"); err != nil {
		return err
	}
	args := remoteSSHOptions(forwardAgent)
	if tty {
		args = append(args, "-t")
	}
	args = append(args, machine.sshTarget(), remoteCommand)
	cmd := exec.Command("ssh", args...)
	if len(stdin) > 0 {
		cmd.Stdin = stdin[0]
	} else {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runSSHForward(machine remoteMachine, localPort int, remotePort int) error {
	if err := requireRemoteClientTools("ssh"); err != nil {
		return err
	}
	forwardSpec := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, remotePort)
	args := append(remoteSSHOptions(false), "-N", "-L", forwardSpec, machine.sshTarget())
	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func remoteSSHOptions(forwardAgent bool) []string {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ServerAliveInterval=30",
	}
	if forwardAgent {
		args = append(args, "-A")
	}
	return args
}

func remoteSSHCommand(forwardAgent bool) string {
	args := append([]string{"ssh"}, remoteSSHOptions(forwardAgent)...)
	return strings.Join(args, " ")
}

func (m remoteMachine) sshTarget() string {
	user := m.SSHUser
	if user == "" {
		user = "root"
	}
	return user + "@" + m.PublicIPv4
}

func shouldForwardSSHAgent(repoURL string) bool {
	repoURL = strings.TrimSpace(repoURL)
	return strings.HasPrefix(repoURL, "git@") || strings.HasPrefix(repoURL, "ssh://")
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func requireRemoteClientTools(names ...string) error {
	for _, name := range names {
		if commandExists(name) {
			continue
		}
		switch name {
		case "rsync":
			return fmt.Errorf("rsync is required for remote mode full-directory sync")
		case "ssh":
			return fmt.Errorf("ssh is required for remote mode")
		default:
			return fmt.Errorf("%s is required for remote mode", name)
		}
	}
	return nil
}

func displayTime(value time.Time) string {
	if value.IsZero() {
		return "(not set)"
	}
	return value.Format(time.RFC3339)
}
