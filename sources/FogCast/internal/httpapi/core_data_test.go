package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/corepackage"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

type dataController struct {
	fakeDevelopmentController
	dataCalls  int
	id         string
	gameID     string
	update     protocol.CoreSettingsUpdate
	ataPackage []byte
	ataDisk    []byte
	ataPath    string
	ataID      string
	ataGameID  string
	ataSelect  protocol.AtaDiskSelection
}

func (c *dataController) InspectCoreData(ctx context.Context, n int64, r io.Reader, id, gameID string) (protocol.CoreDataInspection, *protocol.APIError) {
	c.dataCalls++
	c.id = id
	c.gameID = gameID
	return protocol.CoreDataInspection{}, nil
}
func (c *dataController) UpdateCoreSettings(ctx context.Context, n int64, r io.Reader, u protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError) {
	c.update = u
	c.dataCalls++
	return protocol.CoreDataInspection{}, nil
}
func (c *dataController) LoadLibraryCore(ctx context.Context, n int64, r io.Reader, id, gameID string) (protocol.Status, *protocol.APIError) {
	c.id = id
	c.gameID = gameID
	return c.LoadCore(ctx, n, r)
}
func (c *dataController) LoadLibraryCoreWithAtaDisk(ctx context.Context, n int64, r io.Reader, id, gameID, path string,
	disk protocol.AtaDiskSelection) (protocol.Status, *protocol.APIError) {
	c.ataID, c.ataGameID, c.ataPath, c.ataSelect = id, gameID, path, disk
	var err error
	c.ataPackage, err = io.ReadAll(r)
	if err != nil {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "read package failed"}
	}
	c.ataDisk, err = os.ReadFile(path)
	if err != nil {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "read staged ATA disk failed"}
	}
	return c.LoadCore(ctx, n, bytes.NewReader(c.ataPackage))
}

func TestLibraryLoadForwardsGameIdentity(t *testing.T) {
	c := &dataController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c))
	req := httptest.NewRequest(http.MethodPost, "/v1/library/core/load", strings.NewReader("package"))
	req.ContentLength = int64(len("package"))
	req.Header.Set("Authorization", "Bearer bearer")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-FogCast-Package-ID", strings.Repeat("a", 64))
	req.Header.Set("X-FogCast-Game-ID", "ultima-vi")
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if out.Code != http.StatusOK || c.id != strings.Repeat("a", 64) || c.gameID != "ultima-vi" {
		t.Fatalf("load code=%d body=%s package=%q game=%q", out.Code, out.Body, c.id, c.gameID)
	}
}

