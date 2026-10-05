package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCopilotConfig = `// User settings belong in settings.json.
// This file is managed automatically.
{
  "trusted_folders": ["/work"],
  "copilotTokens": {"https://github.com:octo": "gho_secret"},
  "loggedInUsers": [{"host": "https://github.com", "login": "octo"}],
  "lastLoggedInUser": {"host": "https://github.com", "login": "octo"}
}
`

func setupCopilotHome(t *testing.T) (homeDir, copilotDir string) {
	t.Helper()
	homeDir = t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("COPILOT_HOME", "")
	copilotDir = filepath.Join(homeDir, ".copilot")
	for _, dir := range []string{"session-state/abc", "pkg/darwin", "agents"} {
		if err := os.MkdirAll(filepath.Join(copilotDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"config.json":          testCopilotConfig,
		"settings.json":        "{}\n",
		"agents/a.agent.md":    "agent\n",
		"session-store.db":     "db",
		"session-store.db-wal": "wal",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(copilotDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return homeDir, copilotDir
}

func findMountSource(args []string, target string) string {
	for _, arg := range args {
		if strings.HasSuffix(arg, ":"+target) {
			return strings.TrimSuffix(arg, ":"+target)
		}
	}
	return ""
}

func TestReadCopilotConfigStripsComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(testCopilotConfig), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := readCopilotConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := config["trusted_folders"]; !ok {
		t.Fatalf("expected trusted_folders, got %v", config)
	}
	if !copilotConfigHasPlaintextToken(config) {
		t.Fatal("expected plaintext token detection")
	}
}

func TestBuildRunArgsCopilotConfigMountsAndLiveSessions(t *testing.T) {
	_, copilotDir := setupCopilotHome(t)
	projectDir := t.TempDir()

	args, cleanup, err := buildRunArgs(
		Config{Image: "test-image", CopilotConfig: true, Env: []string{"COPILOT_GITHUB_TOKEN=gho_user"}},
		projectDir, []string{"copilot", "--version"}, false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		for _, p := range cleanup {
			_ = os.RemoveAll(p)
		}
	}()

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, copilotDir+":/host-copilot/.copilot:ro") {
		t.Fatalf("expected direct Copilot dir mount, got %s", argsStr)
	}
	if !strings.Contains(argsStr, filepath.Join(copilotDir, "session-state")+":/host-copilot-session-state:rw") {
		t.Fatalf("expected live session-state mount, got %s", argsStr)
	}
	if v, ok := argEnvValue(args, "YOLOBOX_COPILOT_SESSIONS"); !ok || v != "1" {
		t.Fatalf("expected sessions marker, got %q %t", v, ok)
	}
	if _, ok := argEnvValue(args, "YOLOBOX_NO_COPILOT_AUTH"); ok {
		t.Fatal("did not expect no-auth marker")
	}
	if strings.Count(argsStr, "COPILOT_GITHUB_TOKEN=") != 1 {
		t.Fatalf("expected only user-provided Copilot token, got %s", argsStr)
	}
	src := findMountSource(args, "/host-copilot/config.json:ro")
	if src == "" {
		t.Fatalf("expected processed config mount, got %s", argsStr)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("processed config should be plain JSON: %v\n%s", err, data)
	}
	if _, ok := config["copilotTokens"]; !ok {
		t.Fatal("expected auth keys to be preserved when auth sync is enabled")
	}
}

func TestBuildRunArgsNoCopilotAuthStagesWithoutCredentials(t *testing.T) {
	setupCopilotHome(t)
	t.Setenv("COPILOT_GITHUB_TOKEN", "gho_host")
	projectDir := t.TempDir()

	args, cleanup, err := buildRunArgs(
		Config{Image: "test-image", CopilotConfig: true, NoCopilotAuth: true},
		projectDir, []string{"copilot"}, false,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		for _, p := range cleanup {
			_ = os.RemoveAll(p)
		}
	}()

	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "COPILOT_GITHUB_TOKEN") {
		t.Fatalf("did not expect Copilot token passthrough, got %s", argsStr)
	}
	if v, ok := argEnvValue(args, "YOLOBOX_NO_COPILOT_AUTH"); !ok || v != "1" {
		t.Fatalf("expected no-auth marker, got %q %t", v, ok)
	}
	staged := findMountSource(args, "/host-copilot/.copilot:ro")
	if staged == "" || strings.HasSuffix(staged, "/.copilot") {
		t.Fatalf("expected staged Copilot dir, got %s", argsStr)
	}
	for _, excluded := range []string{"config.json", "pkg", "session-state", "session-store.db", "session-store.db-wal"} {
		if _, err := os.Lstat(filepath.Join(staged, excluded)); err == nil {
			t.Fatalf("staged dir should not contain %s", excluded)
		}
	}
	if _, err := os.Stat(filepath.Join(staged, "agents", "a.agent.md")); err != nil {
		t.Fatalf("expected agents to be staged: %v", err)
	}
	data, err := os.ReadFile(findMountSource(args, "/host-copilot/config.json:ro"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range copilotAuthConfigKeys {
		if strings.Contains(string(data), `"`+key+`"`) {
			t.Fatalf("processed config leaked %s: %s", key, data)
		}
	}
	if !strings.Contains(string(data), "trusted_folders") {
		t.Fatalf("expected non-auth settings kept: %s", data)
	}
}

func TestCopilotTokenHelpers(t *testing.T) {
	if copilotTokenUsable("ghp_classic") || copilotTokenUsable("") || !copilotTokenUsable("gho_x") {
		t.Fatal("unexpected token usability")
	}
	if !copilotTokenEnvProvided(Config{Env: []string{"COPILOT_GITHUB_TOKEN=x"}}, nil, nil) {
		t.Fatal("expected --env token detection")
	}
	if !copilotTokenEnvProvided(Config{}, nil, []string{"COPILOT_GITHUB_TOKEN"}) {
		t.Fatal("expected passthrough token detection")
	}
	if copilotTokenEnvProvided(Config{}, nil, []string{"GH_TOKEN"}) {
		t.Fatal("did not expect GH_TOKEN to count")
	}
}

func TestParseFlagsCopilotConfig(t *testing.T) {
	cfg, _, err := parseBaseFlagsWithConfig("run", []string{"--copilot-config", "--no-copilot-auth", "copilot"}, t.TempDir(), defaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.CopilotConfig || !cfg.NoCopilotAuth {
		t.Fatalf("expected Copilot flags, got %#v", cfg)
	}
	if err := validateConfigConflicts(Config{NoCopilotAuth: true}); err == nil || !strings.Contains(err.Error(), "--no-copilot-auth") {
		t.Fatalf("expected no-copilot-auth conflict, got %v", err)
	}
	yb, tool := splitToolArgs([]string{"--copilot-config", "--no-copilot-auth", "--resume"})
	if len(yb) != 2 || len(tool) != 1 {
		t.Fatalf("unexpected split: %v %v", yb, tool)
	}
}

func TestDockerfileCopilotSyncExcludesHostState(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	var rsync string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "rsync") && strings.Contains(line, "/host-copilot/.copilot/") {
			rsync = line
		}
	}
	if rsync == "" {
		t.Fatal("expected Copilot rsync in entrypoint")
	}
	for _, want := range []string{"--exclude=/pkg/", "--exclude=/config.json", "--exclude=/session-state", `--exclude="/*.db"`} {
		if !strings.Contains(rsync, want) {
			t.Fatalf("Copilot rsync missing %s: %s", want, rsync)
		}
	}
	if strings.Contains(rsync, "--delete") {
		t.Fatal("Copilot rsync must not delete container-local state")
	}
}
