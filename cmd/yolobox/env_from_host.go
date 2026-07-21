package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envVarNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// hostEnvAliasArgs turns KEY=HOST_VAR entries into runtime "-e KEY=value" args
// by reading HOST_VAR from the host environment. A missing host variable is a
// hard error: aliases exist to replace a more privileged variable, so silently
// skipping one would let automatic passthrough supply the value the alias was
// meant to override. Entries are assumed to be validated by validateEnvFromHost.
func hostEnvAliasArgs(entries []string) ([]string, error) {
	var args []string
	for _, entry := range entries {
		key, hostVar, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		value, ok := os.LookupEnv(hostVar)
		if !ok {
			return nil, fmt.Errorf("env_from_host entry %q requires host environment variable %s, which is not set", entry, hostVar)
		}
		args = append(args, "-e", key+"="+value)
	}
	return args, nil
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

// envFromHostKeySet is envFromHostKeys as a lookup set, used to suppress any
// competing automatic forwarding of the same container variable.
func envFromHostKeySet(entries []string) map[string]bool {
	set := make(map[string]bool, len(entries))
	for _, key := range envFromHostKeys(entries) {
		set[key] = true
	}
	return set
}

// validateEnvFromHostConflicts rejects a container variable that is set by both
// env and env_from_host. Both become "-e" args, so which one wins would depend
// on runtime duplicate-flag precedence rather than on anything yolobox promises.
func validateEnvFromHostConflicts(env []string, entries []string) error {
	aliased := envFromHostKeySet(entries)
	for _, key := range envKeys(env) {
		if aliased[key] {
			return fmt.Errorf("%s is set by both env and env_from_host: pick one", key)
		}
	}
	return nil
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
