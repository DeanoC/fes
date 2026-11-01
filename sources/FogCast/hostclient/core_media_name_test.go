package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetCoreEntryMediaNameSendsSelectionBoundRequest(t *testing.T) {
	pkg, media := strings.Repeat("a", 64), strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/library/core-entries/fpga-zx81/media-name" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body["expected_package_id"] != pkg || body["media_id"] != media || body["media_name"] != "MAZE.P" {
			t.Errorf("request body = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(CoreEntry{GameID: "fpga-zx81", PackageID: pkg, MediaID: media, MediaName: "MAZE.P"})
	}))
	defer server.Close()

	got, err := NewClient(server.URL, server.Client()).SetCoreEntryMediaName(context.Background(), "fpga-zx81", pkg, media, "MAZE.P")
	if err != nil || got.GameID != "fpga-zx81" || got.PackageID != pkg || got.MediaID != media || got.MediaName != "MAZE.P" {
		t.Fatalf("SetCoreEntryMediaName() = %+v, %v", got, err)
	}
}

func TestSetCoreEntryMediaNameRejectsMismatchedHostResponse(t *testing.T) {
	pkg, media := strings.Repeat("a", 64), strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(CoreEntry{GameID: "fpga-zx81", PackageID: pkg, MediaID: media, MediaName: "OTHER.P"})
	}))
	defer server.Close()

	if _, err := NewClient(server.URL, server.Client()).SetCoreEntryMediaName(context.Background(), "fpga-zx81", pkg, media, "MAZE.P"); err == nil {
		t.Fatal("accepted a different host media name")
	}
}
