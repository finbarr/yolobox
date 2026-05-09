package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/term"
)

func argEnvValue(args []string, key string) (string, bool) {
	prefix := key + "="
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix), true
		}
	}
	return "", false
}

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
		Image:          "new-image",
		SSHAgent:       true,
		NoNetwork:      true,
		Scratch:        true,
		CodexConfig:    true,
		OpencodeConfig: true,
		Clipboard:      true,
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
	if !dst.CodexConfig {
		t.Error("expected CodexConfig to be true")
	}
	if !dst.OpencodeConfig {
		t.Error("expected OpencodeConfig to be true")
	}
	if !dst.Clipboard {
		t.Error("expected Clipboard to be true")
	}
}

func TestMergeConfigCustomize(t *testing.T) {
	dst := Config{}
	src := Config{
		Customize: CustomizeConfig{
			Packages:   []string{"maven", "default-jdk"},
			Dockerfile: ".yolobox.Dockerfile",
		},
	}

	mergeConfig(&dst, src)

	if len(dst.Customize.Packages) != 2 {
		t.Fatalf("expected 2 customize packages, got %v", dst.Customize.Packages)
	}
	if dst.Customize.Dockerfile != ".yolobox.Dockerfile" {
		t.Fatalf("expected customize dockerfile to merge, got %q", dst.Customize.Dockerfile)
	}
}

func TestMergeConfigProjectFiltering(t *testing.T) {
	dst := Config{}
	src := Config{
		Exclude: []string{".env*", "secrets/**"},
		CopyAs:  []string{".env.sandbox:.env"},
	}

	mergeConfig(&dst, src)

	expectSliceEqual(t, dst.Exclude, []string{".env*", "secrets/**"})
	expectSliceEqual(t, dst.CopyAs, []string{".env.sandbox:.env"})
}

func TestLoadSetupDefaultsIgnoresProjectConfig(t *testing.T) {
	projectDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	globalConfigDir := filepath.Join(configHome, "yolobox")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("failed to create global config dir: %v", err)
	}
	globalConfigPath := filepath.Join(globalConfigDir, "config.toml")
	if err := os.WriteFile(globalConfigPath, []byte("docker = true\nmemory = \"4g\"\n"), 0644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}

	projectConfigPath := filepath.Join(projectDir, ".yolobox.toml")
	projectConfig := "no_network = true\npod = \"dev-pod\"\n[customize]\npackages = [\"cowsay\"]\n"
	if err := os.WriteFile(projectConfigPath, []byte(projectConfig), 0644); err != nil {
		t.Fatalf("failed to write project config: %v", err)
	}

	cfg, err := loadSetupDefaults()
	if err != nil {
		t.Fatalf("loadSetupDefaults failed: %v", err)
	}

	if !cfg.Docker {
		t.Fatal("expected global docker setting to be loaded")
	}
	if cfg.Memory != "4g" {
		t.Fatalf("expected global memory setting, got %q", cfg.Memory)
	}
	if cfg.NoNetwork {
		t.Fatal("did not expect project no_network to affect setup defaults")
	}
	if cfg.Pod != "" {
		t.Fatalf("did not expect project pod to affect setup defaults, got %q", cfg.Pod)
	}
	if len(cfg.Customize.Packages) != 0 {
		t.Fatalf("did not expect project customize packages in setup defaults, got %v", cfg.Customize.Packages)
	}
}

func TestLoadConfigCodexConfig(t *testing.T) {
	projectDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	globalConfigDir := filepath.Join(configHome, "yolobox")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("failed to create global config dir: %v", err)
	}
	globalConfigPath := filepath.Join(globalConfigDir, "config.toml")
	if err := os.WriteFile(globalConfigPath, []byte("codex_config = true\n"), 0644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}

	cfg, err := loadConfig(projectDir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if !cfg.CodexConfig {
		t.Fatal("expected codex_config to load from config file")
	}
}

func TestLoadConfigOpencodeConfig(t *testing.T) {
	projectDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	globalConfigDir := filepath.Join(configHome, "yolobox")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("failed to create global config dir: %v", err)
	}
	globalConfigPath := filepath.Join(globalConfigDir, "config.toml")
	if err := os.WriteFile(globalConfigPath, []byte("opencode_config = true\n"), 0644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}

	cfg, err := loadConfig(projectDir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if !cfg.OpencodeConfig {
		t.Fatal("expected opencode_config to load from config file")
	}
}

func TestSaveGlobalConfigToolConfigs(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	cfg := Config{
		ClaudeConfig:   true,
		CodexConfig:    true,
		GeminiConfig:   true,
		OpencodeConfig: true,
	}
	if err := saveGlobalConfig(cfg); err != nil {
		t.Fatalf("saveGlobalConfig failed: %v", err)
	}

	path := filepath.Join(configHome, "yolobox", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"claude_config = true",
		"codex_config = true",
		"gemini_config = true",
		"opencode_config = true",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected saved config to contain %q, got:\n%s", want, content)
		}
	}
}

