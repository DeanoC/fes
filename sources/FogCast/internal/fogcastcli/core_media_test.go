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
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
)

func TestCoreMediaCommandsUseHostShapes(t *testing.T) {
	pkg, id := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, tc := range []struct {
		args         []string
		method, path string
		body         map[string]any
	}{
		{[]string{"core-entry", "Core", pkg}, "POST", "/api/v1/library/core-entries", map[string]any{"title": "Core", "package_id": pkg}},
		{[]string{"core-entry", "Core", pkg, "blob", id}, "POST", "/api/v1/library/core-entries", map[string]any{"title": "Core", "package_id": pkg, "media_role": "blob", "media_id": id}},
		{[]string{"core-entry", "Frogger", pkg, "blob", id, "firmware"}, "POST", "/api/v1/library/core-entries", map[string]any{"title": "Frogger", "package_id": pkg, "media_role": "blob", "media_id": id, "firmware_required": true}},
		{[]string{"core-firmware-select", id}, "PUT", "/api/v1/library/firmware", map[string]any{"slot": "firmware", "media_id": id}},
		{[]string{"core-firmware-select", "none"}, "PUT", "/api/v1/library/firmware", map[string]any{"slot": "firmware", "media_id": ""}},
		{[]string{"core-media-select", "core-test", pkg, "none", id}, "PUT", "/api/v1/library/core-entries/core-test/media", map[string]any{"expected_package_id": pkg, "expected_media_id": "", "media_role": "blob", "media_id": id}},
		{[]string{"core-media-select", "core-test", pkg, id, "none"}, "PUT", "/api/v1/library/core-entries/core-test/media", map[string]any{"expected_package_id": pkg, "expected_media_id": id, "media_role": "", "media_id": ""}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.Method != tc.method || r.URL.Path != tc.path || !reflect.DeepEqual(body, tc.body) || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("request=%s %s body=%v want=%v", r.Method, r.URL, body, tc.body)
				}
				io.WriteString(w, `{"game_id":"core-test"}`)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			args := append([]string{"--api", server.URL, "--json"}, tc.args...)
			if code := Run(context.Background(), args, &out, &stderr, nil); code != 0 || calls != 1 {
				t.Fatalf("code=%d calls=%d stderr=%s out=%s", code, calls, &stderr, &out)
			}
		})
	}
}

func TestCoreMediaCommandArguments(t *testing.T) {
	for _, name := range []string{"core-entry", "core-media-install", "core-media-select", "core-media-capabilities", "core-firmware-select"} {
		for n := 1; n <= 7; n++ {
			args := []string{name}
			for len(args) < n {
				args = append(args, "x")
			}
			want := name == "core-entry" && (n == 3 || n == 4 || n == 5 || n == 6) ||
				(name == "core-media-install" || name == "core-media-capabilities" || name == "core-firmware-select") && n == 2 ||
				name == "core-media-select" && n == 5
			if validCommand(args) != want || !coreLibraryCommand(name) {
				t.Fatalf("args=%v valid=%v", args, validCommand(args))
			}
			if !want {
				var out, stderr bytes.Buffer
				if code := Run(context.Background(), args, &out, &stderr, nil); code == 0 {
					t.Fatalf("accepted %v", args)
				}
			}
		}
		if !strings.Contains(usageText, name+" ") {
			t.Fatalf("usage omits %s", name)
		}
	}
}

func TestCoreMediaInstallVerifiesDigestAndSize(t *testing.T) {
	data := []byte{0, 1, 2, 255}
	path := filepath.Join(t.TempDir(), "media.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	id := hex.EncodeToString(digest[:])
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"exact", `{"media_id":"` + id + `","size":4}`, true},
		{"wrong digest", `{"media_id":"` + strings.Repeat("a", 64) + `","size":4}`, false},
		{"wrong size", `{"media_id":"` + id + `","size":3}`, false},
		{"missing size", `{"media_id":"` + id + `"}`, false},
		{"missing digest", `{"size":4}`, false},
		{"null", `null`, false},
		{"malformed", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil || !bytes.Equal(body, data) || r.ContentLength != 4 || r.Method != "POST" || r.URL.Path != "/api/v1/core-media" || r.Header.Get("Content-Type") != "application/octet-stream" {
					t.Errorf("request=%s %s length=%d body=%v err=%v", r.Method, r.URL, r.ContentLength, body, err)
				}
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(context.Background(), []string{"--api", server.URL, "--json", "core-media-install", path}, &out, &stderr, nil)
			if (code == 0) != tc.want || calls != 1 {
				t.Fatalf("code=%d calls=%d out=%s err=%s", code, calls, &out, &stderr)
			}
		})
	}
}

func TestCoreMediaSnapshotRegularBoundedFile(t *testing.T) {
	dir := t.TempDir()
	for _, size := range []int{0, 1, 16384, 16385, int(catalog.MaxCoreMediaBytes), int(catalog.MaxCoreMediaBytes) + 1} {
		path := filepath.Join(dir, "media")
		content := bytes.Repeat([]byte{42}, size)
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := snapshotCoreMedia(context.Background(), path)
		want := size >= 1 && int64(size) <= catalog.MaxCoreMediaBytes
		if (err == nil) != want {
			t.Fatalf("size=%d err=%v", size, err)
		}
		if snapshot != nil {
			hash := sha256.New()
			n, readErr := io.Copy(hash, snapshot.file)
			digest := sha256.Sum256(content)
			name := snapshot.file.Name()
			info, statErr := snapshot.file.Stat()
			snapshot.Close()
			if readErr != nil || statErr != nil || n != int64(size) || snapshot.size != n || snapshot.mediaID != hex.EncodeToString(digest[:]) || !bytes.Equal(hash.Sum(nil), digest[:]) || info.Mode().Perm() != 0600 {
				t.Fatalf("snapshot size=%d n=%d err=%v stat=%v", size, n, readErr, statErr)
			}
			if _, err := os.Stat(name); !os.IsNotExist(err) {
				t.Fatalf("snapshot not removed: %v", err)
			}
		}
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; io.WriteString(w, "{}") }))
	defer server.Close()
	for _, path := range []string{dir, link, filepath.Join(dir, "missing"), filepath.Join(dir, "media")} {
		result := runCoreLibraryCommand(context.Background(), server.URL, []string{"core-media-install", path})
		if result.err == nil || calls != 0 {
			t.Fatalf("path=%s result=%+v calls=%d", path, result, calls)
		}
	}
}

func TestCoreMediaMutationsNeverFollowRedirectsOrReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	pkg := strings.Repeat("a", 64)
	for _, args := range [][]string{
		{"core-media-install", path},
		{"core-media-select", "core-test", pkg, "none", pkg},
		{"core-entry", "Core", pkg, "blob", pkg},
	} {
		for _, status := range []int{301, 302, 303, 307, 308, 409, 503, 0} {
			calls, redirects := 0, 0
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; io.WriteString(w, "{}") }))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if status == 0 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"code":"STALE_REVISION","message":"refresh"}}`)
			}))
			result := runCoreLibraryCommand(context.Background(), server.URL, args)
			server.Close()
			destination.Close()
			if result.err == nil || calls != 1 || redirects != 0 {
				t.Fatalf("args=%v status=%d result=%+v calls=%d redirects=%d", args, status, result, calls, redirects)
			}
		}
	}
}
