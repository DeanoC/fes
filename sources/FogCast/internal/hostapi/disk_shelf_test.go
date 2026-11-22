package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type shelfAPIService struct {
	coreLibraryAPIService
	calls []string
	err   error
	made  fogcast.ShelfNewDisk
}

func (s *shelfAPIService) disk(core, title string) fogcast.ShelfDisk {
	size := int64(737280)
	if core == "fes.pcxt-dos" {
		size = 368640
	}
	return fogcast.ShelfDisk{
		GameID: "work-disk", Title: title, PackageID: strings.Repeat("cd", 32), MediaID: strings.Repeat("ab", 32),
		Size: size, Made: true, Kept: fogcast.ShelfKeptNone,
	}
}

func (s *shelfAPIService) DiskShelf(_ context.Context, core string) (fogcast.DiskShelf, error) {
	s.calls = append(s.calls, "list "+core)
	disk := s.disk(core, "Work disk")
	disk.Kept, disk.KeptRevision, disk.InDrive = fogcast.ShelfKeptChanges, strings.Repeat("ef", 32), true
	return fogcast.DiskShelf{Target: "kit", TargetID: "kit-id", KeptKnown: true, Disks: []fogcast.ShelfDisk{disk}}, s.err
}

func (s *shelfAPIService) NewShelfDisk(_ context.Context, core string, req fogcast.ShelfNewDisk) (fogcast.ShelfDisk, error) {
	s.calls, s.made = append(s.calls, "new "+core), req
	return s.disk(core, req.Title), s.err
}

func (s *shelfAPIService) CopyShelfDisk(_ context.Context, core, game, title string) (fogcast.ShelfDisk, error) {
	s.calls = append(s.calls, "copy "+core+" "+game+" "+title)
	return s.disk(core, title), s.err
}

func (s *shelfAPIService) RevertShelfDisk(_ context.Context, core, game, revision string) (fogcast.ShelfDisk, error) {
	s.calls = append(s.calls, "revert "+core+" "+game+" "+revision)
	return s.disk(core, "Work disk"), s.err
}

func (s *shelfAPIService) RenameShelfDisk(_ context.Context, core, game, revision, title string) (fogcast.ShelfDisk, error) {
	s.calls = append(s.calls, "rename "+core+" "+game+" "+revision+" "+title)
	return s.disk(core, title), s.err
}

func (s *shelfAPIService) DeleteShelfDisk(_ context.Context, core, game, revision string) error {
	s.calls = append(s.calls, "delete "+core+" "+game+" "+revision)
	return s.err
}

