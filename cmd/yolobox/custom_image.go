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

func generateCustomDockerfile(baseImage string, packages []string, fragment string) (string, error) {
	normalized := normalizePackages(packages)
	for _, pkg := range normalized {
		if !isValidPackageName(pkg) {
			return "", fmt.Errorf("invalid package name %q", pkg)
		}
	}

	var builder strings.Builder
	builder.WriteString("# syntax=docker/dockerfile:1\n")
	builder.WriteString("FROM ")
	builder.WriteString(baseImage)
	builder.WriteString("\n")

	if len(normalized) > 0 {
		builder.WriteString("USER root\n")
		builder.WriteString("RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \\\n")
		builder.WriteString("    --mount=type=cache,target=/var/lib/apt,sharing=locked \\\n")
		builder.WriteString("    apt-get update && apt-get install -y --no-install-recommends ")
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

func inspectImageID(runtimePath, image, platform string) (string, error) {
	cmd := exec.Command(runtimePath, "image", "inspect", image, "--format", "{{.Id}}")
	output, err := cmd.Output()
	if err != nil {
		if pullErr := pullImage(runtimePath, image, platform); pullErr != nil {
			return "", fmt.Errorf("failed to inspect or pull base image %q: %w", image, err)
		}

		cmd = exec.Command(runtimePath, "image", "inspect", image, "--format", "{{.Id}}")
		output, err = cmd.Output()
		if err != nil {
			return "", fmt.Errorf("failed to inspect base image %q: %w", image, err)
		}
	}
	return strings.TrimSpace(string(output)), nil
}

func customImageExists(runtimePath, tag string) bool {
	return exec.Command(runtimePath, "image", "inspect", tag).Run() == nil
}

func buildCustomImage(runtimePath, tag, dockerfilePath, contextDir, platform string) error {
	platform = dockerPlatform(platform)
	buildArgs := []string{"build", "-t", tag, "-f", dockerfilePath}
	if platform != "" {
		buildArgs = append(buildArgs, "--platform", platform)
	}
	buildArgs = append(buildArgs, contextDir)
	cmd := exec.Command(runtimePath, buildArgs...)
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// pullLatestBaseImage force-pulls the configured base image from its registry,
// even when a copy already exists locally, so a stale-but-present local image is
// refreshed. Any derived image is rebuilt afterwards by prepareCustomImage because
// a changed base image ID changes the derived content-hash tag.
func pullLatestBaseImage(cfg Config) error {
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}
	platform, err := effectivePlatform(cfg)
	if err != nil {
		return err
	}
	info("Pulling base image %s...", cfg.Image)
	return pullImage(runtimePath, cfg.Image, platform)
}

func prepareCustomImage(cfg *Config, projectDir string) (string, error) {
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return "", err
	}
	if filepath.Base(runtimePath) == "container" {
		return "", fmt.Errorf("custom images are not supported with Apple container runtime")
	}

	platform, err := effectivePlatform(*cfg)
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
	dockerfile, err := generateCustomDockerfile(cfg.Image, cfg.Customize.Packages, fragment)
	if err != nil {
		return "", err
	}
	baseImageID, err := inspectImageID(runtimePath, cfg.Image, platform)
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
	if err := buildCustomImage(runtimePath, tag, dockerfilePath, contextDir, platform); err != nil {
		return "", fmt.Errorf("failed to build custom image: %w", err)
	}
	return tag, nil
}
