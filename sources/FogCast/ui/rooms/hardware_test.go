package rooms

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
)

type hardwareFixture struct {
	fakeServices
	lock    sync.Mutex
	value   hostclient.HardwareSnapshot
	err     error
	saveErr error
	saves   [][4]string
}

func (s *hardwareFixture) Hardware(context.Context) (hostclient.HardwareSnapshot, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	v := s.value
	v.Machines = append([]hostclient.HardwareMachine(nil), s.value.Machines...)
	return v, s.err
}
func (s *hardwareFixture) SelectCoreEntryExpansion(_ context.Context, game, pkg, expected, id string) (hostclient.CoreEntryExpansion, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.saves = append(s.saves, [4]string{game, pkg, expected, id})
	if s.saveErr != nil {
		return hostclient.CoreEntryExpansion{}, s.saveErr
	}
	s.value.Machines[0].DraftExpansionID = id
	return hostclient.CoreEntryExpansion{GameID: game, ExpansionID: id}, nil
}
func hardwareData(active bool) *hardwareFixture {
	pkg, card := strings.Repeat("a", 64), strings.Repeat("b", 64)
	s := &hardwareFixture{value: hostclient.HardwareSnapshot{
		Machines: []hostclient.HardwareMachine{{GameID: "zx81-workbench", Title: "My ZX81", CoreID: "fes.zx81", PackageID: pkg, PackageReady: true, FirmwareReady: true, Ready: true,
			Socket:  hostclient.HardwareSocket{ID: "rear", Label: "Rear expansion connector", Supported: true},
			Choices: []hostclient.HardwareExpansion{{ExpansionID: card, Label: "Test memory pack", Description: "Fixture description: adds memory for supported programs.", Ready: true}}}},
		Session: &hostclient.SessionResult{State: "idle"},
	}}
	if active {
		s.value.Session = &hostclient.SessionResult{ID: "still-the-same-session", Target: "fixture", GameID: "zx81-workbench", State: "active", CorePackage: &hostclient.SessionCorePackage{PackageID: pkg, Generation: 1, Composition: &hostclient.SessionComposition{PackageID: pkg}, ABI: hostclient.SessionCoreABI{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: "fes.media.blob", Major: 1}}}}
	}
	return s
}
func frameHas(f Frame, s string) bool {
	for _, op := range f.Ops {
		if op.Kind == OpText && strings.Contains(op.Text, s) {
			return true
		}
	}
	return false
}
func loadHardware(t *testing.T, svc Services, pack Pack) *Instance {
	t.Helper()
	r := newRoom(t, pack, Options{Services: svc, StorePath: filepath.Join(t.TempDir(), "room.json")})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Fits this machine") })
	return r
}

func TestHardwareRoomSavesDraftWithoutChangingActiveMachine(t *testing.T) {
	s := hardwareData(true)
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	r.Activate("card:1")
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Fixture description") })
	s.lock.Lock()
	count := len(s.saves)
	s.lock.Unlock()
	if count != 0 {
		t.Fatal("inspecting a card saved it")
	}
	r.Activate("fit")
	f := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Restart needed") })
	if !frameHas(f, "Running: Empty socket") || !frameHas(f, "Stopping clears memory") {
		t.Fatal("missing active hardware or Stop consequence")
	}
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("saving a setup changed lifecycle: %+v", acts)
	}
	s.lock.Lock()
	if len(s.saves) != 1 || s.saves[0] != [4]string{"zx81-workbench", strings.Repeat("a", 64), "", strings.Repeat("b", 64)} {
		t.Errorf("wrong exact selection: %+v", s.saves)
	}
	if s.value.Session.ID != "still-the-same-session" {
		t.Error("session replaced")
	}
	s.lock.Unlock()
	r.Activate("tape")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionOpenTape || acts[0].SessionID != "still-the-same-session" || acts[0].MediaBinding.Generation != 1 || acts[0].MediaBinding.PackageID != strings.Repeat("a", 64) {
		t.Fatalf("tape did not use picker action: %+v", acts)
	}
	r.Activate("remove")
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Next start: Empty socket") && !frameHas(f, "Restart needed") })
	r.Activate("power")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionStop {
		t.Fatalf("Stop action: %+v", acts)
	}
}

