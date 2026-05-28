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
	defaultRemoteBackendURL       = "https://api.yolobox.dev"
	remoteBackendURLEnv           = "YOLOBOX_BACKEND_URL"
	remoteAuthTokenEnv            = "YOLOBOX_TOKEN"
	remoteBackendDefaultTimeout   = 30 * time.Second
	remoteBackendProvisionTimeout = 5 * time.Minute
)

type remoteBackendCreateRequest struct {
	Name       string `json:"name"`
	SSHUser    string `json:"ssh_user,omitempty"`
	Tier       string `json:"tier,omitempty"`
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

type remoteBackendTunnelKeyResponse struct {
	PrivateKey string `json:"private_key"`
}

type remoteBackendWorkspaceRequest struct {
	SourcePath  string `json:"source_path,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
	RepoURL     string `json:"repo_url,omitempty"`
	Branch      string `json:"branch,omitempty"`
}

type remoteBackendSetupRequest struct {
	Commands []string `json:"commands,omitempty"`
}

type remoteBackendSessionPrepareRequest struct {
	Command []string `json:"command,omitempty"`
	Attach  bool     `json:"attach,omitempty"`
}

type remoteBackendCommandRequest struct {
	Command []string `json:"command,omitempty"`
}

type remoteBackendAgentResult struct {
	Command       string `json:"command,omitempty"`
	AttachCommand string `json:"attach_command,omitempty"`
	Status        string `json:"status,omitempty"`
	RecordCommand bool   `json:"record_command,omitempty"`
	Stdout        string `json:"stdout,omitempty"`
	Stderr        string `json:"stderr,omitempty"`
}

type remoteBackendAgentResponse struct {
	Machine remoteMachine            `json:"machine"`
	Result  remoteBackendAgentResult `json:"result,omitempty"`
}

func createRemoteBackendMachine(cfg Config, projectDir string, opts remoteProvisionOptions) (remoteMachine, error) {
	sourcePath, err := normalizedProjectPath(projectDir)
	if err != nil {
		return remoteMachine{}, err
	}
	repo := currentGitRepo(sourcePath)
	req := remoteBackendCreateRequest{
		Name:       opts.Name,
		SSHUser:    firstNonEmpty(opts.SSHUser, cfg.Remote.SSHUser, "root"),
		Tier:       opts.Tier,
		SourcePath: sourcePath,
		RepoURL:    repo.URL,
		Branch:     repo.Branch,
	}
	var response remoteBackendMachineResponse
	if err := remoteBackendRequestWithTimeout(cfg, http.MethodPost, "/v1/machines", req, &response, remoteBackendProvisionTimeout); err != nil {
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

func prepareRemoteBackendWorkspace(cfg Config, machine remoteMachine) (remoteMachine, error) {
	req := remoteBackendWorkspaceRequest{
		SourcePath:  machine.SourcePath,
		ProjectPath: machine.ProjectPath,
		RepoURL:     machine.RepoURL,
		Branch:      machine.Branch,
	}
	var response remoteBackendAgentResponse
	if err := remoteBackendRequestWithTimeout(cfg, http.MethodPost, "/v1/machines/"+url.PathEscape(machine.Name)+"/workspace", req, &response, remoteBackendProvisionTimeout); err != nil {
		return machine, err
	}
	return mergeRemoteBackendMachine(machine, response.Machine), nil
}

func completeRemoteBackendSync(cfg Config, machine remoteMachine) (remoteMachine, error) {
	req := remoteBackendWorkspaceRequest{
		SourcePath:  machine.SourcePath,
		ProjectPath: machine.ProjectPath,
		RepoURL:     machine.RepoURL,
		Branch:      machine.Branch,
	}
	var response remoteBackendAgentResponse
	if err := remoteBackendRequest(cfg, http.MethodPost, "/v1/machines/"+url.PathEscape(machine.Name)+"/sync-complete", req, &response); err != nil {
		return machine, err
	}
	return mergeRemoteBackendMachine(machine, response.Machine), nil
}

func runRemoteBackendSetup(cfg Config, machine remoteMachine, commands []string) error {
	if len(commands) == 0 {
		return nil
	}
	var response remoteBackendAgentResponse
	req := remoteBackendSetupRequest{Commands: commands}
	if err := remoteBackendRequestWithTimeout(cfg, http.MethodPost, "/v1/machines/"+url.PathEscape(machine.Name)+"/setup", req, &response, remoteBackendProvisionTimeout); err != nil {
		return err
	}
	if response.Result.Stdout != "" {
		_, _ = fmt.Fprint(os.Stdout, response.Result.Stdout)
	}
	if response.Result.Stderr != "" {
		_, _ = fmt.Fprint(os.Stderr, response.Result.Stderr)
	}
	return nil
}

func prepareRemoteBackendSession(cfg Config, machine remoteMachine, command []string, attach bool) (remoteBackendAgentResult, remoteMachine, error) {
	req := remoteBackendSessionPrepareRequest{Command: command, Attach: attach}
	var response remoteBackendAgentResponse
	if err := remoteBackendRequest(cfg, http.MethodPost, "/v1/machines/"+url.PathEscape(machine.Name)+"/sessions/yolobox/prepare", req, &response); err != nil {
		return remoteBackendAgentResult{}, machine, err
	}
	return response.Result, mergeRemoteBackendMachine(machine, response.Machine), nil
}

func remoteBackendSSHCommand(cfg Config, machine remoteMachine, command []string) (string, error) {
	req := remoteBackendCommandRequest{Command: command}
	var response remoteBackendAgentResponse
	if err := remoteBackendRequest(cfg, http.MethodPost, "/v1/machines/"+url.PathEscape(machine.Name)+"/commands/ssh", req, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Result.Command) == "" {
		return "", fmt.Errorf("remote backend returned no SSH command for %s", machine.Name)
	}
	return response.Result.Command, nil
}

func recordRemoteBackendCommand(cfg Config, machine remoteMachine, command []string) error {
	req := remoteBackendCommandRequest{Command: command}
	return remoteBackendRequest(cfg, http.MethodPost, "/v1/machines/"+url.PathEscape(machine.Name)+"/commands/record", req, nil)
}

func releaseRemoteBackendMachine(cfg Config, name string) error {
	return remoteBackendRequest(cfg, http.MethodDelete, "/v1/machines/"+url.PathEscape(name), nil, nil)
}

func getRemoteBackendTunnelKey(cfg Config, name string) (string, error) {
	var response remoteBackendTunnelKeyResponse
	if err := remoteBackendRequest(cfg, http.MethodGet, "/v1/machines/"+url.PathEscape(name)+"/tunnel-key", nil, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.PrivateKey) == "" {
		return "", fmt.Errorf("remote backend returned no tunnel SSH key for %s", name)
	}
	return response.PrivateKey, nil
}

func remoteBackendRequest(cfg Config, method string, endpoint string, body any, out any) error {
	return remoteBackendRequestWithTimeout(cfg, method, endpoint, body, out, remoteBackendDefaultTimeout)
}

func remoteBackendRequestWithTimeout(cfg Config, method string, endpoint string, body any, out any, timeout time.Duration) error {
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
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
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

func mergeRemoteBackendMachine(local remoteMachine, remote remoteMachine) remoteMachine {
	if remote.Name == "" {
		return local
	}
	if remote.SSHUser == "" {
		remote.SSHUser = local.SSHUser
	}
	if remote.ProjectPath == "" {
		remote.ProjectPath = local.ProjectPath
	}
	remote.SSHPrivateKey = local.SSHPrivateKey
	remote.SSHKeyPath = local.SSHKeyPath
	return remote
}
