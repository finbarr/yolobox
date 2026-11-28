package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const archiveTestBody = "persistent data\n"

func volumeArchiveFixture(t *testing.T, entry *tar.Header) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := tar.NewWriter(&data)
	if err := writer.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0750, Uid: 1000, Gid: 1000}); err != nil {
		t.Fatal(err)
	}
	if entry != nil {
		if err := writer.WriteHeader(entry); err != nil {
			t.Fatal(err)
		}
		if entry.Size > 0 {
			if _, err := writer.Write([]byte(archiveTestBody)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func writeVolumeArchiveFixture(t *testing.T, filename, comment string, data []byte, corrupt bool) {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	compressed.Comment = comment
	if _, err := compressed.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	blob := buffer.Bytes()
	if corrupt {
		blob[len(blob)-8] ^= 1 // CRC32 trailer
	}
	if err := os.WriteFile(filename, blob, 0600); err != nil {
		t.Fatal(err)
	}
}

func installArchiveRuntime(t *testing.T, source []byte) (volumeArchiveRuntime, string, string) {
	t.Helper()
	dir := t.TempDir()
	fixture := filepath.Join(dir, "source.tar")
	if err := os.WriteFile(fixture, source, 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls")
	restored := filepath.Join(dir, "restored.tar")
	for key, value := range map[string]string{
		"YOLOBOX_TEST_ARCHIVE_SOURCE":   fixture,
		"YOLOBOX_TEST_ARCHIVE_RESTORED": restored,
		"YOLOBOX_TEST_ARCHIVE_LOG":      log,
		"YOLOBOX_TEST_ARCHIVE_VOLUMES":  "yolobox-home\nyolobox-restored-keep\n",
		"YOLOBOX_TEST_ARCHIVE_ACTIVE":   "",
		"YOLOBOX_TEST_ARCHIVE_FAILURE":  "",
		"YOLOBOX_TEST_ARCHIVE_RACE":     "",
	} {
		t.Setenv(key, value)
	}
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$YOLOBOX_TEST_ARCHIVE_LOG"
case "$1 $2" in
    'volume ls') printf '%s' "$YOLOBOX_TEST_ARCHIVE_VOLUMES" ;;
    'ps --quiet') printf '%s' "$YOLOBOX_TEST_ARCHIVE_ACTIVE" ;;
    'volume create')
        case "$3" in yolobox-restored-*) ;; *) exit 7 ;; esac
        [ "$YOLOBOX_TEST_ARCHIVE_FAILURE" != create ] || exit 9
        printf '%s\n' "$3"
        ;;
    'run --rm')
        case " $* " in
            *' --create '*)
                cat "$YOLOBOX_TEST_ARCHIVE_SOURCE"
                if [ -n "$YOLOBOX_TEST_ARCHIVE_RACE" ]; then
                    printf 'other writer' > "$YOLOBOX_TEST_ARCHIVE_RACE"
                fi
                [ "$YOLOBOX_TEST_ARCHIVE_FAILURE" != archive ] || exit 9
                ;;
            *' --extract '*)
                cat > "$YOLOBOX_TEST_ARCHIVE_RESTORED"
                case "$YOLOBOX_TEST_ARCHIVE_FAILURE" in extract|cleanup) exit 9 ;; esac
                ;;
            *) exit 7 ;;
        esac
        ;;
    'volume rm') [ "$YOLOBOX_TEST_ARCHIVE_FAILURE" != cleanup ] || exit 9 ;;
    *) exit 7 ;;
