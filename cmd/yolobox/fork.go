package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var forkNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type forkInfo struct {
	Name           string
	Source         string
	Copy           string
	RunDir         string
	ComposeProject string
}

func runFork(args []string, projectDir string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printForkUsage()
		return errHelp
	}

	switch args[0] {
	case "resume":
		return runForkResume(args[1:], projectDir)
	case "discard":
		return runForkDiscard(args[1:], projectDir)
	default:
		return runForkCreate(args, projectDir)
	}
}

func printForkUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox fork --name <env> <cmd...>       Run command in a named copied folder")
	fmt.Fprintln(os.Stderr, "  yolobox fork resume <env> [cmd...]       Reopen an existing copied folder")
	fmt.Fprintln(os.Stderr, "  yolobox fork discard <env> --force       Delete a copied folder")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "EXAMPLES:")
	fmt.Fprintln(os.Stderr, "  yolobox fork --name bruno codex")
	fmt.Fprintln(os.Stderr, "  yolobox fork --name diane claude")
	fmt.Fprintln(os.Stderr, "  yolobox fork resume bruno codex")
}

func runForkCreate(args []string, projectDir string) error {
	fs := flag.NewFlagSet("fork", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var name string
	fs.StringVar(&name, "name", "", "developer/environment name")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printForkUsage()
			return errHelp
		}
		return err
	}
	if name == "" {
		return fmt.Errorf("yolobox fork requires --name for v1")
	}
	if err := validateForkName(name); err != nil {
		return err
	}

	info, err := newForkInfo(projectDir, name)
	if err != nil {
		return err
	}
	if err := ensureNewForkAvailable(info); err != nil {
		return err
	}
	if err := os.MkdirAll(info.Copy, 0755); err != nil {
		return fmt.Errorf("failed to create copied folder: %w", err)
	}
	if err := copyFullDirectory(info.Source, info.Copy); err != nil {
		_ = os.RemoveAll(info.Copy)
		return err
	}
	success("Created fork %s at %s", info.Name, info.Copy)

	commandArgs := fs.Args()
	return runForkedYolobox(info, commandArgs)
}

func runForkResume(args []string, projectDir string) error {
	if len(args) == 0 {
		return fmt.Errorf("yolobox fork resume requires a developer/environment name")
	}
	name := args[0]
	if err := validateForkName(name); err != nil {
		return err
	}
	info, err := newForkInfo(projectDir, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(info.Copy); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("copied folder for fork %s is missing: %s", name, info.Copy)
		}
		return err
	}
	commandArgs := args[1:]
	return runForkedYolobox(info, commandArgs)
}

func runForkDiscard(args []string, projectDir string) error {
	if len(args) == 0 {
		return fmt.Errorf("yolobox fork discard requires a developer/environment name")
	}

	name := ""
	force := false
	for _, arg := range args {
		switch arg {
		case "--force":
			force = true
		default:
			if name != "" {
				return fmt.Errorf("unexpected fork discard argument: %s", arg)
			}
			name = arg
		}
	}
	if name == "" {
		return fmt.Errorf("yolobox fork discard requires a developer/environment name")
	}
	if !force {
		return fmt.Errorf("yolobox fork discard requires --force")
	}
	if err := validateForkName(name); err != nil {
		return err
	}
	info, err := newForkInfo(projectDir, name)
	if err != nil {
		return err
	}

	runComposeCleanup(info)
	if err := os.RemoveAll(info.Copy); err != nil {
		return fmt.Errorf("failed to remove copied folder: %w", err)
	}
	success("Discarded fork %s", name)
	return nil
}

