//go:build linux

package tenfoot

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
)

func TestNativeShutdownRetiresAndConfirmsBeforeStop(t *testing.T) {
	var calls []string
	err := shutdownNativeInputSequence(context.Background(), true, func() error { calls = append(calls, "retire"); return nil }, func(context.Context) error { calls = append(calls, "confirmed"); return nil }, func() { calls = append(calls, "stop") }, func() { calls = append(calls, "close") })
	if err != nil || !reflect.DeepEqual(calls, []string{"retire", "confirmed", "stop", "close"}) {
		t.Fatalf("shutdown %v %v", calls, err)
	}
}

func TestNativeShutdownTimeoutAndMissingConfirmationRemainFailures(t *testing.T) {
	for _, missing := range []bool{false, true} {
		var calls []string
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var await func(context.Context) error
		if !missing {
			await = func(ctx context.Context) error { calls = append(calls, "wait"); <-ctx.Done(); return ctx.Err() }
		}
		err := shutdownNativeInputSequence(ctx, true, func() error { calls = append(calls, "retire"); return nil }, await, func() { calls = append(calls, "stop") }, func() { calls = append(calls, "close") })
		if err == nil || calls[0] != "retire" || calls[len(calls)-2] != "stop" || calls[len(calls)-1] != "close" {
			t.Fatalf("shutdown %v %v", calls, err)
		}
	}
}

func TestNativeShutdownCanConfirmAfterDisplayContextCancellation(t *testing.T) {
	displayCtx, cancelDisplay := context.WithCancel(context.Background())
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	cancelDisplay()
	if displayCtx.Err() == nil {
		t.Fatal("display context did not cancel")
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), time.Second)
	defer cancelCleanup()
	err := shutdownNativeInputSequence(cleanupCtx, true, func() error { return nil }, func(ctx context.Context) error {
		if workerCtx.Err() != nil || ctx.Err() != nil {
			t.Fatal("worker or cleanup context canceled with display")
		}
		return nil
	}, cancelWorker, func() {})
	if err != nil || workerCtx.Err() == nil {
		t.Fatal("worker did not survive until cleanup and then stop")
	}
}

type gateBoundaryFake struct {
	gen               uint64
	permit            bool
	faults            []nativeKeyboardFault
	edges             []nativeRawEdge
	repeats           []uint16
	retired           []localhid.DeviceID
	uiRetired         []localhid.DeviceID
	published         []nativeRawBoundary
	decision          nativeEdgeDecision
	uiActions         []uint16
	uiNames           []string
	discarded         []uint64
	uiSnapshotAllowed bool
	uiSnapshots       map[localhid.DeviceID][]uint16
}

