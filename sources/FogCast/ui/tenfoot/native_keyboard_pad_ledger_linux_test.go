//go:build linux

package tenfoot

import (
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/remoteinput"
)

func nativeMappedPadPress() remoteinput.Event {
	return remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
}

func TestNativeMappedPadLedgerSuppressesHeldAndPreservesAliases(t *testing.T) {
	var l nativeMappedPadLedger
	b := padOnlyAuthority{TargetID: "kit"}
	if err := l.begin(1, b); err != nil {
		t.Fatal(err)
	}
	a, c := gateID(1), gateID(2)
	if err := l.edge(1, b, a, 42, true, false, nativeMappedPadPress()); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(1, b, a, 42, false, false, remoteinput.Event{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.next(1, b); ok {
		t.Fatal("snapshot-suppressed hold reached pad")
	}
	for _, key := range []struct {
		id   byte
		code uint16
	}{{1, 30}, {2, 31}} {
		if err := l.edge(1, b, gateID(key.id), key.code, true, true, nativeMappedPadPress()); err != nil {
			t.Fatal(err)
		}
	}
	first, ok := l.next(1, b)
	if !ok || first.device != a {
		t.Fatal("missing first contributor")
	}
	if _, ok := l.next(1, b); ok {
		t.Fatal("two actions dispatched concurrently")
	}
	if !l.complete(first, nil) {
		t.Fatal("first completion rejected")
	}
	second, ok := l.next(1, b)
	if !ok || second.device != c || second.event.Code != first.event.Code {
		t.Fatal("alias contributor lost")
	}
	if !l.complete(second, nil) {
		t.Fatal("second completion rejected")
	}
	if err := l.edge(1, b, a, 30, false, false, remoteinput.Event{}); err != nil {
		t.Fatal(err)
	}
	release, ok := l.next(1, b)
	if !ok || release.event.Action != remoteinput.ActionRelease || release.device != a {
		t.Fatal("consumed release lost its source")
	}
	if _, ok := l.keys[nativeMappedPadKey{device: c, raw: 31}]; !ok {
		t.Fatal("release removed other keyboard")
	}
}

func TestNativeMappedPadLedgerRetiresAfterInflightAndKeepsOtherSource(t *testing.T) {
	var l nativeMappedPadLedger
	b := padOnlyAuthority{TargetID: "kit"}
	_ = l.begin(1, b)
	a, c := gateID(1), gateID(2)
	_ = l.edge(1, b, a, 30, true, true, nativeMappedPadPress())
	inflight, ok := l.next(1, b)
	if !ok {
		t.Fatal("missing action")
	}
	_ = l.edge(1, b, a, 31, true, true, nativeMappedPadPress())
	_ = l.edge(1, b, c, 30, true, true, nativeMappedPadPress())
	if err := l.retire(1, b, a); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.next(1, b); ok {
		t.Fatal("retirement overtook in-flight action")
	}
	if !l.complete(inflight, nil) {
		t.Fatal("in-flight completion lost")
	}
	other, ok := l.next(1, b)
	if !ok || other.device != c || other.retire {
		t.Fatal("other source was dropped")
	}
	_ = l.complete(other, nil)
	retire, ok := l.next(1, b)
	if !ok || !retire.retire || retire.device != a {
		t.Fatal("exact retirement missing")
	}
	if _, ok := l.keys[nativeMappedPadKey{device: c, raw: 30}]; !ok {
		t.Fatal("retirement erased other keyboard")
	}
}

func TestNativeMappedPadLedgerFenceAndAmbiguityNeverReplay(t *testing.T) {
	var l nativeMappedPadLedger
	b := padOnlyAuthority{TargetID: "kit"}
	_ = l.begin(1, b)
	id := gateID(1)
	_ = l.edge(1, b, id, 30, true, true, nativeMappedPadPress())
	first, _ := l.next(1, b)
	if err := l.fence(1, b); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.next(1, b); ok {
		t.Fatal("fence overtook in-flight write")
	}
	if !l.complete(first, nil) {
		t.Fatal("in-flight result lost")
	}
	barrier, ok := l.next(1, b)
	if !ok || !barrier.retire || barrier.device != (localhid.DeviceID{}) {
		t.Fatal("retire-all barrier missing")
	}
	if l.complete(first, nil) {
		t.Fatal("stale completion acknowledged barrier")
	}
	if l.complete(barrier, errors.New("unknown write")) || !l.invalid {
		t.Fatal("ambiguous retirement not quarantined")
	}
	if _, ok := l.next(1, b); ok {
		t.Fatal("ambiguous operation replayed")
	}
}

func TestNativeMappedPadLedgerRequiresRetirementAndRejectsStaleCallbacks(t *testing.T) {
	var l nativeMappedPadLedger
	old := padOnlyAuthority{TargetID: "old"}
	next := padOnlyAuthority{TargetID: "next"}
	if err := l.begin(1, old); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(1, old, gateID(1), 30, true, true, nativeMappedPadPress()); err != nil {
		t.Fatal(err)
	}
	if err := l.fence(1, old); err != nil {
		t.Fatal(err)
	}
	if l.begin(2, next) == nil {
		t.Fatal("unconfirmed queued retirement replaced source")
	}
	if l.confirmedRetired(2, next) == nil {
		t.Fatal("wrong source retirement accepted")
	}
	if err := l.confirmedRetired(1, old); err != nil {
		t.Fatal(err)
	}
	if err := l.begin(2, next); err != nil {
		t.Fatal(err)
	}
	if err := l.edge(2, next, gateID(2), 31, true, true, nativeMappedPadPress()); err != nil {
		t.Fatal(err)
	}
	if l.edge(1, old, gateID(1), 30, false, false, remoteinput.Event{}) == nil {
		t.Fatal("stale edge accepted")
	}
	if l.retire(1, old, gateID(1)) == nil || l.fence(1, old) == nil {
		t.Fatal("stale source retirement accepted")
	}
	if l.invalid || len(l.keys) != 1 || len(l.queue) != 1 {
		t.Fatal("stale callback changed successor")
	}
	if action, ok := l.next(2, next); !ok || action.device != gateID(2) {
		t.Fatal("successor action lost")
	}
}

func TestNativeMappedPadLedgerRetirementWaitsForLocalInflightResult(t *testing.T) {
	var l nativeMappedPadLedger
	b := padOnlyAuthority{TargetID: "kit"}
	_ = l.begin(1, b)
	_ = l.edge(1, b, gateID(1), 30, true, true, nativeMappedPadPress())
	action, ok := l.next(1, b)
	if !ok {
		t.Fatal("missing in-flight action")
	}
	if l.confirmedRetired(1, b) == nil {
		t.Fatal("retired while local write was in flight")
	}
	if !l.complete(action, nil) {
		t.Fatal("in-flight result not drained")
	}
	if err := l.confirmedRetired(1, b); err != nil {
		t.Fatal(err)
	}
}
