package rooms

import (
	"context"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/internal/testwait"
	"io/fs"
	"math"
	"os"
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
	lock             sync.Mutex
	value            hostclient.HardwareSnapshot
	err              error
	saveErr          error
	readErrAfterSave error
	saves            [][4]string
}

type delayedFirstHardwareServices struct {
	*hardwareFixture
	started     chan struct{}
	release     chan struct{}
	callMu      sync.Mutex
	calls       int
	startOnce   sync.Once
	releaseOnce sync.Once
}

func (s *delayedFirstHardwareServices) Hardware(ctx context.Context) (hostclient.HardwareSnapshot, error) {
	snapshot, err := s.hardwareFixture.Hardware(ctx)
	s.callMu.Lock()
	s.calls++
	first := s.calls == 1
	s.callMu.Unlock()
	if first {
		s.startOnce.Do(func() { close(s.started) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return hostclient.HardwareSnapshot{}, ctx.Err()
		}
	}
	return snapshot, err
}

func (s *delayedFirstHardwareServices) releaseFirst() {
	s.releaseOnce.Do(func() { close(s.release) })
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
	if s.readErrAfterSave != nil {
		s.err = s.readErrAfterSave
	}
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

func TestHardwareRoomQueuesRefreshRequestedDuringRead(t *testing.T) {
	svc := &delayedFirstHardwareServices{
		hardwareFixture: hardwareData(false),
		started:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	defer svc.releaseFirst()
	r := newRoom(t, examplePack(t, "example.hardware"), Options{Services: svc, StorePath: filepath.Join(t.TempDir(), "room.json")})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-svc.started:
	case <-time.After(testwait.Timeout):
		t.Fatal("initial hardware read did not start")
	}
	r.Resume()
	svc.hardwareFixture.lock.Lock()
	svc.value.Machines[0].Title = "Updated custom setup"
	svc.hardwareFixture.lock.Unlock()
	svc.releaseFirst()
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Updated custom setup") })
	svc.callMu.Lock()
	calls := svc.calls
	svc.callMu.Unlock()
	if calls != 2 {
		t.Fatalf("hardware reads = %d, want initial plus queued refresh", calls)
	}
}

func TestHardwareRoomDropsSnapshotSupersededByQueuedRefresh(t *testing.T) {
	stale := hardwareData(false).value.Machines[0]
	stale.GameID = "stale-in-flight"
	stale.Title = "Stale in-flight setup"
	selected := stale
	selected.GameID = "selected-setup"
	selected.Title = "Selected setup"
	fixture := hardwareData(false)
	fixture.value.Machines = []hostclient.HardwareMachine{stale}
	svc := &delayedFirstHardwareServices{
		hardwareFixture: fixture,
		started:         make(chan struct{}),
		release:         make(chan struct{}),
	}
	defer svc.releaseFirst()
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Services: svc, StorePath: filepath.Join(t.TempDir(), "room.json"), InitialGameID: selected.GameID,
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-svc.started:
	case <-time.After(testwait.Timeout):
		t.Fatal("initial hardware read did not start")
	}
	r.Resume()

	fixture.lock.Lock()
	fixture.value.Machines = []hostclient.HardwareMachine{stale, selected}
	fixture.lock.Unlock()
	svc.releaseFirst()
	frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, selected.Title) })
	if frameHas(frame, stale.Title) {
		t.Fatalf("superseded snapshot changed the initial selection:\n%s", frameText(frame))
	}
	svc.callMu.Lock()
	calls := svc.calls
	svc.callMu.Unlock()
	if calls != 2 {
		t.Fatalf("hardware reads = %d, want initial plus queued refresh", calls)
	}
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

