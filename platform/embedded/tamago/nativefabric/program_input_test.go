// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

type programInputBase struct{ presents int }

func (*programInputBase) Next(context.Context) (tenfoot.PlatformInput, error) {
	return tenfoot.PlatformInput{At: time.Now()}, nil
}
func (b *programInputBase) Present(context.Context, tenfoot.Snapshot) error { b.presents++; return nil }
func TestProgramInputRoutesMenuAndCoreAcrossReturn(t *testing.T) {
	f, b, c := backendFixture(t)
	source := &programKeyboardReports{}
	base := &programInputBase{}
	p, err := NewProgramInputPlatform(base, b, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProgramInputPlatform(base, b, source); !errors.Is(err, ErrProgramBusy) {
		t.Fatal("duplicate source owner", err)
	}
	ctx := context.Background()
	source.reports = [][]byte{make([]byte, 8), {0, 0, 40, 0, 0, 0, 0, 0}}
	frame, err := p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdSelect || f.cpu.keyboardWrites != 0 {
		t.Fatal("menu route", frame, err)
	}
	play, err := c.Launch(ctx, "native-0")
	if err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{{0, 0, 40, 0, 0, 0, 0, 0}, make([]byte, 8), {0, 0, 22, 0, 0, 0, 0, 0}, make([]byte, 8)}
	frame, err = p.Next(ctx)
	if err != nil || len(frame.Commands) != 0 || frame.Text != "" || f.cpu.keyboardWrites != 27 || f.cpu.keyboardRows != ([9]uint16{}) {
		t.Fatal("core keys became shortcuts or lost releases", frame, err, f.cpu.keyboardWrites)
	}
	old := p.keyboard
	oldSession := p.session
	if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{{0, 0, 20, 0, 0, 0, 0, 0}}
	frame, err = p.Next(ctx)
	if err != nil || len(frame.Commands) != 0 || frame.Text != "" {
		t.Fatal("held core Q leaked into menu", frame, err)
	}
	source.reports = [][]byte{make([]byte, 8), {0, 0, 20, 0, 0, 0, 0, 0}}
	frame, err = p.Next(ctx)
	if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdQuit {
		t.Fatal("fresh menu key missing", frame, err)
	}
	play, err = c.Launch(ctx, "native-1")
	if err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{make([]byte, 8), {0, 0, 4, 0, 0, 0, 0, 0}}
	if _, err = p.Next(ctx); err != nil {
		t.Fatal(err)
	}
	calls := f.cpu.calls
	if err := old.Disconnect(ctx); !errors.Is(err, ErrProgramSession) || f.cpu.calls != calls {
		t.Fatal("old input changed new core", err)
	}
	staleCleanup := &ProgramInputPlatform{backend: b, session: oldSession, keyboard: old}
	if err := staleCleanup.park(ctx); !errors.Is(err, ErrProgramSession) || f.cpu.calls != calls {
		t.Fatal("stale input cleanup parked replacement", err)
	}
	if f.cpu.keyboardRows[0] != 1<<4 {
		t.Fatal("new play lost key")
	}
	if err := p.Present(ctx, tenfoot.Snapshot{}); err != nil || base.presents != 1 {
		t.Fatal("present hook lost", err)
	}
}
func TestProgramInputSourceFailureParksOnlyOwnedPlay(t *testing.T) {
	for _, loseClose := range []bool{false, true} {
		t.Run(map[bool]string{false: "closed", true: "failed-close"}[loseClose], func(t *testing.T) {
			f, b, c := backendFixture(t)
			source := &programKeyboardReports{}
			p, err := NewProgramInputPlatform(&programInputBase{}, b, source)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := c.Launch(ctx, "native-0"); err != nil {
				t.Fatal(err)
			}
			source.reports = [][]byte{make([]byte, 8), {0, 0, 4, 0, 0, 0, 0, 0}}
			if _, err := p.Next(ctx); err != nil {
				t.Fatal(err)
			}
			source.err = errors.New("USB retired")
			f.cpu.loseClose = loseClose
			if _, err := p.Next(ctx); !errors.Is(err, source.err) {
				t.Fatal("failure hidden", err)
			}
			expected := ProgramParked
			if loseClose {
				expected = ProgramRebootRequired
			}
			if f.s.Status().State != expected || f.lease.r.Active {
				t.Fatal("input failure released or left running", f.s.Status())
			}
			calls, polls := f.cpu.calls, source.polls
			if _, err := p.Next(ctx); !errors.Is(err, source.err) {
				t.Fatal(err)
			}
			if result, err := c.Launch(ctx, "native-1"); err == nil && result.ErrorCode == "" {
				t.Fatal("failed input admitted launch")
			}
			if f.cpu.calls != calls || source.polls != polls {
				t.Fatal("failed source/control retried")
			}
		})
	}
}
func TestProgramInputReadyFailureAndBusyAdmission(t *testing.T) {
	f, b, c := backendFixture(t)
	source := &programKeyboardReports{}
	p, err := NewProgramInputPlatform(&programInputBase{}, b, source)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Next(ctx); !errors.Is(err, context.Canceled) || source.polls != 0 {
		t.Fatal("canceled input polled", err)
	}
	b.operation.Lock()
	_, err = p.Next(context.Background())
	b.operation.Unlock()
	if err != nil || source.polls != 0 {
		t.Fatal("transition consumed input", err)
	}
	for range 10 {
		source.reports = append(source.reports, make([]byte, 8))
	}
	if _, err := p.Next(context.Background()); err != nil || source.polls != 8 || len(source.reports) != 2 {
		t.Fatal("unbounded keyboard poll", err)
	}
	source.reports = nil
	source.err = errors.New("USB source failed in menu")
	calls := f.cpu.calls
	if _, err := p.Next(context.Background()); !errors.Is(err, source.err) {
		t.Fatal(err)
	}
	result, err := c.Launch(context.Background(), "native-0")
	if err == nil && result.ErrorCode == "" {
		t.Fatal("failed menu source allowed launch")
	}
	if f.cpu.calls != calls || f.s.Status().State != ProgramReady {
		t.Fatal("menu failure touched firmware")
	}
}

// The actual App consumes USB menu reports, executes real backend/service
// transitions, forwards held/released core snapshots, and returns three times.
type programInputApp struct {
	app           *tenfoot.App
	fixture       *serviceFixture
	source        *programKeyboardReports
	stage, cycles int
	fault         string
}

func (p *programInputApp) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
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
			p.source.reports = [][]byte{make([]byte, 8), {0, 0, 40, 0, 0, 0, 0, 0}}
			p.stage = 1
		}
	case 1:
		if view.Session.State == "active" && p.fixture.s.Status().State == ProgramRunning {
			p.source.reports = [][]byte{{0, 0, 40, 0, 0, 0, 0, 0}, make([]byte, 8), {2, 0, 4, 0, 0, 0, 0, 0}, make([]byte, 8)}
			p.stage = 2
		}
	case 2:
		if p.fixture.cpu.keyboardRows != ([9]uint16{}) {
			return frame, errors.New("core release missing")
		}
		if p.fault != "" {
			p.source.err = errors.New("USB input failed during play")
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
func (*programInputApp) Present(context.Context, tenfoot.Snapshot) error { return nil }
func TestProgramInputFullTenfootUSBLaunchAndReturn(t *testing.T) {
	for _, fault := range []string{"", "source", "close"} {
		t.Run(map[string]string{"": "cycles", "source": "input-failure", "close": "closure-failure"}[fault], func(t *testing.T) {
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
			source := &programKeyboardReports{}
			base := &programInputApp{app: app, fixture: f, source: source, fault: fault}
			platform, err := NewProgramInputPlatform(base, b, source)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err = tenfoot.RunPlatform(ctx, app, device, platform)
			if fault != "" {
				expected := ProgramParked
				if fault == "close" {
					expected = ProgramRebootRequired
				}
				if !errors.Is(err, source.err) || f.s.Status().State != expected || f.lease.r.Active {
					t.Fatalf("failure=%v status=%+v", err, f.s.Status())
				}
				return
			}
			if err != nil {
				t.Fatalf("%v stage=%d cycles=%d", err, base.stage, base.cycles)
			}
			if base.cycles != 3 || f.s.Status().Generation != 6 || !f.lease.r.Active || f.cpu.keyboardWrites != 81 {
				t.Fatalf("cycles=%d writes=%d state=%+v", base.cycles, f.cpu.keyboardWrites, f.s.Status())
			}

		})
	}
}

type programReportFunc func() ([]byte, error)

func (f programReportFunc) PollKeyboard() ([]byte, error) { return f() }
func TestProgramInputCanceledReportUsesFreshOwnedCleanup(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	polls := 0
	source := programReportFunc(func() ([]byte, error) {
		polls++
		if polls == 1 {
			return make([]byte, 8), nil
		}
		cancel()
		return []byte{0, 0, 4, 0, 0, 0, 0, 0}, nil
	})
	p, err := NewProgramInputPlatform(&programInputBase{}, b, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Launch(ctx, "native-0"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if f.s.Status().State != ProgramParked || f.lease.r.Active || polls != 2 {
		t.Fatal("canceled input bypassed owned cleanup", f.s.Status())
	}
	calls := f.cpu.calls
	if _, err := p.Next(context.Background()); err == nil || polls != 2 || f.cpu.calls != calls {
		t.Fatal("canceled source retried")
	}
}
