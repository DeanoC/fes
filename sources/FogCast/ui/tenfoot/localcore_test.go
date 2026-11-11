package tenfoot

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const tenfootPongID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestHostModeLeavesKitUnset(t *testing.T) {
	prefs := filepath.Join(t.TempDir(), "prefs.json")
	host, err := configuredApp(Options{GFX: "sdl", APIBase: "http://127.0.0.1:9", PrefsPath: prefs})
	if err != nil {
		t.Fatal(err)
	}
	if host.localCores != nil || host.localFeed != nil {
		t.Fatal("host mode constructed a local-control client")
	}
	kit, err := configuredApp(Options{GFX: "menu-display", APIBase: "http://127.0.0.1:9", PrefsPath: filepath.Join(t.TempDir(), "kit.json")})
	if err != nil {
		t.Fatal(err)
	}
	if kit.localCores == nil || kit.localFeed == nil {
		t.Fatal("menu-display left the kit client unset")
	}

	h := newRoomHost(t)
	var pack rooms.Pack
	for _, candidate := range rooms.Examples() {
		if candidate.ID == "example.fes-cores" {
			pack = candidate
		}
	}
	if !pack.Valid() {
		t.Fatal("example.fes-cores missing")
	}
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "home.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), "")
	app.SetHomeRooms(true)
	app.SetHomeRoom("example.fes-cores")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	// With no kit the host library alone decides; it has no machine
	// entries here, so every family is Unavailable and nothing launches.
	snap := waitFor(t, app, "families unavailable", homeFamilySettled)
	if d := snap.Room.Destination; d.Kind != rooms.KindFamily || d.Availability != rooms.AvailUnavailable {
		t.Fatalf("host Home %+v", d)
	}
	if app.localCores != nil {
		t.Fatal("home room on the host received a kit client")
	}
}

func TestLeaseReleaseKeepsUnrelatedStatus(t *testing.T) {
	app := &App{status: "Loading library", kitLeaseHave: true, kitLease: KitLeaseStatus{State: "held", Owner: "other"}}
	app.mu.Lock()
	wasForeign := app.foreignKitLeaseLocked()
	app.kitLease = KitLeaseStatus{State: "free"}
	app.clearLeaseRefusalAfterTransitionLocked(wasForeign)
	app.mu.Unlock()
	if got := app.status; got != "Loading library" {
		t.Fatalf("unrelated status cleared on lease release: %q", got)
	}
}

func TestPresentHoldPausesAfterTheStartingFrame(t *testing.T) {
	rec := &recordingMenu{}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	var hold presentHold
	snap := Snapshot{Room: RoomSnapshot{Open: true}, HostPresentsPaused: true}
	if skip, err := hold.skip(context.Background(), dev, snap); skip || err != nil {
		t.Fatal("the starting frame should still be presented")
	}
	if skip, err := hold.skip(context.Background(), dev, snap); !skip || err != nil {
		t.Fatal("later frames should skip")
	}
	dev.Present()
	if calls, _, _ := rec.snapshot(); calls != 0 {
		t.Fatalf("paused present submitted %d frames", calls)
	}
	snap.HostPresentsPaused = false
	if skip, err := hold.skip(context.Background(), dev, snap); skip || err != nil {
		t.Fatal("resume should present")
	}
}

func TestPresentHoldLateAdoptionSkipsArmedFrame(t *testing.T) {
	rec := &recordingMenu{}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	var hold presentHold
	snap := Snapshot{HostPresentsPaused: true, HostLateAdopt: true}
	if skip, err := hold.skip(context.Background(), dev, snap); !skip || err != nil {
		t.Fatalf("late adoption should drain and skip immediately: skip=%v err=%v", skip, err)
	}
	dev.Present()
	if calls, _, _ := rec.snapshot(); calls != 0 {
		t.Fatalf("late adoption submitted %d menu frames", calls)
	}
}

type staleFailMenu struct {
	mu    sync.Mutex
	fail  error
	calls int
}

func (c *staleFailMenu) Status(context.Context) (menudisplay.Status, error) {
	return menudisplay.Status{Available: true, Generation: 4, Width: 1280, Height: 720, Stride: 5120, ByteCount: menudisplay.FrameBytes}, nil
}

