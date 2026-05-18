package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteBackendClientEnsuresAndReleasesMachine(t *testing.T) {
	token := "test-token"
	var ensured bool
	var released bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/ensure":
			var req remoteBackendEnsureRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode ensure request: %v", err)
			}
			if req.Name != "foo" || req.Workspace != "app" {
				t.Fatalf("unexpected ensure request: %+v", req)
			}
			ensured = true
			_ = json.NewEncoder(w).Encode(remoteBackendMachineResponse{
				Machine: remoteMachine{
					Name:              req.Name,
					Provider:          "ignored-by-client",
					ProviderID:        "host-a",
					PublicIPv4:        "203.0.113.10",
					SSHUser:           "root",
					BootstrapComplete: true,
				},
				Status: "leased",
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/machines/foo":
			released = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	projectDir := t.TempDir()
	binDir := t.TempDir()
	for _, name := range []string{"ssh", "rsync"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatalf("failed to write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)

	cfg := defaultConfig()
	cfg.Remote.BackendURL = ts.URL
	cfg.Remote.BackendToken = token
	machine, err := ensureRemoteBackendMachine(cfg, projectDir, remoteProvisionOptions{Name: "foo", Workspace: "app"})
	if err != nil {
		t.Fatalf("ensureRemoteBackendMachine failed: %v", err)
	}
	if !ensured {
		t.Fatal("expected backend ensure request")
	}
	if machine.Provider != remoteProviderBackend || machine.ProviderID != "host-a" || machine.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("unexpected backend machine: %+v", machine)
	}
	if machine.BackendURL != ts.URL {
		t.Fatalf("expected backend URL %q, got %q", ts.URL, machine.BackendURL)
	}
	if err := releaseRemoteBackendMachine(cfg, machine); err != nil {
		t.Fatalf("releaseRemoteBackendMachine failed: %v", err)
	}
	if !released {
		t.Fatal("expected backend release request")
	}
}

func TestRemoteBackendClientReportsUnauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer ts.Close()

	binDir := t.TempDir()
	for _, name := range []string{"ssh", "rsync"} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatalf("failed to write fake %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)

	cfg := defaultConfig()
	cfg.Remote.BackendURL = ts.URL
	cfg.Remote.BackendToken = "wrong-token"
	_, err := ensureRemoteBackendMachine(cfg, t.TempDir(), remoteProvisionOptions{Name: "foo", Workspace: "app"})
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("expected unauthorized backend error, got %v", err)
	}
}
