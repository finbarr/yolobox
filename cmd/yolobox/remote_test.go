package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteBackendClientEnsuresUpdatesListsAndReleasesMachine(t *testing.T) {
	const token = "secret-token"
	var patched remoteMachine
	deleted := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/ensure":
			var req remoteBackendEnsureRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode ensure request: %v", err)
			}
			if req.Name != "foo" || req.RepoURL == "" {
				t.Fatalf("unexpected ensure request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(remoteBackendMachineResponse{
				Machine: remoteMachine{
					Name:       req.Name,
					Provider:   "digitalocean",
					ProviderID: "host-a",
					PublicIPv4: "203.0.113.10",
					SSHUser:    "root",
				},
				Status: "leased",
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/machines/foo":
			if err := json.NewDecoder(r.Body).Decode(&patched); err != nil {
				t.Fatalf("decode patch request: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/machines":
			_ = json.NewEncoder(w).Encode(remoteBackendListResponse{Machines: []remoteMachine{{Name: "foo", PublicIPv4: "203.0.113.10"}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/machines/foo":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	projectDir := t.TempDir()
	runGit(t, projectDir, "init")
	runGit(t, projectDir, "remote", "add", "origin", "git@example.com:repo.git")
	runGit(t, projectDir, "checkout", "-b", "feature/test")

	cfg := defaultConfig()
	cfg.Remote.BackendURL = ts.URL
	cfg.Remote.Token = token
	machine, err := ensureRemoteBackendMachine(cfg, projectDir, remoteProvisionOptions{Name: "foo"})
	if err != nil {
		t.Fatalf("ensureRemoteBackendMachine failed: %v", err)
	}
	if machine.ProjectPath != remoteProjectRoot || machine.Provider != "digitalocean" || machine.ProviderID != "host-a" {
		t.Fatalf("unexpected machine: %+v", machine)
	}
	machine.LastCommand = []string{"codex"}
	if err := updateRemoteBackendMachine(cfg, machine); err != nil {
		t.Fatalf("updateRemoteBackendMachine failed: %v", err)
	}
	if patched.Name != "foo" || patched.LastCommand[0] != "codex" {
		t.Fatalf("unexpected patched machine: %+v", patched)
	}
	machines, err := listRemoteBackendMachines(cfg)
	if err != nil {
		t.Fatalf("listRemoteBackendMachines failed: %v", err)
	}
	if len(machines) != 1 || machines[0].Name != "foo" {
		t.Fatalf("unexpected machine list: %+v", machines)
	}
	if err := releaseRemoteBackendMachine(cfg, "foo"); err != nil {
		t.Fatalf("releaseRemoteBackendMachine failed: %v", err)
	}
	if !deleted {
		t.Fatal("expected delete request")
	}
}

func TestRemoteBackendClientReportsUnauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad token"))
	}))
	defer ts.Close()

	cfg := defaultConfig()
	cfg.Remote.BackendURL = ts.URL
	cfg.Remote.Token = "wrong-token"
	_, _, err := getRemoteBackendMachine(cfg, "foo")
	if err == nil || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("expected unauthorized detail, got %v", err)
	}
}

func TestBuildRemoteSetupScript(t *testing.T) {
	machine := remoteMachine{ProjectPath: "/opt/yolobox/project"}
	script := buildRemoteSetupScript(machine, []string{"docker compose pull"})
	for _, want := range []string{
		"set -euo pipefail",
		"cd '/opt/yolobox/project'",
		"docker compose pull",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected setup script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestRemoteTmuxCommand(t *testing.T) {
	machine := remoteMachine{ProjectPath: "/opt/yolobox/my app"}
	command := remoteTmuxCommand(machine, []string{"yolobox", "codex", "--resume"}, true)
	for _, want := range []string{
		"cd '/opt/yolobox/my app'",
		"tmux new-session -A -s 'yolobox'",
		"codex",
		"--resume",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected tmux command to contain %q, got %s", want, command)
		}
	}
}

func TestRemoteDirectCommand(t *testing.T) {
	machine := remoteMachine{ProjectPath: "/opt/yolobox/my app"}
	command := remoteDirectCommand(machine, []string{"yolobox", "run", "echo", "ok"})
	if !strings.Contains(command, "cd '/opt/yolobox/my app'; exec 'yolobox' 'run' 'echo' 'ok'") {
		t.Fatalf("unexpected direct command: %s", command)
	}
}

func TestRemoteCommandNeedsTTY(t *testing.T) {
	for _, tt := range []struct {
		name    string
		command []string
		want    bool
	}{
		{name: "shell", command: []string{"shell"}, want: true},
		{name: "codex interactive", command: []string{"codex"}, want: true},
		{name: "codex interactive", command: []string{"codex", "exec", "hello"}, want: true},
		{name: "run echo", command: []string{"run", "echo", "ok"}, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := remoteCommandNeedsTTY(tt.command); got != tt.want {
				t.Fatalf("remoteCommandNeedsTTY(%v) = %t, want %t", tt.command, got, tt.want)
			}
		})
	}
}

func TestValidateRemoteDefaultsRequiresLogin(t *testing.T) {
	cfg := defaultConfig()
	cfg.Mode = "remote"
	cfg.RemoteName = "foo"
	cfg.Remote.Token = ""
	t.Setenv(remoteAuthTokenEnv, "")

	err := validateRemoteDefaults(cfg)
	if err == nil || !strings.Contains(err.Error(), "yolobox login") {
		t.Fatalf("expected login error, got %v", err)
	}
	cfg.Remote.Token = "secret-token"
	if err := validateRemoteDefaults(cfg); err != nil {
		t.Fatalf("expected configured token to pass, got %v", err)
	}
}

func TestParseRemoteCreateFlagsBackendOnly(t *testing.T) {
	opts, command, err := parseRemoteCreateFlags([]string{
		"--name", "Foo",
		"--ssh-user", "ubuntu",
		"--backend-url", "https://remote.example.com/",
		"codex",
	}, defaultConfig())
	if err != nil {
		t.Fatalf("parseRemoteCreateFlags failed: %v", err)
	}
	if opts.Name != "foo" || opts.SSHUser != "ubuntu" || opts.BackendURL != "https://remote.example.com" {
		t.Fatalf("unexpected opts: %+v", opts)
	}
	expectSliceEqual(t, command, []string{"codex"})
}

func TestRemoteConfigForProvisionAppliesBackendURLToFollowupRequests(t *testing.T) {
	cfg := defaultConfig()
	cfg.Remote.BackendURL = "https://api.example.com"
	cfg.Remote.SSHUser = "root"

	next, err := remoteConfigForProvision(cfg, remoteProvisionOptions{
		BackendURL: "https://selfhosted.example.com",
		SSHUser:    "ubuntu",
	})
	if err != nil {
		t.Fatalf("remoteConfigForProvision failed: %v", err)
	}
	if next.Remote.BackendURL != "https://selfhosted.example.com" || next.Remote.SSHUser != "ubuntu" {
		t.Fatalf("expected provision options to apply to config, got %+v", next.Remote)
	}
}

func TestLoadConfigRemoteDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	projectDir := t.TempDir()
	config := `mode = "remote"
remote_name = "foo"
[remote]
backend_url = "https://remote.example.com/"
token = "secret-token"
ssh_user = "ubuntu"
setup = ["docker compose pull"]
`
	if err := os.WriteFile(filepath.Join(projectDir, ".yolobox.toml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(projectDir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg.Mode != "remote" || cfg.RemoteName != "foo" {
		t.Fatalf("expected remote mode/name, got mode=%q name=%q", cfg.Mode, cfg.RemoteName)
	}
	if cfg.Remote.BackendURL != "https://remote.example.com" || cfg.Remote.Token != "secret-token" || cfg.Remote.SSHUser != "ubuntu" {
		t.Fatalf("unexpected remote config: %+v", cfg.Remote)
	}
	if len(cfg.Remote.Setup) != 1 || cfg.Remote.Setup[0] != "docker compose pull" {
		t.Fatalf("unexpected remote setup: %+v", cfg.Remote.Setup)
	}
}

func TestSaveGlobalConfigRemote(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg := defaultConfig()
	cfg.Mode = "remote"
	cfg.RemoteName = "Foo"
	cfg.DefaultHarness = "codex"
	cfg.Remote.BackendURL = "https://remote.example.com/"
	cfg.Remote.Token = "secret-token"
	cfg.Remote.SSHUser = "ubuntu"
	cfg.Remote.Setup = []string{"docker compose pull"}

	if err := saveGlobalConfig(cfg); err != nil {
		t.Fatalf("saveGlobalConfig failed: %v", err)
	}
	path, _ := globalConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`mode = "remote"`,
		`remote_name = "foo"`,
		`default_harness = "codex"`,
		`[remote]`,
		`backend_url = "https://remote.example.com/"`,
		`token = "secret-token"`,
		`ssh_user = "ubuntu"`,
		`setup = ["docker compose pull"]`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected saved config to contain %q, got:\n%s", want, content)
		}
	}
}

