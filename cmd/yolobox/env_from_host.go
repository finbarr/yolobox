package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envVarNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// hostEnvAliasArgs turns KEY=HOST_VAR entries into runtime "-e KEY=value" args
// by reading HOST_VAR from the host environment. Entries whose host variable is
// not set are skipped, so the container falls back to whatever the image
// provides. Entries are assumed to be validated by validateEnvFromHost.
func hostEnvAliasArgs(entries []string) []string {
	var args []string
	for _, entry := range entries {
		key, hostVar, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		value, ok := os.LookupEnv(hostVar)
		if !ok {
			continue
		}
		args = append(args, "-e", key+"="+value)
	}
	return args
}

// envFromHostKeys returns the container-side variable names set by the given
// KEY=HOST_VAR entries, never the values.
func envFromHostKeys(entries []string) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if key, _, found := strings.Cut(entry, "="); found && key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func validateEnvFromHost(entries []string) error {
	for _, entry := range entries {
		key, hostVar, found := strings.Cut(entry, "=")
		if !found || key == "" || hostVar == "" {
			return fmt.Errorf("invalid --env-from-host entry %q: expected KEY=HOST_VAR", entry)
		}
		if strings.HasPrefix(hostVar, "$") {
			return fmt.Errorf("invalid --env-from-host entry %q: name the host variable without a leading $ (for example %s=%s)", entry, key, strings.Trim(hostVar, "${}"))
		}
		for _, name := range []string{key, hostVar} {
			if !envVarNamePattern.MatchString(name) {
				return fmt.Errorf("invalid --env-from-host entry %q: %q is not a valid environment variable name", entry, name)
			}
		}
	}
	return nil
}
