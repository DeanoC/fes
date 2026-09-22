package fogcastcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangeTapeAndEjectTapeCommands(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	mediaID := strings.Repeat("b", 64)
	var posts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "host-one", "target": "dev", "target_id": "", "state": "active", "execution": "fpga_development",
				"core_package": map[string]any{
					"package_id": pkg, "generation": 9,
					"abi":               map[string]any{"id": "fes.simple-computer", "major": 1, "minor": 0},
					"active_interfaces": []map[string]any{{"id": "fes.media.blob", "major": 1, "minor": 0}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media":
			posts = append(posts, "change")
			if r.Header.Get("X-FogCast-Session-ID") != "host-one" || r.Header.Get("X-FogCast-Package-ID") != pkg ||
				r.Header.Get("X-FogCast-Core-Generation") != "9" || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("headers=%v", r.Header)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["media_id"] != mediaID || body["name"] != "tape.p" {
				t.Fatalf("body=%v err=%v", body, err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "host-one", "target": "dev", "state": "active", "execution": "fpga_development",
				"core_package": map[string]any{
					"package_id": pkg, "generation": 9,
					"abi":               map[string]any{"id": "fes.simple-computer", "major": 1, "minor": 0},
					"active_interfaces": []map[string]any{{"id": "fes.media.blob", "major": 1, "minor": 0}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media/clear":
			posts = append(posts, "clear")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "host-one", "target": "dev", "state": "active", "execution": "fpga_development",
				"core_package": map[string]any{
					"package_id": pkg, "generation": 9,
					"abi":               map[string]any{"id": "fes.simple-computer", "major": 1, "minor": 0},
					"active_interfaces": []map[string]any{{"id": "fes.media.blob", "major": 1, "minor": 0}},
				},
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	result := runLiveMediaCommand(context.Background(), server.URL, []string{"change-tape", mediaID})
	if result.err != nil || result.exit != 0 {
		t.Fatalf("change-tape: %+v", result.err)
	}
	result = runLiveMediaCommand(context.Background(), server.URL, []string{"eject-tape"})
	if result.err != nil || result.exit != 0 {
		t.Fatalf("eject-tape: %+v", result.err)
	}
	if len(posts) != 2 || posts[0] != "change" || posts[1] != "clear" {
		t.Fatalf("posts=%v", posts)
	}
}

func TestChangeTapePathRequiresPExtension(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "game.tzx")
	if err := os.WriteFile(bad, []byte{1, 2, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	result := runLiveMediaCommand(context.Background(), "http://127.0.0.1:9", []string{"change-tape", bad})
	if result.err == nil {
		t.Fatal("non-.p path accepted")
	}
}
