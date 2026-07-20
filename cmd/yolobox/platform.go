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

// resolveContainerArch determines the architecture the container will run as.
// First match wins: the platform option, a --platform already passed via
// runtime_args, DOCKER_DEFAULT_PLATFORM, then the native host architecture.
func resolveContainerArch(cfg Config) (string, error) {
	if cfg.Platform != "" {
		return archFromPlatform(cfg.Platform)
	}
	if value := platformFromRuntimeArgs(cfg.RuntimeArgs); value != "" {
		return archFromPlatform(value)
	}
	if value := os.Getenv("DOCKER_DEFAULT_PLATFORM"); value != "" {
		return archFromPlatform(value)
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
