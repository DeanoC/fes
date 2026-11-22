//go:build linux

package tenfoot

import (
	"bytes"
	"errors"
	"sort"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
)

// nativeKeyboardGate is owned by the native event loop. It remains separate
// from ordinary input unless an App boundary explicitly installs it. No
// method opens a device or starts a goroutine. Device IDs identify open
// incarnations; fd numbers must never be used as identities.
type nativeKeyboardGate struct {
	boundary   nativeKeyboardBoundary
	publisher  [16]byte
	sequence   uint64
	generation uint64
	dirty      bool
	sources    map[localhid.DeviceID]*nativeKeyboardGateSource
}

type nativeKeyboardGateSource struct {
	input     *nativeInput
	dropped   bool
	uncertain bool
	uiDirty   bool
}

func (g *nativeKeyboardGate) fault(f nativeKeyboardFault) {
	g.dirty = true
	g.boundary.requestNativeKeyboardQuarantine(f)
}

func (g *nativeKeyboardGate) advance() (uint64, error) {
	if g.sequence == ^uint64(0) {
		g.fault(nativeFaultOverflow)
		return 0, errors.New("native capture sequence exhausted")
	}
	g.sequence++
	return g.sequence, nil
}

func (g *nativeKeyboardGate) add(id localhid.DeviceID, in *nativeInput) error {
	if g.boundary == nil || g.publisher == [16]byte{} || id == (localhid.DeviceID{}) || in == nil || in.kind != InputKeyboard {
		return errors.New("invalid native keyboard source")
	}
	if g.sources == nil {
		g.sources = make(map[localhid.DeviceID]*nativeKeyboardGateSource)
	}
	if len(g.sources) >= localhid.MaxDevices || g.sources[id] != nil {
		g.fault(nativeFaultOverflow)
		return errors.New("native keyboard source overflow or duplicate identity")
	}
	for _, source := range g.sources {
		if source.input == in || source.input.fd == in.fd {
			g.fault(nativeFaultHotplug)
			return errors.New("native keyboard descriptor already owned")
		}
	}
	if _, err := g.advance(); err != nil {
		return err
	}
	if err := g.boundary.registerNativeKeyboardUIOnly(id, in.fd); err != nil {
		g.fault(nativeFaultAuthority)
		return err
	}
	g.sources[id] = &nativeKeyboardGateSource{input: in, uiDirty: true}
	reconcileNativeKeyboardUI(in, []*nativeInput{in}, nil, false, func(Command) {})
	g.fault(nativeFaultHotplug)
	return nil
}

func (g *nativeKeyboardGate) quarantineSourceUI(id localhid.DeviceID, source *nativeKeyboardGateSource) {
	source.uiDirty = true
	g.boundary.quarantineNativeKeyboardUIOnly(id)
	devices := make([]*nativeInput, 0, len(g.sources))
	for _, other := range g.sources {
		devices = append(devices, other.input)
	}
	// The App has retired this source's repeat contributors. Clear its
	// native bookkeeping without synthesizing an action or guest write.
	reconcileNativeKeyboardUI(source.input, devices, nil, false, func(Command) {})
}

