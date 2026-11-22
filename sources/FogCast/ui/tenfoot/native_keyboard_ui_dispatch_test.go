package tenfoot

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/remoteinput"
)

func rawDeviceID(n byte) (id localhid.DeviceID) { id[0] = n; return }

func TestNativeUIRawBoundarySuppressesPerDeviceUntilRelease(t *testing.T) {
	var l nativeKeyboardUIRawLedger
	b := nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{
		{ID: rawDeviceID(1), Held: []uint16{29, 113}}, {ID: rawDeviceID(2)},
	}}
	if err := l.replace(b); err != nil {
		t.Fatal(err)
	}
	if l.ctrlHeld() {
		t.Fatal("snapshot-held Ctrl acted as a new press")
	}
	if err := l.edge(rawDeviceID(1), 29, false, false); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(rawDeviceID(1), 29, true, true); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(rawDeviceID(2), 29, true, true); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(rawDeviceID(1), 29, false, false); err != nil {
		t.Fatal(err)
	}
	if !l.ctrlHeld() {
		t.Fatal("release of one keyboard cleared another keyboard's Ctrl")
	}
	l.retire(rawDeviceID(1))
	if !l.ctrlHeld() {
		t.Fatal("retiring one keyboard cleared another")
	}
	l.quarantine()
	if l.ctrlHeld() {
		t.Fatal("quarantined hold remained admitted")
	}
	if err := l.edge(rawDeviceID(2), 29, false, false); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(rawDeviceID(2), 29, true, true); err != nil {
		t.Fatal(err)
	}
	if !l.ctrlHeld() {
		t.Fatal("fresh post-release Ctrl did not arm")
	}
}

func TestNativeUIRawUnknownAndInvalidBoundaryFailClosed(t *testing.T) {
	var l nativeKeyboardUIRawLedger
	id := rawDeviceID(1)
	if err := l.addUnknown(id); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(id, 29, true, true); err != nil {
		t.Fatal(err)
	}
	if l.ctrlHeld() {
		t.Fatal("unknown held Ctrl was assumed up")
	}
	if err := l.edge(id, 29, false, false); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(id, 29, true, true); err != nil {
		t.Fatal(err)
	}
	if !l.ctrlHeld() {
		t.Fatal("post-release Ctrl did not arm")
	}
	bad := nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{{ID: id, Held: []uint16{113, 113}}}}
	if err := l.replace(bad); err == nil || !l.ctrlHeld() {
		t.Fatal("invalid snapshot changed existing ledger")
	}
	l.retire(id)
	if err := l.addUnknown(rawDeviceID(2)); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(rawDeviceID(2), 29, true, true); err != nil {
		t.Fatal(err)
	}
	if l.ctrlHeld() {
		t.Fatal("replacement inherited an armed key")
	}
}

type uiOnlyTransport func(*http.Request) (*http.Response, error)

func (f uiOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNativeUIOnlyStopQuarantinesWithoutLegacyKeyboardWrite(t *testing.T) {
	client := &http.Client{Transport: uiOnlyTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline test") })}
	a := NewApp(NewClient("http://example.invalid", client), 1280, 720, 20)
	defer a.Stop()
	id := rawDeviceID(1)
	a.mu.Lock()
	a.delegatedV2Active = true
	a.nativeKeyboardGeneration = 1
	a.nativeKeyboardWake = make(chan struct{}, 1)
	a.session = hostclient.SessionResult{State: "active", CoreKeyboardHID: true}
	if err := a.nativeUIRaw.replace(nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{{ID: id}}}); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	if err := a.nativeUIRaw.edge(id, 88, true, true); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	token := a.issueNativeUIActionLocked(1, id, 88, true, false)
	if token == 0 {
		a.mu.Unlock()
		t.Fatal("UI action token missing")
	}
	a.rememberPlayHIDLocked(remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Code: remoteinput.KeyA, Action: remoteinput.ActionPress})
	a.mu.Unlock()
	if a.handleNativeKeyboardUIOnly(id, 88, "f12", hidKeyboardF12, true, token, time.Now()) {
		t.Fatal("F12 unexpectedly quit")
	}
	a.mu.Lock()
	phase, quarantine, active, tail := a.stopPhase, a.nativeKeyboardQuarantined, a.delegatedV2Active, a.playHIDTail
	a.mu.Unlock()
	if phase != "stopping" || !quarantine || !active || tail != nil {
		t.Fatalf("stop=%s quarantined=%v active=%v legacy-tail=%v", phase, quarantine, active, tail != nil)
	}
}

