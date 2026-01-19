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

func TestValidateShell(t *testing.T) {
	tests := []struct {
		shell   string
		wantErr bool
	}{
		{"", false},              // empty is valid (resolved to default elsewhere)
		{"bash", false},          // valid
		{"fish", false},          // valid
		{"zsh", false},           // valid
		{"sh", true},             // not in whitelist
		{"/bin/fish", true},      // absolute path rejected
		{"bash -c id", true},     // injection attempt
		{"bash\nid", true},       // newline injection
		{"BASH", true},           // case sensitive - uppercase rejected
		{"Fish", true},           // case sensitive - mixed case rejected
		{"FISH", true},           // case sensitive - uppercase rejected
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			err := validateShell(tt.shell)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateShell(%q) error = %v, wantErr %v", tt.shell, err, tt.wantErr)
			}
		})
	}
}

func TestMergeConfigShell(t *testing.T) {
	// Test shell merging
	dst := Config{Shell: "bash"}
	src := Config{Shell: "fish"}
	mergeConfig(&dst, src)
	if dst.Shell != "fish" {
		t.Errorf("expected shell 'fish', got %q", dst.Shell)
	}

	// Empty doesn't override
	dst2 := Config{Shell: "fish"}
	src2 := Config{Shell: ""}
	mergeConfig(&dst2, src2)
	if dst2.Shell != "fish" {
		t.Errorf("empty shell should not override, got %q", dst2.Shell)
	}
}

func TestDefaultConfigShell(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Shell != "" {
		t.Errorf("expected default shell to be empty, got %q", cfg.Shell)
	}
}

func TestParseBaseFlagsShell(t *testing.T) {
	cfg, rest, err := parseBaseFlags("run", []string{"--shell", "fish", "echo", "hi"}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Shell != "fish" {
		t.Errorf("expected shell 'fish', got %q", cfg.Shell)
	}
	if len(rest) != 2 {
		t.Errorf("expected 2 remaining args, got %d", len(rest))
	}
}

func TestParseBaseFlagsInvalidShell(t *testing.T) {
	_, _, err := parseBaseFlags("run", []string{"--shell", "tcsh"}, t.TempDir())
	if err == nil {
		t.Error("expected error for invalid shell")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("expected 'unsupported shell' error, got %v", err)
	}
}

func TestMergeConfigFileInvalidShell(t *testing.T) {
	// Test that invalid shell in config returns error
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write invalid shell to config
	if err := os.WriteFile(configPath, []byte(`shell = "tcsh"`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	err := mergeConfigFile(configPath, &cfg)

	if err == nil {
		t.Error("expected error for invalid shell in config")
	}
	if !strings.Contains(err.Error(), "invalid shell") {
		t.Errorf("expected 'invalid shell' error, got %v", err)
	}
}

func TestMergeConfigFileValidShell(t *testing.T) {
	// Test that valid shell in config is accepted
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	if err := os.WriteFile(configPath, []byte(`shell = "fish"`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	err := mergeConfigFile(configPath, &cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Shell != "fish" {
		t.Errorf("expected shell 'fish', got %q", cfg.Shell)
	}
}

func TestShellFlagOverridesProjectConfig(t *testing.T) {
	// Test that --shell flag overrides project config
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ".yolobox.toml"), []byte(`shell = "bash"`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, rest, err := parseBaseFlags("run", []string{"--shell", "fish", "echo", "hi"}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Shell != "fish" {
		t.Errorf("expected CLI flag to override config, got %q", cfg.Shell)
	}
	// Verify remaining args parsed correctly
	if len(rest) != 2 {
		t.Errorf("expected 2 remaining args, got %d", len(rest))
	}
	if len(rest) >= 2 && (rest[0] != "echo" || rest[1] != "hi") {
		t.Errorf("unexpected remaining args: %v", rest)
	}
}

func TestResolveShell(t *testing.T) {
	tests := []struct {
		name            string
		cfgShell        string
		shellEnv        string
		wantShell       string
		wantDetected    bool
		wantUnsupported string
	}{
		// No config, no $SHELL -> default bash
		{"default bash", "", "", "bash", false, ""},
		// No config, valid $SHELL -> detected
		{"detect bash", "", "/bin/bash", "bash", true, ""},
		{"detect fish", "", "/usr/bin/fish", "fish", true, ""},
		{"detect fish custom path", "", "/opt/homebrew/bin/fish", "fish", true, ""},
		{"detect zsh", "", "/bin/zsh", "zsh", true, ""},
		{"detect zsh usr", "", "/usr/bin/zsh", "zsh", true, ""},
		// No config, invalid $SHELL -> unsupported
		{"sh unsupported", "", "/bin/sh", "bash", false, "sh"},
		{"tcsh unsupported", "", "/bin/tcsh", "bash", false, "tcsh"},
		{"csh unsupported", "", "/bin/csh", "bash", false, "csh"},
		// Explicit config overrides everything
		{"config bash ignores env", "bash", "/usr/bin/fish", "bash", false, ""},
		{"config fish ignores env", "fish", "/bin/bash", "fish", false, ""},
		{"config fish no env", "fish", "", "fish", false, ""},
		{"config zsh ignores env", "zsh", "/bin/bash", "zsh", false, ""},
		{"config zsh no env", "zsh", "", "zsh", false, ""},
		// Edge cases for $SHELL parsing
		{"trailing slash", "", "/usr/bin/fish/", "fish", true, ""},
		{"double slash", "", "/usr/bin//fish", "fish", true, ""},
		{"just basename", "", "fish", "fish", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Shell: tt.cfgShell}
			res := resolveShell(cfg, tt.shellEnv)

			if res.shell != tt.wantShell {
				t.Errorf("resolveShell().shell = %q, want %q", res.shell, tt.wantShell)
			}
			if res.detected != tt.wantDetected {
				t.Errorf("resolveShell().detected = %v, want %v", res.detected, tt.wantDetected)
			}
			if res.unsupported != tt.wantUnsupported {
				t.Errorf("resolveShell().unsupported = %q, want %q", res.unsupported, tt.wantUnsupported)
			}
		})
	}
}

func TestShellResolutionString(t *testing.T) {
	tests := []struct {
		name string
		res  shellResolution
		want string
	}{
		{"detected fish", shellResolution{shell: "fish", detected: true}, "fish (detected from $SHELL)"},
		{"detected bash", shellResolution{shell: "bash", detected: true}, "bash (detected from $SHELL)"},
		{"unsupported tcsh", shellResolution{shell: "bash", unsupported: "tcsh"}, "bash (default - tcsh not supported)"},
		{"explicit fish", shellResolution{shell: "fish"}, "fish"},
		{"default bash", shellResolution{shell: "bash"}, "bash (default)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.res.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
