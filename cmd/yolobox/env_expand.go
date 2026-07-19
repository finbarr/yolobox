package main

import (
	"os"
	"strings"
)

// expandEnvEntry expands $VAR and ${VAR} references in the value part of a
// KEY=value env entry using the host environment. Unset variables expand to
// an empty string, and $$ produces a literal $. Key-only entries (no "=") are
// returned unchanged so the runtime's own host passthrough applies.
func expandEnvEntry(entry string) string {
	key, value, found := strings.Cut(entry, "=")
	if !found {
		return entry
	}
	expanded := os.Expand(value, func(name string) string {
		if name == "$" {
			return "$"
		}
		return os.Getenv(name)
	})
	return key + "=" + expanded
}