func TestHardwareRoomUsesInitialGameIDInsteadOfSavedNavigation(t *testing.T) {
	s := hardwareData(false)
	s.lock.Lock()
	second := s.value.Machines[0]
	second.GameID = "zx81-second"
	second.Title = "Selected ZX81"
	s.value.Machines = append(s.value.Machines, second)
	s.lock.Unlock()
	storePath := filepath.Join(t.TempDir(), "room.json")
	if err := os.WriteFile(storePath, []byte(`{"navigation":{"game_id":"zx81-workbench"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Services: s, Width: 1280, Height: 720, StorePath: storePath, InitialGameID: "zx81-second",
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Selected ZX81") })
	if frameHas(frame, "My ZX81") {
		t.Fatalf("workbench ignored the selected entry\n%s", frameText(frame))
	}
}

func TestSinclairWorkshopUsesSelectedModel(t *testing.T) {
	for _, tc := range []struct{ core, label, mark string }{
		{"fes.zx80", "Sinclair ZX80", "ZX80"},
		{"fes.ts1000", "Timex Sinclair 1000", "TS1000"},
		{"fes.ts1500", "Timex Sinclair 1500", "TS1500"},
	} {
		t.Run(tc.core, func(t *testing.T) {
			s := hardwareData(false)
			s.value.Machines[0].CoreID = tc.core
			s.value.Machines[0].Title = "My " + tc.label
			r := newRoom(t, examplePack(t, "example.hardware"), Options{
				Services: s, Width: 1280, Height: 720, InitialCoreID: tc.core, InitialGameID: s.value.Machines[0].GameID,
			})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Set up "+tc.mark) })
			if !frameHas(frame, tc.label+" setup") || !frameHas(frame, strings.ToUpper(tc.label)) || frameHas(frame, "Sinclair ZX81 setup") {
				t.Fatalf("wrong model branding:\n%s", frameText(frame))
			}
			r.Activate("setup_library")
			acts := r.TakeActions()
			if len(acts) != 1 || acts[0].Kind != ActionHardwareSetup || acts[0].CoreID != tc.core {
				t.Fatalf("wrong model setup action: %+v", acts)
			}
		})
	}
}

func TestSinclairWorkshopMissingModelKeepsItsName(t *testing.T) {
	s := hardwareData(false)
	s.value.Machines = nil
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Services: s, Width: 1280, Height: 720, InitialCoreID: "fes.ts1500", InitialGameID: "missing-timex",
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		return frameHas(f, "The selected Timex Sinclair 1500 setup is no longer available.")
	})
	if !frameHas(frame, "Timex Sinclair 1500 setup") || frameHas(frame, "ZX81") {
		t.Fatalf("missing model showed another machine:\n%s", frameText(frame))
	}
}

func TestHardwareRoomFallsBackWhenInitialGameIDIsUnavailable(t *testing.T) {
	s := hardwareData(false)
	s.lock.Lock()
	s.value.Machines[0].GameID = "zx81-remaining"
	s.value.Machines[0].Title = "Remaining ZX81"
	s.lock.Unlock()
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Services: s, Width: 1280, Height: 720, StorePath: filepath.Join(t.TempDir(), "room.json"), InitialGameID: "zx81-missing",
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Remaining ZX81") && frameHas(f, "Start machine") })
	if frameHas(frame, "selected ZX81 setup is no longer available") || !frameHas(frame, "Next start: Empty socket") {
		t.Fatalf("remaining setup was not selected after the requested entry disappeared\n%s", frameText(frame))
	}
	r.Activate("power")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "zx81-remaining" {
		t.Fatalf("remaining setup could not start: %+v", acts)
	}
}

func TestHardwareRoomFallsBackWhenSelectedMachineDisappears(t *testing.T) {
	s := hardwareData(false)
	s.lock.Lock()
	second := s.value.Machines[0]
	second.GameID = "zx81-second"
	second.Title = "Selected ZX81"
	s.value.Machines = append(s.value.Machines, second)
	s.lock.Unlock()
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Services: s, Width: 1280, Height: 720, StorePath: filepath.Join(t.TempDir(), "room.json"), InitialGameID: "zx81-second",
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Selected ZX81") && frameHas(f, "Next setup") })
	r.Activate("setup")
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "My ZX81") && frameHas(f, "Next setup") })

	s.lock.Lock()
	s.value.Machines = []hostclient.HardwareMachine{second}
	s.lock.Unlock()
	r.Activate("refresh")
	frame := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Selected ZX81") && frameHas(f, "Start machine") })
	if frameHas(frame, "My ZX81") || frameHas(frame, "selected ZX81 setup is no longer available") {
		t.Fatalf("workbench did not fall back to the remaining setup\n%s", frameText(frame))
	}
	r.Activate("power")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "zx81-second" {
		t.Fatalf("remaining setup could not start: %+v", acts)
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
	stepUntil(t, r, func(f Frame) bool { return !r.hardwareReading && frameHas(f, "Import expansion") })
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

func TestHardwareRoomReadOnlyHostKeepsInspection(t *testing.T) {
	unsupported := false
	s := hardwareData(true)
	s.value.ExpansionSelection = &unsupported
	s.value.Session.CorePackage.Composition.ExpansionID = strings.Repeat("b", 64)
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	f := r.Step(time.Now())
	if !frameHas(f, "Restart needed: the running machine still uses its old setup.") || !frameHas(f, "Open the Sinclair ZX81 setup on the host computer to choose Fit & save.") {
		t.Fatal("read-only setup did not name the host computer")
	}
	if !frameHas(f, "Stopping clears memory") || !frameHas(f, "Stop machine") {
		t.Fatal("read-only running machine hid its Stop consequence or control")
	}
	r.Activate("card:1")
	f = r.Step(time.Now())
	if !frameHas(f, "Previewing. This kit cannot save it.") || !frameHas(f, "Previewing: Test memory pack (not saved)") {
		t.Fatal("inspection did not stay a preview")
	}
	r.Activate("fit")
	r.Activate("remove")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("read-only setup actions changed the machine: %+v", acts)
	}
	r.Activate("power")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionStop {
		t.Fatalf("read-only running machine could not be stopped: %+v", acts)
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if len(s.saves) != 0 || s.value.Machines[0].DraftExpansionID != "" {
		t.Fatalf("read-only room wrote a setup: %+v", s.saves)
	}
}

func TestHardwareRoomSupportedSaveAcknowledgesOnce(t *testing.T) {
	supported := true
	s := hardwareData(false)
	s.value.ExpansionSelection = &supported
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	r.Activate("card:1")
	r.Step(time.Now())
	r.Activate("fit")
	r.Activate("fit")
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Setup saved on the host") && !r.hardwareReading })
	s.lock.Lock()
	defer s.lock.Unlock()
	if len(s.saves) != 1 || s.saves[0] != [4]string{"zx81-workbench", strings.Repeat("a", 64), "", strings.Repeat("b", 64)} {
		t.Fatalf("save count or payload: %+v", s.saves)
	}
	if acts := r.TakeActions(); len(acts) != 0 || s.value.Session.State != "idle" {
		t.Fatalf("save changed the session: %+v %+v", acts, s.value.Session)
	}
}

func TestHardwareRoomFailedReadAfterSaveIsStale(t *testing.T) {
	s := hardwareData(false)
	s.readErrAfterSave = errors.New("host offline")
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	r.Activate("card:1")
	r.Step(time.Now())
	r.Activate("fit")
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Cannot refresh the host") })
	r.Activate("fit")
	s.lock.Lock()
	defer s.lock.Unlock()
	if len(s.saves) != 1 || s.value.Machines[0].DraftExpansionID != strings.Repeat("b", 64) {
		t.Fatalf("failed read replayed or dropped the save: %+v draft=%q", s.saves, s.value.Machines[0].DraftExpansionID)
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
	if len(acts) != 1 || acts[0].Kind != ActionHardwareTapes || acts[0].GameID != "zx81-workbench" || acts[0].CoreID != "fes.zx81" || acts[0].PackageID != strings.Repeat("a", 64) {
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
	stepUntil(t, r, func(f Frame) bool { return !r.hardwareReading && frameHas(f, "Import expansion") })
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
	stepUntil(t, r, func(f Frame) bool { return !r.hardwareReading && frameHas(f, "Import expansion") })
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
	stepUntil(t, r, func(f Frame) bool { return !r.hardwareReading && frameHas(f, "Import expansion") })
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
	stepUntil(t, r, func(f Frame) bool { return !r.hardwareReading && frameHas(f, "Import expansion") })
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
// An unavailable saved card uses the shelf's muted fill on the connector.
func connectorCardMarks(f Frame) (solid int, outlined bool, unavailable int) {
	for _, op := range f.Ops {
		if op.Kind != OpRect || op.Y > 320 {
			continue
		}
		switch FormatHexColor(op.Color) {
		case "#5c7a6b":
			if op.W >= 20 && op.H >= 20 {
				solid++
			} else if op.W <= 8 || op.H <= 8 {
				outlined = true
			}
		case "#53625a":
			if op.W >= 20 && op.H >= 20 {
				unavailable++
			}
		}
	}
	return solid, outlined, unavailable
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
	solid, outlined, unavailable := connectorCardMarks(f)
	if solid != 1 || outlined || unavailable != 0 || !exactText(f, "Next start") || exactText(f, "Preview") {
		t.Fatalf("saved card before preview: solid=%d outlined=%v unavailable=%d next=%v preview=%v", solid, outlined, unavailable, exactText(f, "Next start"), exactText(f, "Preview"))
	}
	r.Activate("card:2")
	f = stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Previewing: Sound expansion") })
	solid, outlined, unavailable = connectorCardMarks(f)
	if solid != 1 || !outlined || unavailable != 0 || !exactText(f, "Next start") || !exactText(f, "Preview") || !frameHas(f, "Next start: 16K RAM pack") {
		t.Fatalf("preview hid the saved card: solid=%d outlined=%v unavailable=%d", solid, outlined, unavailable)
	}
	r.Activate("card:3")
	f = stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Previewing: Expansion unavailable") })
	solid, outlined, unavailable = connectorCardMarks(f)
	if solid != 1 || outlined || unavailable != 0 || !exactText(f, "Next start") || exactText(f, "Preview") || !frameHas(f, "Install its matching package") {
		t.Fatalf("unavailable preview drawn on the connector: solid=%d outlined=%v unavailable=%d previewLabel=%v", solid, outlined, unavailable, exactText(f, "Preview"))
	}
}

// The host can retain a saved expansion after admission fails. That card
// stays named as Next start, and it is drawn muted rather than fitted.
// A ready preview still appears beside it.
func TestHardwareRoomUnavailableSavedCardIsNotFitted(t *testing.T) {
	saved, sound := strings.Repeat("b", 64), strings.Repeat("c", 64)
	s := hardwareData(false)
	s.value.Machines[0].Ready = false
	s.value.Machines[0].UnavailableReason = "The saved expansion is unavailable for this machine package."
	s.value.Machines[0].DraftExpansionID = saved
	s.value.Machines[0].Choices = []hostclient.HardwareExpansion{
		{ExpansionID: saved, Label: "16K RAM pack", Description: "Saved, then rejected for this package.", Ready: false, UnavailableReason: "This expansion is unavailable or does not fit this exact machine package."},
		{ExpansionID: sound, Label: "Sound expansion", Description: "A ready card being inspected.", Ready: true},
	}
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	f := stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Next start: 16K RAM pack") })
	solid, outlined, unavailable := connectorCardMarks(f)
	if solid != 0 || outlined || unavailable != 1 || !exactText(f, "Next start") || exactText(f, "Preview") {
		t.Fatalf("unavailable saved card looked fitted: solid=%d outlined=%v unavailable=%d", solid, outlined, unavailable)
	}
	r.Activate("card:2")
	f = stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Previewing: Sound expansion") })
	solid, outlined, unavailable = connectorCardMarks(f)
	if solid != 0 || !outlined || unavailable != 1 || !exactText(f, "Next start") || !exactText(f, "Preview") || !frameHas(f, "Next start: 16K RAM pack") {
		t.Fatalf("unavailable saved card fitted beside a ready preview: solid=%d outlined=%v unavailable=%d", solid, outlined, unavailable)
	}
}

func cassetteLabel(t *testing.T, name, fragment string) Op {
	t.Helper()
	s := hardwareData(false)
	s.value.Machines[0].MediaName = name
	r := loadHardware(t, s, examplePack(t, "example.hardware"))
	f := r.Step(time.Unix(50, 0))
	for _, op := range f.Ops {
		if op.Kind == OpText && strings.Contains(op.Text, fragment) {
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
	// hardwareTapeName supplies a starter display name, or "Imported cassette"
	// plus eight digest characters. The filename is not on the machine record.
	// Those host labels fit the row unchanged. The longer strings only check
	// that an overflowing label keeps its start and its end.
	for _, name := range []string{"Guess the Number", "Imported cassette abcdef01", "Imported cassette 1234abcd", "No cassette"} {
		got := cassetteLabel(t, name, name)
		if got.Text != name || strings.Contains(got.Text, "…") || got.MaxW < 900 || !textWithin(got) {
			t.Fatalf("host cassette label rewritten: %q max %d", got.Text, got.MaxW)
		}
	}
	one, two := "zx81-workshop-demo-part-one.p", "zx81-workshop-demo-part-two.p"
	left, right := cassetteLabel(t, one, "part-"), cassetteLabel(t, two, "part-")
	if left.Text != one || right.Text != two || left.Text == right.Text {
		t.Fatalf("shared prefix clipped: %q vs %q", left.Text, right.Text)
	}
	if left.MaxW < 900 || !textWithin(left) || !textWithin(right) {
		t.Fatalf("cassette row still narrow: max %d %d within %v %v", left.MaxW, right.MaxW, textWithin(left), textWithin(right))
	}
	prefix := strings.Repeat("zx81-workshop-demo-", 40)
	longLeft, longRight := cassetteLabel(t, prefix+"part-one.p", "part-"), cassetteLabel(t, prefix+"part-two.p", "part-")
	if longLeft.Text == longRight.Text || !strings.HasSuffix(longLeft.Text, "part-one.p") || !strings.HasSuffix(longRight.Text, "part-two.p") {
		t.Fatalf("long names lost their endings: %q vs %q", longLeft.Text, longRight.Text)
	}
	if !textWithin(longLeft) || !textWithin(longRight) || !strings.Contains(longLeft.Text, "…") {
		t.Fatalf("long cassette name still clipped by max width: %q max %d", longLeft.Text, longLeft.MaxW)
	}
}

// TestHardwareRoomNarrowCassetteNameStaysWhole checks a label the old
// `#s * px` estimate would shorten. Narrow letters are thinner than one em,
// so the name is longer than that estimate and still fits the measured row.
func TestHardwareRoomNarrowCassetteNameStaysWhole(t *testing.T) {
	const tail = "narrow"
	// cassetteLabel opens the room at newRoom's default 1280x720.
	scale := math.Min(1280.0/1120.0, 720.0/630.0)
	px := int(math.Max(12, math.Floor(14*scale)))
	limit := 980.0 * scale
	var name string
	for n := 1; n < 400; n++ {
		candidate := strings.Repeat("i", n) + tail
		if float64(len(candidate))*float64(px) <= limit {
			continue
		}
		width := gfx.MeasureTextWeight(candidate, px, gfx.WeightBold)
		if float64(width) > limit {
			t.Fatalf("narrow name overflowed before the byte estimate failed: %q is %dpx, limit %.1f", candidate, width, limit)
		}
		name = candidate
		break
	}
	if name == "" {
		t.Fatal("could not build a narrow cassette name that fits")
	}
	got := cassetteLabel(t, name, tail)
	if got.Text != name || strings.Contains(got.Text, "…") || !textWithin(got) {
		t.Fatalf("fitting narrow name shortened: %q max %d", got.Text, got.MaxW)
	}
}

// workshopKitSize is the hardware-room content box at a kit menu resolution:
// 5% safe area on each edge, then the 52px hardware footer.
func workshopKitSize(displayW, displayH int) (int, int) {
	inset := func(v int) int { return int(float64(v)*0.05 + 0.5) }
	return displayW - 2*inset(displayW), displayH - 2*inset(displayH) - 52
}

func openWorkshop(t *testing.T, w, h int, active bool) *Instance {
	t.Helper()
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Width: w, Height: h, Services: hardwareData(active),
		StorePath: filepath.Join(t.TempDir(), "room.json"),
		Budget:    Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second},
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return frameHas(f, "Fits this machine") })
	deadline := time.Now().Add(testwait.Timeout)
	now := time.Unix(2000, 0)
	for len(r.Images()) == 0 && time.Now().Before(deadline) {
		now = now.Add(16 * time.Millisecond)
		r.Step(now)
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		time.Sleep(time.Millisecond)
	}
	r.Step(now.Add(16 * time.Millisecond))
	r.Step(now.Add(32 * time.Millisecond))
	return r
}

