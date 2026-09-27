package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaUnitHTTPController struct {
	fakeDevelopmentController
	inserts, ejects int
	binding         protocol.MediaUnitBinding
}

func (c *mediaUnitHTTPController) InsertMedia(_ context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError) {
	c.inserts++
	c.binding = b
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != size {
		panic("wrong media unit transport")
	}
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}

func (c *mediaUnitHTTPController) EjectMedia(_ context.Context, b protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError) {
	c.ejects++
	c.binding = b
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}

func TestMediaUnitRoutesRequireBindingLeaseAndBounds(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	controller := &mediaUnitHTTPController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(controller), httpapi.WithKitLease(manager))
	binding := protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Unit: 0}
	send := func(path, body string, lease bool, change func(*http.Request)) int {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer bearer")
		if lease {
			r.Header.Set(httpapi.KitLeaseHeader, grant.Token)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/octet-stream")
		}
		binding.SetHeaders(r.Header)
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	disk := strings.Repeat("d", 143360)
	if code := send("/v1/development/insert-media", disk, true, nil); code != 200 || controller.inserts != 1 || controller.binding != binding {
		t.Fatalf("insert %d %+v", code, controller.binding)
	}
	if code := send("/v1/development/eject-media", "", true, nil); code != 200 || controller.ejects != 1 {
		t.Fatalf("eject %d", code)
	}
	for name, tc := range map[string]struct {
		path   string
		body   string
		lease  bool
		change func(*http.Request)
	}{
		"no lease":       {"/v1/development/insert-media", disk, false, nil},
		"no unit":        {"/v1/development/insert-media", disk, true, func(r *http.Request) { r.Header.Del(protocol.MediaUnitHeader) }},
		"unit 8":         {"/v1/development/insert-media", disk, true, func(r *http.Request) { r.Header.Set(protocol.MediaUnitHeader, "8") }},
		"wrong type":     {"/v1/development/insert-media", disk, true, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		"eject body":     {"/v1/development/eject-media", "x", true, nil},
		"no package":     {"/v1/development/eject-media", "", true, func(r *http.Request) { r.Header.Del(protocol.CorePackageIDHeader) }},
		"get not a post": {"/v1/development/insert-media", disk, true, func(r *http.Request) { r.Method = http.MethodGet }},
	} {
		if code := send(tc.path, tc.body, tc.lease, tc.change); code < 400 || controller.inserts != 1 || controller.ejects != 1 {
			t.Fatalf("%s accepted: %d", name, code)
		}
	}
}
