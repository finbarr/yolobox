package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Apple's container tool gained the features yolobox depends on (file bind
// mounts, image builds, named volume management) in 1.0.
const minAppleContainerVersion = "1.0.0"

var appleContainerVersionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

var (
	appleContainerVersionMu   sync.Mutex
	appleContainerVersionErrs = map[string]error{}
)

func appleContainerUpgradeError(version string) error {
	return fmt.Errorf(`Apple container %s is too old; yolobox requires container >= %s.
Upgrade with: brew upgrade container
or install the latest signed pkg from https://github.com/apple/container/releases
(container 1.0 requires macOS 26 on Apple silicon)`, version, minAppleContainerVersion)
}

// parseAppleContainerVersion extracts the first X.Y.Z version from
// `container --version` output (e.g. "container CLI version 1.0.0 (build: release)").
func parseAppleContainerVersion(output string) (string, error) {
	match := appleContainerVersionPattern.FindString(output)
	if match == "" {
		return "", fmt.Errorf("no version found in Apple container output %q", strings.TrimSpace(output))
	}
	return match, nil
}

// appleContainerVersion probes the installed Apple container CLI version.
// `--version` works on every release and does not need the system service;
// `system version` is a fallback in case the plain output is unparseable.
func appleContainerVersion(runtimePath string) (string, error) {
	for _, args := range [][]string{{"--version"}, {"system", "version", "--format", "json"}} {
		// A wedged container daemon can block CLI calls indefinitely; the
		// version probe must never hang runtime auto-detection.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		output, err := exec.CommandContext(ctx, runtimePath, args...).CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		if version, err := parseAppleContainerVersion(string(output)); err == nil {
			return version, nil
		}
	}
	return "", fmt.Errorf("could not determine Apple container version")
}

// checkAppleContainerVersion errors when the Apple container CLI at
// runtimePath is older than minAppleContainerVersion. Results are memoized
// per path because resolveRuntime is called many times per invocation.
func checkAppleContainerVersion(runtimePath string) error {
	appleContainerVersionMu.Lock()
	defer appleContainerVersionMu.Unlock()
	if err, ok := appleContainerVersionErrs[runtimePath]; ok {
		return err
	}

	err := func() error {
		version, err := appleContainerVersion(runtimePath)
		if err != nil {
			return appleContainerUpgradeError("(unknown version)")
		}
		if compareSemver("v"+version, "v"+minAppleContainerVersion) < 0 {
			return appleContainerUpgradeError(version)
		}
		return nil
	}()
	appleContainerVersionErrs[runtimePath] = err
	return err
}

// ensureAppleContainerSystem starts the container system service when it is
// not already running. The first start may prompt to download a Linux kernel,
// so the command is wired to the terminal.
func ensureAppleContainerSystem(runtimePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if exec.CommandContext(ctx, runtimePath, "system", "status").Run() == nil {
		return nil
	}

	info("Starting Apple container system service...")
	// --enable-kernel-install answers the first-run kernel download prompt,
	// which would otherwise hang non-interactive runs.
	cmd := exec.Command(runtimePath, "system", "start", "--enable-kernel-install")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Apple container system service is not running and could not be started.\nRun `container system start` manually and retry")
	}
	return nil
}

// appleContainerPreflight gates container runs on a supported CLI version and
// a running system service.
func appleContainerPreflight(runtimePath string) error {
	if err := checkAppleContainerVersion(runtimePath); err != nil {
		return err
	}
	return ensureAppleContainerSystem(runtimePath)
}

// parseAppleImageDigest extracts an image identifier from
// `container image inspect` output, which is JSON-only (no --format flag).
// container 1.0 emits a JSON array whose entries carry the manifest digest
// in an "id" field.
func parseAppleImageDigest(output []byte) (string, error) {
	var entries []map[string]any
	if err := json.Unmarshal(output, &entries); err != nil || len(entries) == 0 {
		return "", fmt.Errorf("unexpected Apple container image inspect output")
	}
	for _, key := range []string{"id", "digest"} {
		if value, ok := entries[0][key].(string); ok && value != "" {
			return value, nil
		}
	}
	// The caller only needs a stable cache key, so fall back to hashing the
	// inspect output when no identifier field is present.
	sum := sha256.Sum256(output)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
