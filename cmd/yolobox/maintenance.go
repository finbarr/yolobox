package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

func resetVolumes(args []string) error {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "remove volumes")
	platform := fs.String("platform", "", "only remove volumes for this platform/architecture (e.g. linux/amd64)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return errHelp
		}
		return err
	}
	if !*force {
		return fmt.Errorf("reset requires --force (this will delete all cached data)")
	}

	cfg, err := loadConfigFromEnv()
	if err != nil {
		return err
	}
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}

	if *platform != "" {
		warn("Removing yolobox volumes for %s...", *platform)
	} else {
		warn("Removing yolobox volumes (all architectures)...")
	}
	if err := removeYoloboxVolumes(runtimePath, *platform); err != nil {
		return err
	}
	success("Reset complete.")
	return nil
}

// discoverYoloboxVolumes lists the runtime's volumes and returns the yolobox
// persistent volumes, including per-architecture variants. Quiet listing works
// on Docker, Podman, and Apple container; Docker's Go templates do not.
func discoverYoloboxVolumes(runtimePath string) ([]string, error) {
	cmd := exec.Command(runtimePath, "volume", "ls", "--quiet")
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}
	return matchYoloboxVolumes(strings.Fields(string(output))), nil
}

func removeYoloboxVolumes(runtimePath, platform string) error {
	var selected []string
	if platform != "" {
		var err error
		selected, err = volumesForPlatform(platform)
		if err != nil {
			return err
		}
	}
	volumes, err := discoverYoloboxVolumes(runtimePath)
	if err != nil {
		return err
	}
	if platform != "" {
		volumes = slices.DeleteFunc(volumes, func(name string) bool {
			return !slices.Contains(selected, name)
		})
	}
	if len(volumes) == 0 {
		info("No matching yolobox volumes to remove.")
		return nil
	}
	args := []string{"volume", "rm"}
	// --force confirms the yolobox operation. Apple container has no force
	// deletion flag and rejects missing names, so only pass discovered volumes.
	if filepath.Base(runtimePath) != "container" {
		args = append(args, "-f")
	}
	if err := execCommand(runtimePath, append(args, volumes...)); err != nil {
		return fmt.Errorf("failed to remove yolobox volumes: %w", err)
	}
	return nil
}

func uninstallYolobox(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "confirm uninstall")
	keepVolumes := fs.Bool("keep-volumes", false, "keep runtime volumes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return errHelp
		}
		return err
	}
	if !*force {
		fmt.Println("This will remove:")
		fmt.Println("  - yolobox binary")
		fmt.Println("  - Global yolobox config and update cache")
		if !*keepVolumes {
			fmt.Println("  - Runtime volumes (yolobox-home, yolobox-cache, yolobox-output, and per-architecture variants)")
		}
		fmt.Println("")
		return fmt.Errorf("run with --force to confirm (use --keep-volumes to preserve runtime data)")
	}

	// Resolve and use the configured runtime before deleting its configuration.
	// On cleanup failure, keep the installation intact so the user can retry.
	if !*keepVolumes {
		cfg, err := loadConfigFromEnv()
		if err != nil {
			return err
		}
		runtimePath, err := resolveRuntime(cfg.Runtime)
		if err != nil {
			return err
		}
		info("Removing yolobox volumes...")
		if err := removeYoloboxVolumes(runtimePath, ""); err != nil {
			return err
		}
	}

	configPath, err := globalConfigPath()
	if err != nil {
		return err
	}
	cacheDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	// The update cache lives separately from config on macOS.
	for _, dir := range []string{filepath.Dir(configPath), filepath.Join(cacheDir, "yolobox")} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		info("Removing %s...", dir)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to remove %s: %w", dir, err)
		}
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find executable path: %w", err)
	}

	info("Removing %s...", execPath)
	if err := os.Remove(execPath); err != nil {
		return fmt.Errorf("failed to remove binary: %w (try: sudo rm %s)", err, execPath)
	}

	success("yolobox has been uninstalled. Goodbye!")
	return nil
}