func assertLabelsFit(t *testing.T, f Frame, shown string, extras ...string) {
	t.Helper()
	var op Op
	var found bool
	for _, candidate := range f.Ops {
		if candidate.Kind == OpText && candidate.Text == shown {
			op = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing %q", shown)
	}
	for _, label := range append([]string{shown}, extras...) {
		for _, weight := range []gfx.Weight{gfx.WeightRegular, gfx.WeightBold} {
			if width := gfx.MeasureTextWeight(label, op.Size, weight); width > op.MaxW {
				t.Fatalf("%q size %d is %dpx, button max %d", label, op.Size, width, op.MaxW)
			}
		}
	}
}

// The cold open must not draw the finished workshop in its first callback.
// The kit's hard budget is 48ms; the first live open was failing at 48.33ms.
func TestHardwareRoomColdOpenIsStaged(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Width: w, Height: h, Services: hardwareData(true),
		StorePath: filepath.Join(t.TempDir(), "room.json"),
		Budget:    Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second},
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(3000, 0)
	first := r.Step(now)
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if len(first.Ops) > 8 || frameHas(first, "Import expansion") || frameHas(first, "Setup preview") || !frameHas(first, "Sinclair ZX81 setup") {
		t.Fatalf("cold first draw built the workshop: ops=%d", len(first.Ops))
	}
	var sawUpper bool
	var upperOps int
	var steps int
	for steps = 1; steps <= 24; steps++ {
		now = now.Add(16 * time.Millisecond)
		start := time.Now()
		f := r.Step(now)
		elapsed := time.Since(start)
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		upper := frameHas(f, "Setup preview")
		full := frameHas(f, "Import expansion")
		if upper && !full {
			sawUpper = true
			upperOps = len(f.Ops)
			t.Logf("upper half ops=%d step=%s", upperOps, elapsed)
		}
		if !upper && !full && len(f.Ops) > 8 {
			t.Fatalf("prep draw recorded %d ops", len(f.Ops))
		}
		if full {
			if !sawUpper {
				t.Fatal("Import expansion arrived in the same draw as the first setup preview")
			}
			if upperOps > 55 {
				t.Fatalf("upper half is still most of the picture: %d ops", upperOps)
			}
			if !frameHas(f, "Setup preview") || !exactText(f, "Import expansion") {
				t.Fatal("staged lower half dropped the upper picture or the import label")
			}
			t.Logf("full picture after %d draws, upper ops=%d final ops=%d step=%s", steps+1, upperOps, len(f.Ops), elapsed)
			return
		}
	}
	t.Fatalf("workshop did not finish staging (upper-only=%v)", sawUpper)
}

