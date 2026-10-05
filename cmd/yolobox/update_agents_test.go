package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUpdateAgentsShellScriptUpdatesSelectedTools(t *testing.T) {
	for _, tt := range []struct {
		name    string
		targets []string
		calls   []string
	}{
		{
			name:    "claude",
			targets: []string{"claude"},
			calls: []string{
				"claude update",
				"claude --version",
				"npm install -g --no-audit --no-fund @agentclientprotocol/claude-agent-acp@latest",
				"claude-agent-acp --version",
			},
		},
		{
			name:    "codex",
			targets: []string{"codex"},
			calls: []string{
				"npm install -g --no-audit --no-fund @openai/codex@latest",
				"codex --version",
				"npm install -g --no-audit --no-fund @agentclientprotocol/codex-acp@latest",
				"codex-acp --version",
			},
		},
		{
			name:    "unrelated tool",
			targets: []string{"gemini"},
			calls: []string{
				"npm install -g --no-audit --no-fund @google/gemini-cli@latest",
				"gemini --version",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, calls, err := runUpdateAgentsScript(t, tt.targets, "")
			if err != nil {
				t.Fatalf("update script failed: %v\n%s", err, output)
			}
			if !reflect.DeepEqual(calls, tt.calls) {
				t.Fatalf("unexpected update order or targets:\n got %q\nwant %q", calls, tt.calls)
			}
			if !strings.Contains(output, "agent updates complete\n") {
				t.Fatalf("missing completion message:\n%s", output)
			}
		})
	}
}

func TestUpdateAgentsShellScriptStopsOnUpdateFailure(t *testing.T) {
	for _, tt := range []struct {
		name    string
		targets []string
		fail    string
		calls   []string
	}{
		{
			name:    "Claude CLI failure skips adapter",
			targets: []string{"claude", "gemini"},
			fail:    "claude update",
			calls:   []string{"claude update"},
		},
		{
			name:    "Codex CLI failure skips adapter",
			targets: []string{"codex", "gemini"},
			fail:    "@openai/codex@latest",
			calls:   []string{"npm install -g --no-audit --no-fund @openai/codex@latest"},
		},
		{
			name:    "Claude adapter failure skips remaining targets",
			targets: []string{"claude", "gemini"},
			fail:    "@agentclientprotocol/claude-agent-acp@latest",
			calls: []string{
				"claude update",
				"claude --version",
				"npm install -g --no-audit --no-fund @agentclientprotocol/claude-agent-acp@latest",
			},
		},
		{
			name:    "Codex adapter failure skips remaining targets",
			targets: []string{"codex", "gemini"},
			fail:    "@agentclientprotocol/codex-acp@latest",
			calls: []string{
				"npm install -g --no-audit --no-fund @openai/codex@latest",
				"codex --version",
				"npm install -g --no-audit --no-fund @agentclientprotocol/codex-acp@latest",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, calls, err := runUpdateAgentsScript(t, tt.targets, tt.fail)
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
				t.Fatalf("expected update failure exit 42, got %v\n%s", err, output)
			}
			if !reflect.DeepEqual(calls, tt.calls) {
				t.Fatalf("unexpected calls after failure:\n got %q\nwant %q", calls, tt.calls)
			}
			if strings.Contains(output, "agent updates complete") {
				t.Fatalf("failed update claimed completion:\n%s", output)
			}
		})
	}
}

func runUpdateAgentsScript(t *testing.T, targets []string, fail string) (string, []string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to execute the agent update script")
	}
	home := t.TempDir()
	binDir := filepath.Join(home, "fake-bin")
	if err := os.Mkdir(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(home, "calls.log")
	versionScript := filepath.Join(home, "version.sh")
	writeScript := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("#!"+bash+"\nset -euo pipefail\n"+body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeScript(versionScript, `
bin="${0##*/}"
printf '%s %s\n' "$bin" "$*" >> "$UPDATE_TEST_LOG"
[ "${NO_YOLO:-}" = 1 ]
[ "$*" = --version ]
printf '%s test-version\n' "$bin"
`)
	writeScript(filepath.Join(binDir, "claude"), `
printf 'claude %s\n' "$*" >> "$UPDATE_TEST_LOG"
[ "${NO_YOLO:-}" = 1 ]
if [ "claude $*" = "$UPDATE_TEST_FAIL" ]; then exit 42; fi
case "$*" in
    update) ;;
    --version) printf 'Claude test-version\n' ;;
    *) exit 43 ;;
esac
`)
	writeScript(filepath.Join(binDir, "npm"), `
printf 'npm %s\n' "$*" >> "$UPDATE_TEST_LOG"
package="${!#}"
if [ "$package" = "$UPDATE_TEST_FAIL" ]; then exit 42; fi
case "$package" in
    @openai/codex@latest) bin=codex ;;
    @agentclientprotocol/codex-acp@latest) bin=codex-acp ;;
    @agentclientprotocol/claude-agent-acp@latest) bin=claude-agent-acp ;;
    @google/gemini-cli@latest) bin=gemini ;;
    *) exit 43 ;;
esac
cp "$UPDATE_TEST_VERSION_SCRIPT" "$NPM_CONFIG_PREFIX/bin/$bin"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", updateAgentsShellScript(targets))
	// Do not inherit host npm settings, credentials, or user-installed agent commands.
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + binDir + ":/usr/bin:/bin",
		"UPDATE_TEST_LOG=" + logFile,
		"UPDATE_TEST_FAIL=" + fail,
		"UPDATE_TEST_VERSION_SCRIPT=" + versionScript,
	}
	output, runErr := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("update script timed out: %v\n%s", ctx.Err(), output)
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading executed command log: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".npm-global", "bin")); err != nil {
		t.Fatalf("default persistent npm prefix was not created: %v\n%s", err, output)
	}
	return string(output), strings.Split(strings.TrimSpace(string(data)), "\n"), runErr
}
