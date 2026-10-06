package main

import (
	"bytes"
	"errors"
	"flag"
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

func TestNewBaseFlagSetAppliesEveryFlag(t *testing.T) {
	fs, applyFlags := newBaseFlagSet("test")
	var args []string
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "no-ssh-agent" {
			return
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			args = append(args, "--"+f.Name)
			return
		}
		args = append(args, "--"+f.Name, "val-"+f.Name)
	})
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg := Config{SSHAgent: false}
	if err := applyFlags(&cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}

	checks := map[string]bool{
		"runtime":       cfg.Runtime == "val-runtime",
		"image":         cfg.Image == "val-image",
		"platform":      cfg.Platform == "val-platform",
		"name":          cfg.ContainerName != "",
		"pod":           cfg.Pod == "val-pod",
		"ssh-agent":     cfg.SSHAgent,
		"readonly":      cfg.ReadonlyProject,
		"no-network":    cfg.NoNetwork,
		"no-env":        cfg.NoEnvPassthrough,
		"network":       cfg.Network == "val-network",
		"no-yolo":       cfg.NoYolo,
		"scratch":       cfg.Scratch,
		"claude":        cfg.ClaudeConfig && cfg.NoClaudeAuth,
		"codex":         cfg.CodexConfig,
		"copilot":       cfg.CopilotConfig && cfg.NoCopilotAuth,
		"gemini":        cfg.GeminiConfig,
		"kimi":          cfg.KimiConfig,
		"opencode":      cfg.OpencodeConfig,
		"pi":            cfg.PiConfig,
		"git":           cfg.GitConfig,
		"gh-token":      cfg.GhToken,
		"rtk":           cfg.RTK,
		"copy-agent":    cfg.CopyAgentInstructions,
		"no-project":    cfg.NoProject,
		"docker":        cfg.Docker,
		"clipboard":     cfg.Clipboard,
		"open-bridge":   cfg.OpenBridge,
		"setup":         cfg.Setup,
		"mount":         len(cfg.Mounts) == 1,
		"exclude":       len(cfg.Exclude) == 1,
		"copy-as":       len(cfg.CopyAs) == 1,
		"env":           len(cfg.Env) == 1,
		"env-from-host": len(cfg.EnvFromHost) == 1,
		"cpus":          cfg.CPUs == "val-cpus",
		"memory":        cfg.Memory == "val-memory",
		"shm-size":      cfg.ShmSize == "val-shm-size",
		"gpus":          cfg.GPUs == "val-gpus",
		"device":        len(cfg.Devices) == 1,
		"cap-add":       len(cfg.CapAdd) == 1,
		"cap-drop":      len(cfg.CapDrop) == 1,
		"runtime-arg":   len(cfg.RuntimeArgs) == 1,
		"packages":      len(cfg.Customize.Packages) == 1,
		"customize":     cfg.Customize.Dockerfile != "",
		"rebuild-image": cfg.RebuildImage,
		"ensure-latest": cfg.EnsureLatest,
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("flag %s was not applied to config", name)
		}
	}
}

func TestNewBaseFlagSetSSHAgentFlags(t *testing.T) {
	fs, applyFlags := newBaseFlagSet("test")
	if err := fs.Parse([]string{"--no-ssh-agent"}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SSHAgent: true}
	if err := applyFlags(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SSHAgent {
		t.Error("--no-ssh-agent should disable SSHAgent")
	}

	fs, applyFlags = newBaseFlagSet("test")
	if err := fs.Parse([]string{"--ssh-agent", "--no-ssh-agent"}); err != nil {
		t.Fatal(err)
	}
	if err := applyFlags(&Config{}); err == nil || !strings.Contains(err.Error(), "--no-ssh-agent") {
		t.Fatalf("expected ssh-agent conflict error, got %v", err)
	}
	if _, _, err := parseBaseFlagsWithConfig("test", []string{"--ssh-agent", "--no-ssh-agent"}, t.TempDir(), Config{}); err == nil {
		t.Fatal("expected parseBaseFlagsWithConfig to surface the conflict")
	}
}

func TestParseBaseFlagsWithConfigParseErrors(t *testing.T) {
	if _, _, err := parseBaseFlagsWithConfig("test", []string{"--help"}, t.TempDir(), Config{}); !errors.Is(err, errHelp) {
		t.Fatalf("expected errHelp, got %v", err)
	}
	if _, _, err := parseBaseFlagsWithConfig("test", []string{"--definitely-not-a-flag"}, t.TempDir(), Config{}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestRunCompletionHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		if err := runCompletion([]string{arg}, &bytes.Buffer{}); !errors.Is(err, errHelp) {
			t.Fatalf("completion %s: expected errHelp, got %v", arg, err)
		}
	}
}

func TestRunCmdArgsDispatchesCompletion(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := runCmdArgs([]string{"completion", "bash"}, t.TempDir(), nil)
	os.Stdout = orig
	_ = w.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatalf("runCmdArgs completion bash: %v", runErr)
	}
	if !strings.Contains(out.String(), "_yolobox") {
		t.Fatalf("expected bash completion script on stdout, got %q", out.String())
	}
}
