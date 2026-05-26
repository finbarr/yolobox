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
	"time"
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
	machine := remoteMachine{ProjectPath: "/opt/yolobox/project", SourcePath: "/Users/example/my app"}
	script := buildRemoteSetupScript(machine, []string{"docker compose pull"})
	for _, want := range []string{
		"set -euo pipefail",
		"cd '/Users/example/my app'",
		"docker compose pull",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected setup script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestBuildEnsureRemoteProjectPathScriptCreatesSourceAlias(t *testing.T) {
	machine := remoteMachine{ProjectPath: "/opt/yolobox/project", SourcePath: "/Users/example/my app"}
	script := buildEnsureRemoteProjectPathScript(machine)
	for _, want := range []string{
		"storage='/opt/yolobox/project'",
		"work='/Users/example/my app'",
		"ln -s \"$storage\" \"$work\"",
		"cannot map remote project workdir $work",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected ensure script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestEnsureRemoteProjectPathScriptCreatesSourceAlias(t *testing.T) {
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "rsync"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake rsync: %v", err)
	}

	storagePath := filepath.Join(root, "storage", "project")
	sourcePath := filepath.Join(root, "Users", "example", "project")
	script := buildEnsureRemoteProjectPathScript(remoteMachine{ProjectPath: storagePath, SourcePath: sourcePath})
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ensure script failed: %v\n%s", err, output)
	}

	info, err := os.Stat(storagePath)
	if err != nil {
		t.Fatalf("stat storage path: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected storage path to be a directory")
	}
	target, err := os.Readlink(sourcePath)
	if err != nil {
		t.Fatalf("read source alias: %v", err)
	}
	if target != storagePath {
		t.Fatalf("source alias target = %q, want %q", target, storagePath)
	}
}

func TestRemoteTmuxCommandUsesSourcePathAsVMWorkdir(t *testing.T) {
	machine := remoteMachine{ProjectPath: "/opt/yolobox/project", SourcePath: "/Users/example/my app"}
	command := remoteTmuxCommand(machine, []string{"codex", "--resume"}, true)
	for _, want := range []string{
		"cd '/Users/example/my app'",
		"tmux new-session -A -s 'yolobox'",
		"-c '/Users/example/my app'",
		"'/usr/local/bin/yolobox-remote-session' '/Users/example/my app'",
		"codex",
		"--resume",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected tmux command to contain %q, got %s", want, command)
		}
	}
}

func TestRemoteTmuxCommandFallsBackToProjectPath(t *testing.T) {
	machine := remoteMachine{ProjectPath: "/opt/yolobox/my app"}
	command := remoteTmuxCommand(machine, []string{"codex", "--resume"}, true)
	for _, want := range []string{
		"cd '/opt/yolobox/my app'",
		"-c '/opt/yolobox/my app'",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected tmux command to contain %q, got %s", want, command)
		}
	}
}

func TestRemoteDirectCommandUsesSourcePathAsVMWorkdir(t *testing.T) {
	machine := remoteMachine{ProjectPath: "/opt/yolobox/project", SourcePath: "/Users/example/my app"}
	command := remoteDirectCommand(machine, remoteHostCommand([]string{"run", "echo", "ok"}))
	if !strings.Contains(command, "cd '/Users/example/my app'; exec 'echo' 'ok'") {
		t.Fatalf("unexpected direct command: %s", command)
	}
}

func TestRemoteHostCommandMapsYoloboxSubcommandsToVMCommands(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   []string
		want []string
	}{
		{name: "empty", in: nil, want: []string{"bash"}},
		{name: "shell", in: []string{"shell"}, want: []string{"bash"}},
		{name: "run", in: []string{"run", "make", "test"}, want: []string{"make", "test"}},
		{name: "run empty", in: []string{"run"}, want: []string{"bash"}},
		{name: "agent", in: []string{"codex", "--resume"}, want: []string{"codex", "--resume"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expectSliceEqual(t, remoteHostCommand(tt.in), tt.want)
		})
	}
}

