package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	remoteProviderBackend = "backend"
	remoteBackendTokenEnv = "YOLOBOX_REMOTE_TOKEN"
)

type remoteBackendEnsureRequest struct {
	Name      string `json:"name"`
	Workspace string `json:"workspace,omitempty"`
	SSHUser   string `json:"ssh_user,omitempty"`
	RepoURL   string `json:"repo_url,omitempty"`
	Branch    string `json:"branch,omitempty"`
}

type remoteBackendMachineResponse struct {
	Machine remoteMachine `json:"machine"`
	Status  string        `json:"status,omitempty"`
}

func ensureRemoteBackendMachine(cfg Config, projectDir string, opts remoteProvisionOptions) (remoteMachine, error) {
	if err := requireRemoteClientTools("ssh", "rsync"); err != nil {
		return remoteMachine{}, err
	}
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return remoteMachine{}, err
	}
	repo := currentGitRepo(sourcePath)
	req := remoteBackendEnsureRequest{
		Name:      opts.Name,
		Workspace: effectiveRemoteWorkspace(opts.Workspace),
		SSHUser:   firstNonEmpty(opts.SSHUser, cfg.Remote.SSHUser, "root"),
		RepoURL:   repo.URL,
		Branch:    repo.Branch,
	}
	var response remoteBackendMachineResponse
	if err := remoteBackendRequest(cfg, http.MethodPost, "/v1/machines/ensure", req, &response); err != nil {
		return remoteMachine{}, err
	}
	machine := response.Machine
	machine.Name = opts.Name
	machine.Provider = remoteProviderBackend
	machine.BackendURL = remoteBackendURL(cfg, machine)
	if machine.SSHUser == "" {
		machine.SSHUser = firstNonEmpty(opts.SSHUser, cfg.Remote.SSHUser, "root")
	}
	machine.SourcePath = sourcePath
	machine.RepoURL = repo.URL
	machine.Branch = repo.Branch
	if machine.CreatedAt.IsZero() {
		machine.CreatedAt = time.Now().UTC()
	}
	machine.UpdatedAt = time.Now().UTC()
	if machine.PublicIPv4 == "" {
		return remoteMachine{}, fmt.Errorf("remote backend returned no SSH host for %s", opts.Name)
	}
	return machine, nil
}

func releaseRemoteBackendMachine(cfg Config, machine remoteMachine) error {
	if cfg.Remote.BackendURL == "" {
		cfg.Remote.BackendURL = machine.BackendURL
	}
	return remoteBackendMachineRequest(cfg, machine, http.MethodDelete, "/v1/machines/"+machine.Name, nil, nil)
}

func getRemoteBackendMachine(cfg Config, machine remoteMachine) (remoteMachine, string, error) {
	if cfg.Remote.BackendURL == "" {
		cfg.Remote.BackendURL = machine.BackendURL
	}
	var response remoteBackendMachineResponse
	if err := remoteBackendMachineRequest(cfg, machine, http.MethodGet, "/v1/machines/"+machine.Name, nil, &response); err != nil {
		return remoteMachine{}, "", err
	}
	if response.Machine.BackendURL == "" {
		response.Machine.BackendURL = remoteBackendURL(cfg, machine)
	}
	return response.Machine, response.Status, nil
}

func remoteBackendRequest(cfg Config, method string, endpoint string, body any, out any) error {
	return remoteBackendMachineRequest(cfg, remoteMachine{}, method, endpoint, body, out)
}

func remoteBackendMachineRequest(cfg Config, machine remoteMachine, method string, endpoint string, body any, out any) error {
	baseURL := remoteBackendURL(cfg, machine)
	if baseURL == "" {
		return fmt.Errorf("remote backend URL is not configured")
	}
	token := remoteBackendToken(cfg)
	if token == "" {
		return fmt.Errorf("remote backend token is not configured; set remote.backend_token or %s", remoteBackendTokenEnv)
	}
	var requestBody *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(data)
	} else {
		requestBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, strings.TrimRight(baseURL, "/")+endpoint, requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		detail := strings.TrimSpace(buf.String())
		if detail == "" {
			detail = resp.Status
		}
		return fmt.Errorf("remote backend %s %s failed: %s", method, endpoint, detail)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func putRemoteBackendSession(cfg Config, machine remoteMachine, session remoteSession) error {
	if machine.Provider != remoteProviderBackend || remoteBackendURL(cfg, machine) == "" {
		return nil
	}
	return remoteBackendMachineRequest(cfg, machine, http.MethodPut, "/v1/sessions/"+session.ID, session, nil)
}

func deleteRemoteBackendSession(cfg Config, machine remoteMachine, sessionID string) error {
	if machine.Provider != remoteProviderBackend || remoteBackendURL(cfg, machine) == "" {
		return nil
	}
	return remoteBackendMachineRequest(cfg, machine, http.MethodDelete, "/v1/sessions/"+sessionID, nil, nil)
}

func remoteBackendConfigured(cfg Config) bool {
	return strings.TrimSpace(cfg.Remote.BackendURL) != ""
}

func remoteBackendURL(cfg Config, machine remoteMachine) string {
	if url := strings.TrimSpace(cfg.Remote.BackendURL); url != "" {
		return strings.TrimRight(url, "/")
	}
	return strings.TrimRight(machine.BackendURL, "/")
}

func remoteBackendToken(cfg Config) string {
	if token := strings.TrimSpace(cfg.Remote.BackendToken); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv(remoteBackendTokenEnv))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
