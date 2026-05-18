package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDigitalOceanProviderCreatesDropletWithDefaultPublicKey(t *testing.T) {
	t.Setenv(digitalOceanAccessTokenEnv, "")
	t.Setenv(digitalOceanTokenEnv, "do-token")
	t.Setenv(digitalOceanFallbackTokenEnv, "")
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	publicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForTests user@example"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519.pub"), []byte(publicKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	var sawCreateKey bool
	var sawCreateDroplet bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer do-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/droplets":
			if r.URL.Query().Get("tag_name") != "yolobox-machine-foo" {
				t.Fatalf("unexpected tag query %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(digitalOceanDropletsResponse{})
		case r.Method == http.MethodGet && r.URL.Path == "/v2/account/keys":
			_ = json.NewEncoder(w).Encode(digitalOceanSSHKeysResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/v2/account/keys":
			sawCreateKey = true
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req["public_key"] != publicKey || !strings.HasPrefix(req["name"], "yolobox-") {
				t.Fatalf("unexpected ssh key request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(digitalOceanSSHKeyResponse{SSHKey: digitalOceanSSHKey{ID: 42, PublicKey: publicKey}})
		case r.Method == http.MethodPost && r.URL.Path == "/v2/droplets":
			sawCreateDroplet = true
			var req digitalOceanCreateDropletRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Name != "yolobox-foo" || req.Region != "nyc3" || req.Size != "s-2vcpu-4gb" || req.Image != "ubuntu-24-04-x64" {
				t.Fatalf("unexpected droplet request: %+v", req)
			}
			if len(req.SSHKeys) != 1 || req.SSHKeys[0].(float64) != 42 {
				t.Fatalf("unexpected ssh keys: %#v", req.SSHKeys)
			}
			if !stringSliceContains(req.Tags, "yolobox") || !stringSliceContains(req.Tags, "yolobox-machine-foo") {
				t.Fatalf("expected yolobox tags, got %#v", req.Tags)
			}
			_ = json.NewEncoder(w).Encode(digitalOceanDropletResponse{Droplet: testDigitalOceanDroplet()})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()
	t.Setenv(digitalOceanAPIURLEnv, ts.URL)

	provider, err := newDigitalOceanProvider(defaultConfig(), remoteProvisionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	provider.pollTimeout = 0
	machine, status, err := provider.EnsureMachine(context.Background(), remoteMachineProviderRequest{Name: "foo", SSHUser: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if !sawCreateKey || !sawCreateDroplet {
		t.Fatal("expected provider to create SSH key and droplet")
	}
	if status != "active" || machine.Provider != remoteProviderDigitalOcean || machine.ProviderID != "123" || machine.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("unexpected machine/status: status=%q machine=%+v", status, machine)
	}
}

func TestDigitalOceanProviderUsesConfiguredSSHKeys(t *testing.T) {
	t.Setenv(digitalOceanAccessTokenEnv, "")
	t.Setenv(digitalOceanTokenEnv, "do-token")
	t.Setenv(digitalOceanFallbackTokenEnv, "")
	var sawAccountKeys bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/droplets":
			_ = json.NewEncoder(w).Encode(digitalOceanDropletsResponse{})
		case r.Method == http.MethodGet && r.URL.Path == "/v2/account/keys":
			sawAccountKeys = true
		case r.Method == http.MethodPost && r.URL.Path == "/v2/droplets":
			var req digitalOceanCreateDropletRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if len(req.SSHKeys) != 2 || req.SSHKeys[0].(float64) != 123 || req.SSHKeys[1].(string) != "aa:bb" {
				t.Fatalf("unexpected ssh keys: %#v", req.SSHKeys)
			}
			_ = json.NewEncoder(w).Encode(digitalOceanDropletResponse{Droplet: testDigitalOceanDroplet()})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()
	t.Setenv(digitalOceanAPIURLEnv, ts.URL)

	cfg := defaultConfig()
	cfg.Remote.DigitalOcean.SSHKeys = []string{"123", "aa:bb"}
	provider, err := newDigitalOceanProvider(cfg, remoteProvisionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := provider.EnsureMachine(context.Background(), remoteMachineProviderRequest{Name: "foo", SSHUser: "root"}); err != nil {
		t.Fatal(err)
	}
	if sawAccountKeys {
		t.Fatal("configured ssh keys should not require account key lookup")
	}
}

func TestRemoteDigitalOceanTokenUsesAccessTokenEnv(t *testing.T) {
	cfg := defaultConfig()
	cfg.Remote.DigitalOcean.Token = ""
	t.Setenv(digitalOceanAccessTokenEnv, "access-token")
	t.Setenv(digitalOceanTokenEnv, "legacy-token")
	t.Setenv(digitalOceanFallbackTokenEnv, "fallback-token")

	if got := remoteDigitalOceanToken(cfg); got != "access-token" {
		t.Fatalf("expected access token env to win, got %q", got)
	}
}

func testDigitalOceanDroplet() digitalOceanDroplet {
	var droplet digitalOceanDroplet
	droplet.ID = 123
	droplet.Name = "yolobox-foo"
	droplet.Status = "active"
	droplet.Region.Slug = "nyc3"
	droplet.SizeSlug = "s-2vcpu-4gb"
	droplet.Image.Slug = "ubuntu-24-04-x64"
	droplet.Tags = []string{"yolobox", "yolobox-machine-foo"}
	droplet.Networks.V4 = append(droplet.Networks.V4, struct {
		IPAddress string `json:"ip_address"`
		Type      string `json:"type"`
	}{IPAddress: "203.0.113.10", Type: "public"})
	return droplet
}