// Focus and room size can change after the upper half is presented. The
// lower half must not be appended onto that stale picture, and the draw
// that follows must reuse the rebuilt one.
func TestHardwareRoomStagingRebuildsTheUpperHalf(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	nw, nh := workshopKitSize(1024, 600)
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Width: w, Height: h, Services: hardwareData(true),
		StorePath: filepath.Join(t.TempDir(), "room.json"),
		Budget:    Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second},
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(3100, 0)
	var stale Frame
	for steps := 0; steps < 24; steps++ {
		now = now.Add(16 * time.Millisecond)
		f := r.Step(now)
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		if frameHas(f, "Import expansion") {
			t.Fatal("lower half arrived before the scene could change")
		}
		if frameHas(f, "Setup preview") {
			stale = f
			break
		}
	}
	if !frameHas(stale, "Setup preview") {
		t.Fatal("upper half did not appear")
	}
	if workshopText(t, stale, "Back").Bold {
		t.Fatal("Back started focused")
	}
	r.Resize(nw, nh)
	r.Hover("back")
	wantX, wantY := workshopPoint(nw, nh, 40, 154)
	var rebuilt bool
	var full Frame
	for steps := 0; steps < 8; steps++ {
		now = now.Add(16 * time.Millisecond)
		f := r.Step(now)
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		if frameHas(f, "Setup preview") && !frameHas(f, "Import expansion") {
			preview := workshopText(t, f, "Setup preview")
			if nearPoint(preview.X, preview.Y, wantX, wantY) && workshopText(t, f, "Back").Bold {
				rebuilt = true
			}
			continue
		}
		if frameHas(f, "Import expansion") {
			if !rebuilt {
				t.Fatal("lower half was appended without rebuilding the upper half")
			}
			full = f
			break
		}
	}
	if !frameHas(full, "Import expansion") {
		t.Fatal("workshop did not finish after the scene change")
	}
	preview := workshopText(t, full, "Setup preview")
	if !nearPoint(preview.X, preview.Y, wantX, wantY) {
		t.Fatalf("full picture kept the old layout: got (%.2f,%.2f) want (%.2f,%.2f)", preview.X, preview.Y, wantX, wantY)
	}
	if !workshopText(t, full, "Back").Bold || workshopText(t, full, "Rear expansion").Bold {
		t.Fatal("focus stayed on the upper half's old control")
	}
	now = now.Add(16 * time.Millisecond)
	again := r.Step(now)
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	againPreview := workshopText(t, again, "Setup preview")
	if againPreview.X != preview.X || againPreview.Y != preview.Y || !workshopText(t, again, "Back").Bold || !frameHas(again, "Import expansion") {
		t.Fatal("the following draw did not reuse the rebuilt picture")
	}
}