func TestRemoteBootstrapScriptInstallsVMRuntimeNotNestedYolobox(t *testing.T) {
	script := buildRemoteBootstrapScript()
	for _, want := range []string{
		remoteRuntimeReadyMarker,
		remoteRuntimeSessionScript,
		"@openai/codex",
		"docker network create yolobox-net",
		"YOLOBOX_REMOTE=1",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected remote VM installer to contain %q", want)
		}
	}
	for _, notWant := range []string{
		"ghcr.io/finbarr/yolobox",
		"raw.githubusercontent.com/finbarr/yolobox/master/install.sh",
		"docker pull",
		"exec 'yolobox'",
	} {
		if strings.Contains(script, notWant) {
			t.Fatalf("remote VM installer should not contain nested yolobox path %q", notWant)
		}
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

func TestRunRemoteUnknownCommandDoesNotFallThroughToCreate(t *testing.T) {
	err := runRemote([]string{"resume"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown remote command: resume") {
		t.Fatalf("expected unknown command error, got %v", err)
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

func TestRunLoginCallsBackendAndLogoutRevokesSession(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	loginCalled := false
	logoutCalled := false
	expectedOrigin := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/sign-in/email":
			loginCalled = true
			if r.Header.Get("Origin") != expectedOrigin {
				t.Fatalf("unexpected login origin: %s", r.Header.Get("Origin"))
			}
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
			if r.Header.Get("Origin") != expectedOrigin {
				t.Fatalf("unexpected logout origin: %s", r.Header.Get("Origin"))
			}
			if r.Header.Get("Authorization") != "Bearer session-token" {
				t.Fatalf("unexpected logout auth header: %s", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			t.Fatalf("unexpected auth request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()
	expectedOrigin = ts.URL

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

func TestRunLoginUsesBrowserDeviceFlowByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	oldOpenBrowserURL := openBrowserURL
	oldRemoteAuthSleep := remoteAuthSleep
	defer func() {
		openBrowserURL = oldOpenBrowserURL
		remoteAuthSleep = oldRemoteAuthSleep
	}()
	var openedURL string
	openBrowserURL = func(openURL string) error {
		openedURL = openURL
		return nil
	}
	remoteAuthSleep = func(time.Duration) {}

	polls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/device/code":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req["client_id"] != "yolobox-cli" || req["scope"] != "remote" {
				t.Fatalf("unexpected device code request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(remoteAuthDeviceCodeResponse{
				DeviceCode:              "device-code",
				UserCode:                "ABCD1234",
				VerificationURI:         "https://app.example.com/device",
				VerificationURIComplete: "https://app.example.com/device?user_code=ABCD1234",
				ExpiresIn:               30,
				Interval:                1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/device/token":
			polls++
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req["client_id"] != "yolobox-cli" || req["device_code"] != "device-code" || req["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Fatalf("unexpected device token request: %+v", req)
			}
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(remoteAuthEndpointError{Code: "authorization_pending", Description: "pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(remoteAuthDeviceTokenResponse{AccessToken: "session-token", TokenType: "Bearer"})
		default:
			t.Fatalf("unexpected auth request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	if err := runLogin([]string{"--backend-url", ts.URL}); err != nil {
		t.Fatalf("runLogin failed: %v", err)
	}
	if openedURL != "https://app.example.com/device?user_code=ABCD1234" {
		t.Fatalf("expected browser URL to open, got %q", openedURL)
	}
	if polls != 2 {
		t.Fatalf("expected two polls, got %d", polls)
	}
	cfg, err := loadSetupDefaults()
	if err != nil {
		t.Fatalf("loadSetupDefaults failed: %v", err)
	}
	if cfg.Remote.BackendURL != ts.URL || cfg.Remote.Token != "session-token" {
		t.Fatalf("unexpected login config: %+v", cfg.Remote)
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

func TestRemoteAuthOrigin(t *testing.T) {
	for _, tt := range []struct {
		url  string
		want string
	}{
		{url: "https://api.yolobox.dev", want: "https://api.yolobox.dev"},
		{url: "http://127.0.0.1:8787/", want: "http://127.0.0.1:8787"},
		{url: "not a url", want: ""},
	} {
		if got := remoteAuthOrigin(tt.url); got != tt.want {
			t.Fatalf("remoteAuthOrigin(%q) = %q, want %q", tt.url, got, tt.want)
		}
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
