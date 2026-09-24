package hostapi_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

type meshBytesService struct {
	fakeService
	body []byte
}

func (m *meshBytesService) MeshContentAdvertises(_ context.Context, id meshcontent.ContentID) bool {
	return id == meshcontent.SumSHA256(m.body)
}

func (m *meshBytesService) OpenMeshContent(_ context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	if id != meshcontent.SumSHA256(m.body) {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(m.body)), nil
}

func TestMeshHostContentRoutes(t *testing.T) {
	payload := []byte("bios")
	id := meshcontent.SumSHA256(payload)
	handler := hostapi.New(&meshBytesService{body: payload})
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	source := get("/api/v1/mesh/content/source?id=" + id.String())
	if source.Code != http.StatusOK || !strings.Contains(source.Body.String(), `"advertises":true`) {
		t.Fatalf("source %d %s", source.Code, source.Body.String())
	}
	object := get("/api/v1/mesh/content/object?id=" + id.String())
	if object.Code != http.StatusOK || object.Header().Get("Content-Type") != "application/octet-stream" || object.Body.String() != string(payload) {
		t.Fatalf("object %d %s %q", object.Code, object.Header().Get("Content-Type"), object.Body.String())
	}
	other := meshcontent.SumSHA256([]byte("nope"))
	missing := get("/api/v1/mesh/content/object?id=" + other.String())
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing %d %s", missing.Code, missing.Body.String())
	}
	bad := get("/api/v1/mesh/content/source?id=sha256:zz")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad id %d %s", bad.Code, bad.Body.String())
	}

	unavailable := hostapi.New(&fakeService{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mesh/content/source?id="+id.String(), nil)
	req.Host = "127.0.0.1"
	w := httptest.NewRecorder()
	unavailable.ServeHTTP(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("fake service %d %s", w.Code, w.Body.String())
	}
}
