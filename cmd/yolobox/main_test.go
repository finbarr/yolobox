package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Image != "ghcr.io/finbarr/yolobox:latest" {
		t.Errorf("expected default image ghcr.io/finbarr/yolobox:latest, got %s", cfg.Image)
	}
	if cfg.Runtime != "" {
		t.Errorf("expected empty default runtime, got %s", cfg.Runtime)
	}
}

func TestMergeConfig(t *testing.T) {
	dst := Config{
		Runtime: "docker",
		Image:   "old-image",
	}
	src := Config{
		Image:     "new-image",
		SSHAgent:  true,
		NoNetwork: true,
		Scratch:   true,
	}

	mergeConfig(&dst, src)

	if dst.Runtime != "docker" {
		t.Errorf("expected runtime to stay docker, got %s", dst.Runtime)
	}
	if dst.Image != "new-image" {
		t.Errorf("expected image to be new-image, got %s", dst.Image)
	}
	if !dst.SSHAgent {
		t.Error("expected SSHAgent to be true")
	}
	if !dst.NoNetwork {
		t.Error("expected NoNetwork to be true")
	}
	if !dst.Scratch {
		t.Error("expected Scratch to be true")
	}
}

func TestResolvePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	projectDir := "/project"

	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~", home},
		{"./bar", "/project/bar"},
		{"/absolute/path", "/absolute/path"},
		{"relative", "relative"}, // non-dotted relative paths pass through
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := resolvePath(tt.input, projectDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("resolvePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestResolvePathEmpty(t *testing.T) {
	_, err := resolvePath("", "/project")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestResolveMount(t *testing.T) {
	home, _ := os.UserHomeDir()
	projectDir := "/project"

	tests := []struct {
		input    string
		expected string
	}{
		{"./src:/app/src", "/project/src:/app/src"},
		{"~/secrets:/secrets:ro", filepath.Join(home, "secrets") + ":/secrets:ro"},
		{"/absolute:/dst", "/absolute:/dst"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := resolveMount(tt.input, projectDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("resolveMount(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestResolveMountInvalid(t *testing.T) {
	_, err := resolveMount("no-colon", "/project")
	if err == nil {
		t.Error("expected error for invalid mount")
	}
}

func TestResolvedRuntimeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "auto"},
		{"docker", "docker"},
		{"podman", "podman"},
		{"colima", "docker"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := resolvedRuntimeName(tt.input)
			if result != tt.expected {
				t.Errorf("resolvedRuntimeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildRunArgs(t *testing.T) {
	cfg := Config{
		Image:  "test-image",
		Env:    []string{"FOO=bar"},
		Mounts: []string{},
	}

	args, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")

	// Check essential flags are present
	if !strings.Contains(argsStr, "-it") {
		t.Error("expected -it flag for interactive mode")
	}
	if !strings.Contains(argsStr, "-w /workspace") {
		t.Error("expected -w /workspace")
	}
	if !strings.Contains(argsStr, "YOLOBOX=1") {
		t.Error("expected YOLOBOX=1 env var")
	}
	if !strings.Contains(argsStr, "FOO=bar") {
		t.Error("expected FOO=bar env var")
	}
	if !strings.Contains(argsStr, "test-image") {
		t.Error("expected test-image")
	}

	// Check volume mounts
	if !strings.Contains(argsStr, "yolobox-home:/home/yolo") {
		t.Error("expected yolobox-home volume")
	}
	if !strings.Contains(argsStr, "yolobox-cache:/var/cache") {
		t.Error("expected yolobox-cache volume")
	}
}

func TestBuildRunArgsNoYolo(t *testing.T) {
	cfg := Config{
		Image:    "test-image",
		NoYolo: true,
	}

	args, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "YOLOBOX=1") {
		t.Error("expected YOLOBOX=1 env var to be present")
	}
	if !strings.Contains(argsStr, "NO_YOLO=1") {
		t.Error("expected NO_YOLO=1 env var to be present")
	}
}

func TestBuildRunArgsNoNetwork(t *testing.T) {
	cfg := Config{
		Image:     "test-image",
		NoNetwork: true,
	}

	args, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--network none") {
		t.Error("expected --network none for NoNetwork")
	}
}

func TestBuildRunArgsReadonlyProject(t *testing.T) {
	cfg := Config{
		Image:           "test-image",
		ReadonlyProject: true,
	}

	args, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "/workspace:ro") {
		t.Error("expected /workspace:ro for ReadonlyProject")
	}
	if !strings.Contains(argsStr, "yolobox-output:/output") {
		t.Error("expected yolobox-output volume for ReadonlyProject")
	}
}

func TestBuildRunArgsNonInteractive(t *testing.T) {
	cfg := Config{
		Image: "test-image",
	}

	args, err := buildRunArgs(cfg, "/test/project", []string{"echo", "hello"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "-it") {
		t.Error("expected no -it flag for non-interactive mode")
	}
}

func TestBuildRunArgsScratch(t *testing.T) {
	cfg := Config{
		Image:   "test-image",
		Scratch: true,
	}

	args, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "yolobox-home:/home/yolo") {
		t.Error("expected no yolobox-home volume with Scratch")
	}
	if strings.Contains(argsStr, "yolobox-cache:/var/cache") {
		t.Error("expected no yolobox-cache volume with Scratch")
	}
	// Verify project mount is still present
	if !strings.Contains(argsStr, "/test/project:/workspace") {
		t.Error("expected project mount to still be present with Scratch")
	}
	// Verify no /output volume without ReadonlyProject
	if strings.Contains(argsStr, "/output") {
		t.Error("expected no /output volume without ReadonlyProject")
	}
}

func TestBuildRunArgsScratchWithReadonlyProject(t *testing.T) {
	cfg := Config{
		Image:           "test-image",
		Scratch:         true,
		ReadonlyProject: true,
	}

	args, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	// Should have anonymous /output volume, not named yolobox-output
	if strings.Contains(argsStr, "yolobox-output:/output") {
		t.Error("expected anonymous /output volume with Scratch, got named volume")
	}
	if !strings.Contains(argsStr, "-v /output") {
		t.Error("expected anonymous /output volume for readonly-project with Scratch")
	}
}

func TestParseFlagsScratch(t *testing.T) {
	cfg, rest, err := parseBaseFlags("run", []string{"--scratch", "echo", "hello"}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Scratch {
		t.Error("expected Scratch to be true after parsing --scratch flag")
	}
	if len(rest) != 2 || rest[0] != "echo" || rest[1] != "hello" {
		t.Errorf("expected remaining args [echo hello], got %v", rest)
	}
}

func TestStringSliceFlag(t *testing.T) {
	var s stringSliceFlag

	s.Set("first")
	s.Set("second")

	if len(s) != 2 {
		t.Errorf("expected 2 values, got %d", len(s))
	}
	if s[0] != "first" || s[1] != "second" {
		t.Errorf("unexpected values: %v", s)
	}
	if s.String() != "first,second" {
		t.Errorf("unexpected String(): %s", s.String())
	}
}

func TestAutoPassthroughEnvVars(t *testing.T) {
	// Check that common API keys are in the list
	expected := []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"COPILOT_GITHUB_TOKEN",
		"GITHUB_TOKEN",
		"GH_TOKEN",
	}

	for _, key := range expected {
		found := false
		for _, v := range autoPassthroughEnvVars {
			if v == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in autoPassthroughEnvVars", key)
		}
	}
}

func TestToolShortcuts(t *testing.T) {
	// Check that expected tools are shortcuts
	expected := []string{
		"claude",
		"codex",
		"gemini",
		"opencode",
		"copilot",
	}

	for _, tool := range expected {
		if !isToolShortcut(tool) {
			t.Errorf("expected %s to be a tool shortcut", tool)
		}
	}

	// Check that non-tools are not shortcuts
	nonTools := []string{"run", "help", "version", "setup", "foo"}
	for _, cmd := range nonTools {
		if isToolShortcut(cmd) {
			t.Errorf("expected %s NOT to be a tool shortcut", cmd)
		}
	}
}
