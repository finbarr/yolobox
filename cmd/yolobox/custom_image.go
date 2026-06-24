package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func hasCustomization(cfg Config) bool {
	return len(cfg.Customize.Packages) > 0 || strings.TrimSpace(cfg.Customize.Dockerfile) != ""
}

func validateCustomizeConfig(cfg CustomizeConfig) error {
	for _, pkg := range cfg.Packages {
		if !isValidPackageName(pkg) {
			return fmt.Errorf("invalid package name %q", pkg)
		}
	}
	return nil
}

func isValidPackageName(name string) bool {
	return packageNamePattern.MatchString(strings.TrimSpace(name))
}

func normalizePackages(packages []string) []string {
	seen := make(map[string]struct{}, len(packages))
	normalized := make([]string, 0, len(packages))
	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if _, ok := seen[pkg]; ok {
			continue
		}
		seen[pkg] = struct{}{}
		normalized = append(normalized, pkg)
	}
	sort.Strings(normalized)
	return normalized
}

func resolveCustomizeFile(path, projectDir string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	return resolvePath(path, projectDir)
}

func loadCustomizeFragment(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read customize file %q: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func generateCustomDockerfile(baseImage string, packages []string, fragment string, useBuildKitCacheMounts bool) (string, error) {
	normalized := normalizePackages(packages)
	for _, pkg := range normalized {
		if !isValidPackageName(pkg) {
			return "", fmt.Errorf("invalid package name %q", pkg)
		}
	}

	var builder strings.Builder
	if useBuildKitCacheMounts {
		builder.WriteString("# syntax=docker/dockerfile:1\n")
	}
	builder.WriteString("FROM ")
	builder.WriteString(baseImage)
	builder.WriteString("\n")

	if len(normalized) > 0 {
		builder.WriteString("USER root\n")
		if useBuildKitCacheMounts {
			builder.WriteString("RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \\\n")
			builder.WriteString("    --mount=type=cache,target=/var/lib/apt,sharing=locked \\\n")
			builder.WriteString("    apt-get update && apt-get install -y --no-install-recommends ")
		} else {
			builder.WriteString("RUN apt-get update && apt-get install -y --no-install-recommends ")
		}
		builder.WriteString(strings.Join(normalized, " "))
		builder.WriteString(" && rm -rf /var/lib/apt/lists/*\n")
		builder.WriteString("USER yolo\n")
	}

	if fragment != "" {
		builder.WriteString("\n")
		builder.WriteString(fragment)
		if !strings.HasSuffix(fragment, "\n") {
			builder.WriteString("\n")
		}
	}

	return builder.String(), nil
}

func customImageTag(baseImageID, dockerfile string, packages []string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		baseImageID,
		strings.Join(normalizePackages(packages), "\n"),
		dockerfile,
	}, "\n---\n")))
	return "yolobox-custom:" + hex.EncodeToString(sum[:])[:12]
}

func inspectImageID(runtimePath, image string) (string, error) {
	output, err := runImageInspect(runtimePath, image)
	if err != nil {
		pullCmd := exec.Command(runtimePath, "image", "pull", image)
		pullCmd.Stdin = os.Stdin
		pullCmd.Stdout = os.Stdout
		pullCmd.Stderr = os.Stderr
		if pullErr := pullCmd.Run(); pullErr != nil {
			return "", fmt.Errorf("failed to inspect or pull base image %q: %w", image, err)
		}

		output, err = runImageInspect(runtimePath, image)
		if err != nil {
			return "", fmt.Errorf("failed to inspect base image %q: %w", image, err)
		}
	}
	if isAppleContainerPath(runtimePath) {
		return parseAppleImageDigest(output)
	}
	return strings.TrimSpace(string(output)), nil
}

// runImageInspect returns an image identifier in raw bytes. Apple's container
// tool only emits JSON (no --format flag), so the caller parses its output.
func runImageInspect(runtimePath, image string) ([]byte, error) {
	if isAppleContainerPath(runtimePath) {
		return exec.Command(runtimePath, "image", "inspect", image).Output()
	}
	return exec.Command(runtimePath, "image", "inspect", image, "--format", "{{.Id}}").Output()
}

func customImageExists(runtimePath, tag string) bool {
	return exec.Command(runtimePath, "image", "inspect", tag).Run() == nil
}

func buildCustomImage(runtimePath, tag, dockerfilePath, contextDir string) error {
	cmd := exec.Command(runtimePath, "build", "-t", tag, "-f", dockerfilePath, contextDir)
	if !isAppleContainerPath(runtimePath) {
		cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func prepareCustomImage(cfg *Config, projectDir string) (string, error) {
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return "", err
	}

	customizeFile, err := resolveCustomizeFile(cfg.Customize.Dockerfile, projectDir)
	if err != nil {
		return "", err
	}
	cfg.Customize.Dockerfile = customizeFile

	fragment, err := loadCustomizeFragment(customizeFile)
	if err != nil {
		return "", err
	}
	dockerfile, err := generateCustomDockerfile(cfg.Image, cfg.Customize.Packages, fragment, true)
	if err != nil {
		return "", err
	}
	baseImageID, err := inspectImageID(runtimePath, cfg.Image)
	if err != nil {
		return "", err
	}
	tag := customImageTag(baseImageID, dockerfile, cfg.Customize.Packages)

	if !cfg.RebuildImage && customizeFile == "" && customImageExists(runtimePath, tag) {
		info("Using custom image %s", tag)
		return tag, nil
	}

	buildDir, err := os.MkdirTemp("", "yolobox-custom-image-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp build dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(buildDir)
	}()

	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return "", fmt.Errorf("failed to write generated Dockerfile: %w", err)
	}

	contextDir := buildDir
	if customizeFile != "" {
		contextDir = projectDir
	}

	info("Building custom image %s...", tag)
	if err := buildCustomImage(runtimePath, tag, dockerfilePath, contextDir); err != nil {
		return "", fmt.Errorf("failed to build custom image: %w", err)
	}
	return tag, nil
}
