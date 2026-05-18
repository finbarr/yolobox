package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteRegistryRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	now := time.Now().UTC()
	reg := remoteRegistry{
		Machines: map[string]remoteMachine{
			"foo": {
				Name:        "foo",
				Provider:    remoteProviderDigitalOcean,
				DropletID:   "123",
				PublicIPv4:  "203.0.113.10",
				SSHUser:     "root",
				ProjectPath: "/root/yolobox-projects/app",
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
	}
	if err := saveRemoteRegistry(reg); err != nil {
		t.Fatalf("saveRemoteRegistry failed: %v", err)
	}

	loaded, err := loadRemoteRegistry()
	if err != nil {
		t.Fatalf("loadRemoteRegistry failed: %v", err)
	}
	machine, ok := loaded.Machines["foo"]
	if !ok {
		t.Fatal("expected foo in remote registry")
	}
	if machine.DropletID != "123" || machine.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("unexpected registry machine: %+v", machine)
	}
	workspace, ok := loaded.Workspaces[remoteWorkspaceID("foo", remoteDefaultWorkspace)]
	if !ok {
		t.Fatal("expected legacy machine project to migrate to default workspace")
	}
	if workspace.ProjectPath != "/root/yolobox-projects/app" {
		t.Fatalf("unexpected migrated workspace: %+v", workspace)
	}
}

func TestParseDropletInfo(t *testing.T) {
	info, err := parseDropletInfo("123456 yolobox-foo 203.0.113.10 active\n")
	if err != nil {
		t.Fatalf("parseDropletInfo failed: %v", err)
	}
	if info.ID != "123456" || info.Name != "yolobox-foo" || info.PublicIPv4 != "203.0.113.10" || info.Status != "active" {
		t.Fatalf("unexpected droplet info: %+v", info)
	}
}

func TestCreateDigitalOceanDropletUsesDoctlFlags(t *testing.T) {
	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "doctl-args")
	doctlPath := filepath.Join(binDir, "doctl")
	script := `#!/bin/sh
: > "$YOLOBOX_FAKE_DOCTL_ARGS"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$YOLOBOX_FAKE_DOCTL_ARGS"
done
echo "123456 yolobox-foo 203.0.113.10 active"
`
	if err := os.WriteFile(doctlPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake doctl: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YOLOBOX_FAKE_DOCTL_ARGS", argsPath)

	info, err := createDigitalOceanDroplet(remoteProvisionOptions{
		Name:    "foo",
		Region:  "nyc3",
		Size:    "s-2vcpu-4gb",
		Image:   "ubuntu-24-04-x64",
		SSHKey:  "key123",
		SSHUser: "root",
	}, "yolobox-foo")
	if err != nil {
		t.Fatalf("createDigitalOceanDroplet failed: %v", err)
	}
	if info.ID != "123456" || info.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("unexpected droplet info: %+v", info)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("failed to read doctl args: %v", err)
	}
	args := string(data)
	for _, want := range []string{
		"compute\n",
		"droplet\n",
		"create\n",
		"yolobox-foo\n",
		"--size\ns-2vcpu-4gb\n",
		"--image\nubuntu-24-04-x64\n",
		"--region\nnyc3\n",
		"--ssh-keys\nkey123\n",
		"--tag-names\nyolobox,yolobox-remote\n",
		"--user-data-file\n",
		"--wait\n",
		"--format\nID,Name,PublicIPv4,Status\n",
		"--no-header\n",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected doctl args to contain %q, got:\n%s", want, args)
		}
	}
}

