package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	remoteProviderDigitalOcean = "digitalocean"
	remoteRegistryVersion      = 2
	remoteDefaultWorkspace     = "default"
	remoteDefaultSession       = "yolobox"
	remoteDefaultSessionName   = "main"
)

type remoteMachine struct {
	Name              string    `json:"name"`
	Provider          string    `json:"provider"`
	DropletID         string    `json:"droplet_id,omitempty"`
	DropletName       string    `json:"droplet_name,omitempty"`
	PublicIPv4        string    `json:"public_ipv4,omitempty"`
	Region            string    `json:"region,omitempty"`
	Size              string    `json:"size,omitempty"`
	Image             string    `json:"image,omitempty"`
	SSHUser           string    `json:"ssh_user,omitempty"`
	SourcePath        string    `json:"source_path,omitempty"`
	ProjectPath       string    `json:"project_path,omitempty"`
	RepoURL           string    `json:"repo_url,omitempty"`
	Branch            string    `json:"branch,omitempty"`
	ComposeProject    string    `json:"compose_project,omitempty"`
	LastCommand       []string  `json:"last_command,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	LastSyncedAt      time.Time `json:"last_synced_at,omitempty"`
	BootstrapComplete bool      `json:"bootstrap_complete,omitempty"`
}

type remoteWorkspace struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Machine        string    `json:"machine"`
	SourcePath     string    `json:"source_path,omitempty"`
	ProjectPath    string    `json:"project_path,omitempty"`
	RepoURL        string    `json:"repo_url,omitempty"`
	Branch         string    `json:"branch,omitempty"`
	ComposeProject string    `json:"compose_project,omitempty"`
	ContainerName  string    `json:"container_name,omitempty"`
	HomeVolume     string    `json:"home_volume,omitempty"`
	CacheVolume    string    `json:"cache_volume,omitempty"`
	OutputVolume   string    `json:"output_volume,omitempty"`
	NetworkName    string    `json:"network_name,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastSyncedAt   time.Time `json:"last_synced_at,omitempty"`
}

type remoteSession struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Machine     string    `json:"machine"`
	Workspace   string    `json:"workspace"`
	TmuxSession string    `json:"tmux_session"`
	LastCommand []string  `json:"last_command,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type remoteExposure struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Machine    string    `json:"machine"`
	Workspace  string    `json:"workspace"`
	Kind       string    `json:"kind"`
	LocalPort  int       `json:"local_port,omitempty"`
	RemotePort int       `json:"remote_port,omitempty"`
	TargetHost string    `json:"target_host,omitempty"`
	Visibility string    `json:"visibility,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type remoteRegistry struct {
	Version    int                        `json:"version"`
	Machines   map[string]remoteMachine   `json:"machines"`
	Workspaces map[string]remoteWorkspace `json:"workspaces,omitempty"`
	Sessions   map[string]remoteSession   `json:"sessions,omitempty"`
	Exposures  map[string]remoteExposure  `json:"exposures,omitempty"`
}

type remoteProvisionOptions struct {
	Name      string
	Workspace string
	Provider  string
	Region    string
	Size      string
	Image     string
	SSHKey    string
	SSHUser   string
}

type remoteRef struct {
	Machine   string
	Workspace string
}

type dropletInfo struct {
	ID         string
	Name       string
	PublicIPv4 string
	Status     string
}

func runRemote(args []string, projectDir string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printRemoteUsage()
		return errHelp
	}

	switch args[0] {
	case "attach":
		return runRemoteResume(args[1:], projectDir)
	case "resume":
		return runRemoteResume(args[1:], projectDir)
	case "sync":
		return runRemoteSync(args[1:], projectDir)
	case "stop":
		return runRemoteStop(args[1:], projectDir)
	case "forward":
		return runRemoteForward(args[1:], projectDir)
	case "expose":
		return runRemoteExpose(args[1:], projectDir)
	case "list":
		return runRemoteList(args[1:])
	case "status":
		return runRemoteStatus(args[1:], projectDir)
	case "destroy":
		return runRemoteDestroy(args[1:])
	default:
		return runRemoteCreate(args, projectDir)
	}
}

func printRemoteUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox remote --name <env> [cmd...]            Create or reuse a named remote machine")
	fmt.Fprintln(os.Stderr, "  yolobox remote resume [<env>[/<workspace>]] [cmd...] Reattach to a remote tmux session")
	fmt.Fprintln(os.Stderr, "  yolobox remote attach [<env>[/<workspace>]] [cmd...] Alias for resume")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync [up] [<env>[/<workspace>]]       Copy the current folder to the remote workspace")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync down [<env>[/<workspace>]] --force Copy the remote workspace back locally")
	fmt.Fprintln(os.Stderr, "  yolobox remote forward [<env>[/<workspace>]] <port>  Forward a remote preview port to localhost")
	fmt.Fprintln(os.Stderr, "  yolobox remote stop [<env>[/<workspace>]]            Stop the remote tmux session")
	fmt.Fprintln(os.Stderr, "  yolobox remote list                              List locally registered remote machines")
	fmt.Fprintln(os.Stderr, "  yolobox remote status [<env>[/<workspace>]]          Show local and provider state")
	fmt.Fprintln(os.Stderr, "  yolobox remote destroy <env> --force             Delete the Droplet and local registry entry")
	fmt.Fprintln(os.Stderr, "  Omit [<env>[/<workspace>]] only when remote_name is configured.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "OPTIONS:")
	fmt.Fprintln(os.Stderr, "  --name <env>       Remote machine name")
	fmt.Fprintln(os.Stderr, "  --workspace <name> Remote workspace name")
	fmt.Fprintln(os.Stderr, "  --provider <name>  Remote provider (MVP: digitalocean)")
	fmt.Fprintln(os.Stderr, "  --region <slug>    DigitalOcean region")
	fmt.Fprintln(os.Stderr, "  --size <slug>      DigitalOcean Droplet size")
	fmt.Fprintln(os.Stderr, "  --image <slug>     DigitalOcean image")
	fmt.Fprintln(os.Stderr, "  --ssh-key <id>     DigitalOcean SSH key ID or fingerprint")
	fmt.Fprintln(os.Stderr, "  --ssh-user <user>  SSH user for the remote host")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "EXAMPLES:")
	fmt.Fprintln(os.Stderr, "  yolobox remote --name foo codex")
	fmt.Fprintln(os.Stderr, "  yolobox remote resume foo/default codex")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync up foo/default")
	fmt.Fprintln(os.Stderr, "  yolobox remote forward foo/default 3000")
}

