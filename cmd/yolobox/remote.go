package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	PreviewHostname   string    `json:"preview_hostname,omitempty"`
	PreviewURL        string    `json:"preview_url,omitempty"`
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
	Tier       string
}

func runRemote(args []string, projectDir string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printRemoteUsage()
		return errHelp
	}

	switch args[0] {
	case "create":
		return runRemoteCreate(args[1:], projectDir)
	case "run":
		return runRemoteRun(args[1:], projectDir)
	case "connect":
		return runRemoteConnect(args[1:], projectDir)
	case "sync":
		return runRemoteSync(args[1:], projectDir)
	case "list":
		return runRemoteList(args[1:], projectDir)
	case "status":
		return runRemoteStatus(args[1:], projectDir)
	case "destroy":
		return runRemoteDestroy(args[1:], projectDir)
	default:
		return fmt.Errorf("unknown remote command: %s", args[0])
	}
}

func printRemoteUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox remote create <env> [--tier <tier>] [--no-sync]")
	fmt.Fprintln(os.Stderr, "                                             Create a machine and sync this folder")
	fmt.Fprintln(os.Stderr, "  yolobox remote run <env> <cmd...>          Sync and run on an existing machine")
	fmt.Fprintln(os.Stderr, "  yolobox remote connect <env>               Connect to a shell without syncing")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync up <env>               Copy the current folder to the remote machine")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync down <env> --force     Copy the remote project back locally")
	fmt.Fprintln(os.Stderr, "  yolobox remote list                        List backend machines")
	fmt.Fprintln(os.Stderr, "  yolobox remote status <env>                Show backend machine state")
	fmt.Fprintln(os.Stderr, "  yolobox remote destroy <env> --force       Release/delete remote machine")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "OPTIONS:")
	fmt.Fprintln(os.Stderr, "  --no-sync            Skip the create command's initial project sync")
	fmt.Fprintln(os.Stderr, "  --tier <tier>        Machine size tier for create: small, medium, or large")
	fmt.Fprintln(os.Stderr, "  --ssh-user <user>    SSH user for create")
	fmt.Fprintln(os.Stderr, "  --backend-url <url>  Remote backend API URL for create")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "EXAMPLES:")
	fmt.Fprintln(os.Stderr, "  yolobox login")
	fmt.Fprintln(os.Stderr, "  yolobox remote create foo")
	fmt.Fprintln(os.Stderr, "  yolobox remote run foo codex")
	fmt.Fprintln(os.Stderr, "  yolobox remote connect foo")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync up foo")
}

func runRemoteDefault(cfg Config, projectDir string) error {
	if err := validateRemoteDefaults(cfg); err != nil {
		return err
	}
	name := strings.TrimSpace(cfg.RemoteName)
	machine, err := prepareExistingRemoteMachine(cfg, projectDir, name, true)
	if err != nil {
		return err
	}
	return runRemoteMachineCommand(cfg, machine, remoteDefaultCommand(cfg))
}

func runRemoteCreate(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}

	opts, noSync, err := parseRemoteCreateArgs(args, cfg)
	if err != nil {
		return err
	}
	cfg, err = remoteConfigForProvision(cfg, opts)
	if err != nil {
		return err
	}

	_, err = createRemoteMachine(cfg, projectDir, opts, !noSync)
	return err
}

func runRemoteRun(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	name, commandArgs, err := parseRemoteNameAndCommand("run", args)
	if err != nil {
		return err
	}
	if len(commandArgs) == 0 {
		return fmt.Errorf("yolobox remote run requires a command")
	}
	machine, err := prepareExistingRemoteMachine(cfg, projectDir, name, true)
	if err != nil {
		return err
	}
	return runRemoteMachineCommand(cfg, machine, commandArgs)
}