func TestLibraryLoadStagesAtaDiskAndForwardsOnlyPackageBytes(t *testing.T) {
	const packageBytes = "package archive"
	id, gameID, baseID := strings.Repeat("a", 64), "ultima-vi", strings.Repeat("c", 64)
	cylinders, heads, sectors, ok := protocol.AtaDiskGeometry(protocol.AtaDiskMinBytes)
	if !ok {
		t.Fatal("ATA geometry unavailable")
	}
	disk := protocol.AtaDiskSelection{BaseMediaID: baseID, Size: uint32(protocol.AtaDiskMinBytes), Cylinders: cylinders,
		Heads: heads, SectorsPerTrack: sectors}
	c := &dataController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c))
	diskBytes := bytes.Repeat([]byte{0x5a}, int(disk.Size))
	body := append(append([]byte(nil), diskBytes...), []byte(packageBytes)...)
	req := httptest.NewRequest(http.MethodPost, "/v1/library/core/load", bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	for key, value := range map[string]string{
		"Authorization":                 "Bearer bearer",
		"Content-Type":                  "application/octet-stream",
		"X-FogCast-Package-ID":          id,
		"X-FogCast-Game-ID":             gameID,
		protocol.CorePackageSizeHeader:  fmt.Sprint(len(packageBytes)),
		protocol.AtaDiskSizeHeader:      fmt.Sprint(disk.Size),
		protocol.AtaDiskBaseIDHeader:    baseID,
		protocol.AtaDiskCylindersHeader: fmt.Sprint(cylinders),
		protocol.AtaDiskHeadsHeader:     fmt.Sprint(heads),
		protocol.AtaDiskSectorsHeader:   fmt.Sprint(sectors),
	} {
		req.Header.Set(key, value)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if out.Code != http.StatusOK || c.ataID != id || c.ataGameID != gameID || c.ataPath == "" ||
		!strings.Contains(c.ataPath, "fes-pc386-ata-") || !bytes.Equal(c.ataDisk, diskBytes) ||
		!bytes.Equal(c.ataPackage, []byte(packageBytes)) || !bytes.Equal(c.body, []byte(packageBytes)) ||
		c.size != int64(len(packageBytes)) || c.ataSelect != disk {
		t.Fatalf("load code=%d body=%s staged path=%q id=%q game=%q disk=%q package=%q size=%d selection=%+v",
			out.Code, out.Body, c.ataPath, c.ataID, c.ataGameID, c.ataDisk, c.ataPackage, c.size, c.ataSelect)
	}
	if _, err := os.Stat(c.ataPath); !os.IsNotExist(err) {
		t.Fatalf("staged disk was not removed after dispatch: err=%v", err)
	}
}

func TestCoreSettingsRouteCarriesPc386MachineAndGameIdentity(t *testing.T) {
	c := &dataController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c))
	req := httptest.NewRequest(http.MethodPost, "/v1/library/core/settings", strings.NewReader("package"))
	req.ContentLength = int64(len("package"))
	for key, value := range map[string]string{
		"Authorization":               "Bearer bearer",
		"Content-Type":                "application/octet-stream",
		"X-FogCast-Package-ID":        strings.Repeat("a", 64),
		"X-FogCast-Game-ID":           "ultima-vi",
		"X-FogCast-Expected-Revision": "absent",
		"X-FogCast-PC386-Machine":     `{"preset":3,"frequency":250,"code_profile":1,"turbo":0,"sb_base":576,"sb_irq":5,"sb_dma":1}`,
	} {
		req.Header.Set(key, value)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if out.Code != http.StatusOK || c.update.GameID != "ultima-vi" || c.update.Machine == nil || c.update.Machine.Preset != 3 {
		t.Fatalf("settings code=%d body=%s update=%+v", out.Code, out.Body, c.update)
	}
}
func (c *dataController) LoadComposedCore(ctx context.Context, n int64, r io.Reader, id string) (protocol.Status, *protocol.APIError) {
	c.id = id
	c.dataCalls++
	return protocol.Status{State: protocol.StateActive}, nil
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
		{"/v1/library/core/compose", "", "", 403},
		{"/v1/library/core/compose", grant.Token, "", 200},
		{"/v1/library/core/compose", grant.Token, "?payload_path=/tmp/other", 400},
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
	if c.dataCalls != 3 || c.coreCalls != 0 {
		t.Fatalf("data calls=%d loads=%d", c.dataCalls, c.coreCalls)
	}
}

func TestROMInputRoutesHaveDistinctLaunchAndInspectionLimits(t *testing.T) {
	for _, path := range []string{"/v1/library/core/load", "/v1/development/core", "/v1/development/core/inspect", "/v1/library/core/data/inspect", "/v1/library/core/settings"} {
		limit := int64(corepackage.MaxArchiveSize)
		if path == "/v1/library/core/load" || path == "/v1/development/core" {
			limit = corepackage.MaxROMInputSize
		}
		for _, size := range []int64{limit, limit + 1} {
			t.Run(path+"/"+fmt.Sprint(size), func(t *testing.T) {
				c := &dataController{}
				handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c))
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("sentinel"))
				req.ContentLength = size
				req.Header.Set("Authorization", "Bearer bearer")
				req.Header.Set("Content-Type", "application/octet-stream")
				req.Header.Set("X-FogCast-Package-ID", strings.Repeat("a", 64))
				req.Header.Set("X-FogCast-Expected-Revision", "absent")
				req.Header.Set("X-FogCast-Paddle-Speed", "0")
				out := httptest.NewRecorder()
				handler.ServeHTTP(out, req)
				want := 200
				if size > limit {
					want = 400
				}
				if out.Code != want {
					t.Fatalf("code=%d want=%d body=%s", out.Code, want, out.Body)
				}
				calls := c.coreCalls + c.inspectCalls + c.dataCalls
				if (size == limit) != (calls == 1) {
					t.Fatalf("controller calls=%d", calls)
				}
			})
		}
	}
}
