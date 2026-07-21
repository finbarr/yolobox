package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// nativeArch is the architecture containers run as when no platform override
// is in play. Runs on the native architecture keep the legacy unsuffixed
// volume names so existing data survives upgrades.
var nativeArch = runtime.GOARCH

var persistentVolumeBases = []string{"yolobox-home", "yolobox-cache", "yolobox-output"}

// knownVolumeArchSuffixes limits volume discovery to real architecture
// suffixes so unrelated user volumes (e.g. yolobox-home-backup) are never
// touched by reset/uninstall.
var knownVolumeArchSuffixes = map[string]bool{
	"amd64":    true,
	"arm64":    true,
	"arm":      true,
	"386":      true,
	"ppc64le":  true,
	"s390x":    true,
	"riscv64":  true,
	"mips64le": true,
	"loong64":  true,
}

// archFromPlatform extracts the normalized architecture component from a
// Docker platform value: "linux/amd64" -> "amd64", "arm64" -> "arm64",
// "linux/arm/v7" -> "arm". x86_64 and aarch64 map to their Go names.
func archFromPlatform(value string) (string, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), "/")
	arch := parts[0]
	if len(parts) > 1 {
		arch = parts[1]
	}
	switch arch {
	case "":
		return "", fmt.Errorf("invalid platform %q: missing architecture (expected e.g. linux/amd64 or arm64)", value)
	case "x86_64":
		return "amd64", nil
	case "aarch64":
		return "arm64", nil
	}
	return arch, nil
}

// dockerPlatform maps a configured platform value to the form passed to the
// container runtime. A bare architecture gains an explicit linux/ prefix: the
// docker CLI otherwise defaults the OS component to the client's OS, so on
// macOS "amd64" would resolve to darwin/amd64, which no linux image manifest
// matches. Alias architectures (x86_64, aarch64) map to their Docker names.
func dockerPlatform(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		parts = []string{"linux", parts[0]}
	}
	switch parts[1] {
	case "x86_64":
		parts[1] = "amd64"
	case "aarch64":
		parts[1] = "arm64"
	}
	return strings.Join(parts, "/")
}

// platformFromRuntimeArgs returns the value of a --platform flag already
// present in raw runtime args, or "" when there is none.
func platformFromRuntimeArgs(args []string) string {
	for i, arg := range args {
		if arg == "--platform" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if value, ok := strings.CutPrefix(arg, "--platform="); ok {
			return value
		}
	}
	return ""
}

// platformFromEnv returns DOCKER_DEFAULT_PLATFORM when the selected runtime can
// actually act on it. Apple `container` never reads the Docker-specific
// variable and runs the native architecture regardless, so honoring it there
// would mount another architecture's volumes into a native container.
func platformFromEnv(cfg Config) string {
	value := os.Getenv("DOCKER_DEFAULT_PLATFORM")
	if value == "" || isAppleContainer(cfg.Runtime) {
		return ""
	}
	return value
}

// effectivePlatform returns the single platform value used for run, pull,
// custom-image build, and volume selection. The platform option and a
// --platform inside runtime_args must agree (after normalization); anything
// else would pull one architecture and run another.
//
// DOCKER_DEFAULT_PLATFORM is the lowest-precedence source. Resolving it here
// rather than only when picking volumes means yolobox passes it explicitly to
// run, pull, and build, so the architecture the container actually gets cannot
// drift from the architecture whose volumes were mounted.
func effectivePlatform(cfg Config) (string, error) {
	fromArgs := platformFromRuntimeArgs(cfg.RuntimeArgs)
	if cfg.Platform != "" && fromArgs != "" && dockerPlatform(cfg.Platform) != dockerPlatform(fromArgs) {
		return "", fmt.Errorf("platform conflict: --platform %s disagrees with --platform %s in runtime args", cfg.Platform, fromArgs)
	}
	if cfg.Platform != "" {
		return cfg.Platform, nil
	}
	if fromArgs != "" {
		return fromArgs, nil
	}
	return platformFromEnv(cfg), nil
}

// stripPlatformFromRuntimeArgs removes --platform entries from raw runtime
// args; the effective platform is emitted once, normalized, by buildRunArgs.
func stripPlatformFromRuntimeArgs(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--platform" {
			i++ // also drop the value
			continue
		}
		if strings.HasPrefix(args[i], "--platform=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// resolveContainerArch determines the architecture the container will run as.
// It derives from the effective platform alone, so volume selection can never
// disagree with the --platform yolobox actually passes to the runtime.
func resolveContainerArch(cfg Config) (string, error) {
	platform, err := effectivePlatform(cfg)
	if err != nil {
		return "", err
	}
	if platform != "" {
		return archFromPlatform(platform)
	}
	return nativeArch, nil
}

// volumeNameForArch maps a persistent volume base name to the volume used for
// arch. The native architecture keeps the legacy unsuffixed names; any other
// architecture gets its own suffixed volumes.
func volumeNameForArch(base, arch string) string {
	if arch == nativeArch {
		return base
	}
	return base + "-" + arch
}

// matchYoloboxVolumes filters a list of volume names down to yolobox
// persistent volumes, including per-architecture variants.
func matchYoloboxVolumes(names []string) []string {
	var matched []string
	for _, name := range names {
		for _, base := range persistentVolumeBases {
			if name == base {
				matched = append(matched, name)
				break
			}
			if suffix, ok := strings.CutPrefix(name, base+"-"); ok && knownVolumeArchSuffixes[suffix] {
				matched = append(matched, name)
				break
			}
		}
	}
	return matched
}

// volumesForPlatform returns the persistent volume names a run with the given
// --platform value would use.
func volumesForPlatform(platform string) ([]string, error) {
	arch, err := archFromPlatform(platform)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(persistentVolumeBases))
	for _, base := range persistentVolumeBases {
		names = append(names, volumeNameForArch(base, arch))
	}
	return names, nil
}