func parseRemoteCreateArgs(args []string, cfg Config) (remoteProvisionOptions, bool, error) {
	opts := remoteProvisionOptions{
		SSHUser:    cfg.Remote.SSHUser,
		BackendURL: remoteBackendURL(cfg),
	}
	noSync := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-sync":
			noSync = true
		case arg == "--ssh-user":
			i++
			if i >= len(args) {
				return opts, noSync, fmt.Errorf("remote create --ssh-user requires a value")
			}
			opts.SSHUser = args[i]
		case strings.HasPrefix(arg, "--ssh-user="):
			opts.SSHUser = strings.TrimPrefix(arg, "--ssh-user=")
		case arg == "--tier":
			i++
			if i >= len(args) {
				return opts, noSync, fmt.Errorf("remote create --tier requires a value")
			}
			opts.Tier = args[i]
		case strings.HasPrefix(arg, "--tier="):
			opts.Tier = strings.TrimPrefix(arg, "--tier=")
		case arg == "--backend-url":
			i++
			if i >= len(args) {
				return opts, noSync, fmt.Errorf("remote create --backend-url requires a value")
			}
			opts.BackendURL = args[i]
		case strings.HasPrefix(arg, "--backend-url="):
			opts.BackendURL = strings.TrimPrefix(arg, "--backend-url=")
		case strings.HasPrefix(arg, "-"):
			return opts, noSync, fmt.Errorf("unknown remote create option: %s", arg)
		default:
			if opts.Name != "" {
				return opts, noSync, fmt.Errorf("unexpected remote create argument: %s", arg)
			}
			opts.Name = arg
		}
	}
	opts.Name = strings.ToLower(strings.TrimSpace(opts.Name))
	opts.SSHUser = strings.TrimSpace(opts.SSHUser)
	tier, err := normalizeRemoteMachineTier(opts.Tier)
	if err != nil {
		return opts, noSync, err
	}
	opts.Tier = tier
	opts.BackendURL = strings.TrimRight(strings.TrimSpace(opts.BackendURL), "/")
	if opts.Name == "" {
		return opts, noSync, fmt.Errorf("yolobox remote create requires a remote name")
	}
	if err := validateRemoteName(opts.Name); err != nil {
		return opts, noSync, err
	}
	return opts, noSync, nil
}

