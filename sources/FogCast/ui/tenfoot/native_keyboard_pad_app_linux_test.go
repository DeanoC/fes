//go:build linux

package tenfoot

import (
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

func mappedPadApp(t *testing.T) (*App, padOnlyAuthority) {
	t.Helper()
	a := readyNativeBindingApp(t)
	a.mu.Lock()
	defer a.mu.Unlock()
	nativeSetDescribedPadOnly(a)
	if err := a.nativeUIRaw.replace(nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{{ID: gateID(1), Held: []uint16{42}}, {ID: gateID(2)}}}); err != nil {
		t.Fatal(err)
	}
	b, err := a.padOnlyAuthorityLocked(time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	a.nativePadRetiring = true
	a.nativePadRetirementEpoch = 1
	a.nativePadRetirementBinding = b
	a.nativePadConnectionFence = &localfeed.PadWriteFence{}
	if err = a.nativePadStartMappingLocked(1, b); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func TestNativeMappedPadAppEdgesHeldAndDeviceRetirement(t *testing.T) {
	a, b := mappedPadApp(t)
	edge := func(id byte, code uint16, down bool) {
		t.Helper()
		if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(id), Code: code, Down: down}); err != nil {
			t.Fatal(err)
		}
	}
	edge(1, 42, false) // snapshot-held shift must not emit a release
	a.mu.Lock()
	if len(a.nativeMappedPad.queue) != 0 {
		t.Fatal("snapshot hold escaped")
	}
	a.mu.Unlock()
	edge(1, 30, true)
	edge(2, 30, true)
	a.mu.Lock()
	first, ok := a.nativeMappedPad.next(1, b)
	if !ok || first.device != gateID(1) || first.event.Device != remoteinput.DeviceGamepad {
		t.Fatal("first mapped press missing")
	}
	if !a.nativePadCompleteMappedLocked(first, nil) {
		t.Fatal("first completion")
	}
	second, ok := a.nativeMappedPad.next(1, b)
	if !ok || second.device != gateID(2) {
		t.Fatal("second mapped press missing")
	}
	if !a.nativePadCompleteMappedLocked(second, nil) {
		t.Fatal("second completion")
	}
	a.nativePadRetireDeviceLocked(gateID(1))
	retirement, ok := a.nativeMappedPad.next(1, b)
	if !ok || !retirement.retire || retirement.device != gateID(1) {
		t.Fatal("source retirement missing")
	}
	if _, ok := a.nativeMappedPad.keys[nativeMappedPadKey{device: gateID(2), raw: 30}]; !ok {
		t.Fatal("other keyboard lost")
	}
	if !a.nativePadCompleteMappedLocked(retirement, nil) {
		t.Fatal("retirement completion")
	}
	a.mu.Unlock()
	edge(2, 30, false)
	a.mu.Lock()
	defer a.mu.Unlock()
	release, ok := a.nativeMappedPad.next(1, b)
	if !ok || release.event.Action != remoteinput.ActionRelease || release.device != gateID(2) {
		t.Fatal("source release missing")
	}
}

func TestNativeMappedPadAppStaleErrorCannotRevokeSuccessor(t *testing.T) {
	a, b := mappedPadApp(t)
	if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(1), Code: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(2), Code: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old, ok := a.nativeMappedPad.next(1, b)
	if !ok || !a.nativePadCompleteMappedLocked(old, nil) {
		t.Fatal("old completion")
	}
	next, ok := a.nativeMappedPad.next(1, b)
	if !ok {
		t.Fatal("next missing")
	}
	if a.nativePadCompleteMappedLocked(old, errors.New("late")) || a.nativePadConnectionFence.Revoked() || a.nativeMappedPad.invalid {
		t.Fatal("stale error revoked successor")
	}
	if !a.nativePadCompleteMappedLocked(next, nil) {
		t.Fatal("successor rejected")
	}
}

