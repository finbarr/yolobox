package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidateScratchLogin(t *testing.T) {
	base := Config{Scratch: true, ReuseLogin: true, NoEnvPassthrough: true}
	tests := []struct {
		name    string
		command []string
		edit    func(*Config)
		want    string
	}{
		{name: "requires scratch", command: []string{"codex"}, edit: func(cfg *Config) { cfg.Scratch = false }, want: "requires --scratch"},
		{name: "unrelated tool", command: []string{"bash"}, want: "Claude or Codex launch"},
		{name: "codex host sync", command: []string{"codex"}, edit: func(cfg *Config) { cfg.CodexConfig = true }, want: "--codex-config"},
		{name: "claude host sync", command: []string{"claude"}, edit: func(cfg *Config) { cfg.ClaudeConfig = true }, want: "--claude-config"},
		{name: "other agent host sync", command: []string{"codex"}, edit: func(cfg *Config) { cfg.ClaudeConfig = true }},
		{name: "explicit env", command: []string{"codex"}, edit: func(cfg *Config) { cfg.Env = []string{"OPENAI_API_KEY=dummy"} }, want: "OPENAI_API_KEY"},
		{name: "host alias", command: []string{"claude"}, edit: func(cfg *Config) { cfg.EnvFromHost = []string{"CLAUDE_CODE_OAUTH_TOKEN=MOCK"} }, want: "CLAUDE_CODE_OAUTH_TOKEN"},
		{name: "runtime env", command: []string{"codex"}, edit: func(cfg *Config) { cfg.RuntimeArgs = []string{"--env=CODEX_HOME=/tmp"} }, want: "CODEX_HOME"},
		{name: "unrelated environment", command: []string{"claude"}, edit: func(cfg *Config) { cfg.Env = []string{"OPENAI_API_KEY=dummy"} }},
		{name: "automatic env", command: []string{"codex"}, edit: func(cfg *Config) { cfg.NoEnvPassthrough = false }, want: "OPENAI_API_KEY"},
		{name: "claude bare", command: []string{"claude", "--bare"}, want: "--bare"},
		{name: "codex remote", command: []string{"codex", "--remote=wss://example.invalid"}, want: "--remote"},
		{name: "option-like prompt", command: []string{"claude", "--", "--bare"}},
	}
	t.Setenv("OPENAI_API_KEY", "dummy-auto")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			if test.edit != nil {
				test.edit(&cfg)
			}
			err := validateScratchLogin(cfg, test.command)
			if test.want == "" && err == nil {
				return
			}
			if err == nil || test.want == "" || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateScratchLogin returned %v, want %q", err, test.want)
			}
			if strings.Contains(err.Error(), "dummy") {
				t.Fatal("validation leaked an env value")
			}
		})
	}
}

func TestScratchLoginRunDoesNotReceiveSourceHomeOrTokenInArgs(t *testing.T) {
	const token = "sample-private-access-token"
	cfg := Config{Scratch: true, ReuseLogin: true, NoEnvPassthrough: true, Image: "test-image", scratchLogin: token}
	args, cleanup, err := buildRunArgs(cfg, t.TempDir(), []string{"claude", "--version"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleanup) != 0 {
		t.Fatalf("scratch login created %d staging files on the host", len(cleanup))
	}
	if !slices.Contains(args, scratchLoginEnv) {
		t.Fatal("expected a name-only environment handoff to the runtime")
	}
	for _, arg := range args {
		if strings.Contains(arg, token) || strings.Contains(arg, "yolobox-home") || strings.HasPrefix(arg, scratchLoginEnv+"=") {
			t.Fatal("the scratch agent command exposed its credential or source home in arguments")
		}
		if encoded, ok := strings.CutPrefix(arg, yoloboxContextPayloadEnv+"="); ok {
			manifest, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(manifest), token) {
				t.Fatal("the context manifest exposed a credential")
			}
		}
	}
}