func runRemoteDefault(cfg Config, projectDir string) error {
	name := strings.TrimSpace(cfg.RemoteName)
	if name == "" {
		return fmt.Errorf(`mode = "remote" requires remote_name in config`)
	}
	if err := validateRemoteName(name); err != nil {
		return err
	}
	workspaceName := effectiveRemoteWorkspace(cfg.RemoteWorkspace)
	machineExisted := remoteMachineExists(name)
	machine, err := ensureRemoteMachine(cfg, projectDir, remoteProvisionOptions{Name: name})
	if err != nil {
		return err
	}
	workspace, err := ensureRemoteWorkspace(machine, cfg, projectDir, workspaceName, !machineExisted)
	if err != nil {
		return err
	}
	return attachRemoteWorkspace(machine, workspace, remoteDefaultSessionName, remoteDefaultCommand(cfg))
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
	opts.Workspace = effectiveRemoteWorkspace(opts.Workspace)
	if err := validateRemoteName(opts.Workspace); err != nil {
		return fmt.Errorf("invalid remote workspace: %w", err)
	}
	if len(commandArgs) == 0 {
		commandArgs = remoteDefaultCommand(cfg)
	}

	machine, err := ensureRemoteMachine(cfg, projectDir, opts)
	if err != nil {
		return err
	}
	workspace, err := ensureRemoteWorkspace(machine, cfg, projectDir, opts.Workspace, true)
	if err != nil {
		return err
	}
	return attachRemoteWorkspace(machine, workspace, remoteDefaultSessionName, commandArgs)
}

func parseRemoteCreateFlags(args []string, cfg Config) (remoteProvisionOptions, []string, error) {
	fs := flag.NewFlagSet("remote", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printRemoteUsage

	opts := remoteProvisionOptions{
		Name:      strings.TrimSpace(cfg.RemoteName),
		Workspace: effectiveRemoteWorkspace(cfg.RemoteWorkspace),
		Provider:  cfg.Remote.Provider,
		Region:    cfg.Remote.Region,
		Size:      cfg.Remote.Size,
		Image:     cfg.Remote.Image,
		SSHKey:    cfg.Remote.SSHKey,
		SSHUser:   cfg.Remote.SSHUser,
	}
	fs.StringVar(&opts.Name, "name", opts.Name, "remote machine name")
	fs.StringVar(&opts.Workspace, "workspace", opts.Workspace, "remote workspace name")
	fs.StringVar(&opts.Provider, "provider", opts.Provider, "remote provider")
	fs.StringVar(&opts.Region, "region", opts.Region, "DigitalOcean region")
	fs.StringVar(&opts.Size, "size", opts.Size, "DigitalOcean Droplet size")
	fs.StringVar(&opts.Image, "image", opts.Image, "DigitalOcean image")
	fs.StringVar(&opts.SSHKey, "ssh-key", opts.SSHKey, "DigitalOcean SSH key ID or fingerprint")
	fs.StringVar(&opts.SSHUser, "ssh-user", opts.SSHUser, "remote SSH user")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printRemoteUsage()
			return opts, nil, errHelp
		}
		return opts, nil, err
	}

	opts.Name = strings.ToLower(strings.TrimSpace(opts.Name))
	opts.Workspace = strings.ToLower(strings.TrimSpace(opts.Workspace))
	opts.Provider = strings.ToLower(strings.TrimSpace(opts.Provider))
	opts.Region = strings.TrimSpace(opts.Region)
	opts.Size = strings.TrimSpace(opts.Size)
	opts.Image = strings.TrimSpace(opts.Image)
	opts.SSHKey = strings.TrimSpace(opts.SSHKey)
	opts.SSHUser = strings.TrimSpace(opts.SSHUser)
	return opts, fs.Args(), nil
}

func runRemoteResume(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	ref, commandArgs, err := parseRemoteRefAndCommand("resume", args, cfg)
	if err != nil {
		return err
	}
	if len(commandArgs) == 0 {
		commandArgs = remoteDefaultCommand(cfg)
	}
	machine, workspace, err := loadRemoteTarget(ref)
	if err != nil {
		return err
	}
	return attachRemoteWorkspace(machine, workspace, remoteDefaultSessionName, commandArgs)
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

	ref, rest, err := parseRemoteRefAndCommand("sync", filtered, cfg)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected remote sync args: %v", rest)
	}
	machine, workspace, err := loadRemoteTarget(ref)
	if err != nil {
		return err
	}

	switch direction {
	case "up":
		if err := syncRemoteWorkspace(machine, &workspace, cfg, projectDir); err != nil {
			return err
		}
	case "down":
		if !force {
			return fmt.Errorf("remote sync down overwrites the local folder; pass --force to continue")
		}
		if err := syncRemoteWorkspaceDown(machine, workspace, projectDir); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown remote sync direction %q", direction)
	}

	reg, err := loadRemoteRegistry()
	if err != nil {
		return err
	}
	reg.Machines[machine.Name] = machine
	reg.Workspaces[workspace.ID] = workspace
	if err := saveRemoteRegistry(reg); err != nil {
		return err
	}
	success("Synced remote %s/%s %s", machine.Name, workspace.Name, direction)
	return nil
}

