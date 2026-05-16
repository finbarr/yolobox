package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenBridgeHandler(t *testing.T) {
	oldOpen := openHostURL
	defer func() { openHostURL = oldOpen }()

	var opened string
	openHostURL = func(openURL string) error {
		opened = openURL
		return nil
	}

	bridge := &openBridge{Token: "secret"}
	req := httptest.NewRequest(http.MethodPost, "/open", strings.NewReader("https://example.com/path?q=1"))
	req.Header.Set("X-Yolobox-Open-Token", "secret")
	rec := httptest.NewRecorder()

	bridge.handleOpen(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected open status 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if opened != "https://example.com/path?q=1" {
		t.Fatalf("unexpected opened URL %q", opened)
	}
}

func TestOpenBridgeRejectsBadToken(t *testing.T) {
	bridge := &openBridge{Token: "secret"}
	req := httptest.NewRequest(http.MethodPost, "/open", strings.NewReader("https://example.com"))
	req.Header.Set("X-Yolobox-Open-Token", "wrong")
	rec := httptest.NewRecorder()

	bridge.handleOpen(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestValidateOpenURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "https", raw: "https://example.com/path"},
		{name: "http localhost", raw: "http://localhost:3000"},
		{name: "missing host", raw: "https:///path", wantErr: true},
		{name: "file scheme", raw: "file:///etc/passwd", wantErr: true},
		{name: "shell fragment", raw: "https://example.com\nopen -a Calculator", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOpenURL(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
