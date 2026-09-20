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
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
)

type boundedMediaTestReader struct {
	remaining int64
	maxRead   int
	cancel    context.CancelFunc
}

func (r *boundedMediaTestReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	clear(p[:n])
	r.remaining -= int64(n)
	if r.cancel != nil {
		r.cancel()
	}
	return n, nil
}
func assertNoMediaTemps(t *testing.T, dir string) {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary files=%v err=%v", files, err)
	}
}

func TestCoreMediaSnapshotStreamingCancellationAndLength(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	for _, tc := range []struct {
		name         string
		size, actual int64
		cancel       bool
	}{
		{"large", 200000, 200000, false},
		{"short", 200000, 199999, false},
		{"long", 200000, 200001, false},
		{"cancel", 200000, 200000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := &boundedMediaTestReader{remaining: tc.actual}
			if tc.cancel {
				reader.cancel = cancel
			}
			snapshot, err := snapshotCoreMediaReader(ctx, reader, tc.size)
			if reader.maxRead > 64<<10 {
				t.Fatalf("read buffer=%d", reader.maxRead)
			}
			if tc.name == "large" {
				if err != nil {
					t.Fatal(err)
				}
				name := snapshot.file.Name()
				snapshot.Close()
				if _, err := snapshot.file.Stat(); err == nil {
					t.Fatal("snapshot descriptor remains open")
				}
				if _, err := os.Stat(name); !os.IsNotExist(err) {
					t.Fatalf("snapshot remains: %v", err)
				}
			} else if err == nil {
				snapshot.Close()
				t.Fatal("accepted invalid snapshot")
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel=%v", err)
			}
			assertNoMediaTemps(t, temp)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshotCoreMedia(ctx, "does-not-exist"); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel=%v", err)
	}
	assertNoMediaTemps(t, temp)
}

func TestCoreMediaInstallStreamsLargeSnapshotAndCleansAllResponses(t *testing.T) {
	dir := t.TempDir()
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	path := filepath.Join(dir, "media")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(catalog.MaxCoreMediaBytes)
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	file.Close()
	hash := sha256.New()
	io.Copy(hash, &boundedMediaTestReader{remaining: size})
	id := hex.EncodeToString(hash.Sum(nil))
	for _, mode := range []string{"ok", "digest", "size", "redirect", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/api/v1/core-media" || r.ContentLength != size || len(r.TransferEncoding) != 0 {
					t.Errorf("request=%s %s length=%d transfer=%v", r.Method, r.URL, r.ContentLength, r.TransferEncoding)
				}
				h := sha256.New()
				n, err := io.CopyBuffer(h, r.Body, make([]byte, 64<<10))
				if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != id {
					t.Errorf("upload=%d err=%v", n, err)
				}
				if mode == "disconnect" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				if mode == "redirect" {
					w.Header().Set("Location", "/redirect")
					w.WriteHeader(307)
					return
				}
				resultID, resultSize := id, size
				if mode == "digest" {
					resultID = strings.Repeat("a", 64)
				}
				if mode == "size" {
					resultSize--
				}
				json.NewEncoder(w).Encode(map[string]any{"media_id": resultID, "size": resultSize})
			}))
			defer server.Close()
			result := runCoreLibraryCommand(context.Background(), server.URL, []string{"core-media-install", path})
			if (result.err == nil) != (mode == "ok") || calls.Load() != 1 {
				t.Fatalf("result=%+v calls=%d", result, calls.Load())
			}
			assertNoMediaTemps(t, temp)
		})
	}
	result := runCoreLibraryCommand(context.Background(), ":bad-origin", []string{"core-media-install", path})
	if result.err == nil {
		t.Fatal("invalid origin accepted")
	}
	assertNoMediaTemps(t, temp)
}

func TestCoreMediaInstallCancellationCleansSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 200000)), 0600); err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	done := make(chan commandResult, 1)
	go func() { done <- runCoreLibraryCommand(ctx, server.URL, []string{"core-media-install", path}) }()
	select {
	case result := <-done:
		if result.err == nil || calls.Load() != 1 {
			t.Fatalf("result=%+v calls=%d", result, calls.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	assertNoMediaTemps(t, temp)
}

func TestCoreMediaCapabilitiesCommand(t *testing.T) {
	id := strings.Repeat("a", 64)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v1/core-packages/"+id+"/media-capabilities" || r.ContentLength != 0 {
			t.Errorf("request=%s %s len=%d", r.Method, r.URL, r.ContentLength)
		}
		io.WriteString(w, `{"package_id":"`+id+`","source":"declared-contract","compatibility":"unknown","import_max_bytes":33554432,"media":[]}`)
	}))
	defer server.Close()
	result := runCoreLibraryCommand(context.Background(), server.URL, []string{"core-media-capabilities", id})
	if result.err != nil || calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, calls)
	}
	result = runCoreLibraryCommand(context.Background(), server.URL, []string{"core-media-capabilities", "bad"})
	if result.err == nil || calls != 1 {
		t.Fatalf("bad id result=%+v calls=%d", result, calls)
	}
}
