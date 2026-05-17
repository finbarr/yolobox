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
	"strings"
	"time"
)

const (
	remoteProviderDigitalOcean = "digitalocean"
	remoteRegistryVersion      = 1
	remoteDefaultSession       = "yolobox"
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

type remoteRegistry struct {
	Version  int                      `json:"version"`
	Machines map[string]remoteMachine `json:"machines"`
}

type remoteProvisionOptions struct {
	Name     string
	Provider string
	Region   string
	Size     string
	Image    string
	SSHKey   string
	SSHUser  string
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
	case "resume":
		return runRemoteResume(args[1:], projectDir)
	case "sync":
		return runRemoteSync(args[1:], projectDir)
	case "list":
		return runRemoteList(args[1:])
	case "status":
		return runRemoteStatus(args[1:])
	case "destroy":
		return runRemoteDestroy(args[1:])
	default:
		return runRemoteCreate(args, projectDir)
	}
}

func printRemoteUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox remote --name <env> [cmd...]       Create or reuse a named remote machine")
	fmt.Fprintln(os.Stderr, "  yolobox remote resume <env> [cmd...]       Reattach to a remote tmux session")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync <env>                  Copy the current folder to the remote host")
	fmt.Fprintln(os.Stderr, "  yolobox remote list                        List locally registered remote machines")
	fmt.Fprintln(os.Stderr, "  yolobox remote status <env>                Show local and provider state")
	fmt.Fprintln(os.Stderr, "  yolobox remote destroy <env> --force       Delete the Droplet and local registry entry")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "OPTIONS:")
	fmt.Fprintln(os.Stderr, "  --name <env>       Remote machine name")
	fmt.Fprintln(os.Stderr, "  --provider <name>  Remote provider (MVP: digitalocean)")
	fmt.Fprintln(os.Stderr, "  --region <slug>    DigitalOcean region")
	fmt.Fprintln(os.Stderr, "  --size <slug>      DigitalOcean Droplet size")
	fmt.Fprintln(os.Stderr, "  --image <slug>     DigitalOcean image")
	fmt.Fprintln(os.Stderr, "  --ssh-key <id>     DigitalOcean SSH key ID or fingerprint")
	fmt.Fprintln(os.Stderr, "  --ssh-user <user>  SSH user for the remote host")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "EXAMPLES:")
	fmt.Fprintln(os.Stderr, "  yolobox remote --name foo codex")
	fmt.Fprintln(os.Stderr, "  yolobox remote resume foo codex")
	fmt.Fprintln(os.Stderr, "  yolobox remote sync foo")
}

func runRemoteDefault(cfg Config, projectDir string) error {
	name := strings.TrimSpace(cfg.RemoteName)
	if name == "" {
		return fmt.Errorf(`mode = "remote" requires remote_name in config`)
	}
	if err := validateRemoteName(name); err != nil {
		return err
	}
	machine, err := ensureRemoteMachine(cfg, projectDir, remoteProvisionOptions{Name: name})
	if err != nil {
		return err
	}
	return attachRemoteMachine(machine, remoteDefaultCommand(cfg))
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
	if len(commandArgs) == 0 {
		commandArgs = remoteDefaultCommand(cfg)
	}

	machine, err := ensureRemoteMachine(cfg, projectDir, opts)
	if err != nil {
		return err
	}
	return attachRemoteMachine(machine, commandArgs)
}

func parseRemoteCreateFlags(args []string, cfg Config) (remoteProvisionOptions, []string, error) {
	fs := flag.NewFlagSet("remote", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printRemoteUsage

	opts := remoteProvisionOptions{
		Name:     strings.TrimSpace(cfg.RemoteName),
		Provider: cfg.Remote.Provider,
		Region:   cfg.Remote.Region,
		Size:     cfg.Remote.Size,
		Image:    cfg.Remote.Image,
		SSHKey:   cfg.Remote.SSHKey,
		SSHUser:  cfg.Remote.SSHUser,
	}
	fs.StringVar(&opts.Name, "name", opts.Name, "remote machine name")
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
	name, commandArgs, err := parseRemoteNameAndCommand("resume", args, cfg)
	if err != nil {
		return err
	}
	if len(commandArgs) == 0 {
		commandArgs = remoteDefaultCommand(cfg)
	}
	machine, err := loadRemoteMachine(name)
	if err != nil {
		return err
	}
	return attachRemoteMachine(machine, commandArgs)
}

func runRemoteSync(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	name, rest, err := parseRemoteNameAndCommand("sync", args, cfg)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected remote sync args: %v", rest)
	}
	machine, err := loadRemoteMachine(name)
	if err != nil {
		return err
	}
	if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
		return err
	}
	reg, err := loadRemoteRegistry()
	if err != nil {
		return err
	}
	reg.Machines[machine.Name] = machine
	if err := saveRemoteRegistry(reg); err != nil {
		return err
	}
	success("Synced remote %s", machine.Name)
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

	if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %s\n", "NAME", "PROVIDER", "IP", "REGION", "PROJECT"); err != nil {
		return err
	}
	for _, name := range names {
		m := reg.Machines[name]
		if _, err := fmt.Fprintf(os.Stdout, "%-18s %-14s %-15s %-12s %s\n", m.Name, m.Provider, m.PublicIPv4, m.Region, m.ProjectPath); err != nil {
			return err
		}
	}
	return nil
}

