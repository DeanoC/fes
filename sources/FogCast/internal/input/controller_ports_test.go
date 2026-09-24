package input

import (
	"context"
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type portWrite struct {
	id            string
	generation    uint64
	port, buttons uint8
	keypad        uint16
}

type portsLifecycleControl struct {
	*barrierPackageControl
	fault  bool
	idle   bool
	writes []misterruntime.ControllerRequest
}

func (c *portsLifecycleControl) SetController(_ context.Context, r misterruntime.ControllerRequest) (misterruntime.Protocol2Response, error) {
	c.writes = append(c.writes, r)
	if c.fault {
		return misterruntime.Protocol2Response{}, errors.New("runtime input failed")
	}
	return misterruntime.Protocol2Response{OK: true}, nil
}
func (c *portsLifecycleControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	if c.idle {
		return misterruntime.Protocol2Response{OK: true, State: "idle"}, nil
	}
	return misterruntime.Protocol2Response{State: "failed"}, nil
}

func TestTargetControllerPortsStopHeldBothAndRecoveryAttach(t *testing.T) {
	control := &portsLifecycleControl{barrierPackageControl: &barrierPackageControl{}}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	sink := &controllerPortsSink{fallback: &recordingSink{}}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	generation := uint64(1)
	controller.ConfigureControllerPorts(func(context.Context) (*ControllerBinding, error) {
		return &ControllerBinding{PackageID: "pkg", Generation: generation}, nil
	}, func(id string, gen uint64, port, buttons uint8, keypad uint16) error {
		return runtime.SetController(context.Background(), misterruntime.ControllerRequest{PackageID: id, Generation: gen, Port: port, Buttons: buttons, Keypad: keypad})
	})
	t.Cleanup(func() { control.fault = false; _ = controller.Close() })
	spec := Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.coleco"}
	if err := controller.Attach(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	for port := uint8(0); port < 2; port++ {
		if err := sink.Apply(protocol.InputFrame{Player: port, Device: 1, Kind: 1, Action: 1, Code: 104}); err != nil {
			t.Fatal(err)
		}
	}
	if err := controller.Detach(context.Background(), spec.Session); err != nil {
		t.Fatal(err)
	}
	if len(control.writes) != 4 || control.writes[2].Buttons != 0 || control.writes[3].Buttons != 0 || control.writes[2].Port != 0 || control.writes[3].Port != 1 {
		t.Fatalf("stop held both %+v", control.writes)
	}
	spec.Session = 2
	if err := controller.Attach(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if err := sink.Apply(protocol.InputFrame{Device: 1, Kind: 1, Action: 1, Code: 104}); err != nil {
		t.Fatal(err)
	}
	control.fault = true
	if err := controller.Detach(context.Background(), spec.Session); err == nil {
		t.Fatal("neutralization failure lost")
	}
	spec.Session = 3
	if err := controller.Attach(context.Background(), spec); err == nil {
		t.Fatal("faulted generation rebound")
	}
	// Explicit runtime Stop has now proven idle; the old generation can retire.
	control.idle = true
	generation = 2
	if err := controller.Attach(context.Background(), spec); err != nil {
		t.Fatalf("attach after recovered Stop: %v", err)
	}
	control.fault = false
	control.idle = false
	if err := sink.Apply(protocol.InputFrame{Device: 1, Kind: 1, Action: 1, Code: 105}); err != nil {
		t.Fatal(err)
	}
	last := control.writes[len(control.writes)-1]
	if last.Generation != 2 || last.Buttons != 32 {
		t.Fatalf("replacement %+v", last)
	}
}

func TestControllerPortsIndependentMasksOverlapAndRelease(t *testing.T) {
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(id string, generation uint64, port, buttons uint8, keypad uint16) error {
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		return nil
	}}
	if err := sink.bind(&ControllerBinding{PackageID: "package", Generation: 7, Keypad: true}); err != nil {
		t.Fatal(err)
	}
	apply := func(port uint8, kind remoteinput.Kind, code remoteinput.Code, action remoteinput.Action, value int32) {
		t.Helper()
		if err := sink.Apply(protocol.InputFrame{Player: port, Device: 1, Kind: uint8(kind), Code: uint16(code), Action: uint8(action), Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	apply(0, remoteinput.KindButton, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	apply(1, remoteinput.KindButton, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	apply(0, remoteinput.KindButton, remoteinput.ButtonA, remoteinput.ActionRelease, 0)
	apply(1, remoteinput.KindAxis, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 32767)
	apply(1, remoteinput.KindButton, remoteinput.ButtonDPadRight, remoteinput.ActionPress, 0)
	apply(1, remoteinput.KindAxis, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0)
	apply(1, remoteinput.KindButton, remoteinput.KeypadHash, remoteinput.ActionPress, 0)
	wantButtons := []uint8{16, 16, 0, 24, 24, 24, 24}
	for i, want := range wantButtons {
		if writes[i].buttons != want || writes[i].id != "package" || writes[i].generation != 7 {
			t.Fatalf("write %d=%+v", i, writes[i])
		}
	}
	if writes[6].keypad != 2048 || writes[6].port != 1 {
		t.Fatalf("keypad %+v", writes[6])
	}
	if err := sink.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 8 || writes[7].port != 1 || writes[7].buttons != 0 || writes[7].keypad != 0 {
		t.Fatalf("release %+v", writes)
	}
	if err := sink.bind(&ControllerBinding{PackageID: "next", Generation: 8}); err != nil {
		t.Fatal(err)
	}
	apply(0, remoteinput.KindButton, remoteinput.ButtonB, remoteinput.ActionPress, 0)
	if writes[8] != (portWrite{"next", 8, 0, 32, 0}) {
		t.Fatalf("new generation %+v", writes[8])
	}
}

func TestControllerPortsPartialFailureRetainsOldBindingAndReleasesBoth(t *testing.T) {
	fail := false
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(id string, generation uint64, port, buttons uint8, keypad uint16) error {
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		if fail && port == 0 {
			return errors.New("stale generation")
		}
		return nil
	}}
	_ = sink.bind(&ControllerBinding{PackageID: "old", Generation: 3})
	for port := uint8(0); port < 2; port++ {
		if err := sink.Apply(protocol.InputFrame{Player: port, Device: 1, Kind: 1, Action: 1, Code: 104}); err != nil {
			t.Fatal(err)
		}
	}
	fail = true
	if err := sink.ReleaseAll(); err == nil {
		t.Fatal("lost neutralization failure")
	}
	if len(writes) != 4 || writes[3].port != 1 {
		t.Fatalf("second release skipped: %+v", writes)
	}
	if err := sink.bind(&ControllerBinding{PackageID: "new", Generation: 4}); err == nil {
		t.Fatal("rebound failed old release")
	}
	for _, w := range writes {
		if w.id != "old" || w.generation != 3 {
			t.Fatal("release touched replacement")
		}
	}
	fail = false
	if err := sink.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 5 || writes[4].port != 0 {
		t.Fatalf("retry %+v", writes)
	}
}

func TestControllerPortsRejectBeforeMutationAndPreserveLegacy(t *testing.T) {
	legacy := &recordingSink{}
	writes := 0
	sink := &controllerPortsSink{fallback: legacy, poster: func(string, uint64, uint8, uint8, uint16) error { writes++; return nil }}
	f := protocol.InputFrame{Player: 0, Device: 1, Kind: 1, Action: 1, Code: 104}
	if err := sink.Apply(f); err != nil {
		t.Fatal(err)
	}
	if writes != 0 || len(legacy.frames) != 1 {
		t.Fatal("legacy path changed")
	}
	_ = sink.bind(&ControllerBinding{PackageID: "pkg", Generation: 1})
	for _, bad := range []protocol.InputFrame{{Player: 2, Device: 1, Kind: 1, Action: 1, Code: 104}, {Device: 1, Kind: 1, Action: 1, Code: 120}, {Device: 1, Kind: 1, Action: 1, Code: 104, Value: 1}, {Device: 0, Kind: 0, Action: 1, Code: 104}} {
		if err := sink.Apply(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if writes != 0 {
		t.Fatal("rejected frame wrote state")
	}
}

func TestControllerSelectStartWireBits(t *testing.T) {
	for code, want := range map[remoteinput.Code]uint8{remoteinput.ButtonSelect: 64, remoteinput.ButtonStart: 128} {
		got, keypad := controllerSnapshot(remoteinput.Snapshot{Pressed: []remoteinput.Code{code}}, false)
		if keypad != 0 {
			t.Fatalf("code %d keypad=%d without keypad ports", code, keypad)
		}
		if got != want {
			t.Fatalf("code %d bitmap=%d want=%d", code, got, want)
		}
	}
}

func TestControllerKeypadPortsStartSelectAliasKeypad(t *testing.T) {
	cases := []struct {
		pressed     []remoteinput.Code
		wantButtons uint8
		wantKeypad  uint16
	}{
		{[]remoteinput.Code{remoteinput.ButtonStart}, 128, 1 << 1},
		{[]remoteinput.Code{remoteinput.ButtonSelect}, 64, 1 << 10},
		{[]remoteinput.Code{remoteinput.ButtonStart, remoteinput.Keypad0 + 1}, 128, 1 << 1},
		{[]remoteinput.Code{remoteinput.ButtonStart, remoteinput.KeypadHash}, 128, 1<<1 | 1<<11},
		{[]remoteinput.Code{remoteinput.ButtonA}, 16, 0},
	}
	for i, tc := range cases {
		buttons, keypad := controllerSnapshot(remoteinput.Snapshot{Pressed: tc.pressed}, true)
		if buttons != tc.wantButtons || keypad != tc.wantKeypad {
			t.Fatalf("case %d buttons=%d keypad=%d want %d/%d", i, buttons, keypad, tc.wantButtons, tc.wantKeypad)
		}
	}
}

func TestControllerKeypadPortsStartPressesAndReleasesKeypadOne(t *testing.T) {
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(id string, generation uint64, port, buttons uint8, keypad uint16) error {
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		return nil
	}}
	if err := sink.bind(&ControllerBinding{PackageID: "coleco", Generation: 3, Keypad: true}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []remoteinput.Action{remoteinput.ActionPress, remoteinput.ActionRelease} {
		if err := sink.Apply(protocol.InputFrame{Player: 1, Device: 1, Kind: uint8(remoteinput.KindButton), Code: uint16(remoteinput.ButtonStart), Action: uint8(action)}); err != nil {
			t.Fatal(err)
		}
	}
	want := []portWrite{{"coleco", 3, 1, 128, 1 << 1}, {"coleco", 3, 1, 0, 0}}
	if len(writes) != len(want) || writes[0] != want[0] || writes[1] != want[1] {
		t.Fatalf("writes %+v", writes)
	}
	if sink.dirty[1] {
		t.Fatal("released Start left port dirty")
	}
}