func runRemoteList(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected remote list args: %v", args)
	}
	reg, err := loadRemoteRegistry()
	if err != nil {
		return err
	}
	if len(reg.Machines) == 0 {
		if _, err := fmt.Fprintln(os.Stdout, "No remote machines registered."); err != nil {
			return err
		}
		return nil
	}

	names := make([]string, 0, len(reg.Machines))
	for name := range reg.Machines {
		names = append(names, name)
	}
	sort.Strings(names)

	if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %-16s %s\n", "NAME", "PROVIDER", "IP", "REGION", "WORKSPACE", "PROJECT"); err != nil {
		return err
	}
	for _, name := range names {
		m := reg.Machines[name]
		workspaces := workspacesForMachine(reg, m.Name)
		if len(workspaces) == 0 {
			if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %-16s %s\n", m.Name, m.Provider, m.PublicIPv4, m.Region, "-", m.ProjectPath); err != nil {
				return err
			}
			continue
		}
		for _, workspace := range workspaces {
			if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %-16s %s\n", m.Name, m.Provider, m.PublicIPv4, m.Region, workspace.Name, workspace.ProjectPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func runRemoteStatus(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	ref, rest, err := parseRemoteRefAndCommand("status", args, cfg)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected remote status args: %v", rest)
	}
	machine, workspace, err := loadRemoteTarget(ref)
	if err != nil {
		return err
	}

	providerStatus := "(not checked)"
	if machine.Provider == remoteProviderDigitalOcean && commandExists("doctl") {
		if droplet, err := getDigitalOceanDroplet(machine); err == nil {
			providerStatus = droplet.Status
			if droplet.PublicIPv4 != "" {
				machine.PublicIPv4 = droplet.PublicIPv4
				machine.UpdatedAt = time.Now().UTC()
				if reg, err := loadRemoteRegistry(); err == nil {
					reg.Machines[machine.Name] = machine
					_ = saveRemoteRegistry(reg)
				}
			}
		} else {
			providerStatus = "error: " + err.Error()
		}
	}

	fmt.Printf("%sname:%s %s\n", colorBold, colorReset, machine.Name)
	fmt.Printf("%sprovider:%s %s\n", colorBold, colorReset, machine.Provider)
	fmt.Printf("%sdroplet_id:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.DropletID))
	fmt.Printf("%sprovider_status:%s %s\n", colorBold, colorReset, providerStatus)
	fmt.Printf("%spublic_ipv4:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.PublicIPv4))
	fmt.Printf("%sssh_user:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.SSHUser))
	fmt.Printf("%ssource_path:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.SourcePath))
	fmt.Printf("%srepo:%s %s\n", colorBold, colorReset, configValueOrNotSet(workspace.RepoURL))
	fmt.Printf("%sbranch:%s %s\n", colorBold, colorReset, configValueOrNotSet(workspace.Branch))
	fmt.Printf("%sworkspace:%s %s\n", colorBold, colorReset, workspace.Name)
	fmt.Printf("%sworkspace_id:%s %s\n", colorBold, colorReset, workspace.ID)
	fmt.Printf("%sproject_path:%s %s\n", colorBold, colorReset, configValueOrNotSet(workspace.ProjectPath))
	fmt.Printf("%scompose_project:%s %s\n", colorBold, colorReset, configValueOrNotSet(workspace.ComposeProject))
	fmt.Printf("%slast_synced_at:%s %s\n", colorBold, colorReset, displayTime(workspace.LastSyncedAt))
	fmt.Printf("%sbootstrap_complete:%s %t\n", colorBold, colorReset, machine.BootstrapComplete)
	return nil
}

func runRemoteDestroy(args []string) error {
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
	if !force {
		return fmt.Errorf("yolobox remote destroy requires --force")
	}
	machine, err := loadRemoteMachine(name)
	if err != nil {
		return err
	}
	if machine.Provider == remoteProviderDigitalOcean {
		target := machine.DropletID
		if target == "" {
			target = machine.DropletName
		}
		if target != "" {
			if err := runDoctl("compute", "droplet", "delete", target, "--force"); err != nil {
				return err
			}
		}
	}
	reg, err := loadRemoteRegistry()
	if err != nil {
		return err
	}
	delete(reg.Machines, name)
	for id, workspace := range reg.Workspaces {
		if workspace.Machine == name {
			delete(reg.Workspaces, id)
		}
	}
	for id, session := range reg.Sessions {
		if session.Machine == name {
			delete(reg.Sessions, id)
		}
	}
	for id, exposure := range reg.Exposures {
		if exposure.Machine == name {
			delete(reg.Exposures, id)
		}
	}
	if err := saveRemoteRegistry(reg); err != nil {
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
	ref, rest, err := parseRemoteRefAndCommand("stop", args, cfg)
	if err != nil {
		return err
	}
	sessionName := remoteDefaultSessionName
	if len(rest) > 1 {
		return fmt.Errorf("unexpected remote stop args: %v", rest[1:])
	}
	if len(rest) == 1 {
		sessionName = strings.ToLower(strings.TrimSpace(rest[0]))
		if err := validateRemoteName(sessionName); err != nil {
			return fmt.Errorf("invalid remote session: %w", err)
		}
	}
	machine, workspace, err := loadRemoteTarget(ref)
	if err != nil {
		return err
	}
	sessionID := remoteSessionID(workspace.ID, sessionName)
	tmuxSession := remoteTmuxSessionName(workspace, sessionName)
	script := "tmux has-session -t " + shellQuote(tmuxSession) + " 2>/dev/null && tmux kill-session -t " + shellQuote(tmuxSession) + " || true"
	if err := runSSHCommand(machine, script, false, false); err != nil {
		return err
	}
	reg, err := loadRemoteRegistry()
	if err == nil {
		delete(reg.Sessions, sessionID)
		_ = saveRemoteRegistry(reg)
	}
	success("Stopped remote session %s", sessionID)
	return nil
}

func runRemoteForward(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	ref, localPort, remotePort, err := parseRemoteForwardArgs(args, cfg)
	if err != nil {
		return err
	}
	machine, workspace, err := loadRemoteTarget(ref)
	if err != nil {
		return err
	}
	exposure := remoteExposure{
		ID:         remoteExposureID(workspace.ID, "p"+strconv.Itoa(remotePort)),
		Name:       "p" + strconv.Itoa(remotePort),
		Machine:    machine.Name,
		Workspace:  workspace.ID,
		Kind:       "ssh-forward",
		LocalPort:  localPort,
		RemotePort: remotePort,
		TargetHost: "127.0.0.1",
		Visibility: "local",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	reg, err := loadRemoteRegistry()
	if err == nil {
		if existing, ok := reg.Exposures[exposure.ID]; ok {
			exposure.CreatedAt = existing.CreatedAt
		}
		reg.Exposures[exposure.ID] = exposure
		_ = saveRemoteRegistry(reg)
	}
	info("Forwarding http://127.0.0.1:%d to %s/%s port %d. Press Ctrl+C to stop.", localPort, machine.Name, workspace.Name, remotePort)
	return runSSHForward(machine, localPort, remotePort)
}

func runRemoteExpose(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	ref, port, err := parseRemoteExposeArgs(args, cfg)
	if err != nil {
		return err
	}
	if _, _, err := loadRemoteTarget(ref); err != nil {
		return err
	}
	return fmt.Errorf("managed preview exposure is not available in the open-source MVP yet; use `yolobox remote forward %s %s` for local SSH forwarding", ref.String(), port)
}

func parseRemoteNameAndCommand(command string, args []string, cfg Config) (string, []string, error) {
	if len(args) == 0 {
		name := strings.TrimSpace(cfg.RemoteName)
		if name == "" {
			return "", nil, fmt.Errorf("yolobox remote %s requires a remote name", command)
		}
		return strings.ToLower(name), nil, validateRemoteName(name)
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if err := validateRemoteName(name); err != nil {
		return "", nil, err
	}
	return name, args[1:], nil
}

func parseRemoteRefAndCommand(command string, args []string, cfg Config) (remoteRef, []string, error) {
	if len(args) == 0 {
		name := strings.TrimSpace(cfg.RemoteName)
		if name == "" {
			return remoteRef{}, nil, fmt.Errorf("yolobox remote %s requires a remote name", command)
		}
		ref, err := parseRemoteRef(name, effectiveRemoteWorkspace(cfg.RemoteWorkspace))
		return ref, nil, err
	}
	ref, err := parseRemoteRef(args[0], effectiveRemoteWorkspace(cfg.RemoteWorkspace))
	if err != nil {
		return remoteRef{}, nil, err
	}
	return ref, args[1:], nil
}

func parseRemoteRef(value string, defaultWorkspace string) (remoteRef, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return remoteRef{}, fmt.Errorf("remote name is required")
	}
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return remoteRef{}, fmt.Errorf("remote ref %q should be <machine> or <machine>/<workspace>", value)
	}
	ref := remoteRef{
		Machine:   parts[0],
		Workspace: effectiveRemoteWorkspace(defaultWorkspace),
	}
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		ref.Workspace = strings.TrimSpace(parts[1])
	}
	if err := validateRemoteName(ref.Machine); err != nil {
		return remoteRef{}, err
	}
	if err := validateRemoteName(ref.Workspace); err != nil {
		return remoteRef{}, fmt.Errorf("invalid remote workspace: %w", err)
	}
	return ref, nil
}

func (r remoteRef) String() string {
	if r.Workspace == "" || r.Workspace == remoteDefaultWorkspace {
		return r.Machine
	}
	return r.Machine + "/" + r.Workspace
}

func effectiveRemoteWorkspace(workspace string) string {
	workspace = strings.ToLower(strings.TrimSpace(workspace))
	if workspace == "" {
		return remoteDefaultWorkspace
	}
	return workspace
}

func parseRemoteForwardArgs(args []string, cfg Config) (remoteRef, int, int, error) {
	localPort := 0
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--local-port":
			if i+1 >= len(args) {
				return remoteRef{}, 0, 0, fmt.Errorf("--local-port requires a value")
			}
			port, _, err := parsePort(args[i+1])
			if err != nil {
				return remoteRef{}, 0, 0, fmt.Errorf("invalid local port: %w", err)
			}
			localPort = port
			i++
		default:
			positionals = append(positionals, args[i])
		}
	}

	if len(positionals) == 1 && strings.TrimSpace(cfg.RemoteName) != "" {
		if remotePort, _, err := parsePort(positionals[0]); err == nil {
			ref, err := parseRemoteRef(cfg.RemoteName, effectiveRemoteWorkspace(cfg.RemoteWorkspace))
			if err != nil {
				return remoteRef{}, 0, 0, err
			}
			if localPort == 0 {
				localPort = remotePort
			}
			return ref, localPort, remotePort, nil
		}
	}

	ref, rest, err := parseRemoteRefAndCommand("forward", positionals, cfg)
	if err != nil {
		return remoteRef{}, 0, 0, err
	}
	if len(rest) == 0 {
		return remoteRef{}, 0, 0, fmt.Errorf("yolobox remote forward requires a remote port")
	}
	if len(rest) > 1 {
		return remoteRef{}, 0, 0, fmt.Errorf("unexpected remote forward args: %v", rest[1:])
	}
	remotePort, _, err := parsePort(rest[0])
	if err != nil {
		return remoteRef{}, 0, 0, err
	}
	if localPort == 0 {
		localPort = remotePort
	}
	return ref, localPort, remotePort, nil
}

func parseRemoteExposeArgs(args []string, cfg Config) (remoteRef, string, error) {
	if len(args) == 1 && strings.TrimSpace(cfg.RemoteName) != "" {
		if _, _, err := parsePort(args[0]); err == nil {
			ref, err := parseRemoteRef(cfg.RemoteName, effectiveRemoteWorkspace(cfg.RemoteWorkspace))
			if err != nil {
				return remoteRef{}, "", err
			}
			return ref, args[0], nil
		}
	}
	ref, rest, err := parseRemoteRefAndCommand("expose", args, cfg)
	if err != nil {
		return remoteRef{}, "", err
	}
	if len(rest) == 0 {
		return remoteRef{}, "", fmt.Errorf("yolobox remote expose requires a port")
	}
	if len(rest) > 1 {
		return remoteRef{}, "", fmt.Errorf("unexpected remote expose args: %v", rest[1:])
	}
	if _, _, err := parsePort(rest[0]); err != nil {
		return remoteRef{}, "", err
	}
	return ref, rest[0], nil
}

func parsePort(value string) (int, string, error) {
	value = strings.TrimSpace(value)
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, value, fmt.Errorf("invalid port %q", value)
	}
	return port, value, nil
}

