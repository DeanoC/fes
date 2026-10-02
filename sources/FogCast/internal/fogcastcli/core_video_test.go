package fogcastcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/misteross/expansion"
)

func cliVideoArchive(t *testing.T) (string, []byte, catalog.CoreVideoPart) {
	t.Helper()
	cart := bytes.Repeat([]byte{0x33}, 65536)
	asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)),
		Device: expansion.Device, Format: 1, Map: expansion.ColecoVideoMap, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40),
		ShellBuildID: strings.Repeat("d", 32), ShellPackageID: strings.Repeat("a", 64), ShellSHA256: strings.Repeat("e", 64), Slot: expansion.VideoSlot, SlotMajor: 1}, cart)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := asset.Write(&archive); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scanlines.fexp")
	if err := os.WriteFile(path, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path, archive.Bytes(), catalog.CoreVideoPart{PartID: asset.ID, PackageID: asset.Manifest.ShellPackageID, Profile: "scanlines"}
}

func TestVideoCLIUsesOnlyHostSelectionAPIs(t *testing.T) {
	for _, tc := range []struct {
		args                         []string
		method, path, body, response string
	}{
		{[]string{"video-parts"}, "GET", "/api/v1/library/video-parts", "", `[]`},
		{[]string{"video-profile"}, "GET", "/api/v1/library/settings", "", `{"video_profile":"direct"}`},
		{[]string{"video-profile", "scanlines"}, "PATCH", "/api/v1/library/settings", `{"video_profile":"scanlines"}`, `{"video_profile":"scanlines"}`},
		{[]string{"core-video", "core-test"}, "GET", "/api/v1/library/core-entries/core-test/video", "", `{"preferred_profile":"scanlines","effective_profile":"direct","builtin":true}`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if r.Method != tc.method || r.URL.Path != tc.path || string(body) != tc.body {
					t.Errorf("request=%s %s %s", r.Method, r.URL.Path, body)
				}
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			args := append([]string{"--api", server.URL, "--json"}, tc.args...)
			if code := Run(context.Background(), args, &out, &stderr, nil); code != 0 || calls != 1 {
				t.Fatalf("exit=%d calls=%d stdout=%s stderr=%s", code, calls, out.String(), stderr.String())
			}
		})
	}
}

func TestVideoCLIImportRequiresExactArchiveIdentityAndProfile(t *testing.T) {
	path, data, expected := cliVideoArchive(t)
	for _, valid := range []bool{true, false} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			body, _ := io.ReadAll(r.Body)
			if r.Method != "POST" || r.URL.Path != "/api/v1/library/video-parts/scanlines" ||
				r.ContentLength != int64(len(data)) || len(r.TransferEncoding) != 0 || r.Header.Get("Content-Type") != "application/octet-stream" || !bytes.Equal(body, data) {
				t.Errorf("incorrect archive upload: %s %s size=%d", r.Method, r.URL.Path, r.ContentLength)
			}
			imported := expected
			if !valid {
				imported.Profile = "direct"
			}
			_ = json.NewEncoder(w).Encode(imported)
		}))
		result := runCoreLibraryCommand(context.Background(), server.URL, []string{"video-part-install", "scanlines", path})
		server.Close()
		if calls != 1 || (result.err == nil) != valid {
			t.Fatalf("calls=%d valid=%v result=%+v", calls, valid, result)
		}
		if !valid && !strings.Contains(safeCommandError(result.err).Message, "outcome unknown") {
			t.Fatal("different response claimed a confirmed import")
		}
	}
}

func TestVideoCLIImportRejectsOversizedArchiveBeforeHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.fexp")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(catalog.MaxCoreMediaBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	result := runCoreLibraryCommand(context.Background(), ":invalid-origin", []string{"video-part-install", "direct", path})
	if result.err == nil || safeCommandError(result.err).Code != "BAD_REQUEST" {
		t.Fatalf("oversized archive reached HTTP: %+v", result)
	}
}

func TestVideoCLIImportLostResponseIsNotReplayed(t *testing.T) {
	path, _, _ := cliVideoArchive(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}))
	defer server.Close()
	result := runCoreLibraryCommand(context.Background(), server.URL, []string{"video-part-install", "scanlines", path})
	if result.err == nil || calls.Load() != 1 || !strings.Contains(safeCommandError(result.err).Message, "outcome unknown") {
		t.Fatalf("lost response result=%+v calls=%d", result, calls.Load())
	}
}
