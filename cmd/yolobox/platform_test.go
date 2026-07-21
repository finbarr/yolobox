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

	// Explicit platform agreeing with runtime_args is fine.
	arch, err = resolveContainerArch(Config{Platform: "amd64", RuntimeArgs: []string{"--platform", "linux/amd64"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arch != "amd64" {
		t.Errorf("expected amd64, got %q", arch)
	}

	// Conflicting explicit platform and runtime_args are rejected.
	if _, err := resolveContainerArch(Config{Platform: "linux/386", RuntimeArgs: []string{"--platform", "linux/arm64"}}); err == nil {
		t.Error("expected error for conflicting platform sources")
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
	// The env var is part of the effective platform, so yolobox emits it
	// explicitly rather than trusting the runtime to apply it. That keeps the
	// architecture that runs in step with the volumes that were mounted, and
	// carries the same value to pull and custom-image build.
	if !strings.Contains(argsStr, "--platform linux/amd64") {
		t.Errorf("expected explicit --platform from DOCKER_DEFAULT_PLATFORM, got %s", argsStr)
	}
}

// Apple container cannot act on DOCKER_DEFAULT_PLATFORM and only runs the
// native architecture, so an ambient value must not select another
// architecture's volumes for it.
func TestBuildRunArgsDockerDefaultPlatformIgnoredForAppleContainer(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "linux/amd64")

	runtimeDir := t.TempDir()
	containerPath := filepath.Join(runtimeDir, "container")
	if err := os.WriteFile(containerPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write fake container runtime: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := Config{Runtime: "container", Image: "test-image"}

	args, _, err := buildRunArgs(cfg, t.TempDir(), []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "--platform") {
		t.Errorf("Apple container must not receive --platform, got %s", argsStr)
	}
	if strings.Contains(argsStr, "yolobox-home-amd64") {
		t.Errorf("Apple container must keep native volumes, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-home:/home/yolo") {
		t.Errorf("expected native home volume for Apple container, got %s", argsStr)
	}
}

func TestResolveContainerArchIgnoresEnvForAppleContainer(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "linux/amd64")

	runtimeDir := t.TempDir()
	containerPath := filepath.Join(runtimeDir, "container")
	if err := os.WriteFile(containerPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write fake container runtime: %v", err)
	}
	t.Setenv("PATH", runtimeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	arch, err := resolveContainerArch(Config{Runtime: "container"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arch != "arm64" {
		t.Errorf("expected native arm64 for Apple container, got %q", arch)
	}
}

// One effective platform must reach every runtime invocation. An ambient
// DOCKER_DEFAULT_PLATFORM that only reached `run` would pull one architecture
// and run another.
func TestDockerDefaultPlatformReachesPullAndRun(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "linux/amd64")
	logFile := installLoggingDockerRuntime(t)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"shell", "--ensure-latest"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	log := readRuntimeLog(t, logFile)
	sawPull, sawRun := false, false
	for _, line := range log {
		switch {
		case strings.HasPrefix(line, "pull "):
			sawPull = true
			if !strings.Contains(line, "--platform linux/amd64") {
				t.Errorf("expected pull to carry the effective platform, got: %q", line)
			}
		case strings.HasPrefix(line, "run "):
			sawRun = true
			if !strings.Contains(line, "--platform linux/amd64") {
				t.Errorf("expected run to carry the effective platform, got: %q", line)
			}
		}
	}
	if !sawPull || !sawRun {
		t.Fatalf("expected both a pull and a run, got:\n%s", strings.Join(log, "\n"))
	}
}

func TestContextManifestReportsPlatformAndArch(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "")

	// Native run: no platform is passed to the runtime, but the architecture
	// the volumes belong to is still reported.
	manifest := buildContextManifest(Config{Image: "test-image"}, "/test/project", []string{"bash"}, false, nil, false)
	if manifest.Runtime.Platform != "" {
		t.Errorf("expected no platform for a native run, got %q", manifest.Runtime.Platform)
	}
	if manifest.Runtime.Arch != "arm64" {
		t.Errorf("expected native arm64 arch, got %q", manifest.Runtime.Arch)
	}

	// Emulated run: the manifest reports the same normalized value the runtime
	// receives, plus the configured value under config.
	manifest = buildContextManifest(Config{Image: "test-image", Platform: "amd64"}, "/test/project", []string{"bash"}, false, nil, false)
	if manifest.Runtime.Platform != "linux/amd64" {
		t.Errorf("expected normalized linux/amd64, got %q", manifest.Runtime.Platform)
	}
	if manifest.Runtime.Arch != "amd64" {
		t.Errorf("expected amd64 arch, got %q", manifest.Runtime.Arch)
	}
	if manifest.Config.Platform != "amd64" {
		t.Errorf("expected configured platform preserved, got %q", manifest.Config.Platform)
	}

	// Raw runtime_args are resolved the same way, so in-box guidance sees the
	// architecture the runtime actually got.
	manifest = buildContextManifest(
		Config{Image: "test-image", RuntimeArgs: []string{"--platform=linux/amd64"}},
		"/test/project", []string{"bash"}, false, nil, false,
	)
	if manifest.Runtime.Arch != "amd64" {
		t.Errorf("expected amd64 arch from runtime_args, got %q", manifest.Runtime.Arch)
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

func TestDockerPlatform(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"", ""},
		{"linux/amd64", "linux/amd64"},
		{"linux/arm64", "linux/arm64"},
		{"linux/arm/v7", "linux/arm/v7"},
		// A bare architecture must gain an explicit linux/ prefix: the
		// docker CLI on macOS otherwise resolves "amd64" as darwin/amd64,
		// which no linux image manifest matches.
		{"amd64", "linux/amd64"},
		{"arm64", "linux/arm64"},
		{"x86_64", "linux/amd64"},
		{"aarch64", "linux/arm64"},
		{"linux/x86_64", "linux/amd64"},
		{"LINUX/AMD64", "linux/amd64"},
		{" amd64 ", "linux/amd64"},
	}
	for _, tt := range tests {
		if got := dockerPlatform(tt.value); got != tt.want {
			t.Errorf("dockerPlatform(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestBuildRunArgsPlatformBareArchNormalized(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "")

	cfg := Config{
		Image:    "test-image",
		Platform: "amd64",
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--platform linux/amd64") {
		t.Errorf("expected bare arch to be normalized to --platform linux/amd64, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-home-amd64:/home/yolo") {
		t.Errorf("expected arch-suffixed home volume, got %s", argsStr)
	}
}

func TestPullImageNormalizesBareArchPlatform(t *testing.T) {
	binPath, logFile := installLoggingRuntimeNamed(t, "docker")
	if err := pullImage(binPath, "ghcr.io/finbarr/yolobox:latest", "amd64"); err != nil {
		t.Fatalf("pullImage: %v", err)
	}
	log := readRuntimeLog(t, logFile)
	if !logHasLine(log, "pull --platform linux/amd64 ghcr.io/finbarr/yolobox:latest") {
		t.Fatalf("expected pull with normalized --platform, got:\n%s", strings.Join(log, "\n"))
	}
}

func TestBuildCustomImageNormalizesBareArchPlatform(t *testing.T) {
	binPath, logFile := installLoggingRuntimeNamed(t, "docker")
	if err := buildCustomImage(binPath, "test-tag", "/tmp/Dockerfile", "/tmp", "amd64"); err != nil {
		t.Fatalf("buildCustomImage: %v", err)
	}
	log := readRuntimeLog(t, logFile)
	if !logHasLine(log, "build -t test-tag -f /tmp/Dockerfile --platform linux/amd64 /tmp") {
		t.Fatalf("expected build with normalized --platform, got:\n%s", strings.Join(log, "\n"))
	}
}

func TestEffectivePlatform(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		want    string
		wantErr bool
	}{
		{"neither", Config{}, "", false},
		{"config only", Config{Platform: "linux/amd64"}, "linux/amd64", false},
		{"runtime args only", Config{RuntimeArgs: []string{"--platform", "linux/amd64"}}, "linux/amd64", false},
		{"runtime args equals form", Config{RuntimeArgs: []string{"--platform=linux/arm64"}}, "linux/arm64", false},
		{"both agree after normalization", Config{Platform: "amd64", RuntimeArgs: []string{"--platform", "linux/amd64"}}, "amd64", false},
		{"conflict", Config{Platform: "linux/amd64", RuntimeArgs: []string{"--platform", "linux/arm64"}}, "", true},
	}
	for _, tt := range tests {
		got, err := effectivePlatform(tt.cfg)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %q", tt.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: effectivePlatform = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestStripPlatformFromRuntimeArgs(t *testing.T) {
	got := stripPlatformFromRuntimeArgs([]string{
		"--security-opt", "seccomp=unconfined",
		"--platform", "linux/amd64",
		"--platform=linux/arm64",
		"--memory-swap", "2g",
	})
	want := []string{"--security-opt", "seccomp=unconfined", "--memory-swap", "2g"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("stripPlatformFromRuntimeArgs = %v, want %v", got, want)
	}
}

func TestBuildRunArgsPlatformConflict(t *testing.T) {
	cfg := Config{
		Image:       "test-image",
		Platform:    "linux/amd64",
		RuntimeArgs: []string{"--platform", "linux/arm64"},
	}
	_, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err == nil {
		t.Fatal("expected error for conflicting platform sources")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got: %v", err)
	}
}

func TestBuildRunArgsRuntimeArgsPlatformOnly(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "")

	cfg := Config{
		Image:       "test-image",
		RuntimeArgs: []string{"--memory-swap", "2g", "--platform", "amd64"},
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--platform linux/amd64") {
		t.Errorf("expected normalized --platform from runtime_args, got %s", argsStr)
	}
	if strings.Count(argsStr, "--platform") != 1 {
		t.Errorf("expected exactly one --platform (raw runtime arg stripped), got %s", argsStr)
	}
	if !strings.Contains(argsStr, "--memory-swap 2g") {
		t.Errorf("other runtime args must be preserved, got %s", argsStr)
	}
	if !strings.Contains(argsStr, "yolobox-home-amd64:/home/yolo") {
		t.Errorf("expected arch-suffixed home volume, got %s", argsStr)
	}
}

func TestEnsureLatestPullsWithRuntimeArgsPlatform(t *testing.T) {
	overrideNativeArch(t, "arm64")
	t.Setenv("DOCKER_DEFAULT_PLATFORM", "")
	projectDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	logFile := installLoggingDockerRuntime(t)
	defer silenceStderr(t)()

	if err := runCmdArgs([]string{"shell", "--ensure-latest", "--runtime-arg", "--platform=linux/amd64"}, projectDir, nil); err != nil {
		t.Fatalf("runCmdArgs: %v", err)
	}

	log := readRuntimeLog(t, logFile)
	if !logHasLine(log, "pull --platform linux/amd64 ghcr.io/finbarr/yolobox:latest") {
		t.Fatalf("expected base-image pull to use the runtime-args platform, got:\n%s", strings.Join(log, "\n"))
	}
}

func TestSaveGlobalConfigPlatform(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", t.TempDir())

	cfg := Config{Platform: "linux/amd64"}
	if err := saveGlobalConfig(cfg); err != nil {
		t.Fatalf("saveGlobalConfig failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configHome, "yolobox", "config.toml"))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	if !strings.Contains(string(data), "platform = \"linux/amd64\"") {
		t.Fatalf("expected saved config to persist platform, got:\n%s", string(data))
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