func TestHardwareRoomPreservesFullMediaGeneration(t *testing.T) {
	s := hardwareData(true)
	s.value.Session.CorePackage.Generation = uint64(1)<<63 + 19
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	r.Activate("tape")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].MediaBinding.Generation != s.value.Session.CorePackage.Generation || acts[0].MediaBinding.Target != "fixture" {
		t.Fatalf("Lua lost session binding: %+v", acts)
	}
}

func TestHardwareRoomRestoresFocusAndUsesOrdinaryLaunch(t *testing.T) {
	s := hardwareData(false)
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	r.Hover("power")
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return !r.hardwareReading })
	if !r.Input("select") {
		t.Fatal("controller/keyboard Confirm was not consumed")
	}
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "zx81-workbench" {
		t.Fatalf("focus/launch lost on return: %+v", acts)
	}
	// A direction route reaches another control; pointer isn't required.
	// Along the machine row to Set up ZX81, up into the setup tools to
	// Import expansion, then right to Library.
	for _, dir := range []string{"right", "right", "right", "right", "up", "right"} {
		r.Input(dir)
	}
	r.Input("select")
	if acts = r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionOpenLibrary {
		t.Fatalf("direction navigation failed: %+v", acts)
	}
}

func TestHardwareRoomFailedSelectionRefreshesWithoutReplay(t *testing.T) {
	s := hardwareData(true)
	s.saveErr = errors.New("CONFLICT: setup changed on another client")
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	r.Activate("card:1")
	r.Step(time.Now())
	r.Activate("fit")
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Setup was not confirmed") && !r.hardwareReading })
	s.lock.Lock()
	defer s.lock.Unlock()
	if len(s.saves) != 1 || s.value.Machines[0].DraftExpansionID != "" {
		t.Fatalf("failed save was replayed/applied: %+v", s.saves)
	}
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("failure changed lifecycle: %+v", acts)
	}
}

type withoutArt struct{ fs.FS }

func (f withoutArt) Open(name string) (fs.File, error) {
	if strings.HasPrefix(name, "assets/") {
		return nil, fs.ErrNotExist
	}
	return f.FS.Open(name)
}
func TestHardwareRoomMissingArtworkAndUnavailableHost(t *testing.T) {
	s := hardwareData(false)
	p := examplePack(t, "example.hardware")
	p.FS = withoutArt{p.FS}
	r := loadHardware(t, s, p)
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Machine illustration unavailable") })
	r.Activate("card:1")
	r.Step(time.Now())
	s.lock.Lock()
	s.err = errors.New("host offline")
	s.lock.Unlock()
	r.Activate("refresh")
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Cannot refresh the host") })
	r.Activate("fit")
	r.Activate("power")
	s.lock.Lock()
	defer s.lock.Unlock()
	if len(s.saves) != 0 || len(r.TakeActions()) != 0 {
		t.Fatal("stale room allowed a mutation")
	}
}

func TestHardwareRoomHostSessionUnavailableAndMissingFirmware(t *testing.T) {
	for _, tc := range []string{"session unavailable", "firmware missing", "different session"} {
		t.Run(tc, func(t *testing.T) {
			s := hardwareData(false)
			switch tc {
			case "session unavailable":
				s.value.Session = nil
				s.value.SessionError = "target disconnected"
			case "firmware missing":
				s.value.Machines[0].FirmwareReady = false
				s.value.Machines[0].Ready = false
				s.value.Machines[0].UnavailableReason = "Choose firmware in the FPGA library."
			case "different session":
				s.value.Session = &hostclient.SessionResult{ID: "other", GameID: "other", State: "active"}
			}
			r := loadHardware(t, s, examplePack(t, "example.hardware"))
			r.Activate("power")
			r.Activate("tape")
			if acts := r.TakeActions(); len(acts) != 0 {
				t.Fatalf("unavailable controls acted: %+v", acts)
			}
		})
	}
}

