package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

const (
	remoteDefaultSessionName = "yolobox"
	remoteProjectRoot        = "/opt/yolobox/project"
	remoteKnownHostsFileName = "remote_known_hosts"
)

var remoteBackendAgentRetryDelay = 5 * time.Second

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
	SSHPrivateKey     string    `json:"-"`
	SSHKeyPath        string    `json:"-"`
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
	machine, cleanup, err := prepareExistingRemoteMachine(cfg, projectDir, name, true)
	if err != nil {
		return err
	}
	defer cleanup()
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
	machine, cleanup, err := prepareExistingRemoteMachine(cfg, projectDir, name, true)
	if err != nil {
		return err
	}
	defer cleanup()
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
	if err := requireRemoteMachineBootstrapped(machine); err != nil {
		return err
	}
	cleanup, err := attachRemoteTunnelCredentials(cfg, &machine)
	if err != nil {
		return err
	}
	defer cleanup()
	machine, err = checkRemoteMachineForConnect(machine)
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
	if err := requireRemoteMachineBootstrapped(machine); err != nil {
		return err
	}
	cleanup, err := attachRemoteTunnelCredentials(cfg, &machine)
	if err != nil {
		return err
	}
	defer cleanup()

	switch direction {
	case "up":
		machine, err = prepareRemoteMachineForWorkspace(cfg, machine, projectDir)
		if err != nil {
			return err
		}
		if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
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
	table := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "NAME\tSIZE\tURL"); err != nil {
		return err
	}
	for _, m := range machines {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", m.Name, configValueOrNotSet(m.Size), remoteListURL(m)); err != nil {
			return err
		}
	}
	return table.Flush()
}

func remoteListURL(machine remoteMachine) string {
	if machine.PreviewURL != "" {
		return machine.PreviewURL
	}
	if machine.PreviewHostname != "" {
		return "https://" + machine.PreviewHostname
	}
	return "-"
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
	var machine remoteMachine
	if err := runWithSpinner(
		fmt.Sprintf("Creating remote %s via backend", opts.Name),
		fmt.Sprintf("Remote %s created", opts.Name),
		func() error {
			var err error
			machine, err = createRemoteBackendMachine(cfg, projectDir, opts)
			return err
		},
	); err != nil {
		return remoteMachine{}, err
	}
	var err error
	cleanup, err := attachRemoteTunnelCredentials(cfg, &machine)
	if err != nil {
		return machine, err
	}
	defer cleanup()
	machine, err = prepareRemoteMachineForWorkspace(cfg, machine, projectDir)
	if err != nil {
		return machine, err
	}
	if syncProject {
		if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
			return machine, err
		}
	}
	printRemoteReady(machine)
	return machine, nil
}

func prepareExistingRemoteMachine(cfg Config, projectDir string, name string, syncProject bool) (remoteMachine, func(), error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := validateRemoteName(name); err != nil {
		return remoteMachine{}, func() {}, err
	}
	machine, _, err := getRemoteBackendMachine(cfg, name)
	if err != nil {
		return remoteMachine{}, func() {}, err
	}
	if err := requireRemoteMachineBootstrapped(machine); err != nil {
		return machine, func() {}, err
	}
	cleanup, err := attachRemoteTunnelCredentials(cfg, &machine)
	if err != nil {
		return machine, func() {}, err
	}
	machine, err = prepareRemoteMachineForWorkspace(cfg, machine, projectDir)
	if err != nil {
		cleanup()
		return machine, func() {}, err
	}
	if syncProject {
		if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
			cleanup()
			return machine, func() {}, err
		}
	}
	printRemoteReady(machine)
	return machine, cleanup, nil
}

func prepareRemoteMachineForWorkspace(cfg Config, machine remoteMachine, projectDir string) (remoteMachine, error) {
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}
	if err := ensureRemoteMachineSourcePath(&machine, projectDir); err != nil {
		return machine, err
	}
	if err := requireRemoteClientTools("ssh"); err != nil {
		return machine, err
	}
	if err := requireRemoteMachineBootstrapped(machine); err != nil {
		return machine, err
	}
	if err := runWithSpinner(
		fmt.Sprintf("Preparing remote host %s", machine.Name),
		fmt.Sprintf("Remote host %s prepared", machine.Name),
		func() error {
			var err error
			machine, err = waitForRemoteBackendWorkspace(cfg, machine, 5*time.Minute)
			return err
		},
	); err != nil {
		return machine, err
	}
	if err := waitForRemoteMachineSSH(machine, "Waiting for SSH on remote"); err != nil {
		return machine, err
	}
	return machine, nil
}