func TestBuildRemoteSetupScript(t *testing.T) {
	workspace := remoteWorkspace{
		Name:        "default",
		ProjectPath: "/root/yolobox-projects/yolobox",
	}
	script := buildRemoteSetupScript(workspace, []string{"docker compose pull"})

	for _, want := range []string{
		"cd '/root/yolobox-projects/yolobox'",
		"docker compose pull",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected setup script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestRsyncProjectToRemoteUsesFullDirectory(t *testing.T) {
	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "rsync-args")
	rsyncPath := filepath.Join(binDir, "rsync")
	script := `#!/bin/sh
: > "$YOLOBOX_FAKE_RSYNC_ARGS"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$YOLOBOX_FAKE_RSYNC_ARGS"
done
exit 0
`
	if err := os.WriteFile(rsyncPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake rsync: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YOLOBOX_FAKE_RSYNC_ARGS", argsPath)

	source := t.TempDir()
	machine := remoteMachine{
		PublicIPv4:  "203.0.113.10",
		SSHUser:     "root",
		ProjectPath: "/root/yolobox-projects/yolobox",
	}
	if err := rsyncProjectToRemote(machine, source); err != nil {
		t.Fatalf("rsyncProjectToRemote failed: %v", err)
	}

	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("failed to read fake rsync args: %v", err)
	}
	args := string(data)
	for _, want := range []string{
		"-az\n",
		"--delete\n",
		"-e\n",
		source + string(os.PathSeparator) + "\n",
		"root@203.0.113.10:/root/yolobox-projects/yolobox/\n",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected rsync args to contain %q, got:\n%s", want, args)
		}
	}
}

func TestEnsureRemoteMachineRequiresRsyncBeforeCreate(t *testing.T) {
	projectDir := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", t.TempDir())

	binDir := t.TempDir()
	doctlPath := filepath.Join(binDir, "doctl")
	doctlMarker := filepath.Join(t.TempDir(), "doctl-called")
	doctlScript := `#!/bin/sh
touch "$YOLOBOX_FAKE_DOCTL_CALLED"
echo "unexpected doctl call" >&2
exit 1
`
	if err := os.WriteFile(doctlPath, []byte(doctlScript), 0755); err != nil {
		t.Fatalf("failed to write fake doctl: %v", err)
	}
	sshPath := filepath.Join(binDir, "ssh")
	if err := os.WriteFile(sshPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write fake ssh: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("YOLOBOX_FAKE_DOCTL_CALLED", doctlMarker)

	cfg := defaultConfig()
	_, err := ensureRemoteMachine(cfg, projectDir, remoteProvisionOptions{
		Name:    "foo",
		Region:  "nyc3",
		Size:    "s-2vcpu-4gb",
		Image:   "ubuntu-24-04-x64",
		SSHKey:  "key123",
		SSHUser: "root",
	})
	if err == nil || !strings.Contains(err.Error(), "rsync is required") {
		t.Fatalf("expected missing rsync error, got %v", err)
	}
	if _, err := os.Stat(doctlMarker); !os.IsNotExist(err) {
		t.Fatalf("expected doctl not to be called, marker stat error: %v", err)
	}
}

func TestRemoteTmuxCommand(t *testing.T) {
	workspace := remoteWorkspace{ProjectPath: "/root/yolobox-projects/my app"}
	command := remoteTmuxCommand(workspace, "yolobox-foo-default-main", []string{"yolobox", "codex", "--resume"})

	for _, want := range []string{
		"tmux new-session -A -s 'yolobox-foo-default-main'",
		"-c '/root/yolobox-projects/my app'",
		"yolobox",
		"codex",
		"--resume",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("expected tmux command to contain %q, got %q", want, command)
		}
	}
}

