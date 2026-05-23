package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultRemoteBackendURL = "https://api.yolobox.dev"
	remoteBackendURLEnv     = "YOLOBOX_BACKEND_URL"
	remoteAuthTokenEnv      = "YOLOBOX_TOKEN"
)

type remoteBackendEnsureRequest struct {
	Name       string `json:"name"`
	SSHUser    string `json:"ssh_user,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
	RepoURL    string `json:"repo_url,omitempty"`
	Branch     string `json:"branch,omitempty"`
}

type remoteBackendMachineResponse struct {
	Machine remoteMachine `json:"machine"`
	Status  string        `json:"status,omitempty"`
}

type remoteBackendListResponse struct {
	Machines []remoteMachine `json:"machines"`
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
		Name:       opts.Name,
		SSHUser:    firstNonEmpty(opts.SSHUser, cfg.Remote.SSHUser, "root"),
		SourcePath: sourcePath,
		RepoURL:    repo.URL,
		Branch:     repo.Branch,
	}
	var response remoteBackendMachineResponse
	if err := remoteBackendRequest(cfg, http.MethodPost, "/v1/machines/ensure", req, &response); err != nil {
		return remoteMachine{}, err
	}
	machine := response.Machine
	machine.Name = opts.Name
	machine.SourcePath = sourcePath
	machine.RepoURL = repo.URL
	machine.Branch = repo.Branch
	if machine.SSHUser == "" {
		machine.SSHUser = req.SSHUser
	}
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}
	if machine.CreatedAt.IsZero() {
		machine.CreatedAt = time.Now().UTC()
	}
	machine.UpdatedAt = time.Now().UTC()
	if machine.PublicIPv4 == "" {
		return remoteMachine{}, fmt.Errorf("remote backend returned no SSH host for %s", opts.Name)
	}
	return machine, nil
}

func getRemoteBackendMachine(cfg Config, name string) (remoteMachine, string, error) {
	var response remoteBackendMachineResponse
	if err := remoteBackendRequest(cfg, http.MethodGet, "/v1/machines/"+url.PathEscape(name), nil, &response); err != nil {
		return remoteMachine{}, "", err
	}
	machine := response.Machine
	machine.Name = name
	if machine.SSHUser == "" {
		machine.SSHUser = firstNonEmpty(cfg.Remote.SSHUser, "root")
	}
	if machine.ProjectPath == "" {
		machine.ProjectPath = remoteProjectPath()
	}
	return machine, response.Status, nil
}

func listRemoteBackendMachines(cfg Config) ([]remoteMachine, error) {
	var response remoteBackendListResponse
	if err := remoteBackendRequest(cfg, http.MethodGet, "/v1/machines", nil, &response); err != nil {
		return nil, err
	}
	for i := range response.Machines {
		if response.Machines[i].ProjectPath == "" {
			response.Machines[i].ProjectPath = remoteProjectPath()
		}
		if response.Machines[i].SSHUser == "" {
			response.Machines[i].SSHUser = firstNonEmpty(cfg.Remote.SSHUser, "root")
		}
	}
	return response.Machines, nil
}

func updateRemoteBackendMachine(cfg Config, machine remoteMachine) error {
	return remoteBackendRequest(cfg, http.MethodPatch, "/v1/machines/"+url.PathEscape(machine.Name), machine, nil)
}

func releaseRemoteBackendMachine(cfg Config, name string) error {
	return remoteBackendRequest(cfg, http.MethodDelete, "/v1/machines/"+url.PathEscape(name), nil, nil)
}

func remoteBackendRequest(cfg Config, method string, endpoint string, body any, out any) error {
	baseURL := remoteBackendURL(cfg)
	if baseURL == "" {
		return fmt.Errorf("remote backend URL is not configured")
	}
	token := remoteAuthToken(cfg)
	if token == "" {
		return fmt.Errorf("remote session token is not configured; run `yolobox login` or set %s", remoteAuthTokenEnv)
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

func remoteBackendURL(cfg Config) string {
	if url := strings.TrimSpace(os.Getenv(remoteBackendURLEnv)); url != "" {
		return strings.TrimRight(url, "/")
	}
	if url := strings.TrimSpace(cfg.Remote.BackendURL); url != "" {
		return strings.TrimRight(url, "/")
	}
	return defaultRemoteBackendURL
}

func remoteAuthToken(cfg Config) string {
	if token := strings.TrimSpace(os.Getenv(remoteAuthTokenEnv)); token != "" {
		return token
	}
	return strings.TrimSpace(cfg.Remote.Token)
}

func validateRemoteBackendURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("expected http or https URL")
	}
	if parsed.Host == "" {
		return fmt.Errorf("expected URL host")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