func workshopText(t *testing.T, f Frame, label string) Op {
	t.Helper()
	for _, op := range f.Ops {
		if op.Kind == OpText && op.Text == label {
			return op
		}
	}
	t.Fatalf("missing %q", label)
	return Op{}
}

func workshopPoint(w, h int, x, y float64) (float32, float32) {
	scale := math.Min(float64(w)/1120, float64(h)/630)
	ox := (float64(w) - 1120*scale) / 2
	oy := (float64(h) - 630*scale) / 2
	return float32(ox + x*scale), float32(oy + y*scale)
}

func nearPoint(x, y, wantX, wantY float32) bool {
	return math.Abs(float64(x-wantX)) < 0.75 && math.Abs(float64(y-wantY)) < 0.75
}

func TestHardwareRoomToolLabelsFitAtKitSize(t *testing.T) {
	for _, display := range [][2]int{{1280, 720}, {1024, 600}} {
		w, h := workshopKitSize(display[0], display[1])
		t.Run(fmt.Sprintf("%dx%d-content-%dx%d", display[0], display[1], w, h), func(t *testing.T) {
			r := openWorkshop(t, w, h, true)
			f := r.Step(time.Unix(3000, 0))
			assertLabelsFit(t, f, "Import expansion")
			assertLabelsFit(t, f, "Refresh", "Checking...")
		})
	}
}