func (f *gateBoundaryFake) requestNativeKeyboardQuarantine(x nativeKeyboardFault) {
	f.faults = append(f.faults, x)
	f.gen++
}
func (f *gateBoundaryFake) nativeKeyboardSnapshotPermit() (uint64, bool) { return f.gen, f.permit }
func (f *gateBoundaryFake) publishNativeKeyboardBoundary(gen uint64, b nativeRawBoundary) error {
	if gen != f.gen || !f.permit {
		return errors.New("stale permit")
	}
	f.published = append(f.published, b)
	return nil
}
func (f *gateBoundaryFake) recordNativeKeyboardEdge(gen uint64, e nativeRawEdge) (nativeEdgeDecision, error) {
	if gen != f.gen {
		return nativeEdgeDecision{}, errors.New("stale permit")
	}
	f.edges = append(f.edges, e)
	return f.decision, nil
}
func (f *gateBoundaryFake) recordNativeKeyboardRepeat(gen uint64, id localhid.DeviceID, code uint16) (nativeUIDisposition, uint64, error) {
	if gen != f.gen {
		return nativeUISuppress, 0, errors.New("stale permit")
	}
	f.repeats = append(f.repeats, code)
	return nativeUIAllow, 1, nil
}
func (f *gateBoundaryFake) retireNativeKeyboardDevice(gen uint64, id localhid.DeviceID) error {
	if gen != f.gen {
		return errors.New("stale permit")
	}
	f.retired = append(f.retired, id)
	return nil
}
func (f *gateBoundaryFake) handleNativeKeyboardUIOnly(_ localhid.DeviceID, code uint16, name string, _ uint8, _ bool, _ uint64, _ time.Time) bool {
	f.uiActions = append(f.uiActions, code)
	f.uiNames = append(f.uiNames, name)
	return false
}
func (f *gateBoundaryFake) discardNativeKeyboardUIAction(token uint64) error {
	f.discarded = append(f.discarded, token)
	return nil
}
func (f *gateBoundaryFake) retireNativeKeyboardUIOnly(id localhid.DeviceID) {
	f.uiRetired = append(f.uiRetired, id)
}
func (*gateBoundaryFake) quarantineNativeKeyboardUIOnly(localhid.DeviceID)          {}
func (*gateBoundaryFake) registerNativeKeyboardUIOnly(localhid.DeviceID, int) error { return nil }
func (f *gateBoundaryFake) nativeKeyboardUIOnlySnapshotAllowed() bool               { return f.uiSnapshotAllowed }
func (f *gateBoundaryFake) publishNativeKeyboardUIOnly(id localhid.DeviceID, held []uint16) error {
	if f.uiSnapshots == nil {
		f.uiSnapshots = map[localhid.DeviceID][]uint16{}
	}
	f.uiSnapshots[id] = append([]uint16(nil), held...)
	return nil
}

func gateID(n byte) (id localhid.DeviceID) { id[0] = n; return }
func newGate() (*nativeKeyboardGate, *gateBoundaryFake) {
	f := &gateBoundaryFake{decision: nativeEdgeDecision{UI: nativeUIAllow, Guest: nativeGuestBuffered}}
	f.decision.UIAction = 1
	return &nativeKeyboardGate{boundary: f, publisher: [16]byte{1}}, f
}
func gateCaps(codes ...uint16) []byte {
	b := make([]byte, nativeKeyBitmapBytes)
	for _, code := range codes {
		b[code/8] |= 1 << (code % 8)
	}
	return b
}

func TestNativeGateDispatchIsUIOnlyAndSuppressesHeldKeys(t *testing.T) {
	g, f := newGate()
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	if err := g.add(gateID(1), in); err != nil {
		t.Fatal(err)
	}
	reconcileNativeKeyboardUI(in, []*nativeInput{in}, []uint16{42}, true, func(Command) {})
	if handled, quit, err := g.dispatch(gateID(1), 1, 42, 0, time.Time{}); !handled || quit || err != nil {
		t.Fatalf("held release: %v %v %v", handled, quit, err)
	}
	if len(f.edges) != 1 || len(f.uiActions) != 0 || len(f.discarded) != 1 {
		t.Fatal("suppressed release reached UI or missed guest bookkeeping")
	}
	if handled, quit, err := g.dispatch(gateID(1), 1, 42, 1, time.Time{}); !handled || quit || err != nil {
		t.Fatalf("fresh press: %v %v %v", handled, quit, err)
	}
	if len(f.edges) != 2 || len(f.uiActions) != 1 || f.uiActions[0] != 42 {
		t.Fatal("fresh edge was not UI-only and buffered once")
	}
	f.decision.Guest = nativeGuestOrdinary
	if handled, _, err := g.dispatch(gateID(1), 1, 42, 0, time.Time{}); !handled || err == nil || len(f.uiActions) != 1 {
		t.Fatal("delegated gate fell back to ordinary keyboard")
	}
}

