// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/usbhub"
)

type lockedUSBKeyboardSource struct {
	mu      sync.Mutex
	reports [][]byte
	paused  bool
	pauses  int
	resumes int
	polls   int
}

func (s *lockedUSBKeyboardSource) enqueue(reports ...[]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range reports {
		s.reports = append(s.reports, append([]byte(nil), r...))
	}
}

func (s *lockedUSBKeyboardSource) PausePolling(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return errors.New("duplicate pause")
	}
	s.paused = true
	s.pauses++
	return nil
}

func (s *lockedUSBKeyboardSource) ResumePolling(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.paused {
		return errors.New("unpaired resume")
	}
	s.paused = false
	s.resumes++
	return nil
}

func (s *lockedUSBKeyboardSource) PollKeyboard() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return nil, errors.New("polled while paused")
	}
	s.polls++
	if len(s.reports) == 0 {
		return nil, nil
	}
	rep := s.reports[0]
	s.reports = s.reports[1:]
	return rep, nil
}

func (s *lockedUSBKeyboardSource) PollKeyboardInto(dst []byte) (int, error) {
	rep, err := s.PollKeyboard()
	if err != nil || rep == nil {
		return 0, err
	}
	return copy(dst, rep), nil
}

type lockedMultiShortcutSource struct {
	mu      sync.Mutex
	inner   *fakeMultiSource
	paused  bool
	pauses  int
	resumes int
}

func newLockedMultiShortcutSource() *lockedMultiShortcutSource {
	return &lockedMultiShortcutSource{inner: newFakeMultiSource()}
}

func (s *lockedMultiShortcutSource) enqueueKeys(reports ...[8]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.keys(reports...)
}

func (s *lockedMultiShortcutSource) Service() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return errors.New("serviced while paused")
	}
	return s.inner.Service()
}

func (s *lockedMultiShortcutSource) NextKeyboardReport() ([8]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.NextKeyboardReport()
}

func (s *lockedMultiShortcutSource) NextGamepadUpdate() (usbhub.GamepadUpdate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.NextGamepadUpdate()
}

func (s *lockedMultiShortcutSource) NextMouseUpdate() (usbhub.MouseUpdate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.NextMouseUpdate()
}

func (s *lockedMultiShortcutSource) KeyboardNeutral() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.KeyboardNeutral()
}

func (s *lockedMultiShortcutSource) GamepadNeutral() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.GamepadNeutral()
}

func (s *lockedMultiShortcutSource) MouseNeutral() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.MouseNeutral()
}

func (s *lockedMultiShortcutSource) SetGameplayActive(active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.SetGameplayActive(active)
}

func (s *lockedMultiShortcutSource) StartPairing(d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.StartPairing(d)
}

func (s *lockedMultiShortcutSource) StopPairing() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.StopPairing()
}

func (s *lockedMultiShortcutSource) PairingActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.PairingActive()
}

func (s *lockedMultiShortcutSource) PairingRemaining() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.PairingRemaining()
}

func (s *lockedMultiShortcutSource) BluetoothSnapshot() usbhub.BluetoothSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.BluetoothSnapshot()
}

func (s *lockedMultiShortcutSource) BluetoothGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.BluetoothGeneration()
}

func (s *lockedMultiShortcutSource) GamepadGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.GamepadGeneration()
}

func (s *lockedMultiShortcutSource) GamepadDevicesInto(dst []usbhub.GamepadDevice) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.GamepadDevicesInto(dst)
}

func (s *lockedMultiShortcutSource) ForgetBluetoothDevice(address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.ForgetBluetoothDevice(address)
}

func (s *lockedMultiShortcutSource) SaveBondsIfDirty(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.SaveBondsIfDirty(ctx)
}

func (s *lockedMultiShortcutSource) PausePolling(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return errors.New("duplicate pause")
	}
	s.paused = true
	s.pauses++
	return s.inner.PausePolling(ctx)
}

func (s *lockedMultiShortcutSource) ResumePolling(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.paused {
		return errors.New("unpaired resume")
	}
	s.paused = false
	s.resumes++
	return s.inner.ResumePolling(ctx)
}

func (s *lockedMultiShortcutSource) Ready() <-chan struct{} {
	return s.inner.Ready()
}

func TestProgramKeyboardRolloverAndShortcutInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("ProgramInputPlatform", func(t *testing.T) {
		f, b, c := backendFixture(t)
		src := &programKeyboardReports{}
		p, err := NewProgramInputPlatform(&programInputBase{}, b, src)
		if err != nil {
			t.Fatal(err)
		}
		src.reports = [][]byte{make([]byte, 8)}
		if _, err := p.Next(ctx); err != nil {
			t.Fatal(err)
		}
		play, err := c.Launch(ctx, "native-0")
		if err != nil {
			t.Fatal(err)
		}

		// 1. Held Ctrl+Alt+Escape before neutral release must NOT trigger CmdStop.
		src.reports = [][]byte{{0x05, 0, 0x29, 0, 0, 0, 0, 0}}
		frame, err := p.Next(ctx)
		if err != nil || len(frame.Commands) != 0 || f.cpu.keyboardRows != ([9]uint16{}) {
			t.Fatalf("held Ctrl+Alt+Escape bypassed launch fence: frame=%+v err=%v", frame, err)
		}

		// 2. Neutral arms; holding Escape then adding Ctrl+Alt stays ordinary core Escape.
		src.reports = [][]byte{
			make([]byte, 8),
			{0, 0, 0x29, 0, 0, 0, 0, 0},
			{0x05, 0, 0x29, 0, 0, 0, 0, 0},
		}
		frame, err = p.Next(ctx)
		if err != nil || len(frame.Commands) != 0 || f.cpu.keyboardRows[2] != 1<<9 {
			t.Fatalf("adding modifiers onto held Escape became return: frame=%+v rows=%v err=%v", frame, f.cpu.keyboardRows, err)
		}

		// 3. Rollover does not wedge decoder; fresh simultaneous Ctrl+Alt+Escape returns cleanly.
		src.reports = [][]byte{
			make([]byte, 8),
			{0x05, 0, 1, 1, 1, 1, 1, 1},
			{0x05, 0, 0x29, 0, 0, 0, 0, 0},
		}
		frame, err = p.Next(ctx)
		if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop || f.cpu.keyboardRows != ([9]uint16{}) {
			t.Fatalf("expected CmdStop after rollover + fresh simultaneous chord: frame=%+v rows=%v err=%v", frame, f.cpu.keyboardRows, err)
		}
		if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ProgramMultiInputPlatform", func(t *testing.T) {
		f, b, c := backendFixture(t)
		src := newFakeMultiSource()
		p, err := NewProgramMultiInputPlatform(&programInputBase{}, b, src, multiTestConfig())
		if err != nil {
			t.Fatal(err)
		}
		multiNext(t, p, "arm idle menu")

		// 1. Launch while holding Ctrl+Alt+Escape -> fence blocks return until neutral.
		src.keys([8]byte{0x05, 0, 0x29})
		multiNext(t, p, "press chord in menu", tenfoot.CmdBack)
		play, err := c.Launch(ctx, "native-0")
		if err != nil {
			t.Fatal(err)
		}
		multiNext(t, p, "held chord blocked by launch fence")

		// 2. Neutral arms; adding Ctrl+Alt onto held Escape stays core Escape.
		src.keys([8]byte{}, [8]byte{0, 0, 0x29}, [8]byte{0x05, 0, 0x29})
		multiNext(t, p, "held Escape with added modifiers stays core key")
		if f.cpu.keyboardRows[2] != 1<<9 {
			t.Fatalf("expected Escape row bit retained, got %v", f.cpu.keyboardRows)
		}

		// 3. Rollover ignored cleanly; sequential Ctrl+Alt -> Escape triggers CmdStop and neutralizes.
		src.keys([8]byte{}, [8]byte{0x05, 0, 1, 1, 1, 1, 1, 1}, [8]byte{0x05}, [8]byte{0x05, 0, 0x29})
		multiNext(t, p, "sequential Ctrl+Alt->Esc after rollover triggers CmdStop", tenfoot.CmdStop)
		if f.cpu.keyboardRows != ([9]uint16{}) {
			t.Fatalf("expected neutralized keyboard rows after CmdStop, got %v", f.cpu.keyboardRows)
		}
		if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestProgramUSBKeyboardRepeatedReturnAndQuitPark(t *testing.T) {
	for _, mode := range []string{"input", "multi"} {
		t.Run(mode, func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			backend, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "zx81", Title: "ZX81", Request: request}})
			if err != nil {
				t.Fatal(err)
			}
			device, err := gfx.NewSoftware(1280, 720)
			if err != nil {
				t.Fatal(err)
			}
			defer device.Close()

			inputSource := &lockedUSBKeyboardSource{}
			multiSource := newLockedMultiShortcutSource()
			enqueue := func(reports ...[8]byte) {
				if mode == "multi" {
					multiSource.enqueueKeys(reports...)
					return
				}
				for _, r := range reports {
					inputSource.enqueue(r[:])
				}
			}

			var traceBuf strings.Builder
			var traceMu sync.Mutex
			stage := 0
			serial, err := NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false },
				func(_ context.Context, raw []byte) error {
					traceMu.Lock()
					traceBuf.Write(raw)
					traceMu.Unlock()
					if !bytes.HasPrefix(raw, []byte("FES_NATIVE_TENFOOT_STATE ")) {
						return nil
					}
					var view struct {
						Loading bool                 `json:"loading"`
						Titles  []string             `json:"titles"`
						Session string               `json:"session"`
						Native  ProgramServiceStatus `json:"native"`
					}
					if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("FES_NATIVE_TENFOOT_STATE ")), &view); err != nil {
						return err
					}
					switch stage {
					case 0:
						if !view.Loading && len(view.Titles) == 1 && view.Native.State == ProgramReady {
							enqueue([8]byte{}, [8]byte{0, 0, 0x28}, [8]byte{})
							stage = 1
						}
					case 1:
						if view.Session == "active" && view.Native.State == ProgramRunning {
							enqueue([8]byte{}, [8]byte{0, 0, 0x04}, [8]byte{}, [8]byte{0x05}, [8]byte{0x05, 0, 0x29})
							stage = 2
						}
					case 2:
						if view.Session == "idle" && view.Native.State == ProgramReady {
							enqueue([8]byte{}, [8]byte{0, 0, 0x28}, [8]byte{})
							stage = 3
						}
					case 3:
						if view.Session == "active" && view.Native.State == ProgramRunning {
							enqueue([8]byte{}, [8]byte{0x05, 0, 0x29})
							stage = 4
						}
					case 4:
						if view.Session == "idle" && view.Native.State == ProgramReady {
							enqueue([8]byte{}, [8]byte{0, 0, 0x14})
							stage = 5
						}
					}
					return nil
				})
			if err != nil {
				t.Fatal(err)
			}
			serial.EnableLifecycleTrace()

			var inner tenfoot.Platform
			if mode == "multi" {
				inner, err = NewProgramMultiInputPlatform(serial, backend, multiSource, multiTestConfig())
			} else {
				inner, err = NewProgramInputPlatform(serial, backend, inputSource)
			}
			if err != nil {
				t.Fatal(err)
			}
			menuPlatform, err := NewProgramMenuPlatform(f.s, inner, device.Framebuffer)
			if err != nil {
				t.Fatal(err)
			}

			app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: backend, Timeout: 10 * time.Second}), 1280, 720, 400)
			if err := app.SetPreferenceStore(nativePreferences{}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := tenfoot.RunPlatform(ctx, app, device, menuPlatform); err != nil {
				t.Fatalf("%s tenfoot.RunPlatform failed at stage=%d: %v", mode, stage, err)
			}
			if stage != 5 || f.s.Status().State != ProgramReady || f.r.f.lease.r.Active {
				t.Fatalf("%s expected clean exit to ProgramReady at stage 5, got stage=%d status=%+v lease=%t", mode, stage, f.s.Status(), f.r.f.lease.r.Active)
			}
			if err := backend.ParkMenu(context.Background()); err != nil {
				t.Fatalf("%s ParkMenu after USB Q failed: %v", mode, err)
			}
			if f.s.Status().State != ProgramParked {
				t.Fatalf("%s expected ProgramParked after ParkMenu, got %+v", mode, f.s.Status())
			}

			var paused bool
			var pauses, resumes int
			wantSource := "usb-boot-keyboard"
			if mode == "multi" {
				wantSource = "usb-multi"
				multiSource.mu.Lock()
				paused, pauses, resumes = multiSource.paused, multiSource.pauses, multiSource.resumes
				multiSource.mu.Unlock()
			} else {
				inputSource.mu.Lock()
				paused, pauses, resumes = inputSource.paused, inputSource.pauses, inputSource.resumes
				inputSource.mu.Unlock()
			}
			if !paused || pauses != 5 || resumes != 4 {
				t.Fatalf("%s expected balanced pause/resume (paused=true, pauses=5, resumes=4), got paused=%t pauses=%d resumes=%d", mode, paused, pauses, resumes)
			}

			traceMu.Lock()
			trace := traceBuf.String()
			traceMu.Unlock()
			if strings.Count(trace, "FES_NATIVE_INPUT_ACTION source="+wantSource+" action=stop") != 2 {
				t.Fatalf("%s expected 2 USB stop actions, trace:\n%s", mode, trace)
			}
			if strings.Count(trace, "FES_NATIVE_INPUT_ACTION source="+wantSource+" action=quit") != 1 {
				t.Fatalf("%s expected 1 USB quit action, trace:\n%s", mode, trace)
			}
		})
	}
}