func ensureRemoteMachine(cfg Config, projectDir string, opts remoteProvisionOptions) (remoteMachine, error) {
	opts.Name = strings.ToLower(strings.TrimSpace(opts.Name))
	if err := validateRemoteName(opts.Name); err != nil {
		return remoteMachine{}, err
	}

	reg, err := loadRemoteRegistry()
	if err != nil {
		return remoteMachine{}, err
	}
	if machine, ok := reg.Machines[opts.Name]; ok {
		if machine.PublicIPv4 == "" && machine.Provider == remoteProviderDigitalOcean && commandExists("doctl") {
			if droplet, err := getDigitalOceanDroplet(machine); err == nil {
				machine.PublicIPv4 = droplet.PublicIPv4
				machine.UpdatedAt = time.Now().UTC()
				reg.Machines[opts.Name] = machine
				_ = saveRemoteRegistry(reg)
			}
		}
		return machine, nil
	}

	opts = applyRemoteDefaults(cfg, opts)
	if err := validateRemoteProvisionOptions(opts); err != nil {
		return remoteMachine{}, err
	}
	if err := requireRemoteClientTools("doctl", "ssh", "rsync"); err != nil {
		return remoteMachine{}, err
	}

	dropletName := "yolobox-" + opts.Name
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return remoteMachine{}, err
	}
	repo := currentGitRepo(sourcePath)
	projectPath := remoteWorkspaceProjectPath(opts.Name, effectiveRemoteWorkspace(opts.Workspace), sourcePath)
	now := time.Now().UTC()

	info("Creating DigitalOcean Droplet %s...", dropletName)
	droplet, err := createDigitalOceanDroplet(opts, dropletName)
	if err != nil {
		return remoteMachine{}, err
	}

	machine := remoteMachine{
		Name:           opts.Name,
		Provider:       opts.Provider,
		DropletID:      droplet.ID,
		DropletName:    droplet.Name,
		PublicIPv4:     droplet.PublicIPv4,
		Region:         opts.Region,
		Size:           opts.Size,
		Image:          opts.Image,
		SSHUser:        opts.SSHUser,
		SourcePath:     sourcePath,
		ProjectPath:    projectPath,
		RepoURL:        repo.URL,
		Branch:         repo.Branch,
		ComposeProject: composeProjectName(projectPath, opts.Name),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	reg.Machines[machine.Name] = machine
	if err := saveRemoteRegistry(reg); err != nil {
		return remoteMachine{}, err
	}

	if err := waitForRemoteSSH(machine, 5*time.Minute); err != nil {
		return machine, err
	}
	if err := bootstrapRemoteHost(machine); err != nil {
		return machine, err
	}
	machine.BootstrapComplete = true
	machine.UpdatedAt = time.Now().UTC()
	reg.Machines[machine.Name] = machine
	if err := saveRemoteRegistry(reg); err != nil {
		return machine, err
	}
	success("Remote %s is ready at %s", machine.Name, machine.PublicIPv4)
	return machine, nil
}

