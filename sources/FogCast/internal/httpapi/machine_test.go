package httpapi_test

import (
	"context"
	"encoding/json"
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

type machineController struct {
	dataController
	machine   *protocol.LibraryMachineBinding
	game      string
	staged    []byte
	stagedSHA string
}

func (c *machineController) LoadLibraryMachineCore(ctx context.Context, n int64, r io.Reader, id string, b protocol.LibraryMachineBinding) (protocol.Status, *protocol.APIError) {
	c.machine, c.id = &b, id
	return protocol.Status{State: protocol.StateActive}, nil
}
func (c *machineController) InspectGameCoreData(ctx context.Context, n int64, r io.Reader, id, game string) (protocol.CoreDataInspection, *protocol.APIError) {
	c.game, c.id = game, id
	return protocol.CoreDataInspection{}, nil
}
func (c *machineController) DiskImage(ctx context.Context, sha string) (protocol.DiskImageStatus, *protocol.APIError) {
	if sha == c.stagedSHA {
		return protocol.DiskImageStatus{SHA256: sha, Size: int64(len(c.staged)), Staged: true}, nil
	}
	return protocol.DiskImageStatus{SHA256: sha}, nil
}
func (c *machineController) StageDiskImage(ctx context.Context, sha string, n int64, r io.Reader) (protocol.DiskImageStatus, *protocol.APIError) {
	data, _ := io.ReadAll(r)
	c.staged, c.stagedSHA = data, sha
	return protocol.DiskImageStatus{SHA256: sha, Size: n, Staged: true}, nil
}

func TestMachineRoutesCarryGameDiskAndSettings(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	c := &machineController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c), httpapi.WithKitLease(manager))
	sha := strings.Repeat("c", 64)
	send := func(path, lease string, body io.Reader, size int64, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, body)
		req.ContentLength = size
		req.Header.Set("Authorization", "Bearer bearer")
		if size > 0 {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		req.Header.Set(httpapi.KitLeaseHeader, lease)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	pkg := map[string]string{"X-FogCast-Package-ID": strings.Repeat("a", 64)}
	with := func(extra map[string]string) map[string]string {
		h := map[string]string{}
		for k, v := range pkg {
			h[k] = v
		}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}

	// The probe reads without a lease; the upload is a kit mutation.
	out := send("/v1/library/disk-images/probe", "", http.NoBody, 0, map[string]string{protocol.DiskImageHeader: sha})
	var status protocol.DiskImageStatus
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &status) != nil || status.Staged || status.SHA256 != sha {
		t.Fatalf("probe %d %s", out.Code, out.Body)
	}
	image := strings.Repeat("x", 1<<20)
	if out := send("/v1/library/disk-images", "", strings.NewReader(image), 1<<20, map[string]string{protocol.DiskImageHeader: sha}); out.Code != 403 {
		t.Fatalf("upload without lease %d", out.Code)
	}
	if out := send("/v1/library/disk-images", grant.Token, strings.NewReader(image[:1000]), 1000, map[string]string{protocol.DiskImageHeader: sha}); out.Code != 400 {
		t.Fatalf("short upload %d", out.Code)
	}
	if out := send("/v1/library/disk-images", grant.Token, strings.NewReader(image), 1<<20, map[string]string{protocol.DiskImageHeader: "nope"}); out.Code != 400 {
		t.Fatalf("bad digest header %d", out.Code)
	}
	if out := send("/v1/library/disk-images", grant.Token, strings.NewReader(image), 1<<20, map[string]string{protocol.DiskImageHeader: sha}); out.Code != 200 || len(c.staged) != 1<<20 {
		t.Fatalf("upload %d %s", out.Code, out.Body)
	}

	// The library load passes the game and disk binding through.
	out = send("/v1/library/core/load", grant.Token, strings.NewReader("package"), 7, with(map[string]string{
		protocol.LibraryGameHeader: "ultima-vi", protocol.DiskImageHeader: sha, protocol.DiskGeometryHeader: "130/16/63"}))
	if out.Code != 200 || c.machine == nil || c.machine.GameID != "ultima-vi" || c.machine.Disk == nil || c.machine.Disk.SHA256 != sha ||
		c.machine.Disk.Geometry != (protocol.DiskGeometry{Cylinders: 130, Heads: 16, SectorsPerTrack: 63}) {
		t.Fatalf("machine load %d %+v", out.Code, c.machine)
	}
	if out := send("/v1/library/core/load", grant.Token, strings.NewReader("package"), 7, with(map[string]string{protocol.DiskImageHeader: sha, protocol.DiskGeometryHeader: "130/16/63"})); out.Code != 400 {
		t.Fatalf("disk without game %d", out.Code)
	}

	// Inspect and settings key the record by game.
	if out := send("/v1/library/core/data/inspect", "", strings.NewReader("package"), 7, with(map[string]string{protocol.LibraryGameHeader: "ultima-vi"})); out.Code != 200 || c.game != "ultima-vi" {
		t.Fatalf("game inspect %d %q", out.Code, c.game)
	}
	settings := with(map[string]string{protocol.LibraryGameHeader: "ultima-vi", "X-FogCast-Expected-Revision": "absent", protocol.MachineSettingsHeader: "3,0,1,0,576,5,1"})
	if out := send("/v1/library/core/settings", grant.Token, strings.NewReader("package"), 7, settings); out.Code != 200 ||
		c.update.Machine == nil || c.update.GameID != "ultima-vi" || c.update.Machine.SBBase != 0x240 {
		t.Fatalf("machine settings %d %+v", out.Code, c.update)
	}
	for name, bad := range map[string]map[string]string{
		"irq-9":       with(map[string]string{protocol.LibraryGameHeader: "ultima-vi", "X-FogCast-Expected-Revision": "absent", protocol.MachineSettingsHeader: "3,0,1,0,576,9,1"}),
		"no-game":     with(map[string]string{"X-FogCast-Expected-Revision": "absent", protocol.MachineSettingsHeader: "3,0,1,0,576,5,1"}),
		"with-paddle": with(map[string]string{protocol.LibraryGameHeader: "ultima-vi", "X-FogCast-Expected-Revision": "absent", protocol.MachineSettingsHeader: "3,0,1,0,576,5,1", "X-FogCast-Paddle-Speed": "1"}),
	} {
		if out := send("/v1/library/core/settings", grant.Token, strings.NewReader("package"), 7, bad); out.Code != 400 {
			t.Errorf("%s settings %d", name, out.Code)
		}
	}
}