func TestHardwareRoomDoesNotInventMissingComposition(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint(mismatch), func(t *testing.T) {
			s := hardwareData(true)
			if mismatch {
				s.value.Session.CorePackage.Composition.PackageID = strings.Repeat("c", 64)
			} else {
				s.value.Session.CorePackage.Composition = nil
			}
			r := loadHardware(t, s, examplePack(t, "example.hardware"))
			f := r.Step(time.Now())
			if !frameHas(f, "Running: Hardware unavailable") || frameHas(f, "Running: Empty socket") || frameHas(f, "Restart needed") {
				t.Fatal("missing or mismatched receipt treated as known hardware")
			}
			r.Activate("power")
			if len(r.TakeActions()) != 0 {
				t.Fatal("unknown hardware allowed room Stop")
			}
		})
	}
}

func TestHardwareRoomSelectsCassetteBeforeLaunchAndGatesLiveHDMIControls(t *testing.T) {
	s := hardwareData(false)
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	r.Activate("tape")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionHardwareTapes || acts[0].GameID != "zx81-workbench" || acts[0].PackageID != strings.Repeat("a", 64) {
		t.Fatalf("pre-launch tape binding: %+v", acts)
	}
	r.Activate("setup_library")
	if acts = r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionHardwareSetup {
		t.Fatalf("setup not reachable: %+v", acts)
	}
	r.Close()
	s = hardwareData(true)
	index := NewIndex(Examples())
	pack, _ := index.Find("example.hardware")
	r, err := New(pack, Options{Width: 1120, Height: 630, Services: s, SessionDisplayRequired: true, Budget: Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return !r.hardwareReading })
	r.Activate("tape")
	if acts = r.TakeActions(); len(acts) != 0 {
		t.Fatalf("offered invisible live controls: %+v", acts)
	}
}

func TestKitHardwareRoomOffersTapeForObservedSessionDisplay(t *testing.T) {
	s := hardwareData(true)
	s.value.Session.CorePackage.ActiveInterfaces = append(s.value.Session.CorePackage.ActiveInterfaces,
		hostclient.SessionCoreInterface{ID: "fes.video.session-display", Major: 1},
		hostclient.SessionCoreInterface{ID: "fes.memory.hps-ddr", Major: 1})
	r, err := New(examplePack(t, "example.hardware"), Options{Width: 1120, Height: 630, Services: s, SessionDisplayRequired: true, Budget: Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return !r.hardwareReading })
	r.Activate("tape")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenTape || acts[0].SessionID != s.value.Session.ID || acts[0].MediaBinding.Generation != s.value.Session.CorePackage.Generation {
		t.Fatalf("kit tape action lost observed capability or session binding: %+v", acts)
	}
}

func TestKitHardwareRoomOffersTapeForComputerTapeUnit(t *testing.T) {
	s := hardwareData(true)
	tape := protocol.Zx81TapeInterface()
	s.value.Session.CorePackage.ABI = hostclient.SessionCoreABI{ID: "fes.computer", Major: 1}
	s.value.Session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{
		{ID: tape.ID, Major: tape.Major, Minor: tape.Minor},
		{ID: "fes.memory.hps-ddr", Major: 1},
		{ID: "fes.video.session-display", Major: 1},
	}
	r, err := New(examplePack(t, "example.hardware"), Options{Width: 1120, Height: 630, Services: s, SessionDisplayRequired: true, Budget: Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return !r.hardwareReading })
	r.Activate("tape")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenTape || acts[0].SessionID != s.value.Session.ID || acts[0].MediaBinding.Generation != s.value.Session.CorePackage.Generation {
		t.Fatalf("computer tape unit with session display did not offer the picker: %+v", acts)
	}
}

