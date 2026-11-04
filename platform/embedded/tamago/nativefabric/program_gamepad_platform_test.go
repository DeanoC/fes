// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

type programPadReports struct {
	reports [][]byte
	err     error
	polls   int
}

func (s *programPadReports) PollGamepad() ([]byte, error) {
	s.polls++
	if len(s.reports) == 0 {
		return nil, s.err
	}
	r := s.reports[0]
	s.reports = s.reports[1:]
	return r, nil
}
func TestProgramGamepadPlatformMenuCoreAndReturnFences(t *testing.T) {
	f, b, c := backendFixture(t)
	source := &programPadReports{}
	base := &programInputBase{}
	d := gamepadDescriptor(t)
	keys := gamepadKeys()
	p, err := NewProgramGamepadPlatform(base, b, source, d, input.GamepadButtons{Confirm: 1, Back: 2}, keys)
	if err != nil {
		t.Fatal(err)
	}
	d[0] ^= 255
	keys.Buttons[0] = 4
	if _, err := NewProgramInputPlatform(base, b, &programKeyboardReports{}); !errors.Is(err, ErrProgramBusy) {
		t.Fatal("second source accepted", err)
	}
	ctx := context.Background()
	source.reports = [][]byte{padReport(128, 128, 1)}
	frame, err := p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdSelect || f.cpu.keyboardWrites != 0 {
		t.Fatal(frame, err)
	}
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{padReport(128, 128, 1), padReport(128, 128, 0), padReport(128, 128, 3), padReport(128, 128, 0)}
	frame, err = p.Next(ctx)
	if err != nil || len(frame.Commands) != 0 || frame.Text != "" || f.cpu.keyboardWrites != 27 || f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatal(frame, err, f.cpu.keyboardWrites)
	}
	old, oldSession := p.gamepad, p.session
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{padReport(128, 128, 2)}
	if frame, err = p.Next(ctx); err != nil || len(frame.Commands) != 0 {
		t.Fatal("held play button reached menu", frame, err)
	}
	source.reports = [][]byte{padReport(128, 128, 0), padReport(128, 128, 2)}
	if frame, err = p.Next(ctx); err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdBack {
		t.Fatal(frame, err)
	}
	if _, err := c.Launch(ctx, "native-1"); err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{padReport(128, 128, 0), padReport(128, 128, 1)}
	if _, err := p.Next(ctx); err != nil || f.cpu.keyboardRows[1] != 1<<6 {
		t.Fatal(err, f.cpu.keyboardRows)
	}
	calls := f.cpu.calls
	if err := old.Disconnect(ctx); !errors.Is(err, ErrProgramSession) || f.cpu.calls != calls {
		t.Fatal(err)
	}
	stale := &ProgramInputPlatform{backend: b, session: oldSession, gamepad: old}
	if err := stale.park(ctx); !errors.Is(err, ErrProgramSession) || f.cpu.calls != calls {
		t.Fatal("stale source parked replacement", err)
	}
	if err := p.Present(ctx, tenfoot.Snapshot{}); err != nil || base.presents != 1 {
		t.Fatal(err)
	}
}
func TestProgramGamepadPlatformValidationPollingAndMenuFailure(t *testing.T) {
	f, b, c := backendFixture(t)
	source := &programPadReports{}
	base := &programInputBase{}
	d := gamepadDescriptor(t)
	if _, err := NewProgramGamepadPlatform(base, b, source, []byte{0}, input.GamepadButtons{1, 2}, gamepadKeys()); err == nil || b.inputAttached {
		t.Fatal("bad schema claimed input", err)
	}
	if _, err := NewProgramGamepadPlatform(base, b, source, d, input.GamepadButtons{1, 1}, gamepadKeys()); err == nil || b.inputAttached {
		t.Fatal("bad menu mapping claimed input", err)
	}
	p, err := NewProgramGamepadPlatform(base, b, source, d, input.GamepadButtons{1, 2}, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Next(ctx); !errors.Is(err, context.Canceled) || source.polls != 0 {
		t.Fatal(err)
	}
	b.operation.Lock()
	_, err = p.Next(context.Background())
	b.operation.Unlock()
	if err != nil || source.polls != 0 {
		t.Fatal("transition consumed source", err)
	}
	for n := 0; n < 10; n++ {
		source.reports = append(source.reports, padReport(128, 128, 0))
	}
	if _, err := p.Next(context.Background()); err != nil || source.polls != 8 || len(source.reports) != 2 {
		t.Fatal(err, source.polls)
	}
	source.reports = nil
	source.err = errors.New("retired pad in menu")
	calls := f.cpu.calls
	if _, err := p.Next(context.Background()); !errors.Is(err, source.err) {
		t.Fatal(err)
	}
	result, err := c.Launch(context.Background(), "native-0")
	if err == nil && result.ErrorCode == "" {
		t.Fatal("failed input admitted play")
	}
	if f.cpu.calls != calls || f.s.Status().State != ProgramReady {
		t.Fatal("menu failure touched firmware")
	}
}

type programGamepadApp struct {
	app           *tenfoot.App
	fixture       *serviceFixture
	source        *programPadReports
	stage, cycles int
	fault         string
}

func (p *programGamepadApp) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	select {
	case <-ctx.Done():
		return tenfoot.PlatformInput{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	frame := tenfoot.PlatformInput{At: time.Now()}
	view := p.app.Snapshot()
	switch p.stage {
	case 0:
		if len(view.Games) > 0 && !view.Loading && p.fixture.s.Status().State == ProgramReady {
			p.source.reports = [][]byte{padReport(128, 128, 0), padReport(128, 128, 1)}
			p.stage = 1
		}
	case 1:
		if view.Session.State == "active" && p.fixture.s.Status().State == ProgramRunning {
			p.source.reports = [][]byte{padReport(128, 128, 1), padReport(128, 128, 0), padReport(128, 128, 3), padReport(128, 128, 0)}
			p.stage = 2
		}
	case 2:
		if len(p.source.reports) != 0 {
			return frame, nil
		}
		if p.fixture.cpu.keyboardRows != ([9]uint16{}) {
			return frame, errors.New("gamepad release missing")
		}
		if p.fault == "malformed" {
			p.source.reports = [][]byte{{0}}
		} else if p.fault == "rows" {
			p.source.reports = [][]byte{padReport(128, 128, 1)}
			p.fixture.cpu.failKeyboardAt = p.fixture.cpu.keyboardWrites + 2
		} else if p.fault != "" {
			p.source.err = errors.New("gamepad source failed during play")
			p.fixture.cpu.loseClose = p.fault == "close"
		} else {
			frame.Commands = []tenfoot.Command{tenfoot.CmdStop}
		}
		p.stage = 3
	case 3:
		if view.Session.State == "idle" && p.fixture.s.Status().State == ProgramReady {
			p.cycles++
			p.stage = 0
			if p.cycles == 3 {
				frame.Commands = []tenfoot.Command{tenfoot.CmdQuit}
			}
		}
	}
	return frame, nil
}
func (*programGamepadApp) Present(context.Context, tenfoot.Snapshot) error { return nil }
func TestProgramGamepadFullTenfootLaunchReturnAndFailures(t *testing.T) {
	for _, fault := range []string{"", "source", "close", "malformed", "rows"} {
		t.Run(map[string]string{"": "cycles", "source": "input-failure", "close": "closure-failure", "malformed": "invalid-report", "rows": "partial-core-write"}[fault], func(t *testing.T) {
			f, b, _ := backendFixture(t)
			client := tenfoot.NewClient("http://native.invalid", &http.Client{Transport: b, Timeout: 10 * time.Second})
			app := tenfoot.NewApp(client, 640, 480, 100)
			if err := app.SetPreferenceStore(nativePreferences{}); err != nil {
				t.Fatal(err)
			}
			device, err := gfx.NewSoftware(640, 480)
			if err != nil {
				t.Fatal(err)
			}
			defer device.Close()
			source := &programPadReports{}
			base := &programGamepadApp{app: app, fixture: f, source: source, fault: fault}
			platform, err := NewProgramGamepadPlatform(base, b, source, gamepadDescriptor(t), input.GamepadButtons{1, 2}, gamepadKeys())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err = tenfoot.RunPlatform(ctx, app, device, platform)
			if fault != "" {
				want := ProgramParked
				if fault == "close" || fault == "rows" {
					want = ProgramRebootRequired
				}
				if err == nil || f.s.Status().State != want || f.lease.r.Active {
					t.Fatal(err, f.s.Status())
				}
				if source.err != nil && !errors.Is(err, source.err) || fault == "malformed" && !strings.Contains(err.Error(), "invalid USB gamepad report") {
					t.Fatal("lost input failure", err)
				}
				calls, polls := f.cpu.calls, source.polls
				if _, nextErr := platform.Next(context.Background()); nextErr != err || f.cpu.calls != calls || source.polls != polls {
					t.Fatal("failed source retried", nextErr)
				}
				return
			}
			if err != nil || base.cycles != 3 || f.s.Status().Generation != 6 || !f.lease.r.Active || f.cpu.keyboardWrites != 81 {
				t.Fatal(err, base.stage, base.cycles, f.s.Status(), f.cpu.keyboardWrites)
			}
		})
	}
}
