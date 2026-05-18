package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

type fakeRemoteMachineProvider struct {
	ensured  []remoteMachineProviderRequest
	released []string
}

func (p *fakeRemoteMachineProvider) Name() string {
	return "fake"
}

func (p *fakeRemoteMachineProvider) EnsureMachine(_ context.Context, req remoteMachineProviderRequest) (remoteMachine, string, error) {
	p.ensured = append(p.ensured, req)
	now := time.Now().UTC()
	return remoteMachine{
		Name:       req.Name,
		Provider:   p.Name(),
		ProviderID: "fake-" + req.Name,
		PublicIPv4: "203.0.113.10",
		Region:     "test",
		Size:       "tiny",
		Image:      "ubuntu",
		SSHUser:    req.SSHUser,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, "leased", nil
}

func (p *fakeRemoteMachineProvider) GetMachine(_ context.Context, machine remoteMachine) (remoteMachine, string, error) {
	return machine, "running", nil
}

func (p *fakeRemoteMachineProvider) ReleaseMachine(_ context.Context, machine remoteMachine) error {
	p.released = append(p.released, machine.Name)
	return nil
}

func TestRemoteBackendServerEnsuresPublishesAndDeletes(t *testing.T) {
	store, err := newRemoteBackendStateStore(filepath.Join(t.TempDir(), "backend.json"), "fake")
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeRemoteMachineProvider{}
	server := &remoteBackendServer{token: "secret", provider: provider, store: store}
	ts := httptest.NewServer(server)
	defer ts.Close()

	unauthReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/machines", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := http.DefaultClient.Do(unauthReq); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %s", resp.Status)
	}

	ensureBody := []byte(`{"name":"foo","workspace":"app","ssh_user":"root","repo_url":"git@example.com:repo.git","branch":"main"}`)
	ensureReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/machines/ensure", bytes.NewReader(ensureBody))
	if err != nil {
		t.Fatal(err)
	}
	ensureReq.Header.Set("Authorization", "Bearer secret")
	ensureReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(ensureReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected ensure OK, got %s", resp.Status)
	}
	var ensureResp remoteBackendMachineResponse
	if err := json.NewDecoder(resp.Body).Decode(&ensureResp); err != nil {
		t.Fatal(err)
	}
	if ensureResp.Machine.Provider != remoteProviderBackend || ensureResp.Machine.ProviderID != "fake-foo" {
		t.Fatalf("unexpected backend machine response: %+v", ensureResp.Machine)
	}
	if len(provider.ensured) != 1 || provider.ensured[0].Workspace != "app" || provider.ensured[0].RepoURL == "" {
		t.Fatalf("unexpected ensure request: %+v", provider.ensured)
	}

	session := remoteSession{ID: "foo-app-main", Name: "main", Machine: "foo", Workspace: "foo-app", TmuxSession: "tmux-name"}
	var sessionBuf bytes.Buffer
	if err := json.NewEncoder(&sessionBuf).Encode(session); err != nil {
		t.Fatal(err)
	}
	putReq, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/sessions/foo-app-main", &sessionBuf)
	if err != nil {
		t.Fatal(err)
	}
	putReq.Header.Set("Authorization", "Bearer secret")
	putReq.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(putReq); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected session put OK, got %s", resp.Status)
	}

	listReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/sessions?machine=foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	listReq.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatal(err)
	}
	var listResp struct {
		Sessions []remoteSession `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Sessions) != 1 || listResp.Sessions[0].ID != "foo-app-main" {
		t.Fatalf("unexpected sessions response: %+v", listResp)
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, ts.URL+"/v1/machines/foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	deleteReq.Header.Set("Authorization", "Bearer secret")
	if resp, err := http.DefaultClient.Do(deleteReq); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected delete no content, got %s", resp.Status)
	}
	if len(provider.released) != 1 || provider.released[0] != "foo" {
		t.Fatalf("expected provider release, got %+v", provider.released)
	}
}