func TestKitHardwareRoomHidesComputerTapeWithoutPlane(t *testing.T) {
	s := hardwareData(true)
	tape := protocol.Zx81TapeInterface()
	s.value.Session.CorePackage.ABI = hostclient.SessionCoreABI{ID: "fes.computer", Major: 1}
	s.value.Session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: tape.ID, Major: tape.Major, Minor: tape.Minor}}
	r, err := New(examplePack(t, "example.hardware"), Options{Width: 1120, Height: 630, Services: s, SessionDisplayRequired: true, Budget: Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return !r.hardwareReading })
	r.Activate("tape")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("computer tape without session display opened the picker: %+v", acts)
	}
}

func hardwareFocus(t *testing.T, r *Instance) string {
	t.Helper()
	nav, _ := r.store.values["navigation"].(map[string]any)
	if nav == nil {
		// store.set keeps Lua tables as converted Go values.
		if m, ok := r.store.values["navigation"].(map[any]any); ok {
			if f, ok := m["focus"].(string); ok {
				return f
			}
		}
		t.Fatalf("no navigation in store: %#v", r.store.values["navigation"])
	}
	f, _ := nav["focus"].(string)
	return f
}

// Every control is reachable with Direction alone, and each tool sits next
// to its neighbours, so nothing depends on a pointer or on Tab order.
func TestHardwareRoomEveryControlReachableByDirection(t *testing.T) {
	r := loadHardware(t, hardwareData(false), examplePack(t, "example.hardware"))
	f := r.Step(time.Unix(50, 0))
	var ids []string
	for _, hit := range f.Hits {
		ids = append(ids, hit.ID)
	}
	edges := map[string][]string{}
	for _, id := range ids {
		for _, dir := range []string{"up", "down", "left", "right"} {
			r.Hover(id)
			r.Input(dir)
			edges[id] = append(edges[id], hardwareFocus(t, r))
		}
	}
	seen := map[string]bool{"power": true}
	queue := []string{"power"}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, next := range edges[id] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	for _, id := range ids {
		if !seen[id] {
			t.Fatalf("%s is not reachable by Direction from Start: %v", id, edges)
		}
	}
	// The tools are a grid: Import sits under More cards, Library under Refresh.
	step := func(from, dir string) string { r.Hover(from); r.Input(dir); return hardwareFocus(t, r) }
	if got := step("shelf_page", "down"); got != "import_card" {
		t.Fatalf("Down from More cards = %s", got)
	}
	if got := step("refresh", "down"); got != "library" {
		t.Fatalf("Down from Refresh = %s", got)
	}
}

// Without a session reading the room says Running is unknown, never that
// the machine is not running.
func TestHardwareRoomRunningIsUnknownWithoutASessionReading(t *testing.T) {
	s := hardwareData(false)
	s.value.Session = nil
	s.value.SessionError = "target unreachable"
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	f := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Running: unknown") })
	if frameHas(f, "Running: not running") || frameHas(f, "Running: Not running") {
		t.Fatal("unknown session shown as not running")
	}
}

func exactText(f Frame, s string) bool {
	for _, op := range f.Ops {
		if op.Kind == OpText && op.Text == s {
			return true
		}
	}
	return false
}

// Solid saved cards are the filled connector body. Shelf cards sit much lower.
func connectorCardMarks(f Frame) (solid int, outlined bool) {
	for _, op := range f.Ops {
		if op.Kind != OpRect || FormatHexColor(op.Color) != "#5c7a6b" || op.Y > 320 {
			continue
		}
		if op.W >= 20 && op.H >= 20 {
			solid++
		} else if op.W <= 8 || op.H <= 8 {
			outlined = true
		}
	}
	return solid, outlined
}

