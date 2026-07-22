package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mustUnsetenv clears a variable for the duration of the test, the inverse of
// t.Setenv, so alias sources can be verified as genuinely absent.
func mustUnsetenv(t *testing.T, name string) {
	t.Helper()
	prev, had := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("failed to unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if had {
			if err := os.Setenv(name, prev); err != nil {
				t.Fatalf("failed to restore %s: %v", name, err)
			}
		}
	})
}

func TestHostEnvAliasArgs(t *testing.T) {
	t.Setenv("YOLOBOX_TEST_SRC", "secret-value")
	t.Setenv("YOLOBOX_TEST_EMPTY", "")

	tests := []struct {
		name    string
		entries []string
		want    []string
		wantErr string
	}{
		{"alias to different name", []string{"GH_TOKEN=YOLOBOX_TEST_SRC"}, []string{"-e", "GH_TOKEN=secret-value"}, ""},
		{"alias to same name", []string{"YOLOBOX_TEST_SRC=YOLOBOX_TEST_SRC"}, []string{"-e", "YOLOBOX_TEST_SRC=secret-value"}, ""},
		{"multiple entries", []string{"A=YOLOBOX_TEST_SRC", "B=YOLOBOX_TEST_SRC"}, []string{"-e", "A=secret-value", "-e", "B=secret-value"}, ""},
		{"unset host var is a hard error", []string{"A=YOLOBOX_TEST_UNSET"}, nil, "YOLOBOX_TEST_UNSET, which is not set"},
		{"set-but-empty host var is forwarded", []string{"A=YOLOBOX_TEST_EMPTY"}, []string{"-e", "A="}, ""},
		{"no entries", nil, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := hostEnvAliasArgs(tt.entries)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("hostEnvAliasArgs(%q) = %q, want error containing %q", tt.entries, got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("hostEnvAliasArgs(%q) error = %v, want error containing %q", tt.entries, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("hostEnvAliasArgs(%q) unexpected error: %v", tt.entries, err)
			}
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
		EnvFromHost: []string{"GH_TOKEN=YOLOBOX_TEST_SRC"},
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
}

func TestBuildRunArgsEnvFromHostMissingSourceFailsClosed(t *testing.T) {
	t.Setenv("GH_TOKEN", "write-token")
	mustUnsetenv(t, "YOLOBOX_TEST_UNSET")

	cfg := Config{
		Image:       "test-image",
		EnvFromHost: []string{"GH_TOKEN=YOLOBOX_TEST_UNSET"},
	}

	_, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, true)
	if err == nil {
		t.Fatal("expected an error when the env_from_host source variable is unset")
	}
	if !strings.Contains(err.Error(), "YOLOBOX_TEST_UNSET") {
		t.Errorf("expected the missing host variable to be named, got: %v", err)
	}
}

// An alias must own its container variable outright: the more privileged host
// value it replaces must not reach the container through automatic passthrough
// or --gh-token, and the guarantee must not depend on duplicate "-e" ordering.
func TestBuildRunArgsEnvFromHostSuppressesCompetingForwarding(t *testing.T) {
	t.Setenv("GH_TOKEN", "write-token")
	t.Setenv("YOLOBOX_TEST_RO_TOKEN", "read-only-token")
	defer silenceStderr(t)()

	cfg := Config{
		Image:       "test-image",
		GhToken:     true,
		EnvFromHost: []string{"GH_TOKEN=YOLOBOX_TEST_RO_TOKEN"},
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(args, "GH_TOKEN=read-only-token") {
		t.Fatalf("expected aliased GH_TOKEN=read-only-token in args: %v", args)
	}
	for _, arg := range args {
		if strings.Contains(arg, "write-token") {
			t.Errorf("privileged host GH_TOKEN leaked into args as %q: %v", arg, args)
		}
		if strings.HasPrefix(arg, "GH_TOKEN=") && arg != "GH_TOKEN=read-only-token" {
			t.Errorf("competing GH_TOKEN arg %q survived alongside the alias: %v", arg, args)
		}
	}
}

func TestValidateEnvFromHostConflictsWithEnv(t *testing.T) {
	err := validateRuntimeOptions(Config{
		Env:         []string{"GH_TOKEN=literal"},
		EnvFromHost: []string{"GH_TOKEN=YOLOBOX_TEST_SRC"},
	})
	if err == nil {
		t.Fatal("expected an error when a key is set by both env and env_from_host")
	}
	if !strings.Contains(err.Error(), "both env and env_from_host") {
		t.Errorf("expected a conflict message, got: %v", err)
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
	// The privileged host token the alias is meant to replace.
	t.Setenv("GH_TOKEN", "write-token")
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
	if slices.Contains(args, "GH_TOKEN=write-token") {
		t.Errorf("automatic passthrough leaked the privileged host GH_TOKEN: %v", args)
	}
}

func TestRunCmdArgsEnvFromHostMissingSourceFailsClosed(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GH_TOKEN", "write-token")
	mustUnsetenv(t, "YOLOBOX_TEST_RO_TOKEN")
	argsFile := installFakeDockerRuntime(t)
	defer silenceStderr(t)()

	config := "env_from_host = [\"GH_TOKEN=YOLOBOX_TEST_RO_TOKEN\"]\n"
	if err := os.WriteFile(filepath.Join(projectDir, ".yolobox.toml"), []byte(config), 0644); err != nil {
		t.Fatalf("failed to write project config: %v", err)
	}

	err := runCmdArgs([]string{"run", "bash"}, projectDir, nil)
	if err == nil {
		t.Fatal("expected an error when the env_from_host source variable is unset")
	}
	if !strings.Contains(err.Error(), "YOLOBOX_TEST_RO_TOKEN") {
		t.Errorf("expected the missing host variable to be named, got: %v", err)
	}
	if _, statErr := os.Stat(argsFile); statErr == nil {
		t.Error("expected the container never to be launched when an alias source is missing")
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
