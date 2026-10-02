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
	r.Input("right")
	r.Input("right")
	r.Input("right")
	r.Input("right")
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