func TestLoadConfigClipboard(t *testing.T) {
	projectDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	globalConfigDir := filepath.Join(configHome, "yolobox")
	if err := os.MkdirAll(globalConfigDir, 0755); err != nil {
		t.Fatalf("failed to create global config dir: %v", err)
	}
	globalConfigPath := filepath.Join(globalConfigDir, "config.toml")
	if err := os.WriteFile(globalConfigPath, []byte("clipboard = true\n"), 0644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}

	cfg, err := loadConfig(projectDir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if !cfg.Clipboard {
		t.Fatal("expected clipboard to load from config file")
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

func TestMatchProjectPattern(t *testing.T) {
	tests := []struct {
		pattern string
		rel     string
		want    bool
	}{
		{pattern: ".env*", rel: ".env", want: true},
		{pattern: ".env*", rel: "config/.env", want: false},
		{pattern: "secrets/**", rel: "secrets", want: true},
		{pattern: "secrets/**", rel: "secrets/nested/token.txt", want: true},
		{pattern: "**/*.pem", rel: "certs/dev/key.pem", want: true},
		{pattern: "**/*.pem", rel: "certs/dev/key.txt", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"::"+tt.rel, func(t *testing.T) {
			got := matchProjectPattern(tt.pattern, tt.rel)
			if got != tt.want {
				t.Fatalf("matchProjectPattern(%q, %q) = %t, want %t", tt.pattern, tt.rel, got, tt.want)
			}
		})
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

	args, cleanupPaths, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")

	// Check essential flags are present
	if !strings.Contains(argsStr, "-it") {
		t.Error("expected -it flag for interactive mode")
	}
	if !strings.Contains(argsStr, "-w /test/project") {
		t.Error("expected -w /test/project (workdir should be actual project path)")
	}
	if !strings.Contains(argsStr, "YOLOBOX=1") {
		t.Error("expected YOLOBOX=1 env var")
	}
	if !strings.Contains(argsStr, "YOLOBOX_PROJECT_PATH=/test/project") {
		t.Error("expected YOLOBOX_PROJECT_PATH env var")
	}
	if !strings.Contains(argsStr, "YOLOBOX_CONTEXT_FILE=/run/yolobox/context.json") {
		t.Error("expected YOLOBOX_CONTEXT_FILE env var")
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
	if _, ok := argEnvValue(args, yoloboxContextPayloadEnv); !ok {
		t.Errorf("expected %s env var", yoloboxContextPayloadEnv)
	}
	if strings.Contains(argsStr, ":/run/yolobox:ro") {
		t.Error("did not expect context manifest bind mount")
	}
	if len(cleanupPaths) != 0 {
		t.Fatalf("did not expect cleanup paths for basic run args, got %v", cleanupPaths)
	}

	// Verify no --network flag when using default network
	if strings.Contains(argsStr, "--network") {
		t.Error("expected no --network flag for default network behavior")
	}
}

func TestBuildRunArgsNoProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	projectDir := t.TempDir()

	cfg := Config{
		Image:     "test-image",
		NoProject: true,
	}

	args, _, err := buildRunArgs(cfg, projectDir, []string{"bash"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")

	if strings.Contains(argsStr, "-w "+projectDir) {
		t.Error("--no-project should skip workdir")
	}
	if strings.Contains(argsStr, "YOLOBOX_PROJECT_PATH") {
		t.Error("--no-project should skip YOLOBOX_PROJECT_PATH")
	}
	if !strings.Contains(argsStr, "YOLOBOX_HOST_UID") {
		t.Error("--no-project should still pass host UID for persistent home volume ownership")
	}
	for i, arg := range args {
		if arg == "-v" && i+1 < len(args) && strings.Contains(args[i+1], projectDir) {
			t.Errorf("--no-project should skip project mount, found: %s", args[i+1])
		}
	}

	// Named volumes should still be present
	if !strings.Contains(argsStr, "yolobox-home:/home/yolo") {
		t.Error("expected yolobox-home volume even with --no-project")
	}
}

func TestBuildRunArgsNoProjectContextManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	projectDir := t.TempDir()

	cfg := Config{
		Image:       "test-image",
		NoProject:   true,
		RuntimeArgs: []string{"--workdir=/workspace"},
	}

	args, _, err := buildRunArgs(cfg, projectDir, []string{"bash"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload, ok := argEnvValue(args, yoloboxContextPayloadEnv)
	if !ok {
		t.Fatalf("expected %s env var, got %s", yoloboxContextPayloadEnv, strings.Join(args, " "))
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("failed to decode context manifest payload: %v", err)
	}

	var manifest contextManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to decode context manifest: %v", err)
	}

	if !manifest.Config.NoProject {
		t.Fatal("expected no_project in manifest config")
	}
	if manifest.Paths.Project != "" {
		t.Fatalf("expected no project path when automatic project mount is disabled, got %q", manifest.Paths.Project)
	}
	if manifest.Launch.WorkingDir != "/workspace" {
		t.Fatalf("expected workdir from runtime args, got %q", manifest.Launch.WorkingDir)
	}
}

func TestNoProjectConflicts(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "no-project with readonly-project",
			cfg:  Config{NoProject: true, ReadonlyProject: true},
			want: "cannot use --no-project with --readonly-project",
		},
		{
			name: "no-project with exclude",
			cfg:  Config{NoProject: true, Exclude: []string{"node_modules"}},
			want: "cannot use --no-project with --exclude",
		},
		{
			name: "no-project with copy-as",
			cfg:  Config{NoProject: true, CopyAs: []string{"a.txt:b.txt"}},
			want: "cannot use --no-project with --copy-as",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigConflicts(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, err.Error())
			}
		})
	}
}

func TestBuildRunArgsCopyAgentInstructionsIncludesAgentSkills(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	claudeDir := filepath.Join(homeDir, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "skills", "demo-skill"), 0755); err != nil {
		t.Fatalf("failed to create Claude skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte("host claude guidance\n"), 0644); err != nil {
		t.Fatalf("failed to write CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "skills", "demo-skill", "SKILL.md"), []byte("---\nname: demo-skill\ndescription: test\n---\n"), 0644); err != nil {
		t.Fatalf("failed to write Claude skill: %v", err)
	}
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "skills", "demo-codex-skill"), 0755); err != nil {
		t.Fatalf("failed to create Codex skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "AGENTS.md"), []byte("host codex guidance\n"), 0644); err != nil {
		t.Fatalf("failed to write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "skills", "demo-codex-skill", "SKILL.md"), []byte("---\nname: demo-codex-skill\ndescription: test\n---\n"), 0644); err != nil {
		t.Fatalf("failed to write Codex skill: %v", err)
	}

	cfg := Config{CopyAgentInstructions: true}
	args, _, err := buildRunArgs(cfg, projectDir, []string{"echo", "hello"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, filepath.Join(claudeDir, "CLAUDE.md")+":/host-agent-instructions/claude/CLAUDE.md:ro") {
		t.Fatalf("expected CLAUDE.md mount, got %s", argsStr)
	}
	if !strings.Contains(argsStr, filepath.Join(claudeDir, "skills")+":/host-agent-instructions/claude/skills:ro") {
		t.Fatalf("expected Claude skills mount, got %s", argsStr)
	}
	if !strings.Contains(argsStr, filepath.Join(codexDir, "AGENTS.md")+":/host-agent-instructions/codex/AGENTS.md:ro") {
		t.Fatalf("expected AGENTS.md mount, got %s", argsStr)
	}
	if !strings.Contains(argsStr, filepath.Join(codexDir, "skills")+":/host-agent-instructions/codex/skills:ro") {
		t.Fatalf("expected Codex skills mount, got %s", argsStr)
	}
}

func TestBuildRunArgsContextManifestContents(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := Config{
		Image:             "test-image",
		Env:               []string{"FOO=bar", "SUPER_SECRET=do-not-leak", "DEBUG"},
		ReadonlyProject:   true,
		NoYolo:            true,
		ClaudeConfig:      true,
		CodexConfig:       true,
		GeminiConfig:      true,
		OpencodeConfig:    true,
		Clipboard:         true,
		ClipboardEndpoint: "http://host.docker.internal:12345",
		ClipboardToken:    "test-token",
		Network:           "devnet",
		Customize: CustomizeConfig{
			Packages:   []string{"jq"},
			Dockerfile: ".yolobox.Dockerfile",
		},
	}

	args, cleanupPaths, err := buildRunArgs(cfg, projectDir, []string{"echo", "hello"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cleanupPaths) != 0 {
		t.Fatalf("did not expect context manifest cleanup path, got %v", cleanupPaths)
	}

	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, ":/run/yolobox:ro") {
		t.Fatalf("did not expect context manifest mount, got %s", argsStr)
	}
	payload, ok := argEnvValue(args, yoloboxContextPayloadEnv)
	if !ok {
		t.Fatalf("expected %s env var, got %s", yoloboxContextPayloadEnv, argsStr)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("failed to decode context manifest payload: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(projectDir, ".yolobox-context*")); err != nil {
		t.Fatalf("failed to check project context temp dirs: %v", err)
	} else if len(matches) != 0 {
		t.Fatalf("did not expect context temp dirs in project: %v", matches)
	}
	if strings.Contains(string(data), "do-not-leak") {
		t.Fatal("did not expect raw env values in context manifest")
	}

	var manifest contextManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to decode context manifest: %v", err)
	}

	if manifest.SchemaVersion != 1 {
		t.Fatalf("expected schema version 1, got %d", manifest.SchemaVersion)
	}
	if !manifest.InsideYolobox {
		t.Fatal("expected inside_yolobox to be true")
	}
	if manifest.Paths.Project != projectDir {
		t.Fatalf("expected project path %s, got %s", projectDir, manifest.Paths.Project)
	}
	if manifest.Paths.Home != "/home/yolo" {
		t.Fatalf("expected /home/yolo home path, got %s", manifest.Paths.Home)
	}
	if manifest.Paths.Output != "/output" {
		t.Fatalf("expected readonly project output path, got %s", manifest.Paths.Output)
	}
	if manifest.Launch.ContextFile != yoloboxContextFile {
		t.Fatalf("expected context file %s, got %s", yoloboxContextFile, manifest.Launch.ContextFile)
	}
	if !reflect.DeepEqual(manifest.Launch.Command, []string{"echo", "hello"}) {
		t.Fatalf("unexpected launch command: %v", manifest.Launch.Command)
	}
	if manifest.Launch.Interactive {
		t.Fatal("expected non-interactive launch")
	}
	if !contains(manifest.Launch.AutoPassthroughEnvKeys, "OPENAI_API_KEY") {
		t.Fatalf("expected OPENAI_API_KEY in auto passthrough env keys, got %v", manifest.Launch.AutoPassthroughEnvKeys)
	}
	if !reflect.DeepEqual(manifest.Config.EnvKeys, []string{"FOO", "SUPER_SECRET", "DEBUG"}) {
		t.Fatalf("unexpected env keys: %v", manifest.Config.EnvKeys)
	}
	if !manifest.Config.ReadonlyProject {
		t.Fatal("expected readonly_project in manifest config")
	}
	if manifest.Config.NoProject {
		t.Fatal("did not expect no_project in manifest config")
	}
	if !manifest.Config.NoYolo {
		t.Fatal("expected no_yolo in manifest config")
	}
	if manifest.Config.Network != "devnet" {
		t.Fatalf("expected network devnet, got %s", manifest.Config.Network)
	}
	if !manifest.Config.ClaudeConfig {
		t.Fatal("expected claude_config in manifest config")
	}
	if !manifest.Config.CodexConfig {
		t.Fatal("expected codex_config in manifest config")
	}
	if !manifest.Config.GeminiConfig {
		t.Fatal("expected gemini_config in manifest config")
	}
	if !manifest.Config.OpencodeConfig {
		t.Fatal("expected opencode_config in manifest config")
	}
	if !manifest.Config.Clipboard {
		t.Fatal("expected clipboard in manifest config")
	}
	if !reflect.DeepEqual(manifest.Config.Customize.Packages, []string{"jq"}) {
		t.Fatalf("unexpected customize packages: %v", manifest.Config.Customize.Packages)
	}
	if manifest.Config.Customize.Dockerfile != ".yolobox.Dockerfile" {
		t.Fatalf("unexpected customize dockerfile: %s", manifest.Config.Customize.Dockerfile)
	}
}

func TestDescribeYoloboxContextReportsManifestProjectAccess(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available")
	}

	projectDir := t.TempDir()
	contextDir := t.TempDir()
	manifest := buildContextManifest(Config{Image: "test-image"}, projectDir, []string{"codex"}, false, nil, false)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("failed to encode manifest: %v", err)
	}
	contextPath := filepath.Join(contextDir, "context.json")
	if err := os.WriteFile(contextPath, data, 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	cmd := exec.Command("bash", yoloboxSkillContextScriptPath(t))
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "YOLOBOX_CONTEXT_FILE="+contextPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("context script failed: %v\n%s", err, out)
	}

	output := string(out)
	for _, want := range []string{
		"Source: manifest",
		"Automatic project mount: true",
		"Readonly project mode: false",
		"Project writable now: true",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
}

func TestDescribeYoloboxContextReportsNoProjectManifest(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available")
	}

	projectDir := t.TempDir()
	contextDir := t.TempDir()
	manifest := buildContextManifest(
		Config{Image: "test-image", NoProject: true, RuntimeArgs: []string{"--workdir=/workspace"}},
		projectDir,
		[]string{"bash"},
		false,
		nil,
		false,
	)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("failed to encode manifest: %v", err)
	}
	contextPath := filepath.Join(contextDir, "context.json")
	if err := os.WriteFile(contextPath, data, 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	cmd := exec.Command("bash", yoloboxSkillContextScriptPath(t))
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "YOLOBOX_CONTEXT_FILE="+contextPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("context script failed: %v\n%s", err, out)
	}

	output := string(out)
	for _, want := range []string{
		"Source: manifest",
		"Automatic project mount: false",
		"Project: (automatic mount disabled)",
		"Workdir: /workspace",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Project writable now:") {
		t.Fatalf("did not expect project writability check when automatic mount is disabled:\n%s", output)
	}
}

func TestDescribeYoloboxContextFallbackUsesProjectAccessBeforeOutputPath(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	projectDir := t.TempDir()
	outputDir := t.TempDir()

	cmd := exec.Command("bash", yoloboxSkillContextScriptPath(t))
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(),
		"YOLOBOX=1",
		"YOLOBOX_CONTEXT_FILE="+filepath.Join(t.TempDir(), "missing.json"),
		"YOLOBOX_PROJECT_PATH="+projectDir,
		"YOLOBOX_OUTPUT_PATH="+outputDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("context script failed: %v\n%s", err, out)
	}

	output := string(out)
	for _, want := range []string{
		"Source: inferred (manifest unavailable)",
		"Automatic project mount: unknown",
		"Readonly project mode: false",
		"Project writable now: true",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Output: "+outputDir) {
		t.Fatalf("did not expect fallback to report output path for writable project:\n%s", output)
	}
}