// snapshotUI is local UI bookkeeping only. The native event loop is the
// sole fd owner and cannot concurrently publish a guest snapshot; App also
// requires guest delivery already fenced. It never sends a BEGIN or edge.
func (g *nativeKeyboardGate) snapshotUI(capabilities, held func(int) ([]byte, error), reconcile func(*nativeInput, []uint16), unknown func(*nativeInput)) {
	if !g.boundary.nativeKeyboardUIOnlySnapshotAllowed() || capabilities == nil || held == nil || reconcile == nil || unknown == nil {
		return
	}
	for id, source := range g.sources {
		if !source.uiDirty || source.dropped {
			continue
		}
		source.uiDirty = false
		caps, err := capabilities(source.input.fd)
		var before []byte
		if err == nil {
			_, before, err = snapshotNativeKeyboard(source.input.pending, caps, func() ([]byte, error) { return make([]byte, nativeKeyBitmapBytes), nil })
		}
		if err != nil {
			g.quarantineSourceUI(id, source)
			source.uiDirty = false
			g.fault(nativeFaultCapability)
			continue
		}
		attempted := false
		raw, pending, err := snapshotNativeKeyboard(source.input.pending, caps, func() ([]byte, error) { attempted = true; return held(source.input.fd) })
		if err != nil {
			if attempted {
				source.input.pending = before
				source.uncertain = true
				unknown(source.input)
			}
			g.quarantineSourceUI(id, source)
			source.uiDirty = false
			g.fault(nativeFaultDesync)
			continue
		}
		source.input.pending = pending
		source.uncertain = false
		reconcile(source.input, raw)
		if err := g.boundary.publishNativeKeyboardUIOnly(id, raw); err != nil {
			g.quarantineSourceUI(id, source)
			source.uiDirty = false
			g.fault(nativeFaultAuthority)
		}
	}
}

// event processes one decoded event. During desynchronization all records
// through SYN_REPORT are discarded. Repeats are not new physical edges.
// The caller must respect both returned dispositions and use the App's
// UI-only path whenever the guest disposition is not ordinary.
func (g *nativeKeyboardGate) event(id localhid.DeviceID, typ, code uint16, value int32) (nativeEdgeDecision, bool, error) {
	source := g.sources[id]
	if source == nil {
		g.fault(nativeFaultAuthority)
		return nativeEdgeDecision{}, true, errors.New("unknown native keyboard source")
	}
	if typ == 0 && code == 3 { // SYN_DROPPED
		if _, err := g.advance(); err != nil {
			g.quarantineSourceUI(id, source)
			return nativeEdgeDecision{}, true, err
		}
		source.dropped = true
		g.quarantineSourceUI(id, source)
		g.fault(nativeFaultDesync)
		return nativeEdgeDecision{}, true, nil
	}
	if source.dropped {
		if typ == 0 && code == 0 { // SYN_REPORT
			source.dropped = false
		}
		return nativeEdgeDecision{}, true, nil
	}
	if source.uncertain && typ == 1 {
		// An ioctl failure may have left older key events in the kernel
		// queue; those events cannot arm UI or reach the guest.
		return nativeEdgeDecision{}, true, nil
	}
	if typ != 1 {
		return nativeEdgeDecision{}, false, nil
	}
	if code == 0 || code >= 256 || (value != 0 && value != 1 && value != 2) {
		g.quarantineSourceUI(id, source)
		g.fault(nativeFaultCapability)
		return nativeEdgeDecision{}, true, errors.New("invalid native keyboard edge")
	}
	if value == 2 { // Repeat can reach UI but never becomes a guest edge.
		gen, _ := g.boundary.nativeKeyboardSnapshotPermit()
		ui, token, err := g.boundary.recordNativeKeyboardRepeat(gen, id, code)
		if err != nil || ui > nativeUIAllow || (ui == nativeUIAllow) != (token != 0) {
			g.quarantineSourceUI(id, source)
			g.fault(nativeFaultAuthority)
			return nativeEdgeDecision{}, true, errors.New("invalid native keyboard repeat decision")
		}
		return nativeEdgeDecision{UI: ui, Guest: nativeGuestQuarantined, UIAction: token}, true, nil
	}
	seq, err := g.advance()
	if err != nil {
		g.quarantineSourceUI(id, source)
		return nativeEdgeDecision{}, true, err
	}
	gen, _ := g.boundary.nativeKeyboardSnapshotPermit()
	decision, err := g.boundary.recordNativeKeyboardEdge(gen, nativeRawEdge{Device: id, Sequence: seq, Code: code, Down: value == 1})
	if err != nil {
		g.quarantineSourceUI(id, source)
		g.fault(nativeFaultAuthority)
		return nativeEdgeDecision{}, true, err
	}
	if decision.UI > nativeUIAllow || decision.Guest > nativeGuestBuffered ||
		(decision.UI == nativeUIAllow) != (decision.UIAction != 0) {
		g.quarantineSourceUI(id, source)
		g.fault(nativeFaultAuthority)
		return nativeEdgeDecision{}, true, errors.New("invalid native edge decision")
	}
	return decision, true, nil
}