// The kit's draw callback hard-stops at 48ms while a core is running. Host
// wall time is not that budget. This guards the cost the workshop adds: a
// settled frame reuses the presented picture, and a focus change does not
// remeasure the layout.
func TestHardwareRoomSettledDrawStaysCheap(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := openWorkshop(t, w, h, true)
	before := r.measures
	now := time.Unix(4000, 0)
	for i := 0; i < 8; i++ {
		now = now.Add(16 * time.Millisecond)
		f := r.Step(now)
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		if !frameHas(f, "Import expansion") || !frameHas(f, "Running:") {
			t.Fatal("reused frame lost the workshop")
		}
	}
	if got := r.measures - before; got != 0 {
		t.Fatalf("settled draw measured text %d times", got)
	}
	allocs := testing.AllocsPerRun(30, func() {
		now = now.Add(16 * time.Millisecond)
		r.Step(now)
		if r.Err() != nil {
			t.Fatalf("settled step: %v", r.Err())
		}
	})
	t.Logf("settled allocs/step %.1f measures %d", allocs, r.measures-before)
	if allocs > 80 {
		t.Fatalf("settled draw allocs/step %.1f; reuse should stay far under the previous ~1350", allocs)
	}
	r.Hover("import_card")
	measured := r.measures
	focused := r.Step(now.Add(16 * time.Millisecond))
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if delta := r.measures - measured; delta > 6 {
		t.Fatalf("focus redraw measured text %d times", delta)
	}
	if !exactText(focused, "Import expansion") {
		t.Fatal("focus redraw dropped Import expansion")
	}
	// Flip a status string so each step rebuilds the picture at the same
	// size. Layout, focus changes and store writes stay outside the count.
	dirty, err := r.L.LoadString(`message = message == "bench-a" and "bench-b" or "bench-a"`)
	if err != nil {
		t.Fatal(err)
	}
	redraw := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			r.L.Push(dirty)
			if err := r.L.PCall(0, 0, nil); err != nil {
				b.Fatal(err)
			}
			now = now.Add(16 * time.Millisecond)
			r.Step(now)
			if r.Err() != nil {
				b.Fatal(r.Err())
			}
		}
	})
	t.Logf("scene-change redraw allocs/op %d ns/op %d", redraw.AllocsPerOp(), redraw.NsPerOp())
	if redraw.AllocsPerOp() > 80 {
		t.Fatalf("scene-change redraw allocs/op %d; rebuilding command tables was about 660", redraw.AllocsPerOp())
	}
}

