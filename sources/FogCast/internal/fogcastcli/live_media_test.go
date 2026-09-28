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

	"github.com/DeanoC/FogCast/protocol"
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

// Library launches of fes.computer run as native play; development loads
// stay accepted.
func TestChangeDiskImportsExactImageAndEjectDiskClears(t *testing.T) {
	for _, execution := range []string{"fpga_native", "fpga_development"} {
		t.Run(execution, func(t *testing.T) { testChangeAndEjectDisk(t, execution) })
	}
	if hostMutationClient().Timeout != 0 {
		t.Fatal("disk mutations must not use the short host client timeout")
	}
}

func testChangeAndEjectDisk(t *testing.T, execution string) {
	pkg := strings.Repeat("a", 64)
	mediaID := strings.Repeat("c", 64)
	session := func() map[string]any {
		return map[string]any{
			"id": "host-one", "target": "dev", "state": "active", "execution": execution,
			"core_package": map[string]any{
				"package_id": pkg, "generation": 3,
				"abi":               map[string]any{"id": "fes.computer", "major": 1, "minor": 0},
				"active_interfaces": []map[string]any{{"id": "fes.keyboard.hid", "major": 1, "minor": 0}, {"id": "fes.media.apple2-floppy", "major": 1, "minor": 0}},
				"media_units": []map[string]any{{"unit": 0, "interface": map[string]any{"id": "fes.media.apple2-floppy", "major": 1, "minor": 0},
					"min_bytes": 143360, "max_bytes": 143360, "chunk_bytes": 512, "state": "empty"}},
			},
		}
	}
	var posts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/core-media":
			if r.ContentLength != 143360 {
				t.Fatalf("import size %d", r.ContentLength)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"media_id": mediaID, "size": 143360})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_ = json.NewEncoder(w).Encode(session())
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media":
			posts = append(posts, "change")
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["media_id"] != mediaID || body["name"] != "dos33.dsk" ||
				r.Header.Get("X-FogCast-Core-Generation") != "3" {
				t.Fatalf("body=%v err=%v headers=%v", body, err, r.Header)
			}
			_ = json.NewEncoder(w).Encode(session())
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media/clear":
			posts = append(posts, "clear")
			_ = json.NewEncoder(w).Encode(session())
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	disk := filepath.Join(dir, "dos33.dsk")
	if err := os.WriteFile(disk, make([]byte, 143360), 0600); err != nil {
		t.Fatal(err)
	}
	if result := runLiveMediaCommand(context.Background(), server.URL, []string{"change-disk", disk}); result.err != nil {
		t.Fatalf("change-disk: %v", result.err)
	}
	if result := runLiveMediaCommand(context.Background(), server.URL, []string{"eject-disk"}); result.err != nil {
		t.Fatalf("eject-disk: %v", result.err)
	}
	if len(posts) != 2 || posts[0] != "change" || posts[1] != "clear" {
		t.Fatalf("posts=%v", posts)
	}
	short := filepath.Join(dir, "short.dsk")
	if err := os.WriteFile(short, make([]byte, 1024), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{short, filepath.Join(dir, "prodos.po")} {
		if result := runLiveMediaCommand(context.Background(), server.URL, []string{"change-disk", path}); result.err == nil {
			t.Fatalf("%s accepted", path)
		}
	}
	// A tape command refuses a disk-only session before posting.
	if result := runLiveMediaCommand(context.Background(), server.URL, []string{"eject-tape"}); result.err == nil || len(posts) != 2 {
		t.Fatalf("tape eject posted to a disk session: %v", result.err)
	}
}

func TestChangeDiskDigestUsesTheActiveDisk(t *testing.T) {
	for _, tc := range []struct {
		iface, name string
		size        int64
	}{
		{"fes.media.apple2-floppy", "disk.dsk", protocol.Apple2FloppyBytes},
		{"fes.media.c64-disk", "disk.d64", protocol.C64DiskBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mediaID := strings.Repeat("d", 64)
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				session := map[string]any{
					"id": "host-one", "target": "dev", "state": "active", "execution": "fpga_native",
					"core_package": map[string]any{
						"package_id": strings.Repeat("a", 64), "generation": 3,
						"abi":               map[string]any{"id": "fes.computer", "major": 1, "minor": 0},
						"active_interfaces": []map[string]any{{"id": tc.iface, "major": 1, "minor": 0}},
						"media_units": []map[string]any{{"unit": 0, "interface": map[string]any{"id": tc.iface, "major": 1, "minor": 0},
							"min_bytes": tc.size, "max_bytes": tc.size, "chunk_bytes": 512, "state": "empty"}},
					},
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
					_ = json.NewEncoder(w).Encode(session)
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media":
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					got = body["name"]
					if body["media_id"] != mediaID {
						t.Fatalf("media_id %q", body["media_id"])
					}
					_ = json.NewEncoder(w).Encode(session)
				default:
					t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			if result := runLiveMediaCommand(context.Background(), server.URL, []string{"change-disk", mediaID}); result.err != nil {
				t.Fatal(result.err)
			}
			if got != tc.name {
				t.Fatalf("name %q", got)
			}
		})
	}
}
