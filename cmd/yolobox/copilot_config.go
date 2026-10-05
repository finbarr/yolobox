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

func copilotConfigHasPlaintextToken(config map[string]interface{}) bool {
	for _, key := range []string{"copilotTokens", "copilot_tokens"} {
		if tokens, ok := config[key].(map[string]interface{}); ok && len(tokens) > 0 {
			return true
		}
	}
	return false
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

// copilotTokenEnvProvided reports whether COPILOT_GITHUB_TOKEN is already
// being set explicitly or by automatic env passthrough.
func copilotTokenEnvProvided(cfg Config, aliasedEnvKeys map[string]bool, autoPassthroughEnvKeys []string) bool {
	const key = "COPILOT_GITHUB_TOKEN"
	if aliasedEnvKeys[key] {
		return true
	}
	for _, k := range autoPassthroughEnvKeys {
		if k == key {
			return true
		}
	}
	for _, env := range cfg.Env {
		name, _, _ := strings.Cut(env, "=")
		if name == key {
			return true
		}
	}
	return false
}

// copilotTokenUsable filters out token types Copilot CLI rejects.
func copilotTokenUsable(token string) bool {
	return token != "" && !strings.HasPrefix(token, "ghp_")
}

// getCopilotToken resolves the host Copilot login the same way Copilot CLI
// does after env vars: OS keychain, plaintext config, then `gh auth token`.
// A plaintext config token is synced with config.json, so it is not returned,
// but found still reports true.
func getCopilotToken() (token string, found bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	config, _ := readCopilotConfig(filepath.Join(copilotHomeDir(home), "config.json"))
	if token := getCopilotKeychainToken(config); copilotTokenUsable(token) {
		return token, true
	}
	if copilotConfigHasPlaintextToken(config) {
		return "", true
	}
	if token := getGhToken(); copilotTokenUsable(token) {
		return token, true
	}
	return "", false
}

func copilotLastLoggedInAccount(config map[string]interface{}) string {
	for _, key := range []string{"lastLoggedInUser", "last_logged_in_user"} {
		user, ok := config[key].(map[string]interface{})
		if !ok {
			continue
		}
		host, _ := user["host"].(string)
		login, _ := user["login"].(string)
		if host != "" && login != "" {
			return host + ":" + login
		}
	}
	return ""
}

func getCopilotKeychainToken(config map[string]interface{}) string {
	switch runtime.GOOS {
	case "darwin":
		if account := copilotLastLoggedInAccount(config); account != "" {
			if token := runCredentialCommand("security", "find-generic-password", "-s", copilotKeychainService, "-a", account, "-w"); token != "" {
				return token
			}
		}
		return runCredentialCommand("security", "find-generic-password", "-s", copilotKeychainService, "-w")
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "secret-tool", "lookup", "service", copilotKeychainService).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return ""
}

func runCredentialCommand(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
