package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"
)

const maxOpenURLBytes = 8 * 1024

var openHostURL = defaultOpenHostURL

type openBridge struct {
	Endpoint string
	Token    string

	server *http.Server
}

func startOpenBridge(runtimeName string) (*openBridge, error) {
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start open bridge: %w", err)
	}

	token, err := randomToken()
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to generate open bridge token: %w", err)
	}

	bridge := &openBridge{
		Endpoint: fmt.Sprintf("http://%s:%d", hostBridgeHostName(runtimeName), listener.Addr().(*net.TCPAddr).Port),
		Token:    token,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/open", bridge.handleOpen)
	bridge.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := bridge.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			warn("Open bridge stopped: %s", err)
		}
	}()

	return bridge, nil
}

func (b *openBridge) Close() {
	if b == nil || b.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = b.server.Shutdown(ctx)
}

func (b *openBridge) handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !b.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer func() { _ = r.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(r.Body, maxOpenURLBytes+1))
	if err != nil {
		http.Error(w, "failed to read open payload", http.StatusBadRequest)
		return
	}
	if len(data) > maxOpenURLBytes {
		http.Error(w, "open URL too large", http.StatusRequestEntityTooLarge)
		return
	}

	openURL := strings.TrimSpace(string(data))
	if err := validateOpenURL(openURL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := openHostURL(openURL); err != nil {
		http.Error(w, "failed to open URL on host", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (b *openBridge) authorized(r *http.Request) bool {
	got := r.Header.Get("X-Yolobox-Open-Token")
	if b.Token == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(b.Token)) == 1
}

func validateOpenURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("open URL is required")
	}
	if strings.ContainsAny(raw, "\r\n\t") {
		return fmt.Errorf("open URL must not contain control whitespace")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return fmt.Errorf("invalid open URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("open bridge only supports http and https URLs")
	}
	if parsed.Host == "" {
		return fmt.Errorf("open URL must include a host")
	}
	return nil
}

func defaultOpenHostURL(openURL string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", openURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", openURL)
	default:
		cmd = exec.Command("xdg-open", openURL)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