func runForkedYolobox(info forkInfo, commandArgs []string) error {
	if _, err := os.Stat(info.RunDir); err != nil {
		if os.IsNotExist(err) {
			info.RunDir = info.Copy
		} else {
			return err
		}
	}

	origDir, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(info.RunDir); err != nil {
		return fmt.Errorf("failed to enter copied folder: %w", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	forkCfg := info.forkConfig()
	runErr := runCmdArgs(commandArgs, info.RunDir, &forkCfg)

	runComposeCleanup(info)
	printForkPreserved(info)

	return runErr
}

func (info forkInfo) forkConfig() ForkConfig {
	return ForkConfig{
		Name:           info.Name,
		Source:         info.Source,
		Copy:           info.Copy,
		ComposeProject: info.ComposeProject,
	}
}

func printForkPreserved(info forkInfo) {
	fmt.Fprintf(os.Stderr, "\nFork %s preserved.\n\n", info.Name)
	fmt.Fprintln(os.Stderr, "Copy:")
	fmt.Fprintf(os.Stderr, "  %s\n\n", info.Copy)
	fmt.Fprintln(os.Stderr, "Resume:")
	fmt.Fprintf(os.Stderr, "  yolobox fork resume %s\n\n", info.Name)
	fmt.Fprintln(os.Stderr, "Discard:")
	fmt.Fprintf(os.Stderr, "  yolobox fork discard %s --force\n", info.Name)
}

func runComposeCleanup(info forkInfo) {
	composeDir := findComposeDir(info.RunDir, info.Copy)
	if composeDir == "" {
		return
	}

	cfg, err := loadConfig(composeDir)
	if err != nil {
		warn("Skipping Compose cleanup: failed to load yolobox config: %v", err)
		return
	}
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		warn("Skipping Compose cleanup: %v", err)
		return
	}
	if isAppleContainerPath(runtimePath) {
		return
	}

	cmd := exec.Command(runtimePath, "compose", "-p", info.ComposeProject, "down", "--volumes", "--remove-orphans")
	cmd.Dir = composeDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if strings.Contains(strings.ToLower(trimmed), "no configuration file") {
			return
		}
		if trimmed == "" {
			warn("Compose cleanup failed for %s: %v", info.ComposeProject, err)
			return
		}
		warn("Compose cleanup failed for %s: %s", info.ComposeProject, trimmed)
	}
}

func findComposeDir(paths ...string) string {
	for _, path := range paths {
		for _, name := range []string{"compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml"} {
			if _, err := os.Stat(filepath.Join(path, name)); err == nil {
				return path
			}
		}
	}
	return ""
}

func newForkInfo(projectDir, name string) (forkInfo, error) {
	source, err := filepath.Abs(projectDir)
	if err != nil {
		return forkInfo{}, err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return forkInfo{}, err
	}
	folderName := slugify(filepath.Base(source), "folder")
	copy := filepath.Join(filepath.Dir(source), ".yolobox-forks", folderName, name)

	return forkInfo{
		Name:           name,
		Source:         source,
		Copy:           copy,
		RunDir:         copy,
		ComposeProject: composeProjectName(source, name),
	}, nil
}

func ensureNewForkAvailable(info forkInfo) error {
	if _, err := os.Stat(info.Copy); err == nil {
		return fmt.Errorf("fork copied folder already exists: %s\nUse: yolobox fork resume %s", info.Copy, info.Name)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func copyFullDirectory(source, destination string) error {
	cmd := exec.Command("cp", "-a", source+string(os.PathSeparator)+".", destination)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			trimmed = err.Error()
		}
		return fmt.Errorf("failed to copy folder: %s", trimmed)
	}
	return nil
}

func validateForkName(name string) error {
	if !forkNamePattern.MatchString(name) {
		return fmt.Errorf("invalid developer/environment name %q: use lowercase letters, numbers, and hyphens", name)
	}
	return nil
}

func composeProjectName(source, forkName string) string {
	hash := sha1.Sum([]byte(source))
	shortHash := hex.EncodeToString(hash[:])[:10]
	return slugify(filepath.Base(source), "folder") + "-" + shortHash + "-" + forkName
}

func slugify(value, fallback string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return fallback
	}
	return slug
}
