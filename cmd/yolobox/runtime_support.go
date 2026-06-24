package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var currentUID = os.Getuid

// isAppleContainerPath checks if a resolved runtime binary path is Apple's
// container tool.
func isAppleContainerPath(path string) bool {
	return filepath.Base(path) == "container"
}

// isAppleContainer checks if the resolved runtime is Apple's container tool.
func isAppleContainer(runtime string) bool {
	path, err := resolveRuntime(runtime)
	if err != nil {
		return false
	}
	return isAppleContainerPath(path)
}

// isRootlessPodman returns true when the resolved runtime is podman and the
// current process is not running as root (i.e. rootless mode). Rootless Podman
// uses a user namespace where the host UID maps to root inside the container,
// which breaks bind-mount permissions for non-root container users.
func isRootlessPodman(runtime string) bool {
	path, err := resolveRuntime(runtime)
	if err != nil {
		return false
	}
	return strings.HasSuffix(path, "/podman") && currentUID() != 0
}

// volumeRemoveArgs returns the runtime-appropriate volume removal command.
// Apple's container tool uses `volume delete` and has no force flag.
func volumeRemoveArgs(apple bool, force bool, volumes ...string) []string {
	if apple {
		return append([]string{"volume", "delete"}, volumes...)
	}
	args := []string{"volume", "rm"}
	if force {
		args = append(args, "-f")
	}
	return append(args, volumes...)
}

func persistentVolumeMount(name, target string, rootlessPodman bool) string {
	if !rootlessPodman {
		return name + ":" + target
	}
	// :Z keeps SELinux labels stable across runs; :U migrates existing rootless
	// Podman volumes from subordinate-ID ownership to the keep-id container user.
	return name + ":" + target + ":Z,U"
}

// dirContainsSymlinks reports whether dir contains any symbolic links.
func dirContainsSymlinks(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			return true
		}
		if e.IsDir() && dirContainsSymlinks(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

// stageDirResolvingSymlinks copies src to a temp directory under ~/.yolobox/tmp/,
// dereferencing all symlinks so that every entry is a regular file or directory.
// Returns the path to the staged copy of the directory.
func stageDirResolvingSymlinks(src string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	tmpBase := filepath.Join(home, ".yolobox", "tmp")
	if err := os.MkdirAll(tmpBase, 0700); err != nil {
		return "", err
	}
	dst, err := os.MkdirTemp(tmpBase, "staged-*")
	if err != nil {
		return "", err
	}
	if err := copyDirDereferenced(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	return dst, nil
}

// copyDirDereferenced recursively copies src into dst, following all symlinks.
// Broken symlinks are silently skipped.
func copyDirDereferenced(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		info, err := os.Stat(srcPath)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if err := copyDirDereferenced(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}

		data, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

// findDockerSocket returns the Docker socket path to use as a volume mount source.
// On macOS, Docker always runs inside a VM (Docker Desktop or Colima), and the
// socket inside the VM is at /var/run/docker.sock regardless of the host-side path.
// On Linux, Docker runs natively so we return the actual host socket path.
func findDockerSocket() (string, error) {
	const vmInternalSocket = "/var/run/docker.sock"

	if dh := os.Getenv("DOCKER_HOST"); dh != "" {
		if strings.HasPrefix(dh, "unix://") {
			sock := strings.TrimPrefix(dh, "unix://")
			if _, err := os.Stat(sock); err == nil {
				if runtime.GOOS == "darwin" {
					return vmInternalSocket, nil
				}
				return sock, nil
			}
		}
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		"/var/run/docker.sock",
		filepath.Join(home, ".docker", "run", "docker.sock"),
		filepath.Join(home, ".colima", "default", "docker.sock"),
	}

	for _, sock := range candidates {
		if _, err := os.Stat(sock); err == nil {
			if runtime.GOOS == "darwin" {
				return vmInternalSocket, nil
			}
			return sock, nil
		}
	}

	return "", fmt.Errorf("docker socket not found. is Docker running?")
}

// findSSHAgentSocket returns the SSH agent socket path to use as a volume mount source.
// On Linux, SSH_AUTH_SOCK works directly. On macOS, the host's SSH_AUTH_SOCK path
// doesn't exist inside the Docker VM, so we need the VM-internal path instead.
func findSSHAgentSocket() (string, error) {
	if runtime.GOOS != "darwin" {
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return "", fmt.Errorf("ssh auth sock not set")
		}
		return sock, nil
	}

	home, _ := os.UserHomeDir()

	dockerDesktopSock := filepath.Join(home, ".docker", "run", "docker.sock")
	if _, err := os.Stat(dockerDesktopSock); err == nil {
		return "/run/host-services/ssh-auth.sock", nil
	}

	colimaSock := filepath.Join(home, ".colima", "default", "docker.sock")
	if _, err := os.Stat(colimaSock); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "colima", "ssh", "--", "printenv", "SSH_AUTH_SOCK")
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("ssh agent forwarding requires Colima's forwardAgent: true.\nEdit ~/.colima/default/colima.yaml, set forwardAgent: true, then: colima stop && colima start")
		}
		sock := strings.TrimSpace(string(out))
		if sock == "" {
			return "", fmt.Errorf("ssh agent forwarding requires Colima's forwardAgent: true.\nEdit ~/.colima/default/colima.yaml, set forwardAgent: true, then: colima stop && colima start")
		}
		return sock, nil
	}

	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return "/run/host-services/ssh-auth.sock", nil
	}

	return "", fmt.Errorf("could not determine SSH agent socket path for macOS Docker VM")
}

// ensureDockerNetwork creates the yolobox-net Docker network if it doesn't exist.
func ensureDockerNetwork(runtimeName string, networkName string) error {
	runtimePath, err := resolveRuntime(runtimeName)
	if err != nil {
		return err
	}

	if exec.Command(runtimePath, "network", "inspect", networkName).Run() == nil {
		return nil
	}

	cmd := exec.Command(runtimePath, "network", "create", networkName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Race guard: another yolobox instance may have created it between
		// the inspect and the create.
		if strings.Contains(strings.ToLower(string(output)), "exists") {
			return nil
		}
		return fmt.Errorf("failed to create Docker network %q: %s", networkName, strings.TrimSpace(string(output)))
	}
	return nil
}

func appendRunFlag(args []string, flagName, value string) []string {
	if value == "" {
		return args
	}
	return append(args, "--"+flagName, value)
}

func shouldAttachTTY(command []string, explicitInteractive, stdinTTY, stdoutTTY bool) bool {
	if explicitInteractive {
		return true
	}
	if !stdinTTY || !stdoutTTY || len(command) == 0 {
		return false
	}

	cmd := filepath.Base(command[0])
	switch {
	case isToolShortcut(cmd):
		return toolInvocationNeedsTTY(cmd, command[1:])
	case cmd == "bash" || cmd == "sh" || cmd == "zsh" || cmd == "fish":
		return shellInvocationNeedsTTY(command[1:])
	default:
		return false
	}
}

func shellInvocationNeedsTTY(args []string) bool {
	if len(args) == 0 {
		return true
	}
	for _, arg := range args {
		if arg == "--command" {
			return false
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.Contains(arg, "i") {
			return true
		}
		if strings.Contains(arg, "c") {
			return false
		}
	}
	return true
}

func toolInvocationNeedsTTY(tool string, args []string) bool {
	switch tool {
	case "claude", "pi":
		for _, arg := range args {
			if arg == "-p" || arg == "--print" {
				return false
			}
		}
	}
	return true
}
