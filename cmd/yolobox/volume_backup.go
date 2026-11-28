package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
)

const volumeArchiveHeader = "yolobox-home-v1 arch="
const volumeArchiveMount = "/yolobox-volume"

var homeVolumeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`)

func validateHomeVolumeName(name string) error {
	if !homeVolumeNamePattern.MatchString(name) {
		return fmt.Errorf("invalid home volume name %q: use letters, digits, dots, underscores or hyphens, starting with a letter or digit", name)
	}
	return nil
}

func runVolumeCommand(args []string) error {
	if len(args) == 0 || slices.Contains([]string{"-h", "--help"}, args[0]) {
		printVolumeUsage()
		return errHelp
	}
	operation := args[0]
	if operation != "backup" && operation != "restore" {
		return fmt.Errorf("unknown volume operation %q: use backup or restore", operation)
	}
	flags := flag.NewFlagSet("volume "+operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runtimeName := flags.String("runtime", "", "container runtime")
	platform := flags.String("platform", "", "container platform")
	image := flags.String("image", "", "helper image with GNU tar")
	var homeVolume string
	if operation == "backup" {
		flags.StringVar(&homeVolume, "home-volume", "", "source home volume")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printVolumeUsage()
			return errHelp
		}
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("volume %s requires an archive file path", operation)
	}
	if homeVolume != "" {
		if err := validateHomeVolumeName(homeVolume); err != nil {
			return err
		}
	}
	cfg, err := loadSetupDefaults()
	if err != nil {
		return err
	}
	if *runtimeName != "" {
		cfg.Runtime = *runtimeName
	}
	if *platform != "" {
		cfg.Platform = *platform
		cfg.RuntimeArgs = stripPlatformFromRuntimeArgs(cfg.RuntimeArgs)
	}
	if *image != "" {
		cfg.Image = *image
	}
	helper, err := newVolumeArchiveRuntime(cfg)
	if err != nil {
		return err
	}
	archive := flags.Arg(0)
	if operation == "backup" {
		if homeVolume == "" {
			homeVolume = volumeNameForArch("yolobox-home", helper.arch)
		}
		if err := helper.backupHomeVolume(homeVolume, archive); err != nil {
			return err
		}
		success("Backed up %s to %q. Keep this archive private: it can contain credentials.", homeVolume, archive)
		return nil
	}
	volume, err := helper.restoreHomeVolume(archive)
	if err != nil {
		return err
	}
	launchRuntime := cfg.Runtime
	if launchRuntime == "" {
		launchRuntime = filepath.Base(helper.path)
	}
	launchPlatform := helper.platform
	if launchPlatform == "" {
		launchPlatform = "linux/" + helper.arch
	}
	info("Try the restored home with: yolobox run --runtime %s --platform %s --home-volume %s bash", quoteVolumeHintArg(launchRuntime), quoteVolumeHintArg(launchPlatform), volume)
	_, err = fmt.Fprintln(os.Stdout, volume)
	return err
}

func quoteVolumeHintArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func printVolumeUsage() {
	fmt.Fprintln(os.Stderr, "Usage: yolobox volume backup [--runtime R] [--platform P] [--image I] [--home-volume NAME] FILE")
	fmt.Fprintln(os.Stderr, "       yolobox volume restore [--runtime R] [--platform P] [--image I] FILE")
	fmt.Fprintln(os.Stderr, "Back up a stopped Docker or Podman home volume or restore it into a new one. Archives can contain credentials.")
}

type volumeArchiveRuntime struct {
	path, image, platform, arch string
	rootlessPodman              bool
}

func newVolumeArchiveRuntime(cfg Config) (volumeArchiveRuntime, error) {
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return volumeArchiveRuntime{}, err
	}
	switch filepath.Base(runtimePath) {
	case "docker", "podman":
	default:
		return volumeArchiveRuntime{}, fmt.Errorf("volume backup and restore require Docker or Podman")
	}
	platform, err := effectivePlatform(cfg)
	if err != nil {
		return volumeArchiveRuntime{}, err
	}
	arch, err := resolveContainerArch(cfg)
	if err != nil {
		return volumeArchiveRuntime{}, err
	}
	if !knownVolumeArchSuffixes[arch] {
		return volumeArchiveRuntime{}, fmt.Errorf("unsupported volume archive architecture %q", arch)
	}
	if strings.HasPrefix(cfg.Image, "-") {
		return volumeArchiveRuntime{}, fmt.Errorf("invalid volume helper image %q", cfg.Image)
	}
	return volumeArchiveRuntime{
		path:           runtimePath,
		image:          cfg.Image,
		platform:       dockerPlatform(platform),
		arch:           arch,
		rootlessPodman: isRootlessPodman(cfg.Runtime),
	}, nil
}

func (helper volumeArchiveRuntime) runtimeOutput(args ...string) (string, error) {
	command := exec.Command(helper.path, args...)
	command.Stderr = os.Stderr
	output, err := command.Output()
	return string(output), err
}

func (helper volumeArchiveRuntime) tarCommand(volume string, restore bool) *exec.Cmd {
	args := []string{"run", "--rm", "--network", "none", "--read-only", "--user", "0:0", "--cap-drop", "ALL",
		"--cap-add", "DAC_OVERRIDE", "--security-opt", "no-new-privileges", "--security-opt", "label=disable",
		"--entrypoint", "/usr/bin/tar"}
	args = appendRunFlag(args, "platform", helper.platform)
	options := "nocopy"
	if helper.rootlessPodman {
		args = append(args, "--userns=keep-id:uid=1000,gid=1000")
	}
	if restore {
		args = append(args, "-i", "--cap-add", "CHOWN", "--cap-add", "FOWNER", "--cap-add", "FSETID")
	} else {
		options += ",ro"
	}
	// The helper reads existing contexts without relabeling the source. Avoid
	// copying image files into a newly created volume before extraction.
	args = append(args, "-v", volume+":"+volumeArchiveMount+":"+options, helper.image,
		"--directory", volumeArchiveMount, "--file", "-", "--numeric-owner")
	if restore {
		args = append(args, "--extract", "--same-owner", "--same-permissions")
	} else {
		args = append(args, "--create", "--format=pax", "--one-file-system", ".")
	}
	cmd := exec.Command(helper.path, args...)
	cmd.Stderr = os.Stderr
	return cmd
}

func (helper volumeArchiveRuntime) backupHomeVolume(volume, archive string) error {
	if _, err := os.Lstat(archive); err == nil {
		return fmt.Errorf("archive %q already exists", archive)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check backup destination: %w", err)
	}
	if err := requireHomeVolume(helper.path, volume); err != nil {
		return err
	}
	if err := helper.requireStoppedVolume(volume); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(archive), ".yolobox-volume-backup-*")
	if err != nil {
		return fmt.Errorf("create backup archive: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	compressed := gzip.NewWriter(file)
	compressed.Comment = volumeArchiveHeader + helper.arch
	command := helper.tarCommand(volume, false)
	output, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open home archive stream: %w", err)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start home archiver: %w", err)
	}
	validationErr := validateVolumeArchive(io.TeeReader(output, compressed))
	if validationErr != nil {
		// Drain any remainder so a runtime helper cannot block on its output.
		_, _ = io.Copy(io.Discard, output)
	}
	if err := command.Wait(); err != nil {
		return fmt.Errorf("archive home volume %q: %w", volume, err)
	}
	if validationErr != nil {
		return fmt.Errorf("validate new backup archive: %w", validationErr)
	}
	if err := helper.requireStoppedVolume(volume); err != nil {
		return err
	}
	if err := compressed.Close(); err != nil {
		return fmt.Errorf("finish backup archive: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync backup archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup archive: %w", err)
	}
	// A same-directory hard link publishes the complete private file without
	// replacing an archive created concurrently at the destination.
	if err := os.Link(file.Name(), archive); err != nil {
		return fmt.Errorf("publish backup archive %q: %w", archive, err)
	}
	return nil
}

func (helper volumeArchiveRuntime) requireStoppedVolume(volume string) error {
	containers, err := helper.runtimeOutput("ps", "--quiet", "--filter", "volume="+volume)
	if err != nil {
		return fmt.Errorf("check for running containers on home volume %q: %w", volume, err)
	}
	if strings.TrimSpace(containers) != "" {
		return fmt.Errorf("home volume %q is in use; stop its containers and keep them stopped during backup", volume)
	}
	return nil
}

func (helper volumeArchiveRuntime) restoreHomeVolume(archive string) (volume string, err error) {
	// Open nonblocking so an accidentally supplied FIFO never hangs the CLI.
	file, err := os.OpenFile(archive, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("open backup archive: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect backup archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("backup archive %q must be a regular file", archive)
	}
	validatedDigest := sha256.New()
	compressed, err := gzip.NewReader(io.TeeReader(file, validatedDigest))
	if err != nil {
		return "", fmt.Errorf("open gzip backup archive: %w", err)
	}
	defer func() { _ = compressed.Close() }()
	arch, ok := strings.CutPrefix(compressed.Comment, volumeArchiveHeader)
	if !ok || !knownVolumeArchSuffixes[arch] {
		return "", fmt.Errorf("file %q is not a supported yolobox home archive", archive)
	}
	if arch != helper.arch {
		return "", fmt.Errorf("backup architecture %s differs from restore architecture %s; select the matching --platform", arch, helper.arch)
	}
	if err := validateVolumeArchive(compressed); err != nil {
		return "", fmt.Errorf("validate backup archive: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind backup archive: %w", err)
	}
	extractedDigest := sha256.New()
	if err := compressed.Reset(io.TeeReader(file, extractedDigest)); err != nil {
		return "", fmt.Errorf("reopen gzip backup archive: %w", err)
	}
	var volumeID [16]byte
	if _, err := rand.Read(volumeID[:]); err != nil {
		return "", fmt.Errorf("generate restore volume name: %w", err)
	}
	newName := "yolobox-restored-" + hex.EncodeToString(volumeID[:])
	volumes, err := runtimeVolumeNames(helper.path)
	if err != nil {
		return "", fmt.Errorf("list volumes before restore: %w", err)
	}
	if slices.Contains(volumes, newName) {
		return "", fmt.Errorf("restore volume %q already exists; retry to generate a new name", newName)
	}
	if _, err := helper.runtimeOutput("volume", "create", newName); err != nil {
		return "", fmt.Errorf("create restore volume %q: %w", newName, err)
	}
	defer func() {
		if err == nil {
			return
		}
		if _, cleanupErr := helper.runtimeOutput("volume", "rm", newName); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove incomplete restore volume %q: %w", newName, cleanupErr))
		}
	}()

	command := helper.tarCommand(newName, true)
	command.Stdin = compressed
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("extract backup into %q: %w", newName, err)
	}
	// Tar can stop at its end marker. Finish the gzip stream and compare the
	// exact bytes with those validated before creating the new volume.
	if _, err := io.Copy(io.Discard, compressed); err != nil {
		return "", fmt.Errorf("finish reading backup archive: %w", err)
	}
	if !bytes.Equal(validatedDigest.Sum(nil), extractedDigest.Sum(nil)) {
		return "", fmt.Errorf("backup archive %q changed during restore", archive)
	}
	return newName, nil
}

func validateVolumeArchive(source io.Reader) error {
	reader := tar.NewReader(source)
	rootSeen := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if !safeVolumeArchivePath(header.Name) {
			return fmt.Errorf("archive entry has an unsafe path %q", header.Name)
		}
		if !rootSeen {
			if (header.Name != "." && header.Name != "./") || header.Typeflag != tar.TypeDir {
				return fmt.Errorf("archive must begin with the home directory entry")
			}
			rootSeen = true
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeDir, tar.TypeSymlink:
		case tar.TypeLink:
			if !safeVolumeArchivePath(header.Linkname) {
				return fmt.Errorf("archive entry %q has an unsafe hard-link target %q", header.Name, header.Linkname)
			}
		default:
			return fmt.Errorf("unsupported archive entry type %q for %q", header.Typeflag, header.Name)
		}
	}
	if !rootSeen {
		return fmt.Errorf("archive is missing the home directory entry")
	}
	// Consume tar padding and check the gzip trailer when restoring, even if tar
	// has already reached its end marker.
	_, err := io.Copy(io.Discard, source)
	return err
}

func safeVolumeArchivePath(name string) bool {
	return name != "" && !strings.HasPrefix(name, "/") && !slices.Contains(strings.Split(name, "/"), "..")
}
