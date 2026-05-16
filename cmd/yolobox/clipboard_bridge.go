package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"time"

	hostclipboard "github.com/atotto/clipboard"
)

const maxClipboardBytes = 10 * 1024 * 1024

var (
	writeHostClipboard = hostclipboard.WriteAll
	readHostClipboard  = hostclipboard.ReadAll
)

type clipboardBridge struct {
	Endpoint string
	Token    string

	server *http.Server
}

func startClipboardBridge(runtimeName string) (*clipboardBridge, error) {
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start clipboard bridge: %w", err)
	}

	token, err := randomToken()
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to generate clipboard bridge token: %w", err)
	}

	bridge := &clipboardBridge{
		Endpoint: fmt.Sprintf("http://%s:%d", hostBridgeHostName(runtimeName), listener.Addr().(*net.TCPAddr).Port),
		Token:    token,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/copy", bridge.handleCopy)
	mux.HandleFunc("/paste", bridge.handlePaste)
	bridge.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := bridge.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			warn("Clipboard bridge stopped: %s", err)
		}
	}()

	return bridge, nil
}

func (b *clipboardBridge) Close() {
	if b == nil || b.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = b.server.Shutdown(ctx)
}

func (b *clipboardBridge) handleCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !b.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer func() { _ = r.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(r.Body, maxClipboardBytes+1))
	if err != nil {
		http.Error(w, "failed to read clipboard payload", http.StatusBadRequest)
		return
	}
	if len(data) > maxClipboardBytes {
		http.Error(w, "clipboard payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := writeHostClipboard(string(data)); err != nil {
		http.Error(w, "failed to write host clipboard", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (b *clipboardBridge) handlePaste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !b.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	text, err := readHostClipboard()
	if err != nil {
		http.Error(w, "failed to read host clipboard", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, text)
}

func (b *clipboardBridge) authorized(r *http.Request) bool {
	got := r.Header.Get("X-Yolobox-Clipboard-Token")
	if b.Token == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(b.Token)) == 1
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func clipboardHostName(runtimeName string) string {
	return hostBridgeHostName(runtimeName)
}

func clipboardRuntimeArgs(runtimeName string) []string {
	return hostBridgeRuntimeArgs(runtimeName)
}

func hostBridgeHostName(runtimeName string) string {
	if os.Getenv("YOLOBOX") == "1" {
		if ip := firstNonLoopbackIPv4(); ip != "" {
			return ip
		}
	}

	runtimeBase := resolvedRuntimeName(runtimeName)
	if path, err := resolveRuntime(runtimeName); err == nil {
		runtimeBase = filepath.Base(path)
	}
	switch runtimeBase {
	case "podman", "container":
		return "host.containers.internal"
	default:
		return "host.docker.internal"
	}
}

func firstNonLoopbackIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		return ip4.String()
	}
	return ""
}

func hostBridgeRuntimeArgs(runtimeName string) []string {
	if goruntime.GOOS != "linux" {
		return nil
	}
	path, err := resolveRuntime(runtimeName)
	if err != nil || filepath.Base(path) != "docker" {
		return nil
	}
	return []string{"--add-host", "host.docker.internal:host-gateway"}
}
