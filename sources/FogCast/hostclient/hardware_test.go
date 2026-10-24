package hostclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHardwareClientDecodesExpansionSelection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		present bool
		value   bool
	}{
		{"omitted", `{"machines":[]}`, false, false},
		{"unsupported", `{"machines":[],"expansion_selection":false}`, true, false},
		{"supported", `{"machines":[],"expansion_selection":true}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			view, err := NewClient(server.URL, server.Client()).Hardware(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if tc.present != (view.ExpansionSelection != nil) || (view.ExpansionSelection != nil && *view.ExpansionSelection != tc.value) {
				t.Fatalf("expansion_selection = %v", view.ExpansionSelection)
			}
		})
	}
}

func TestHardwareClientDecodesShelfComputers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"machines":[],"computers":[{"game_id":"fpga-zx81","title":"ZX81","core_id":"fes.zx81","package_id":"`+strings.Repeat("a", 64)+`","ready":true}]}`)
	}))
	defer server.Close()
	view, err := NewClient(server.URL, server.Client()).Hardware(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Computers) != 1 || view.Computers[0].GameID != "fpga-zx81" || view.Computers[0].CoreID != "fes.zx81" || !view.Computers[0].Ready {
		t.Fatalf("computers %+v", view.Computers)
	}
}

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