func yoloboxSkillContextScriptPath(t *testing.T) string {
	t.Helper()

	var candidates []string
	_, file, _, ok := runtime.Caller(0)
	if ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(file), "..", "..", "skills", "yolobox", "scripts", "describe-yolobox-context.sh"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "..", "..", "skills", "yolobox", "scripts", "describe-yolobox-context.sh"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("failed to locate yolobox context script; tried: %s", strings.Join(candidates, ", "))
	return ""
}

func TestBuildRunArgsContextManifestAppleRuntime(t *testing.T) {
	projectDir := t.TempDir()
	runtimeDir := t.TempDir()
	containerPath := filepath.Join(runtimeDir, "container")
	if err := os.WriteFile(containerPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write fake container runtime: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := Config{
		Runtime: "container",
		Image:   "test-image",
	}

	args, cleanupPaths, err := buildRunArgs(cfg, projectDir, []string{"echo"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cleanupPaths) != 0 {
		t.Fatalf("did not expect context manifest cleanup path, got %v", cleanupPaths)
	}

	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, ":/run/yolobox:ro") {
		t.Fatalf("did not expect context manifest mount for Apple container runtime, got %s", argsStr)
	}
	if _, ok := argEnvValue(args, yoloboxContextPayloadEnv); !ok {
		t.Fatalf("expected %s env var for Apple container runtime, got %s", yoloboxContextPayloadEnv, argsStr)
	}
	if !strings.Contains(argsStr, "YOLOBOX_CONTEXT_FILE=/run/yolobox/context.json") {
		t.Fatalf("expected context env var for Apple container runtime, got %s", argsStr)
	}
}

func TestBuildRunArgsNoYolo(t *testing.T) {
	cfg := Config{
		Image:  "test-image",
		NoYolo: true,
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
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

func TestBuildRunArgsRuntimeArgs(t *testing.T) {
	cfg := Config{
		Image:       "test-image",
		RuntimeArgs: []string{"--security-opt", "seccomp=unconfined"},
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	imageIdx := -1
	for i, arg := range args {
		if arg == "test-image" {
			imageIdx = i
			break
		}
	}
	if imageIdx == -1 {
		t.Fatalf("test-image not found in args: %v", args)
	}
	if imageIdx < 2 {
		t.Fatalf("runtime args missing before image: %v", args)
	}
	if args[imageIdx-2] != "--security-opt" || args[imageIdx-1] != "seccomp=unconfined" {
		t.Fatalf("runtime args not placed before image: %v", args)
	}
}

func TestValidateRuntimeOptions(t *testing.T) {
	cfg := Config{
		CPUs:    "2.5",
		Memory:  "4g",
		ShmSize: "1GiB",
	}

	if err := validateRuntimeOptions(cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestValidateRuntimeOptionsInvalid(t *testing.T) {
	tests := []Config{
		{CPUs: "zero"},
		{CPUs: "-1"},
		{Memory: "a lot"},
		{ShmSize: "123x"},
		{RuntimeArgs: []string{"", " "}},
	}

	for _, cfg := range tests {
		if err := validateRuntimeOptions(cfg); err == nil {
			t.Fatalf("expected error for cfg %#v", cfg)
		}
	}
}

func TestParseMultilineInput(t *testing.T) {
	input := "alpha\n beta\r\n\ngamma"
	values := parseMultilineInput(input)
	expected := []string{"alpha", "beta", "gamma"}
	if len(values) != len(expected) {
		t.Fatalf("expected %d values, got %d (%v)", len(expected), len(values), values)
	}
	for i, val := range expected {
		if values[i] != val {
			t.Fatalf("expected %q at index %d, got %q", val, i, values[i])
		}
	}
}

func TestBuildRunArgsNoNetwork(t *testing.T) {
	cfg := Config{
		Image:     "test-image",
		NoNetwork: true,
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
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

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "/test/project:/test/project:ro") {
		t.Error("expected /test/project:/test/project:ro for ReadonlyProject")
	}
	if !strings.Contains(argsStr, "yolobox-output:/output") {
		t.Error("expected yolobox-output volume for ReadonlyProject")
	}
}

func TestBuildRunArgsCodexConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	codexConfigDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexConfigDir, 0755); err != nil {
		t.Fatalf("failed to create codex config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexConfigDir, "config.toml"), []byte("model = \"gpt-5\"\n"), 0644); err != nil {
		t.Fatalf("failed to write codex config file: %v", err)
	}

	cfg := Config{
		Image:       "test-image",
		CodexConfig: true,
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, codexConfigDir+":/host-codex/.codex:ro") {
		t.Fatalf("expected codex config mount, got %s", argsStr)
	}
}

func TestBuildRunArgsOpencodeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	opencodeConfigDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(opencodeConfigDir, 0755); err != nil {
		t.Fatalf("failed to create opencode config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(opencodeConfigDir, "config.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatalf("failed to write opencode config file: %v", err)
	}

	cfg := Config{
		Image:          "test-image",
		OpencodeConfig: true,
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, opencodeConfigDir+":/host-opencode/.config/opencode:ro") {
		t.Fatalf("expected opencode config mount, got %s", argsStr)
	}
}

func TestDockerfileCodexConfigImportPreservesAuth(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read Dockerfile: %v", err)
	}
	dockerfile := string(data)
	start := strings.Index(dockerfile, "'# Copy Codex config from host staging area if present'")
	if start < 0 {
		t.Fatal("failed to find Codex config import block")
	}
	rest := dockerfile[start:]
	end := strings.Index(rest, "'# Copy git config from host staging area if present'")
	if end < 0 {
		t.Fatal("failed to find end of Codex config import block")
	}
	block := rest[:end]

	if strings.Contains(block, "rm -rf /home/yolo/.codex") {
		t.Fatal("Codex config import must not delete the existing container config directory")
	}
	for _, want := range []string{
		"CODEX_AUTH_BACKUP",
		"sudo mv -f /home/yolo/.codex/auth.json",
		"sudo cp -a /host-codex/.codex/. /home/yolo/.codex/",
		"sudo mv -f \"$CODEX_AUTH_BACKUP\" /home/yolo/.codex/auth.json",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected Codex config import block to contain %q", want)
		}
	}
}

func TestDockerfileConfiguresGitHubTokenCredentialHelper(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read Dockerfile: %v", err)
	}
	dockerfile := string(data)

	for _, want := range []string{
		"/opt/yolobox/bin/git-credential-github-token",
		"credential.https://github.com.helper",
		"GH_TOKEN",
		"GITHUB_TOKEN",
		"x-access-token",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("expected Dockerfile to contain %q", want)
		}
	}
}

