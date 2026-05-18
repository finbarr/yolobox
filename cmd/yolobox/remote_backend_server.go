package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	remoteBackendListenEnv = "YOLOBOX_BACKEND_LISTEN"
	remoteBackendTokenName = "YOLOBOX_BACKEND_TOKEN"
	remoteBackendStateEnv  = "YOLOBOX_BACKEND_STATE"
	remoteBackendStateVer  = 1
)

type remoteBackendServeOptions struct {
	Listen   string
	Token    string
	Provider string
	State    string
}

type remoteBackendServiceState struct {
	Version   int                      `json:"version"`
	Provider  string                   `json:"provider"`
	Machines  map[string]remoteMachine `json:"machines"`
	Sessions  map[string]remoteSession `json:"sessions,omitempty"`
	UpdatedAt time.Time                `json:"updated_at"`
}

type remoteBackendStateStore struct {
	path  string
	mu    sync.Mutex
	state remoteBackendServiceState
}

type remoteBackendServer struct {
	token    string
	provider remoteMachineProvider
	store    *remoteBackendStateStore
}

func runRemoteBackend(args []string, projectDir string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printRemoteBackendUsage()
		return errHelp
	}
	switch args[0] {
	case "serve":
		return runRemoteBackendServe(args[1:], projectDir)
	default:
		return fmt.Errorf("unknown remote backend command %q", args[0])
	}
}

func printRemoteBackendUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox remote backend serve --provider digitalocean --token <token>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "OPTIONS:")
	fmt.Fprintln(os.Stderr, "  --listen <addr>     Listen address (default 127.0.0.1:8787)")
	fmt.Fprintln(os.Stderr, "  --token <token>     Backend API bearer token; or YOLOBOX_BACKEND_TOKEN")
	fmt.Fprintln(os.Stderr, "  --provider <name>   Machine provider adapter, currently digitalocean")
	fmt.Fprintln(os.Stderr, "  --state <path>      Backend state file")
}

func runRemoteBackendServe(args []string, projectDir string) error {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	opts := remoteBackendServeOptions{
		Listen:   firstNonEmpty(os.Getenv(remoteBackendListenEnv), "127.0.0.1:8787"),
		Token:    firstNonEmpty(os.Getenv(remoteBackendTokenName), cfg.Remote.BackendToken),
		Provider: firstNonEmpty(os.Getenv(remoteProviderEnv), cfg.Remote.Provider),
		State:    os.Getenv(remoteBackendStateEnv),
	}
	if opts.State == "" {
		opts.State, err = remoteBackendStatePath()
		if err != nil {
			return err
		}
	}

	fs := flag.NewFlagSet("remote backend serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printRemoteBackendUsage
	fs.StringVar(&opts.Listen, "listen", opts.Listen, "listen address")
	fs.StringVar(&opts.Token, "token", opts.Token, "backend API bearer token")
	fs.StringVar(&opts.Provider, "provider", opts.Provider, "machine provider adapter")
	fs.StringVar(&opts.State, "state", opts.State, "backend state file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected remote backend serve args: %v", fs.Args())
	}
	opts.Provider = strings.ToLower(strings.TrimSpace(opts.Provider))
	opts.Token = strings.TrimSpace(opts.Token)
	if opts.Provider == "" {
		return fmt.Errorf("remote backend serve requires --provider or remote.provider")
	}
	if opts.Token == "" {
		return fmt.Errorf("remote backend serve requires --token or %s", remoteBackendTokenName)
	}
	cfg.Remote.Provider = opts.Provider
	server, err := newRemoteBackendServer(cfg, opts)
	if err != nil {
		return err
	}
	info("Remote backend listening on %s with %s provider", opts.Listen, opts.Provider)
	return http.ListenAndServe(opts.Listen, server)
}

func newRemoteBackendServer(cfg Config, opts remoteBackendServeOptions) (*remoteBackendServer, error) {
	provider, err := newRemoteMachineProvider(cfg, remoteProvisionOptions{Provider: opts.Provider})
	if err != nil {
		return nil, err
	}
	store, err := newRemoteBackendStateStore(opts.State, provider.Name())
	if err != nil {
		return nil, err
	}
	return &remoteBackendServer{
		token:    strings.TrimSpace(opts.Token),
		provider: provider,
		store:    store,
	}, nil
}