func normalizeRemoteMachineTier(tier string) (string, error) {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "" {
		return "", nil
	}
	switch tier {
	case "small", "medium", "large":
		return tier, nil
	default:
		return "", fmt.Errorf("invalid remote machine tier %q; expected small, medium, or large", tier)
	}
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

func runRemoteConnect(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	name, rest, err := parseRemoteNameAndCommand("connect", args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected remote connect args: %v", rest)
	}
	machine, _, err := getRemoteBackendMachine(cfg, name)
	if err != nil {
		return err
	}
	machine, err = prepareRemoteMachineForConnect(cfg, machine, projectDir)
	if err != nil {
		return err
	}
	return runRemoteMachineCommand(cfg, machine, []string{"shell"})
}

func runRemoteSync(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return fmt.Errorf("yolobox remote sync requires direction: up or down")
	}
	direction := args[0]
	if direction != "up" && direction != "down" {
		return fmt.Errorf("unknown remote sync direction %q", direction)
	}
	args = args[1:]
	force := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--force" {
			force = true
			continue
		}
		filtered = append(filtered, arg)
	}

	name, rest, err := parseRemoteNameAndCommand("sync "+direction, filtered)
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
	if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %-18s %-32s %s\n", "NAME", "PROVIDER", "IP", "REGION", "SIZE", "PREVIEW", "STORAGE"); err != nil {
		return err
	}
	for _, m := range machines {
		if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %-18s %-32s %s\n", m.Name, configValueOrNotSet(m.Provider), m.PublicIPv4, m.Region, configValueOrNotSet(m.Size), configValueOrNotSet(m.PreviewHostname), m.ProjectPath); err != nil {
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
	name, rest, err := parseRemoteNameAndCommand("status", args)
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
	fmt.Printf("%ssize:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.Size))
	fmt.Printf("%sssh_user:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.SSHUser))
	fmt.Printf("%spreview_url:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.PreviewURL))
	fmt.Printf("%ssource_path:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.SourcePath))
	fmt.Printf("%srepo:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.RepoURL))
	fmt.Printf("%sbranch:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.Branch))
	fmt.Printf("%sproject_path:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.ProjectPath))
	fmt.Printf("%swork_path:%s %s\n", colorBold, colorReset, configValueOrNotSet(remoteWorkPath(machine)))
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

func parseRemoteNameAndCommand(command string, args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("yolobox remote %s requires a remote name", command)
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if err := validateRemoteName(name); err != nil {
		return "", nil, err
	}
	return name, args[1:], nil
}

func createRemoteMachine(cfg Config, projectDir string, opts remoteProvisionOptions, syncProject bool) (remoteMachine, error) {
	opts.Name = strings.ToLower(strings.TrimSpace(opts.Name))
	if err := validateRemoteName(opts.Name); err != nil {
		return remoteMachine{}, err
	}
	if opts.BackendURL != "" {
		cfg.Remote.BackendURL = strings.TrimRight(strings.TrimSpace(opts.BackendURL), "/")
	}
	if err := requireRemoteClientTools("ssh"); err != nil {
		return remoteMachine{}, err
	}
	machine, err := createRemoteBackendMachine(cfg, projectDir, opts)
	if err != nil {
		return remoteMachine{}, err
	}
	machine, err = prepareRemoteMachineForConnect(cfg, machine, projectDir)
	if err != nil {
		return machine, err
	}
	if syncProject {
		if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
			return machine, err
		}
		if err := updateRemoteBackendMachine(cfg, machine); err != nil {
			return machine, err
		}
	}
	if machine.PreviewURL != "" {
		success("Remote %s is ready at %s via backend; preview %s", machine.Name, machine.PublicIPv4, machine.PreviewURL)
	} else {
		success("Remote %s is ready at %s via backend", machine.Name, machine.PublicIPv4)
	}
	return machine, nil
}

func prepareExistingRemoteMachine(cfg Config, projectDir string, name string, syncProject bool) (remoteMachine, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := validateRemoteName(name); err != nil {
		return remoteMachine{}, err
	}
	machine, _, err := getRemoteBackendMachine(cfg, name)
	if err != nil {
		return remoteMachine{}, err
	}
	machine, err = prepareRemoteMachineForConnect(cfg, machine, projectDir)
	if err != nil {
		return machine, err
	}
	if syncProject {
		if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
			return machine, err
		}
		if err := updateRemoteBackendMachine(cfg, machine); err != nil {
			return machine, err
		}
	}
	if machine.PreviewURL != "" {
		success("Remote %s is ready at %s via backend; preview %s", machine.Name, machine.PublicIPv4, machine.PreviewURL)
	} else {
		success("Remote %s is ready at %s via backend", machine.Name, machine.PublicIPv4)
	}
	return machine, nil
}

func prepareRemoteMachineForConnect(cfg Config, machine remoteMachine, projectDir string) (remoteMachine, error) {
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}
	if err := ensureRemoteMachineSourcePath(&machine, projectDir); err != nil {
		return machine, err
	}
	if err := requireRemoteClientTools("ssh"); err != nil {
		return machine, err
	}
	if err := waitForRemoteSSH(machine, 5*time.Minute); err != nil {
		return machine, err
	}
	if err := bootstrapRemoteHost(machine); err != nil {
		return machine, err
	}
	if !machine.BootstrapComplete {
		machine.BootstrapComplete = true
		machine.UpdatedAt = time.Now().UTC()
	}
	if err := ensureRemoteProjectPath(machine); err != nil {
		return machine, err
	}
	if err := updateRemoteBackendMachine(cfg, machine); err != nil {
		return machine, err
	}
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
	info("Preparing remote host %s...", machine.Name)
	return runRemoteScript(machine, buildRemoteBootstrapScript(), false)
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

func cleanRemoteSourcePath(sourcePath string) string {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return ""
	}
	cleaned := filepath.Clean(sourcePath)
	if cleaned == "." || cleaned == string(os.PathSeparator) || !filepath.IsAbs(cleaned) {
		return ""
	}
	return cleaned
}

func ensureRemoteMachineSourcePath(machine *remoteMachine, projectDir string) error {
	if sourcePath := cleanRemoteSourcePath(machine.SourcePath); sourcePath != "" {
		machine.SourcePath = sourcePath
		return nil
	}
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return err
	}
	machine.SourcePath = sourcePath
	return nil
}

func remoteWorkPath(machine remoteMachine) string {
	if sourcePath := cleanRemoteSourcePath(machine.SourcePath); sourcePath != "" {
		return sourcePath
	}
	projectPath := strings.TrimSpace(machine.ProjectPath)
	if projectPath == "" {
		return remoteProjectPath()
	}
	cleaned := filepath.Clean(projectPath)
	if cleaned == "." || !filepath.IsAbs(cleaned) {
		return remoteProjectPath()
	}
	return cleaned
}

func ensureRemoteProjectPath(machine remoteMachine) error {
	return runRemoteScript(machine, buildEnsureRemoteProjectPathScript(machine), false)
}

func buildEnsureRemoteProjectPathScript(machine remoteMachine) string {
	projectPath := strings.TrimSpace(machine.ProjectPath)
	if projectPath == "" {
		projectPath = remoteProjectPath()
	}
	projectPath = filepath.Clean(projectPath)
	workPath := remoteWorkPath(machine)
	projectParent := filepath.Dir(projectPath)
	workParent := filepath.Dir(workPath)

	var b strings.Builder
	b.WriteString("set -euo pipefail\n")
	b.WriteString("if ! command -v rsync >/dev/null 2>&1; then\n")
	b.WriteString("  apt-get update\n")
	b.WriteString("  apt-get install -y rsync\n")
	b.WriteString("fi\n")
	b.WriteString("storage=" + shellQuote(projectPath) + "\n")
	b.WriteString("work=" + shellQuote(workPath) + "\n")
	b.WriteString("mkdir -p " + shellQuote(projectParent) + " \"$storage\"\n")
	if workPath != projectPath {
		b.WriteString("mkdir -p " + shellQuote(workParent) + "\n")
		b.WriteString("if [ -L \"$work\" ]; then\n")
		b.WriteString("  current=\"$(readlink \"$work\" || true)\"\n")
		b.WriteString("  if [ \"$current\" != \"$storage\" ]; then\n")
		b.WriteString("    ln -sfn \"$storage\" \"$work\"\n")
		b.WriteString("  fi\n")
		b.WriteString("elif [ -e \"$work\" ]; then\n")
		b.WriteString("  if [ -d \"$work\" ] && [ -z \"$(find \"$work\" -mindepth 1 -maxdepth 1 -print -quit)\" ]; then\n")
		b.WriteString("    rmdir \"$work\"\n")
		b.WriteString("    ln -s \"$storage\" \"$work\"\n")
		b.WriteString("  else\n")
		b.WriteString("    echo \"cannot map remote project workdir $work: path already exists and is not an empty directory or symlink\" >&2\n")
		b.WriteString("    exit 1\n")
		b.WriteString("  fi\n")
		b.WriteString("else\n")
		b.WriteString("  ln -s \"$storage\" \"$work\"\n")
		b.WriteString("fi\n")
	}
	return b.String()
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
	b.WriteString("cd " + shellQuote(remoteWorkPath(machine)) + "\n")
	for _, command := range setup {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		b.WriteString(command + "\n")
	}
	return b.String()
}

func runRemoteMachineCommand(cfg Config, machine remoteMachine, commandArgs []string) error {
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
	recordCommand := true
	var remoteCommand string

	if interactive {
		sessionExists, err := remoteManagedSessionExists(machine)
		if err != nil {
			return err
		}
		if stdinTTY && stdoutTTY {
			if sessionExists {
				remoteCommand = remoteTmuxAttachCommand(machine)
				recordCommand = false
				if remoteShellCommand(commandArgs) {
					info("Connecting to existing remote session %s (%s)", machine.Name, machine.PublicIPv4)
				} else {
					info("Connecting to existing remote session %s (%s); %q was not started", machine.Name, machine.PublicIPv4, strings.Join(commandArgs, " "))
				}
			} else {
				remoteCommand = remoteTmuxCommand(machine, remoteHostCommand(commandArgs), true)
				info("Starting remote session %s (%s)", machine.Name, machine.PublicIPv4)
			}
			sshTTY = true
		} else {
			if sessionExists {
				return fmt.Errorf("remote session %s is already running; run `yolobox remote connect %s` from a terminal to attach", machine.Name, machine.Name)
			}
			remoteCommand = remoteTmuxCommand(machine, remoteHostCommand(commandArgs), false)
			info("Starting detached remote session %s (%s); run from a terminal to connect", machine.Name, machine.PublicIPv4)
		}
	} else {
		remoteCommand = remoteDirectCommand(machine, remoteHostCommand(commandArgs))
		info("Running on remote %s (%s)", machine.Name, machine.PublicIPv4)
	}

	if err := runSSHCommand(machine, remoteCommand, sshTTY, shouldForwardSSHAgent(machine.RepoURL)); err != nil {
		return err
	}

	if !recordCommand {
		return nil
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

func remoteShellCommand(command []string) bool {
	if len(command) == 0 {
		return true
	}
	if len(command) != 1 {
		return false
	}
	cmd := filepath.Base(command[0])
	return cmd == "shell" || cmd == "run"
}

func remoteHostCommand(command []string) []string {
	if len(command) == 0 {
		return []string{"bash"}
	}
	switch filepath.Base(command[0]) {
	case "shell":
		return []string{"bash"}
	case "run":
		if len(command) == 1 {
			return []string{"bash"}
		}
		return append([]string{}, command[1:]...)
	default:
		return append([]string{}, command...)
	}
}

func remoteCommandPrefix(machine remoteMachine) string {
	workPath := remoteWorkPath(machine)
	parts := []string{
		"export PATH=\"/opt/yolobox/bin:/root/.npm-global/bin:/home/yolo/.npm-global/bin:/root/.local/bin:/home/yolo/.local/bin:/usr/local/go/bin:$PATH\"",
		"export NPM_CONFIG_PREFIX=\"${NPM_CONFIG_PREFIX:-$HOME/.npm-global}\"",
		"export YOLOBOX=1",
		"export YOLOBOX_REMOTE=1",
		"export YOLOBOX_PROJECT_PATH=" + shellQuote(workPath),
		"export YOLOBOX_CONTEXT_FILE=" + shellQuote(yoloboxContextFile),
	}
	if strings.TrimSpace(machine.PreviewURL) != "" {
		parts = append(parts, "export YOLOBOX_PREVIEW_URL="+shellQuote(machine.PreviewURL))
	}
	if strings.TrimSpace(machine.PreviewHostname) != "" {
		parts = append(parts, "export YOLOBOX_PREVIEW_HOSTNAME="+shellQuote(machine.PreviewHostname))
	}
	parts = append(parts,
		"if [ -x "+shellQuote(remoteRuntimeSessionScript)+" ]; then "+shellQuote(remoteRuntimeSessionScript)+" "+shellQuote(workPath)+"; fi",
		"cd "+shellQuote(workPath),
	)
	return strings.Join(parts, "; ") + "; "
}

func remoteDirectCommand(machine remoteMachine, command []string) string {
	return remoteCommandPrefix(machine) + "exec " + shellJoin(command)
}

func remoteTmuxAttachCommand(machine remoteMachine) string {
	return remoteCommandPrefix(machine) + "tmux attach-session -t " + shellQuote(remoteDefaultSessionName)
}

func remoteTmuxCommand(machine remoteMachine, command []string, joinSession bool) string {
	workPath := remoteWorkPath(machine)
	sessionCommand := remoteCommandPrefix(machine) + "exec " + shellJoin(command)
	if joinSession {
		return remoteCommandPrefix(machine) + "tmux new-session -A -s " + shellQuote(remoteDefaultSessionName) + " -c " + shellQuote(workPath) + " " + shellQuote(sessionCommand)
	}
	return remoteCommandPrefix(machine) + "tmux new-session -d -s " + shellQuote(remoteDefaultSessionName) + " -c " + shellQuote(workPath) + " " + shellQuote(sessionCommand)
}

func remoteManagedSessionExists(machine remoteMachine) (bool, error) {
	if err := requireRemoteClientTools("ssh"); err != nil {
		return false, err
	}
	args := remoteSSHOptions(false)
	args = append(args, machine.sshTarget(), "tmux has-session -t "+shellQuote(remoteDefaultSessionName)+" 2>/dev/null")
	cmd := exec.Command("ssh", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("check remote session %s: %w", machine.Name, err)
	}
	return true, nil
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