func TestParseRemoteForwardUsesConfiguredTargetForPortOnly(t *testing.T) {
	cfg := defaultConfig()
	cfg.RemoteName = "foo"

	name, localPort, remotePort, err := parseRemoteForwardArgs([]string{"3000", "--local-port", "13000"}, cfg)
	if err != nil {
		t.Fatalf("parseRemoteForwardArgs failed: %v", err)
	}
	if name != "foo" {
		t.Fatalf("unexpected remote name: %s", name)
	}
	if localPort != 13000 || remotePort != 3000 {
		t.Fatalf("unexpected ports: local=%d remote=%d", localPort, remotePort)
	}
}

func TestRemoteSyncDownRequiresForce(t *testing.T) {
	projectDir := t.TempDir()
	cfg := `remote_name = "foo"
[remote]
backend_url = "http://127.0.0.1:1"
token = "secret-token"
`
	if err := os.WriteFile(filepath.Join(projectDir, ".yolobox.toml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	err := runRemoteSync([]string{"down", "foo"}, projectDir)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected --force error before backend request, got %v", err)
	}
}

func TestRunSSHForwardUsesLocalPortMapping(t *testing.T) {
	tmpDir := t.TempDir()
	sshPath := filepath.Join(tmpDir, "ssh")
	logPath := filepath.Join(tmpDir, "ssh.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(logPath) + "\n"
	if err := os.WriteFile(sshPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	machine := remoteMachine{PublicIPv4: "203.0.113.10", SSHUser: "root"}
	if err := runSSHForward(machine, 13000, 3000); err != nil {
		t.Fatalf("runSSHForward failed: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "127.0.0.1:13000:127.0.0.1:3000") {
		t.Fatalf("expected local port forward mapping, got %s", got)
	}
}

func TestRunLoginCallsBackendAndLogoutRevokesSession(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	loginCalled := false
	logoutCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/sign-in/email":
			loginCalled = true
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req["email"] != "user@example.com" || req["password"] != "secret-password" {
				t.Fatalf("unexpected login request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(remoteAuthLoginResponse{Token: "session-token"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/sign-out":
			logoutCalled = true
			if r.Header.Get("Authorization") != "Bearer session-token" {
				t.Fatalf("unexpected logout auth header: %s", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			t.Fatalf("unexpected auth request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	if err := runLogin([]string{"--backend-url", ts.URL, "--email", "user@example.com", "--password", "secret-password"}); err != nil {
		t.Fatalf("runLogin failed: %v", err)
	}
	cfg, err := loadSetupDefaults()
	if err != nil {
		t.Fatalf("loadSetupDefaults failed: %v", err)
	}
	if cfg.Remote.BackendURL != ts.URL || cfg.Remote.Token != "session-token" {
		t.Fatalf("unexpected login config: %+v", cfg.Remote)
	}
	if err := runLogout(nil); err != nil {
		t.Fatalf("runLogout failed: %v", err)
	}
	if !loginCalled || !logoutCalled {
		t.Fatalf("expected login and logout calls, login=%t logout=%t", loginCalled, logoutCalled)
	}
	cfg, err = loadSetupDefaults()
	if err != nil {
		t.Fatalf("loadSetupDefaults after logout failed: %v", err)
	}
	if cfg.Remote.Token != "" {
		t.Fatalf("expected logout to clear token, got %+v", cfg.Remote)
	}
}

func TestRunLoginStoresExistingToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	if err := runLogin([]string{"--backend-url", "https://remote.example.com", "--token", "secret-token"}); err != nil {
		t.Fatalf("runLogin failed: %v", err)
	}
	cfg, err := loadSetupDefaults()
	if err != nil {
		t.Fatalf("loadSetupDefaults failed: %v", err)
	}
	if cfg.Remote.BackendURL != "https://remote.example.com" || cfg.Remote.Token != "secret-token" {
		t.Fatalf("unexpected login config: %+v", cfg.Remote)
	}
}

func TestRunLoginSignupCallsBackend(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/sign-up/email" {
			t.Fatalf("unexpected signup request %s %s", r.Method, r.URL.Path)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req["email"] != "new@example.com" || req["password"] != "secret-password" || req["name"] != "New User" {
			t.Fatalf("unexpected signup request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(remoteAuthLoginResponse{Token: "signup-session-token"})
	}))
	defer ts.Close()

	if err := runLogin([]string{"--signup", "--backend-url", ts.URL, "--email", "new@example.com", "--password", "secret-password", "--name", "New User"}); err != nil {
		t.Fatalf("runLogin signup failed: %v", err)
	}
	cfg, err := loadSetupDefaults()
	if err != nil {
		t.Fatalf("loadSetupDefaults failed: %v", err)
	}
	if cfg.Remote.Token != "signup-session-token" {
		t.Fatalf("expected signup session token, got %+v", cfg.Remote)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
