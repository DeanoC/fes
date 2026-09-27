package hostclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHardwareClientRefusesMalformedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"machines":[],"session":{"state":"maybe"}}`)
	}))
	defer server.Close()
	if _, err := NewClient(server.URL, server.Client()).Hardware(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid session state") {
		t.Fatalf("malformed active snapshot accepted: %v", err)
	}
}

func TestHardwareClientPreservesStaleSelectionError(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"error":{"code":"STALE_REVISION","message":"core entry selection changed; refresh and retry"}}`)
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	ctx := context.Background()
	if _, err := client.SelectCoreEntryExpansion(ctx, "my-zx81", strings.Repeat("a", 64), "", ""); err == nil || !strings.Contains(err.Error(), "STALE_REVISION") {
		t.Fatalf("conflict lost: %v", err)
	}
	if requests != 1 {
		t.Fatal("draft write was replayed")
	}
	if _, err := client.SelectCoreEntryExpansion(ctx, "my-zx81", "invalid", "", ""); err == nil || requests != 1 {
		t.Fatalf("invalid identity sent to host: %v calls=%d", err, requests)
	}
}
