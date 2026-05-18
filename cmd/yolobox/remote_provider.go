package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	remoteProviderEnv = "YOLOBOX_REMOTE_PROVIDER"
)

type remoteMachineProvider interface {
	Name() string
	EnsureMachine(context.Context, remoteMachineProviderRequest) (remoteMachine, string, error)
	GetMachine(context.Context, remoteMachine) (remoteMachine, string, error)
	ReleaseMachine(context.Context, remoteMachine) error
}

type remoteMachineProviderRequest struct {
	Name      string
	Workspace string
	SSHUser   string
	RepoURL   string
	Branch    string
}

func remoteProviderName(cfg Config, opts remoteProvisionOptions) string {
	if opts.Provider != "" {
		return strings.ToLower(strings.TrimSpace(opts.Provider))
	}
	if provider := strings.TrimSpace(cfg.Remote.Provider); provider != "" {
		return strings.ToLower(provider)
	}
	return strings.ToLower(strings.TrimSpace(os.Getenv(remoteProviderEnv)))
}

func remoteDirectProviderConfigured(cfg Config, opts remoteProvisionOptions) bool {
	return remoteProviderName(cfg, opts) != ""
}

func newRemoteMachineProvider(cfg Config, opts remoteProvisionOptions) (remoteMachineProvider, error) {
	provider := remoteProviderName(cfg, opts)
	switch provider {
	case remoteProviderDigitalOcean:
		return newDigitalOceanProvider(cfg, opts)
	case "":
		return nil, fmt.Errorf("remote provider is not configured")
	default:
		return nil, fmt.Errorf("unknown remote provider %q", provider)
	}
}

func ensureRemoteProviderMachine(cfg Config, projectDir string, opts remoteProvisionOptions) (remoteMachine, error) {
	if err := requireRemoteClientTools("ssh", "rsync"); err != nil {
		return remoteMachine{}, err
	}
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return remoteMachine{}, err
	}
	repo := currentGitRepo(sourcePath)
	provider, err := newRemoteMachineProvider(cfg, opts)
	if err != nil {
		return remoteMachine{}, err
	}
	machine, _, err := provider.EnsureMachine(context.Background(), remoteMachineProviderRequest{
		Name:      opts.Name,
		Workspace: effectiveRemoteWorkspace(opts.Workspace),
		SSHUser:   firstNonEmpty(opts.SSHUser, cfg.Remote.SSHUser, "root"),
		RepoURL:   repo.URL,
		Branch:    repo.Branch,
	})
	if err != nil {
		return remoteMachine{}, err
	}
	machine.Name = opts.Name
	machine.Provider = provider.Name()
	machine.SourcePath = sourcePath
	machine.RepoURL = repo.URL
	machine.Branch = repo.Branch
	if machine.SSHUser == "" {
		machine.SSHUser = firstNonEmpty(opts.SSHUser, cfg.Remote.SSHUser, "root")
	}
	if machine.CreatedAt.IsZero() {
		machine.CreatedAt = time.Now().UTC()
	}
	machine.UpdatedAt = time.Now().UTC()
	if machine.PublicIPv4 == "" {
		return remoteMachine{}, fmt.Errorf("remote provider %s returned no SSH host for %s", provider.Name(), opts.Name)
	}
	return machine, nil
}

func getRemoteProviderMachine(cfg Config, machine remoteMachine) (remoteMachine, string, error) {
	provider, err := newRemoteMachineProvider(cfg, remoteProvisionOptions{Provider: machine.Provider})
	if err != nil {
		return remoteMachine{}, "", err
	}
	return provider.GetMachine(context.Background(), machine)
}

func releaseRemoteProviderMachine(cfg Config, machine remoteMachine) error {
	provider, err := newRemoteMachineProvider(cfg, remoteProvisionOptions{Provider: machine.Provider})
	if err != nil {
		return err
	}
	return provider.ReleaseMachine(context.Background(), machine)
}