func execCommand(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type upgradeOptions struct {
	Check bool
}

func parseUpgradeOptions(args []string) (upgradeOptions, error) {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "check latest release without upgrading")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return upgradeOptions{}, errHelp
		}
		return upgradeOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return upgradeOptions{}, fmt.Errorf("unexpected args: %v", fs.Args())
	}
	return upgradeOptions{Check: *check}, nil
}

func upgradeYolobox(args []string) error {
	opts, err := parseUpgradeOptions(args)
	if err != nil {
		return err
	}

	info("Checking for updates...")

	client := &http.Client{Timeout: 30 * time.Second}
	release, err := fetchLatestRelease(client)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := latestVersionFromRelease(release)
	if latestVersion == "" {
		return fmt.Errorf("latest release is missing a tag")
	}
	currentVersion := strings.TrimPrefix(Version, "v")
	latest := releaseInfo{
		Version: latestVersion,
		URL:     releaseURL(release),
		Notes:   releaseSummary(release),
	}

	if opts.Check {
		return printUpgradeCheck(latest, currentVersion)
	}

	if !isNewerVersion(latestVersion, Version) {
		success("Already at latest version (%s)", Version)
	} else {
		info("New version available: %s (current: %s)", latestVersion, currentVersion)

		binaryName := fmt.Sprintf("yolobox-%s-%s", runtime.GOOS, runtime.GOARCH)
		var downloadURL string
		for _, asset := range release.Assets {
			if asset.Name == binaryName {
				downloadURL = asset.BrowserDownloadURL
				break
			}
		}

		if downloadURL == "" {
			return fmt.Errorf("no binary available for %s/%s", runtime.GOOS, runtime.GOARCH)
		}

		info("Downloading %s...", binaryName)
		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("failed to download: %w", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != 200 {
			return fmt.Errorf("failed to download: HTTP %d", resp.StatusCode)
		}

		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return fmt.Errorf("failed to resolve executable path: %w", err)
		}

		tmpFile, err := os.CreateTemp(filepath.Dir(execPath), "yolobox-upgrade-*")
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()

		_, err = io.Copy(tmpFile, resp.Body)
		if closeErr := tmpFile.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to write binary: %w", err)
		}

		if err := os.Chmod(tmpPath, 0755); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to chmod: %w", err)
		}

		if err := os.Rename(tmpPath, execPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to replace binary: %w", err)
		}

		success("Binary upgraded to %s", latestVersion)
		printPostUpgradeNotes(latest)
	}

	info("Pulling latest Docker image...")
	cfg := defaultConfig()
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}
	platform, err := effectivePlatform(cfg)
	if err != nil {
		return err
	}
	if err := pullImage(runtimePath, cfg.Image, platform); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	success("Upgrade complete!")
	return nil
}

func printUpgradeCheck(latest releaseInfo, currentVersion string) error {
	if isNewerVersion(latest.Version, Version) {
		info("New version available: %s (current: %s)", latest.Version, currentVersion)
		printReleaseNotes(os.Stderr, latest.Notes)
		if latest.URL != "" {
			fmt.Fprintf(os.Stderr, "   Release: %s\n", latest.URL)
		}
		fmt.Fprintf(os.Stderr, "   Run %syolobox upgrade%s to update\n", colorBold, colorReset)
		return nil
	}

	success("Already at latest version (%s)", Version)
	if latest.URL != "" {
		fmt.Fprintf(os.Stderr, "   Latest release: %s\n", latest.URL)
	}
	return nil
}

func printPostUpgradeNotes(latest releaseInfo) {
	if len(latest.Notes) == 0 && latest.URL == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "\n%sUpgraded to yolobox v%s%s\n", colorGreen, latest.Version, colorReset)
	printReleaseNotes(os.Stderr, latest.Notes)
	if latest.URL != "" {
		fmt.Fprintf(os.Stderr, "   Release: %s\n", latest.URL)
	}
	fmt.Fprintln(os.Stderr, "")
}
