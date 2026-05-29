package main

import (
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = oldStdout

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	_ = r.Close()
	return string(out)
}

func stripOutputColors(output string) string {
	return strings.NewReplacer(
		colorBold, "",
		colorRed, "",
		colorGreen, "",
		colorYellow, "",
		colorBlue, "",
		colorCyan, "",
		colorReset, "",
	).Replace(output)
}

func isolateRemoteSSHHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeFakeRemoteSSHCertificate(t *testing.T, w http.ResponseWriter, r *http.Request, host string) {
	t.Helper()
	var req remoteBackendSSHCertificateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decode SSH certificate request: %v", err)
	}
	if !strings.HasPrefix(req.PublicKey, "ssh-ed25519 ") {
		t.Fatalf("unexpected SSH certificate public key: %q", req.PublicKey)
	}
	_ = json.NewEncoder(w).Encode(remoteBackendSSHCertificateResponse{
		Certificate: "ssh-ed25519-cert-v01@openssh.com FAKE-CERT yolobox-test",
		Host:        host,
		Port:        22,
		SSHUser:     "root",
	})
}

func TestRemoteBackendClientCreatesListsAndReleasesMachine(t *testing.T) {
	const token = "secret-token"
	deleted := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines":
			var req remoteBackendCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			if req.Name != "foo" || req.RepoURL == "" {
				t.Fatalf("unexpected create request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(remoteBackendMachineResponse{
				Machine: remoteMachine{
					Name:       req.Name,
					Provider:   "digitalocean",
					ProviderID: "host-a",
					PublicIPv4: "203.0.113.10",
					SSHUser:    "root",
				},
				Status: "created",
			})
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
	machine, err := createRemoteBackendMachine(cfg, projectDir, remoteProvisionOptions{Name: "foo"})
	if err != nil {
		t.Fatalf("createRemoteBackendMachine failed: %v", err)
	}
	if machine.ProjectPath != remoteProjectRoot || machine.Provider != "digitalocean" || machine.ProviderID != "host-a" {
		t.Fatalf("unexpected machine: %+v", machine)
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

func TestRemoteBackendClientUsesHTTP1ForTLS(t *testing.T) {
	const token = "secret-token"
	gotProto := ""
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProto = r.Proto
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(remoteBackendListResponse{})
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()

	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(ts.Certificate())
	transport := remoteBackendHTTPTransport()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.RootCAs = rootCAs
	oldClient := remoteBackendHTTPClient
	remoteBackendHTTPClient = &http.Client{Transport: transport}
	t.Cleanup(func() {
		remoteBackendHTTPClient = oldClient
	})

	cfg := defaultConfig()
	cfg.Remote.BackendURL = ts.URL
	cfg.Remote.Token = token
	if _, err := listRemoteBackendMachines(cfg); err != nil {
		t.Fatalf("listRemoteBackendMachines failed: %v", err)
	}
	if gotProto != "HTTP/1.1" {
		t.Fatalf("expected backend client to use HTTP/1.1, got %s", gotProto)
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

func TestRemoteListShowsCompactMachineTable(t *testing.T) {
	const token = "secret-token"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/machines" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(remoteBackendListResponse{Machines: []remoteMachine{{
			Name:            "boom",
			Provider:        "digitalocean",
			PublicIPv4:      "203.0.113.10",
			Region:          "nyc3",
			Size:            "s-2vcpu-4gb-amd",
			PreviewHostname: "amber-bridge-a1b2c3.hosted.yolobox.dev",
			PreviewURL:      "https://amber-bridge-a1b2c3.hosted.yolobox.dev",
			ProjectPath:     "/opt/yolobox/project",
		}}})
	}))
	defer ts.Close()

	t.Setenv(remoteBackendURLEnv, ts.URL)
	t.Setenv(remoteAuthTokenEnv, token)

	var runErr error
	output := captureStdout(t, func() {
		runErr = runRemoteList(nil, t.TempDir())
	})
	if runErr != nil {
		t.Fatalf("runRemoteList failed: %v", runErr)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header and one row, got %q", output)
	}
	if got := strings.Fields(lines[0]); strings.Join(got, "|") != "NAME|SIZE|URL" {
		t.Fatalf("unexpected header fields: %v", got)
	}
	if got := strings.Fields(lines[1]); strings.Join(got, "|") != "boom|s-2vcpu-4gb-amd|https://amber-bridge-a1b2c3.hosted.yolobox.dev" {
		t.Fatalf("unexpected row fields: %v", got)
	}
	for _, notWant := range []string{"PROVIDER", "IP", "REGION", "STORAGE", "203.0.113.10", "/opt/yolobox/project"} {
		if strings.Contains(output, notWant) {
			t.Fatalf("remote list output should not contain %q:\n%s", notWant, output)
		}
	}
}

