package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFakeRuntime(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake %s runtime: %v", name, err)
	}
	return path
}

func TestRuntimeAutoDetectOrder(t *testing.T) {
	darwin := runtimeAutoDetectOrder("darwin")
	if strings.Join(darwin, ",") != "container,docker,podman" {
		t.Fatalf("unexpected darwin auto-detect order: %v", darwin)
	}
	linux := runtimeAutoDetectOrder("linux")
	if strings.Join(linux, ",") != "docker,podman" {
		t.Fatalf("unexpected linux auto-detect order: %v", linux)
	}
}

func TestParseAppleContainerVersion(t *testing.T) {
	tests := []struct {
		output  string
		want    string
		wantErr bool
	}{
		{"container CLI version 1.0.0 (build: release, commit: abc1234)", "1.0.0", false},
		{"container CLI version 0.4.1", "0.4.1", false},
		{"container version 12.34.56\nextra line", "12.34.56", false},
		{"no version here", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := parseAppleContainerVersion(tt.output)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseAppleContainerVersion(%q): expected error, got %q", tt.output, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAppleContainerVersion(%q): unexpected error: %v", tt.output, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseAppleContainerVersion(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}

func TestCheckAppleContainerVersion(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeFakeRuntime(t, dir, "container-old", "#!/bin/sh\necho 'container CLI version 0.9.0 (build: release, commit: abc)'\n")
	newPath := writeFakeRuntime(t, dir, "container-new", "#!/bin/sh\necho 'container CLI version 1.2.0 (build: release, commit: def)'\n")
	brokenPath := writeFakeRuntime(t, dir, "container-broken", "#!/bin/sh\nexit 1\n")

	err := checkAppleContainerVersion(oldPath)
	if err == nil {
		t.Fatal("expected version error for container 0.9.0")
	}
	if !strings.Contains(err.Error(), "requires container >= 1.0.0") || !strings.Contains(err.Error(), "brew") {
		t.Fatalf("unexpected version error: %v", err)
	}

	if err := checkAppleContainerVersion(newPath); err != nil {
		t.Fatalf("unexpected error for container 1.2.0: %v", err)
	}

	if err := checkAppleContainerVersion(brokenPath); err == nil {
		t.Fatal("expected error for broken container binary")
	}
}

func TestCheckAppleContainerVersionMemoized(t *testing.T) {
	dir := t.TempDir()
	// The script appends to a counter file on each invocation.
	marker := filepath.Join(dir, "calls")
	path := writeFakeRuntime(t, dir, "container", "#!/bin/sh\necho x >> "+marker+"\necho 'container CLI version 1.0.0'\n")

	for i := 0; i < 3; i++ {
		if err := checkAppleContainerVersion(path); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("expected probe to run once: %v", err)
	}
	if calls := strings.Count(string(data), "x"); calls != 1 {
		t.Fatalf("expected exactly one probe invocation, got %d", calls)
	}
}

func TestResolveRuntimeAutoDetectSkipsOldAppleContainer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("auto-detect only prefers Apple container on darwin")
	}
	dir := t.TempDir()
	writeFakeRuntime(t, dir, "container", "#!/bin/sh\necho 'container CLI version 0.9.0'\n")
	dockerPath := writeFakeRuntime(t, dir, "docker", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	path, err := resolveRuntime("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != dockerPath {
		t.Fatalf("expected auto-detect to fall back to docker, got %s", path)
	}
}

func TestResolveRuntimeAutoDetectPrefersAppleContainer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("auto-detect only prefers Apple container on darwin")
	}
	dir := t.TempDir()
	containerPath := writeFakeRuntime(t, dir, "container", "#!/bin/sh\necho 'container CLI version 1.0.0'\n")
	writeFakeRuntime(t, dir, "docker", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	path, err := resolveRuntime("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != containerPath {
		t.Fatalf("expected auto-detect to prefer Apple container, got %s", path)
	}
}

func TestVolumeRemoveArgs(t *testing.T) {
	tests := []struct {
		apple bool
		force bool
		want  string
	}{
		{true, false, "volume delete a b"},
		{true, true, "volume delete a b"},
		{false, false, "volume rm a b"},
		{false, true, "volume rm -f a b"},
	}
	for _, tt := range tests {
		got := strings.Join(volumeRemoveArgs(tt.apple, tt.force, "a", "b"), " ")
		if got != tt.want {
			t.Errorf("volumeRemoveArgs(%v, %v) = %q, want %q", tt.apple, tt.force, got, tt.want)
		}
	}
}

func TestParseAppleImageDigest(t *testing.T) {
	// container 1.0 inspect format: array of {configuration, id, variants}.
	digest, err := parseAppleImageDigest([]byte(`[{"configuration":{"name":"alpine"},"id":"e8cab8ec8514","variants":[]}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest != "e8cab8ec8514" {
		t.Fatalf("unexpected digest: %q", digest)
	}

	digest, err = parseAppleImageDigest([]byte(`[{"name":"test","digest":"sha256:abc123"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest != "sha256:abc123" {
		t.Fatalf("unexpected digest: %q", digest)
	}

	// Missing digest falls back to a stable hash of the inspect output.
	fallback1, err := parseAppleImageDigest([]byte(`[{"name":"test"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fallback2, err := parseAppleImageDigest([]byte(`[{"name":"test"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fallback1 != fallback2 || !strings.HasPrefix(fallback1, "sha256:") {
		t.Fatalf("expected stable hashed fallback, got %q vs %q", fallback1, fallback2)
	}

	if _, err := parseAppleImageDigest([]byte("not json")); err == nil {
		t.Fatal("expected error for non-JSON output")
	}
	if _, err := parseAppleImageDigest([]byte("[]")); err == nil {
		t.Fatal("expected error for empty inspect output")
	}
}

func setupFakeAppleContainerRuntime(t *testing.T) {
	t.Helper()
	runtimeDir := t.TempDir()
	writeFakeRuntime(t, runtimeDir, "container", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestBuildRunArgsAppleContainerDirectFileMounts(t *testing.T) {
	setupFakeAppleContainerRuntime(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	gitConfig := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitConfig, []byte("[user]\n\tname = Test\n"), 0644); err != nil {
		t.Fatalf("failed to write .gitconfig: %v", err)
	}

	cfg := Config{
		Runtime:   "container",
		Image:     "test-image",
		GitConfig: true,
	}

	args, _, err := buildRunArgs(cfg, t.TempDir(), []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, gitConfig+":/host-git/.gitconfig:ro") {
		t.Fatalf("expected direct .gitconfig file mount, got %s", argsStr)
	}
	if strings.Contains(argsStr, "/host-files") {
		t.Fatalf("did not expect staged /host-files mount, got %s", argsStr)
	}
	if strings.Contains(argsStr, "YOLOBOX_HOST_FILES") {
		t.Fatalf("did not expect YOLOBOX_HOST_FILES env var, got %s", argsStr)
	}
}

func TestBuildRunArgsAppleContainerScratchOutputTmpfs(t *testing.T) {
	setupFakeAppleContainerRuntime(t)

	cfg := Config{
		Runtime:         "container",
		Image:           "test-image",
		ReadonlyProject: true,
		Scratch:         true,
	}

	args, _, err := buildRunArgs(cfg, t.TempDir(), []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--tmpfs /output") {
		t.Fatalf("expected tmpfs /output for Apple container scratch mode, got %s", argsStr)
	}
	if strings.Contains(argsStr, "-v /output") {
		t.Fatalf("did not expect anonymous /output volume for Apple container, got %s", argsStr)
	}
}

func TestBuildRunArgsAppleContainerDefaultMemory(t *testing.T) {
	setupFakeAppleContainerRuntime(t)

	cfg := Config{
		Runtime: "container",
		Image:   "test-image",
	}
	args, _, err := buildRunArgs(cfg, t.TempDir(), []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.Join(args, " "), "--memory 4g") {
		t.Fatalf("expected default --memory 4g for Apple container, got %s", strings.Join(args, " "))
	}

	cfg.Memory = "2g"
	args, _, err = buildRunArgs(cfg, t.TempDir(), []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--memory 2g") || strings.Contains(argsStr, "--memory 4g") {
		t.Fatalf("expected explicit memory to win, got %s", argsStr)
	}
}

func TestBuildRunArgsDockerNoDefaultMemory(t *testing.T) {
	runtimeDir := t.TempDir()
	writeFakeRuntime(t, runtimeDir, "docker", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := Config{
		Runtime: "docker",
		Image:   "test-image",
	}
	args, _, err := buildRunArgs(cfg, t.TempDir(), []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(strings.Join(args, " "), "--memory") {
		t.Fatalf("did not expect default memory for docker/podman, got %s", strings.Join(args, " "))
	}
}

func TestValidateRuntimeConstraintsAppleContainer(t *testing.T) {
	setupFakeAppleContainerRuntime(t)

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"gpus", Config{Runtime: "container", GPUs: "all"}, "--gpus"},
		{"devices", Config{Runtime: "container", Devices: []string{"/dev/null"}}, "--device"},
		{"docker socket", Config{Runtime: "container", Docker: true}, "--docker"},
		{"clean", Config{Runtime: "container"}, ""},
	}
	for _, tt := range tests {
		err := validateRuntimeConstraints(tt.cfg)
		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("%s: unexpected error: %v", tt.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: expected error mentioning %q, got %v", tt.name, tt.wantErr, err)
		}
	}
}

func TestGenerateCustomDockerfileWithoutCacheMounts(t *testing.T) {
	dockerfile, err := generateCustomDockerfile("base", []string{"ripgrep"}, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(dockerfile, "--mount=type=cache") {
		t.Fatalf("did not expect BuildKit cache mounts:\n%s", dockerfile)
	}
	if strings.Contains(dockerfile, "# syntax=") {
		t.Fatalf("did not expect BuildKit syntax directive:\n%s", dockerfile)
	}
	if !strings.Contains(dockerfile, "RUN apt-get update && apt-get install -y --no-install-recommends ripgrep") {
		t.Fatalf("expected plain apt-get install:\n%s", dockerfile)
	}
}

func TestHostBridgeHostNameAppleContainer(t *testing.T) {
	setupFakeAppleContainerRuntime(t)
	got := hostBridgeHostName("container")
	// Apple container has no host.docker.internal-style hostname, so the
	// bridge endpoint uses the host's LAN IP when one is available.
	if want := firstNonLoopbackIPv4(); want != "" {
		if got != want {
			t.Fatalf("expected host LAN IP %q for Apple container, got %q", want, got)
		}
	} else if got != "host.containers.internal" {
		t.Fatalf("unexpected fallback hostname for Apple container: %q", got)
	}
}