func TestNativeGateDeniedReleaseCleansOnlyItsSource(t *testing.T) {
	g, f := newGate()
	a := &nativeInput{fd: 10, kind: InputKeyboard, keysHeld: map[uint16]string{42: "leftshift"}, shifts: map[uint16]bool{42: true}, commands: map[uint16]Command{42: CmdUp}, localKeyboardUI: &nativeKeyboardUIState{}}
	b := &nativeInput{fd: 11, kind: InputKeyboard, keysHeld: map[uint16]string{42: "leftshift"}, shifts: map[uint16]bool{42: true}, commands: map[uint16]Command{42: CmdUp}}
	if err := g.add(gateID(1), a); err != nil {
		t.Fatal(err)
	}
	if err := g.add(gateID(2), b); err != nil {
		t.Fatal(err)
	}
	a.keysHeld, a.shifts, a.commands = map[uint16]string{42: "leftshift"}, map[uint16]bool{42: true}, map[uint16]Command{42: CmdUp}
	b.keysHeld, b.shifts, b.commands = map[uint16]string{42: "leftshift"}, map[uint16]bool{42: true}, map[uint16]Command{42: CmdUp}
	a.localKeyboardUI.admitted[42] = true
	f.decision = nativeEdgeDecision{UI: nativeUISuppress, Guest: nativeGuestBuffered}
	if handled, _, err := g.dispatch(gateID(1), 1, 42, 0, time.Time{}); !handled || err != nil {
		t.Fatalf("release %v %v", handled, err)
	}
	if len(a.keysHeld) != 0 || len(a.shifts) != 0 || len(a.commands) != 0 || len(f.uiActions) != 0 {
		t.Fatal("denied release retained native state or dispatched UI")
	}
	if !b.shifts[42] || b.commands[42] != CmdUp || b.keysHeld[42] == "" {
		t.Fatal("release cleared another source")
	}
}

func TestNativeGateDroppedEventMakesOnlyThatNativeSourceUnknown(t *testing.T) {
	g, _ := newGate()
	a := &nativeInput{fd: 10, kind: InputKeyboard, keysHeld: map[uint16]string{42: "shift"}, shifts: map[uint16]bool{42: true}, commands: map[uint16]Command{42: CmdUp}}
	b := &nativeInput{fd: 11, kind: InputKeyboard, keysHeld: map[uint16]string{42: "shift"}, shifts: map[uint16]bool{42: true}, commands: map[uint16]Command{42: CmdUp}}
	if err := g.add(gateID(1), a); err != nil {
		t.Fatal(err)
	}
	if err := g.add(gateID(2), b); err != nil {
		t.Fatal(err)
	}
	a.keysHeld, a.shifts, a.commands = map[uint16]string{42: "shift"}, map[uint16]bool{42: true}, map[uint16]Command{42: CmdUp}
	b.keysHeld, b.shifts, b.commands = map[uint16]string{42: "shift"}, map[uint16]bool{42: true}, map[uint16]Command{42: CmdUp}
	if handled, _, err := g.dispatch(gateID(1), 0, 3, 0, time.Time{}); !handled || err != nil {
		t.Fatalf("drop: %v %v", handled, err)
	}
	if a.localKeyboardUI == nil || !a.localKeyboardUI.unknown || len(a.keysHeld) != 0 || len(a.shifts) != 0 || len(a.commands) != 0 {
		t.Fatal("lost source was not neutralized locally")
	}
	if !b.shifts[42] || b.keysHeld[42] == "" || b.commands[42] != CmdUp {
		t.Fatal("another source was changed")
	}
}

func TestNativeGateInvalidKeyRecordMakesOnlyThatSourceUnknown(t *testing.T) {
	g, _ := newGate()
	a, b := &nativeInput{fd: 10, kind: InputKeyboard}, &nativeInput{fd: 11, kind: InputKeyboard}
	if err := g.add(gateID(1), a); err != nil {
		t.Fatal(err)
	}
	if err := g.add(gateID(2), b); err != nil {
		t.Fatal(err)
	}
	a.keysHeld, a.shifts, a.commands = map[uint16]string{42: "shift"}, map[uint16]bool{42: true}, map[uint16]Command{42: CmdUp}
	b.keysHeld, b.shifts, b.commands = map[uint16]string{42: "shift"}, map[uint16]bool{42: true}, map[uint16]Command{42: CmdUp}
	if handled, _, err := g.dispatch(gateID(1), 1, 42, 3, time.Time{}); !handled || err == nil {
		t.Fatal("invalid value accepted")
	}
	if a.localKeyboardUI == nil || !a.localKeyboardUI.unknown || len(a.keysHeld) != 0 || len(a.shifts) != 0 || len(a.commands) != 0 {
		t.Fatal("invalid source remained trusted")
	}
	if !b.shifts[42] || b.commands[42] != CmdUp || b.keysHeld[42] == "" {
		t.Fatal("invalid source changed another keyboard")
	}
}