func (s *remoteBackendServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
		return
	}
	if !s.authorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/machines/ensure":
		s.handleEnsureMachine(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/machines":
		s.handleListMachines(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/machines/"):
		s.handleMachine(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions":
		s.handleListSessions(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
		s.handleSession(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, "not_found", "endpoint not found")
	}
}

func (s *remoteBackendServer) authorized(r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")) == s.token
}

func (s *remoteBackendServer) handleEnsureMachine(w http.ResponseWriter, r *http.Request) {
	var req remoteBackendEnsureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid JSON request")
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	req.Workspace = effectiveRemoteWorkspace(req.Workspace)
	if err := validateRemoteName(req.Name); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := validateRemoteName(req.Workspace); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid workspace: "+err.Error())
		return
	}
	machine, status, err := s.provider.EnsureMachine(r.Context(), remoteMachineProviderRequest{
		Name:      req.Name,
		Workspace: req.Workspace,
		SSHUser:   firstNonEmpty(req.SSHUser, "root"),
		RepoURL:   req.RepoURL,
		Branch:    req.Branch,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "provider_error", err.Error())
		return
	}
	machine.Name = req.Name
	machine.Provider = remoteProviderBackend
	machine.UpdatedAt = time.Now().UTC()
	if machine.CreatedAt.IsZero() {
		machine.CreatedAt = machine.UpdatedAt
	}
	if err := s.store.update(func(st *remoteBackendServiceState) {
		st.Machines[req.Name] = machine
	}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "state_error", err.Error())
		return
	}
	if status == "" {
		status = "leased"
	}
	writeJSON(w, http.StatusOK, remoteBackendMachineResponse{Machine: machine, Status: status})
}

func (s *remoteBackendServer) handleListMachines(w http.ResponseWriter, r *http.Request) {
	state := s.store.snapshot()
	machines := make([]remoteMachine, 0, len(state.Machines))
	for _, machine := range state.Machines {
		machines = append(machines, machine)
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": machines})
}

func (s *remoteBackendServer) handleMachine(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/machines/"), "/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "machine name is required")
		return
	}
	if err := validateRemoteName(name); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	state := s.store.snapshot()
	machine, ok := state.Machines[name]
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not_found", "machine is not leased")
		return
	}
	switch r.Method {
	case http.MethodGet:
		refreshed, status, err := s.provider.GetMachine(r.Context(), machine)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "provider_error", err.Error())
			return
		}
		refreshed.Name = name
		refreshed.Provider = remoteProviderBackend
		_ = s.store.update(func(st *remoteBackendServiceState) {
			st.Machines[name] = refreshed
		})
		writeJSON(w, http.StatusOK, remoteBackendMachineResponse{Machine: refreshed, Status: status})
	case http.MethodDelete:
		if err := s.provider.ReleaseMachine(context.Background(), machine); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "provider_error", err.Error())
			return
		}
		if err := s.store.update(func(st *remoteBackendServiceState) {
			delete(st.Machines, name)
			for id, session := range st.Sessions {
				if session.Machine == name {
					delete(st.Sessions, id)
				}
			}
		}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "state_error", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *remoteBackendServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	state := s.store.snapshot()
	machineFilter := strings.TrimSpace(r.URL.Query().Get("machine"))
	sessions := make([]remoteSession, 0, len(state.Sessions))
	for _, session := range state.Sessions {
		if machineFilter != "" && session.Machine != machineFilter {
			continue
		}
		sessions = append(sessions, session)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *remoteBackendServer) handleSession(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "session id is required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var session remoteSession
		if err := json.NewDecoder(r.Body).Decode(&session); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid JSON request")
			return
		}
		session.ID = id
		if session.CreatedAt.IsZero() {
			session.CreatedAt = time.Now().UTC()
		}
		session.UpdatedAt = time.Now().UTC()
		if err := s.store.update(func(st *remoteBackendServiceState) {
			st.Sessions[id] = session
		}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "state_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": session})
	case http.MethodDelete:
		if err := s.store.update(func(st *remoteBackendServiceState) {
			delete(st.Sessions, id)
		}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "state_error", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func remoteBackendStatePath() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "yolobox", "backend.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "yolobox", "backend.json"), nil
}

func newRemoteBackendStateStore(path string, provider string) (*remoteBackendStateStore, error) {
	store := &remoteBackendStateStore{path: path}
	if err := store.load(provider); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *remoteBackendStateStore) load(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = remoteBackendServiceState{
		Version:  remoteBackendStateVer,
		Provider: provider,
		Machines: map[string]remoteMachine{},
		Sessions: map[string]remoteSession{},
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return fmt.Errorf("failed to read remote backend state: %w", err)
	}
	if s.state.Machines == nil {
		s.state.Machines = map[string]remoteMachine{}
	}
	if s.state.Sessions == nil {
		s.state.Sessions = map[string]remoteSession{}
	}
	if s.state.Version == 0 {
		s.state.Version = remoteBackendStateVer
	}
	if s.state.Provider == "" {
		s.state.Provider = provider
	}
	return nil
}

func (s *remoteBackendStateStore) update(fn func(*remoteBackendServiceState)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.state)
	s.state.Version = remoteBackendStateVer
	s.state.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.path, data, 0644)
}

func (s *remoteBackendStateStore) snapshot() remoteBackendServiceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	machines := make(map[string]remoteMachine, len(s.state.Machines))
	for name, machine := range s.state.Machines {
		machines[name] = machine
	}
	sessions := make(map[string]remoteSession, len(s.state.Sessions))
	for id, session := range s.state.Sessions {
		sessions[id] = session
	}
	return remoteBackendServiceState{
		Version:   s.state.Version,
		Provider:  s.state.Provider,
		Machines:  machines,
		Sessions:  sessions,
		UpdatedAt: s.state.UpdatedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, id string, message string) {
	writeJSON(w, status, digitalOceanErrorResponse{ID: id, Message: message})
}