// dispatch handles the native event-loop boundary. Its caller must not invoke the
// legacy combined guest/UI path for a handled record, including errors.
// The boundary owns guest buffering; this function performs UI-only dispatch.
func (g *nativeKeyboardGate) dispatch(id localhid.DeviceID, typ, code uint16, value int32, now time.Time) (handled, quit bool, err error) {
	decision, handled, err := g.event(id, typ, code, value)
	if err != nil || !handled {
		return handled, false, err
	}
	if decision.Guest == nativeGuestOrdinary {
		if source := g.sources[id]; source != nil {
			g.quarantineSourceUI(id, source)
		}
		g.fault(nativeFaultAuthority)
		if decision.UIAction != 0 {
			_ = g.boundary.discardNativeKeyboardUIAction(decision.UIAction)
		}
		return true, false, errors.New("ordinary keyboard route unavailable through delegated gate")
	}
	if typ != 1 || value < 0 || value > 2 {
		return true, false, nil
	}
	source := g.sources[id]
	if source == nil {
		return true, false, errors.New("missing native source")
	}
	ui := nativeKeyboardUIEdge(source.input, code, value, decision.UI == nativeUIAllow)
	if ui.cleanup {
		// The App boundary has already retired this source's repeat hold on
		// every key-up. Clear native per-source bookkeeping without invoking
		// legacy guest releases or affecting another keyboard/pad.
		delete(source.input.keysHeld, code)
		delete(source.input.shifts, code)
		delete(source.input.commands, code)
	}
	if !ui.dispatch {
		if decision.UIAction != 0 {
			_ = g.boundary.discardNativeKeyboardUIAction(decision.UIAction)
		}
		return true, false, nil
	}
	name := nativeKeyName(code)
	if code == nativeKeyF12 {
		name = "f12"
	}
	return true, g.boundary.handleNativeKeyboardUIOnly(id, code, name, nativeHIDUsage(code), value != 0, decision.UIAction, now), nil
}

// afterReadEAGAIN may be called only by the single native loop immediately
// after it has drained this same fd through EAGAIN. All previously read
// records must have been consumed, and SYN_DROPPED recovery must be complete.
// Future real releases can then participate in the App's release-to-arm UI
// policy; this is not a physical snapshot and does not ready the guest.
func (g *nativeKeyboardGate) afterReadEAGAIN(id localhid.DeviceID) error {
	source := g.sources[id]
	if source == nil || source.dropped || len(source.input.pending) != 0 {
		return errors.New("native input drain is incomplete")
	}
	source.uncertain = false
	return nil
}

// retire accounts only the removed incarnation. No guest release is posted:
// the App owns token-bound neutralization and must retain quarantine on an
// uncertain result. UI cleanup is isolated to the removed source.
func (g *nativeKeyboardGate) retire(id localhid.DeviceID) error {
	if g.sources[id] == nil {
		g.fault(nativeFaultAuthority)
		return errors.New("unknown native keyboard source")
	}
	if _, err := g.advance(); err != nil {
		return err
	}
	g.fault(nativeFaultHotplug)
	gen, _ := g.boundary.nativeKeyboardSnapshotPermit()
	g.boundary.retireNativeKeyboardUIOnly(id)
	err := g.boundary.retireNativeKeyboardDevice(gen, id)
	delete(g.sources, id)
	return err
}