func (c *staleFailMenu) Present(context.Context, uint64, []byte) (menudisplay.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.fail != nil {
		return menudisplay.Result{}, c.fail
	}
	return menudisplay.Result{Generation: 4, DisplayedSequence: uint64(c.calls)}, nil
}

func (c *staleFailMenu) snapshot() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestPresentHoldLateAdoptionPausesAfterStaleFailure(t *testing.T) {
	rec := &staleFailMenu{fail: errors.New("stale menu")}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	dev.Present()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := dev.WaitIdle(ctx); err == nil || !strings.Contains(err.Error(), "stale menu") {
		t.Fatalf("failed frame WaitIdle = %v", err)
	}
	dev.Resume()
	var hold presentHold
	snap := Snapshot{HostPresentsPaused: true, HostLateAdopt: true}
	if skip, err := hold.skip(context.Background(), dev, snap); !skip || err != nil {
		t.Fatalf("late adoption after a stale failure: skip=%v err=%v", skip, err)
	}
	before := rec.snapshot()
	dev.Present()
	if dev.FramePending() || rec.snapshot() != before {
		t.Fatalf("late adoption left the menu presenting: pending=%v calls=%d", dev.FramePending(), rec.snapshot())
	}
}

func TestKitSessionPadStaysLocalWhenTheFeedFails(t *testing.T) {
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	feed := &errPadFeed{}
	app.SetKitLocal(&fakeLocalCores{}, feed)
	app.mu.Lock()
	app.session.State = "active"
	app.mu.Unlock()
	t.Cleanup(app.Stop)

	now := time.Unix(50, 0)
	if !app.HandleLocalPad(padButton(remoteinput.ButtonA, true), now) {
		t.Fatal("active kit session let the menu take the pad")
	}
	if feed.count(remoteinput.ButtonA, remoteinput.ActionPress) != 1 {
		t.Fatal("failed local write was not attempted")
	}
	if h.launchCount() != 0 || h.inputCount() != 0 {
		t.Fatalf("failed local write fell back to the host launches=%d inputs=%d", h.launchCount(), h.inputCount())
	}

	app.SetKitLocal(&fakeLocalCores{}, nil)
	if !app.HandleLocalPad(padButton(remoteinput.ButtonB, true), now.Add(time.Second)) {
		t.Fatal("missing local socket fell through to the menu")
	}
	if h.launchCount() != 0 || h.inputCount() != 0 {
		t.Fatalf("missing local socket fell back to the host launches=%d inputs=%d", h.launchCount(), h.inputCount())
	}
}

func roomTextHas(s Snapshot, text string) bool {
	for _, op := range s.Room.Frame.Ops {
		if op.Kind == rooms.OpText && strings.Contains(op.Text, text) {
			return true
		}
	}
	return false
}

func padButton(code remoteinput.Code, down bool) remoteinput.Event {
	action := remoteinput.ActionRelease
	if down {
		action = remoteinput.ActionPress
	}
	return remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: action, Code: code}
}

type fakeLocalCores struct {
	mu      sync.Mutex
	cores   []localcores.Core
	listErr error
}

func (f *fakeLocalCores) List(context.Context) ([]localcores.Core, error) {
	if f == nil {
		return []localcores.Core{}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]localcores.Core, len(f.cores))
	copy(out, f.cores)
	return out, nil
}

type fakePadFeed struct {
	mu     sync.Mutex
	events []remoteinput.Event
}

func (f *fakePadFeed) Send(e remoteinput.Event, _ time.Time) error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
	return nil
}

func (f *fakePadFeed) Close() {}

func (f *fakePadFeed) saw(code remoteinput.Code, action remoteinput.Action) bool {
	return f.count(code, action) > 0
}

func (f *fakePadFeed) count(code remoteinput.Code, action remoteinput.Action) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.Code == code && e.Action == action {
			n++
		}
	}
	return n
}

// errPadFeed records the frame and reports that the local socket write failed.
type errPadFeed struct {
	fakePadFeed
}

func (f *errPadFeed) Send(e remoteinput.Event, now time.Time) error {
	_ = f.fakePadFeed.Send(e, now)
	return errors.New("local socket is down")
}

func (f *fakePadFeed) snapshot() []remoteinput.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]remoteinput.Event(nil), f.events...)
}