func BenchmarkHardwareRoomSettledDraw(b *testing.B) {
	w, h := workshopKitSize(1280, 720)
	var pack Pack
	for _, candidate := range Examples() {
		if candidate.ID == "example.hardware" {
			pack = candidate
			break
		}
	}
	if !pack.Valid() {
		b.Fatal("example.hardware missing")
	}
	r, err := New(pack, Options{
		Width: w, Height: h, Services: hardwareData(true),
		Budget: Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second},
	})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	if err = r.Load(); err != nil {
		b.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	now := time.Unix(5000, 0)
	for !frameHas(r.Step(now), "Fits this machine") {
		now = now.Add(16 * time.Millisecond)
		if r.Err() != nil {
			b.Fatal(r.Err())
		}
		if time.Now().After(deadline) {
			b.Fatal("workshop fixture did not draw")
		}
		time.Sleep(time.Millisecond)
	}
	for len(r.Images()) == 0 && time.Now().Before(deadline) {
		now = now.Add(16 * time.Millisecond)
		r.Step(now)
		time.Sleep(time.Millisecond)
	}
	r.Step(now.Add(16 * time.Millisecond))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now = now.Add(16 * time.Millisecond)
		r.Step(now)
		if r.Err() != nil {
			b.Fatal(r.Err())
		}
	}
}

func BenchmarkHardwareRoomRedraw(b *testing.B) {
	w, h := workshopKitSize(1280, 720)
	var pack Pack
	for _, candidate := range Examples() {
		if candidate.ID == "example.hardware" {
			pack = candidate
			break
		}
	}
	r, err := New(pack, Options{
		Width: w, Height: h, Services: hardwareData(true),
		Budget: Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second},
	})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	if err = r.Load(); err != nil {
		b.Fatal(err)
	}
	now := time.Unix(5000, 0)
	deadline := time.Now().Add(2 * time.Second)
	for !frameHas(r.Step(now), "Fits this machine") {
		now = now.Add(16 * time.Millisecond)
		if time.Now().After(deadline) {
			b.Fatal(r.Err())
		}
		time.Sleep(time.Millisecond)
	}
	for len(r.Images()) == 0 && time.Now().Before(deadline) {
		now = now.Add(16 * time.Millisecond)
		r.Step(now)
		time.Sleep(time.Millisecond)
	}
	r.Step(now)
	dirty, err := r.L.LoadString(`message = message == "bench-a" and "bench-b" or "bench-a"`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.L.Push(dirty)
		if err = r.L.PCall(0, 0, nil); err != nil {
			b.Fatal(err)
		}
		now = now.Add(16 * time.Millisecond)
		r.Step(now)
		if r.Err() != nil {
			b.Fatal(r.Err())
		}
	}
}

// Colours the light appearance owns and the overlay must not draw: dark ink
// and green on a dimmed machine would be near 1:1 contrast.
var lightOnlyColours = map[gfx.Color]string{
	gfx.RGB(244, 240, 232): "paper", gfx.RGB(255, 252, 245): "panel", gfx.RGB(233, 230, 220): "raised",
	gfx.RGB(36, 54, 49): "ink", gfx.RGB(83, 98, 90): "muted", gfx.RGB(206, 207, 192): "line",
	gfx.RGB(35, 90, 70): "accent", gfx.RGB(246, 235, 203): "warm",
}

func overlayScrim(f Frame) bool {
	return f.HasClear && f.Clear == gfx.RGBA(20, 25, 24, 200)
}

