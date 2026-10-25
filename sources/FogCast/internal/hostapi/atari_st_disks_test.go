package hostapi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type blankAtariAPIService struct {
	coreLibraryAPIService
	calls   int
	created bool
	err     error
	req     fogcast.BlankAtariStRequest
}

func (s *blankAtariAPIService) CreateBlankAtariStDisk(_ context.Context, req fogcast.BlankAtariStRequest) (fogcast.BlankAtariStDisk, error) {
	s.calls++
	s.req = req
	if s.err != nil {
		return fogcast.BlankAtariStDisk{}, s.err
	}
	disk := fogcast.BlankAtariStDisk{
		MediaID: strings.Repeat("ab", 32), Size: 737280, Name: "WORK",
		Tracks: 80, Heads: 2, Sectors: 9, Created: s.created,
	}
	if req.Tracks != 0 {
		disk.Tracks, disk.Heads, disk.Sectors = req.Tracks, req.Heads, req.Sectors
		disk.Size = int64(req.Tracks * req.Heads * req.Sectors * 512)
	}
	if req.Bind {
		disk.Entry = &catalog.CoreEntry{GameID: req.GameID, Title: "Desk", CoreID: "fes.atari-st", PackageID: req.ExpectedPackageID, MediaRole: protocol.DiskRole, MediaID: disk.MediaID}
	}
	return disk, nil
}

func TestAtariStDiskLayoutsAndCreate(t *testing.T) {
	s := &blankAtariAPIService{created: true}
	handler := hostapi.New(s)
	layouts := mediaRequest(handler, "GET", "/api/v1/library/atari-st-disks", "", "")
	if layouts.Code != 200 {
		t.Fatalf("layouts %d %s", layouts.Code, layouts.Body.String())
	}
	var listed struct {
		Layouts []struct {
			Tracks  int   `json:"tracks"`
			Heads   int   `json:"heads"`
			Sectors int   `json:"sectors"`
			Bytes   int64 `json:"bytes"`
		} `json:"layouts"`
	}
	if err := json.Unmarshal(layouts.Body.Bytes(), &listed); err != nil || len(listed.Layouts) != 12 {
		t.Fatalf("layouts %+v %v", listed, err)
	}
	found := false
	for _, layout := range listed.Layouts {
		if layout.Tracks == 80 && layout.Heads == 2 && layout.Sectors == 9 && layout.Bytes == 737280 {
			found = true
		}
	}
	if !found {
		t.Fatal("720 KiB layout missing")
	}

	pkg := strings.Repeat("cd", 32)
	body := `{"name":"work","bind":true,"game_id":"atari-desk","expected_package_id":"` + pkg + `","expected_media_id":""}`
	created := mediaRequest(handler, "POST", "/api/v1/library/atari-st-disks", body, "application/json")
	if created.Code != 201 || s.calls != 1 || !s.req.Bind || s.req.Name != "work" || s.req.GameID != "atari-desk" || s.req.ExpectedPackageID != pkg || s.req.ExpectedMediaID != "" {
		t.Fatalf("create %d calls=%d req=%+v body=%s", created.Code, s.calls, s.req, created.Body.String())
	}
	var disk struct {
		MediaID string             `json:"media_id"`
		Size    int64              `json:"size"`
		Name    string             `json:"name"`
		Created bool               `json:"created"`
		Entry   *catalog.CoreEntry `json:"entry"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &disk); err != nil || !disk.Created || disk.Size != 737280 || disk.Name != "WORK" || disk.Entry == nil || disk.Entry.MediaRole != "disk" || disk.Entry.MediaID != disk.MediaID {
		t.Fatalf("disk %+v %v", disk, err)
	}

	s.created = false
	again := mediaRequest(handler, "POST", "/api/v1/library/atari-st-disks", `{"name":"work"}`, "application/json")
	if again.Code != 200 || s.req.Bind {
		t.Fatalf("store-only %d bind=%v", again.Code, s.req.Bind)
	}
	for _, bad := range []string{
		`{"name":"work","game_id":"atari-desk"}`,
		`{"name":"work","bind":true,"game_id":"atari-desk","expected_package_id":"` + pkg + `"}`,
		`{"name":""}`,
		`{"name":"work","extra":true}`,
	} {
		if response := mediaRequest(handler, "POST", "/api/v1/library/atari-st-disks", bad, "application/json"); response.Code != 400 {
			t.Fatalf("accepted %s -> %d", bad, response.Code)
		}
	}
	if mediaRequest(handler, "POST", "/api/v1/library/atari-st-disks", "", "").Code != 400 {
		t.Fatal("empty body accepted")
	}
	unavailable := mediaRequest(hostapi.New(&coreMediaAPIService{}), "POST", "/api/v1/library/atari-st-disks", `{"name":"work"}`, "application/json")
	if unavailable.Code != 501 {
		t.Fatalf("missing service %d", unavailable.Code)
	}
	s.err = &protocol.APIError{Code: protocol.CodeUnsupportedOperation}
	rejected := mediaRequest(handler, "POST", "/api/v1/library/atari-st-disks", `{"name":"work"}`, "application/json")
	if rejected.Code != 400 || !strings.Contains(rejected.Body.String(), "UNSUPPORTED_OPERATION") {
		t.Fatalf("rejected %d %s", rejected.Code, rejected.Body.String())
	}
}
