package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	account, selected := copilotLastLoggedInAccount(config)
	if !selected || account.key() != "https://github.com:octo" {
		t.Fatalf("unexpected selected account %#v %t", account, selected)
	}
	if !copilotConfigHasPlaintextToken(config, account, selected) {
		t.Fatal("expected plaintext token detection")
	}
	if copilotConfigHasPlaintextToken(config, copilotAccount{host: "https://github.com", login: "other"}, true) {
		t.Fatal("plaintext token for another account must not count for the selected account")
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
}

func clearCopilotAuthEnv(t *testing.T) {
	t.Helper()
	for _, key := range copilotAuthEnvKeys {
		t.Setenv(key, "")
	}
}

func TestCopilotAuthEnvProvided(t *testing.T) {
	clearCopilotAuthEnv(t)
	t.Setenv("HOST_B_TOKEN", "github_pat_b")
	t.Setenv("HOST_CLASSIC", "ghp_classic")
	cases := []struct {
		name        string
		cfg         Config
		passthrough []string
		ghToken     string
		hostEnv     map[string]string
		want        bool
	}{
		{name: "none", want: false},
		{name: "--gh-token GH_TOKEN", ghToken: "gho_b", want: true},
		{name: "--gh-token classic token is unsupported", ghToken: "ghp_b", want: false},
		{name: "explicit GH_TOKEN after --gh-token wins", cfg: Config{Env: []string{"GH_TOKEN="}}, ghToken: "gho_b", want: false},
		{name: "explicit usable GH_TOKEN overrides classic --gh-token", cfg: Config{Env: []string{"GH_TOKEN=gho_b"}}, ghToken: "ghp_b", want: true},
		{name: "explicit copilot token", cfg: Config{Env: []string{"COPILOT_GITHUB_TOKEN=x"}}, want: true},
		{name: "explicit empty copilot token still owns the key", cfg: Config{Env: []string{"COPILOT_GITHUB_TOKEN="}}, want: true},
		{name: "passthrough copilot token", passthrough: []string{"COPILOT_GITHUB_TOKEN"}, hostEnv: map[string]string{"COPILOT_GITHUB_TOKEN": "gho_x"}, want: true},
		{name: "explicit GH_TOKEN", cfg: Config{Env: []string{"GH_TOKEN=gho_b"}}, want: true},
		{name: "explicit GITHUB_TOKEN", cfg: Config{Env: []string{"GITHUB_TOKEN=github_pat_b"}}, want: true},
		{name: "bare env key reads host value", cfg: Config{Env: []string{"GITHUB_TOKEN"}}, hostEnv: map[string]string{"GITHUB_TOKEN": "gho_b"}, want: true},
		{name: "unsupported classic GH_TOKEN", cfg: Config{Env: []string{"GH_TOKEN=ghp_classic"}}, want: false},
		{name: "empty GH_TOKEN", cfg: Config{Env: []string{"GH_TOKEN="}}, want: false},
		{name: "GH_TOKEN alias", cfg: Config{EnvFromHost: []string{"GH_TOKEN=HOST_B_TOKEN"}}, want: true},
		{name: "GITHUB_TOKEN alias", cfg: Config{EnvFromHost: []string{"GITHUB_TOKEN=HOST_B_TOKEN"}}, want: true},
		{name: "COPILOT_GITHUB_TOKEN alias", cfg: Config{EnvFromHost: []string{"COPILOT_GITHUB_TOKEN=HOST_CLASSIC"}}, want: true},
		{name: "classic alias", cfg: Config{EnvFromHost: []string{"GH_TOKEN=HOST_CLASSIC"}}, want: false},
		{name: "passthrough GH_TOKEN", passthrough: []string{"GH_TOKEN"}, hostEnv: map[string]string{"GH_TOKEN": "gho_b"}, want: true},
		{name: "passthrough GITHUB_TOKEN", passthrough: []string{"GITHUB_TOKEN"}, hostEnv: map[string]string{"GITHUB_TOKEN": "gho_b"}, want: true},
		{name: "unrelated passthrough", passthrough: []string{"OPENAI_API_KEY"}, hostEnv: map[string]string{"OPENAI_API_KEY": "sk"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.hostEnv {
				t.Setenv(k, v)
			}
			if got := copilotAuthEnvProvided(tc.cfg, tc.passthrough, tc.ghToken); got != tc.want {
				t.Fatalf("copilotAuthEnvProvided = %t, want %t", got, tc.want)
			}
		})
	}
}

