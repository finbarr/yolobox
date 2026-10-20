package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installNoisyDockerRuntime installs a fake runtime whose `pull`, `build` and
// `run` all write to stdout, the way `podman build` streams its STEP log.  The
// custom-image inspect fails so a build is always required.
func installNoisyDockerRuntime(t *testing.T) {
	t.Helper()

	runtimeDir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "info" ]; then
	echo 8589934592
	exit 0
fi
if [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
	case "$3" in
		yolobox-custom:*) exit 1 ;;
		*) echo "sha256:fakebaseimageid"; exit 0 ;;
	esac
fi
case "$1" in
	pull) echo "PULL-PROGRESS"; cat >/dev/null ;;
	build) echo "STEP 1/2: FROM fake"; cat >/dev/null ;;
	run) echo "COMMAND-OUTPUT"; cat ;;
esac
exit 0
`
	dockerPath := filepath.Join(runtimeDir, "docker")
	if err := os.WriteFile(dockerPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake docker runtime: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// captureStdout runs fn with os.Stdout replaced by a pipe and returns what was
// written to it, including by child processes that inherit it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		_ = w.Close()
		_ = r.Close()
	}()
	fn()
	os.Stdout = oldStdout
	_ = w.Close()

	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	_ = r.Close()
	return out.String()
}

// `yolobox run` is a stdio transport: `yolobox run codex-acp` and `yolobox run
// claude-agent-acp` speak JSON-RPC over the pipe an ACP client is reading, so
// image pull and build progress must not be written there.
func TestImagePrepPreservesCommandStdio(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	installNoisyDockerRuntime(t)

	// A client can send its first protocol request before image preparation
	// finishes. Even a runtime that reads stdin must leave it for the command.
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = input
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = input.Close()
		_ = writer.Close()
	})
	const request = "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n"
	if _, err := writer.WriteString(request); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var runErr error
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			runErr = runCmdArgs([]string{"run", "--ensure-latest", "--packages", "cowsay", "true"}, projectDir, nil)
		})
	})
	if runErr != nil {
		t.Fatalf("runCmdArgs: %v", runErr)
	}

	for _, noise := range []string{"PULL-PROGRESS", "STEP 1/2: FROM fake"} {
		if strings.Contains(stdout, noise) {
			t.Errorf("image preparation wrote %q to stdout:\n%s", noise, stdout)
		}
		if !strings.Contains(stderr, noise) {
			t.Errorf("expected %q on stderr, got:\n%s", noise, stderr)
		}
	}

	// The container command's own output still belongs on stdout; redirecting
	// the preparation steps must not take that with it.
	if want := "COMMAND-OUTPUT\n" + request; stdout != want {
		t.Errorf("command stdout = %q, want %q (including untouched stdin)", stdout, want)
	}
}
