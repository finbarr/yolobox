package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type remoteTunnelMessage struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

func runRemoteSSHProxy(args []string, projectDir string) error {
	if len(args) != 1 {
		return fmt.Errorf("__remote-ssh-proxy requires a remote name")
	}
	name := strings.ToLower(strings.TrimSpace(args[0]))
	if err := validateRemoteName(name); err != nil {
		return err
	}
	cfg, err := loadConfig(projectDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	header := http.Header{}
	token := remoteAuthToken(cfg)
	if token == "" {
		return fmt.Errorf("remote session token is not configured; run `yolobox login` or set %s", remoteAuthTokenEnv)
	}
	header.Set("Authorization", "Bearer "+token)
	conn, _, err := websocket.Dial(ctx, remoteSSHTunnelURL(cfg, name), &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	readyCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	messageType, data, err := conn.Read(readyCtx)
	if err != nil {
		return fmt.Errorf("open remote SSH tunnel: %w", err)
	}
	if messageType != websocket.MessageText {
		return fmt.Errorf("remote SSH tunnel returned unexpected binary handshake")
	}
	var ready remoteTunnelMessage
	if err := json.Unmarshal(data, &ready); err != nil {
		return fmt.Errorf("decode remote SSH tunnel handshake: %w", err)
	}
	if ready.Type != "ready" {
		if ready.Message == "" {
			ready.Message = "remote SSH tunnel failed"
		}
		return fmt.Errorf("%s", ready.Message)
	}

	errCh := make(chan error, 2)
	var closeOnce sync.Once
	closeTunnel := func() {
		closeOnce.Do(func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		})
	}
	go func() {
		errCh <- copyStdinToTunnel(ctx, conn, closeTunnel)
	}()
	go func() {
		errCh <- copyTunnelToStdout(ctx, conn)
	}()

	err = <-errCh
	closeTunnel()
	if err == nil || err == io.EOF {
		return nil
	}
	return err
}

func copyStdinToTunnel(ctx context.Context, conn *websocket.Conn, closeTunnel func()) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if writeErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			closeTunnel()
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func copyTunnelToStdout(ctx context.Context, conn *websocket.Conn) error {
	for {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return nil
			}
			return err
		}
		switch messageType {
		case websocket.MessageBinary:
			if _, err := os.Stdout.Write(data); err != nil {
				return err
			}
		case websocket.MessageText:
			var message remoteTunnelMessage
			if json.Unmarshal(data, &message) == nil && message.Type == "error" {
				if message.Message == "" {
					message.Message = "remote SSH tunnel failed"
				}
				return fmt.Errorf("%s", message.Message)
			}
		}
	}
}

func remoteSSHTunnelURL(cfg Config, name string) string {
	base := remoteBackendURL(cfg)
	parsed, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/") + "/v1/machines/" + url.PathEscape(name) + "/tunnel/ssh"
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/machines/" + url.PathEscape(name) + "/tunnel/ssh"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