// fakeCredentialTools puts fake security, secret-tool, and gh commands first on
// PATH. Selected-account lookups for bob return bobKeychain/bobGh (failing when
// empty); every unscoped lookup returns alice's token.
func fakeCredentialTools(t *testing.T, goos, bobKeychain, bobGh string) {
	t.Helper()
	dir := t.TempDir()
	scripts := map[string]string{
		"security": `case "$*" in *"-a https://github.com:bob"*) [ -n "` + bobKeychain + `" ] || exit 44; echo "` + bobKeychain + `"; exit 0;; esac
echo gho_alice_keychain`,
		"secret-tool": `case "$*" in *"account https://github.com:bob"*) [ -n "` + bobKeychain + `" ] || exit 1; echo "` + bobKeychain + `"; exit 0;; esac
echo gho_alice_keychain`,
		"gh": `case "$*" in *"--hostname github.com --user bob"*) [ -n "` + bobGh + `" ] || exit 1; echo "` + bobGh + `"; exit 0;; esac
echo gho_alice_gh`,
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := copilotKeychainGOOS
	copilotKeychainGOOS = goos
	t.Cleanup(func() { copilotKeychainGOOS = prev })
}

func writeCopilotConfigFile(t *testing.T, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("COPILOT_HOME", "")
	dir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if content == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

const bobSelectedConfig = `// managed
{"lastLoggedInUser": {"host": "https://github.com", "login": "bob"}%s}
`

func TestGetCopilotTokenScopedToSelectedAccount(t *testing.T) {
	bobPlaintext := `, "copilotTokens": {"https://github.com:bob": "gho_bob_plain"}`
	alicePlaintext := `, "copilotTokens": {"https://github.com:alice": "gho_alice_plain"}`
	cases := []struct {
		name        string
		config      string
		bobKeychain string
		bobGh       string
		wantToken   string
		wantFound   bool
	}{
		{name: "selected keychain entry", config: fmt.Sprintf(bobSelectedConfig, ""), bobKeychain: "gho_bob_keychain", wantToken: "gho_bob_keychain", wantFound: true},
		{name: "missing selected keychain entry keeps selected plaintext", config: fmt.Sprintf(bobSelectedConfig, bobPlaintext), wantToken: "", wantFound: true},
		{name: "missing selected keychain entry uses selected gh account", config: fmt.Sprintf(bobSelectedConfig, alicePlaintext), bobGh: "gho_bob_gh", wantToken: "gho_bob_gh", wantFound: true},
		{name: "selected account has no credentials", config: fmt.Sprintf(bobSelectedConfig, alicePlaintext), wantToken: "", wantFound: false},
		{name: "no selected account uses default keychain entry", config: "{}", wantToken: "gho_alice_keychain", wantFound: true},
		{name: "no config uses default keychain entry", config: "", wantToken: "gho_alice_keychain", wantFound: true},
	}
	for _, goos := range []string{"darwin", "linux"} {
		for _, tc := range cases {
			t.Run(goos+"/"+tc.name, func(t *testing.T) {
				writeCopilotConfigFile(t, tc.config)
				fakeCredentialTools(t, goos, tc.bobKeychain, tc.bobGh)
				token, found := getCopilotToken()
				if token != tc.wantToken || found != tc.wantFound {
					t.Fatalf("getCopilotToken() = %q, %t; want %q, %t", token, found, tc.wantToken, tc.wantFound)
				}
			})
		}
	}
}

func TestBuildRunArgsCopilotConfigPreservesSuppliedGitHubTokens(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		hostEnv map[string]string
		inject  bool
	}{
		{name: "no supplied token forwards host login", inject: true},
		{name: "explicit GH_TOKEN", cfg: Config{Env: []string{"GH_TOKEN=gho_b"}}},
		{name: "explicit GITHUB_TOKEN", cfg: Config{Env: []string{"GITHUB_TOKEN=gho_b"}}},
		{name: "GH_TOKEN alias", cfg: Config{EnvFromHost: []string{"GH_TOKEN=HOST_B"}}, hostEnv: map[string]string{"HOST_B": "gho_b"}},
		{name: "GITHUB_TOKEN alias", cfg: Config{EnvFromHost: []string{"GITHUB_TOKEN=HOST_B"}}, hostEnv: map[string]string{"HOST_B": "gho_b"}},
		{name: "GH_TOKEN passthrough", hostEnv: map[string]string{"GH_TOKEN": "gho_b"}},
		{name: "GITHUB_TOKEN passthrough", hostEnv: map[string]string{"GITHUB_TOKEN": "gho_b"}},
		{name: "classic GH_TOKEN passthrough is unsupported", hostEnv: map[string]string{"GH_TOKEN": "ghp_b"}, inject: true},
		{name: "--gh-token GH_TOKEN", cfg: Config{GhToken: true, NoEnvPassthrough: true}},
		{name: "--gh-token cleared by explicit GH_TOKEN", cfg: Config{GhToken: true, NoEnvPassthrough: true, Env: []string{"GH_TOKEN="}}, inject: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupCopilotHome(t)
			clearCopilotAuthEnv(t)
			fakeCredentialTools(t, "darwin", "", "")
			for k, v := range tc.hostEnv {
				t.Setenv(k, v)
			}
			cfg := tc.cfg
			cfg.Image = "test-image"
			cfg.CopilotConfig = true
			args, cleanup, err := buildRunArgs(cfg, t.TempDir(), []string{"copilot"}, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer func() {
				for _, p := range cleanup {
					_ = os.RemoveAll(p)
				}
			}()
			argsStr := strings.Join(args, " ")
			injected := strings.Contains(argsStr, "COPILOT_GITHUB_TOKEN=gho_alice_keychain")
			if injected != tc.inject {
				t.Fatalf("host Copilot login injected = %t, want %t: %s", injected, tc.inject, argsStr)
			}
			if tc.cfg.GhToken && !strings.Contains(argsStr, "GH_TOKEN=gho_alice_gh") {
				t.Fatalf("expected --gh-token to forward GH_TOKEN: %s", argsStr)
			}
		})
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

// copilotSessionEntrypointBlock extracts the Copilot session-state setup from
// the entrypoint printf in the Dockerfile.
func copilotSessionEntrypointBlock(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "'COPILOT_SESSIONS=") {
			inBlock = true
		}
		if !inBlock {
			continue
		}
		if strings.HasPrefix(trimmed, "'# Copy git config") {
			break
		}
		trimmed = strings.TrimSuffix(trimmed, `\`)
		trimmed = strings.TrimSpace(trimmed)
		lines = append(lines, strings.TrimSuffix(strings.TrimPrefix(trimmed, "'"), "'"))
	}
	if len(lines) == 0 {
		t.Fatal("expected Copilot session-state block in entrypoint")
	}
	return strings.Join(lines, "\n")
}

// fakeCopilotContainer models one container's private filesystem view of the
// fixed /var/lib/yolobox path while /home/yolo is shared, by swapping the view
// link to this container's target before it acts.
type fakeCopilotContainer struct {
	view   string
	target string
}

func (c *fakeCopilotContainer) activate(t *testing.T) {
	t.Helper()
	_ = os.Remove(c.view)
	if c.target != "" {
		if err := os.Symlink(c.target, c.view); err != nil {
			t.Fatal(err)
		}
	}
}

func (c *fakeCopilotContainer) start(t *testing.T, block, home, hostSessions string, synced bool) {
	t.Helper()
	c.activate(t)
	script := strings.NewReplacer(
		"/home/yolo", home,
		"/var/lib/yolobox/copilot-session-state", c.view,
		"/host-copilot-session-state", hostSessions,
		"sudo ", "",
	).Replace(block)
	cmd := exec.Command("bash", "-euc", script)
	cmd.Env = append(os.Environ(), "YOLOBOX_COPILOT_SESSIONS=")
	if synced {
		cmd.Env = append(cmd.Env, "YOLOBOX_COPILOT_SESSIONS=1")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("entrypoint session block failed: %v\n%s", err, out)
	}
	target, err := os.Readlink(c.view)
	if err != nil {
		t.Fatalf("expected container-local session view: %v", err)
	}
	c.target = target
}

// write creates a session file through the shared session-state path, the
// way a process running in this container would.
func (c *fakeCopilotContainer) write(t *testing.T, home, name string) {
	t.Helper()
	c.activate(t)
	shared := filepath.Join(home, ".copilot", "session-state")
	if link, err := os.Readlink(shared); err != nil || link != c.view {
		t.Fatalf("shared session-state must stay a stable link to the container view, got %q (%v)", link, err)
	}
	if err := os.WriteFile(filepath.Join(shared, name), []byte(name), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEntrypointCopilotSessionsAreContainerSpecific(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	block := copilotSessionEntrypointBlock(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	hostSessions := filepath.Join(root, "host-sessions")
	for _, dir := range []string{filepath.Join(home, ".copilot", "session-state", "old"), hostSessions, filepath.Join(root, "var")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Existing box-local session history from before the stable view existed.
	if err := os.WriteFile(filepath.Join(home, ".copilot", "session-state", "old", "events.jsonl"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	view := filepath.Join(root, "var", "copilot-session-state")
	synced := &fakeCopilotContainer{view: view}
	local := &fakeCopilotContainer{view: view}

	synced.start(t, block, home, hostSessions, true)
	synced.write(t, home, "before")
	// A no-sync run (for example update-agents) overlaps the synced box.
	local.start(t, block, home, hostSessions, false)
	synced.write(t, home, "after")
	local.write(t, home, "local")
	// Restarting a synced box must not redirect the running local box either.
	(&fakeCopilotContainer{view: view}).start(t, block, home, hostSessions, true)
	local.write(t, home, "local-after")
	synced.write(t, home, "after-restart")

	for _, name := range []string{"before", "after", "after-restart"} {
		if _, err := os.Stat(filepath.Join(hostSessions, name)); err != nil {
			t.Fatalf("synced container write %q did not reach host session-state: %v", name, err)
		}
	}
	localStore := filepath.Join(home, ".copilot", "session-state.container")
	for _, name := range []string{"local", "local-after", "old/events.jsonl"} {
		if _, err := os.Stat(filepath.Join(localStore, name)); err != nil {
			t.Fatalf("expected %q in box-local session store: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(hostSessions, name)); err == nil {
			t.Fatalf("box-local write %q leaked into host session-state", name)
		}
	}
}
