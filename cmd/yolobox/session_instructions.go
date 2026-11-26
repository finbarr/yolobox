package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// The base64 payload must fit in a single Linux process argument.
const maxSessionInstructionsBytes = 64 * 1024

func encodeSessionInstructions(path, projectDir string) (string, error) {
	resolved, err := resolvePath(path, projectDir)
	if err != nil {
		return "", fmt.Errorf("resolve --instructions file: %w", err)
	}
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(projectDir, resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("read --instructions file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("--instructions file %q must be a regular file", path)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("read --instructions file: %w", err)
	}
	defer func() { _ = file.Close() }()
	contents, err := io.ReadAll(io.LimitReader(file, maxSessionInstructionsBytes+1))
	if err != nil {
		return "", fmt.Errorf("read --instructions file: %w", err)
	}
	if len(contents) > maxSessionInstructionsBytes {
		return "", fmt.Errorf("--instructions file %q exceeds 64 KiB", path)
	}
	if !utf8.Valid(contents) || bytes.ContainsRune(contents, '\x00') || len(bytes.TrimSpace(contents)) == 0 {
		return "", fmt.Errorf("--instructions file %q must contain nonempty UTF-8 text", path)
	}
	return base64.StdEncoding.EncodeToString(contents), nil
}

func validateSessionInstructionsArgs(tool string, args []string) error {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		name, value, hasValue := strings.Cut(arg, "=")
		if tool == "claude" {
			switch name {
			case "--continue", "-c", "--resume", "-r", "--from-pr", "--fork-session":
				return fmt.Errorf("--instructions requires a fresh Claude session")
			case "--append-system-prompt-file":
				return fmt.Errorf("use either --instructions or Claude's --append-system-prompt-file")
			}
			continue
		}
		if arg == "resume" || arg == "fork" {
			return fmt.Errorf("--instructions requires a fresh Codex session")
		}
		if name == "--remote" {
			return fmt.Errorf("--instructions requires the Codex session to run inside the box")
		}
		// A value such as a model or profile named "resume" is not a subcommand.
		switch name {
		case "-c", "--config", "--disable", "--enable", "-m", "--model", "-p", "--profile", "-C", "--cd", "-i", "--image", "--add-dir", "--local-provider":
			if !hasValue && index+1 < len(args) {
				index++
				value = args[index]
			}
		}
		key, setting, _ := strings.Cut(value, "=")
		disablesHooks := name == "--disable" && (value == "hooks" || value == "codex_hooks")
		if (name == "-c" || name == "--config") && strings.TrimSpace(setting) == "false" {
			key = strings.TrimSpace(key)
			disablesHooks = key == "features.hooks" || key == "features.codex_hooks"
		}
		if disablesHooks {
			return fmt.Errorf("--instructions requires Codex session hooks to be enabled")
		}
	}
	return nil
}