func TestNativeGateUIOnlySnapshotArmsFirstPressAndSuppressesHeldKeys(t *testing.T) {
	g, f := newGate()
	f.uiSnapshotAllowed = true
	a := &nativeInput{fd: 10, kind: InputKeyboard}
	if err := g.add(gateID(1), a); err != nil {
		t.Fatal(err)
	}
	reads := 0
	caps := func(int) ([]byte, error) { return gateCaps(30, 42, 88), nil }
	held := func(int) ([]byte, error) { reads++; return gateCaps(42, 88), nil }
	reconcile := func(in *nativeInput, held []uint16) {
		reconcileNativeKeyboardUI(in, []*nativeInput{a}, held, true, func(Command) {})
	}
	unknown := func(in *nativeInput) { reconcileNativeKeyboardUI(in, []*nativeInput{a}, nil, false, func(Command) {}) }
	g.snapshotUI(caps, held, reconcile, unknown)
	if reads != 1 || !reflect.DeepEqual(f.uiSnapshots[gateID(1)], []uint16{42, 88}) {
		t.Fatal("UI snapshot not published")
	}
	if handled, _, err := g.dispatch(gateID(1), 1, 30, 1, time.Time{}); !handled || err != nil || len(f.uiActions) != 1 {
		t.Fatal("first fresh UI press was suppressed")
	}
	if handled, _, err := g.dispatch(gateID(1), 1, 42, 0, time.Time{}); !handled || err != nil || len(f.uiActions) != 1 {
		t.Fatal("held Shift release caused UI action")
	}
	if handled, _, err := g.dispatch(gateID(1), 1, 88, 0, time.Time{}); !handled || err != nil || len(f.uiActions) != 1 {
		t.Fatal("held Stop release caused UI action")
	}
	if handled, _, err := g.dispatch(gateID(1), 1, 88, 1, time.Time{}); !handled || err != nil || len(f.uiNames) != 2 || f.uiNames[1] != "f12" {
		t.Fatal("fresh F12 did not reach the UI-only dispatcher")
	}
	b := &nativeInput{fd: 11, kind: InputKeyboard}
	if err := g.add(gateID(2), b); err != nil {
		t.Fatal(err)
	}
	g.snapshotUI(caps, held, func(in *nativeInput, held []uint16) {
		reconcileNativeKeyboardUI(in, []*nativeInput{a, b}, held, true, func(Command) {})
	}, unknown)
	if reads != 2 || !a.localKeyboardUI.admitted[30] {
		t.Fatal("hotplug re-snapshotted or changed existing source")
	}
	f.uiSnapshotAllowed = false
	c := &nativeInput{fd: 12, kind: InputKeyboard}
	if err := g.add(gateID(3), c); err != nil {
		t.Fatal(err)
	}
	g.snapshotUI(caps, held, reconcile, unknown)
	if reads != 2 {
		t.Fatal("UI snapshot performed while guest route was active")
	}
}

