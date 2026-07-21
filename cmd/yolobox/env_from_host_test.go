package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHostEnvAliasArgs(t *testing.T) {
	t.Setenv("YOLOBOX_TEST_SRC", "secret-value")
	t.Setenv("YOLOBOX_TEST_EMPTY", "")

	tests := []struct {
		name    string
		entries []string
		want    []string
	}{
		{"alias to different name", []string{"GH_TOKEN=YOLOBOX_TEST_SRC"}, []string{"-e", "GH_TOKEN=secret-value"}},
		{"alias to same name", []string{"YOLOBOX_TEST_SRC=YOLOBOX_TEST_SRC"}, []string{"-e", "YOLOBOX_TEST_SRC=secret-value"}},
		{"multiple entries", []string{"A=YOLOBOX_TEST_SRC", "B=YOLOBOX_TEST_SRC"}, []string{"-e", "A=secret-value", "-e", "B=secret-value"}},
		{"unset host var is skipped", []string{"A=YOLOBOX_TEST_UNSET"}, nil},
		{"set-but-empty host var is forwarded", []string{"A=YOLOBOX_TEST_EMPTY"}, []string{"-e", "A="}},
		{"no entries", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostEnvAliasArgs(tt.entries)
			if !slices.Equal(got, tt.want) {
				t.Errorf("hostEnvAliasArgs(%q) = %q, want %q", tt.entries, got, tt.want)
			}
		})
	}
}

func TestValidateEnvFromHost(t *testing.T) {
	tests := []struct {
		name    string
		entry   string
		wantErr string
	}{
		{"valid", "GH_TOKEN=RO_GH_TOKEN", ""},
		{"missing equals", "GH_TOKEN", "expected KEY=HOST_VAR"},
		{"empty key", "=RO_GH_TOKEN", "expected KEY=HOST_VAR"},
		{"empty host var", "GH_TOKEN=", "expected KEY=HOST_VAR"},
		{"dollar prefixed host var", "GH_TOKEN=$RO_GH_TOKEN", "without a leading $"},
		{"braced host var", "GH_TOKEN=${RO_GH_TOKEN}", "without a leading $"},
		{"host var with invalid chars", "GH_TOKEN=RO-GH-TOKEN", "not a valid environment variable name"},
		{"key with invalid chars", "GH TOKEN=RO_GH_TOKEN", "not a valid environment variable name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuntimeOptions(Config{EnvFromHost: []string{tt.entry}})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateRuntimeOptions(%q) = %v, want nil", tt.entry, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateRuntimeOptions(%q) = nil, want error containing %q", tt.entry, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validateRuntimeOptions(%q) = %v, want error containing %q", tt.entry, err, tt.wantErr)
			}
		})
	}
}

func TestBuildRunArgsEnvFromHost(t *testing.T) {
	t.Setenv("YOLOBOX_TEST_SRC", "secret-value")

	cfg := Config{
		Image:       "test-image",
		Env:         []string{"HASH=$2b$12$example", "YOLOBOX_TEST_SRC"},
		EnvFromHost: []string{"GH_TOKEN=YOLOBOX_TEST_SRC", "MISSING=YOLOBOX_TEST_UNSET"},
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(args, "GH_TOKEN=secret-value") {
		t.Errorf("expected aliased GH_TOKEN=secret-value in args: %v", args)
	}
	// Literal --env values must survive untouched, including bare $ sequences.
	if !slices.Contains(args, "HASH=$2b$12$example") {
		t.Errorf("expected literal env entry to pass through unchanged: %v", args)
	}
	if !slices.Contains(args, "YOLOBOX_TEST_SRC") {
		t.Errorf("expected key-only entry passed through unchanged: %v", args)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "MISSING=") {
			t.Errorf("expected unset host var to be skipped, found %q in args: %v", arg, args)
		}
	}
}

func TestContextManifestIncludesEnvFromHostKeys(t *testing.T) {
	t.Setenv("YOLOBOX_TEST_SRC", "secret-value")

	cfg := Config{
		Image:       "test-image",
		Env:         []string{"DEBUG=1"},
		EnvFromHost: []string{"GH_TOKEN=YOLOBOX_TEST_SRC"},
	}

	manifest := buildContextManifest(cfg, "/test/project", []string{"bash"}, true, nil, false)
	if !slices.Contains(manifest.Config.EnvKeys, "GH_TOKEN") {
		t.Errorf("expected GH_TOKEN in manifest env keys: %v", manifest.Config.EnvKeys)
	}
	if !slices.Contains(manifest.Config.EnvKeys, "DEBUG") {
		t.Errorf("expected DEBUG in manifest env keys: %v", manifest.Config.EnvKeys)
	}
	// The manifest must not leak the aliased value.
	for _, key := range manifest.Config.EnvKeys {
		if strings.Contains(key, "secret-value") {
			t.Errorf("manifest env keys leaked a host value: %v", manifest.Config.EnvKeys)
		}
	}
}

func TestRunCmdArgsEnvFromHostEndToEnd(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLOBOX_TEST_RO_TOKEN", "read-only-token")
	argsFile := installFakeDockerRuntime(t)
	defer silenceStderr(t)()

	config := "env = [\"HASH=$2b$12$example\"]\nenv_from_host = [\"GH_TOKEN=YOLOBOX_TEST_RO_TOKEN\"]\n"
	if err := os.WriteFile(filepath.Join(projectDir, ".yolobox.toml"), []byte(config), 0644); err != nil {
		t.Fatalf("failed to write project config: %v", err)
	}

	if err := runCmdArgs([]string{"run", "--env-from-host", "EXTRA=YOLOBOX_TEST_RO_TOKEN", "bash"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs failed: %v", err)
	}

	args := readFakeRuntimeArgs(t, argsFile)
	for _, want := range []string{"GH_TOKEN=read-only-token", "EXTRA=read-only-token", "HASH=$2b$12$example"} {
		if !slices.Contains(args, want) {
			t.Errorf("expected runtime arg %q in %v", want, args)
		}
	}
}

func TestRunCmdArgsEnvFromHostRejectsDollarSyntax(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	installFakeDockerRuntime(t)
	defer silenceStderr(t)()

	err := runCmdArgs([]string{"run", "--env-from-host", "GH_TOKEN=$RO_TOKEN", "bash"}, projectDir, nil)
	if err == nil {
		t.Fatal("expected an error for $-prefixed host variable name")
	}
	if !strings.Contains(err.Error(), "without a leading $") {
		t.Errorf("expected a hint about the leading $, got: %v", err)
	}
}
