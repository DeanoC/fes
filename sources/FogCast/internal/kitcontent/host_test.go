package kitcontent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

func TestLauncherSourceReadsHostContent(t *testing.T) {
	payload := []byte("cart-bytes")
	id := meshcontent.SumSHA256(payload)
	const token = "launcher-secret-token"
	const targetID = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
	var sawAuth, sawTarget bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization") == "Bearer "+token
		sawTarget = r.Header.Get("X-FogCast-Target-ID") == targetID
		if r.URL.Query().Get("id") != id.String() || len(r.URL.Query()) != 1 {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/v1/mesh/content/source":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"advertises":true}`)
		case "/api/v1/mesh/content/object":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"` + server.URL + `","token":"` + token + `","target_id":"` + targetID + `","theme":"sofa","hps_framebuffer":false}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := OpenLauncherSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if !source.Advertises(id) || !sawAuth || !sawTarget {
		t.Fatalf("advertises auth=%v target=%v", sawAuth, sawTarget)
	}
	reader, err := source.Open(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("body %q err %v", got, err)
	}
	missing := meshcontent.SumSHA256([]byte("other"))
	if source.Advertises(missing) {
		t.Fatal("missing id advertised")
	}
}

func TestLauncherSourceMissingFileAndInvalidConfig(t *testing.T) {
	_, err := OpenLauncherSource(filepath.Join(t.TempDir(), "missing.json"))
	if !os.IsNotExist(err) {
		t.Fatalf("missing err %v", err)
	}
	path := filepath.Join(t.TempDir(), "launcher.json")
	const secret = "this-token-must-not-leak"
	if err := os.WriteFile(path, []byte(`{"api":"http://127.0.0.1:1","token":"`+secret+`","target_id":"not-a-uuid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = OpenLauncherSource(path)
	if err == nil || strings.Contains(err.Error(), secret) || os.IsNotExist(err) {
		t.Fatalf("invalid err %v", err)
	}
}

func TestLauncherSourceTransportFailureDoesNotAdvertise(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:1","token":"secret","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := OpenLauncherSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if source.Advertises(meshcontent.SumSHA256([]byte("x"))) {
		t.Fatal("unreachable host advertised content")
	}
}
