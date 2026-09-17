package fogcastcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func obstructMediaSnapshotRemoval(t *testing.T, dir string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "fogcast-media-snapshot-*"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("snapshot paths=%v err=%v", paths, err)
	}
	path := paths[0]
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "obstruction"), nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCoreMediaSnapshotCloseErrorsAndIdempotence(t *testing.T) {
	for _, mode := range []string{"ok", "close", "remove"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			snapshot, err := snapshotCoreMediaReader(context.Background(), strings.NewReader("abc"), 3)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "close" {
				if err := snapshot.file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "remove" {
				obstructMediaSnapshotRemoval(t, dir)
			}
			first := snapshot.Close()
			second := snapshot.Close()
			if (first == nil) != (mode == "ok") || first != second {
				t.Fatalf("close first=%v second=%v", first, second)
			}
			if mode == "close" && !errors.Is(first, os.ErrClosed) {
				t.Fatalf("lost close error: %v", first)
			}
			if mode != "remove" {
				assertNoMediaTemps(t, dir)
			}
		})
	}
}

type failingSnapshotReader struct{ read func([]byte) (int, error) }

func (r failingSnapshotReader) Read(p []byte) (int, error) { return r.read(p) }

func TestCoreMediaSnapshotJoinsReadAndCleanupErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	cause := errors.New("injected read failure")
	reader := failingSnapshotReader{read: func([]byte) (int, error) {
		obstructMediaSnapshotRemoval(t, dir)
		return 0, cause
	}}
	snapshot, err := snapshotCoreMediaReader(context.Background(), reader, 3)
	if snapshot != nil || !errors.Is(err, cause) || !strings.Contains(err.Error(), "remove media snapshot") {
		t.Fatalf("snapshot=%v error=%v", snapshot, err)
	}
}

func TestCoreMediaCommandCleanupFailurePreservesPriorError(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful upload", true: "failed upload"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			path := filepath.Join(t.TempDir(), "source")
			if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				obstructMediaSnapshotRemoval(t, dir)
				if fail {
					w.WriteHeader(409)
					io.WriteString(w, `{"error":{"code":"STALE_REVISION","message":"refresh"}}`)
					return
				}
				digest := sha256.Sum256([]byte("abc"))
				json.NewEncoder(w).Encode(map[string]any{"media_id": hex.EncodeToString(digest[:]), "size": 3})
			}))
			defer server.Close()
			result := runCoreLibraryCommand(context.Background(), server.URL, []string{"core-media-install", path})
			if result.exit != 1 || result.err == nil || !strings.Contains(result.err.Error(), "remove media snapshot") {
				t.Fatalf("result=%+v", result)
			}
			if fail {
				var apiErr *protocol.APIError
				if !errors.As(result.err, &apiErr) || apiErr.Code != protocol.CodeStaleRevision {
					t.Fatalf("lost prior failure: %v", result.err)
				}
			}
		})
	}
}
