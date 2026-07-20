package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func overrideNativeArch(t *testing.T, arch string) {
	t.Helper()
	prev := nativeArch
	nativeArch = arch
	t.Cleanup(func() {
		nativeArch = prev
	})
}

func TestArchFromPlatform(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"linux/amd64", "amd64"},
		{"linux/arm64", "arm64"},
		{"amd64", "amd64"},
		{"arm64", "arm64"},
		{"x86_64", "amd64"},
		{"aarch64", "arm64"},
		{"linux/x86_64", "amd64"},
		{"linux/aarch64", "arm64"},
		{"linux/arm/v7", "arm"},
		{"LINUX/AMD64", "amd64"},
		{" linux/amd64 ", "amd64"},
	}
	for _, tt := range tests {
		got, err := archFromPlatform(tt.value)
		if err != nil {
			t.Errorf("archFromPlatform(%q): unexpected error: %v", tt.value, err)
			continue
		}
		if got != tt.want {
			t.Errorf("archFromPlatform(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestArchFromPlatformInvalid(t *testing.T) {
	for _, value := range []string{"", "   ", "linux/", "/", "//"} {
		if _, err := archFromPlatform(value); err == nil {
			t.Errorf("archFromPlatform(%q): expected error, got none", value)
		}
	}
}

func TestPlatformFromRuntimeArgs(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"--memory-swap", "2g"}, ""},
		{[]string{"--platform", "linux/amd64"}, "linux/amd64"},
		{[]string{"--platform=linux/amd64"}, "linux/amd64"},
		{[]string{"--security-opt", "seccomp=unconfined", "--platform", "linux/arm64"}, "linux/arm64"},
		{[]string{"--platform"}, ""},
	}
	for _, tt := range tests {
		if got := platformFromRuntimeArgs(tt.args); got != tt.want {
			t.Errorf("platformFromRuntimeArgs(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestResolveContainerArchPrecedence(t *testing.T) {
	overrideNativeArch(t, "arm64")

	// Nothing configured: native arch.
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "")
	arch, err := resolveContainerArch(Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arch != "arm64" {
		t.Errorf("expected native arm64, got %q", arch)
	}

	// Env var beats native.
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "linux/amd64")
	arch, err = resolveContainerArch(Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arch != "amd64" {
		t.Errorf("expected amd64 from DOCKER_DEFAULT_PLATFORM, got %q", arch)
	}

	// runtime_args beat env.
	arch, err = resolveContainerArch(Config{RuntimeArgs: []string{"--platform", "linux/arm64"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arch != "arm64" {
		t.Errorf("expected arm64 from runtime_args, got %q", arch)
	}

	// Explicit platform beats everything.
	arch, err = resolveContainerArch(Config{Platform: "linux/386", RuntimeArgs: []string{"--platform", "linux/arm64"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arch != "386" {
		t.Errorf("expected 386 from cfg.Platform, got %q", arch)
	}

	// Invalid explicit platform is an error.
	if _, err := resolveContainerArch(Config{Platform: "linux/"}); err == nil {
		t.Error("expected error for invalid cfg.Platform")
	}
}

func TestVolumeNameForArch(t *testing.T) {
	overrideNativeArch(t, "arm64")

	if got := volumeNameForArch("yolobox-home", "arm64"); got != "yolobox-home" {
		t.Errorf("native arch should keep legacy name, got %q", got)
	}
	if got := volumeNameForArch("yolobox-home", "amd64"); got != "yolobox-home-amd64" {
		t.Errorf("non-native arch should get suffix, got %q", got)
	}
	if got := volumeNameForArch("yolobox-cache", "amd64"); got != "yolobox-cache-amd64" {
		t.Errorf("non-native arch should get suffix, got %q", got)
	}
}

func TestMergeConfigPlatform(t *testing.T) {
	dst := defaultConfig()
	mergeConfig(&dst, Config{Platform: "linux/amd64"})
	if dst.Platform != "linux/amd64" {
		t.Errorf("expected platform merged, got %q", dst.Platform)
	}
	mergeConfig(&dst, Config{})
	if dst.Platform != "linux/amd64" {
		t.Errorf("empty src should not clear platform, got %q", dst.Platform)
	}
}

func TestBuildRunArgsPlatformEmulated(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "")

	cfg := Config{
		Image:           "test-image",
		Platform:        "linux/amd64",
		ReadonlyProject: true,
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--platform linux/amd64") {
		t.Errorf("expected --platform linux/amd64 in run args, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-home-amd64:/home/yolo") {
		t.Errorf("expected arch-suffixed home volume, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-cache-amd64:/var/cache") {
		t.Errorf("expected arch-suffixed cache volume, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-output-amd64:/output") {
		t.Errorf("expected arch-suffixed output volume, got %s", argsStr)
	}
	if strings.Contains(argsStr, "yolobox-home:/home/yolo") {
		t.Errorf("legacy home volume must not be mounted for emulated arch, got %s", argsStr)
	}
}

func TestBuildRunArgsPlatformNativeKeepsLegacyNames(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "")

	cfg := Config{
		Image:    "test-image",
		Platform: "linux/arm64",
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--platform linux/arm64") {
		t.Errorf("expected --platform passthrough for native arch, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-home:/home/yolo") {
		t.Errorf("expected legacy home volume name for native arch, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-cache:/var/cache") {
		t.Errorf("expected legacy cache volume name for native arch, got %s", argsStr)
	}
}

func TestBuildRunArgsDockerDefaultPlatformEnv(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "linux/amd64")

	cfg := Config{Image: "test-image"}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "yolobox-home-amd64:/home/yolo") {
		t.Errorf("expected arch-suffixed home volume from DOCKER_DEFAULT_PLATFORM, got %s", argsStr)
	}
	// The docker CLI already honors DOCKER_DEFAULT_PLATFORM itself; yolobox
	// only adds --platform when configured explicitly.
	if strings.Contains(argsStr, "--platform") {
		t.Errorf("did not expect explicit --platform from env-only config, got %s", argsStr)
	}
}

func TestBuildRunArgsPlatformInvalid(t *testing.T) {
	cfg := Config{
		Image:    "test-image",
		Platform: "linux/",
	}
	if _, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false); err == nil {
		t.Fatal("expected error for invalid platform value")
	}
}

func TestBuildRunArgsPlatformAppleContainerUnsupported(t *testing.T) {
	runtimeDir := t.TempDir()
	containerPath := filepath.Join(runtimeDir, "container")
	if err := os.WriteFile(containerPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write fake container runtime: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := Config{
		Runtime:  "container",
		Image:    "test-image",
		Platform: "linux/amd64",
	}

	_, _, err := buildRunArgs(cfg, t.TempDir(), []string{"bash"}, false)
	if err == nil {
		t.Fatal("expected Apple container runtime to reject --platform")
	}
	if !strings.Contains(err.Error(), "not supported with Apple container runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMatchYoloboxVolumes(t *testing.T) {
	names := []string{
		"yolobox-home",
		"yolobox-cache",
		"yolobox-output",
		"yolobox-home-amd64",
		"yolobox-cache-arm64",
		"yolobox-output-riscv64",
		"yolobox-home-backup",
		"yolobox-net",
		"my-yolobox-home",
		"postgres-data",
	}
	want := []string{
		"yolobox-home",
		"yolobox-cache",
		"yolobox-output",
		"yolobox-home-amd64",
		"yolobox-cache-arm64",
		"yolobox-output-riscv64",
	}
	got := matchYoloboxVolumes(names)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("matchYoloboxVolumes = %v, want %v", got, want)
	}
}

func TestVolumesForPlatform(t *testing.T) {
	overrideNativeArch(t, "arm64")

	got, err := volumesForPlatform("amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"yolobox-home-amd64", "yolobox-cache-amd64", "yolobox-output-amd64"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("volumesForPlatform(amd64) = %v, want %v", got, want)
	}

	got, err = volumesForPlatform("linux/arm64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = []string{"yolobox-home", "yolobox-cache", "yolobox-output"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("volumesForPlatform(linux/arm64) = %v, want %v", got, want)
	}

	if _, err := volumesForPlatform("linux/"); err == nil {
		t.Error("expected error for invalid platform")
	}
}

func TestPullImagePassesPlatform(t *testing.T) {
	binPath, logFile := installLoggingRuntimeNamed(t, "docker")
	if err := pullImage(binPath, "ghcr.io/finbarr/yolobox:latest", "linux/amd64"); err != nil {
		t.Fatalf("pullImage: %v", err)
	}
	log := readRuntimeLog(t, logFile)
	if !logHasLine(log, "pull --platform linux/amd64 ghcr.io/finbarr/yolobox:latest") {
		t.Fatalf("expected pull with --platform, got:\n%s", strings.Join(log, "\n"))
	}
}
