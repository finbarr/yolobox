package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func at(day int) time.Time {
	return time.Date(2026, 7, day, 12, 0, 0, 0, time.UTC)
}

// noDirs reports every project directory as missing; existingDirs reports the
// listed ones as present.
func noDirs(string) bool { return false }

func existingDirs(dirs ...string) func(string) bool {
	present := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		present[dir] = true
	}
	return func(dir string) bool { return present[dir] }
}

func ids(images []customImage) string {
	parts := make([]string, 0, len(images))
	for _, img := range images {
		parts = append(parts, img.ID)
	}
	return strings.Join(parts, ",")
}

func assertIDs(t *testing.T, label string, got []customImage, want string) {
	t.Helper()
	if ids(got) != want {
		t.Errorf("%s: got [%s], want [%s]", label, ids(got), want)
	}
}

func TestClassifyKeepsNewestImagePerProject(t *testing.T) {
	images := []customImage{
		{ID: "old", Project: "/proj", Created: at(1)},
		{ID: "new", Project: "/proj", Created: at(3)},
		{ID: "middle", Project: "/proj", Created: at(2)},
	}

	keep, remove, unlabeled := classifyCustomImages(images, nil, existingDirs("/proj"))

	assertIDs(t, "keep", keep, "new")
	assertIDs(t, "remove", remove, "middle,old")
	assertIDs(t, "unlabeled", unlabeled, "")
}

func TestClassifyKeepsNewestPerProjectIndependently(t *testing.T) {
	images := []customImage{
		{ID: "a-old", Project: "/a", Created: at(1)},
		{ID: "b-new", Project: "/b", Created: at(4)},
		{ID: "a-new", Project: "/a", Created: at(2)},
		{ID: "b-old", Project: "/b", Created: at(3)},
	}

	keep, remove, _ := classifyCustomImages(images, nil, existingDirs("/a", "/b"))

	assertIDs(t, "keep", keep, "a-new,b-new")
	assertIDs(t, "remove", remove, "a-old,b-old")
}

func TestClassifyRemovesAllImagesOfMissingProject(t *testing.T) {
	images := []customImage{
		{ID: "old", Project: "/gone", Created: at(1)},
		{ID: "new", Project: "/gone", Created: at(3)},
	}

	keep, remove, _ := classifyCustomImages(images, nil, noDirs)

	assertIDs(t, "keep", keep, "")
	assertIDs(t, "remove", remove, "new,old")
}

func TestClassifyNeverRemovesUnlabeledImages(t *testing.T) {
	images := []customImage{
		{ID: "legacy-old", Created: at(1)},
		{ID: "legacy-new", Created: at(2)},
	}

	keep, remove, unlabeled := classifyCustomImages(images, nil, noDirs)

	assertIDs(t, "keep", keep, "")
	assertIDs(t, "remove", remove, "")
	assertIDs(t, "unlabeled", unlabeled, "legacy-new,legacy-old")
}

func TestClassifyKeepsImageUsedByContainerDespiteMissingProject(t *testing.T) {
	images := []customImage{
		{ID: "running", Project: "/gone", Created: at(1)},
	}
	inUse := map[string]bool{"running": true}

	keep, remove, _ := classifyCustomImages(images, inUse, noDirs)

	assertIDs(t, "keep", keep, "running")
	assertIDs(t, "remove", remove, "")
}

func TestClassifyKeepsSupersededImageUsedByContainer(t *testing.T) {
	images := []customImage{
		{ID: "superseded", Tags: []string{"yolobox-custom:aaa"}, Project: "/proj", Created: at(1)},
		{ID: "current", Tags: []string{"yolobox-custom:bbb"}, Project: "/proj", Created: at(2)},
	}
	// Containers report the tag they were started from, not the image ID.
	inUse := map[string]bool{"yolobox-custom:aaa": true}

	keep, remove, _ := classifyCustomImages(images, inUse, existingDirs("/proj"))

	assertIDs(t, "keep", keep, "current,superseded")
	assertIDs(t, "remove", remove, "")
}

func TestClassifyKeepsUnlabeledImageUsedByContainerOutOfManualList(t *testing.T) {
	images := []customImage{
		{ID: "legacy-running", Created: at(1)},
		{ID: "legacy-idle", Created: at(2)},
	}
	inUse := map[string]bool{"legacy-running": true}

	keep, remove, unlabeled := classifyCustomImages(images, inUse, noDirs)

	assertIDs(t, "keep", keep, "legacy-running")
	assertIDs(t, "remove", remove, "")
	assertIDs(t, "unlabeled", unlabeled, "legacy-idle")
}