func waitForRemoteBackendWorkspace(cfg Config, machine remoteMachine, timeout time.Duration) (remoteMachine, error) {
	deadline := time.Now().Add(timeout)
	for {
		updated, err := prepareRemoteBackendWorkspace(cfg, machine)
		if err == nil {
			return updated, nil
		}
		if !shouldRetryRemoteBackendWorkspace(err) || time.Now().After(deadline) {
			return machine, err
		}
		time.Sleep(remoteBackendAgentRetryDelay)
	}
}

func shouldRetryRemoteBackendWorkspace(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "agent_disconnected") ||
		strings.Contains(message, "remote machine agent is not connected")
}

func checkRemoteMachineForConnect(machine remoteMachine) (remoteMachine, error) {
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}
	if err := requireRemoteClientTools("ssh"); err != nil {
		return machine, err
	}
	if err := requireRemoteMachineBootstrapped(machine); err != nil {
		return machine, err
	}
	if err := waitForRemoteMachineSSH(machine, "Checking SSH on remote"); err != nil {
		return machine, err
	}
	return machine, nil
}

func attachRemoteTunnelCredentials(cfg Config, machine *remoteMachine) (func(), error) {
	privateKey := strings.TrimSpace(machine.SSHPrivateKey)
	if privateKey == "" {
		key, err := getRemoteBackendTunnelKey(cfg, machine.Name)
		if err != nil {
			return func() {}, err
		}
		privateKey = strings.TrimSpace(key)
	}
	if privateKey == "" {
		return func() {}, fmt.Errorf("remote %s does not have backend tunnel SSH credentials", machine.Name)
	}
	keyFile, err := os.CreateTemp("", "yolobox-remote-key-*")
	if err != nil {
		return func() {}, err
	}
	path := keyFile.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := os.Chmod(path, 0600); err != nil {
		_ = keyFile.Close()
		cleanup()
		return func() {}, err
	}
	if _, err := keyFile.WriteString(privateKey + "\n"); err != nil {
		_ = keyFile.Close()
		cleanup()
		return func() {}, err
	}
	if err := keyFile.Close(); err != nil {
		cleanup()
		return func() {}, err
	}
	machine.SSHPrivateKey = privateKey
	machine.SSHKeyPath = path
	return cleanup, nil
}

func requireRemoteMachineBootstrapped(machine remoteMachine) error {
	if machine.BootstrapComplete {
		return nil
	}
	return fmt.Errorf("remote %s is not bootstrapped; backend setup has not completed for this machine", machine.Name)
}

func waitForRemoteMachineSSH(machine remoteMachine, messagePrefix string) error {
	return runWithSpinner(
		fmt.Sprintf("%s %s", messagePrefix, machine.Name),
		fmt.Sprintf("SSH ready on remote %s", machine.Name),
		func() error {
			return waitForRemoteSSH(machine, 5*time.Minute)
		},
	)
}

func printRemoteReady(machine remoteMachine) {
	success("Remote %s is ready", machine.Name)
	if previewURL := strings.TrimSpace(machine.PreviewURL); previewURL != "" {
		link("Preview: %s", previewURL)
	}
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
		args, err := remoteSSHOptions(machine, false)
		if err != nil {
			return err
		}
		cmd := exec.Command("ssh", append(args, "-o", "ConnectTimeout=5", machine.sshTarget(), "true")...)
		if err := cmd.Run(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for remote tunnel SSH on %s", machine.Name)
		}
		time.Sleep(5 * time.Second)
	}
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
	if err := rsyncPathToRemote(*machine, machine.ProjectPath, sourcePath); err != nil {
		return err
	}
	if err := runRemoteBackendSetup(cfg, *machine, cfg.Remote.Setup); err != nil {
		return err
	}
	machine.LastSyncedAt = time.Now().UTC()
	machine.UpdatedAt = machine.LastSyncedAt
	*machine, err = completeRemoteBackendSync(cfg, *machine)
	return err
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

