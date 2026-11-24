//go:build linux

package tenfoot

import "testing"

func TestNativeKeyboardUIReconcilePreservesOtherSource(t *testing.T) {
	a := &nativeInput{kind: InputKeyboard, commands: map[uint16]Command{1: CmdHome, 2: CmdTab}, shifts: map[uint16]bool{42: true}}
	b := &nativeInput{kind: InputKeyboard, commands: map[uint16]Command{1: CmdHome}}
	var cancelled []Command
	reconcileNativeKeyboardUI(a, []*nativeInput{a, b}, []uint16{42, 113}, true, func(c Command) { cancelled = append(cancelled, c) })
	if len(cancelled) != 1 || cancelled[0] != CmdTab || len(a.commands) != 0 || len(a.shifts) != 0 || len(b.commands) != 1 {
		t.Fatalf("cross-device UI reconciliation: cancelled %v a %+v b %+v", cancelled, a.commands, b.commands)
	}
	if a.keysHeld[42] != "shift" || !a.localKeyboardUI.suppressed[113] {
		t.Fatal("held raw keys were not suppressed")
	}
}

func TestNativeKeyboardUIKnownSnapshotRequiresRealRelease(t *testing.T) {
	in := &nativeInput{kind: InputKeyboard}
	reconcileNativeKeyboardUI(in, nil, []uint16{42, 113}, true, func(Command) {})
	for _, value := range []int32{1, 2, 0} {
		if got := nativeKeyboardUIEdge(in, 113, value, true); got.dispatch || got.cleanup {
			t.Fatalf("snapshot-held edge %d: %+v", value, got)
		}
	}
	if _, present := in.keysHeld[113]; present {
		t.Fatal("held key did not clear on real release")
	}
	if got := nativeKeyboardUIEdge(in, 113, 1, true); !got.dispatch || got.cleanup {
		t.Fatalf("fresh press %+v", got)
	}
	if got := nativeKeyboardUIEdge(in, 113, 2, true); !got.dispatch || got.cleanup {
		t.Fatalf("repeat %+v", got)
	}
	if got := nativeKeyboardUIEdge(in, 113, 0, false); got.dispatch || !got.cleanup {
		t.Fatalf("consumed release did not request cleanup: %+v", got)
	}
	if got := nativeKeyboardUIEdge(in, 42, 2, true); got.dispatch {
		t.Fatal("other snapshot-held Shift repeated")
	}
}

func TestNativeKeyboardUIUnknownStateRequiresPerCodeRelease(t *testing.T) {
	in := &nativeInput{kind: InputKeyboard, commands: map[uint16]Command{113: CmdHome}}
	var cancelled []Command
	reconcileNativeKeyboardUI(in, nil, nil, false, func(c Command) { cancelled = append(cancelled, c) })
	if len(cancelled) != 1 || cancelled[0] != CmdHome {
		t.Fatal("unknown state retained UI command")
	}
	if got := nativeKeyboardUIEdge(in, 113, 1, true); got.dispatch {
		t.Fatal("unknown key press armed UI")
	}
	if got := nativeKeyboardUIEdge(in, 113, 0, true); got.dispatch || got.cleanup {
		t.Fatal("first release acted as UI")
	}
	if got := nativeKeyboardUIEdge(in, 113, 1, true); !got.dispatch {
		t.Fatal("fresh post-release press blocked")
	}
	if got := nativeKeyboardUIEdge(in, 42, 1, true); got.dispatch {
		t.Fatal("release on one code armed another")
	}
}

func TestNativeKeyboardUIInvalidSnapshotIsUnknown(t *testing.T) {
	in := &nativeInput{kind: InputKeyboard}
	reconcileNativeKeyboardUI(in, nil, []uint16{272}, true, func(Command) {})
	if !in.localKeyboardUI.unknown {
		t.Fatal("hybrid held snapshot was trusted")
	}
	if nativeKeyboardUIEdge(in, 42, 1, true).dispatch {
		t.Fatal("unknown state armed key")
	}
}