func TestDiskShelfRoutes(t *testing.T) {
	s := &shelfAPIService{}
	handler := hostapi.New(s)
	const base = "/api/v1/library/disk-shelves/fes.atari-st"
	pkg, revision := strings.Repeat("cd", 32), strings.Repeat("ef", 32)

	listed := mediaRequest(handler, "GET", base, "", "")
	var shelf struct {
		Target    string `json:"target"`
		KeptKnown bool   `json:"kept_known"`
		Disks     []struct {
			GameID       string `json:"game_id"`
			Title        string `json:"title"`
			Made         bool   `json:"made"`
			Kept         string `json:"kept"`
			KeptRevision string `json:"kept_revision"`
			InDrive      bool   `json:"in_drive"`
			Size         int64  `json:"size"`
		} `json:"disks"`
	}
	if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &shelf) != nil || !shelf.KeptKnown || shelf.Target != "kit" || len(shelf.Disks) != 1 ||
		shelf.Disks[0].Kept != "changes" || shelf.Disks[0].KeptRevision != revision || !shelf.Disks[0].InDrive || !shelf.Disks[0].Made || shelf.Disks[0].Size != 737280 {
		t.Fatalf("list %d %s", listed.Code, listed.Body)
	}
	created := mediaRequest(handler, "POST", base, `{"title":"Work disk 2","package_id":"`+pkg+`","tracks":80,"heads":1,"sectors":9}`, "application/json")
	if created.Code != 201 || s.made != (fogcast.ShelfNewDisk{Title: "Work disk 2", PackageID: pkg, Tracks: 80, Heads: 1, Sectors: 9}) {
		t.Fatalf("new %d %+v %s", created.Code, s.made, created.Body)
	}
	// The core in the path picks the shelf; contents go through as asked.
	freedos := mediaRequest(handler, "POST", "/api/v1/library/disk-shelves/fes.pcxt-dos", `{"title":"Boot","package_id":"`+pkg+`","contents":"freedos"}`, "application/json")
	if freedos.Code != 201 || s.calls[len(s.calls)-1] != "new fes.pcxt-dos" || s.made != (fogcast.ShelfNewDisk{Title: "Boot", PackageID: pkg, Contents: "freedos"}) {
		t.Fatalf("PC/XT new %d %+v %v", freedos.Code, s.made, s.calls)
	}
	for _, tc := range []struct {
		action, body, call string
		code               int
	}{
		{"copy", `{"title":"Spare"}`, "copy fes.atari-st work-disk Spare", 201},
		{"revert", `{"expected_revision":"` + revision + `"}`, "revert fes.atari-st work-disk " + revision, 200},
		{"rename", `{"expected_revision":"","title":"Letters"}`, "rename fes.atari-st work-disk  Letters", 200},
		{"delete", `{"expected_revision":"` + revision + `"}`, "delete fes.atari-st work-disk " + revision, 200},
	} {
		response := mediaRequest(handler, "POST", base+"/work-disk/"+tc.action, tc.body, "application/json")
		if tc.action == "delete" && strings.TrimSpace(response.Body.String()) != `{"game_id":"work-disk"}` {
			t.Fatalf("delete body %s", response.Body)
		}
		if response.Code != tc.code || s.calls[len(s.calls)-1] != tc.call {
			t.Fatalf("%s %d %s calls=%v", tc.action, response.Code, response.Body, s.calls)
		}
	}
	calls := len(s.calls)
	for path, body := range map[string]string{
		base:                                  `{"title":" ","package_id":"` + pkg + `"}`,
		base + "?x=1":                         `{"title":"Work","package_id":"abc"}`,
		base + "/work-disk/copy":              `{"title":"Spare","expected_revision":""}`,
		base + "/work-disk/copy?x=1":          `{}`,
		base + "/work-disk/revert":            `{"expected_revision":"abc"}`,
		base + "/work-disk/revert?x=1":        `{"title":"Spare"}`,
		base + "/work-disk/rename":            `{"title":"Letters"}`,
		base + "/work-disk/delete":            `{}`,
		base + "/-bad/delete":                 `{"expected_revision":""}`,
		base + "/work-disk/delete?x=1":        `{"expected_revision":"","extra":1}`,
		"/api/v1/library/disk-shelves/Fes.ST": `{"title":"Work","package_id":"` + pkg + `"}`,
		"/api/v1/library/disk-shelves/Fes.ST/work-disk/delete": `{"expected_revision":""}`,
	} {
		if response := mediaRequest(handler, "POST", path, body, "application/json"); response.Code != 400 {
			t.Fatalf("accepted %s %s -> %d", path, body, response.Code)
		}
	}
	if len(s.calls) != calls {
		t.Fatalf("invalid requests dispatched: %v", s.calls[calls:])
	}
	if mediaRequest(handler, "POST", base+"/work-disk/lock", `{}`, "application/json").Code == 200 {
		t.Fatal("an unknown shelf action was served")
	}
	if code := mediaRequest(hostapi.New(&coreMediaAPIService{}), "GET", base, "", "").Code; code != 501 {
		t.Fatalf("missing service %d", code)
	}
	// The disk in the drive and changed saved data are conflicts.
	for _, code := range []protocol.ErrorCode{protocol.CodeBusy, protocol.CodeStaleRevision} {
		s.err = &protocol.APIError{Code: code}
		response := mediaRequest(handler, "POST", base+"/work-disk/delete", `{"expected_revision":""}`, "application/json")
		if response.Code != 409 || !strings.Contains(response.Body.String(), string(code)) {
			t.Fatalf("%s -> %d %s", code, response.Code, response.Body)
		}
	}
}

