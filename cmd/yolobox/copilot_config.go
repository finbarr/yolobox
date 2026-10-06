package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const copilotKeychainService = "copilot-cli"

// copilotAuthConfigKeys are config.json keys that carry Copilot CLI login
// state. Recent CLIs use camelCase; older releases used snake_case.
var copilotAuthConfigKeys = []string{
	"copilotTokens",
	"copilot_tokens",
	"authTokens",
	"loggedInUsers",
	"logged_in_users",
	"lastLoggedInUser",
	"last_logged_in_user",
}

// copilotSyncExcludedRootNames are root-level ~/.copilot entries that are
// host-specific, volatile, or handled separately. Keep in sync with the
// entrypoint rsync excludes in the Dockerfile.
var copilotSyncExcludedRootNames = map[string]bool{
	"config.json":              true, // preprocessed and mounted separately
	"session-state":            true, // live-mounted
	"pkg":                      true, // host-platform CLI binaries
	"Library":                  true, // macOS caches
	"logs":                     true,
	"run":                      true,
	"ide":                      true, // host IDE lock files
	"computer-use":             true,
	"media-cache":              true,
	"canvas-catalog-probe":     true,
	"sidebar-sessions-state":   true,
	"open-sessions-state.json": true,
}

func copilotSyncExcludesRootName(name string) bool {
	if copilotSyncExcludedRootNames[name] {
		return true
	}
	return strings.HasSuffix(name, ".db") ||
		strings.Contains(name, ".db-") ||
		strings.Contains(name, ".db.") ||
		strings.HasSuffix(name, ".lock")
}

func copilotRootExclusions(dir string) map[string]bool {
	exclusions := map[string]bool{}
	for name := range copilotSyncExcludedRootNames {
		exclusions[name] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return exclusions
	}
	for _, e := range entries {
		if copilotSyncExcludesRootName(e.Name()) {
			exclusions[e.Name()] = true
		}
	}
	return exclusions
}

// copilotHomeDir mirrors Copilot CLI's own resolution of its config directory.
func copilotHomeDir(home string) string {
	if dir := os.Getenv("COPILOT_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".copilot")
}

// readCopilotConfig parses Copilot's config.json, which starts with `//`
// comment lines that plain JSON parsers reject.
func readCopilotConfig(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cleaned bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	var config map[string]interface{}
	if err := json.Unmarshal(cleaned.Bytes(), &config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, fmt.Errorf("%s is not a JSON object", path)
	}
	return config, nil
}

// preprocessCopilotConfig writes a comment-free copy of config.json to a
// Docker-visible temp file, optionally stripping login state.
func preprocessCopilotConfig(srcPath string, stripAuth bool) (string, error) {
	config, err := readCopilotConfig(srcPath)
	if err != nil {
		return "", err
	}
	if stripAuth {
		for _, key := range copilotAuthConfigKeys {
			delete(config, key)
		}
	}
	processed, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	tmpDir := filepath.Join(home, ".yolobox", "tmp")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(tmpDir, "copilot-config-*.json")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(processed); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := os.Chmod(f.Name(), 0600); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// copilotConfigMounts returns runtime args that expose host Copilot config to
// the entrypoint import area, plus temp paths to clean up and, for Apple
// container, files to place under the host-files directory.
func copilotConfigMounts(noAuth, appleContainer bool) ([]string, []string, map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, nil, err
	}
	dir := copilotHomeDir(home)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, nil, nil, nil
	}

	var args, cleanup []string
	files := map[string]string{}
	exclusions := copilotRootExclusions(dir)

	sessionsDir := filepath.Join(dir, "session-state")
	if info, err := os.Stat(sessionsDir); err == nil && info.IsDir() {
		mountSrc := sessionsDir
		if resolved, err := filepath.EvalSymlinks(sessionsDir); err == nil {
			mountSrc = resolved
		}
		args = append(args, "-v", mountSrc+":/host-copilot-session-state:rw")
		args = append(args, "-e", "YOLOBOX_COPILOT_SESSIONS=1")
	}

	mountSrc := dir
	if noAuth {
		// Stage so host config.json (which may hold plaintext tokens) is never
		// visible inside the box.
		staged, err := stageDirResolvingSymlinksExcludingRoot(dir, nil, exclusions)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to stage Copilot config without host authentication: %w", err)
		}
		mountSrc = staged
		cleanup = append(cleanup, staged)
	} else if dirContainsSymlinksExcludingRoot(dir, exclusions) {
		staged, err := stageDirResolvingSymlinksExcludingRoot(dir, nil, exclusions)
		if err != nil {
			warn("Failed to resolve symlinks in %s: %s", dir, err)
		} else {
			mountSrc = staged
			cleanup = append(cleanup, staged)
		}
	}
	args = append(args, "-v", mountSrc+":/host-copilot/.copilot:ro")

	configPath := filepath.Join(dir, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		processed, err := preprocessCopilotConfig(configPath, noAuth)
		if err != nil {
			warn("Failed to read host Copilot config %s: %s", configPath, err)
		} else {
			cleanup = append(cleanup, processed)
			if appleContainer {
				files[processed] = "copilot/config.json"
			} else {
				args = append(args, "-v", processed+":/host-copilot/config.json:ro")
			}
		}
	}

	return args, cleanup, files, nil
}

