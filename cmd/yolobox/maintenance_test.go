package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Model the runtimes' volume state and command grammar. In particular, Apple
// rejects -f and missing volume names; unrelated volumes must survive cleanup.
func installVolumeRuntime(t *testing.T, runtimeName, volumes string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "volumes")
	log := filepath.Join(dir, "calls")
	if err := os.WriteFile(state, []byte(volumes), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOLOBOX_TEST_VOLUMES", state)
	t.Setenv("YOLOBOX_TEST_VOLUME_LOG", log)
	t.Setenv("YOLOBOX_TEST_LIST_FAIL", "")
	t.Setenv("YOLOBOX_TEST_REMOVE_FAIL", "")
	script := `#!/bin/sh
set -eu
runtime=${0##*/}
printf '%s %s\n' "$runtime" "$*" >> "$YOLOBOX_TEST_VOLUME_LOG"
if [ "$*" = "volume ls --quiet" ]; then
    if [ -n "$YOLOBOX_TEST_LIST_FAIL" ]; then
        echo 'volume listing unavailable' >&2
        exit 7
    fi
    cat "$YOLOBOX_TEST_VOLUMES"
    exit 0
fi
[ "$1 $2" = "volume rm" ] || exit 64
shift 2
if [ "$runtime" != container ]; then
    [ "$1" = -f ] || exit 64
    shift
fi
[ "$#" -gt 0 ] || exit 64
if [ -n "$YOLOBOX_TEST_REMOVE_FAIL" ]; then
    echo 'volume is in use' >&2
    exit 9
fi
for volume in "$@"; do
    grep -Fxq -- "$volume" "$YOLOBOX_TEST_VOLUMES" || exit 3
done
for volume in "$@"; do
    awk -v name="$volume" '$0 != name' "$YOLOBOX_TEST_VOLUMES" > "$YOLOBOX_TEST_VOLUMES.tmp"
    mv "$YOLOBOX_TEST_VOLUMES.tmp" "$YOLOBOX_TEST_VOLUMES"
done
`
	// Shadow all runtimes, including fallback auto-selection, so no test can
	// accidentally reach a real Docker/Podman/Apple volume store.
	for _, name := range []string{"docker", "podman", "container"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(dir, runtimeName), state, log
}

func TestRemoveYoloboxVolumes(t *testing.T) {
	const initial = "yolobox-home\nyolobox-cache\nyolobox-home-amd64\npostgres-data\nyolobox-home-backup\n"
	for _, runtimeName := range []string{"docker", "podman", "container"} {
		for _, tc := range []struct {
			name, volumes, platform, failure, want, wantErr string
		}{
			{"all architectures", initial, "", "", "postgres-data\nyolobox-home-backup\n", ""},
			{"one architecture with missing volumes", initial, "linux/amd64", "", "yolobox-home\nyolobox-cache\npostgres-data\nyolobox-home-backup\n", ""},
			{"no matches", "postgres-data\n", "", "", "postgres-data\n", ""},
			{"empty store", "", "", "", "", ""},
			{"platform has no volumes", "yolobox-home\n", "linux/amd64", "", "yolobox-home\n", ""},
			{"listing failure", initial, "", "list", initial, "failed to list volumes"},
			{"deletion failure", initial, "", "remove", initial, "failed to remove yolobox volumes"},
			{"invalid platform", initial, "linux/", "", initial, "invalid platform"},
		} {
			t.Run(runtimeName+"/"+tc.name, func(t *testing.T) {
				overrideNativeArch(t, "arm64")
				path, state, log := installVolumeRuntime(t, runtimeName, tc.volumes)
				if tc.failure == "list" {
					t.Setenv("YOLOBOX_TEST_LIST_FAIL", "1")
				}
				if tc.failure == "remove" {
					t.Setenv("YOLOBOX_TEST_REMOVE_FAIL", "1")
				}
				err := removeYoloboxVolumes(path, tc.platform)
				if tc.wantErr == "" && err != nil {
					t.Fatal(err)
				}
				if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				remaining, err := os.ReadFile(state)
				if err != nil || string(remaining) != tc.want {
					t.Fatalf("remaining volumes = %q, error = %v; want %q", remaining, err, tc.want)
				}
				calls, _ := os.ReadFile(log)
				if tc.want == tc.volumes && tc.failure != "remove" && strings.Contains(string(calls), "volume rm") {
					t.Fatalf("unexpected deletion attempt: %s", calls)
				}
			})
		}
	}
}

// Run destructive CLI paths only in a disposable copy of the test executable.
// Even if uninstall regresses to falsely succeeding, the parent test survives.
func TestMaintenanceCommandProcess(t *testing.T) {
	if os.Getenv("YOLOBOX_TEST_MAINTENANCE_CHILD") != "1" {
		return
	}
	args := strings.Fields(os.Getenv("YOLOBOX_TEST_MAINTENANCE_ARGS"))
	projectDir, err := os.Getwd()
	if err == nil {
		err = runCmdArgs(args, projectDir, nil)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestMaintenanceCommands(t *testing.T) {
	for _, tc := range []struct {
		name, args, failure, wantErr string
		uninstalled, keepVolumes     bool
	}{
		{"reset confirmation", "reset", "", "requires --force", false, true},
		{"reset succeeds", "reset --force", "", "", false, false},
		{"reset fails", "reset --force", "remove", "failed to remove", false, true},
		{"uninstall confirmation", "uninstall", "", "run with --force", false, true},
		{"uninstall succeeds", "uninstall --force", "", "", true, false},
		{"uninstall listing fails", "uninstall --force", "list", "failed to list", false, true},
		{"uninstall deletion fails", "uninstall --force", "remove", "failed to remove", false, true},
		{"uninstall config invalid", "uninstall --force", "config", "toml:", false, true},
		{"keep volumes without runtime", "uninstall --force --keep-volumes", "runtime", "", true, true},
		{"uninstall runtime missing", "uninstall --force", "runtime", "not found", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtimePath, state, log := installVolumeRuntime(t, "container", "yolobox-home\npostgres-data\n")
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
			config, err := globalConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
				t.Fatal(err)
			}
			if tc.failure == "runtime" {
				runtimePath = filepath.Join(dir, "missing-runtime")
			}
			contents := fmt.Sprintf("runtime = %q\n", runtimePath)
			if tc.failure == "config" {
				contents = "runtime = ["
			}
			if err := os.WriteFile(config, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			cache := versionCachePath()
			if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cache, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.failure == "list" {
				t.Setenv("YOLOBOX_TEST_LIST_FAIL", "1")
			}
			if tc.failure == "remove" {
				t.Setenv("YOLOBOX_TEST_REMOVE_FAIL", "1")
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(executable)
			if err != nil {
				t.Fatal(err)
			}
			copyPath := filepath.Join(dir, "yolobox-test")
			if err := os.WriteFile(copyPath, data, 0755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(copyPath, "-test.run=^TestMaintenanceCommandProcess$")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "YOLOBOX_TEST_MAINTENANCE_CHILD=1", "YOLOBOX_TEST_MAINTENANCE_ARGS="+tc.args)
			output, err := cmd.CombinedOutput()
			if (err != nil) != (tc.wantErr != "") || !strings.Contains(string(output), tc.wantErr) {
				t.Fatalf("command error = %v, output = %s; want %q", err, output, tc.wantErr)
			}
			for _, path := range []string{copyPath, config, cache} {
				_, err := os.Stat(path)
				if os.IsNotExist(err) != tc.uninstalled || (err != nil && !os.IsNotExist(err)) {
					t.Errorf("path %s: error = %v, want removed = %v", path, err, tc.uninstalled)
				}
			}
			remaining, err := os.ReadFile(state)
			want := "postgres-data\n"
			if tc.keepVolumes {
				want = "yolobox-home\n" + want
			}
			if err != nil || string(remaining) != want {
				t.Fatalf("remaining volumes = %q, error = %v; want %q", remaining, err, want)
			}
			calls, _ := os.ReadFile(log)
			if strings.Contains(string(calls), "docker ") || strings.Contains(string(calls), "podman ") {
				t.Fatalf("cleanup lost the configured Apple runtime: %s", calls)
			}
			if tc.uninstalled && tc.keepVolumes && len(calls) > 0 {
				t.Fatalf("--keep-volumes contacted runtime: %s", calls)
			}
		})
	}
}
