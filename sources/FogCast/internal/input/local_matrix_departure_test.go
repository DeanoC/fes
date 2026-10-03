package input

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func newLocalMatrixFeed(t *testing.T, core string) (net.Conn, <-chan uint64) {
	t.Helper()
	posted := make(chan uint64, 64)
	keys := NewKeyboardSink()
	keys.SetPoster(func(_ context.Context, matrix uint64) error {
		posted <- matrix
		return nil
	})
	sink := &controllerPortsSink{keys: keys}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.keyboard = keys
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Keyboard: true, CoreID: core}, nil
	})
	path := localSocketPath(t, "matrix.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- controller.ServeLocalInput(ctx, path) }()
	t.Cleanup(func() {
		cancel()
		_ = controller.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve local matrix input: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("local matrix input did not stop")
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.Dial("unix", path)
		if err == nil {
			t.Cleanup(func() { _ = conn.Close() })
			return conn, posted
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial local matrix input: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A keyboard press/release is an ordered barrier on the same local socket.
// Its two posts confirm that all preceding pad frames were consumed, including
// frames whose merged matrix did not change. Enter is not a joystick mapping.
func localMatrixAfterFrames(t *testing.T, conn net.Conn, posted <-chan uint64, frames ...protocol.InputFrame) uint64 {
	t.Helper()
	for _, frame := range append(frames, keyboardFrameFor(zx81keys.KeyEnter, remoteinput.ActionPress)) {
		if err := bridge.WriteFrame(conn, frame); err != nil {
			t.Fatalf("write local matrix input: %v", err)
		}
	}
	enterBit := zx81keys.Neutral ^ zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.KeyEnter: true})
	wait := func(pressed bool) uint64 {
		t.Helper()
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case matrix := <-posted:
				if (matrix&enterBit == 0) == pressed {
					return matrix
				}
			case <-deadline.C:
				t.Fatal("local matrix input did not reach the keyboard barrier")
			}
		}
	}
	wait(true)
	if err := bridge.WriteFrame(conn, keyboardFrameFor(zx81keys.KeyEnter, remoteinput.ActionRelease)); err != nil {
		t.Fatalf("release local matrix barrier: %v", err)
	}
	return wait(false)
}

func assertLocalMatrix(t *testing.T, got uint64, codes ...remoteinput.Code) {
	t.Helper()
	held := make(map[remoteinput.Code]bool, len(codes))
	for _, code := range codes {
		held[code] = true
	}
	if want := zx81keys.Matrix(held); got != want {
		t.Fatalf("matrix = %010x, want %010x for held keys %v", got, want, codes)
	}
}

func TestLocalMatrixDeparturePreservesSurvivingPad(t *testing.T) {
	for _, core := range []string{"fes.sms", "fes.coleco", "fes.sg1000"} {
		t.Run(core, func(t *testing.T) {
			for _, tc := range []struct {
				name        string
				firstCode   remoteinput.Code
				secondCode  remoteinput.Code
				firstValue  int32
				secondValue int32
				before      []remoteinput.Code
				after       []remoteinput.Code
			}{
				{"same button", remoteinput.ButtonA, remoteinput.ButtonA, 0, 0, []remoteinput.Code{zx81keys.Letter('V')}, []remoteinput.Code{zx81keys.Letter('V')}},
				{"different buttons", remoteinput.ButtonA, remoteinput.ButtonDPadRight, 0, 0, []remoteinput.Code{zx81keys.Letter('V'), zx81keys.Letter('Z')}, []remoteinput.Code{zx81keys.Letter('Z')}},
				{"same axis direction", remoteinput.AxisLeftX, remoteinput.AxisLeftX, 24000, 26000, []remoteinput.Code{zx81keys.Letter('Z')}, []remoteinput.Code{zx81keys.Letter('Z')}},
				{"opposite axis directions", remoteinput.AxisLeftX, remoteinput.AxisLeftX, -24000, 26000, []remoteinput.Code{zx81keys.Letter('C'), zx81keys.Letter('Z')}, []remoteinput.Code{zx81keys.Letter('Z')}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					conn, posted := newLocalMatrixFeed(t, core)
					frame := func(player uint8, code remoteinput.Code, value int32, pressed bool) protocol.InputFrame {
						action := remoteinput.ActionPress
						if !pressed {
							action, value = remoteinput.ActionRelease, 0
						}
						if code == remoteinput.AxisLeftX {
							action = remoteinput.ActionAbsolute
						}
						return gamepad(player, code, action, value)
					}
					got := localMatrixAfterFrames(t, conn, posted,
						frame(0, tc.firstCode, tc.firstValue, true),
						frame(1, tc.secondCode, tc.secondValue, true))
					assertLocalMatrix(t, got, tc.before...)
					// This is the hub's unplug sequence: released buttons, centered
					// axes, then the physical player's departure marker.
					got = localMatrixAfterFrames(t, conn, posted,
						frame(0, tc.firstCode, tc.firstValue, false),
						gamepad(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0),
						gamepad(0, remoteinput.AxisLeftY, remoteinput.ActionAbsolute, 0),
						localPlayerDeparture(0))
					assertLocalMatrix(t, got, tc.after...)
					got = localMatrixAfterFrames(t, conn, posted,
						frame(1, tc.secondCode, tc.secondValue, false),
						gamepad(1, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0),
						gamepad(1, remoteinput.AxisLeftY, remoteinput.ActionAbsolute, 0),
						localPlayerDeparture(1))
					assertLocalMatrix(t, got)
				})
			}
		})
	}
}

func TestLocalMatrixDeparturePreservesPhysicalKeyboard(t *testing.T) {
	for _, keyboardFirst := range []bool{true, false} {
		name := "keyboard after pad"
		if keyboardFirst {
			name = "keyboard before pad"
		}
		t.Run(name, func(t *testing.T) {
			conn, posted := newLocalMatrixFeed(t, "fes.sms")
			key := zx81keys.Letter('V')
			pad := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
			keyboard := keyboardFrameFor(key, remoteinput.ActionPress)
			frames := []protocol.InputFrame{pad, keyboard}
			if keyboardFirst {
				frames[0], frames[1] = frames[1], frames[0]
			}
			assertLocalMatrix(t, localMatrixAfterFrames(t, conn, posted, frames...), key)
			got := localMatrixAfterFrames(t, conn, posted,
				gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0),
				gamepad(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0),
				gamepad(0, remoteinput.AxisLeftY, remoteinput.ActionAbsolute, 0),
				localPlayerDeparture(0))
			assertLocalMatrix(t, got, key)
			assertLocalMatrix(t, localMatrixAfterFrames(t, conn, posted, keyboardFrameFor(key, remoteinput.ActionRelease)))
		})
	}
}