// Over a running machine on the kit plane the workshop clears to a
// translucent scrim, draws the dark roles, and leaves the machine visible
// where the illustration was. Turning the flag off restores the opaque room.
func TestHardwareRoomOverlayDrawsOverTheRunningMachine(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := openWorkshop(t, w, h, true)
	// The artwork decodes asynchronously; openWorkshop does not insist on it.
	opaque := stepUntil(t, r, func(f Frame) bool {
		for _, op := range f.Ops {
			if op.Kind == OpImage {
				return true
			}
		}
		return false
	})
	now := time.Unix(5000, 0)
	if !opaque.HasClear || opaque.Clear != gfx.RGB(244, 240, 232) {
		t.Fatalf("host room clear = %+v", opaque.Clear)
	}
	r.SetOverlay(true)
	now = now.Add(16 * time.Millisecond)
	f := r.Step(now)
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if !overlayScrim(f) || !frameHas(f, "Import expansion") || !frameHas(f, "Running:") {
		t.Fatalf("overlay did not repaint the workshop over a scrim: clear %+v", f.Clear)
	}
	for _, op := range f.Ops {
		if op.Kind == OpImage {
			t.Fatalf("overlay drew artwork %q over the machine", op.Image)
		}
		if name, light := lightOnlyColours[op.Color]; light && (op.Kind == OpRect || op.Kind == OpText) {
			t.Fatalf("overlay drew the light %s colour: %+v", name, op)
		}
	}
	if len(f.Ops) > len(opaque.Ops) {
		t.Fatalf("overlay costs more ops than the opaque room: %d > %d", len(f.Ops), len(opaque.Ops))
	}
	running := workshopText(t, f, "Running: Empty socket")
	if running.Color != gfx.RGB(237, 243, 233) {
		t.Fatalf("overlay text colour %+v", running.Color)
	}
	// The settled overlay reuses its picture like the opaque room.
	before := r.measures
	allocs := testing.AllocsPerRun(30, func() {
		now = now.Add(16 * time.Millisecond)
		if g := r.Step(now); !overlayScrim(g) {
			t.Fatal("settled overlay lost its scrim")
		}
	})
	if allocs > 80 || r.measures != before {
		t.Fatalf("settled overlay allocs/step %.1f measures %d", allocs, r.measures-before)
	}
	r.SetOverlay(false)
	back := r.Step(now.Add(16 * time.Millisecond))
	if !back.HasClear || back.Clear != gfx.RGB(244, 240, 232) {
		t.Fatalf("leaving the overlay kept clear %+v", back.Clear)
	}
	var art bool
	for _, op := range back.Ops {
		art = art || op.Kind == OpImage
	}
	if !art {
		t.Fatal("opaque room lost its illustration")
	}
}

// A cold open over a running machine never flashes the opaque paper.
func TestHardwareRoomColdOpenOverlayStartsOnTheScrim(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := newRoom(t, examplePack(t, "example.hardware"), Options{
		Width: w, Height: h, Services: hardwareData(true),
		StorePath: filepath.Join(t.TempDir(), "room.json"),
		Budget:    Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second},
	})
	r.SetOverlay(true)
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(5100, 0)
	for steps := 0; steps < 24; steps++ {
		now = now.Add(16 * time.Millisecond)
		f := r.Step(now)
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
		if !overlayScrim(f) {
			t.Fatalf("draw %d cleared to %+v over the running machine", steps, f.Clear)
		}
		if frameHas(f, "Import expansion") {
			return
		}
	}
	t.Fatal("overlay workshop did not finish staging")
}

// A flip of room.overlay during the staged cold open never keeps presenting
// the opening painted in the other appearance.
func TestHardwareRoomColdOpenFollowsAnOverlayFlip(t *testing.T) {
	for _, start := range []bool{true, false} {
		for flipAt := 1; flipAt <= 15; flipAt++ {
			w, h := workshopKitSize(1280, 720)
			r := newRoom(t, examplePack(t, "example.hardware"), Options{
				Width: w, Height: h, Services: hardwareData(true),
				StorePath: filepath.Join(t.TempDir(), "room.json"),
				Budget:    Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second},
			})
			r.SetOverlay(start)
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			now := time.Unix(5200, 0)
			for step := 0; step < 30; step++ {
				overlay := start
				if step >= flipAt {
					overlay = !start
				}
				r.SetOverlay(overlay)
				now = now.Add(16 * time.Millisecond)
				f := r.Step(now)
				if r.Err() != nil {
					t.Fatal(r.Err())
				}
				if overlayScrim(f) != overlay {
					t.Fatalf("start %v flip at %d: draw %d presented clear %+v with overlay %v", start, flipAt, step, f.Clear, overlay)
				}
			}
		}
	}
}
