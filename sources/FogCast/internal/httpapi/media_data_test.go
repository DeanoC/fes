package httpapi_test

import (
	"context"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mediaDataHTTPController struct {
	mediaUnitHTTPController
	libraryCalls, saveCalls int
	library                 protocol.LibraryMediaBinding
}

func (c *mediaDataHTTPController) InsertLibraryMedia(_ context.Context, size int64, body io.Reader, b protocol.LibraryMediaBinding) (protocol.Status, *protocol.APIError) {
	c.libraryCalls++
	c.library = b
	data, _ := io.ReadAll(body)
	if int64(len(data)) != size {
		panic("wrong size")
	}
	return protocol.Status{State: protocol.StateActive}, nil
}
func (c *mediaDataHTTPController) SaveMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError) {
	c.saveCalls++
	return protocol.Status{State: protocol.StateActive}, nil
}
func TestLibraryDiskRoutesRequireLeaseExactBindingAndFraming(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	c := &mediaDataHTTPController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c), httpapi.WithKitLease(manager))
	b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 9}, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
	send := func(path, body string, change func(*http.Request)) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer bearer")
		r.Header.Set(httpapi.KitLeaseHeader, grant.Token)
		b.SetHeaders(r.Header)
		if body != "" {
			r.Header.Set("Content-Type", "application/octet-stream")
		}
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	disk := strings.Repeat("d", 737280)
	if send("/v1/library/media/insert", disk, nil) != 200 || c.libraryCalls != 1 || c.library != b {
		t.Fatal("bound insert")
	}
	if send("/v1/library/media/save", "", nil) != 200 || c.saveCalls != 1 {
		t.Fatal("save")
	}
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Del(httpapi.KitLeaseHeader) }, func(r *http.Request) { r.Header.Add(protocol.MediaGameHeader, b.GameID) }, func(r *http.Request) { r.Header.Del(protocol.MediaBaseHeader) }, func(r *http.Request) { r.ContentLength-- }, func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }} {
		if send("/v1/library/media/insert", disk, change) == 200 {
			t.Fatal("bad library upload admitted")
		}
	}
	if send("/v1/library/media/save", "x", nil) == 200 || c.libraryCalls != 1 || c.saveCalls != 1 {
		t.Fatal("invalid body dispatched")
	}
}
