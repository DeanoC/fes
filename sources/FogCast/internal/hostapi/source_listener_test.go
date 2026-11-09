package hostapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

type cassetteSource struct {
	entry catalog.CoreEntry
	data  []byte
	err   error
}

func (s *cassetteSource) LibrarySourceEntries(context.Context) ([]catalog.CoreEntry, error) {
	return []catalog.CoreEntry{s.entry}, s.err
}
func (s *cassetteSource) LibrarySourceContentAdvertises(_ context.Context, id meshcontent.ContentID) (bool, error) {
	return id == meshcontent.SumSHA256(s.data), s.err
}
func (s *cassetteSource) OpenLibrarySourceContent(ctx context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	ok, err := s.LibrarySourceContentAdvertises(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

func TestLibrarySourceHandlerIsReadOnlyAndUsesExistingWireShapes(t *testing.T) {
	payload := []byte("zx81 cassette")
	id := meshcontent.SumSHA256(payload)
	entry := catalog.CoreEntry{GameID: "zx81-game", Title: "Game", CoreID: "fes.zx81", PackageID: strings.Repeat("a", 64), MediaRole: "cassette", MediaID: id.Digest}
	source := &cassetteSource{entry: entry, data: payload}
	server := httptest.NewServer(hostapi.NewLibrarySourceHandler(source))
	defer server.Close()
	get := func(path string, expected int) []byte {
		t.Helper()
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != expected {
			t.Fatalf("GET %s: status=%d body=%q err=%v", path, response.StatusCode, data, err)
		}
		return data
	}
	var list struct {
		Entries []catalog.CoreEntry `json:"entries"`
	}
	if err := json.Unmarshal(get("/api/v1/library/core-entries", 200), &list); err != nil || len(list.Entries) != 1 || list.Entries[0] != entry {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if got := string(get("/api/v1/mesh/content/source?id="+id.String(), 200)); !strings.Contains(got, `"advertises":true`) {
		t.Fatal(got)
	}
	if got := get("/api/v1/mesh/content/object?id="+id.String(), 200); !bytes.Equal(got, payload) {
		t.Fatalf("object=%q", got)
	}
	get("/api/v1/mesh/content/object?id="+meshcontent.SumSHA256([]byte("firmware")).String(), 404)
	get("/api/v1/mesh/content/source?id=sha256:bad", 400)
	get("/api/v1/library/core-entries?extra=1", 400)
	get("/api/v1/session", 404)
	get("/api/v1/games", 404)
	for _, method := range []string{http.MethodPost, http.MethodHead, http.MethodDelete} {
		req, err := http.NewRequest(method, server.URL+"/api/v1/library/core-entries", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s returned %d", method, response.StatusCode)
		}
	}
	source.err = errors.New("private catalog error")
	if body := get("/api/v1/library/core-entries", 500); bytes.Contains(body, []byte("private")) {
		t.Fatalf("leaked internal error: %s", body)
	}
}