func ensureRemoteWorkspace(machine remoteMachine, cfg Config, projectDir string, workspaceName string, syncProject bool) (remoteWorkspace, error) {
	workspaceName = effectiveRemoteWorkspace(workspaceName)
	if err := validateRemoteName(workspaceName); err != nil {
		return remoteWorkspace{}, fmt.Errorf("invalid remote workspace: %w", err)
	}
	reg, err := loadRemoteRegistry()
	if err != nil {
		return remoteWorkspace{}, err
	}
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return remoteWorkspace{}, err
	}
	repo := currentGitRepo(sourcePath)
	id := remoteWorkspaceID(machine.Name, workspaceName)
	now := time.Now().UTC()
	workspace, ok := reg.Workspaces[id]
	if !ok {
		projectPath := remoteWorkspaceProjectPath(machine.Name, workspaceName, sourcePath)
		workspace = remoteWorkspace{
			ID:             id,
			Name:           workspaceName,
			Machine:        machine.Name,
			ProjectPath:    projectPath,
			ComposeProject: composeProjectName(projectPath, id),
			ContainerName:  remoteResourceName("workspace", id),
			HomeVolume:     remoteResourceName("home", id),
			CacheVolume:    remoteResourceName("cache", id),
			OutputVolume:   remoteResourceName("output", id),
			NetworkName:    remoteResourceName("net", id),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	}
	workspace.SourcePath = sourcePath
	workspace.RepoURL = repo.URL
	workspace.Branch = repo.Branch
	if workspace.ProjectPath == "" {
		workspace.ProjectPath = remoteWorkspaceProjectPath(machine.Name, workspaceName, sourcePath)
	}
	if workspace.ComposeProject == "" {
		workspace.ComposeProject = composeProjectName(workspace.ProjectPath, id)
	}
	if workspace.ContainerName == "" {
		workspace.ContainerName = remoteResourceName("workspace", id)
	}
	if workspace.HomeVolume == "" {
		workspace.HomeVolume = remoteResourceName("home", id)
	}
	if workspace.CacheVolume == "" {
		workspace.CacheVolume = remoteResourceName("cache", id)
	}
	if workspace.OutputVolume == "" {
		workspace.OutputVolume = remoteResourceName("output", id)
	}
	if workspace.NetworkName == "" {
		workspace.NetworkName = remoteResourceName("net", id)
	}
	workspace.UpdatedAt = now

	if syncProject {
		if err := syncRemoteWorkspace(machine, &workspace, cfg, projectDir); err != nil {
			reg.Workspaces[id] = workspace
			reg.Machines[machine.Name] = machine
			_ = saveRemoteRegistry(reg)
			return workspace, err
		}
	}
	reg.Workspaces[id] = workspace
	machine.SourcePath = workspace.SourcePath
	machine.ProjectPath = workspace.ProjectPath
	machine.RepoURL = workspace.RepoURL
	machine.Branch = workspace.Branch
	machine.ComposeProject = workspace.ComposeProject
	machine.LastSyncedAt = workspace.LastSyncedAt
	machine.UpdatedAt = time.Now().UTC()
	reg.Machines[machine.Name] = machine
	if err := saveRemoteRegistry(reg); err != nil {
		return workspace, err
	}
	return workspace, nil
}

func applyRemoteDefaults(cfg Config, opts remoteProvisionOptions) remoteProvisionOptions {
	if opts.Provider == "" {
		opts.Provider = cfg.Remote.Provider
	}
	if opts.Region == "" {
		opts.Region = cfg.Remote.Region
	}
	if opts.Size == "" {
		opts.Size = cfg.Remote.Size
	}
	if opts.Image == "" {
		opts.Image = cfg.Remote.Image
	}
	if opts.SSHKey == "" {
		opts.SSHKey = cfg.Remote.SSHKey
	}
	if opts.SSHUser == "" {
		opts.SSHUser = cfg.Remote.SSHUser
	}
	return opts
}

