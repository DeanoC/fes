package host_test

import (
	"context"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type remoteInputCapabilityAttacher interface {
	AttachWithCapabilities(context.Context, string, bool) error
}

func attachRemoteInputWithCapabilities(t *testing.T, input *host.RemoteInput, core string, keyboard bool) {
	t.Helper()
	attacher, ok := interface{}(input).(remoteInputCapabilityAttacher)
	if !ok {
		t.Fatalf("RemoteInput does not expose capability-aware attach")
	}
	if err := attacher.AttachWithCapabilities(context.Background(), core, keyboard); err != nil {
		t.Fatalf("attach %s keyboard=%v: %v", core, keyboard, err)
	}
}

func framesSnapshot(starter *testBridgeStarter) []protocol.InputFrame {
	starter.mu.Lock()
	sinks := append([]*testSink(nil), starter.sinks...)
	starter.mu.Unlock()
	var frames []protocol.InputFrame
	for _, sink := range sinks {
		sink.mu.Lock()
		frames = append(frames, sink.events...)
		sink.mu.Unlock()
	}
	return frames
}

func waitForFrameCount(t *testing.T, starter *testBridgeStarter, count int) []protocol.InputFrame {
	t.Helper()
	waitFor(t, time.Second, func() bool { return len(framesSnapshot(starter)) >= count })
	return framesSnapshot(starter)
}

func requireKeyboardFrame(t *testing.T, frame protocol.InputFrame, action remoteinput.Action, code remoteinput.Code) {
	t.Helper()
	if frame.Device != uint8(remoteinput.DeviceKeyboard) || frame.Kind != uint8(remoteinput.KindKey) ||
		frame.Action != uint8(action) || remoteinput.Code(frame.Code) != code {
		t.Fatalf("frame = %+v, want keyboard action=%d code=%d", frame, action, code)
	}
}

func TestRemoteInputColecoMapsControllerInputsToKeyboardMatrixBits(t *testing.T) {
	cases := []struct {
		name  string
		event remoteinput.Event
		code  remoteinput.Code
		bit   uint
	}{
		{name: "dpad up", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadUp}, code: zx81keys.KeyShift, bit: 0},
		{name: "dpad right", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadRight}, code: zx81keys.Letter('Z'), bit: 1},
		{name: "dpad down", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadDown}, code: zx81keys.Letter('X'), bit: 2},
		{name: "dpad left", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadLeft}, code: zx81keys.Letter('C'), bit: 3},
		{name: "fire one", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}, code: zx81keys.Letter('V'), bit: 4},
		{name: "fire two", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonB}, code: zx81keys.Letter('Q'), bit: 10},
		{name: "left stick left", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftX, Value: -32767}, code: zx81keys.Letter('C'), bit: 3},
		{name: "left stick right", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftX, Value: 32767}, code: zx81keys.Letter('Z'), bit: 1},
		{name: "left stick up", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftY, Value: -32767}, code: zx81keys.KeyShift, bit: 0},
		{name: "left stick down", event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftY, Value: 32767}, code: zx81keys.Letter('X'), bit: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			starter := &testBridgeStarter{}
			input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			attachRemoteInputWithCapabilities(t, input, "fes.coleco", true)
			if err := input.Send(context.Background(), tc.event); err != nil {
				t.Fatalf("send: %v", err)
			}
			frames := waitForFrameCount(t, starter, 1)
			requireKeyboardFrame(t, frames[0], remoteinput.ActionPress, tc.code)
			matrix := zx81keys.Matrix(map[remoteinput.Code]bool{tc.code: true})
			if matrix != zx81keys.Neutral&^(uint64(1)<<tc.bit) {
				t.Fatalf("matrix = %#x, want bit %d asserted", matrix, tc.bit)
			}
		})
	}
}

func TestRemoteInputColecoMappingRequiresExactCoreAndKeyboardCapability(t *testing.T) {
	cases := []struct {
		name     string
		core     string
		keyboard bool
	}{
		{name: "no keyboard interface", core: "fes.coleco", keyboard: false},
		{name: "wrong core case", core: "FES.coleco", keyboard: true},
		{name: "zx81", core: "fes.zx81", keyboard: true},
		{name: "pong", core: "fes.pong", keyboard: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			starter := &testBridgeStarter{}
			input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			attachRemoteInputWithCapabilities(t, input, tc.core, tc.keyboard)
			event := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
			if err := input.Send(context.Background(), event); err != nil {
				t.Fatalf("send: %v", err)
			}
			frames := waitForFrameCount(t, starter, 1)
			frame := frames[0]
			if frame.Device != uint8(remoteinput.DeviceGamepad) || frame.Kind != uint8(remoteinput.KindButton) ||
				frame.Action != uint8(remoteinput.ActionPress) || remoteinput.Code(frame.Code) != remoteinput.ButtonA {
				t.Fatalf("frame = %+v, want unchanged gamepad A", frame)
			}
		})
	}
}

