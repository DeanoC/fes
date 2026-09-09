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

type dataController struct {
	fakeDevelopmentController
	dataCalls int
	id        string
	update    protocol.CoreSettingsUpdate
}

func (c *dataController) InspectCoreData(ctx context.Context, n int64, r io.Reader, id string) (protocol.CoreDataInspection, *protocol.APIError) {
	c.dataCalls++
	c.id = id
	return protocol.CoreDataInspection{}, nil
}
func (c *dataController) UpdateCoreSettings(ctx context.Context, n int64, r io.Reader, u protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError) {
	c.update = u
	c.dataCalls++
	return protocol.CoreDataInspection{}, nil
}
func (c *dataController) LoadLibraryCore(ctx context.Context, n int64, r io.Reader, id string) (protocol.Status, *protocol.APIError) {
	c.id = id
	return c.LoadCore(ctx, n, r)
}
func TestCoreDataRoutesRequireLeaseOnlyForWritesAndRejectRemotePaths(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	c := &dataController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c), httpapi.WithKitLease(manager))
	for _, tc := range []struct {
		path, lease, query string
		code               int
	}{
		{"/v1/library/core/data/inspect", "", "", 200},
		{"/v1/library/core/settings", "", "", 403},
		{"/v1/library/core/load", "", "", 403},
		{"/v1/library/core/settings", grant.Token, "", 200},
		{"/v1/library/core/data/inspect", "", "?data_root=/tmp/other", 400},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path+tc.query, strings.NewReader("package"))
		req.Header.Set("Authorization", "Bearer bearer")
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-FogCast-Package-ID", strings.Repeat("a", 64))
		req.Header.Set("X-FogCast-Expected-Revision", "absent")
		req.Header.Set("X-FogCast-Paddle-Speed", "2")
		req.Header.Set(httpapi.KitLeaseHeader, tc.lease)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.code {
			t.Fatalf("%s code=%d body=%s", tc.path+tc.query, out.Code, out.Body)
		}
	}
	if c.dataCalls != 2 || c.coreCalls != 0 {
		t.Fatalf("data calls=%d loads=%d", c.dataCalls, c.coreCalls)
	}
}