func TestNativeGateUIOnlySnapshotFailsClosedOnHybridAndUncertainIOCTL(t *testing.T) {
	for _, hybrid := range []bool{true, false} {
		g, f := newGate()
		f.uiSnapshotAllowed = true
		in := &nativeInput{fd: 10, kind: InputKeyboard, pending: snapshotTestEvent(1, 42, 0)}
		if err := g.add(gateID(1), in); err != nil {
			t.Fatal(err)
		}
		reads := 0
		g.snapshotUI(func(int) ([]byte, error) {
			if hybrid {
				return gateCaps(42, 272), nil
			}
			return gateCaps(42), nil
		}, func(int) ([]byte, error) { reads++; return nil, errors.New("ioctl failed") }, func(*nativeInput, []uint16) { t.Fatal("failed snapshot reconciled as known") }, func(in *nativeInput) { reconcileNativeKeyboardUI(in, []*nativeInput{in}, nil, false, func(Command) {}) })
		if len(f.uiSnapshots) != 0 || in.localKeyboardUI == nil || !in.localKeyboardUI.unknown {
			t.Fatal("failed UI snapshot was treated as known")
		}
		if hybrid && reads != 0 {
			t.Fatal("hybrid reached state ioctl")
		}
		if !hybrid {
			if reads != 1 || !g.sources[gateID(1)].uncertain || len(in.pending) != 0 {
				t.Fatal("uncertain ioctl retained stale key events")
			}
			if _, used, err := g.event(gateID(1), 1, 42, 0); err != nil || !used || len(f.edges) != 0 {
				t.Fatal("uncertain queued release escaped")
			}
		}
	}
}

func TestNativePollGateAdoptsDistinctOpenIncarnationsWithoutFallback(t *testing.T) {
	g, f := newGate()
	var next byte
	ins := &nativeInputs{localKeyboardGate: g, localKeyboardNewID: func() (localhid.DeviceID, error) { next++; return gateID(next), nil }}
	a := &nativeInput{fd: 10, kind: InputKeyboard}
	if err := ins.adoptNativeKeyboard(a); err != nil {
		t.Fatal(err)
	}
	first := a.localKeyboardID
	if err := g.retire(first); err != nil {
		t.Fatal(err)
	}
	b := &nativeInput{fd: 10, kind: InputKeyboard}
	if err := ins.adoptNativeKeyboard(b); err != nil {
		t.Fatal(err)
	}
	if first == b.localKeyboardID {
		t.Fatal("reused fd reused device incarnation")
	}
	unknown := &nativeInput{fd: 11, kind: InputKeyboard}
	if handled, _, err := ins.gatedKeyboardRecord(unknown, 1, 30, 1, time.Time{}); !handled || err == nil {
		t.Fatal("unregistered keyboard fell through")
	}
	if len(f.edges) != 0 {
		t.Fatal("unregistered source reached guest")
	}
	plain := &nativeInputs{}
	if handled, _, err := plain.gatedKeyboardRecord(b, 1, 30, 1, time.Time{}); handled || err != nil {
		t.Fatal("nil gate changed ordinary path")
	}
}

func TestNativePollGateInstallsBeforeAdoptingExistingAndHotplug(t *testing.T) {
	g, _ := newGate()
	var next byte
	a, b := &nativeInput{fd: 10, kind: InputKeyboard}, &nativeInput{fd: 11, kind: InputKeyboard}
	ins := &nativeInputs{devices: []*nativeInput{a, b}, localKeyboardNewID: func() (localhid.DeviceID, error) { next++; return gateID(next), nil }}
	if err := ins.installNativeKeyboardGate(g); err != nil {
		t.Fatal(err)
	}
	if a.localKeyboardID == (localhid.DeviceID{}) || b.localKeyboardID == (localhid.DeviceID{}) || a.localKeyboardID == b.localKeyboardID {
		t.Fatal("existing keyboards not adopted distinctly")
	}
	c := &nativeInput{fd: 12, kind: InputKeyboard}
	if err := ins.adoptNativeKeyboard(c); err != nil || c.localKeyboardID == a.localKeyboardID || c.localKeyboardID == b.localKeyboardID {
		t.Fatal("hotplug incarnation not distinct")
	}
	badGate, _ := newGate()
	broken := &nativeInputs{devices: []*nativeInput{{fd: 20, kind: InputKeyboard}}, localKeyboardNewID: func() (localhid.DeviceID, error) { return localhid.DeviceID{}, errors.New("random unavailable") }}
	if err := broken.installNativeKeyboardGate(badGate); err == nil || broken.localKeyboardGate == nil {
		t.Fatal("partial installation fell back")
	}
	if handled, _, err := broken.gatedKeyboardRecord(broken.devices[0], 1, 30, 1, time.Time{}); !handled || err == nil {
		t.Fatal("failed adoption fell through")
	}
}