func TestPrintRemoteReadySplitsPreviewURL(t *testing.T) {
	output := captureStderr(t, func() {
		printRemoteReady(remoteMachine{
			Name:       "boom",
			PublicIPv4: "203.0.113.10",
			PreviewURL: "https://ember-bridge-15e1e3.hosted.yolobox.dev",
		})
	})
	clean := stripOutputColors(output)
	lines := strings.Split(strings.TrimSpace(clean), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected ready and preview lines, got %q", clean)
	}
	if lines[0] != "✓ Remote boom is ready" {
		t.Fatalf("unexpected ready line %q", lines[0])
	}
	if lines[1] != "↗ Preview: https://ember-bridge-15e1e3.hosted.yolobox.dev" {
		t.Fatalf("unexpected preview line %q", lines[1])
	}
	for _, notWant := range []string{"203.0.113.10", "via backend"} {
		if strings.Contains(clean, notWant) {
			t.Fatalf("ready output should not contain %q:\n%s", notWant, clean)
		}
	}
}

func TestPrintRemoteReadyOmitsEmptyPreviewURL(t *testing.T) {
	output := captureStderr(t, func() {
		printRemoteReady(remoteMachine{Name: "boom", PublicIPv4: "203.0.113.10"})
	})
	clean := stripOutputColors(output)
	if strings.TrimSpace(clean) != "✓ Remote boom is ready" {
		t.Fatalf("unexpected ready output %q", clean)
	}
	if strings.Contains(clean, "203.0.113.10") || strings.Contains(clean, "Preview:") {
		t.Fatalf("ready output should not include IP or empty preview:\n%s", clean)
	}
}

func TestRunWithSpinnerFallsBackToStableProgressWhenNotTerminal(t *testing.T) {
	called := false
	var runErr error
	output := captureStderr(t, func() {
		runErr = runWithSpinner("Waiting for backend", "Backend ready", func() error {
			called = true
			return nil
		})
	})
	if runErr != nil {
		t.Fatalf("runWithSpinner returned error: %v", runErr)
	}
	if !called {
		t.Fatal("expected callback to run")
	}
	clean := stripOutputColors(output)
	if !strings.Contains(clean, "→ Waiting for backend...\n") {
		t.Fatalf("expected waiting line, got %q", clean)
	}
	if !strings.Contains(clean, "✓ Backend ready\n") {
		t.Fatalf("expected done line, got %q", clean)
	}
}