func TestNativeUIOnlyRejectsQuarantinedAndStaleActions(t *testing.T) {
	a := NewApp(nil, 1280, 720, 20)
	defer a.Stop()
	id := rawDeviceID(1)
	a.mu.Lock()
	a.delegatedV2Active, a.nativeKeyboardGeneration = true, 1
	a.session = hostclient.SessionResult{State: "active", CoreKeyboardHID: true}
	if err := a.nativeUIRaw.replace(nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{{ID: id}}}); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	if err := a.nativeUIRaw.edge(id, 88, true, true); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	old := a.issueNativeUIActionLocked(1, id, 88, true, false)
	a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
	a.mu.Unlock()
	a.handleNativeKeyboardUIOnly(id, 88, "f12", hidKeyboardF12, true, old, time.Now())
	a.mu.Lock()
	phase := a.stopPhase
	a.mu.Unlock()
	if phase == "stopping" {
		t.Fatal("quarantined UI token stopped the session")
	}

	a.mu.Lock()
	if err := a.nativeUIRaw.edge(id, 88, false, false); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	if err := a.nativeUIRaw.edge(id, 88, true, true); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	stale := a.issueNativeUIActionLocked(a.nativeKeyboardGeneration, id, 88, true, false)
	if err := a.nativeUIRaw.edge(id, 88, false, false); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	if err := a.nativeUIRaw.edge(id, 88, true, true); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()
	a.handleNativeKeyboardUIOnly(id, 88, "f12", hidKeyboardF12, true, stale, time.Now())
	a.mu.Lock()
	phase = a.stopPhase
	a.mu.Unlock()
	if phase == "stopping" {
		t.Fatal("old key revision authorized a later press")
	}
}

func TestNativeUIPhysicalReleaseRetiresRepeatWithoutDispatch(t *testing.T) {
	for _, delayedToken := range []bool{false, true} {
		t.Run(map[bool]string{false: "consumed", true: "stale-token"}[delayedToken], func(t *testing.T) {
			a := NewApp(nil, 1280, 720, 20)
			defer a.Stop()
			id := rawDeviceID(1)
			now := time.Unix(100, 0)
			a.mu.Lock()
			a.delegatedV2Active, a.nativeKeyboardGeneration = true, 1
			if err := a.replaceNativeUIRawLocked(nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{{ID: id}}}); err != nil {
				a.mu.Unlock()
				t.Fatal(err)
			}
			a.repeat.Down(CmdLeft, now) // surviving legacy pad
			if err := a.recordNativeUIRawLocked(id, 106, true, true); err != nil {
				a.mu.Unlock()
				t.Fatal(err)
			}
			if _, err := a.repeat.DownNative([16]byte(id), 106, CmdRight, now.Add(time.Millisecond)); err != nil {
				a.mu.Unlock()
				t.Fatal(err)
			}
			if err := a.recordNativeUIRawLocked(id, 106, false, false); err != nil {
				a.mu.Unlock()
				t.Fatal(err)
			}
			var token uint64
			if delayedToken {
				token = a.issueNativeUIActionLocked(1, id, 106, false, false)
				a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
			}
			a.mu.Unlock()
			if delayedToken {
				a.handleNativeKeyboardUIOnly(id, 106, "right", nativeHIDUsage(106), false, token, now)
			}
			if a.repeat.Held() != CmdLeft {
				t.Fatal("physical release left native repeat or removed pad")
			}
			if got := a.repeat.Tick(now.Add(repeatDelay + 2*time.Millisecond)); got != CmdLeft {
				t.Fatalf("surviving pad repeat=%v", got)
			}
		})
	}
}

func TestNativeUIOSKMovementUsesOnlySharedRepeater(t *testing.T) {
	a := NewApp(nil, 1280, 720, 20)
	defer a.Stop()
	id := rawDeviceID(1)
	now := time.Unix(100, 0)
	a.mu.Lock()
	a.delegatedV2Active, a.nativeKeyboardGeneration, a.searchOpen = true, 1, true
	if err := a.replaceNativeUIRawLocked(nativeRawBoundary{Publisher: [16]byte{1}, Devices: []nativeRawDevice{{ID: id}}}); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	if err := a.recordNativeUIRawLocked(id, 106, true, true); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	press := a.issueNativeUIActionLocked(1, id, 106, true, false)
	a.mu.Unlock()
	a.handleNativeKeyboardUIOnly(id, 106, "right", nativeHIDUsage(106), true, press, now)
	a.mu.Lock()
	afterPress := a.searchField.Snapshot().FocusID
	repeat := a.issueNativeUIActionLocked(1, id, 106, true, true)
	a.mu.Unlock()
	a.handleNativeKeyboardUIOnly(id, 106, "right", nativeHIDUsage(106), true, repeat, now.Add(repeatDelay))
	a.mu.Lock()
	afterRawRepeat := a.searchField.Snapshot().FocusID
	a.mu.Unlock()
	if afterRawRepeat != afterPress {
		t.Fatal("raw repeat moved OSK outside the shared arbiter")
	}
	if cmd := a.repeat.Tick(now.Add(repeatDelay + time.Millisecond)); cmd != CmdRight {
		t.Fatalf("shared repeat=%v", cmd)
	} else {
		a.HandleCommand(cmd, now.Add(repeatDelay+time.Millisecond))
	}
	a.mu.Lock()
	afterTick := a.searchField.Snapshot().FocusID
	a.mu.Unlock()
	if afterTick == afterPress {
		t.Fatal("shared repeat did not move OSK")
	}
}
