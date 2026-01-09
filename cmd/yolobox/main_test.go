package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Image != "yolobox/base:latest" {
		t.Errorf("expected default image yolobox/base:latest, got %s", cfg.Image)
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
		Memory:    "4g",
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
	if dst.Memory != "4g" {
		t.Errorf("expected memory to be 4g, got %s", dst.Memory)
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
		Image:   "test-image",
		Memory:  "2g",
		CPUs:    "1",
		Env:     []string{"FOO=bar"},
		Mounts:  []string{},
		Secrets: []string{},
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
	if !strings.Contains(argsStr, "--memory 2g") {
		t.Error("expected --memory 2g")
	}
	if !strings.Contains(argsStr, "--cpus 1") {
		t.Error("expected --cpus 1")
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
	if !strings.Contains(argsStr, "yolobox-tools:/opt/tools") {
		t.Error("expected yolobox-tools volume")
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