esac
`
	runtimePath := filepath.Join(dir, "docker")
	if err := os.WriteFile(runtimePath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return volumeArchiveRuntime{path: runtimePath, image: "test-image", arch: "amd64", platform: "linux/amd64"}, log, restored
}

func TestVolumeBackupRoundTrip(t *testing.T) {
	header := &tar.Header{Name: "./agent-settings", Mode: 0600, Uid: 1001, Gid: 1002, Size: int64(len(archiveTestBody))}
	fixture := volumeArchiveFixture(t, header)
	helper, log, restored := installArchiveRuntime(t, fixture)
	archive := filepath.Join(t.TempDir(), "home.tar.gz")
	if err := helper.backupHomeVolume("yolobox-home", archive); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("archive mode = %o", info.Mode().Perm())
	}
	restoredName, err := helper.restoreHomeVolume(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(restoredName, "yolobox-restored-") || restoredName == "yolobox-restored-keep" {
		t.Fatalf("unexpected restore volume: %q", restoredName)
	}
	restoredTar, err := os.ReadFile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredTar, fixture) {
		t.Fatal("restore helper did not receive exact source tar stream")
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"ps --quiet --filter volume=yolobox-home",
		"-v yolobox-home:/yolobox-volume:nocopy,ro",
		"volume create " + restoredName,
		"-v " + restoredName + ":/yolobox-volume:nocopy",
		"--network none --read-only",
		"--same-owner --same-permissions",
	} {
		if !strings.Contains(string(calls), required) {
			t.Errorf("missing runtime args %q in %s", required, calls)
		}
	}
	if strings.Contains(string(calls), "volume rm ") {
		t.Fatalf("successful restore deleted a volume: %s", calls)
	}
}

func TestVolumeBackupFailures(t *testing.T) {
	for _, tc := range []struct {
		name, active, failure, volume, wantErr, wantFile string
		entry                                            *tar.Header
	}{
		{name: "missing source", volume: "yolobox-home-other", wantErr: "does not exist"},
		{name: "active source", active: "a-running-container", wantErr: "is in use"},
		{name: "archiver fails after writing", failure: "archive", wantErr: "archive home volume"},
		{name: "unsupported entry", entry: &tar.Header{Name: "./fifo", Typeflag: tar.TypeFifo}, wantErr: "unsupported archive entry type"},
		{name: "concurrent publisher", wantErr: "publish backup archive", wantFile: "other writer"},
		{name: "existing destination", wantErr: "already exists", wantFile: "original"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			helper, _, _ := installArchiveRuntime(t, volumeArchiveFixture(t, tc.entry))
			t.Setenv("YOLOBOX_TEST_ARCHIVE_ACTIVE", tc.active)
			t.Setenv("YOLOBOX_TEST_ARCHIVE_FAILURE", tc.failure)
			dir := t.TempDir()
			archive := filepath.Join(dir, "home.tar.gz")
			switch tc.wantFile {
			case "other writer":
				t.Setenv("YOLOBOX_TEST_ARCHIVE_RACE", archive)
			case "original":
				if err := os.WriteFile(archive, []byte(tc.wantFile), 0600); err != nil {
					t.Fatal(err)
				}
			}
			volume := tc.volume
			if volume == "" {
				volume = "yolobox-home"
			}
			if err := helper.backupHomeVolume(volume, archive); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("backup error = %v, want %q", err, tc.wantErr)
			}
			pending, err := filepath.Glob(filepath.Join(dir, ".yolobox-volume-backup-*"))
			if err != nil || len(pending) != 0 {
				t.Fatalf("temporary archive remnants %v, error = %v", pending, err)
			}
			data, err := os.ReadFile(archive)
			if tc.wantFile == "" {
				if !os.IsNotExist(err) {
					t.Fatalf("failed backup published an archive: %v", err)
				}
			} else if err != nil || string(data) != tc.wantFile {
				t.Fatalf("original archive = %q, error = %v", data, err)
			}
		})
	}
}

func TestVolumeRestoreRejectsInvalidArchivesBeforeCreatingVolume(t *testing.T) {
	for _, tc := range []struct {
		name, arch, wantErr string
		entry               *tar.Header
		corrupt             bool
	}{
		{name: "different architecture", arch: "arm64", wantErr: "differs from restore architecture"},
		{name: "unsupported format", arch: "unknown", wantErr: "not a supported yolobox home archive"},
		{name: "bad checksum", corrupt: true, wantErr: "invalid checksum"},
		{name: "absolute path", entry: &tar.Header{Name: "/etc/passwd"}, wantErr: "unsafe path"},
		{name: "parent traversal", entry: &tar.Header{Name: "a/../secret"}, wantErr: "unsafe path"},
		{name: "hardlink traversal", entry: &tar.Header{Name: "x", Typeflag: tar.TypeLink, Linkname: "../password"}, wantErr: "unsafe hard-link target"},
		{name: "special file", entry: &tar.Header{Name: "x", Typeflag: tar.TypeChar}, wantErr: "unsupported archive entry type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			helper, log, _ := installArchiveRuntime(t, nil)
			archive := filepath.Join(t.TempDir(), "home.tar.gz")
			arch := tc.arch
			if arch == "" {
				arch = "amd64"
			}
			writeVolumeArchiveFixture(t, archive, volumeArchiveHeader+arch, volumeArchiveFixture(t, tc.entry), tc.corrupt)
			if _, err := helper.restoreHomeVolume(archive); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("restore error = %v, want %q", err, tc.wantErr)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("invalid archive reached the runtime: %v", err)
			}
		})
	}
}

func TestVolumeRestoreFailureCleansOnlyNewVolume(t *testing.T) {
	for _, failure := range []string{"create", "extract", "cleanup"} {
		t.Run(failure, func(t *testing.T) {
			helper, log, _ := installArchiveRuntime(t, nil)
			t.Setenv("YOLOBOX_TEST_ARCHIVE_FAILURE", failure)
			archive := filepath.Join(t.TempDir(), "home.tar.gz")
			writeVolumeArchiveFixture(t, archive, volumeArchiveHeader+"amd64", volumeArchiveFixture(t, nil), false)
			if _, err := helper.restoreHomeVolume(archive); err == nil || (failure == "cleanup" && !strings.Contains(err.Error(), "remove incomplete restore volume")) {
				t.Fatalf("unexpected restore error = %v", err)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var created, removed string
			for _, call := range strings.Split(string(calls), "\n") {
				if name, ok := strings.CutPrefix(call, "volume create "); ok {
					created = name
				}
				if name, ok := strings.CutPrefix(call, "volume rm "); ok {
					removed = name
				}
			}
			if !strings.HasPrefix(created, "yolobox-restored-") || created == "yolobox-restored-keep" {
				t.Fatalf("failure targeted an existing volume: %s", calls)
			}
			if failure == "create" && removed != "" || failure != "create" && removed != created {
				t.Fatalf("wrong volume cleanup: %s", calls)
			}
		})
	}
}

func TestVolumeArchiveRootlessPodmanMount(t *testing.T) {
	helper := volumeArchiveRuntime{path: "/example/podman", image: "test-image", rootlessPodman: true}
	for _, restore := range []bool{false, true} {
		args := helper.tarCommand("volume-name", restore).Args
		mount := "volume-name:/yolobox-volume:nocopy"
		if !restore {
			mount += ",ro"
		}
		if !slices.Contains(args, mount) || !slices.Contains(args, "--userns=keep-id:uid=1000,gid=1000") ||
			!slices.Contains(args, "label=disable") || slices.Contains(args, "-t") {
			t.Errorf("restore %t: incorrect rootless Podman helper args: %v", restore, args)
		}
	}
}

func TestValidateVolumeArchiveRequiresTarContent(t *testing.T) {
	if err := validateVolumeArchive(strings.NewReader("")); err == nil {
		t.Fatal("empty archive accepted")
	}
	if err := validateVolumeArchive(bytes.NewReader(volumeArchiveFixture(t, nil))); err != nil {
		t.Fatalf("valid empty home rejected: %v", err)
	}
}