func TestValidateRemoteProvisionOptions(t *testing.T) {
	err := validateRemoteProvisionOptions(remoteProvisionOptions{
		Name:     "foo",
		Provider: "aws",
		Region:   "nyc3",
		Size:     "s-2vcpu-4gb",
		Image:    "ubuntu-24-04-x64",
		SSHKey:   "abc123",
		SSHUser:  "root",
	})
	if err == nil || !strings.Contains(err.Error(), "digitalocean") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestLoadConfigRemoteDefaults(t *testing.T) {
	projectDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	globalConfigDir := filepath.Join(configHome, "yolobox")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("failed to create global config dir: %v", err)
	}
	config := `mode = "remote"
remote_name = "foo"

[remote]
region = "sfo3"
size = "s-4vcpu-8gb"
ssh_key = "abc123"
setup = ["docker compose pull"]
`
	if err := os.WriteFile(filepath.Join(globalConfigDir, "config.toml"), []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := loadConfig(projectDir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg.Mode != "remote" || cfg.RemoteName != "foo" {
		t.Fatalf("expected remote mode/name, got mode=%q name=%q", cfg.Mode, cfg.RemoteName)
	}
	if cfg.Remote.Provider != remoteProviderDigitalOcean {
		t.Fatalf("expected default provider, got %q", cfg.Remote.Provider)
	}
	if cfg.Remote.Region != "sfo3" || cfg.Remote.Size != "s-4vcpu-8gb" || cfg.Remote.SSHKey != "abc123" {
		t.Fatalf("unexpected remote config: %+v", cfg.Remote)
	}
	if len(cfg.Remote.Setup) != 1 || cfg.Remote.Setup[0] != "docker compose pull" {
		t.Fatalf("unexpected remote setup: %+v", cfg.Remote.Setup)
	}
}

func TestSaveGlobalConfigRemote(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	cfg := defaultConfig()
	cfg.Mode = "remote"
	cfg.RemoteName = "Foo"
	cfg.RemoteWorkspace = "App"
	cfg.DefaultHarness = "codex"
	cfg.Remote.SSHKey = "abc123"
	cfg.Remote.Setup = []string{"docker compose pull"}
	if err := saveGlobalConfig(cfg); err != nil {
		t.Fatalf("saveGlobalConfig failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configHome, "yolobox", "config.toml"))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		`mode = "remote"`,
		`remote_name = "foo"`,
		`remote_workspace = "app"`,
		`default_harness = "codex"`,
		`[remote]`,
		`provider = "digitalocean"`,
		`ssh_key = "abc123"`,
		`setup = ["docker compose pull"]`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected saved config to contain %q, got:\n%s", want, content)
		}
	}
}

func TestDefaultRemoteModeAttachesRegisteredMachine(t *testing.T) {
	projectDir := t.TempDir()
	configHome := t.TempDir()
	stateHome := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", home)

	globalConfigDir := filepath.Join(configHome, "yolobox")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	config := "mode = \"remote\"\nremote_name = \"foo\"\ndefault_harness = \"codex\"\n"
	if err := os.WriteFile(filepath.Join(globalConfigDir, "config.toml"), []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	now := time.Now().UTC()
	if err := saveRemoteRegistry(remoteRegistry{Machines: map[string]remoteMachine{
		"foo": {
			Name:        "foo",
			Provider:    remoteProviderDigitalOcean,
			PublicIPv4:  "203.0.113.10",
			SSHUser:     "root",
			ProjectPath: "/root/yolobox-projects/app",
			RepoURL:     "git@github.com:finbarr/app.git",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}}); err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	binDir := t.TempDir()
	sshArgsPath := filepath.Join(t.TempDir(), "ssh-args")
	sshPath := filepath.Join(binDir, "ssh")
	script := `#!/bin/sh
: > "$YOLOBOX_FAKE_SSH_ARGS"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$YOLOBOX_FAKE_SSH_ARGS"
done
exit 0
`
	if err := os.WriteFile(sshPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake ssh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YOLOBOX_FAKE_SSH_ARGS", sshArgsPath)

	if err := runCmdArgs(nil, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs failed: %v", err)
	}
	data, err := os.ReadFile(sshArgsPath)
	if err != nil {
		t.Fatalf("failed to read fake ssh args: %v", err)
	}
	args := string(data)
	for _, want := range []string{
		"-A\n",
		"root@203.0.113.10\n",
		"tmux new-session -A -s 'yolobox-foo-default-main'",
		"yolobox",
		"codex",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected ssh args to contain %q, got:\n%s", want, args)
		}
	}
}

func TestParseRemoteRefSupportsWorkspace(t *testing.T) {
	ref, err := parseRemoteRef("Foo/App", remoteDefaultWorkspace)
	if err != nil {
		t.Fatalf("parseRemoteRef failed: %v", err)
	}
	if ref.Machine != "foo" || ref.Workspace != "app" {
		t.Fatalf("unexpected remote ref: %+v", ref)
	}

	ref, err = parseRemoteRef("foo", "Review")
	if err != nil {
		t.Fatalf("parseRemoteRef with default workspace failed: %v", err)
	}
	if ref.Machine != "foo" || ref.Workspace != "review" {
		t.Fatalf("unexpected default workspace ref: %+v", ref)
	}
}

func TestParseRemoteForwardUsesConfiguredTargetForPortOnly(t *testing.T) {
	cfg := defaultConfig()
	cfg.RemoteName = "foo"
	cfg.RemoteWorkspace = "app"

	ref, localPort, remotePort, err := parseRemoteForwardArgs([]string{"3000", "--local-port", "13000"}, cfg)
	if err != nil {
		t.Fatalf("parseRemoteForwardArgs failed: %v", err)
	}
	if ref.Machine != "foo" || ref.Workspace != "app" {
		t.Fatalf("unexpected remote ref: %+v", ref)
	}
	if localPort != 13000 || remotePort != 3000 {
		t.Fatalf("unexpected ports: local=%d remote=%d", localPort, remotePort)
	}
}

func TestParseRemoteExposeUsesConfiguredTargetForPortOnly(t *testing.T) {
	cfg := defaultConfig()
	cfg.RemoteName = "foo"
	cfg.RemoteWorkspace = "app"

	ref, port, err := parseRemoteExposeArgs([]string{"3000"}, cfg)
	if err != nil {
		t.Fatalf("parseRemoteExposeArgs failed: %v", err)
	}
	if ref.Machine != "foo" || ref.Workspace != "app" {
		t.Fatalf("unexpected remote ref: %+v", ref)
	}
	if port != "3000" {
		t.Fatalf("unexpected port: %s", port)
	}
}

func TestRemoteStatusUsesConfiguredTarget(t *testing.T) {
	projectDir := t.TempDir()
	configHome := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	if err := os.WriteFile(filepath.Join(projectDir, ".yolobox.toml"), []byte("remote_name = \"foo\"\nremote_workspace = \"app\"\n"), 0644); err != nil {
		t.Fatalf("failed to write project config: %v", err)
	}

	now := time.Now().UTC()
	workspace := remoteWorkspace{
		ID:          remoteWorkspaceID("foo", "app"),
		Name:        "app",
		Machine:     "foo",
		ProjectPath: "/root/yolobox-workspaces/foo-app/project",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := saveRemoteRegistry(remoteRegistry{
		Machines: map[string]remoteMachine{
			"foo": {
				Name:       "foo",
				Provider:   remoteProviderDigitalOcean,
				PublicIPv4: "203.0.113.10",
				SSHUser:    "root",
				CreatedAt:  now,
				UpdatedAt:  now,
			},
		},
		Workspaces: map[string]remoteWorkspace{workspace.ID: workspace},
	}); err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	if err := runRemoteStatus(nil, projectDir); err != nil {
		t.Fatalf("runRemoteStatus failed: %v", err)
	}
}

func TestRemoteSyncDownRequiresForce(t *testing.T) {
	projectDir := t.TempDir()
	configHome := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", t.TempDir())

	now := time.Now().UTC()
	workspace := remoteWorkspace{
		ID:          remoteWorkspaceID("foo", remoteDefaultWorkspace),
		Name:        remoteDefaultWorkspace,
		Machine:     "foo",
		ProjectPath: "/root/yolobox-workspaces/foo-default/project",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := saveRemoteRegistry(remoteRegistry{
		Machines: map[string]remoteMachine{
			"foo": {
				Name:       "foo",
				Provider:   remoteProviderDigitalOcean,
				PublicIPv4: "203.0.113.10",
				SSHUser:    "root",
				CreatedAt:  now,
				UpdatedAt:  now,
			},
		},
		Workspaces: map[string]remoteWorkspace{workspace.ID: workspace},
	}); err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	err := runRemoteSync([]string{"down", "foo"}, projectDir)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected --force error, got %v", err)
	}
}

func TestRunSSHForwardUsesLocalPortMapping(t *testing.T) {
	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "ssh-args")
	sshPath := filepath.Join(binDir, "ssh")
	script := `#!/bin/sh
: > "$YOLOBOX_FAKE_SSH_ARGS"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$YOLOBOX_FAKE_SSH_ARGS"
done
exit 0
`
	if err := os.WriteFile(sshPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake ssh: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YOLOBOX_FAKE_SSH_ARGS", argsPath)

	machine := remoteMachine{PublicIPv4: "203.0.113.10", SSHUser: "root"}
	if err := runSSHForward(machine, 13000, 3000); err != nil {
		t.Fatalf("runSSHForward failed: %v", err)
	}

	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("failed to read fake ssh args: %v", err)
	}
	args := string(data)
	for _, want := range []string{
		"-N\n",
		"-L\n",
		"127.0.0.1:13000:127.0.0.1:3000\n",
		"root@203.0.113.10\n",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected ssh args to contain %q, got:\n%s", want, args)
		}
	}
}
