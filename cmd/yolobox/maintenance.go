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

	var volumes []string
	if *platform != "" {
		volumes, err = volumesForPlatform(*platform)
		if err != nil {
			return err
		}
		warn("Removing yolobox volumes for %s...", *platform)
	} else {
		volumes = discoverYoloboxVolumes(runtimePath)
		warn("Removing yolobox volumes (all architectures)...")
	}
	args = append([]string{"volume", "rm", "-f"}, volumes...)
	if err := execCommand(runtimePath, args); err != nil {
		return err
	}
	success("Fresh start! Volumes removed.")
	return nil
}

// discoverYoloboxVolumes lists the runtime's volumes and returns the yolobox
// persistent volumes, including per-architecture variants. Falls back to the
// legacy fixed names when the query fails.
func discoverYoloboxVolumes(runtimePath string) []string {
	output, err := exec.Command(runtimePath, "volume", "ls", "--format", "{{.Name}}").Output()
	if err != nil {
		return persistentVolumeBases
	}
	volumes := matchYoloboxVolumes(strings.Fields(string(output)))
	if len(volumes) == 0 {
		return persistentVolumeBases
	}
	return volumes
}

func uninstallYolobox(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "confirm uninstall")
	keepVolumes := fs.Bool("keep-volumes", false, "keep Docker volumes")
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
		fmt.Println("  - ~/.config/yolobox/ (config and cache)")
		if !*keepVolumes {
			fmt.Println("  - Docker volumes (yolobox-home, yolobox-cache, yolobox-output, and per-architecture variants)")
		}
		fmt.Println("")
		return fmt.Errorf("run with --force to confirm (use --keep-volumes to preserve Docker data)")
	}

	configDir, err := os.UserConfigDir()
	if err == nil {
		yoloboxConfig := filepath.Join(configDir, "yolobox")
		if _, err := os.Stat(yoloboxConfig); err == nil {
			info("Removing %s...", yoloboxConfig)
			_ = os.RemoveAll(yoloboxConfig)
		}
	}

	if !*keepVolumes {
		cfg, err := loadConfigFromEnv()
		if err == nil {
			runtimePath, err := resolveRuntime(cfg.Runtime)
			if err == nil {
				info("Removing Docker volumes...")
				volumes := discoverYoloboxVolumes(runtimePath)
				_ = execCommand(runtimePath, append([]string{"volume", "rm", "-f"}, volumes...))
			}
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
