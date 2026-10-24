package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClipboardBridgeCopySizeLimit(t *testing.T) {
	oldWrite := writeHostClipboard
	defer func() { writeHostClipboard = oldWrite }()

	var copied int
	writeHostClipboard = func(text string) error {
		copied = len(text)
		return nil
	}

	const tenMiB = 10 * 1024 * 1024
	bridge := &clipboardBridge{Token: "secret"}

	tests := []struct {
		name       string
		size       int
		wantStatus int
		wantCopied int
	}{
		{"exactly 10 MiB is accepted", tenMiB, http.StatusNoContent, tenMiB},
		{"one byte over 10 MiB is rejected", tenMiB + 1, http.StatusRequestEntityTooLarge, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copied = 0
			req := httptest.NewRequest(http.MethodPost, "/copy", strings.NewReader(strings.Repeat("a", tt.size)))
			req.Header.Set("X-Yolobox-Clipboard-Token", "secret")
			rec := httptest.NewRecorder()
			bridge.handleCopy(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if copied != tt.wantCopied {
				t.Fatalf("expected %d bytes copied, got %d", tt.wantCopied, copied)
			}
		})
	}
}
