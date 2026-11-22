package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// shelfHost is a host whose disk shelf the picker reads and changes: the
// Atari ST's, or with core set the PC/XT's.
type shelfHost struct {
	t         *testing.T
	core      string // the shelf's core; empty is the Atari ST
	freeDOS   bool   // the host bundles a FreeDOS disk
	pkg       string
	disks     []hostclient.ShelfDisk
	keptKnown bool
	refuse    int // status for the next change, then cleared
	code      string
	refuseOn  string        // when set, only a change whose path ends with this is refused
	oldHost   bool          // a host from before the shelf routes
	hold      chan struct{} // when set, a change waits for it to close
	calls     []string
	sizes     [2]int64
}

func (h *shelfHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	core, format := "fes.atari-st", "atari-st-floppy"
	if h.core != "" {
		core, format = h.core, "pcxt-floppy"
	}
	base := "/api/v1/library/disk-shelves/" + core
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	text := func(k string) string { v, _ := body[k].(string); return v }
	find := func(id string) int {
		for i, d := range h.disks {
			if d.GameID == id {
				return i
			}
		}
		return -1
	}
	made := func(title string, media string) hostclient.ShelfDisk {
		d := hostclient.ShelfDisk{GameID: "fpga-" + strings.ReplaceAll(strings.ToLower(title), " ", "-"), Title: title, PackageID: h.pkg,
			MediaID: media, Size: 737280, Made: true, Kept: hostclient.ShelfKeptNone}
		h.disks = append(h.disks, d)
		return d
	}
	switch {
	case r.URL.Path == "/api/v1/games":
		io.WriteString(w, `{"games":[]}`)
		return
	case r.URL.Path == "/api/v1/library/firmware/bundled":
		list := protocol.BundledFirmwareList{Firmware: []protocol.BundledFirmware{}}
		if h.freeDOS {
			list.Disks = []protocol.BundledDisk{{ID: "freedos-xt-360", Name: "FreeDOS boot disk", CoreID: "fes.pcxt-dos", Size: 368640, MediaID: strings.Repeat("f", 64)}}
		}
		json.NewEncoder(w).Encode(list)
		return
	case strings.HasPrefix(r.URL.Path, "/api/v1/core-packages/"):
		json.NewEncoder(w).Encode(protocol.CoreMediaCapabilities{PackageID: h.pkg, Media: []protocol.CoreMediaCapability{
			{Role: "disk", Format: format, MinBytes: h.sizes[0], MaxBytes: h.sizes[1], Extensions: []string{".st"}}}})
		return
	case strings.HasPrefix(r.URL.Path, base) && h.oldHost:
		http.NotFound(w, r)
		return
	case strings.HasPrefix(r.URL.Path, "/api/v1/games/"):
		// The launch resolves the title; this host has no detail for it.
		h.calls = append(h.calls, "game "+strings.TrimPrefix(r.URL.Path, "/api/v1/games/"))
		http.NotFound(w, r)
		return
	case r.URL.Path == base && r.Method == http.MethodGet:
		h.calls = append(h.calls, "list")
		disks := h.disks
		if !h.keptKnown {
			disks = nil
			for _, d := range h.disks {
				if d.Made {
					d.Kept, d.KeptRevision = hostclient.ShelfKeptUnknown, ""
					disks = append(disks, d)
				}
			}
		}
		if disks == nil {
			disks = []hostclient.ShelfDisk{}
		}
		json.NewEncoder(w).Encode(hostclient.DiskShelf{Target: "kit", KeptKnown: h.keptKnown, Disks: disks})
		return
	}
	if !strings.HasPrefix(r.URL.Path, base) || r.Method != http.MethodPost {
		h.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 500)
		return
	}
	if h.hold != nil {
		<-h.hold
	}
	h.calls = append(h.calls, strings.TrimPrefix(r.URL.Path, base)+" "+string(raw))
	if h.refuse != 0 && (h.refuseOn == "" || strings.HasSuffix(r.URL.Path, h.refuseOn)) {
		w.WriteHeader(h.refuse)
		io.WriteString(w, `{"error":{"code":"`+h.code+`","message":"refused"}}`)
		h.refuse, h.refuseOn = 0, ""
		return
	}
	if r.URL.Path == base {
		w.WriteHeader(http.StatusCreated)
		d := made(text("title"), strings.Repeat("1", 64))
		if h.core != "" {
			d.Size = 368640
		} else {
			d.Size = int64(body["tracks"].(float64)) * int64(body["heads"].(float64)) * int64(body["sectors"].(float64)) * 512
		}
		h.disks[len(h.disks)-1].Size = d.Size
		json.NewEncoder(w).Encode(d)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, base+"/"), "/")
	i := find(parts[0])
	if i < 0 {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"code":"ROM_NOT_FOUND","message":"gone"}}`)
		return
	}
	switch parts[1] {
	case "copy":
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(made(text("title"), strings.Repeat("2", 64)))
	case "revert":
		h.disks[i].Kept, h.disks[i].KeptRevision = hostclient.ShelfKeptNone, ""
		d := h.disks[i]
		if !d.Made {
			h.disks = append(h.disks[:i], h.disks[i+1:]...)
		}
		json.NewEncoder(w).Encode(d)
	case "rename":
		h.disks = append(h.disks[:i], h.disks[i+1:]...)
		json.NewEncoder(w).Encode(made(text("title"), strings.Repeat("3", 64)))
	case "delete":
		h.disks = append(h.disks[:i], h.disks[i+1:]...)
		json.NewEncoder(w).Encode(map[string]string{"game_id": parts[0]})
	}
}

func newShelfApp(t *testing.T) (*App, *shelfHost) {
	t.Helper()
	return newShelfAppFor(t, &shelfHost{keptKnown: true, sizes: [2]int64{368640, 839680}})
}

func newShelfAppFor(t *testing.T, h *shelfHost) (*App, *shelfHost) {
	t.Helper()
	pkg := strings.Repeat("a", 64)
	h.t, h.pkg = t, pkg
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	core := "fes.atari-st"
	if h.core != "" {
		core = h.core
	}
	a.mu.Lock()
	a.openDiskShelfLocked(rooms.Action{Kind: rooms.ActionHardwareDisks, CoreID: core, PackageID: pkg})
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	return a, h
}

// pick selects the row of that kind on the level shown.
func shelfPick(t *testing.T, a *App, kind string) {
	t.Helper()
	a.mu.Lock()
	rows := a.coreLibraryRowsLocked()
	index := -1
	for i, row := range rows {
		if row.Kind == kind {
			index = i
		}
	}
	if index < 0 {
		a.mu.Unlock()
		t.Fatalf("no %s row among %+v", kind, rows)
	}
	a.coreLibrary.Index = index
	a.handleCoreLibraryLocked(CmdSelect)
	a.mu.Unlock()
	waitCoreLibrary(t, a)
}

func shelfType(t *testing.T, a *App, name string) {
	t.Helper()
	a.mu.Lock()
	if a.settingsOSKKind != settingsOSKShelfName {
		a.mu.Unlock()
		t.Fatal("the name keyboard is not open")
	}
	a.settingsOSKField.Buffer = name
	a.submitSettingsOSKLocked()
	a.mu.Unlock()
	waitCoreLibrary(t, a)
}

func shelfKinds(a *App) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var kinds []string
	for _, row := range a.coreLibraryRowsLocked() {
		kinds = append(kinds, row.Kind)
	}
	return strings.Join(kinds, ",")
}

func shelfSnap(a *App) FirmwarePickerSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.coreLibrarySnapshotLocked()
}

func TestDiskShelfMakesANamedDiskOfAChosenSize(t *testing.T) {
	a, h := newShelfApp(t)
	if snap := shelfSnap(a); snap.Title != "Your disks" || shelfKinds(a) != "shelf-new" || !strings.Contains(snap.Status, "blank disk to save your work on") {
		t.Fatalf("empty shelf: %q rows %s status %q", snap.Title, shelfKinds(a), snap.Status)
	}
	shelfPick(t, a, "shelf-new")
	if snap := shelfSnap(a); snap.Title != "New disk" || snap.Rows[1].Name != "Size · 720K" {
		t.Fatalf("new disk form: %+v", snap)
	}
	// Making a disk needs a name first; nothing is sent without one.
	shelfPick(t, a, "shelf-make")
	if len(h.calls) != 1 || !strings.Contains(shelfSnap(a).Status, "name first") {
		t.Fatalf("unnamed disk: calls %v status %q", h.calls, shelfSnap(a).Status)
	}
	shelfPick(t, a, "shelf-name")
	shelfType(t, a, "  Work disk 2 ")
	shelfPick(t, a, "shelf-size")
	if got := shelfSnap(a).Rows; got[0].Name != "Name · Work disk 2" || got[1].Name != "Size · 360K" {
		t.Fatalf("form after edits: %+v", got)
	}
	shelfPick(t, a, "shelf-make")
	want := ` {"heads":1,"package_id":"` + h.pkg + `","sectors":9,"title":"Work disk 2","tracks":80}`
	if h.calls[1] != want {
		t.Fatalf("new disk request %q", h.calls[1])
	}
	snap := shelfSnap(a)
	if snap.Title != "Your disks" || len(snap.Rows) != 2 || snap.Rows[1].Name != "Work disk 2 · 360K" || snap.Status != "Made Work disk 2." {
		t.Fatalf("shelf after making: %+v", snap)
	}
}

func TestDiskShelfOffersOnlyTheSizesThePackageTakes(t *testing.T) {
	a, h := newShelfApp(t)
	h.sizes = [2]int64{737280, 737280}
	a.mu.Lock()
	a.loadDiskShelfLocked("", "")
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	shelfPick(t, a, "shelf-new")
	shelfPick(t, a, "shelf-size")
	if got := shelfSnap(a).Rows[1].Name; got != "Size · 720K" {
		t.Fatalf("a 720K-only package offered %q", got)
	}
	// A package with no ST floppy offers no new disk.
	h.sizes = [2]int64{1, 2}
	a.mu.Lock()
	a.loadDiskShelfLocked("", "")
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	if kinds := shelfKinds(a); kinds != "" {
		t.Fatalf("rows without a usable size: %s", kinds)
	}
}

func TestDiskShelfDiskActionsFollowWhatTheDiskIs(t *testing.T) {
	a, h := newShelfApp(t)
	rev := strings.Repeat("e", 64)
	h.disks = []hostclient.ShelfDisk{
		{GameID: "fpga-dungeon", Title: "Dungeon", PackageID: h.pkg, MediaID: strings.Repeat("4", 64), Size: 737280, Kept: hostclient.ShelfKeptChanges, KeptRevision: rev},
		{GameID: "fpga-work", Title: "Work", PackageID: h.pkg, MediaID: strings.Repeat("5", 64), Size: 737280, Made: true, Kept: hostclient.ShelfKeptNone, InDrive: true},
		{GameID: "fpga-bad", Title: "Bad", PackageID: h.pkg, MediaID: strings.Repeat("6", 64), Size: 737280, Made: true, Kept: hostclient.ShelfKeptDamaged, KeptRevision: rev},
	}
	reload := func() {
		a.mu.Lock()
		a.loadDiskShelfLocked("", "")
		a.mu.Unlock()
		waitCoreLibrary(t, a)
	}
	reload()
	rows := shelfSnap(a).Rows
	if rows[1].Name != "Dungeon · 720K · Game disk with saved changes" || rows[2].Name != "Work · 720K · In drive A" || rows[3].Name != "Bad · 720K · Saved changes are damaged" {
		t.Fatalf("shelf rows %+v", rows)
	}
	for id, want := range map[string]string{
		// A game disk is never renamed or deleted here.
		"fpga-dungeon": "shelf-start,shelf-copy,shelf-revert",
		// The disk in the drive is not started again.
		"fpga-work": "shelf-copy,shelf-rename,shelf-delete",
		// Damaged changes can't be copied; they can only be removed.
		"fpga-bad": "shelf-start,shelf-revert,shelf-delete",
	} {
		reload()
		shelfPick(t, a, "shelf-disk:"+id)
		if kinds := shelfKinds(a); kinds != want {
			t.Fatalf("%s actions %s, want %s", id, kinds, want)
		}
	}
	// A kit that can't be asked lists made disks only, and offers nothing
	// that depends on what is saved.
	h.keptKnown = false
	reload()
	snap := shelfSnap(a)
	if len(snap.Rows) != 3 || !strings.Contains(snap.Status, "Can't check for saved changes") {
		t.Fatalf("unknown shelf %+v", snap)
	}
	shelfPick(t, a, "shelf-disk:fpga-work")
	if kinds := shelfKinds(a); kinds != "" {
		t.Fatalf("actions on a disk in the drive with unknown changes: %s", kinds)
	}
}

func TestDiskShelfGoingBackAsksFirstAndOffersACopy(t *testing.T) {
	a, h := newShelfApp(t)
	rev := strings.Repeat("e", 64)
	h.disks = []hostclient.ShelfDisk{{GameID: "fpga-dungeon", Title: "Dungeon", PackageID: h.pkg, MediaID: strings.Repeat("4", 64), Size: 737280, Kept: hostclient.ShelfKeptChanges, KeptRevision: rev}}
	a.mu.Lock()
	a.loadDiskShelfLocked("", "")
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	shelfPick(t, a, "shelf-disk:fpga-dungeon")
	shelfPick(t, a, "shelf-revert")
	snap := shelfSnap(a)
	// The safe choice is first and focused.
	if snap.Title != "Go back to the original?" || shelfKinds(a) != "shelf-cancel,shelf-copy-revert,shelf-confirm" || snap.Index != 0 {
		t.Fatalf("confirmation %+v", snap)
	}
	before := len(h.calls)
	shelfPick(t, a, "shelf-cancel")
	if len(h.calls) != before || shelfSnap(a).Title != "Dungeon" {
		t.Fatalf("keeping the changes sent %v", h.calls[before:])
	}
	// Back steps out one level at a time before it closes the shelf.
	shelfPick(t, a, "shelf-revert")
	for _, title := range []string{"Dungeon", "Your disks"} {
		a.mu.Lock()
		a.handleCoreLibraryLocked(CmdBack)
		open := a.coreLibrary.Open
		a.mu.Unlock()
		if !open || shelfSnap(a).Title != title {
			t.Fatalf("back reached %q open=%v, want %q", shelfSnap(a).Title, open, title)
		}
	}
	shelfPick(t, a, "shelf-disk:fpga-dungeon")
	shelfPick(t, a, "shelf-revert")
	shelfPick(t, a, "shelf-copy-revert")
	shelfType(t, a, "Dungeon save")
	got := h.calls[len(h.calls)-3:]
	if got[0] != `/fpga-dungeon/copy {"title":"Dungeon save"}` || got[1] != `/fpga-dungeon/revert {"expected_revision":"`+rev+`"}` || got[2] != "list" {
		t.Fatalf("copy then go back sent %v", got)
	}
	snap = shelfSnap(a)
	if len(snap.Rows) != 2 || snap.Rows[1].Name != "Dungeon save · 720K" || !strings.Contains(snap.Status, "back to its original") {
		t.Fatalf("shelf after going back: %+v", snap)
	}
}

func TestDiskShelfRenameDeleteAndRefusals(t *testing.T) {
	a, h := newShelfApp(t)
	rev := strings.Repeat("e", 64)
	h.disks = []hostclient.ShelfDisk{{GameID: "fpga-work", Title: "Work", PackageID: h.pkg, MediaID: strings.Repeat("5", 64), Size: 737280, Made: true, Kept: hostclient.ShelfKeptNone, KeptRevision: rev}}
	a.mu.Lock()
	a.loadDiskShelfLocked("", "")
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	shelfPick(t, a, "shelf-disk:fpga-work")
	// The same name changes nothing.
	before := len(h.calls)
	shelfPick(t, a, "shelf-rename")
	shelfType(t, a, "Work")
	if len(h.calls) != before {
		t.Fatalf("renaming to the same name sent %v", h.calls[before:])
	}
	// A refusal says what to do and leaves the disk's actions up.
	for _, tc := range []struct {
		status     int
		code, want string
	}{
		{http.StatusConflict, "BUSY", "Stop the computer first"},
		{http.StatusConflict, "STALE_REVISION", "has changed since it was listed"},
		{http.StatusBadRequest, "BAD_REQUEST", "name is taken"},
		{http.StatusServiceUnavailable, "MISTER_UNAVAILABLE", "Can't check the kit"},
	} {
		h.refuse, h.code = tc.status, tc.code
		shelfPick(t, a, "shelf-rename")
		shelfType(t, a, "Letters")
		// The shelf is read again, so a retry starts from what the kit holds.
		snap := shelfSnap(a)
		if !strings.Contains(snap.Status, tc.want) || snap.Title != "Your disks" || len(h.disks) != 1 || h.calls[len(h.calls)-1] != "list" {
			t.Fatalf("%s: status %q title %q calls %v", tc.code, snap.Status, snap.Title, h.calls)
		}
		shelfPick(t, a, "shelf-disk:fpga-work")
	}
	shelfPick(t, a, "shelf-rename")
	shelfType(t, a, "Letters")
	if last := h.calls[len(h.calls)-2]; last != `/fpga-work/rename {"expected_revision":"`+rev+`","title":"Letters"}` {
		t.Fatalf("rename request %q", last)
	}
	if snap := shelfSnap(a); snap.Rows[1].Name != "Letters · 720K" || snap.Status != "Renamed to Letters." {
		t.Fatalf("after rename %+v", snap)
	}
	shelfPick(t, a, "shelf-disk:fpga-letters")
	shelfPick(t, a, "shelf-delete")
	if snap := shelfSnap(a); snap.Title != "Delete this disk?" || shelfKinds(a) != "shelf-cancel,shelf-confirm" || snap.Index != 0 {
		t.Fatalf("delete confirmation %+v", snap)
	}
	shelfPick(t, a, "shelf-confirm")
	if last := h.calls[len(h.calls)-2]; last != `/fpga-letters/delete {"expected_revision":""}` {
		t.Fatalf("delete request %q", last)
	}
	if snap := shelfSnap(a); len(snap.Rows) != 1 || snap.Status != "Deleted Letters." {
		t.Fatalf("after delete %+v", snap)
	}
}

// A change in flight is not cancelled by leaving: the host does it in
// several steps, and a rename cut short would leave two disks.
// A new disk takes about half a minute on a kit, so the shelf says what it
// is doing until the host answers.
func TestDiskShelfSaysWhatItIsDoingWhileAChangeRuns(t *testing.T) {
	a, h := newShelfApp(t)
	shelfPick(t, a, "shelf-new")
	shelfPick(t, a, "shelf-name")
	shelfType(t, a, "Work")
	h.hold = make(chan struct{})
	a.mu.Lock()
	for i, row := range a.coreLibraryRowsLocked() {
		if row.Kind == "shelf-make" {
			a.coreLibrary.Index = i
		}
	}
	a.handleCoreLibraryLocked(CmdSelect)
	a.mu.Unlock()
	if got := shelfSnap(a).Status; got != "Making Work… This can take half a minute." {
		t.Errorf("status while making: %q", got)
	}
	close(h.hold)
	waitCoreLibrary(t, a)
	if got := shelfSnap(a).Status; got != "Made Work." {
		t.Fatalf("status after making: %q", got)
	}
}

func TestDiskShelfWaitsForAChangeInFlight(t *testing.T) {
	a, _ := newShelfApp(t)
	a.mu.Lock()
	defer a.mu.Unlock()
	cancelled := false
	a.coreLibrary.Busy = true
	a.coreLibrary.Cancel = func() { cancelled = true }
	for _, cmd := range []Command{CmdBack, CmdHome, CmdSettings, CmdSelect} {
		a.handleCoreLibraryLocked(cmd)
	}
	if cancelled || !a.coreLibrary.Open || !a.coreLibrary.Busy {
		t.Fatalf("a shelf change was abandoned: cancelled=%v open=%v", cancelled, a.coreLibrary.Open)
	}
}

// A copy that worked and a go-back that was refused leave two facts to show:
// the new disk exists, and the old one still has its changes.
func TestDiskShelfHalfDoneCopyThenGoBackShowsBoth(t *testing.T) {
	a, h := newShelfApp(t)
	rev := strings.Repeat("e", 64)
	h.disks = []hostclient.ShelfDisk{{GameID: "fpga-dungeon", Title: "Dungeon", PackageID: h.pkg, MediaID: strings.Repeat("4", 64), Size: 737280, Kept: hostclient.ShelfKeptChanges, KeptRevision: rev}}
	a.mu.Lock()
	a.loadDiskShelfLocked("", "")
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	shelfPick(t, a, "shelf-disk:fpga-dungeon")
	shelfPick(t, a, "shelf-revert")
	shelfPick(t, a, "shelf-copy-revert")
	h.refuse, h.code, h.refuseOn = http.StatusConflict, "BUSY", "/revert"
	shelfType(t, a, "Dungeon save")
	snap := shelfSnap(a)
	if snap.Title != "Your disks" || len(snap.Rows) != 3 || !strings.Contains(snap.Status, "Copied to Dungeon save, but Dungeon still has its changes. Stop the computer first") {
		t.Fatalf("half-done copy and go back: %+v", snap)
	}
}

func TestDiskShelfStartsTheComputerWithTheDiskAsItIs(t *testing.T) {
	a, h := newShelfApp(t)
	h.disks = []hostclient.ShelfDisk{{GameID: "fpga-work", Title: "Work", PackageID: h.pkg, MediaID: strings.Repeat("5", 64), Size: 737280, Made: true, Kept: hostclient.ShelfKeptNone}}
	a.mu.Lock()
	a.room = &rooms.Instance{}
	a.loadDiskShelfLocked("", "")
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	shelfPick(t, a, "shelf-disk:fpga-work")
	shelfPick(t, a, "shelf-start")
	a.mu.Lock()
	open, game, pkg, expected := a.coreLibrary.Open, a.launch.GameID, a.launchPackageID, a.launchExpectedPackageID
	a.mu.Unlock()
	if open || game != "fpga-work" || pkg != "" || expected != "" {
		t.Fatalf("start: picker open=%v launch %q package %q/%q", open, game, pkg, expected)
	}
	// The entry's package is left as it is: no package change rides along.
	for _, c := range h.calls {
		if strings.Contains(c, "core-entries") {
			t.Fatalf("starting a shelf disk changed its entry: %v", h.calls)
		}
	}
}

func TestDiskShelfOnAnOlderHostSaysSo(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	h := &shelfHost{t: t, pkg: pkg, keptKnown: true, oldHost: true, sizes: [2]int64{368640, 839680}}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.mu.Lock()
	a.openDiskShelfLocked(rooms.Action{Kind: rooms.ActionHardwareDisks, CoreID: "fes.atari-st", PackageID: pkg})
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	if snap := shelfSnap(a); len(snap.Rows) != 0 || !strings.Contains(snap.Status, "doesn't have the disk shelf yet") {
		t.Fatalf("older host: %+v", snap)
	}
}

func TestPcxtShelfMakesAFreeDOSOrABlankDisk(t *testing.T) {
	a, h := newShelfAppFor(t, &shelfHost{core: "fes.pcxt-dos", freeDOS: true, keptKnown: true, sizes: [2]int64{368640, 368640}})
	if snap := shelfSnap(a); snap.Title != "Your disks" || shelfKinds(a) != "shelf-new" || !strings.Contains(snap.Status, "can start FreeDOS") {
		t.Fatalf("empty PC/XT shelf: %q rows %s status %q", snap.Title, shelfKinds(a), snap.Status)
	}
	shelfPick(t, a, "shelf-new")
	rows := shelfSnap(a).Rows
	if len(rows) != 4 || rows[1].Name != "Size · 360K" || rows[2].Name != "Contents · Starts FreeDOS" {
		t.Fatalf("PC/XT new disk form: %+v", rows)
	}
	// The size is shown so the limit is visible; it has nothing else to be.
	shelfPick(t, a, "shelf-size")
	if got := shelfSnap(a).Rows[1].Name; got != "Size · 360K" {
		t.Fatalf("size after a change: %q", got)
	}
	shelfPick(t, a, "shelf-name")
	shelfType(t, a, "FreeDOS boot disk")
	shelfPick(t, a, "shelf-make")
	want := `{"contents":"freedos","package_id":"` + h.pkg + `","title":"FreeDOS boot disk"}`
	if got := h.calls[len(h.calls)-2]; got != " "+want {
		t.Fatalf("FreeDOS disk request %q", got)
	}
	if snap := shelfSnap(a); len(snap.Rows) != 2 || snap.Rows[1].Name != "FreeDOS boot disk · 360K" {
		t.Fatalf("shelf after making: %+v", snap.Rows)
	}
	shelfPick(t, a, "shelf-new")
	shelfPick(t, a, "shelf-contents")
	if got := shelfSnap(a).Rows[2].Name; got != "Contents · Formatted and empty" {
		t.Fatalf("contents after a change: %q", got)
	}
	shelfPick(t, a, "shelf-name")
	shelfType(t, a, "Work disk")
	shelfPick(t, a, "shelf-make")
	if got := h.calls[len(h.calls)-2]; !strings.Contains(got, `"contents":"blank"`) || strings.Contains(got, "tracks") {
		t.Fatalf("blank disk request %q", got)
	}
}

// A host without the FreeDOS disk offers only a blank one.
func TestPcxtShelfWithoutFreeDOSOffersABlankDisk(t *testing.T) {
	a, _ := newShelfAppFor(t, &shelfHost{core: "fes.pcxt-dos", keptKnown: true, sizes: [2]int64{368640, 368640}})
	shelfPick(t, a, "shelf-new")
	if rows := shelfSnap(a).Rows; len(rows) != 4 || rows[2].Name != "Contents · Formatted and empty" {
		t.Fatalf("form without FreeDOS: %+v", rows)
	}
	shelfPick(t, a, "shelf-contents")
	if got := shelfSnap(a).Rows[2].Name; got != "Contents · Formatted and empty" {
		t.Fatalf("contents cycled to %q", got)
	}
}