func TestRemoteInputColecoKeepsOverlappingKeyboardDPadAndAxisHeld(t *testing.T) {
	starter := &testBridgeStarter{}
	input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	attachRemoteInputWithCapabilities(t, input, "fes.coleco", true)

	keyboardDown := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: zx81keys.KeyShift}
	keyboardUp := keyboardDown
	keyboardUp.Action = remoteinput.ActionRelease
	dpadDown := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadUp}
	dpadUp := dpadDown
	dpadUp.Action = remoteinput.ActionRelease
	axisDown := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftY, Value: -32767}
	axisNeutral := axisDown
	axisNeutral.Value = 0

	for _, event := range []remoteinput.Event{keyboardDown, dpadDown, axisDown} {
		if err := input.Send(context.Background(), event); err != nil {
			t.Fatalf("send held event %+v: %v", event, err)
		}
	}
	waitForFrameCount(t, starter, 1)
	if got := len(framesSnapshot(starter)); got != 1 {
		t.Fatalf("overlapping presses emitted %d frames, want 1", got)
	}

	for _, event := range []remoteinput.Event{keyboardUp, dpadUp} {
		if err := input.Send(context.Background(), event); err != nil {
			t.Fatalf("send partial release %+v: %v", event, err)
		}
	}
	if got := len(framesSnapshot(starter)); got != 1 {
		t.Fatalf("partial releases emitted %d frames, want 1", got)
	}
	if err := input.Send(context.Background(), axisNeutral); err != nil {
		t.Fatalf("send axis neutral: %v", err)
	}
	frames := waitForFrameCount(t, starter, 2)
	if len(frames) != 2 {
		t.Fatalf("final release emitted %d frames, want 2", len(frames))
	}
	requireKeyboardFrame(t, frames[1], remoteinput.ActionRelease, zx81keys.KeyShift)
}

func TestRemoteInputColecoSourceCloseDropsMappedStateBeforeReconnect(t *testing.T) {
	starter := &testBridgeStarter{}
	input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	attachRemoteInputWithCapabilities(t, input, "fes.coleco", true)
	sessionID := input.Status().SessionID
	source, err := input.ClaimSource(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	up := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadUp}
	if err := source.SendEvent(context.Background(), up, time.Now()); err != nil {
		t.Fatal(err)
	}
	waitForFrameCount(t, starter, 1)
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := input.ClaimSource(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	fire := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if err := replacement.SendEvent(context.Background(), fire, time.Now()); err != nil {
		t.Fatal(err)
	}
	frames := waitForFrameCount(t, starter, 2)
	if len(frames) != 2 {
		t.Fatalf("reconnected frames = %d, want one old and one new frame: %+v", len(frames), frames)
	}
	requireKeyboardFrame(t, frames[1], remoteinput.ActionPress, zx81keys.Letter('V'))
}

func TestRemoteInputColecoStopClearsMappedStateForNewGeneration(t *testing.T) {
	starter := &testBridgeStarter{}
	input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	attachRemoteInputWithCapabilities(t, input, "fes.coleco", true)
	up := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadUp}
	if err := input.Send(context.Background(), up); err != nil {
		t.Fatal(err)
	}
	waitForFrameCount(t, starter, 1)
	if err := input.Detach(context.Background(), "session_stop"); err != nil {
		t.Fatal(err)
	}
	attachRemoteInputWithCapabilities(t, input, "fes.coleco", true)
	waitFor(t, time.Second, func() bool {
		starter.mu.Lock()
		defer starter.mu.Unlock()
		return len(starter.sinks) >= 2
	})
	if got := len(framesSnapshot(starter)); got != 1 {
		t.Fatalf("new generation replayed %d stale frames", got-1)
	}
	right := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadRight}
	if err := input.Send(context.Background(), right); err != nil {
		t.Fatal(err)
	}
	frames := waitForFrameCount(t, starter, 2)
	if len(frames) != 2 {
		t.Fatalf("post-stop frames = %d, want 2: %+v", len(frames), frames)
	}
	requireKeyboardFrame(t, frames[1], remoteinput.ActionPress, zx81keys.Letter('Z'))
}
