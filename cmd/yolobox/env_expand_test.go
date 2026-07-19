package main

import (
	"slices"
	"testing"
)

func TestExpandEnvEntry(t *testing.T) {
	t.Setenv("YOLOBOX_TEST_SRC", "secret-value")
	t.Setenv("YOLOBOX_TEST_OTHER", "other-value")

	tests := []struct {
		name  string
		entry string
		want  string
	}{
		{"dollar var", "A=$YOLOBOX_TEST_SRC", "A=secret-value"},
		{"braced var", "A=${YOLOBOX_TEST_SRC}", "A=secret-value"},
		{"mixed literal and var", "A=prefix-${YOLOBOX_TEST_SRC}-suffix", "A=prefix-secret-value-suffix"},
		{"multiple vars", "A=$YOLOBOX_TEST_SRC:$YOLOBOX_TEST_OTHER", "A=secret-value:other-value"},
		{"unset var expands empty", "A=$YOLOBOX_TEST_UNSET", "A="},
		{"escaped dollar", "A=$$YOLOBOX_TEST_SRC", "A=$YOLOBOX_TEST_SRC"},
		{"no expansion needed", "A=plain", "A=plain"},
		{"key-only passthrough untouched", "YOLOBOX_TEST_SRC", "YOLOBOX_TEST_SRC"},
		{"key never expanded", "$YOLOBOX_TEST_SRC=x", "$YOLOBOX_TEST_SRC=x"},
		{"trailing dollar kept", "A=100$", "A=100$"},
		{"empty value", "A=", "A="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandEnvEntry(tt.entry); got != tt.want {
				t.Errorf("expandEnvEntry(%q) = %q, want %q", tt.entry, got, tt.want)
			}
		})
	}
}

func TestBuildRunArgsExpandsEnvEntries(t *testing.T) {
	t.Setenv("YOLOBOX_TEST_SRC", "secret-value")

	cfg := Config{
		Image: "test-image",
		Env:   []string{"A=$YOLOBOX_TEST_SRC", "YOLOBOX_TEST_SRC"},
	}

	args, _, err := buildRunArgs(cfg, "/test/project", []string{"bash"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(args, "A=secret-value") {
		t.Errorf("expected expanded env entry A=secret-value in args: %v", args)
	}
	if !slices.Contains(args, "YOLOBOX_TEST_SRC") {
		t.Errorf("expected key-only entry YOLOBOX_TEST_SRC passed through unchanged in args: %v", args)
	}
}