// snapshot performs the all-device boundary only after App confirms a
// neutralized source and grants a current generation. The injected readers
// must inspect capabilities and issue EVIOCGKEY on the given already-open fd.
// reconcile updates only native per-device UI bookkeeping, without a guest
// write or a synthesized UI action. unknown cancels that source's UI holds
// without clicks and requires release-to-arm, without asserting any key is
// up. Both callbacks are infallible, bounded, local bookkeeping. Any failure
// keeps the guest route quarantined.
func (g *nativeKeyboardGate) snapshot(capabilities, held func(int) ([]byte, error), reconcile func(*nativeInput, []uint16), unknown func(*nativeInput)) (bool, error) {
	if !g.dirty {
		return false, nil
	}
	gen, permitted := g.boundary.nativeKeyboardSnapshotPermit()
	if !permitted {
		return false, nil
	}
	if capabilities == nil || held == nil || reconcile == nil || unknown == nil || len(g.sources) == 0 {
		g.fault(nativeFaultCapability)
		return false, errors.New("native snapshot is incomplete")
	}
	ids := make([]localhid.DeviceID, 0, len(g.sources))
	for id, source := range g.sources {
		if source.dropped {
			return false, nil
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	caps := make(map[localhid.DeviceID][]byte, len(ids))
	beforeIOCTL := make(map[localhid.DeviceID][]byte, len(ids))
	for _, id := range ids {
		source := g.sources[id]
		var err error
		caps[id], err = capabilities(source.input.fd)
		if err == nil {
			caps[id] = bytes.Clone(caps[id])
			// Validate all capability and already-read pending streams before
			// issuing any state ioctl, including for a later hybrid device.
			_, beforeIOCTL[id], err = snapshotNativeKeyboard(source.input.pending, caps[id], func() ([]byte, error) {
				return make([]byte, nativeKeyBitmapBytes), nil
			})
		}
		if err != nil {
			g.fault(nativeFaultCapability)
			return false, err
		}
	}
	boundary := nativeRawBoundary{Publisher: g.publisher, Sequence: g.sequence}
	for _, id := range ids {
		source := g.sources[id]
		attempted := false
		raw, pending, err := snapshotNativeKeyboard(source.input.pending, caps[id], func() ([]byte, error) {
			attempted = true
			return held(source.input.fd)
		})
		if err != nil {
			if attempted {
				g.boundary.quarantineNativeKeyboardUIOnly(id)
				// The ioctl may have flushed the kernel queue even on error.
				// Do not let already-read, pre-attempt key edges arm UI state.
				source.input.pending = beforeIOCTL[id]
				source.uncertain = true
				unknown(source.input)
			}
			g.fault(nativeFaultDesync)
			return false, err
		}
		// The kernel has already flushed this fd's prior EV_KEY records;
		// retain the matching userspace boundary even if a later fd fails.
		source.input.pending = pending
		source.uncertain = false
		// Until the complete multi-device guest boundary is accepted, a
		// later failure leaves App UI state unknown for this fd.
		source.uiDirty = true
		g.boundary.quarantineNativeKeyboardUIOnly(id)
		// Native UI state must be repaired immediately: later devices can fail
		// or the App permit can change after this fd's queued release was flushed.
		// This callback is bounded, local bookkeeping and cannot fail or post.
		reconcile(source.input, raw)
		if current, ok := g.boundary.nativeKeyboardSnapshotPermit(); !ok || current != gen {
			g.fault(nativeFaultAuthority)
			return false, errors.New("native snapshot permit changed")
		}
		boundary.Devices = append(boundary.Devices, nativeRawDevice{ID: id, FD: source.input.fd, Held: raw})
	}
	if err := g.boundary.publishNativeKeyboardBoundary(gen, boundary); err != nil {
		g.fault(nativeFaultAuthority)
		return false, err
	}
	for _, id := range ids {
		g.sources[id].uiDirty = false
	}
	g.generation, g.dirty = gen, false
	return true, nil
}
