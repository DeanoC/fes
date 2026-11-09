package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrepareLocalCoreEntry(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/library/local-core-entry" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			PackageID string `json:"package_id"`
			GameID    string `json:"game_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PackageID != pkg || req.GameID != "sms-game" {
			t.Errorf("request=%+v err=%v", req, err)
		}
		_ = json.NewEncoder(w).Encode(CoreEntry{GameID: "fpga-game", PackageID: pkg, CoreID: "fes.sms"})
	}))
	defer server.Close()
	entry, err := NewClient(server.URL, server.Client()).PrepareLocalCoreEntry(context.Background(), pkg, "sms-game")
	if err != nil || entry.GameID != "fpga-game" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
}

func TestPrepareLocalCoreEntryAllowsSelectedPackage(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PackageID string `json:"package_id"`
			GameID    string `json:"game_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PackageID != "" || req.GameID != "sms-game" {
			t.Errorf("request=%+v err=%v", req, err)
		}
		_ = json.NewEncoder(w).Encode(CoreEntry{GameID: "fpga-game", PackageID: pkg, CoreID: "fes.sms"})
	}))
	defer server.Close()
	entry, err := NewClient(server.URL, server.Client()).PrepareLocalCoreEntry(context.Background(), "", "sms-game")
	if err != nil || entry.PackageID != pkg {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
}
