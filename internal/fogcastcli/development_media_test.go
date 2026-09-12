package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

func TestCoreMediaUsesRunningHostAndBindsObservedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coleco.rom")
	if err := os.WriteFile(path, []byte{0, 255, 42}, 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if r.Method != "GET" || r.URL.Path != "/api/v1/session" {
				t.Errorf("unexpected observation: %s %s", r.Method, r.URL.Path)
			}
		} else {
			b, _ := io.ReadAll(r.Body)
			if r.Header.Get("X-FogCast-Target") != "target-a" || r.Header.Get("X-FogCast-Target-ID") != "id-a" {
				t.Errorf("missing observed target binding: %v", r.Header)
			}
			if r.Method != "POST" || r.URL.Path != "/api/v1/session/development-media" || r.Header.Get("X-FogCast-Session-ID") != "host-one" || r.Header.Get("X-FogCast-Package-ID") != strings.Repeat("a", 64) || r.Header.Get("X-FogCast-Core-Generation") != "18446744073709551615" || !bytes.Equal(b, []byte{0, 255, 42}) {
				t.Errorf("wrong upload: %s %s headers=%v body=%v", r.Method, r.URL.Path, r.Header, b)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "host-one", "target": "target-a", "target_id": "id-a", "state": "active", "execution": "fpga_development", "core_package": protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: ^uint64(0), ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}})
	}))
	defer server.Close()
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--api", server.URL, "--json", "core-media", path}, &out, &stderr, func(context.Context, fogcast.Paths) (Service, error) {
		t.Fatal("CLI opened independent service")
		return nil, nil
	})
	if code != 0 || calls != 2 {
		t.Fatalf("exit=%d calls=%d out=%s err=%s", code, calls, &out, &stderr)
	}
}

func TestCoreMediaRejectsInvalidLocalFilesBeforeContactingHost(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	oversized := filepath.Join(dir, "large")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oversized, make([]byte, 16385), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oversized, link); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid media contacted host") }))
	defer server.Close()
	for _, path := range []string{dir, empty, oversized, link, filepath.Join(dir, "missing")} {
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), []string{"--api", server.URL, "--json", "core-media", path}, &out, &stderr, nil); code != 1 {
			t.Fatalf("%s: exit=%d", path, code)
		}
	}
}