func TestNativeMappedPadFocusFenceSuppressesUntilRelease(t *testing.T) {
	a, b := mappedPadApp(t)
	if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(1), Code: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	old := a.nativePadMappingFence
	a.nativePadFenceMappingLocked()
	if !old.Revoked() {
		t.Fatal("old mapped generation live")
	}
	barrier, ok := a.nativeMappedPad.next(1, b)
	if !ok || !barrier.retire || barrier.device != (localhid.DeviceID{}) {
		t.Fatal("focus retirement missing")
	}
	if !a.nativePadCompleteMappedLocked(barrier, nil) {
		t.Fatal("barrier completion")
	}
	a.mu.Unlock()
	if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(1), Code: 30, Down: false}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	if len(a.nativeMappedPad.queue) != 0 {
		t.Fatal("held release replayed")
	}
	a.mu.Unlock()
	if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(1), Code: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.nativeMappedPad.queue) != 1 {
		t.Fatal("fresh press not admitted")
	}
}

func TestNativeMappedPadBackspaceRetiresMappedKeysAndStops(t *testing.T) {
	a, b := mappedPadApp(t)
	if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(1), Code: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	decision, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(2), Code: 14, Down: true})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	barrier, ok := a.nativeMappedPad.next(1, b)
	a.mu.Unlock()
	if !ok || !barrier.retire || barrier.device != (localhid.DeviceID{}) {
		t.Fatal("Backspace failed to fence mapped hold")
	}
	a.handleNativeKeyboardUIOnly(gateID(2), 14, nativeKeyName(14), nativeHIDUsage(14), true, decision.UIAction, time.Now())
	a.mu.Lock()
	phase := a.stopPhase
	a.mu.Unlock()
	if phase != "stopping" {
		t.Fatalf("Backspace failed to stop: %s", phase)
	}
	a.Stop()
}

func TestNativeMappedPadSearchFencesBeforeFocusChanges(t *testing.T) {
	a, b := mappedPadApp(t)
	if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(1), Code: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.nativePadMappingFence
	a.openSearchLocked()
	if !a.searchOpen || !old.Revoked() {
		t.Fatal("search transition did not synchronously revoke mapping")
	}
	barrier, ok := a.nativeMappedPad.next(1, b)
	if !ok || !barrier.retire || barrier.device != (localhid.DeviceID{}) {
		t.Fatal("search did not queue mapped retirement")
	}
}

func TestPadOnlyAuthorityRejectsForeignAndDeniedLease(t *testing.T) {
	for _, native := range []bool{true, false} {
		for _, tc := range []struct{ name, state, owner, leaseState, leaseOwner, code string }{
			{name: "foreign health", state: "held", owner: "remote"},
			{name: "busy health", state: "busy"},
			{name: "foreign lease", leaseState: "held", leaseOwner: "remote"},
			{name: "lease denied", code: "KIT_LEASE_DENIED"},
			{name: "lease busy", code: "KIT_LEASE_BUSY"},
		} {
			t.Run(tc.name+map[bool]string{true: "/native", false: "/sdl"}[native], func(t *testing.T) {
				a := readyNativeBindingApp(t)
				defer a.Stop()
				a.mu.Lock()
				defer a.mu.Unlock()
				nativeSetDescribedPadOnly(a)
				a.kitLocalKeyboardUnavailable = !native
				if _, err := a.padOnlyAuthorityLocked(time.Now(), native); err != nil {
					t.Fatalf("baseline rejected: %v", err)
				}
				a.health.Connection.State = tc.state
				a.health.Connection.Owner = tc.owner
				a.kitLease.State = tc.leaseState
				a.kitLease.Owner = tc.leaseOwner
				a.kitLease.ErrorCode = tc.code
				if _, err := a.padOnlyAuthorityLocked(time.Now(), native); err == nil {
					t.Fatal("foreign or denied authority accepted")
				}
			})
		}
	}
}

func TestNativePadNewForeignLeaseFencesInFlightSource(t *testing.T) {
	a, b := mappedPadApp(t)
	if _, err := a.recordNativeKeyboardEdge(1, nativeRawEdge{Device: gateID(1), Code: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	action, ok := a.nativeMappedPad.next(1, b)
	if !ok {
		t.Fatal("missing in-flight press")
	}
	connection, mapping := a.nativePadConnectionFence, action.mappingFence
	a.kitLease.State = "held"
	a.kitLease.Owner = "remote"
	a.nativePadObserveLeaseLocked()
	if !connection.Revoked() || !mapping.Revoked() || !a.nativeMappedPad.invalid {
		t.Fatal("new foreign lease did not fence in-flight source")
	}
	if a.nativePadCompleteMappedLocked(action, errors.New("revoked")) {
		t.Fatal("revoked write accepted")
	}
}