func TestPrepareScratchLoginUsesOnlyTrustedHelper(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "runtime-args")
	const fixture = `#!/bin/sh
if [ "$1" = volume ]; then
    printf '%s\n' "$MOCK_VOLUME"
    exit
fi
printf '%s\0' "$@" > "$MOCK_ARGS_LOG"
if [ "$MOCK_RESULT" = invalid ]; then
    printf 'sample raw credential must never surface\n' >&2
    exit 3
fi
printf '%s\n' '{"tool":"claude","access_token":"sample-token","expires_at":2000000000}'
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	arch := "arm64"
	if nativeArch == arch {
		arch = "amd64"
	}
	volume := "yolobox-home-" + arch
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MOCK_ARGS_LOG", log)
	t.Setenv("MOCK_VOLUME", volume)
	cfg := Config{Runtime: "docker", Platform: "linux/" + arch, Scratch: true, ReuseLogin: true,
		Image: "project-image:latest", NoEnvPassthrough: true, Mounts: []string{"/outside:/inside"}}

	payload, err := prepareScratchLogin(cfg, []string{"claude", "user prompt"})
	if err != nil || !json.Valid([]byte(payload)) || !strings.Contains(payload, "sample-token") {
		t.Fatalf("prepareScratchLogin did not return the mock payload: %v", err)
	}
	logged, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(string(logged), "\x00")
	for _, expected := range []string{defaultConfig().Image, "--read-only", "--network", "none", "--platform", "linux/" + arch,
		"/usr/bin/python3", "export", "claude", volume + ":/loan-home:ro,nocopy"} {
		if !slices.Contains(args, expected) {
			t.Errorf("trusted helper missing %q", expected)
		}
	}
	for _, unexpected := range []string{"project-image:latest", "user prompt", "/outside:/inside", "-i", "-t", "sample-token"} {
		if slices.Contains(args, unexpected) {
			t.Errorf("trusted helper received %q", unexpected)
		}
	}

	t.Setenv("MOCK_RESULT", "invalid")
	if _, err := prepareScratchLogin(cfg, []string{"claude"}); err == nil || !strings.Contains(err.Error(), "no valid cached claude access token") || strings.Contains(err.Error(), "sample raw credential") {
		t.Fatalf("expected credential-safe invalid token error, got %v", err)
	}
	t.Setenv("MOCK_VOLUME", "unrelated-volume")
	if _, err := prepareScratchLogin(cfg, []string{"claude"}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing home volume error, got %v", err)
	}
}

func TestScratchLoginHelperFiltersCredentials(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required to exercise the embedded helper")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "login.py")
	if err := os.WriteFile(script, []byte(scratchLoginScript), 0600); err != nil {
		t.Fatal(err)
	}
	const checks = `
import base64, json, os, runpy, sys
from pathlib import Path

module = runpy.run_path(sys.argv[1])
export_login = module['export_login']
import_login = module['import_login']
home = Path(sys.argv[2]) / 'source'
scratch = Path(sys.argv[2]) / 'scratch'
for directory in (home / '.claude', home / '.codex', scratch):
    directory.mkdir(parents=True)

claude = home / '.claude' / '.credentials.json'
claude.write_text(json.dumps({'claudeAiOauth': {'accessToken': 'sk-ant-oat01-fake', 'refreshToken': 'do-not-copy', 'expiresAt': 100000}}))
payload = export_login(home, 'claude', 99)
assert payload == {'tool': 'claude', 'access_token': 'sk-ant-oat01-fake', 'expires_at': 100}
assert import_login(scratch, payload, 99) == 'sk-ant-oat01-fake'
assert list(scratch.iterdir()) == []

claims = base64.urlsafe_b64encode(json.dumps({'exp': 100, 'https://api.openai.com/auth': {'chatgpt_account_id': 'fallback-account'}}).encode()).decode().rstrip('=')
jwt = 'header.' + claims + '.signature'
codex = home / '.codex' / 'auth.json'
codex.write_text(json.dumps({'auth_mode': 'chatgpt', 'tokens': {'access_token': jwt, 'id_token': 'unused-id', 'refresh_token': 'do-not-copy', 'account_id': 'selected-account'}}))
payload = export_login(home, 'codex', 99)
assert payload == {'tool': 'codex', 'access_token': jwt, 'account_id': 'selected-account', 'expires_at': 100}
assert import_login(scratch, payload, 99) == ''
installed = scratch / '.codex' / 'auth.json'
assert os.stat(installed).st_mode & 0o777 == 0o600
auth = json.loads(installed.read_text())
assert auth['auth_mode'] == 'chatgptAuthTokens'
assert auth['tokens'] == {'access_token': jwt, 'id_token': jwt, 'account_id': 'selected-account', 'refresh_token': ''}
assert 'do-not-copy' in codex.read_text()

def fails(action, exc):
    try:
        action()
    except exc:
        return
    raise AssertionError('expected operation to fail')

fails(lambda: export_login(home, 'codex', 100), ValueError)
fails(lambda: export_login(home, 'claude', 100), ValueError)
fails(lambda: import_login(scratch, payload, 100), ValueError)
fails(lambda: import_login(scratch, payload, 99), OSError)

claude.rename(claude.with_suffix('.real'))
claude.symlink_to(claude.with_suffix('.real'))
fails(lambda: export_login(home, 'claude', 99), OSError)
codex.unlink()
os.mkfifo(codex)
fails(lambda: export_login(home, 'codex', 99), ValueError)
`
	output, err := exec.Command(python, "-I", "-c", checks, script, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("embedded scratch login helper check failed: %v\n%s", err, output)
	}
}
