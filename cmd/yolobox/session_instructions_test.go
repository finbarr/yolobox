package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSessionInstructionsRuntimeHandoff(t *testing.T) {
	project := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	contents := "# Review notes\n\nKeep quotes and spaces: ‘café’ and $(literal).\n"
	if err := os.WriteFile(filepath.Join(project, "review notes.md"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"claude", "codex"} {
		t.Run(tool, func(t *testing.T) {
			base, toolArgs := splitToolArgs([]string{"--instructions", "review notes.md", "--", "review this"})
			cfg, _, err := parseBaseFlagsWithConfig(tool, base, project, Config{Image: "test-image", NoEnvPassthrough: true})
			if err != nil {
				t.Fatal(err)
			}
			args, cleanupPaths, err := buildRunArgs(cfg, project, append([]string{tool}, toolArgs...), false)
			if err != nil {
				t.Fatal(err)
			}
			if len(cleanupPaths) != 0 {
				t.Fatalf("unexpected host staging files: %v", cleanupPaths)
			}
			if want := []string{"test-image", "yolobox-session-instructions", tool, "review this"}; !slices.Equal(args[len(args)-len(want):], want) {
				t.Fatalf("runtime command did not preserve tool arguments: %v", args[len(args)-len(want):])
			}
			payload, found := argEnvValue(args, "YOLOBOX_SESSION_INSTRUCTIONS_B64")
			decoded, err := base64.StdEncoding.DecodeString(payload)
			if !found || err != nil || string(decoded) != contents {
				t.Fatalf("invalid instructions payload: found=%v, error=%v", found, err)
			}
		})
	}
}

func TestSessionInstructionsRejectsIncompatibleUse(t *testing.T) {
	for _, test := range []struct {
		tool string
		args []string
		want string
	}{
		{"claude", []string{"--resume=123"}, "fresh Claude"},
		{"claude", []string{"--append-system-prompt-file", "other.md"}, "either --instructions"},
		{"codex", []string{"exec", "resume", "--last"}, "fresh Codex"},
		{"codex", []string{"--disable", "hooks"}, "requires Codex session hooks"},
		{"codex", []string{"-c", "features.hooks = false"}, "requires Codex session hooks"},
		{"codex", []string{"--remote=wss://example.com"}, "inside the box"},
	} {
		err := validateSessionInstructionsArgs(test.tool, test.args)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s %v: expected %q error, got %v", test.tool, test.args, test.want, err)
		}
	}
	if err := validateSessionInstructionsArgs("codex", []string{"--model", "resume", "fork=part of a prompt"}); err != nil {
		t.Errorf("ordinary option value or prompt rejected: %v", err)
	}
	if _, _, err := parseBaseFlagsWithConfig("gemini", []string{"--instructions", "review.md"}, t.TempDir(), Config{}); err == nil {
		t.Error("unsupported shortcut accepted --instructions")
	}
}