func rsyncPathToRemote(machine remoteMachine, projectPath string, sourcePath string) error {
	source := sourcePath + string(os.PathSeparator)
	target := machine.sshTarget() + ":" + projectPath + "/"
	args := []string{
		"-az",
		"--delete",
		"--human-readable",
		source,
		target,
	}
	sshCommand, err := remoteSSHCommand(machine, false)
	if err != nil {
		return err
	}
	args = append(args[:3], append([]string{"-e", sshCommand}, args[3:]...)...)
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
		source,
		target,
	}
	sshCommand, err := remoteSSHCommand(machine, false)
	if err != nil {
		return err
	}
	args = append(args[:3], append([]string{"-e", sshCommand}, args[3:]...)...)
	cmd := exec.Command("rsync", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runRemoteMachineCommand(cfg Config, machine remoteMachine, commandArgs []string) error {
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}
	if len(commandArgs) == 0 {
		commandArgs = []string{"shell"}
	}

	stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	interactive := remoteCommandNeedsTTY(commandArgs)

	if interactive {
		if stdinTTY && stdoutTTY {
			result, updated, err := prepareRemoteBackendSession(cfg, machine, commandArgs, true)
			if err != nil {
				return err
			}
			machine = updated
			if result.Status == "exists" {
				if remoteShellCommand(commandArgs) {
					info("Connecting to existing remote session %s via backend tunnel", machine.Name)
				} else {
					info("Connecting to existing remote session %s via backend tunnel; %q was not started", machine.Name, strings.Join(commandArgs, " "))
				}
			} else {
				info("Starting remote session %s via backend tunnel", machine.Name)
			}
			if strings.TrimSpace(result.AttachCommand) == "" {
				return nil
			}
			return runSSHCommand(machine, result.AttachCommand, true, false)
		} else {
			if _, _, err := prepareRemoteBackendSession(cfg, machine, commandArgs, false); err != nil {
				return err
			}
			info("Starting detached remote session %s via backend tunnel; run from a terminal to connect", machine.Name)
			return nil
		}
	}

	remoteCommand, err := remoteBackendSSHCommand(cfg, machine, commandArgs)
	if err != nil {
		return err
	}
	info("Running on remote %s via backend tunnel", machine.Name)
	if err := runSSHCommand(machine, remoteCommand, false, shouldForwardSSHAgent(machine.RepoURL)); err != nil {
		return err
	}
	if err := recordRemoteBackendCommand(cfg, machine, commandArgs); err != nil {
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

func runSSHCommand(machine remoteMachine, remoteCommand string, tty bool, forwardAgent bool, stdin ...*strings.Reader) error {
	if err := requireRemoteClientTools("ssh"); err != nil {
		return err
	}
	args, err := remoteSSHOptions(machine, forwardAgent)
	if err != nil {
		return err
	}
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

func remoteSSHOptions(machine remoteMachine, forwardAgent bool) ([]string, error) {
	if strings.TrimSpace(machine.SSHKeyPath) == "" {
		return nil, fmt.Errorf("remote %s has no tunnel SSH key; remote operations require the backend tunnel", machine.Name)
	}
	knownHostsPath, err := remoteKnownHostsPath()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-i", machine.SSHKeyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "UserKnownHostsFile=" + knownHostsPath,
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "CheckHostIP=no",
		"-o", "HashKnownHosts=no",
		"-o", "ServerAliveInterval=30",
		"-o", "ProxyCommand=" + remoteSSHProxyCommand(machine),
	}
	if forwardAgent {
		args = append(args, "-A")
	}
	return args, nil
}

func remoteKnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for remote SSH known hosts: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("resolve home directory for remote SSH known hosts: home directory is empty")
	}
	dir := filepath.Join(home, ".yolobox")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create remote SSH state directory: %w", err)
	}
	path := filepath.Join(dir, remoteKnownHostsFileName)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
		if createErr != nil {
			return "", fmt.Errorf("create remote SSH known hosts file: %w", createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", fmt.Errorf("close remote SSH known hosts file: %w", closeErr)
		}
	} else if err != nil {
		return "", fmt.Errorf("stat remote SSH known hosts file: %w", err)
	}
	return path, nil
}

func remoteSSHCommand(machine remoteMachine, forwardAgent bool) (string, error) {
	options, err := remoteSSHOptions(machine, forwardAgent)
	if err != nil {
		return "", err
	}
	args := append([]string{"ssh"}, options...)
	return shellJoin(args), nil
}

func (m remoteMachine) sshTarget() string {
	user := m.SSHUser
	if user == "" {
		user = "root"
	}
	return user + "@yolobox-" + m.Name
}

func remoteSSHProxyCommand(machine remoteMachine) string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		exe = "yolobox"
	}
	return shellJoin([]string{exe, "__remote-ssh-proxy", machine.Name})
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
