//go:build linux

package bridge

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

type recordedIoctl struct {
	request uintptr
	value   uintptr
}

type scriptedEventWriter struct {
	calls   int
	failAt  int
	shortAt int
}

func (w *scriptedEventWriter) Write(buffer []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, errors.New("injected event write failure")
	}
	if w.calls == w.shortAt {
		return len(buffer) - 1, nil
	}
	return len(buffer), nil
}

func TestCreateUInputGamepadConfiguresExactIdentityCapabilitiesAndRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uinput")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []recordedIoctl
	sink, err := createUInputGamepad(path, func(_ uintptr, request uintptr, value uintptr) error {
		calls = append(calls, recordedIoctl{request: request, value: value})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	wantSetup := []recordedIoctl{
		{0x40045564, 0x00},  // UI_SET_EVBIT, EV_SYN
		{0x40045564, 0x01},  // UI_SET_EVBIT, EV_KEY
		{0x40045564, 0x03},  // UI_SET_EVBIT, EV_ABS
		{0x40045565, 0x220}, // BTN_DPAD_UP
		{0x40045565, 0x221}, // BTN_DPAD_DOWN
		{0x40045565, 0x222}, // BTN_DPAD_LEFT
		{0x40045565, 0x223}, // BTN_DPAD_RIGHT
		{0x40045565, 0x130}, // BTN_A
		{0x40045565, 0x131}, // BTN_B
		{0x40045565, 0x132}, // BTN_C
		{0x40045565, 0x13b}, // BTN_START
		{0x40045565, 0x133}, // BTN_X
		{0x40045565, 0x134}, // BTN_Y
		{0x40045565, 0x136}, // BTN_TL
		{0x40045565, 0x137}, // BTN_TR
		{0x40045565, 0x13a}, // BTN_SELECT
		{0x40045567, 0x00},  // UI_SET_ABSBIT, ABS_X
		{0x40045567, 0x01},  // UI_SET_ABSBIT, ABS_Y
		{0x00005501, 0x00},  // UI_DEV_CREATE
	}
	if len(calls) != len(wantSetup) {
		t.Fatalf("setup ioctl calls = %#v, want %#v", calls, wantSetup)
	}
	for index := range wantSetup {
		if calls[index] != wantSetup[index] {
			t.Fatalf("setup ioctl %d = %#v, want %#v", index, calls[index], wantSetup[index])
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1116 {
		t.Fatalf("uinput_user_dev bytes = %d, want 1116", len(data))
	}
	if got := string(bytes.TrimRight(data[:80], "\x00")); got != "FogCast Virtual Gamepad" {
		t.Fatalf("device name = %q", got)
	}
	if got := [4]uint16{
		binary.LittleEndian.Uint16(data[80:82]),
		binary.LittleEndian.Uint16(data[82:84]),
		binary.LittleEndian.Uint16(data[84:86]),
		binary.LittleEndian.Uint16(data[86:88]),
	}; got != [4]uint16{0x0006, 0x0000, 0x0001, 0x0001} {
		t.Fatalf("device identity = %#v", got)
	}
	for _, axis := range []int{0, 1} {
		maxOffset := 92 + axis*4
		minOffset := 92 + 64*4 + axis*4
		if max := int32(binary.LittleEndian.Uint32(data[maxOffset : maxOffset+4])); max != 32767 {
			t.Fatalf("axis %d max = %d", axis, max)
		}
		if min := int32(binary.LittleEndian.Uint32(data[minOffset : minOffset+4])); min != -32768 {
			t.Fatalf("axis %d min = %d", axis, min)
		}
	}

	if err := sink.Apply(protocol.InputFrame{Device: 1, Kind: 1, Action: 1, Code: protocol.InputCodeButtonC}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantEvents := make([]byte, 32)
	binary.LittleEndian.PutUint16(wantEvents[8:10], 1)
	binary.LittleEndian.PutUint16(wantEvents[10:12], 0x132)
	binary.LittleEndian.PutUint32(wantEvents[12:16], 1)
	// The second zero-valued record is EV_SYN/SYN_REPORT.
	if !bytes.Equal(data[1116:], wantEvents) {
		t.Fatalf("C event records = %x, want %x", data[1116:], wantEvents)
	}

	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != len(wantSetup)+1 || calls[len(calls)-1] != (recordedIoctl{request: 0x00005502}) {
		t.Fatalf("lifetime ioctl calls = %#v, want one UI_DEV_DESTROY", calls)
	}
}

func TestNativeUInputRejectsFramesOutsideOnePlayerGamepadContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uinput")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sink, err := createUInputGamepad(path, func(uintptr, uintptr, uintptr) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()

	tests := []struct {
		name  string
		frame protocol.InputFrame
	}{
		{name: "other player", frame: protocol.InputFrame{Player: 1, Device: 1, Kind: 1, Action: 1, Code: 104}},
		{name: "keyboard device", frame: protocol.InputFrame{Player: 0, Device: 0, Kind: 0, Action: 1, Code: 2}},
		{name: "key kind on gamepad", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 0, Action: 1, Code: 104}},
		{name: "system kind", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 3, Action: 1, Code: 104}},
		{name: "unknown button", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 1, Action: 1, Code: 113}},
		{name: "button absolute", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 1, Action: 2, Code: 104}},
		{name: "axis press", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 2, Action: 1, Code: 200}},
		{name: "axis release", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 2, Action: 0, Code: 201}},
		{name: "button code as axis", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 2, Action: 2, Code: 104}},
		{name: "axis code as button", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 1, Action: 1, Code: 200}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := sink.Apply(test.frame); !errors.Is(err, errRejectedInputFrame) {
				t.Fatalf("out-of-contract native frame error = %v, want rejected-frame marker", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("rejected frame emitted %d bytes", len(after)-len(before))
			}
		})
	}
}