// copilotAuthEnvKeys are the env vars Copilot CLI reads for a login, in
// precedence order. Any of them outranks the OS keychain.
var copilotAuthEnvKeys = []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"}

// copilotAuthEnvProvided reports whether the caller already supplies a Copilot
// login through explicit env values, env_from_host aliases, or automatic
// passthrough. Injecting a host login on top would silently override the
// caller's account, since COPILOT_GITHUB_TOKEN has the highest precedence.
func copilotAuthEnvProvided(cfg Config, autoPassthroughEnvKeys []string) bool {
	values := map[string]string{}
	present := map[string]bool{}
	for _, key := range autoPassthroughEnvKeys {
		values[key] = os.Getenv(key)
		present[key] = true
	}
	for _, env := range cfg.Env {
		name, value, hasValue := strings.Cut(env, "=")
		if !hasValue {
			value = os.Getenv(name)
		}
		values[name] = value
		present[name] = true
	}
	for _, entry := range cfg.EnvFromHost {
		key, hostVar, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		values[key] = os.Getenv(hostVar)
		present[key] = true
	}
	// Never add a competing COPILOT_GITHUB_TOKEN, whatever its value.
	if present["COPILOT_GITHUB_TOKEN"] {
		return true
	}
	for _, key := range copilotAuthEnvKeys[1:] {
		if copilotTokenUsable(values[key]) {
			return true
		}
	}
	return false
}

// copilotTokenUsable filters out token types Copilot CLI rejects.
func copilotTokenUsable(token string) bool {
	return token != "" && !strings.HasPrefix(token, "ghp_")
}

// copilotAccount identifies the Copilot login selected by lastLoggedInUser.
type copilotAccount struct {
	host  string
	login string
}

// key matches Copilot's keychain account and plaintext token map key.
func (a copilotAccount) key() string {
	return a.host + ":" + a.login
}

func (a copilotAccount) ghHostname() string {
	host := strings.TrimPrefix(strings.TrimPrefix(a.host, "https://"), "http://")
	return strings.TrimSuffix(host, "/")
}

// getCopilotToken resolves the host Copilot login the same way Copilot CLI
// does after env vars: OS keychain, plaintext config, then `gh auth token`.
// A plaintext config token is synced with config.json, so it is not returned,
// but found still reports true. When config selects an account, every source
// is scoped to it so another stored account is never forwarded instead.
func getCopilotToken() (token string, found bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	config, _ := readCopilotConfig(filepath.Join(copilotHomeDir(home), "config.json"))
	account, selected := copilotLastLoggedInAccount(config)
	if token := getCopilotKeychainToken(account, selected); copilotTokenUsable(token) {
		return token, true
	}
	if copilotConfigHasPlaintextToken(config, account, selected) {
		return "", true
	}
	if token := getCopilotGhToken(account, selected); copilotTokenUsable(token) {
		return token, true
	}
	return "", false
}

func copilotLastLoggedInAccount(config map[string]interface{}) (copilotAccount, bool) {
	for _, key := range []string{"lastLoggedInUser", "last_logged_in_user"} {
		user, ok := config[key].(map[string]interface{})
		if !ok {
			continue
		}
		host, _ := user["host"].(string)
		login, _ := user["login"].(string)
		if host != "" && login != "" {
			return copilotAccount{host: host, login: login}, true
		}
	}
	return copilotAccount{}, false
}

func copilotConfigHasPlaintextToken(config map[string]interface{}, account copilotAccount, selected bool) bool {
	for _, key := range []string{"copilotTokens", "copilot_tokens"} {
		tokens, ok := config[key].(map[string]interface{})
		if !ok || len(tokens) == 0 {
			continue
		}
		if !selected {
			return true
		}
		if token, ok := tokens[account.key()].(string); ok && token != "" {
			return true
		}
	}
	return false
}

// copilotKeychainGOOS is overridable so tests can exercise each platform path.
var copilotKeychainGOOS = runtime.GOOS

func getCopilotKeychainToken(account copilotAccount, selected bool) string {
	switch copilotKeychainGOOS {
	case "darwin":
		if selected {
			return runCredentialCommand("security", "find-generic-password", "-s", copilotKeychainService, "-a", account.key(), "-w")
		}
		return runCredentialCommand("security", "find-generic-password", "-s", copilotKeychainService, "-w")
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return ""
		}
		if selected {
			return runCredentialCommand("secret-tool", "lookup", "service", copilotKeychainService, "account", account.key())
		}
		return runCredentialCommand("secret-tool", "lookup", "service", copilotKeychainService)
	}
	return ""
}

// getCopilotGhToken mirrors Copilot's `gh auth token` fallback, scoped to the
// selected account when config names one.
func getCopilotGhToken(account copilotAccount, selected bool) string {
	if !selected {
		return getGhToken()
	}
	return runCredentialCommand("gh", "auth", "token", "--hostname", account.ghHostname(), "--user", account.login)
}

func runCredentialCommand(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
