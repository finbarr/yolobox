package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaseCompletionFlagsMatchFlagSet(t *testing.T) {
	flags := baseCompletionFlags()
	byName := map[string]completionFlag{}
	for _, f := range flags {
		byName[f.Name] = f
	}
	for _, want := range []string{"runtime", "docker", "mount", "no-network", "ensure-latest", "copilot-config"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing base flag %q in completions", want)
		}
	}
	if !byName["docker"].IsBool {
		t.Error("--docker should be a bool flag")
	}
	if byName["runtime"].IsBool {
		t.Error("--runtime should take a value")
	}
	if !byName["mount"].Repeat || !byName["env"].Repeat {
		t.Error("--mount and --env should be repeatable")
	}
}

func TestRunCompletionScripts(t *testing.T) {
	for _, shell := range completionShells {
		var out bytes.Buffer
		if err := runCompletion([]string{shell}, &out); err != nil {
			t.Fatalf("completion %s: %v", shell, err)
		}
		script := out.String()
		for _, f := range baseCompletionFlags() {
			if !strings.Contains(script, "--"+f.Name) {
				t.Errorf("%s script missing --%s", shell, f.Name)
			}
		}
		for _, name := range append(completionCommandNames(), toolShortcuts...) {
			if !strings.Contains(script, name) {
				t.Errorf("%s script missing %q", shell, name)
			}
		}
	}
}

func TestRunCompletionErrors(t *testing.T) {
	var out bytes.Buffer
	if err := runCompletion([]string{"fish"}, &out); err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("expected unsupported shell error, got %v", err)
	}
	if err := runCompletion(nil, &out); err == nil {
		t.Fatal("expected error when shell is missing")
	}
	if err := runCompletion([]string{"bash", "extra"}, &out); err == nil {
		t.Fatal("expected error for extra args")
	}
}

func TestCompletionScriptsParse(t *testing.T) {
	for _, shell := range completionShells {
		bin, err := exec.LookPath(shell)
		if err != nil {
			t.Logf("%s not installed; skipping syntax check", shell)
			continue
		}
		var out bytes.Buffer
		if err := runCompletion([]string{shell}, &out); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "yolobox."+shell)
		if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if msg, err := exec.Command(bin, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("%s -n failed: %v\n%s", shell, err, msg)
		}
	}
}
