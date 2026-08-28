package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildLine returns the single `build ...` invocation recorded in the fake
// runtime log, failing the test when there is not exactly one.
func buildLine(t *testing.T, log []string) string {
	t.Helper()
	var found []string
	for _, line := range log {
		if strings.HasPrefix(line, "build ") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one build invocation, got %d:\n%s", len(found), strings.Join(log, "\n"))
	}
	return found[0]
}

// chdir switches to dir for the duration of the test. prepareCustomImage labels
// images with the working directory, which for a fork is the copied folder
// rather than the original project, so a test that asserts on the label has to
// run from the directory it expects.
// It returns the resolved working directory, which on macOS differs from the
// path handed in (/var/... versus /private/var/...).
func chdir(t *testing.T, dir string) string {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	resolved, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after chdir: %v", err)
	}
	return resolved
}

func TestCustomImageBuildRecordsProjectLabel(t *testing.T) {
	projectDir := t.TempDir()
	resolvedDir := chdir(t, projectDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(projectDir, ".yolobox.toml"),
		[]byte("[customize]\npackages = [\"jq\"]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	logFile := installLoggingDockerRuntime(t)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"shell"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	build := buildLine(t, readRuntimeLog(t, logFile))
	want := "--label " + customImageProjectLabel + "=" + resolvedDir
	if !strings.Contains(build, want) {
		t.Fatalf("expected build to carry %q, got: %q", want, build)
	}
}