// The host client and the routes agree on every shelf request and answer.
func TestDiskShelfClient(t *testing.T) {
	ctx := context.Background()
	s := &shelfAPIService{}
	server := httptest.NewServer(hostapi.New(s))
	defer server.Close()
	client := hostclient.NewClient(server.URL, server.Client())
	pkg, revision := strings.Repeat("cd", 32), strings.Repeat("ef", 32)

	shelf, err := client.DiskShelf(ctx, hostclient.AtariStShelfCore)
	if err != nil || !shelf.KeptKnown || len(shelf.Disks) != 1 || shelf.Disks[0].KeptRevision != revision || !shelf.Disks[0].InDrive {
		t.Fatalf("list %+v %v", shelf, err)
	}
	disk, err := client.NewShelfDisk(ctx, hostclient.AtariStShelfCore, hostclient.NewShelfDiskRequest{Title: "Work disk 2", PackageID: pkg})
	if err != nil || disk.Title != "Work disk 2" || s.made.Tracks != 80 || s.made.Heads != 2 || s.made.Sectors != 9 {
		t.Fatalf("new %+v %+v %v", disk, s.made, err)
	}
	boot, err := client.NewShelfDisk(ctx, hostclient.PcxtShelfCore, hostclient.NewShelfDiskRequest{Title: "Boot", PackageID: pkg, Contents: hostclient.ShelfContentsFreeDOS})
	if err != nil || boot.Size != 368640 || s.made != (fogcast.ShelfNewDisk{Title: "Boot", PackageID: pkg, Contents: "freedos"}) {
		t.Fatalf("PC/XT new %+v %+v %v", boot, s.made, err)
	}
	// The fake answers every request with one disk, so a copy that comes
	// back as its own source is refused by the client.
	st := hostclient.AtariStShelfCore
	if _, err := client.CopyShelfDisk(ctx, st, "work-disk", "Spare"); err == nil {
		t.Fatal("a copy with its source's identity was accepted")
	}
	if _, err := client.CopyShelfDisk(ctx, st, "dungeon", "Spare"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RevertShelfDisk(ctx, hostclient.PcxtShelfCore, "work-disk", revision); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RenameShelfDisk(ctx, st, "work-disk", "", "Letters"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteShelfDisk(ctx, st, "work-disk", revision); err != nil {
		t.Fatal(err)
	}
	want := []string{"list fes.atari-st", "new fes.atari-st", "new fes.pcxt-dos", "copy fes.atari-st work-disk Spare", "copy fes.atari-st dungeon Spare",
		"revert fes.pcxt-dos work-disk " + revision, "rename fes.atari-st work-disk  Letters", "delete fes.atari-st work-disk " + revision}
	if strings.Join(s.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v", s.calls)
	}
	// Requests the host would refuse never leave the client.
	if _, err := client.RevertShelfDisk(ctx, st, "work-disk", ""); err == nil {
		t.Fatal("revert without a revision left the client")
	}
	if _, err := client.NewShelfDisk(ctx, st, hostclient.NewShelfDiskRequest{Title: "Odd", PackageID: pkg, Tracks: 40, Heads: 1, Sectors: 9}); err == nil {
		t.Fatal("bad geometry left the client")
	}
	for _, req := range []hostclient.NewShelfDiskRequest{
		{Title: "Odd", PackageID: pkg, Contents: "freedos"},                                 // an ST disk is blank
		{Title: "Odd", PackageID: pkg},                                                      // a PC/XT disk says what it holds
		{Title: "Odd", PackageID: pkg, Contents: "blank", Tracks: 80, Heads: 2, Sectors: 9}, // and takes no geometry
	} {
		core := hostclient.PcxtShelfCore
		if req.Contents == "freedos" {
			core = st
		}
		if _, err := client.NewShelfDisk(ctx, core, req); err == nil {
			t.Fatalf("%s %+v left the client", core, req)
		}
	}
	if _, err := client.DiskShelf(ctx, "fes.c64"); err == nil {
		t.Fatal("a shelf for a computer without one was asked for")
	}
	if err := client.DeleteShelfDisk(ctx, st, "work-disk", "abc"); err == nil {
		t.Fatal("bad revision left the client")
	}
	if len(s.calls) != len(want) {
		t.Fatalf("invalid requests reached the host: %v", s.calls[len(want):])
	}
	s.err = &protocol.APIError{Code: protocol.CodeBusy}
	if err := client.DeleteShelfDisk(ctx, st, "work-disk", ""); err == nil || !strings.Contains(err.Error(), "BUSY") {
		t.Fatalf("busy delete: %v", err)
	}
}
