package fogcastcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func writeHardDiskFile(t *testing.T, dir string, size int64) string {
	t.Helper()
	path := filepath.Join(dir, "disk.img")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	file.Close()
	return path
}

func TestCoreHardDiskInstallUsesHardDiskRouteAndLimits(t *testing.T) {
	dir, temp := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", temp)
	// Above the 32 MiB general limit, as only the hard-disk route admits.
	size := int64(catalog.MaxCoreMediaBytes) + 512
	path := writeHardDiskFile(t, dir, size)
	hash := sha256.New()
	io.Copy(hash, &boundedMediaTestReader{remaining: size})
	id := hex.EncodeToString(hash.Sum(nil))
	for _, mode := range []string{"ok", "digest", "error"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/api/v1/core-media/hard-disk" || r.ContentLength != size ||
					r.Header.Get("Content-Type") != "application/octet-stream" {
					t.Errorf("request=%s %s length=%d type=%q", r.Method, r.URL, r.ContentLength, r.Header.Get("Content-Type"))
				}
				h := sha256.New()
				if n, err := io.Copy(h, r.Body); err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != id {
					t.Errorf("upload=%d err=%v", n, err)
				}
				switch mode {
				case "error":
					w.WriteHeader(400)
					io.WriteString(w, `{"error":{"code":"BAD_REQUEST","message":"hard-disk import requires a raw image"}}`)
				case "digest":
					json.NewEncoder(w).Encode(map[string]any{"media_id": strings.Repeat("a", 64), "size": size})
				default:
					w.WriteHeader(201)
					json.NewEncoder(w).Encode(map[string]any{"media_id": id, "size": size,
						"geometry": map[string]int{"cylinders": 66, "heads": 16, "sectors_per_track": 63}})
				}
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(context.Background(), []string{"--api", server.URL, "--json", "core-hard-disk-install", path}, &out, &stderr, nil)
			if (code == 0) != (mode == "ok") || calls.Load() != 1 {
				t.Fatalf("code=%d calls=%d stderr=%s", code, calls.Load(), stderr.String())
			}
			if mode == "ok" && !strings.Contains(out.String(), `"geometry":{"cylinders":66,"heads":16,"sectors_per_track":63}`) {
				t.Fatalf("output %s lacks the inferred geometry", out.String())
			}
			assertNoMediaTemps(t, temp)
		})
	}
}

func TestCoreHardDiskInstallRefusesImagesTheDiskDoesNotAdmit(t *testing.T) {
	dir, temp := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", temp)
	for name, size := range map[string]int64{
		"partial sector": protocol.AtaDiskMinBytes + 1,
		"too small":      protocol.AtaDiskMinBytes - 512,
		"too large":      catalog.MaxHardDiskMediaBytes + 512,
	} {
		t.Run(name, func(t *testing.T) {
			path := writeHardDiskFile(t, dir, size)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer server.Close()
			result := runCoreLibraryCommand(context.Background(), server.URL, []string{"core-hard-disk-install", path})
			if result.err == nil || calls.Load() != 0 {
				t.Fatalf("result=%+v calls=%d", result, calls.Load())
			}
			if e, ok := result.err.(*protocol.APIError); !ok || e.Code != protocol.CodeBadRequest || !strings.Contains(e.Message, "512-byte sectors") {
				t.Fatalf("error=%v", result.err)
			}
			assertNoMediaTemps(t, temp)
		})
	}
}

func TestCoreDiskGeometryCommand(t *testing.T) {
	pkg, media := strings.Repeat("a", 64), strings.Repeat("b", 64)
	var got []map[string]any
	reply := func(geometry any) map[string]any {
		entry := map[string]any{"game_id": "dos", "title": "DOS", "core_id": "fes.pc386", "package_id": pkg, "media_id": media, "media_role": "hard-disk"}
		if geometry != nil {
			entry["disk_geometry"] = geometry
		}
		return entry
	}
	var answer map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/api/v1/library/core-entries/dos/disk-geometry" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request=%s %s", r.Method, r.URL)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
		json.NewEncoder(w).Encode(answer)
	}))
	defer server.Close()
	run := func(args ...string) int {
		var out, stderr bytes.Buffer
		return Run(context.Background(), append([]string{"--api", server.URL, "--json"}, args...), &out, &stderr, nil)
	}
	geometry := map[string]any{"cylinders": float64(512), "heads": float64(8), "sectors_per_track": float64(32)}
	answer = reply(geometry)
	if code := run("core-disk-geometry", "dos", pkg, media, "512/8/32"); code != 0 {
		t.Fatalf("override exit %d", code)
	}
	want := map[string]any{"expected_package_id": pkg, "expected_media_id": media, "geometry": geometry}
	if len(got) != 1 || !jsonEqual(got[0], want) {
		t.Fatalf("override body %v", got)
	}
	answer = reply(nil)
	if code := run("core-disk-geometry", "dos", pkg, media, "infer"); code != 0 {
		t.Fatalf("infer exit %d", code)
	}
	if len(got) != 2 || got[1]["geometry"] != nil {
		t.Fatalf("infer body %v", got)
	}
	// The host answering with a different geometry fails the command.
	if code := run("core-disk-geometry", "dos", pkg, media, "512/8/32"); code == 0 {
		t.Fatal("accepted a response without the override")
	}
	calls := len(got)
	for _, bad := range [][]string{
		{"core-disk-geometry", "dos", pkg, media, "512/17/32"},
		{"core-disk-geometry", "dos", pkg, media, "512/8/64"},
		{"core-disk-geometry", "dos", pkg, media, "0/8/32"},
		{"core-disk-geometry", "dos", pkg, media, "512x8x32"},
		{"core-disk-geometry", "dos", pkg, "none", "512/8/32"},
		{"core-disk-geometry", "dos", pkg, media},
	} {
		if code := run(bad...); code == 0 {
			t.Fatalf("accepted %v", bad)
		}
	}
	if len(got) != calls {
		t.Fatalf("invalid geometry reached the host: %v", got[calls:])
	}
}

func TestCoreMediaSelectAcceptsHardDiskRole(t *testing.T) {
	pkg, media := strings.Repeat("a", 64), strings.Repeat("b", 64)
	var body map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"game_id": "dos", "package_id": pkg, "media_id": media, "media_role": "hard-disk"})
	}))
	defer server.Close()
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--api", server.URL, "--json", "core-media-select", "dos", pkg, "none", media, "hard-disk"}, &out, &stderr, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if body["media_role"] != "hard-disk" || body["media_id"] != media {
		t.Fatalf("body %v", body)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