func TestNativePollGateRefusesPreexistingLegacyKeyboardHolds(t *testing.T) {
	g, _ := newGate()
	in := &nativeInput{fd: 10, kind: InputKeyboard, keysHeld: map[uint16]string{30: "a"}, commands: map[uint16]Command{30: CmdLeft}}
	ins := &nativeInputs{devices: []*nativeInput{in}, localKeyboardNewID: func() (localhid.DeviceID, error) { return gateID(1), nil }}
	if err := ins.installNativeKeyboardGate(g); err == nil {
		t.Fatal("legacy hold was adopted")
	}
	if ins.localKeyboardGate == nil || in.keysHeld[30] != "a" || in.commands[30] != CmdLeft {
		t.Fatal("failed adoption fell back or lost legacy state")
	}
	if handled, _, err := ins.gatedKeyboardRecord(in, 1, 30, 0, time.Time{}); !handled || err == nil {
		t.Fatal("legacy source fell through after failed adoption")
	}
}

func TestNativeGateSnapshotWaitsForPermitAndPreservesRawState(t *testing.T) {
	g, f := newGate()
	a := &nativeInput{fd: 10, kind: InputKeyboard, pending: bytes.Join([][]byte{snapshotTestEvent(1, 113, 0), snapshotTestEvent(0, 0, 0)}, nil)}
	b := &nativeInput{fd: 11, kind: InputKeyboard}
	if err := g.add(gateID(1), a); err != nil {
		t.Fatal(err)
	}
	if err := g.add(gateID(2), b); err != nil {
		t.Fatal(err)
	}
	reads := 0
	caps := func(int) ([]byte, error) { return gateCaps(42, 113), nil }
	held := func(fd int) ([]byte, error) {
		reads++
		if fd == 10 {
			return gateCaps(42, 113), nil
		}
		return gateCaps(42), nil
	}
	reconciled := map[int][]uint16{}
	reconcile := func(in *nativeInput, raw []uint16) { reconciled[in.fd] = raw }
	if ok, err := g.snapshot(caps, held, reconcile, func(*nativeInput) {}); err != nil || ok || reads != 0 {
		t.Fatalf("without permit: %v %v reads %d", ok, err, reads)
	}
	f.permit = true
	if ok, err := g.snapshot(caps, held, reconcile, func(*nativeInput) {}); err != nil || !ok {
		t.Fatalf("snapshot: %v %v", ok, err)
	}
	if reads != 2 || !reflect.DeepEqual(reconciled[10], []uint16{42, 113}) || !reflect.DeepEqual(reconciled[11], []uint16{42}) {
		t.Fatalf("reconcile %v reads %d", reconciled, reads)
	}
	if !bytes.Equal(a.pending, snapshotTestEvent(0, 0, 0)) || len(f.published) != 1 || len(f.published[0].Devices) != 2 {
		t.Fatal("boundary/pending mismatch")
	}
	for _, id := range []localhid.DeviceID{gateID(1), gateID(2)} {
		if _, used, err := g.event(id, 1, 42, 0); err != nil || !used {
			t.Fatalf("release %v %v", used, err)
		}
	}
	if len(f.edges) != 2 || f.edges[0].Sequence >= f.edges[1].Sequence || f.edges[0].Device == f.edges[1].Device {
		t.Fatalf("edge ordering: %+v", f.edges)
	}
}