func TestClassifyMatchesContainerShortImageID(t *testing.T) {
	full := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	images := []customImage{
		{ID: full, Project: "/gone", Created: at(1)},
	}
	// `docker ps` reports a 12-character ID with no sha256: prefix.
	inUse := map[string]bool{"abcdef123456": true}

	keep, remove, _ := classifyCustomImages(images, inUse, noDirs)

	assertIDs(t, "keep", keep, full)
	assertIDs(t, "remove", remove, "")
}

// installPruneRuntime installs a fake runtime that reports two derived images
// for liveProject: `old` (superseded) and `new` (current), with no containers
// running. Every invocation is appended to the returned log file.
func installPruneRuntime(t *testing.T, liveProject string) string {
	t.Helper()
	runtimeDir := t.TempDir()
	logFile := filepath.Join(t.TempDir(), "docker-log")
	script := `#!/bin/sh
echo "$*" >> "$YOLOBOX_FAKE_RUNTIME_LOG"
if [ "$1" = "info" ]; then
	echo 8589934592
	exit 0
fi
if [ "$1" = "image" ] && [ "$2" = "ls" ]; then
	printf 'old\nnew\n'
	exit 0
fi
if [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
	printf 'old\t2026-07-01T12:00:00Z\t100\t%s\tyolobox-custom:old\n' "$YOLOBOX_FAKE_PROJECT"
	printf 'new\t2026-07-02T12:00:00Z\t200\t%s\tyolobox-custom:new\n' "$YOLOBOX_FAKE_PROJECT"
	exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(runtimeDir, "docker"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("YOLOBOX_FAKE_RUNTIME_LOG", logFile)
	t.Setenv("YOLOBOX_FAKE_PROJECT", liveProject)
	return logFile
}

func TestPruneImagesWithoutForceDeletesNothing(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	logFile := installPruneRuntime(t, projectDir)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"prune-images"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	log := readRuntimeLog(t, logFile)
	for _, line := range log {
		if strings.HasPrefix(line, "image rm") {
			t.Fatalf("dry run must not delete, got: %q", line)
		}
	}
}

func TestPruneImagesWithForceDeletesOnlySupersededImage(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	logFile := installPruneRuntime(t, projectDir)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"prune-images", "--force"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	log := readRuntimeLog(t, logFile)
	if !logHasLine(log, "image rm yolobox-custom:old") {
		t.Fatalf("expected the superseded image to be removed, got:\n%s", strings.Join(log, "\n"))
	}
	for _, line := range log {
		if strings.Contains(line, "rm") && strings.Contains(line, "yolobox-custom:new") {
			t.Fatalf("must not remove the current image, got: %q", line)
		}
	}
}

func TestReportMarksImageKeptBecauseAContainerUsesIt(t *testing.T) {
	// The oldest image for a live project would normally be deleted; it survives
	// only because a container references it, and the report has to say so or the
	// "keep" reads as arbitrary.
	keep := []customImage{
		{ID: "current", Tags: []string{"yolobox-custom:new"}, Project: "/proj", Created: at(2)},
		{ID: "pinned", Tags: []string{"yolobox-custom:old"}, Project: "/proj", Created: at(1)},
	}
	inUse := map[string]bool{"yolobox-custom:old": true}

	output := captureStderr(t, func() {
		reportCustomImages(keep, nil, nil, inUse)
	})

	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "yolobox-custom:old") {
			continue
		}
		if !strings.Contains(line, "in use") {
			t.Fatalf("expected the container-pinned image to be marked in use, got: %q", line)
		}
		return
	}
	t.Fatalf("expected the pinned image in the report, got:\n%s", output)
}

func TestClassifyHandlesNoImages(t *testing.T) {
	keep, remove, unlabeled := classifyCustomImages(nil, nil, noDirs)

	if len(keep) != 0 || len(remove) != 0 || len(unlabeled) != 0 {
		t.Fatalf("expected all sets empty, got keep=%d remove=%d unlabeled=%d",
			len(keep), len(remove), len(unlabeled))
	}
}
