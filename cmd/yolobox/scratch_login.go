package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const scratchLoginEnv = "YOLOBOX_SCRATCH_LOGIN"

//go:embed assets/yolobox-scratch-login.py
var scratchLoginScript string

func validateScratchLogin(cfg Config, command []string) error {
	if !cfg.ReuseLogin {
		return nil
	}
	if !cfg.Scratch {
		return errors.New("--reuse-login requires --scratch")
	}
	if len(command) == 0 || (command[0] != "claude" && command[0] != "codex") {
		return errors.New("--reuse-login requires a Claude or Codex launch")
	}

	tool := command[0]
	authKeys := []string{"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_HOME"}
	if tool == "claude" {
		authKeys = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"}
		if cfg.ClaudeConfig {
			return errors.New("--reuse-login cannot be combined with --claude-config for Claude")
		}
	} else if cfg.CodexConfig {
		return errors.New("--reuse-login cannot be combined with --codex-config for Codex")
	}
	for _, arg := range command[1:] {
		if arg == "--" {
			break
		}
		name, _, _ := strings.Cut(arg, "=")
		switch {
		case tool == "claude" && name == "--bare":
			return errors.New("--reuse-login cannot be combined with Claude's --bare mode")
		case tool == "codex" && (name == "--remote" || name == "--oss" || name == "--local-provider"):
			return fmt.Errorf("--reuse-login cannot be combined with Codex's %s", name)
		}
	}

	keys := envFromHostKeySet(cfg.EnvFromHost)
	for _, key := range envKeys(cfg.Env) {
		keys[key] = true
	}
	for _, key := range scratchRuntimeEnvKeys(cfg.RuntimeArgs) {
		keys[key] = true
	}
	for _, key := range append(authKeys, scratchLoginEnv) {
		if keys[key] {
			return fmt.Errorf("--reuse-login cannot be combined with forwarding %s for %s", key, tool)
		}
		if !cfg.NoEnvPassthrough && contains(autoPassthroughEnvVars, key) && os.Getenv(key) != "" {
			return fmt.Errorf("--reuse-login conflicts with the host %s, use --no-env-passthrough for this run", key)
		}
	}
	return nil
}

func scratchRuntimeEnvKeys(args []string) []string {
	var keys []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		spec := ""
		switch {
		case arg == "-e" || arg == "--env":
			if index+1 < len(args) {
				index++
				spec = args[index]
			}
		case strings.HasPrefix(arg, "--env="):
			spec = strings.TrimPrefix(arg, "--env=")
		case strings.HasPrefix(arg, "-e"):
			spec = strings.TrimPrefix(strings.TrimPrefix(arg, "-e"), "=")
		}
		if spec != "" {
			key, _, _ := strings.Cut(spec, "=")
			keys = append(keys, key)
		}
	}
	return keys
}

// prepareScratchLogin reads only the requested harness's current access token
// in a separate, network-isolated helper. The user-selected project image and
// eventual agent command never receive the source home volume.
func prepareScratchLogin(cfg Config, command []string) (string, error) {
	if err := validateScratchLogin(cfg, command); err != nil {
		return "", err
	}
	if !cfg.ReuseLogin {
		return "", nil
	}
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return "", err
	}
	runtimeName := filepath.Base(runtimePath)
	if runtimeName != "docker" && runtimeName != "podman" {
		return "", errors.New("--reuse-login requires Docker or Podman")
	}
	platform, err := effectivePlatform(cfg)
	if err != nil {
		return "", err
	}
	arch, err := resolveContainerArch(cfg)
	if err != nil {
		return "", err
	}
	volume := volumeNameForArch("yolobox-home", arch)
	tool := command[0]
	if err := requireHomeVolume(runtimePath, volume); err != nil {
		return "", fmt.Errorf("cannot reuse the %s login, start and sign in with a regular box first: %w", tool, err)
	}

	args := []string{"run", "--rm", "--network", "none", "--read-only", "--user", "0:0",
		"--cap-drop", "ALL", "--cap-add", "DAC_OVERRIDE", "--security-opt", "no-new-privileges", "--security-opt", "label=disable",
		"--entrypoint", "/usr/bin/python3"}
	args = appendRunFlag(args, "platform", dockerPlatform(platform))
	if runtimeName == "podman" && isRootlessPodman(cfg.Runtime) {
		args = append(args, "--userns=keep-id:uid=1000,gid=1000")
	}
	args = append(args, "-v", volume+":/loan-home:ro,nocopy", defaultConfig().Image,
		"-I", "-c", scratchLoginScript, "export", tool)

	helper := exec.Command(runtimePath, args...)
	output, err := helper.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
			return "", fmt.Errorf("no valid cached %s access token in %s, open a regular %s box to sign in or refresh the login first", tool, volume, tool)
		}
		// Never surface output from a subprocess that accesses credential files.
		return "", fmt.Errorf("could not read the cached %s login with %s: %w", tool, runtimeName, err)
	}
	output = bytes.TrimSpace(output)
	var response struct {
		Tool string `json:"tool"`
	}
	if len(output) > 20*1024 || json.Unmarshal(output, &response) != nil || response.Tool != tool {
		return "", errors.New("invalid response from the scratch login helper")
	}
	return string(output), nil
}