func TestBuildRunArgsRootlessPodmanPersistentVolumes(t *testing.T) {
	runtimeDir := t.TempDir()
	podmanPath := filepath.Join(runtimeDir, "podman")
	if err := os.WriteFile(podmanPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write fake podman runtime: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	prevCurrentUID := currentUID
	currentUID = func() int { return 501 }
	t.Cleanup(func() {
		currentUID = prevCurrentUID
	})

	cfg := Config{
		Runtime:         "podman",
		Image:           "test-image",
		ReadonlyProject: true,
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--userns=keep-id:uid=1000,gid=1000") {
		t.Fatalf("expected rootless podman keep-id user namespace, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-home:/home/yolo:Z,U") {
		t.Fatalf("expected rootless podman home volume to use :Z,U, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-cache:/var/cache:Z,U") {
		t.Fatalf("expected rootless podman cache volume to use :Z,U, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-output:/output:Z,U") {
		t.Fatalf("expected rootless podman output volume to use :Z,U, got %s", argsStr)
	}
}

func TestBuildRunArgsProjectFiltering(t *testing.T) {
	projectDir := t.TempDir()
	envPath := filepath.Join(projectDir, ".env")
	sandboxPath := filepath.Join(projectDir, ".env.sandbox")
	secretsDir := filepath.Join(projectDir, "secrets")

	if err := os.WriteFile(envPath, []byte("REAL=1\n"), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", envPath, err)
	}
	if err := os.WriteFile(sandboxPath, []byte("SANDBOX=1\n"), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", sandboxPath, err)
	}
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatalf("failed to create secrets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "token.txt"), []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	cfg := Config{
		Image:           "test-image",
		ReadonlyProject: true,
		Exclude:         []string{".env*", "secrets/**"},
		CopyAs:          []string{".env.sandbox:.env"},
	}

	args, cleanupPaths, err := buildRunArgs(cfg, projectDir, []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cleanupPaths) == 0 {
		t.Fatal("expected cleanup paths for staged filtered project view")
	}
	viewRoot := cleanupPaths[0]

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, viewRoot+":"+projectDir+":ro") {
		t.Fatalf("expected filtered project root mount from %s to %s, got %s", viewRoot, projectDir, argsStr)
	}
	if strings.Contains(argsStr, sandboxPath+":"+envPath) {
		t.Fatalf("did not expect nested copy-as mount in filtered readonly mode, got %s", argsStr)
	}
	if placeholder, err := os.ReadFile(filepath.Join(viewRoot, ".env.sandbox")); err != nil {
		t.Fatalf("expected excluded sandbox placeholder file: %v", err)
	} else if len(placeholder) != 0 {
		t.Fatalf("expected excluded sandbox placeholder to be empty, got %q", string(placeholder))
	}
	if entries, err := os.ReadDir(filepath.Join(viewRoot, "secrets")); err != nil {
		t.Fatalf("expected excluded secrets placeholder dir: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("expected excluded secrets placeholder dir to be empty, got %d entries", len(entries))
	}
	if replacement, err := os.ReadFile(filepath.Join(viewRoot, ".env")); err != nil {
		t.Fatalf("expected copy-as destination in filtered view: %v", err)
	} else if string(replacement) != "SANDBOX=1\n" {
		t.Fatalf("expected copied replacement contents, got %q", string(replacement))
	}
}

func TestBuildRunArgsProjectFilteringReadonlyProject(t *testing.T) {
	projectDir := t.TempDir()
	envPath := filepath.Join(projectDir, ".env")
	sandboxPath := filepath.Join(projectDir, ".env.sandbox")

	if err := os.WriteFile(envPath, []byte("REAL=1\n"), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", envPath, err)
	}
	if err := os.WriteFile(sandboxPath, []byte("SANDBOX=1\n"), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", sandboxPath, err)
	}

	cfg := Config{
		Image:           "test-image",
		ReadonlyProject: true,
		CopyAs:          []string{".env.sandbox:.env"},
	}

	args, cleanupPaths, err := buildRunArgs(cfg, projectDir, []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if len(cleanupPaths) == 0 {
		t.Fatal("expected staged readonly project view")
	}
	viewRoot := cleanupPaths[0]
	if !strings.Contains(argsStr, viewRoot+":"+projectDir+":ro") {
		t.Fatalf("expected readonly staged project mount, got %s", argsStr)
	}
	if replacement, err := os.ReadFile(filepath.Join(viewRoot, ".env")); err != nil {
		t.Fatalf("expected copied replacement file: %v", err)
	} else if string(replacement) != "SANDBOX=1\n" {
		t.Fatalf("unexpected replacement contents: %q", string(replacement))
	}
}

func TestBuildRunArgsProjectFilteringAppleRuntimeUnsupported(t *testing.T) {
	projectDir := t.TempDir()
	runtimeDir := t.TempDir()
	containerPath := filepath.Join(runtimeDir, "container")
	if err := os.WriteFile(containerPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write fake container runtime: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := Config{
		Runtime: "container",
		Image:   "test-image",
		Exclude: []string{".env*"},
	}

	_, _, err := buildRunArgs(cfg, projectDir, []string{"bash"}, false)
	if err == nil {
		t.Fatal("expected Apple container runtime to reject file filtering")
	}
	if !strings.Contains(err.Error(), "not supported with Apple container runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildRunArgsNonInteractive(t *testing.T) {
	cfg := Config{
		Image: "test-image",
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"echo", "hello"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "-it") {
		t.Error("expected no -it flag for non-interactive mode")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) && !strings.Contains(argsStr, "-i") {
		t.Error("expected -i flag when non-interactive test stdin is not a terminal")
	}
}

func TestShouldAttachTTY(t *testing.T) {
	tests := []struct {
		name                string
		command             []string
		explicitInteractive bool
		stdinTTY            bool
		stdoutTTY           bool
		want                bool
	}{
		{
			name:                "explicit interactive shell",
			command:             []string{"bash"},
			explicitInteractive: true,
			stdinTTY:            false,
			stdoutTTY:           false,
			want:                true,
		},
		{
			name:      "claude shortcut interactive",
			command:   []string{"claude"},
			stdinTTY:  true,
			stdoutTTY: true,
			want:      true,
		},
		{
			name:      "claude print mode keeps streams separate",
			command:   []string{"claude", "-p", "hello"},
			stdinTTY:  true,
			stdoutTTY: true,
			want:      false,
		},
		{
			name:      "shell via run is interactive on terminal",
			command:   []string{"bash"},
			stdinTTY:  true,
			stdoutTTY: true,
			want:      true,
		},
		{
			name:      "scripted shell stays non interactive",
			command:   []string{"bash", "-lc", "echo hello"},
			stdinTTY:  true,
			stdoutTTY: true,
			want:      false,
		},
		{
			name:      "generic command stays non interactive",
			command:   []string{"echo", "hello"},
			stdinTTY:  true,
			stdoutTTY: true,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAttachTTY(tt.command, tt.explicitInteractive, tt.stdinTTY, tt.stdoutTTY)
			if got != tt.want {
				t.Fatalf("shouldAttachTTY(%v, %t, %t, %t) = %t, want %t", tt.command, tt.explicitInteractive, tt.stdinTTY, tt.stdoutTTY, got, tt.want)
			}
		})
	}
}

func TestBuildRunArgsScratch(t *testing.T) {
	cfg := Config{
		Image:   "test-image",
		Scratch: true,
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
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
	// Verify project mount is still present (at real path)
	if !strings.Contains(argsStr, "/test/project:/test/project") {
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

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
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

func TestParseFlagsCustomize(t *testing.T) {
	projectDir := t.TempDir()
	cfg, rest, err := parseBaseFlags("run", []string{
		"--packages", "default-jdk, maven",
		"--customize-file", ".yolobox.Dockerfile",
		"--rebuild-image",
		"java", "--version",
	}, projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Customize.Packages) != 2 || cfg.Customize.Packages[0] != "default-jdk" || cfg.Customize.Packages[1] != "maven" {
		t.Fatalf("unexpected customize packages: %v", cfg.Customize.Packages)
	}
	if cfg.Customize.Dockerfile != ".yolobox.Dockerfile" {
		t.Fatalf("unexpected customize dockerfile: %q", cfg.Customize.Dockerfile)
	}
	if !cfg.RebuildImage {
		t.Fatal("expected RebuildImage to be true")
	}
	if len(rest) != 2 || rest[0] != "java" || rest[1] != "--version" {
		t.Fatalf("unexpected remaining args: %v", rest)
	}
}

func TestParseFlagsCustomizeInvalidPackage(t *testing.T) {
	_, _, err := parseBaseFlags("run", []string{"--packages", "default-jdk,$(evil)", "java"}, t.TempDir())
	if err == nil {
		t.Fatal("expected invalid package name error")
	}
}

func TestParseFlagsProjectFiltering(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("REAL=1\n"), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env.sandbox"), []byte("SANDBOX=1\n"), 0644); err != nil {
		t.Fatalf("failed to write .env.sandbox: %v", err)
	}

	cfg, rest, err := parseBaseFlags("run", []string{
		"--readonly-project",
		"--exclude", ".env*",
		"--copy-as", ".env.sandbox:.env",
		"env",
	}, projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectSliceEqual(t, cfg.Exclude, []string{".env*"})
	expectSliceEqual(t, cfg.CopyAs, []string{".env.sandbox:.env"})
	expectSliceEqual(t, rest, []string{"env"})
}

func TestParseFlagsProjectFilteringRejectsMissingCopyAsDestination(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".env.sandbox"), []byte("SANDBOX=1\n"), 0644); err != nil {
		t.Fatalf("failed to write .env.sandbox: %v", err)
	}

	_, _, err := parseBaseFlags("run", []string{"--readonly-project", "--copy-as", ".env.sandbox:.env", "env"}, projectDir)
	if err == nil {
		t.Fatal("expected missing copy-as destination to fail")
	}
	if !strings.Contains(err.Error(), "must already exist as a file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseFlagsProjectFilteringRequiresReadonlyProject(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("REAL=1\n"), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	_, _, err := parseBaseFlags("run", []string{"--exclude", ".env*", "env"}, projectDir)
	if err == nil {
		t.Fatal("expected readonly-project requirement to fail")
	}
	if !strings.Contains(err.Error(), "require --readonly-project") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHasCustomization(t *testing.T) {
	if hasCustomization(Config{}) {
		t.Fatal("expected empty config to have no customization")
	}
	if !hasCustomization(Config{Customize: CustomizeConfig{Packages: []string{"default-jdk"}}}) {
		t.Fatal("expected packages customization to be detected")
	}
	if !hasCustomization(Config{Customize: CustomizeConfig{Dockerfile: ".yolobox.Dockerfile"}}) {
		t.Fatal("expected dockerfile customization to be detected")
	}
}

func TestGenerateCustomDockerfile(t *testing.T) {
	dockerfile, err := generateCustomDockerfile("ghcr.io/finbarr/yolobox:latest", []string{"maven", "default-jdk", "maven"}, "USER root\nRUN echo hi\nUSER yolo\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(dockerfile, "FROM ghcr.io/finbarr/yolobox:latest") {
		t.Fatalf("expected base image in generated Dockerfile:\n%s", dockerfile)
	}
	if !strings.Contains(dockerfile, "apt-get install -y --no-install-recommends default-jdk maven") {
		t.Fatalf("expected sorted package install in generated Dockerfile:\n%s", dockerfile)
	}
	if !strings.Contains(dockerfile, "RUN echo hi") {
		t.Fatalf("expected fragment content in generated Dockerfile:\n%s", dockerfile)
	}
}

func TestGenerateCustomDockerfileRejectsInvalidPackage(t *testing.T) {
	_, err := generateCustomDockerfile("base", []string{"$(evil)"}, "")
	if err == nil {
		t.Fatal("expected invalid package to be rejected")
	}
}

func TestCustomImageTagStable(t *testing.T) {
	tagA := customImageTag("sha256:base", "FROM base\n", []string{"maven", "default-jdk"})
	tagB := customImageTag("sha256:base", "FROM base\n", []string{"default-jdk", "maven", "maven"})
	if tagA != tagB {
		t.Fatalf("expected normalized package order to yield same tag, got %q vs %q", tagA, tagB)
	}
}

func TestResolveCustomizeFile(t *testing.T) {
	projectDir := t.TempDir()
	got, err := resolveCustomizeFile(".yolobox.Dockerfile", projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Join(projectDir, ".yolobox.Dockerfile") {
		t.Fatalf("unexpected resolved customize file: %q", got)
	}
}

func TestLoadCustomizeFragment(t *testing.T) {
	projectDir := t.TempDir()
	path := filepath.Join(projectDir, ".yolobox.Dockerfile")
	if err := os.WriteFile(path, []byte("USER root\nRUN echo hi\n"), 0644); err != nil {
		t.Fatalf("failed to write test customize file: %v", err)
	}

	got, err := loadCustomizeFragment(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "USER root\nRUN echo hi" {
		t.Fatalf("unexpected customize fragment contents: %q", got)
	}
}

func TestStringSliceFlag(t *testing.T) {
	var s stringSliceFlag

	if err := s.Set("first"); err != nil {
		t.Fatalf("Set(first) failed: %v", err)
	}
	if err := s.Set("second"); err != nil {
		t.Fatalf("Set(second) failed: %v", err)
	}

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

func TestComparableVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v0.10.0", "v0.10.0"},
		{"0.10.0", "v0.10.0"},
		{"v0.10.0-9-gabcdef", "v0.10.0"},
		{"dev", ""},
		{"", ""},
	}

	for _, tt := range tests {
		if got := comparableVersion(tt.input); got != tt.want {
			t.Errorf("comparableVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{"0.10.0", "0.9.4", true},
		{"0.10.0", "v0.10.0-9-gabcdef", false},
		{"0.10.0", "0.10.0", false},
		{"0.10.0", "0.10.1", false},
		{"0.10.0", "dev", true},
	}

	for _, tt := range tests {
		if got := isNewerVersion(tt.latest, tt.current); got != tt.want {
			t.Errorf("isNewerVersion(%q, %q) = %t, want %t", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestWrapCommaList(t *testing.T) {
	lines := wrapCommaList([]string{"AAAA", "BBBB", "CCCC", "DDDD"}, 15)
	want := []string{"AAAA, BBBB", "CCCC, DDDD"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("wrapCommaList() = %#v, want %#v", lines, want)
	}
}

func TestAutoPassthroughEnvVars(t *testing.T) {
	// Check that common API keys are in the list
	expected := []string{
		"ANTHROPIC_API_KEY",
		"CLAUDE_CODE_OAUTH_TOKEN",
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

func TestPrintUsageListsActualAutoPassthroughEnvVars(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stderr = w
	printUsage()
	_ = w.Close()
	os.Stderr = oldStderr

	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	_ = r.Close()

	helpText := string(output)
	for _, key := range autoPassthroughEnvVars {
		if !strings.Contains(helpText, key) {
			t.Errorf("expected help text to mention %s", key)
		}
	}
	for _, unexpected := range []string{"GEMINI_MODEL", "GOOGLE_API_KEY"} {
		if strings.Contains(helpText, unexpected) {
			t.Errorf("did not expect help text to mention %s", unexpected)
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

func TestBuildRunArgsNetwork(t *testing.T) {
	cfg := Config{
		Image:   "test-image",
		Network: "dev_network",
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"echo"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--network dev_network") {
		t.Error("expected --network dev_network")
	}
}

func TestBuildRunArgsPod(t *testing.T) {
	cfg := Config{
		Image:     "test-image",
		Pod:       "dev-pod",
		Network:   "ignored_network",
		NoNetwork: true,
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"echo"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--pod dev-pod") {
		t.Error("expected --pod dev-pod")
	}
	if strings.Contains(argsStr, "--network ignored_network") || strings.Contains(argsStr, "--network none") {
		t.Errorf("expected pod mode to suppress network flags, got: %s", argsStr)
	}
}

func TestBuildRunArgsResourceLimits(t *testing.T) {
	cfg := Config{
		Image:   "test-image",
		CPUs:    "4",
		Memory:  "8g",
		ShmSize: "2g",
		GPUs:    "all",
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	for _, expect := range []string{
		"--cpus 4",
		"--memory 8g",
		"--shm-size 2g",
		"--gpus all",
	} {
		if !strings.Contains(argsStr, expect) {
			t.Fatalf("expected run args to contain %s, got %s", expect, argsStr)
		}
	}
}

func TestBuildRunArgsDeviceSecurity(t *testing.T) {
	cfg := Config{
		Image:   "test-image",
		Devices: []string{"/dev/kvm:/dev/kvm"},
		CapAdd:  []string{"SYS_PTRACE"},
		CapDrop: []string{"MKNOD"},
		GPUs:    "all",
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	for _, expect := range []string{
		"--device /dev/kvm:/dev/kvm",
		"--cap-add SYS_PTRACE",
		"--cap-drop MKNOD",
		"--gpus all",
	} {
		if !strings.Contains(argsStr, expect) {
			t.Fatalf("expected run args to contain %s, got %s", expect, argsStr)
		}
	}
}

func TestMergeConfigNetwork(t *testing.T) {
	dst := Config{
		Runtime: "docker",
		Image:   "old-image",
	}
	src := Config{
		Network: "my_network",
	}

	mergeConfig(&dst, src)

	if dst.Network != "my_network" {
		t.Errorf("expected Network to be my_network, got %s", dst.Network)
	}
}

func TestMergeConfigPod(t *testing.T) {
	dst := Config{
		Runtime: "docker",
		Image:   "old-image",
	}
	src := Config{
		Pod: "my_pod",
	}

	mergeConfig(&dst, src)

	if dst.Pod != "my_pod" {
		t.Errorf("expected Pod to be my_pod, got %s", dst.Pod)
	}
}

func TestParseFlagsNetwork(t *testing.T) {
	cfg, rest, err := parseBaseFlags("run", []string{"--network", "mynet", "echo"}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Network != "mynet" {
		t.Errorf("expected Network=mynet, got %s", cfg.Network)
	}
	if len(rest) != 1 || rest[0] != "echo" {
		t.Errorf("expected remaining args [echo], got %v", rest)
	}
}

func TestParseFlagsPod(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	cfg, rest, err := parseBaseFlags("run", []string{"--pod", "mypod", "echo"}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Pod != "mypod" {
		t.Errorf("expected Pod=mypod, got %s", cfg.Pod)
	}
	if len(rest) != 1 || rest[0] != "echo" {
		t.Errorf("expected remaining args [echo], got %v", rest)
	}
}

func TestParseFlagsResourceLimits(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	args := []string{
		"--cpus", "4",
		"--memory", "8g",
		"--shm-size", "1g",
		"--gpus", "all",
		"--runtime-arg", "--cpu-shares=512",
		"echo",
	}

	cfg, rest, err := parseBaseFlags("run", args, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.CPUs != "4" {
		t.Errorf("unexpected CPU limit: %+v", cfg.CPUs)
	}
	if cfg.Memory != "8g" || cfg.ShmSize != "1g" {
		t.Errorf("unexpected memory limits: %+v", cfg)
	}
	if cfg.GPUs != "all" {
		t.Errorf("expected GPUs=all, got %s", cfg.GPUs)
	}
	if len(cfg.RuntimeArgs) != 1 || cfg.RuntimeArgs[0] != "--cpu-shares=512" {
		t.Fatalf("expected runtime args to be captured, got %+v", cfg.RuntimeArgs)
	}
	if len(rest) != 1 || rest[0] != "echo" {
		t.Errorf("expected remaining args [echo], got %v", rest)
	}
}

func TestParseFlagsDeviceSecurity(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	args := []string{
		"--device", "/dev/kvm:/dev/kvm",
		"--cap-add", "SYS_PTRACE",
		"--cap-drop", "MKNOD",
		"--runtime-arg", "--security-opt",
		"--runtime-arg", "seccomp=unconfined",
		"--gpus", "all",
		"echo",
	}

	cfg, rest, err := parseBaseFlags("run", args, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectSliceEqual(t, cfg.Devices, []string{"/dev/kvm:/dev/kvm"})
	expectSliceEqual(t, cfg.CapAdd, []string{"SYS_PTRACE"})
	expectSliceEqual(t, cfg.CapDrop, []string{"MKNOD"})
	expectSliceEqual(t, cfg.RuntimeArgs, []string{"--security-opt", "seccomp=unconfined"})
	if cfg.GPUs != "all" {
		t.Errorf("expected GPUs=all, got %s", cfg.GPUs)
	}
	if len(rest) != 1 || rest[0] != "echo" {
		t.Errorf("expected remaining args [echo], got %v", rest)
	}
}

func expectSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected slice %v, got %v", want, got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("expected slice %v, got %v", want, got)
		}
	}
}

func TestParseFlagsNetworkConflict(t *testing.T) {
	_, _, err := parseBaseFlags("run", []string{"--network", "mynet", "--no-network", "echo"}, t.TempDir())
	if err == nil {
		t.Error("expected error for --network with --no-network")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot use --network with --no-network") {
		t.Errorf("expected conflict error message, got: %v", err)
	}
}

func TestParseFlagsPodNetworkConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	_, _, err := parseBaseFlags("run", []string{"--pod", "mypod", "--network", "mynet", "echo"}, t.TempDir())
	if err == nil {
		t.Error("expected error for --pod with --network")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot use --pod with --network") {
		t.Errorf("expected conflict error message, got: %v", err)
	}
}

func TestParseFlagsPodNoNetworkConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	_, _, err := parseBaseFlags("run", []string{"--pod", "mypod", "--no-network", "echo"}, t.TempDir())
	if err == nil {
		t.Error("expected error for --pod with --no-network")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot use --pod with --no-network") {
		t.Errorf("expected conflict error message, got: %v", err)
	}
}

func TestSplitToolArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantYolobox []string
		wantTool    []string
	}{
		{
			name:        "tool flag only",
			args:        []string{"--resume"},
			wantYolobox: nil,
			wantTool:    []string{"--resume"},
		},
		{
			name:        "tool flag with value",
			args:        []string{"--resume", "abc123"},
			wantYolobox: nil,
			wantTool:    []string{"--resume", "abc123"},
		},
		{
			name:        "yolobox flag then tool flag",
			args:        []string{"--no-network", "--resume"},
			wantYolobox: []string{"--no-network"},
			wantTool:    []string{"--resume"},
		},
		{
			name:        "yolobox pod flag with value then tool flag",
			args:        []string{"--pod", "mypod", "--resume"},
			wantYolobox: []string{"--pod", "mypod"},
			wantTool:    []string{"--resume"},
		},
		{
			name:        "yolobox flag with value then tool flag",
			args:        []string{"--env", "FOO=bar", "--resume"},
			wantYolobox: []string{"--env", "FOO=bar"},
			wantTool:    []string{"--resume"},
		},
		{
			name:        "yolobox flag with equals then tool flag",
			args:        []string{"--env=FOO=bar", "--resume"},
			wantYolobox: []string{"--env=FOO=bar"},
			wantTool:    []string{"--resume"},
		},
		{
			name:        "project filtering flags stay with yolobox",
			args:        []string{"--exclude", ".env*", "--copy-as", ".env.sandbox:.env", "--resume"},
			wantYolobox: []string{"--exclude", ".env*", "--copy-as", ".env.sandbox:.env"},
			wantTool:    []string{"--resume"},
		},
		{
			name:        "codex config flag stays with yolobox",
			args:        []string{"--codex-config", "--resume"},
			wantYolobox: []string{"--codex-config"},
			wantTool:    []string{"--resume"},
		},
		{
			name:        "multiple yolobox flags then tool args",
			args:        []string{"--no-network", "--scratch", "--resume", "abc123"},
			wantYolobox: []string{"--no-network", "--scratch"},
			wantTool:    []string{"--resume", "abc123"},
		},
		{
			name:        "customization flags stay with yolobox",
			args:        []string{"--packages", "default-jdk,maven", "--rebuild-image", "--resume"},
			wantYolobox: []string{"--packages", "default-jdk,maven", "--rebuild-image"},
			wantTool:    []string{"--resume"},
		},
		{
			name:        "explicit separator",
			args:        []string{"--no-network", "--", "--help"},
			wantYolobox: []string{"--no-network"},
			wantTool:    []string{"--help"},
		},
		{
			name:        "non-flag arg",
			args:        []string{"somefile.txt"},
			wantYolobox: nil,
			wantTool:    []string{"somefile.txt"},
		},
		{
			name:        "yolobox flag then non-flag arg",
			args:        []string{"--scratch", "somefile.txt"},
			wantYolobox: []string{"--scratch"},
			wantTool:    []string{"somefile.txt"},
		},
		{
			name:        "no args",
			args:        []string{},
			wantYolobox: nil,
			wantTool:    nil,
		},
		{
			name:        "only yolobox flags",
			args:        []string{"--scratch", "--no-network"},
			wantYolobox: []string{"--scratch", "--no-network"},
			wantTool:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotYolobox, gotTool := splitToolArgs(tt.args)

			if len(gotYolobox) != len(tt.wantYolobox) {
				t.Errorf("yolobox args: got %v, want %v", gotYolobox, tt.wantYolobox)
			} else {
				for i := range gotYolobox {
					if gotYolobox[i] != tt.wantYolobox[i] {
						t.Errorf("yolobox args[%d]: got %q, want %q", i, gotYolobox[i], tt.wantYolobox[i])
					}
				}
			}

			if len(gotTool) != len(tt.wantTool) {
				t.Errorf("tool args: got %v, want %v", gotTool, tt.wantTool)
			} else {
				for i := range gotTool {
					if gotTool[i] != tt.wantTool[i] {
						t.Errorf("tool args[%d]: got %q, want %q", i, gotTool[i], tt.wantTool[i])
					}
				}
			}
		})
	}
}

func TestDetectTimezone(t *testing.T) {
	// Test with TZ env var set
	t.Setenv("TZ", "Europe/London")
	tz := detectTimezone()
	if tz != "Europe/London" {
		t.Errorf("expected Europe/London from TZ env, got %q", tz)
	}

	// Test without TZ env var (falls back to /etc/localtime)
	t.Setenv("TZ", "")
	tz = detectTimezone()
	// On most systems /etc/localtime exists; just verify it doesn't crash
	// and returns either a valid timezone or empty string
	if tz != "" && !strings.Contains(tz, "/") {
		t.Errorf("expected IANA timezone with '/' or empty string, got %q", tz)
	}
}

func TestParseFlagsCodexConfig(t *testing.T) {
	cfg, rest, err := parseBaseFlags("run", []string{"--codex-config", "codex", "--version"}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.CodexConfig {
		t.Fatal("expected CodexConfig to be true after parsing --codex-config")
	}
	expectSliceEqual(t, rest, []string{"codex", "--version"})
}

func TestParseFlagsOpencodeConfig(t *testing.T) {
	cfg, rest, err := parseBaseFlags("run", []string{"--opencode-config", "opencode", "--version"}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.OpencodeConfig {
		t.Fatal("expected OpencodeConfig to be true after parsing --opencode-config")
	}
	expectSliceEqual(t, rest, []string{"opencode", "--version"})
}

func TestParseFlagsClipboard(t *testing.T) {
	cfg, rest, err := parseBaseFlags("run", []string{"--clipboard", "codex", "--version"}, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Clipboard {
		t.Fatal("expected Clipboard to be true after parsing --clipboard")
	}
	expectSliceEqual(t, rest, []string{"codex", "--version"})
}

func TestValidateClipboardNoNetworkConflict(t *testing.T) {
	err := validateConfigConflicts(Config{Clipboard: true, NoNetwork: true})
	if err == nil {
		t.Fatal("expected clipboard/no-network conflict")
	}
	if !strings.Contains(err.Error(), "--clipboard") {
		t.Fatalf("expected clipboard error, got %v", err)
	}
}

func TestBuildRunArgsClipboard(t *testing.T) {
	cfg := Config{
		Image:             "test-image",
		Clipboard:         true,
		ClipboardEndpoint: "http://host.docker.internal:12345",
		ClipboardToken:    "test-token",
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	for _, want := range []string{
		"YOLOBOX_CLIPBOARD=1",
		"YOLOBOX_CLIPBOARD_ENDPOINT=http://host.docker.internal:12345",
		"YOLOBOX_CLIPBOARD_TOKEN=test-token",
	} {
		if !strings.Contains(argsStr, want) {
			t.Fatalf("expected %s in args: %s", want, argsStr)
		}
	}
}

func TestBuildRunArgsTimezone(t *testing.T) {
	t.Setenv("TZ", "America/New_York")
	cfg := Config{
		Image: "test-image",
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "TZ=America/New_York") {
		t.Error("expected TZ=America/New_York in args")
	}
}

func TestPreprocessClaudeConfig(t *testing.T) {
	// Use temp dir as HOME so the function writes to tmpDir/.yolobox/tmp/
	// instead of the real ~/.yolobox/tmp/ (which may not exist or be writable)
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	srcPath := filepath.Join(tmpDir, ".claude.json")

	// Config with installMethod that should be removed
	srcContent := `{
  "numStartups": 10,
  "installMethod": "native",
  "autoUpdates": false
}`
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Run preprocessing
	resultPath := preprocessClaudeConfig(srcPath)
	if resultPath == "" {
		t.Fatal("preprocessClaudeConfig returned empty path")
	}

	// Verify the file uses a unique name (not the fixed "claude-config.json")
	baseName := filepath.Base(resultPath)
	if baseName == "claude-config.json" {
		t.Error("expected unique temp file name, got fixed claude-config.json")
	}
	if !strings.HasPrefix(baseName, "claude-config-") || !strings.HasSuffix(baseName, ".json") {
		t.Errorf("expected temp file matching claude-config-*.json, got %s", baseName)
	}

	// Read the result
	result, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("failed to read result file: %v", err)
	}

	resultStr := string(result)

	// Should NOT contain installMethod
	if strings.Contains(resultStr, "installMethod") {
		t.Errorf("result should not contain installMethod, got: %s", resultStr)
	}

	// Should still contain other fields
	if !strings.Contains(resultStr, "numStartups") {
		t.Errorf("result should contain numStartups, got: %s", resultStr)
	}
	if !strings.Contains(resultStr, "autoUpdates") {
		t.Errorf("result should contain autoUpdates, got: %s", resultStr)
	}
}

func TestConcurrentPreprocessClaudeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	srcPath := filepath.Join(tmpDir, ".claude.json")

	srcContent := `{"numStartups": 10, "installMethod": "native"}`
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Call preprocessClaudeConfig concurrently and verify unique paths
	const n = 10
	paths := make([]string, n)
	done := make(chan int, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			paths[idx] = preprocessClaudeConfig(srcPath)
			done <- idx
		}(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}

	// All paths should be non-empty and unique
	seen := make(map[string]bool)
	for i, p := range paths {
		if p == "" {
			t.Fatalf("preprocessClaudeConfig[%d] returned empty path", i)
		}
		if seen[p] {
			t.Errorf("duplicate temp path from concurrent calls: %s", p)
		}
		seen[p] = true
	}
}

func TestDirContainsSymlinks(t *testing.T) {
	// Directory with no symlinks
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "nested.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	if dirContainsSymlinks(dir) {
		t.Error("expected no symlinks in plain directory")
	}

	// Directory with a symlink
	target := filepath.Join(dir, "file.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if !dirContainsSymlinks(dir) {
		t.Error("expected symlinks detected")
	}
}

func TestDirContainsSymlinksNested(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "real.txt")
	if err := os.WriteFile(target, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sub, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if !dirContainsSymlinks(dir) {
		t.Error("expected nested symlink to be detected")
	}
}

func TestCopyDirDereferenced(t *testing.T) {
	// Create source directory with a symlink
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "regular.txt"), []byte("regular"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an external file to symlink to
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "external.txt"), []byte("external-content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Symlink from inside src to external file
	if err := os.Symlink(filepath.Join(external, "external.txt"), filepath.Join(src, "linked.txt")); err != nil {
		t.Fatal(err)
	}

	// Symlink to an external directory
	extDir := filepath.Join(external, "subdir")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "deep.txt"), []byte("deep-content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(extDir, filepath.Join(src, "linked-dir")); err != nil {
		t.Fatal(err)
	}

	// Copy with dereference
	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyDirDereferenced(src, dst); err != nil {
		t.Fatalf("copyDirDereferenced failed: %v", err)
	}

	// Verify regular file was copied
	data, err := os.ReadFile(filepath.Join(dst, "regular.txt"))
	if err != nil || string(data) != "regular" {
		t.Errorf("regular.txt: got %q, err %v", data, err)
	}

	// Verify symlinked file was dereferenced (copied as regular file)
	data, err = os.ReadFile(filepath.Join(dst, "linked.txt"))
	if err != nil || string(data) != "external-content" {
		t.Errorf("linked.txt: got %q, err %v", data, err)
	}
	info, _ := os.Lstat(filepath.Join(dst, "linked.txt"))
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("linked.txt should be a regular file, not a symlink")
	}

	// Verify symlinked directory was dereferenced
	data, err = os.ReadFile(filepath.Join(dst, "linked-dir", "deep.txt"))
	if err != nil || string(data) != "deep-content" {
		t.Errorf("linked-dir/deep.txt: got %q, err %v", data, err)
	}
	info, _ = os.Lstat(filepath.Join(dst, "linked-dir"))
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("linked-dir should be a regular directory, not a symlink")
	}
}

func TestCopyDirDereferencedSkipsBrokenSymlinks(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "good.txt"), []byte("good"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/path/file.txt", filepath.Join(src, "broken.txt")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyDirDereferenced(src, dst); err != nil {
		t.Fatalf("copyDirDereferenced should not fail on broken symlinks: %v", err)
	}

	// good.txt should exist
	if _, err := os.Stat(filepath.Join(dst, "good.txt")); err != nil {
		t.Error("good.txt should have been copied")
	}

	// broken.txt should be skipped
	if _, err := os.Stat(filepath.Join(dst, "broken.txt")); err == nil {
		t.Error("broken.txt should have been skipped")
	}
}

func TestStageDirResolvingSymlinks(t *testing.T) {
	src := t.TempDir()
	external := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, "config.json"), []byte(`{"key":"value"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "shared.json"), []byte(`{"shared":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "shared.json"), filepath.Join(src, "shared.json")); err != nil {
		t.Fatal(err)
	}

	staged, err := stageDirResolvingSymlinks(src)
	if err != nil {
		t.Fatalf("stageDirResolvingSymlinks failed: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(staged)
	}()

	// Verify the staged directory contains dereferenced files
	data, err := os.ReadFile(filepath.Join(staged, "shared.json"))
	if err != nil || string(data) != `{"shared":true}` {
		t.Errorf("staged shared.json: got %q, err %v", data, err)
	}

	// Verify it's a regular file, not a symlink
	info, _ := os.Lstat(filepath.Join(staged, "shared.json"))
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("staged shared.json should be a regular file")
	}
}

func TestFindSSHAgentSocketLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test")
	}

	// With SSH_AUTH_SOCK set, should return it directly
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-test/agent.123")
	sock, err := findSSHAgentSocket()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sock != "/tmp/ssh-test/agent.123" {
		t.Errorf("expected /tmp/ssh-test/agent.123, got %s", sock)
	}

	// Without SSH_AUTH_SOCK, should error
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err = findSSHAgentSocket()
	if err == nil {
		t.Error("expected error when SSH_AUTH_SOCK is empty")
	}
}

func TestFindSSHAgentSocketMacOSNoSSHAuthSock(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS-only test")
	}

	// On macOS, SSH_AUTH_SOCK is not used directly — the function should
	// detect the Docker runtime and return the VM-internal path or error.
	// Without Docker running, it may error — that's the expected behavior.
	t.Setenv("SSH_AUTH_SOCK", "")
	sock, err := findSSHAgentSocket()
	if err != nil {
		// Expected when Docker/Colima isn't configured
		return
	}
	// If it succeeds, the socket path should be non-empty
	if sock == "" {
		t.Error("expected non-empty socket path")
	}
}