func TestNativeGateRejectsHybridBeforeAnyStateIOCTL(t *testing.T) {
	g, f := newGate()
	f.permit = true
	_ = g.add(gateID(1), &nativeInput{fd: 10, kind: InputKeyboard})
	_ = g.add(gateID(2), &nativeInput{fd: 11, kind: InputKeyboard})
	reads := 0
	caps := func(fd int) ([]byte, error) {
		if fd == 11 {
			return gateCaps(30, 272), nil
		}
		return gateCaps(30), nil
	}
	_, err := g.snapshot(caps, func(int) ([]byte, error) { reads++; return gateCaps(30), nil }, func(*nativeInput, []uint16) {}, func(*nativeInput) {})
	if err == nil || reads != 0 || len(f.published) != 0 || !g.dirty {
		t.Fatalf("hybrid err %v reads %d", err, reads)
	}
}

func TestNativeGateDroppedAndSourceRetirement(t *testing.T) {
	g, f := newGate()
	f.permit = true
	_ = g.add(gateID(1), &nativeInput{fd: 10, kind: InputKeyboard})
	_ = g.add(gateID(2), &nativeInput{fd: 11, kind: InputKeyboard})
	_, _, _ = g.event(gateID(1), 0, 3, 0)
	if _, used, err := g.event(gateID(1), 1, 42, 1); err != nil || !used || len(f.edges) != 0 {
		t.Fatal("dropped interval dispatched")
	}
	reads := 0
	caps := func(int) ([]byte, error) { return gateCaps(42), nil }
	get := func(int) ([]byte, error) { reads++; return gateCaps(), nil }
	if ok, err := g.snapshot(caps, get, func(*nativeInput, []uint16) {}, func(*nativeInput) {}); err != nil || ok || reads != 0 {
		t.Fatal("snapshot before SYN_REPORT")
	}
	_, _, _ = g.event(gateID(1), 0, 0, 0)
	if ok, err := g.snapshot(caps, get, func(*nativeInput, []uint16) {}, func(*nativeInput) {}); err != nil || !ok {
		t.Fatalf("recovery %v", err)
	}
	if err := g.retire(gateID(1)); err != nil {
		t.Fatal(err)
	}
	if len(f.retired) != 1 || f.retired[0] != gateID(1) || len(f.uiRetired) != 1 || g.sources[gateID(2)] == nil {
		t.Fatal("retirement crossed devices")
	}
}

func TestNativeGatePermitChangeFailsClosed(t *testing.T) {
	g, f := newGate()
	f.permit = true
	_ = g.add(gateID(1), &nativeInput{fd: 10, kind: InputKeyboard})
	reconciled := false
	_, err := g.snapshot(func(int) ([]byte, error) { return gateCaps(30), nil }, func(int) ([]byte, error) { f.gen++; return gateCaps(30), nil }, func(*nativeInput, []uint16) { reconciled = true }, func(*nativeInput) {})
	if err == nil || len(f.published) != 0 || !g.dirty || !reconciled {
		t.Fatal("stale permit published")
	}
}

func TestNativeGateRepeatIsUIOnlyAndNotPhysicalEdge(t *testing.T) {
	g, f := newGate()
	_ = g.add(gateID(1), &nativeInput{fd: 10, kind: InputKeyboard})
	seq := g.sequence
	decision, consumed, err := g.event(gateID(1), 1, 113, 2)
	if err != nil || !consumed || decision.UI != nativeUIAllow || decision.Guest != nativeGuestQuarantined || len(f.repeats) != 1 || len(f.edges) != 0 || g.sequence != seq {
		t.Fatalf("repeat decision %+v, consumed %v, error %v", decision, consumed, err)
	}
}