func runRemoteStatus(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("yolobox remote status requires a remote name")
	}
	if len(args) > 1 {
		return fmt.Errorf("unexpected remote status args: %v", args[1:])
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	machine, err := loadRemoteMachine(name)
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
	fmt.Printf("%srepo:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.RepoURL))
	fmt.Printf("%sbranch:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.Branch))
	fmt.Printf("%sproject_path:%s %s\n", colorBold, colorReset, configValueOrNotSet(machine.ProjectPath))
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
	if err := saveRemoteRegistry(reg); err != nil {
		return err
	}
	success("Destroyed remote %s", name)
	return nil
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
	projectPath := "/root/yolobox-projects/" + slugify(filepath.Base(sourcePath), "project")
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
	if err := syncRemoteProject(&machine, cfg, projectDir); err != nil {
		reg.Machines[machine.Name] = machine
		_ = saveRemoteRegistry(reg)
		return machine, err
	}
	reg.Machines[machine.Name] = machine
	if err := saveRemoteRegistry(reg); err != nil {
		return machine, err
	}
	success("Remote %s is ready at %s", machine.Name, machine.PublicIPv4)
	return machine, nil
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
mkdir -p /root/yolobox-projects
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
mkdir -p /root/yolobox-projects
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
	machine.RepoURL = repo.URL
	machine.Branch = repo.Branch
	machine.SourcePath = sourcePath
	if machine.ProjectPath == "" {
		machine.ProjectPath = "/root/yolobox-projects/" + slugify(filepath.Base(sourcePath), "project")
	}
	if machine.ComposeProject == "" {
		machine.ComposeProject = composeProjectName(machine.ProjectPath, machine.Name)
	}

	info("Copying %s to %s:%s...", sourcePath, machine.Name, machine.ProjectPath)
	if err := ensureRemoteProjectPath(*machine); err != nil {
		return err
	}
	if err := rsyncProjectToRemote(*machine, sourcePath); err != nil {
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
	parent := path.Dir(machine.ProjectPath)
	script := "set -euo pipefail\n" +
		"if ! command -v rsync >/dev/null 2>&1; then\n" +
		"  apt-get update\n" +
		"  apt-get install -y rsync\n" +
		"fi\n" +
		"mkdir -p " + shellQuote(parent) + " " + shellQuote(machine.ProjectPath) + "\n"
	return runRemoteScript(machine, script, false)
}

func rsyncProjectToRemote(machine remoteMachine, sourcePath string) error {
	source := sourcePath + string(os.PathSeparator)
	target := machine.sshTarget() + ":" + machine.ProjectPath + "/"
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

func attachRemoteMachine(machine remoteMachine, commandArgs []string) error {
	if machine.PublicIPv4 == "" {
		return fmt.Errorf("remote %s has no public IPv4 in registry; run yolobox remote status %s", machine.Name, machine.Name)
	}
	if len(commandArgs) == 0 {
		commandArgs = []string{"shell"}
	}
	sessionName := remoteTmuxSessionName(machine.Name)
	remoteCommand := remoteTmuxCommand(machine, sessionName, append([]string{"yolobox"}, commandArgs...))
	info("Attaching to remote %s (%s)", machine.Name, machine.PublicIPv4)
	if err := runSSHCommand(machine, remoteCommand, true, shouldForwardSSHAgent(machine.RepoURL)); err != nil {
		return err
	}

	reg, err := loadRemoteRegistry()
	if err == nil {
		machine.LastCommand = commandArgs
		machine.UpdatedAt = time.Now().UTC()
		reg.Machines[machine.Name] = machine
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

func remoteTmuxSessionName(name string) string {
	return remoteDefaultSession + "-" + name
}

func remoteTmuxCommand(machine remoteMachine, sessionName string, command []string) string {
	return "tmux new-session -A -s " + shellQuote(sessionName) + " -c " + shellQuote(machine.ProjectPath) + " " + shellQuote(shellJoin(command))
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

func loadRemoteRegistry() (remoteRegistry, error) {
	path, err := remoteRegistryPath()
	if err != nil {
		return remoteRegistry{}, err
	}
	reg := remoteRegistry{
		Version:  remoteRegistryVersion,
		Machines: map[string]remoteMachine{},
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
	if reg.Version == 0 {
		reg.Version = remoteRegistryVersion
	}
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