func TestRemoteRunUsesExistingMachine(t *testing.T) {
	const token = "secret-token"
	isolateRemoteSSHHome(t)
	sawPost := false
	syncCompleteCalls := 0
	recordCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/machines/foo":
			_ = json.NewEncoder(w).Encode(remoteBackendMachineResponse{
				Machine: remoteMachine{
					Name:              "foo",
					Provider:          "digitalocean",
					ProviderID:        "host-a",
					PublicIPv4:        "203.0.113.10",
					SSHUser:           "root",
					BootstrapComplete: true,
				},
				Status: "active",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/foo/ssh-cert":
			writeFakeRemoteSSHCertificate(t, w, r, "203.0.113.10")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/foo/sync-complete":
			syncCompleteCalls++
			_ = json.NewEncoder(w).Encode(remoteBackendAgentResponse{Machine: remoteMachine{
				Name:              "foo",
				SSHUser:           "root",
				ProjectPath:       remoteProjectRoot,
				BootstrapComplete: true,
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/foo/commands/ssh":
			var req remoteBackendCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode command request: %v", err)
			}
			if strings.Join(req.Command, " ") != "true" {
				t.Fatalf("unexpected command request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(remoteBackendAgentResponse{Result: remoteBackendAgentResult{Command: "true"}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/foo/commands/record":
			recordCalls++
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines":
			sawPost = true
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte("unexpected create"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	rsyncArgsFile := filepath.Join(root, "rsync-args")
	if err := os.WriteFile(filepath.Join(fakeBin, "rsync"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$YOLOBOX_FAKE_RSYNC_ARGS\"\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake rsync: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(remoteBackendURLEnv, ts.URL)
	t.Setenv(remoteAuthTokenEnv, token)
	t.Setenv("YOLOBOX_FAKE_RSYNC_ARGS", rsyncArgsFile)

	if err := runRemote([]string{"run", "foo", "true"}, t.TempDir()); err != nil {
		t.Fatalf("remote run failed: %v", err)
	}
	if sawPost {
		t.Fatal("remote run must not create missing machines")
	}
	if syncCompleteCalls != 1 || recordCalls != 1 {
		t.Fatalf("expected sync/record calls, got sync=%d record=%d", syncCompleteCalls, recordCalls)
	}
	rsyncArgs, err := os.ReadFile(rsyncArgsFile)
	if err != nil {
		t.Fatalf("read rsync args: %v", err)
	}
	if strings.Contains(string(rsyncArgs), "--info=stats1") {
		t.Fatalf("rsync args should stay compatible with macOS rsync:\n%s", rsyncArgs)
	}
	if !strings.Contains(string(rsyncArgs), "--delete") {
		t.Fatalf("expected rsync args to include --delete:\n%s", rsyncArgs)
	}
	if !strings.Contains(string(rsyncArgs), "203.0.113.10") {
		t.Fatalf("rsync should use direct SSH to the provider public IP:\n%s", rsyncArgs)
	}
	if !strings.Contains(string(rsyncArgs), "CertificateFile=") || !strings.Contains(string(rsyncArgs), "HostKeyAlias=yolobox-foo-host-a") {
		t.Fatalf("expected rsync SSH command to use a certificate and stable host-key alias:\n%s", rsyncArgs)
	}
}

func TestRemoteSyncUpOnlyRecordsSyncThroughBackend(t *testing.T) {
	const token = "secret-token"
	isolateRemoteSSHHome(t)
	syncCompleteCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/machines/foo":
			_ = json.NewEncoder(w).Encode(remoteBackendMachineResponse{
				Machine: remoteMachine{
					Name:              "foo",
					Provider:          "digitalocean",
					ProviderID:        "host-a",
					PublicIPv4:        "203.0.113.10",
					SSHUser:           "root",
					BootstrapComplete: true,
				},
				Status: "active",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/foo/ssh-cert":
			writeFakeRemoteSSHCertificate(t, w, r, "203.0.113.10")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/foo/sync-complete":
			syncCompleteCalls++
			_ = json.NewEncoder(w).Encode(remoteBackendAgentResponse{Machine: remoteMachine{
				Name:              "foo",
				SSHUser:           "root",
				ProjectPath:       remoteProjectRoot,
				BootstrapComplete: true,
			}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "ssh"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "rsync"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake rsync: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(remoteBackendURLEnv, ts.URL)
	t.Setenv(remoteAuthTokenEnv, token)

	if err := runRemoteSync([]string{"up", "foo"}, t.TempDir()); err != nil {
		t.Fatalf("remote sync up failed: %v", err)
	}
	if syncCompleteCalls != 1 {
		t.Fatalf("expected sync-complete call, got %d", syncCompleteCalls)
	}
}

func TestRemoteSSHOptionsUsePersistentKnownHosts(t *testing.T) {
	home := isolateRemoteSSHHome(t)
	machine := remoteMachine{
		Name:               "foo",
		ProviderID:         "host-a",
		PublicIPv4:         "203.0.113.10",
		SSHKeyPath:         filepath.Join(t.TempDir(), "id_ed25519"),
		SSHCertificatePath: filepath.Join(t.TempDir(), "id_ed25519-cert.pub"),
	}

	args, err := remoteSSHOptions(machine, false)
	if err != nil {
		t.Fatalf("remoteSSHOptions failed: %v", err)
	}
	joined := strings.Join(args, "\n")
	knownHostsPath := filepath.Join(home, ".yolobox", remoteKnownHostsFileName)
	for _, want := range []string{
		"UserKnownHostsFile=" + knownHostsPath,
		"StrictHostKeyChecking=accept-new",
		"CheckHostIP=no",
		"HashKnownHosts=no",
		"CertificateFile=" + machine.SSHCertificatePath,
		"HostKeyAlias=yolobox-foo-host-a",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected SSH options to contain %q, got:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{
		"UserKnownHostsFile=/dev/null",
		"StrictHostKeyChecking=no",
	} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("SSH options must not contain %q:\n%s", unwanted, joined)
		}
	}
	info, err := os.Stat(knownHostsPath)
	if err != nil {
		t.Fatalf("expected known hosts file to be created: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("known hosts file mode = %o, want 600", got)
	}
}

func TestRemoteBootstrapScriptInstallsVMRuntimeNotNestedYolobox(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("assets", "remote-vm-install.sh"))
	if err != nil {
		t.Fatalf("read remote VM installer: %v", err)
	}
	script := string(data)
	for _, want := range []string{
		remoteRuntimeReadyMarker,
		remoteRuntimeSessionScript,
		"@openai/codex",
		"docker network create yolobox-net",
		"YOLOBOX_REMOTE=1",
		"/usr/local/lib/yolobox/agent.mjs",
		"/v1/agent/connect",
		"/var/log/yolobox-remote-install.log",
		"step \"installing base packages\"",
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
	err := runRemote([]string{"creeate", "foo"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown remote command: creeate") {
		t.Fatalf("expected unknown command error, got %v", err)
	}
	err = runRemote([]string{"--name", "foo"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown remote command: --name") {
		t.Fatalf("expected old --name form to be rejected, got %v", err)
	}
}

func TestParseRemoteCreateArgsBackendOnly(t *testing.T) {
	opts, noSync, err := parseRemoteCreateArgs([]string{
		"Foo",
		"--ssh-user", "ubuntu",
		"--backend-url", "https://remote.example.com/",
		"--tier", "Medium",
		"--no-sync",
	}, defaultConfig())
	if err != nil {
		t.Fatalf("parseRemoteCreateArgs failed: %v", err)
	}
	if opts.Name != "foo" || opts.SSHUser != "ubuntu" || opts.BackendURL != "https://remote.example.com" || opts.Tier != "medium" {
		t.Fatalf("unexpected opts: %+v", opts)
	}
	if !noSync {
		t.Fatal("expected --no-sync to be parsed")
	}
}

func TestParseRemoteCreateArgsRejectsUnknownTier(t *testing.T) {
	_, _, err := parseRemoteCreateArgs([]string{"foo", "--tier", "enormous"}, defaultConfig())
	if err == nil || !strings.Contains(err.Error(), "invalid remote machine tier") {
		t.Fatalf("expected invalid tier error, got %v", err)
	}
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

func TestRemoteSyncRequiresExplicitDirection(t *testing.T) {
	err := runRemoteSync([]string{"foo"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `unknown remote sync direction "foo"`) {
		t.Fatalf("expected explicit direction error, got %v", err)
	}
}

func TestRemoteConnectRejectsCommandArgs(t *testing.T) {
	projectDir := t.TempDir()
	cfg := `[remote]
backend_url = "http://127.0.0.1:1"
token = "secret-token"
`
	if err := os.WriteFile(filepath.Join(projectDir, ".yolobox.toml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	err := runRemoteConnect([]string{"foo", "codex"}, projectDir)
	if err == nil || !strings.Contains(err.Error(), "unexpected remote connect args") {
		t.Fatalf("expected connect args rejection before backend request, got %v", err)
	}
}

func TestRemoteConnectDoesNotPrepareWorkspace(t *testing.T) {
	const token = "secret-token"
	isolateRemoteSSHHome(t)
	sessionCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/machines/foo":
			_ = json.NewEncoder(w).Encode(remoteBackendMachineResponse{
				Machine: remoteMachine{
					Name:              "foo",
					Provider:          "digitalocean",
					ProviderID:        "host-a",
					PublicIPv4:        "203.0.113.10",
					SSHUser:           "root",
					SourcePath:        "/Users/example/project",
					ProjectPath:       remoteProjectRoot,
					BootstrapComplete: true,
				},
				Status: "active",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/foo/ssh-cert":
			writeFakeRemoteSSHCertificate(t, w, r, "203.0.113.10")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/foo/sessions/yolobox/prepare":
			sessionCalls++
			var req remoteBackendSessionPrepareRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode session request: %v", err)
			}
			if req.Attach || strings.Join(req.Command, " ") != "shell" {
				t.Fatalf("unexpected session request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(remoteBackendAgentResponse{
				Machine: remoteMachine{
					Name:              "foo",
					SSHUser:           "root",
					SourcePath:        "/Users/example/project",
					ProjectPath:       remoteProjectRoot,
					BootstrapComplete: true,
				},
				Result: remoteBackendAgentResult{Status: "started_detached", RecordCommand: true},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}
	sshLog := filepath.Join(root, "ssh.log")
	fakeSSH := `#!/bin/sh
printf '%s\n' "$*" >> "$YOLOBOX_FAKE_SSH_LOG"
case "$*" in
  *"tmux has-session"*) exit 1 ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(fakeBin, "ssh"), []byte(fakeSSH), 0755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "rsync"), []byte("#!/bin/sh\nexit 42\n"), 0755); err != nil {
		t.Fatalf("write fake rsync: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YOLOBOX_FAKE_SSH_LOG", sshLog)
	t.Setenv(remoteBackendURLEnv, ts.URL)
	t.Setenv(remoteAuthTokenEnv, token)

	if err := runRemoteConnect([]string{"foo"}, t.TempDir()); err != nil {
		t.Fatalf("remote connect failed: %v", err)
	}
	if sessionCalls != 1 {
		t.Fatalf("expected connect to ask backend to prepare one session, got %d", sessionCalls)
	}
	logBytes, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	sshLogContent := string(logBytes)
	if strings.Contains(sshLogContent, "bash -s") {
		t.Fatalf("connect should not run bootstrap or workspace setup scripts:\n%s", sshLogContent)
	}
	if strings.Contains(sshLogContent, "tmux has-session") || strings.Contains(sshLogContent, "tmux new-session") {
		t.Fatalf("connect should leave tmux session lifecycle to the backend agent:\n%s", sshLogContent)
	}
	if strings.Contains(sshLogContent, "tmux attach-session") {
		t.Fatalf("non-terminal connect should not attach locally:\n%s", sshLogContent)
	}
}

func TestRemoteConnectFailsWhenBackendHasNotBootstrappedMachine(t *testing.T) {
	const token = "secret-token"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/machines/foo" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(remoteBackendMachineResponse{
			Machine: remoteMachine{
				Name:       "foo",
				PublicIPv4: "203.0.113.10",
				SSHUser:    "root",
			},
			Status: "active",
		})
	}))
	defer ts.Close()

	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0755); err != nil {
		t.Fatalf("create fake bin: %v", err)
	}
	sshLog := filepath.Join(root, "ssh.log")
	if err := os.WriteFile(filepath.Join(fakeBin, "ssh"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$YOLOBOX_FAKE_SSH_LOG\"\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YOLOBOX_FAKE_SSH_LOG", sshLog)
	t.Setenv(remoteBackendURLEnv, ts.URL)
	t.Setenv(remoteAuthTokenEnv, token)

	err := runRemoteConnect([]string{"foo"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "remote foo is not bootstrapped") {
		t.Fatalf("expected unbootstrapped error, got %v", err)
	}
	if data, readErr := os.ReadFile(sshLog); readErr == nil && len(data) > 0 {
		t.Fatalf("connect should fail before SSH when backend has not bootstrapped the machine:\n%s", data)
	}
}

func TestRunLoginTokenAndLogoutRevokesSession(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	logoutCalled := false
	expectedOrigin := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
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

	if err := runLogin([]string{"--backend-url", ts.URL, "--token", "session-token"}); err != nil {
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
	if !logoutCalled {
		t.Fatal("expected logout call")
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

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
