// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	stdimage "image"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

// Model elapsed secure/MMIO work independently of USB polling. The real DWC2
// driver's packet bound is separately exercised in dwc2/menu_service_test.go.
type menuInputClockCPU struct {
	*programMenuCPU
	now *time.Time
}

func (c *menuInputClockCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	*c.now = c.now.Add(2 * time.Millisecond)
	return c.programMenuCPU.Invoke(a)
}

type menuDeadlineReports struct {
	now, last *time.Time
	started   time.Time
	next      int
	polls     int
}

func (s *menuDeadlineReports) PollKeyboard() ([]byte, error) {
	s.polls++
	if s.last != nil && s.now.Sub(*s.last) >= 500*time.Millisecond {
		return nil, errors.New("pending USB packet expired")
	}
	tick := *s.now
	s.last = &tick
	if s.next >= 3 || s.now.Sub(s.started) < time.Duration(s.next+1)*300*time.Millisecond {
		return nil, nil
	}
	r := make([]byte, 8)
	if s.next != 1 {
		r[2] = 40
	}
	s.next++
	return r, nil
}

func TestProgramMenuServicesPendingUSBThroughSlowFrame(t *testing.T) {
	for _, withService := range []bool{false, true} {
		t.Run(map[bool]string{false: "unserviced-control", true: "serviced"}[withService], func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			backend, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "zx81", Title: "ZX81", Request: request}})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Unix(100, 0)
			cpu := &menuInputClockCPU{f.cpu, &now}
			session := f.s.state.menu.session
			session.bridges.cpu, session.port.cpu = cpu, cpu
			source := &menuDeadlineReports{now: &now, started: now}
			usb, err := NewProgramInputPlatform(&programInputBase{}, backend, source)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := usb.Next(ctx); err != nil {
				t.Fatal(err)
			}
			if withService {
				platform, err := NewProgramMenuPlatform(f.s, usb, func() *stdimage.RGBA { return menuFrame(0x42) })
				if err != nil {
					t.Fatal(err)
				}
				err = platform.Present(ctx, tenfoot.Snapshot{})
			} else {
				err = f.s.PresentMenu(ctx, menuFrame(0x42))
			}
			if err != nil || now.Sub(source.started) < time.Second || session.serviceInput != nil {
				t.Fatal("frame work or scoped callback invalid", now.Sub(source.started), err)
			}
			frame, err := usb.Next(ctx)
			if !withService {
				if err == nil {
					t.Fatal("unserviced control did not expire pending input")
				}
				return
			}
			if err != nil || source.next != 3 || len(frame.Commands) != 2 || frame.Commands[0] != tenfoot.CmdSelect || frame.Commands[1] != tenfoot.CmdSelect {
				t.Fatal("slow presentation lost press/release/press edges", frame, source.next, source.polls, err)
			}
			f.assertFrame(t, 0x42)
			if frame, err = usb.Next(ctx); err != nil || len(frame.Commands) != 0 {
				t.Fatal("queued edges replayed", frame, err)
			}
		})
	}
}

func TestProgramMenuBufferedInputCannotCrossLaunchReturnOrPreflight(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "launch-return", true: "rejected-content"}[reject], func(t *testing.T) {
			_, b, client := backendFixture(t)
			source := &pausableKeyboard{}
			p, err := NewProgramInputPlatform(&programInputBase{}, b, source)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			source.reports = [][]byte{{0, 0, 20, 0, 0, 0, 0, 0}}
			if err = p.serviceMenuInput(ctx); err != nil || len(p.pending) != 1 {
				t.Fatal("menu edge not buffered", err)
			}
			if reject {
				b.entries[0].Request.PackageID = "invalid"
			}
			play, launchErr := client.Launch(ctx, "native-0")
			if !reject {
				if launchErr != nil {
					t.Fatal(launchErr)
				}
				if _, err = client.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
					t.Fatal(err)
				}
			} else if launchErr == nil && play.ErrorCode == "" {
				t.Fatal("invalid content launched")
			}
			source.reports = [][]byte{{0, 0, 20, 0, 0, 0, 0, 0}}
			if frame, err := p.Next(ctx); err != nil || len(frame.Commands) != 0 {
				t.Fatal("queued or held Q crossed an ownership boundary", frame, err)
			}
			source.reports = [][]byte{make([]byte, 8), {0, 0, 20, 0, 0, 0, 0, 0}}
			if frame, err := p.Next(ctx); err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdQuit {
				t.Fatal("fresh neutral/press fence lost", frame, err)
			}
		})
	}
}

func TestProgramMenuGamepadBuffersDistinctPressEdges(t *testing.T) {
	_, b, _ := backendFixture(t)
	source := &programPadReports{}
	p, err := NewProgramGamepadPlatform(&programInputBase{}, b, source, gamepadDescriptor(t), input.GamepadButtons{Confirm: 1, Back: 2}, gamepadKeys())
	if err != nil {
		t.Fatal(err)
	}
	source.reports = [][]byte{padReport(128, 128, 1), padReport(128, 128, 0), padReport(128, 128, 1)}
	if err = p.serviceMenuInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	frame, err := p.Next(context.Background())
	if err != nil || len(frame.Commands) != 2 || frame.Commands[0] != tenfoot.CmdSelect || frame.Commands[1] != tenfoot.CmdSelect {
		t.Fatal(frame, err)
	}
}

func TestProgramMenuInputFailureRetiresWithoutReplay(t *testing.T) {
	for _, fault := range []string{"source", "overflow", "cancel", "owner"} {
		t.Run(fault, func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			backend, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "zx81", Title: "ZX81", Request: request}})
			if err != nil {
				t.Fatal(err)
			}
			source := &programKeyboardReports{}
			usb, err := NewProgramInputPlatform(&programInputBase{}, backend, source)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "cancel" {
				f.r.f.loader.boot.readerMemoryRegion.OrderFrameStores = func() error { cancel(); return nil }
			}
			if fault == "source" {
				source.err = errors.New("device disconnected")
			}
			if fault == "overflow" {
				for range maxProgramMenuEvents + 1 {
					source.reports = append(source.reports, []byte{0, 0, 40, 0, 0, 0, 0, 0}, make([]byte, 8))
				}
			}
			if fault == "owner" {
				f.menu.cpu.state.Generation++
			}
			platform, err := NewProgramMenuPlatform(f.s, usb, func() *stdimage.RGBA { return menuFrame(0x42) })
			if err != nil {
				t.Fatal(err)
			}
			if err = platform.Present(ctx, tenfoot.Snapshot{}); err == nil {
				t.Fatal("failed presentation accepted")
			}
			assertMenuRetired(t, f)
			polls, calls := source.polls, f.cpu.calls
			if err = platform.Present(context.Background(), tenfoot.Snapshot{}); err == nil || source.polls != polls || f.cpu.calls != calls || f.s.state.menu.session.serviceInput != nil {
				t.Fatal("failed source/display retried", err)
			}
		})
	}
}
