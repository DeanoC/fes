package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A shelf change writes on the kit's own storage and takes longer than an
// ordinary request (27 s for a new disk on Kit B), so the shelf calls must
// not use the caller's short timeout.
func TestAtariStShelfChangesOutlastTheOrdinaryTimeout(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		switch r.URL.Path {
		case "/api/v1/library/atari-st-shelf":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(AtariStShelf{KeptKnown: true, Disks: []AtariStShelfDisk{}})
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(AtariStShelfDisk{GameID: "fpga-work", Title: "work", PackageID: pkg,
				MediaID: strings.Repeat("b", 64), Size: 737280, Made: true, Kept: ShelfKeptNone})
		case "/api/v1/library/atari-st-shelf/fpga-work/delete":
			_ = json.NewEncoder(w).Encode(map[string]string{"game_id": "fpga-work"})
		case "/api/v1/health":
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	short := server.Client()
	short.Timeout = 50 * time.Millisecond
	client := NewClient(server.URL, short)
	ctx := context.Background()
	if _, err := client.AtariStShelf(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := client.NewAtariStShelfDisk(ctx, "work", pkg, 0, 0, 0); err != nil {
		t.Fatalf("new disk: %v", err)
	}
	if err := client.DeleteAtariStShelfDisk(ctx, "fpga-work", ""); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := client.Health(ctx); err == nil {
		t.Fatal("an ordinary request kept the caller's short timeout before, and should still")
	}
}