func TestHardwareRoomPreviewKeepsTheSavedCard(t *testing.T) {
	saved, sound, missing := strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)
	s := hardwareData(false)
	s.value.Machines[0].DraftExpansionID = saved
	s.value.Machines[0].Choices = []hostclient.HardwareExpansion{
		{ExpansionID: saved, Label: "16K RAM pack", Description: "Saved memory for the next start.", Ready: true},
		{ExpansionID: sound, Label: "Sound expansion", Description: "A ready card being inspected.", Ready: true},
		{ExpansionID: missing, Label: "Expansion unavailable", Description: "Retained for inspection.", Ready: false, UnavailableReason: "Install its matching package before fitting it."},
	}
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	f := r.Step(time.Unix(50, 0))
	solid, outlined := connectorCardMarks(f)
	if solid != 1 || outlined || !exactText(f, "Next start") || exactText(f, "Preview") {
		t.Fatalf("saved card before preview: solid=%d outlined=%v next=%v preview=%v", solid, outlined, exactText(f, "Next start"), exactText(f, "Preview"))
	}
	r.Activate("card:2")
	f = stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Previewing: Sound expansion") })
	solid, outlined = connectorCardMarks(f)
	if solid != 1 || !outlined || !exactText(f, "Next start") || !exactText(f, "Preview") || !frameHas(f, "Next start: 16K RAM pack") {
		t.Fatalf("preview hid the saved card: solid=%d outlined=%v", solid, outlined)
	}
	r.Activate("card:3")
	f = stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Previewing: Expansion unavailable") })
	solid, outlined = connectorCardMarks(f)
	if solid != 1 || outlined || !exactText(f, "Next start") || exactText(f, "Preview") || !frameHas(f, "Install its matching package") {
		t.Fatalf("unavailable preview drawn on the connector: solid=%d outlined=%v previewLabel=%v", solid, outlined, exactText(f, "Preview"))
	}
}

func cassetteLabel(t *testing.T, name string) Op {
	t.Helper()
	s := hardwareData(false)
	s.value.Machines[0].MediaName = name
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	f := r.Step(time.Unix(50, 0))
	for _, op := range f.Ops {
		if op.Kind == OpText && strings.Contains(op.Text, "part-") {
			return op
		}
	}
	t.Fatalf("cassette name %q not drawn", name)
	return Op{}
}

func textWithin(op Op) bool {
	weight := gfx.WeightRegular
	if op.Bold {
		weight = gfx.WeightBold
	}
	return op.MaxW > 0 && gfx.MeasureTextWeight(op.Text, op.Size, weight) <= op.MaxW
}

func TestHardwareRoomCassetteNamesStayDistinct(t *testing.T) {
	one, two := "zx81-workshop-demo-part-one.p", "zx81-workshop-demo-part-two.p"
	left, right := cassetteLabel(t, one), cassetteLabel(t, two)
	if left.Text != one || right.Text != two || left.Text == right.Text {
		t.Fatalf("shared prefix clipped: %q vs %q", left.Text, right.Text)
	}
	if left.MaxW < 900 || !textWithin(left) || !textWithin(right) {
		t.Fatalf("cassette row still narrow: max %d %d within %v %v", left.MaxW, right.MaxW, textWithin(left), textWithin(right))
	}
	prefix := strings.Repeat("zx81-workshop-demo-", 40)
	longLeft, longRight := cassetteLabel(t, prefix+"part-one.p"), cassetteLabel(t, prefix+"part-two.p")
	if longLeft.Text == longRight.Text || !strings.HasSuffix(longLeft.Text, "part-one.p") || !strings.HasSuffix(longRight.Text, "part-two.p") {
		t.Fatalf("long names lost their endings: %q vs %q", longLeft.Text, longRight.Text)
	}
	if !textWithin(longLeft) || !textWithin(longRight) || !strings.Contains(longLeft.Text, "…") {
		t.Fatalf("long cassette name still clipped by max width: %q max %d", longLeft.Text, longLeft.MaxW)
	}
}