func validateRemoteProvisionOptions(opts remoteProvisionOptions) error {
	if err := validateRemoteName(opts.Name); err != nil {
		return err
	}
	if opts.Provider == "" {
		return fmt.Errorf("remote provider is required")
	}
	if opts.Provider != remoteProviderDigitalOcean {
		return fmt.Errorf("remote provider %q is not supported yet; MVP supports digitalocean", opts.Provider)
	}
	if opts.Region == "" {
		return fmt.Errorf("remote region is required")
	}
	if opts.Size == "" {
		return fmt.Errorf("remote size is required")
	}
	if opts.Image == "" {
		return fmt.Errorf("remote image is required")
	}
	if opts.SSHKey == "" {
		return fmt.Errorf("remote mode requires a DigitalOcean SSH key ID or fingerprint via --ssh-key or remote.ssh_key")
	}
	if opts.SSHUser == "" {
		return fmt.Errorf("remote ssh user is required")
	}
	return nil
}

func validateRemoteDefaults(cfg Config) error {
	if strings.TrimSpace(cfg.RemoteName) == "" {
		return fmt.Errorf(`mode = "remote" requires remote_name`)
	}
	if err := validateRemoteName(cfg.RemoteName); err != nil {
		return err
	}
	if err := validateRemoteName(effectiveRemoteWorkspace(cfg.RemoteWorkspace)); err != nil {
		return fmt.Errorf("invalid remote_workspace: %w", err)
	}
	opts := applyRemoteDefaults(cfg, remoteProvisionOptions{Name: cfg.RemoteName})
	return validateRemoteProvisionOptions(opts)
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

func createDigitalOceanDroplet(opts remoteProvisionOptions, dropletName string) (dropletInfo, error) {
	if !commandExists("doctl") {
		return dropletInfo{}, fmt.Errorf("doctl is required for remote mode; install and authenticate DigitalOcean CLI first")
	}
	userDataPath, err := writeRemoteUserData()
	if err != nil {
		return dropletInfo{}, err
	}
	defer func() {
		_ = os.Remove(userDataPath)
	}()

	args := []string{
		"compute", "droplet", "create", dropletName,
		"--size", opts.Size,
		"--image", opts.Image,
		"--region", opts.Region,
		"--ssh-keys", opts.SSHKey,
		"--tag-names", "yolobox,yolobox-remote",
		"--user-data-file", userDataPath,
		"--wait",
		"--format", "ID,Name,PublicIPv4,Status",
		"--no-header",
	}
	output, err := doctlOutput(args...)
	if err != nil {
		return dropletInfo{}, err
	}
	droplet, err := parseDropletInfo(output)
	if err != nil {
		return dropletInfo{}, err
	}
	if droplet.PublicIPv4 == "" {
		refreshed, err := getDigitalOceanDroplet(remoteMachine{DropletID: droplet.ID, DropletName: droplet.Name})
		if err == nil {
			droplet = refreshed
		}
	}
	return droplet, nil
}

func writeRemoteUserData() (string, error) {
	file, err := os.CreateTemp("", "yolobox-remote-user-data-*.sh")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	script := `#!/bin/bash
set -eux
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl git rsync tmux
if ! command -v docker >/dev/null 2>&1; then
  curl -fsSL https://get.docker.com | sh
fi
if ! command -v yolobox >/dev/null 2>&1; then
  curl -fsSL https://raw.githubusercontent.com/finbarr/yolobox/master/install.sh | bash
fi
docker pull ghcr.io/finbarr/yolobox:latest || true
mkdir -p /root/yolobox-workspaces /root/yolobox-projects
`
	if _, err := file.WriteString(script); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func getDigitalOceanDroplet(machine remoteMachine) (dropletInfo, error) {
	target := machine.DropletID
	if target == "" {
		target = machine.DropletName
	}
	if target == "" {
		return dropletInfo{}, fmt.Errorf("missing Droplet ID/name")
	}
	output, err := doctlOutput("compute", "droplet", "get", target, "--format", "ID,Name,PublicIPv4,Status", "--no-header")
	if err != nil {
		return dropletInfo{}, err
	}
	return parseDropletInfo(output)
}

func parseDropletInfo(output string) (dropletInfo, error) {
	line := strings.TrimSpace(output)
	if line == "" {
		return dropletInfo{}, fmt.Errorf("empty doctl Droplet output")
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return dropletInfo{}, fmt.Errorf("unexpected doctl Droplet output: %q", line)
	}
	info := dropletInfo{
		ID:   fields[0],
		Name: fields[1],
	}
	if len(fields) == 3 {
		info.Status = fields[2]
		return info, nil
	}
	info.PublicIPv4 = fields[2]
	info.Status = fields[3]
	return info, nil
}

func doctlOutput(args ...string) (string, error) {
	cmd := exec.Command("doctl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("doctl %s failed: %s", strings.Join(args, " "), detail)
	}
	return string(output), nil
}

func runDoctl(args ...string) error {
	cmd := exec.Command("doctl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
mkdir -p /root/yolobox-workspaces /root/yolobox-projects
`
	return runRemoteScript(machine, script, false)
}

func syncRemoteProject(machine *remoteMachine, cfg Config, projectDir string) error {
	workspace, err := ensureRemoteWorkspace(*machine, cfg, projectDir, remoteDefaultWorkspace, false)
	if err != nil {
		return err
	}
	if err := syncRemoteWorkspace(*machine, &workspace, cfg, projectDir); err != nil {
		return err
	}
	machine.SourcePath = workspace.SourcePath
	machine.ProjectPath = workspace.ProjectPath
	machine.RepoURL = workspace.RepoURL
	machine.Branch = workspace.Branch
	machine.ComposeProject = workspace.ComposeProject
	machine.LastSyncedAt = workspace.LastSyncedAt
	machine.UpdatedAt = workspace.UpdatedAt
	return nil
}

func syncRemoteWorkspace(machine remoteMachine, workspace *remoteWorkspace, cfg Config, projectDir string) error {
	if err := requireRemoteClientTools("ssh", "rsync"); err != nil {
		return err
	}
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return err
	}
	repo := currentGitRepo(sourcePath)
	workspace.RepoURL = repo.URL
	workspace.Branch = repo.Branch
	workspace.SourcePath = sourcePath
	if workspace.ProjectPath == "" {
		workspace.ProjectPath = remoteWorkspaceProjectPath(machine.Name, workspace.Name, sourcePath)
	}
	if workspace.ComposeProject == "" {
		workspace.ComposeProject = composeProjectName(workspace.ProjectPath, workspace.ID)
	}

	info("Copying %s to %s/%s:%s...", sourcePath, machine.Name, workspace.Name, workspace.ProjectPath)
	if err := ensureRemoteWorkspacePath(machine, *workspace); err != nil {
		return err
	}
	if err := rsyncPathToRemote(machine, workspace.ProjectPath, sourcePath); err != nil {
		return err
	}
	if len(cfg.Remote.Setup) > 0 {
		if err := runRemoteScript(machine, buildRemoteSetupScript(*workspace, cfg.Remote.Setup), false); err != nil {
			return err
		}
	}
	workspace.LastSyncedAt = time.Now().UTC()
	workspace.UpdatedAt = workspace.LastSyncedAt
	return nil
}

func syncRemoteWorkspaceDown(machine remoteMachine, workspace remoteWorkspace, projectDir string) error {
	if err := requireRemoteClientTools("ssh", "rsync"); err != nil {
		return err
	}
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return err
	}
	info("Copying %s/%s:%s back to %s...", machine.Name, workspace.Name, workspace.ProjectPath, sourcePath)
	if err := rsyncPathFromRemote(machine, workspace.ProjectPath, sourcePath); err != nil {
		return err
	}
	return nil
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

func ensureRemoteProjectPath(machine remoteMachine) error {
	workspace := defaultWorkspaceFromMachine(machine)
	return ensureRemoteWorkspacePath(machine, workspace)
}

func ensureRemoteWorkspacePath(machine remoteMachine, workspace remoteWorkspace) error {
	parent := path.Dir(workspace.ProjectPath)
	script := "set -euo pipefail\n" +
		"if ! command -v rsync >/dev/null 2>&1; then\n" +
		"  apt-get update\n" +
		"  apt-get install -y rsync\n" +
		"fi\n" +
		"mkdir -p " + shellQuote(parent) + " " + shellQuote(workspace.ProjectPath) + "\n"
	return runRemoteScript(machine, script, false)
}

func rsyncProjectToRemote(machine remoteMachine, sourcePath string) error {
	projectPath := machine.ProjectPath
	if projectPath == "" {
		projectPath = defaultWorkspaceFromMachine(machine).ProjectPath
	}
	return rsyncPathToRemote(machine, projectPath, sourcePath)
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

func buildRemoteSetupScript(workspace remoteWorkspace, setup []string) string {
	var b strings.Builder
	b.WriteString("set -euo pipefail\n")
	b.WriteString("cd " + shellQuote(workspace.ProjectPath) + "\n")
	for _, command := range setup {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		b.WriteString(command + "\n")
	}
	return b.String()
}

func attachRemoteMachine(machine remoteMachine, commandArgs []string) error {
	return attachRemoteWorkspace(machine, defaultWorkspaceFromMachine(machine), remoteDefaultSessionName, commandArgs)
}

func attachRemoteWorkspace(machine remoteMachine, workspace remoteWorkspace, sessionName string, commandArgs []string) error {
	if machine.PublicIPv4 == "" {
		return fmt.Errorf("remote %s has no public IPv4 in registry; run yolobox remote status %s", machine.Name, machine.Name)
	}
	if len(commandArgs) == 0 {
		commandArgs = []string{"shell"}
	}
	tmuxSession := remoteTmuxSessionName(workspace, sessionName)
	remoteCommand := remoteTmuxCommand(workspace, tmuxSession, append([]string{"yolobox"}, commandArgs...))
	info("Attaching to remote %s/%s (%s)", machine.Name, workspace.Name, machine.PublicIPv4)
	if err := runSSHCommand(machine, remoteCommand, true, shouldForwardSSHAgent(machine.RepoURL)); err != nil {
		return err
	}

	reg, err := loadRemoteRegistry()
	if err == nil {
		now := time.Now().UTC()
		sessionID := remoteSessionID(workspace.ID, sessionName)
		session := remoteSession{
			ID:          sessionID,
			Name:        sessionName,
			Machine:     machine.Name,
			Workspace:   workspace.ID,
			TmuxSession: tmuxSession,
			LastCommand: commandArgs,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if existing, ok := reg.Sessions[sessionID]; ok {
			session.CreatedAt = existing.CreatedAt
		}
		reg.Sessions[sessionID] = session
		machine.LastCommand = commandArgs
		machine.UpdatedAt = now
		reg.Machines[machine.Name] = machine
		workspace.UpdatedAt = now
		reg.Workspaces[workspace.ID] = workspace
		_ = saveRemoteRegistry(reg)
	}
	return nil
}

func remoteDefaultCommand(cfg Config) []string {
	if harness := normalizeDefaultHarness(cfg.DefaultHarness); harness != "" {
		return []string{harness}
	}
	return []string{"shell"}
}

func remoteTmuxSessionName(workspace remoteWorkspace, sessionName string) string {
	return remoteDefaultSession + "-" + sanitizeRemoteResourceID(workspace.ID) + "-" + sessionName
}

func remoteTmuxCommand(workspace remoteWorkspace, sessionName string, command []string) string {
	return "tmux new-session -A -s " + shellQuote(sessionName) + " -c " + shellQuote(workspace.ProjectPath) + " " + shellQuote(shellJoin(command))
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

func loadRemoteTarget(ref remoteRef) (remoteMachine, remoteWorkspace, error) {
	reg, err := loadRemoteRegistry()
	if err != nil {
		return remoteMachine{}, remoteWorkspace{}, err
	}
	machine, ok := reg.Machines[ref.Machine]
	if !ok {
		return remoteMachine{}, remoteWorkspace{}, fmt.Errorf("remote %s is not registered", ref.Machine)
	}
	workspaceID := remoteWorkspaceID(ref.Machine, ref.Workspace)
	workspace, ok := reg.Workspaces[workspaceID]
	if !ok {
		if ref.Workspace == remoteDefaultWorkspace && machine.ProjectPath != "" {
			workspace = defaultWorkspaceFromMachine(machine)
		} else {
			return remoteMachine{}, remoteWorkspace{}, fmt.Errorf("remote workspace %s is not registered", workspaceID)
		}
	}
	return machine, workspace, nil
}

func workspacesForMachine(reg remoteRegistry, machineName string) []remoteWorkspace {
	workspaces := make([]remoteWorkspace, 0)
	for _, workspace := range reg.Workspaces {
		if workspace.Machine == machineName {
			workspaces = append(workspaces, workspace)
		}
	}
	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].Name < workspaces[j].Name
	})
	return workspaces
}

func defaultWorkspaceFromMachine(machine remoteMachine) remoteWorkspace {
	id := remoteWorkspaceID(machine.Name, remoteDefaultWorkspace)
	projectPath := machine.ProjectPath
	if projectPath == "" {
		projectPath = "/root/yolobox-workspaces/" + sanitizeRemoteResourceID(id) + "/project"
	}
	workspace := remoteWorkspace{
		ID:             id,
		Name:           remoteDefaultWorkspace,
		Machine:        machine.Name,
		SourcePath:     machine.SourcePath,
		ProjectPath:    projectPath,
		RepoURL:        machine.RepoURL,
		Branch:         machine.Branch,
		ComposeProject: machine.ComposeProject,
		CreatedAt:      machine.CreatedAt,
		UpdatedAt:      machine.UpdatedAt,
		LastSyncedAt:   machine.LastSyncedAt,
	}
	if workspace.ComposeProject == "" {
		workspace.ComposeProject = composeProjectName(workspace.ProjectPath, id)
	}
	workspace.ContainerName = remoteResourceName("workspace", id)
	workspace.HomeVolume = remoteResourceName("home", id)
	workspace.CacheVolume = remoteResourceName("cache", id)
	workspace.OutputVolume = remoteResourceName("output", id)
	workspace.NetworkName = remoteResourceName("net", id)
	return workspace
}

func remoteWorkspaceID(machineName string, workspaceName string) string {
	return strings.ToLower(strings.TrimSpace(machineName)) + "/" + effectiveRemoteWorkspace(workspaceName)
}

func remoteSessionID(workspaceID string, sessionName string) string {
	return workspaceID + "/" + strings.ToLower(strings.TrimSpace(sessionName))
}

func remoteExposureID(workspaceID string, name string) string {
	return workspaceID + "/" + strings.ToLower(strings.TrimSpace(name))
}

func remoteWorkspaceProjectPath(machineName string, workspaceName string, sourcePath string) string {
	workspaceID := remoteWorkspaceID(machineName, workspaceName)
	return "/root/yolobox-workspaces/" + sanitizeRemoteResourceID(workspaceID) + "/" + slugify(filepath.Base(sourcePath), "project")
}

func remoteResourceName(kind string, id string) string {
	name := "yolobox-" + kind + "-" + sanitizeRemoteResourceID(id)
	if len(name) > 63 {
		return name[:63]
	}
	return name
}

func sanitizeRemoteResourceID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	var b strings.Builder
	lastDash := false
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	value := strings.Trim(b.String(), "-")
	if value == "" {
		return "default"
	}
	return value
}

func displayTime(value time.Time) string {
	if value.IsZero() {
		return "(not set)"
	}
	return value.Format(time.RFC3339)
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
		case "doctl":
			return fmt.Errorf("doctl is required for remote mode; install and authenticate the DigitalOcean CLI first")
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

func remoteRegistryPath() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "yolobox", "remotes.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "yolobox", "remotes.json"), nil
}

func loadRemoteMachine(name string) (remoteMachine, error) {
	reg, err := loadRemoteRegistry()
	if err != nil {
		return remoteMachine{}, err
	}
	machine, ok := reg.Machines[name]
	if !ok {
		return remoteMachine{}, fmt.Errorf("remote %s is not registered", name)
	}
	return machine, nil
}

func remoteMachineExists(name string) bool {
	reg, err := loadRemoteRegistry()
	if err != nil {
		return false
	}
	_, ok := reg.Machines[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func loadRemoteRegistry() (remoteRegistry, error) {
	path, err := remoteRegistryPath()
	if err != nil {
		return remoteRegistry{}, err
	}
	reg := remoteRegistry{
		Version:    remoteRegistryVersion,
		Machines:   map[string]remoteMachine{},
		Workspaces: map[string]remoteWorkspace{},
		Sessions:   map[string]remoteSession{},
		Exposures:  map[string]remoteExposure{},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return remoteRegistry{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return reg, nil
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return remoteRegistry{}, fmt.Errorf("failed to read remote registry: %w", err)
	}
	if reg.Machines == nil {
		reg.Machines = map[string]remoteMachine{}
	}
	if reg.Workspaces == nil {
		reg.Workspaces = map[string]remoteWorkspace{}
	}
	if reg.Sessions == nil {
		reg.Sessions = map[string]remoteSession{}
	}
	if reg.Exposures == nil {
		reg.Exposures = map[string]remoteExposure{}
	}
	if reg.Version == 0 {
		reg.Version = remoteRegistryVersion
	}
	migrateRemoteRegistry(&reg)
	return reg, nil
}

func saveRemoteRegistry(reg remoteRegistry) error {
	path, err := remoteRegistryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create remote registry directory: %w", err)
	}
	reg.Version = remoteRegistryVersion
	if reg.Machines == nil {
		reg.Machines = map[string]remoteMachine{}
	}
	if reg.Workspaces == nil {
		reg.Workspaces = map[string]remoteWorkspace{}
	}
	if reg.Sessions == nil {
		reg.Sessions = map[string]remoteSession{}
	}
	if reg.Exposures == nil {
		reg.Exposures = map[string]remoteExposure{}
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write remote registry: %w", err)
	}
	return nil
}

func migrateRemoteRegistry(reg *remoteRegistry) {
	for name, machine := range reg.Machines {
		if machine.ProjectPath == "" {
			continue
		}
		id := remoteWorkspaceID(name, remoteDefaultWorkspace)
		if _, ok := reg.Workspaces[id]; ok {
			continue
		}
		reg.Workspaces[id] = defaultWorkspaceFromMachine(machine)
	}
}