func TestNativeGateReconcilesBeforeLaterSnapshotFailure(t *testing.T) {
	g, f := newGate()
	f.permit = true
	a := &nativeInput{fd: 10, kind: InputKeyboard, pending: snapshotTestEvent(1, 113, 0), commands: map[uint16]Command{113: CmdHome}}
	b := &nativeInput{fd: 11, kind: InputKeyboard}
	_ = g.add(gateID(1), a)
	_ = g.add(gateID(2), b)
	caps := func(int) ([]byte, error) { return gateCaps(113), nil }
	get := func(fd int) ([]byte, error) {
		if fd == 11 {
			return nil, errors.New("removed")
		}
		return gateCaps(), nil
	}
	reconcile := func(in *nativeInput, raw []uint16) {
		if len(raw) == 0 {
			delete(in.commands, 113)
		}
	}
	if ok, err := g.snapshot(caps, get, reconcile, func(*nativeInput) {}); err == nil || ok {
		t.Fatal("second ioctl failure accepted")
	}
	if len(a.commands) != 0 || len(a.pending) != 0 || len(f.published) != 0 || !g.dirty {
		t.Fatal("flushed key-up left stale native command or guest route ready")
	}
}

func TestNativeGateUnknownStateAfterAttemptedIOCTL(t *testing.T) {
	for _, invalidBitmap := range []bool{false, true} {
		t.Run(map[bool]string{false: "ioctl-error", true: "bad-bitmap"}[invalidBitmap], func(t *testing.T) {
			g, f := newGate()
			f.permit = true
			in := &nativeInput{fd: 10, kind: InputKeyboard, commands: map[uint16]Command{113: CmdHome},
				pending: bytes.Join([][]byte{snapshotTestEvent(1, 113, 0), snapshotTestEvent(1, 113, 1), snapshotTestEvent(0, 0, 0)}, nil)}
			_ = g.add(gateID(1), in)
			unknownCalls, knownCalls := 0, 0
			_, err := g.snapshot(func(int) ([]byte, error) { return gateCaps(113), nil }, func(int) ([]byte, error) {
				if invalidBitmap {
					return []byte{0}, nil
				}
				return nil, errors.New("ambiguous ioctl")
			}, func(*nativeInput, []uint16) { knownCalls++ }, func(got *nativeInput) {
				unknownCalls++
				delete(got.commands, 113) // caller also latches release-to-arm
			})
			if err == nil || knownCalls != 0 || unknownCalls != 1 || len(in.commands) != 0 || len(f.published) != 0 || !g.dirty || !bytes.Equal(in.pending, snapshotTestEvent(0, 0, 0)) {
				t.Fatal("unknown post-ioctl state was treated as a valid snapshot")
			}
		})
	}
}

func TestNativeGateDiscardsUncertainKernelEventsUntilDrain(t *testing.T) {
	g, f := newGate()
	f.permit = true
	in := &nativeInput{fd: 10, kind: InputKeyboard}
	_ = g.add(gateID(1), in)
	_, err := g.snapshot(func(int) ([]byte, error) { return gateCaps(113), nil }, func(int) ([]byte, error) {
		return nil, errors.New("ioctl outcome unknown")
	}, func(*nativeInput, []uint16) {}, func(*nativeInput) {})
	if err == nil {
		t.Fatal("ioctl failure accepted")
	}
	// These could be pre-ioctl kernel records. Neither may arm the UI.
	_, _, _ = g.event(gateID(1), 1, 113, 0)
	_, _, _ = g.event(gateID(1), 1, 113, 1)
	if len(f.edges) != 0 {
		t.Fatal("stale kernel records reached app bookkeeping")
	}
	in.pending = []byte{1}
	if err = g.afterReadEAGAIN(gateID(1)); err == nil {
		t.Fatal("partial drain accepted")
	}
	in.pending = nil
	if err = g.afterReadEAGAIN(gateID(1)); err != nil {
		t.Fatal(err)
	}
	// A genuinely later release/press can be processed under release-to-arm.
	_, _, _ = g.event(gateID(1), 1, 113, 0)
	_, _, _ = g.event(gateID(1), 1, 113, 1)
	if len(f.edges) != 2 || f.edges[0].Down || !f.edges[1].Down || !g.dirty {
		t.Fatal("fresh events were not sequenced or guest quarantine was cleared")
	}
}
