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

// completionFixtures creates a bin dir with a uniquely named executable and a
// data dir containing a filename with spaces.
func completionFixtures(t *testing.T) (binDir, dataDir string) {
	t.Helper()
	root := t.TempDir()
	binDir = filepath.Join(root, "bin")
	dataDir = filepath.Join(root, "data")
	for _, dir := range []string{binDir, dataDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(binDir, "pr71-command"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "Docker fragment"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return binDir, dataDir
}

func writeCompletionScript(t *testing.T, shell string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runCompletion([]string{shell}, &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "yolobox."+shell)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const bashCompletionDriver = `
source "$1"; shift
COMP_WORDS=("$@")
COMP_CWORD=$(( ${#COMP_WORDS[@]} - 1 ))
COMP_LINE="${COMP_WORDS[*]}"
COMP_POINT=${#COMP_LINE}
_yolobox
(( ${#COMPREPLY[@]} )) && printf '%s\0' "${COMPREPLY[@]}"
exit 0
`

// bashComplete invokes _yolobox with COMP_WORDS as bash's readline would split
// them (COMP_WORDBREAKS splits "--flag=value" into "--flag" "=" "value").
func bashComplete(t *testing.T, bash, script, binDir string, words ...string) []string {
	t.Helper()
	args := append([]string{"--norc", "--noprofile", "-c", bashCompletionDriver, "driver", script}, words...)
	cmd := exec.Command(bash, args...)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash completion %q: %v", words, err)
	}
	var got []string
	for _, s := range strings.Split(string(out), "\x00") {
		if s != "" {
			got = append(got, s)
		}
	}
	return got
}

func TestBashCompletionBehavior(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	script := writeCompletionScript(t, "bash")
	binDir, dataDir := completionFixtures(t)
	spaced := filepath.Join(dataDir, "Docker fragment")
	partial := filepath.Join(dataDir, "Dock")

	exact := []struct {
		name  string
		words []string
		want  []string
	}{
		{"runtime space", []string{"yolobox", "run", "--runtime", "d"}, []string{"docker"}},
		{"runtime equals", []string{"yolobox", "run", "--runtime", "=", "d"}, []string{"docker"}},
		{"runtime equals empty", []string{"yolobox", "run", "--runtime", "="}, []string{"docker", "podman", "container"}},
		{"runtime equals unsplit", []string{"yolobox", "run", "--runtime=d"}, []string{"--runtime=docker"}},
		{"runtime top-level equals", []string{"yolobox", "--runtime", "=", "p"}, []string{"podman"}},
		{"runtime tool shortcut equals", []string{"yolobox", "claude", "--runtime", "=", "c"}, []string{"container"}},
		{"platform space", []string{"yolobox", "run", "--platform", "linux/ar"}, []string{"linux/arm64"}},
		{"platform equals", []string{"yolobox", "shell", "--platform", "=", "linux/ar"}, []string{"linux/arm64"}},
		{"reset platform space", []string{"yolobox", "reset", "--platform", "linux/am"}, []string{"linux/amd64"}},
		{"reset platform equals", []string{"yolobox", "reset", "--platform", "=", "linux/am"}, []string{"linux/amd64"}},
		{"customize-file spaces", []string{"yolobox", "run", "--customize-file", partial}, []string{spaced}},
		{"customize-file equals spaces", []string{"yolobox", "run", "--customize-file", "=", partial}, []string{spaced}},
		{"mount spaces", []string{"yolobox", "run", "--mount", partial}, []string{spaced}},
		{"copy-as spaces", []string{"yolobox", "run", "--copy-as", partial}, []string{spaced}},
		{"run command", []string{"yolobox", "run", "pr71"}, []string{"pr71-command"}},
		{"run command after equals flag", []string{"yolobox", "run", "--runtime", "=", "docker", "pr71"}, []string{"pr71-command"}},
		{"fork command", []string{"yolobox", "fork", "--name", "unit", "pr71"}, []string{"pr71-command"}},
		{"fork command after equals name", []string{"yolobox", "fork", "--name", "=", "unit", "pr71"}, []string{"pr71-command"}},
	}
	for _, tc := range exact {
		t.Run(tc.name, func(t *testing.T) {
			got := bashComplete(t, bash, script, binDir, tc.words...)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("completion %q = %q, want %q", tc.words, got, tc.want)
			}
		})
	}
}

const zshCompletionDriver = `
zmodload zsh/zpty || exit 3
script=$1 out=$2 bindir=$3
shift 3
zpty z zsh -f -i
zpty -w z "unsetopt beep; PS1='> '; PATH=${(q)bindir}:\$PATH; autoload -Uz compinit; compinit -u -D; source ${(q)script}"
zpty -w z "yb_dump() { print -r -- \"<\$BUFFER>\" >> ${(q)out}; BUFFER=''; }; zle -N yb_dump; bindkey '^X' yb_dump"
for line in "$@"; do
    zpty -w -n z "$line"$'\t\x18'
    while zpty -r -t z chunk; do :; done
done
integer i
for (( i = 0; i < 200; i++ )); do
    while zpty -r -t z chunk; do :; done
    [[ -f $out ]] && (( ${#${(f)"$(<$out)"}} >= $# )) && break
    sleep 0.05
done
zpty -d z
`

// zshComplete types each line into an interactive zsh, presses Tab, and
// returns the resulting command line buffers.
func zshComplete(t *testing.T, zsh, script, binDir string, lines ...string) []string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "buffers")
	args := append([]string{"-f", "-c", zshCompletionDriver, "driver", script, out, binDir}, lines...)
	cmd := exec.Command(zsh, args...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 3 {
			t.Skip("zsh/zpty module unavailable")
		}
		t.Fatalf("zsh driver: %v\n%s", err, msg)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("zsh completion produced no output: %v", err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestZshCompletionBehavior(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	script := writeCompletionScript(t, "zsh")
	binDir, dataDir := completionFixtures(t)

	cases := []struct{ line, want string }{
		{"yolobox run pr71", "yolobox run pr71-command "},
		{"yolobox fork --name unit pr71", "yolobox fork --name unit pr71-command "},
		{"yolobox run --runtime d", "yolobox run --runtime docker "},
		{"yolobox run --runtime=d", "yolobox run --runtime=docker "},
		{"yolobox run --platform linux/ar", "yolobox run --platform linux/arm64 "},
		{"yolobox run --platform=linux/ar", "yolobox run --platform=linux/arm64 "},
		{"yolobox run --customize-file " + dataDir + "/Dock", "yolobox run --customize-file " + dataDir + `/Docker\ fragment `},
	}
	var lines []string
	for _, tc := range cases {
		lines = append(lines, tc.line)
	}
	got := zshComplete(t, zsh, script, binDir, lines...)
	if len(got) != len(cases) {
		t.Fatalf("expected %d buffers, got %d: %q", len(cases), len(got), got)
	}
	for i, tc := range cases {
		if want := "<" + tc.want + ">"; got[i] != want {
			t.Errorf("zsh completion of %q = %q, want %q", tc.line, got[i], want)
		}
	}
}
