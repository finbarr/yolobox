package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installLoggingDockerRuntime is like installFakeDockerRuntime but APPENDS every
// invocation (one line per call, args space-joined) so a test can observe the
// full sequence of runtime commands, e.g. a `pull` followed by a `run`.
func installLoggingDockerRuntime(t *testing.T) string {
	t.Helper()
	runtimeDir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "docker-log")
	dockerPath := filepath.Join(runtimeDir, "docker")
	script := `#!/bin/sh
if [ "$1" = "info" ]; then
	echo 8589934592
	exit 0
fi
echo "$*" >> "$YOLOBOX_FAKE_RUNTIME_LOG"
if [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
	case "$3" in
		yolobox-custom:*) exit 1 ;;
		*) echo "sha256:fakebaseimageid"; exit 0 ;;
	esac
fi
exit 0
`
	if err := os.WriteFile(dockerPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YOLOBOX_FAKE_RUNTIME_LOG", logFile)
	return logFile
}

func readRuntimeLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func logHasLine(log []string, prefix string) bool {
	for _, line := range log {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func TestEnsureLatestPullsBaseImage(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	logFile := installLoggingDockerRuntime(t)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"shell", "--ensure-latest"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	log := readRuntimeLog(t, logFile)
	if !logHasLine(log, "pull ghcr.io/finbarr/yolobox:latest") {
		t.Fatalf("expected a base-image pull, got runtime invocations:\n%s", strings.Join(log, "\n"))
	}
	if logHasLine(log, "build ") {
		t.Fatalf("did not expect a build without customization, got:\n%s", strings.Join(log, "\n"))
	}
	// The base image (not a derived image) must run, and the final container
	// command should be the expected `bash` for `shell`.
	sawRun := false
	for _, line := range log {
		if !strings.HasPrefix(line, "run ") {
			continue
		}
		sawRun = true
		if !strings.HasSuffix(line, " bash") {
			t.Fatalf("expected container command to end with `bash`, got: %q", line)
		}
	}
	if !sawRun {
		t.Fatalf("expected a container run, got:\n%s", strings.Join(log, "\n"))
	}
}

func TestEnsureLatestBuildsDerivedImageAfterPull(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(projectDir, ".yolobox.toml"),
		[]byte("[customize]\npackages = [\"jq\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	logFile := installLoggingDockerRuntime(t)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"shell", "--ensure-latest"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	log := readRuntimeLog(t, logFile)
	pullIdx, buildIdx := -1, -1
	for i, line := range log {
		if strings.HasPrefix(line, "pull ghcr.io/finbarr/yolobox:latest") {
			pullIdx = i
		}
		if strings.HasPrefix(line, "build ") {
			buildIdx = i
		}
	}
	if pullIdx == -1 || buildIdx == -1 {
		t.Fatalf("expected both a pull and a build, got:\n%s", strings.Join(log, "\n"))
	}
	if pullIdx > buildIdx {
		t.Fatalf("expected the pull to precede the build, got pull@%d build@%d:\n%s", pullIdx, buildIdx, strings.Join(log, "\n"))
	}
}

func TestEnsureLatestPullsUnderNoNetwork(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	logFile := installLoggingDockerRuntime(t)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"shell", "--ensure-latest", "--no-network"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	log := readRuntimeLog(t, logFile)
	if !logHasLine(log, "pull ghcr.io/finbarr/yolobox:latest") {
		t.Fatalf("expected the pull to still happen under --no-network, got:\n%s", strings.Join(log, "\n"))
	}
}

func TestEnsureLatestToolShortcutNotForwardedToTool(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	logFile := installLoggingDockerRuntime(t)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"claude", "--ensure-latest"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	log := readRuntimeLog(t, logFile)
	if !logHasLine(log, "pull ghcr.io/finbarr/yolobox:latest") {
		t.Fatalf("expected a base-image pull, got:\n%s", strings.Join(log, "\n"))
	}
	// The flag must be consumed as a yolobox flag, not passed through to claude.
	for _, line := range log {
		if strings.HasPrefix(line, "run ") && strings.Contains(line, "--ensure-latest") {
			t.Fatalf("--ensure-latest leaked into the container command: %q", line)
		}
		if strings.HasPrefix(line, "run ") && !strings.HasSuffix(line, " claude") {
			t.Fatalf("expected container command to end with `claude`, got: %q", line)
		}
	}
}