func TestOpenUInputPreservesLegacyKeyboardAndSelectMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uinput")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sink, err := OpenUInput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	for _, frame := range []protocol.InputFrame{
		{Player: 3, Device: 0, Kind: 0, Action: 1, Code: 2},
		{Player: 2, Device: 1, Kind: 1, Action: 1, Code: 107},
	} {
		if err := sink.Apply(frame); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 64 {
		t.Fatalf("legacy event bytes = %d, want 64", len(data))
	}
	if code := binary.LittleEndian.Uint16(data[10:12]); code != 30 {
		t.Fatalf("legacy KeyA code = %d, want 30", code)
	}
	if code := binary.LittleEndian.Uint16(data[42:44]); code != 314 {
		t.Fatalf("legacy Select code = %d, want 314", code)
	}
}

func TestUInputReleaseRetainsPressedStateUntilEventAndSyncSucceed(t *testing.T) {
	for _, failure := range []struct {
		name    string
		failAt  int
		shortAt int
	}{
		{name: "event", failAt: 3},
		{name: "sync", failAt: 4},
		{name: "short event", shortAt: 3},
		{name: "short sync", shortAt: 4},
	} {
		t.Run(failure.name, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "uinput")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			writer := &scriptedEventWriter{failAt: failure.failAt, shortAt: failure.shortAt}
			sink := &UInputSink{
				file:    file,
				native:  true,
				write:   writer.Write,
				pressed: make(map[uint16]bool),
				axes:    make(map[uint16]int32),
			}
			press := protocol.InputFrame{Player: 0, Device: 1, Kind: 1, Action: 1, Code: 104}
			if err := sink.Apply(press); err != nil {
				t.Fatal(err)
			}
			release := press
			release.Action = 0
			if err := sink.Apply(release); err == nil {
				t.Fatal("failed release write was accepted")
			}
			if !sink.pressed[104] {
				t.Fatal("failed release write discarded tracked pressed state")
			}
			writer.failAt = 0
			writer.shortAt = 0
			beforeRetry := writer.calls
			if err := sink.ReleaseAll(); err != nil {
				t.Fatal(err)
			}
			if writer.calls-beforeRetry != 2 || sink.pressed[104] {
				t.Fatalf("release retry writes=%d pressed=%t, want two writes and neutral state", writer.calls-beforeRetry, sink.pressed[104])
			}
		})
	}
}

func TestUInputNonNeutralStateIsRetainedForCleanupAfterWriteFailure(t *testing.T) {
	frames := []struct {
		name  string
		frame protocol.InputFrame
	}{
		{name: "button", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 1, Action: 1, Code: 104}},
		{name: "axis", frame: protocol.InputFrame{Player: 0, Device: 1, Kind: 2, Action: 2, Code: 200, Value: 123}},
	}
	failures := []struct {
		name    string
		failAt  int
		shortAt int
	}{
		{name: "event", failAt: 1},
		{name: "sync", failAt: 2},
		{name: "short event", shortAt: 1},
		{name: "short sync", shortAt: 2},
	}
	for _, frame := range frames {
		for _, failure := range failures {
			t.Run(frame.name+"/"+failure.name, func(t *testing.T) {
				file, err := os.CreateTemp(t.TempDir(), "uinput")
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				writer := &scriptedEventWriter{failAt: failure.failAt, shortAt: failure.shortAt}
				sink := &UInputSink{
					file:    file,
					native:  true,
					write:   writer.Write,
					pressed: make(map[uint16]bool),
					axes:    make(map[uint16]int32),
				}
				if err := sink.Apply(frame.frame); err == nil {
					t.Fatal("failed non-neutral write was accepted")
				}
				if frame.frame.Kind == 1 && !sink.pressed[frame.frame.Code] {
					t.Fatal("possibly delivered press was not retained for cleanup")
				}
				if frame.frame.Kind == 2 && sink.axes[frame.frame.Code] != frame.frame.Value {
					t.Fatal("possibly delivered axis value was not retained for cleanup")
				}
				writer.failAt = 0
				writer.shortAt = 0
				beforeRelease := writer.calls
				if err := sink.ReleaseAll(); err != nil {
					t.Fatal(err)
				}
				if writer.calls-beforeRelease != 2 {
					t.Fatalf("cleanup writes = %d, want event plus SYN_REPORT", writer.calls-beforeRelease)
				}
				if sink.pressed[frame.frame.Code] {
					t.Fatal("cleanup retained pressed state")
				}
				if _, tracked := sink.axes[frame.frame.Code]; tracked {
					t.Fatal("cleanup retained axis state")
				}
			})
		}
	}
}

func TestSNESNativeButtonsEmitAndReleaseExactLinuxRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uinput")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	sink, err := createUInputGamepad(path, func(uintptr, uintptr, uintptr) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	for wire, linux := range map[uint16]uint16{107: 314, 109: 307, 110: 308, 111: 310, 112: 311} {
		before, _ := os.ReadFile(path)
		if err := sink.Apply(protocol.InputFrame{Device: 1, Kind: 1, Action: 1, Code: wire}); err != nil {
			t.Fatalf("wire %d: %v", wire, err)
		}
		if err := sink.ReleaseAll(); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(path)
		events := after[len(before):]
		if len(events) != 64 || binary.LittleEndian.Uint16(events[10:12]) != linux || binary.LittleEndian.Uint32(events[12:16]) != 1 || binary.LittleEndian.Uint16(events[42:44]) != linux || binary.LittleEndian.Uint32(events[44:48]) != 0 {
			t.Fatalf("wire %d events %x", wire, events)
		}
	}
}
