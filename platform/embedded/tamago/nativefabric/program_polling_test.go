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
)

type pausableKeyboard struct {
	programKeyboardReports
	paused              bool
	pauses, resumes     int
	pauseErr, resumeErr error
	duringPause         func()
}

func TestProgramPollingActualTenfootCycles(t *testing.T) {
	f, b, _ := backendFixture(t)
	app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: b, Timeout: 10 * time.Second}), 640, 480, 100)
	if err := app.SetPreferenceStore(nativePreferences{}); err != nil {
		t.Fatal(err)
	}
	device, err := gfx.NewSoftware(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	source := &pausableKeyboard{}
	base := &programInputApp{app: app, fixture: f, source: &source.programKeyboardReports}
	platform, err := NewProgramInputPlatform(base, b, source)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tenfoot.RunPlatform(ctx, app, device, platform); err != nil {
		t.Fatal(err)
	}
	if base.cycles != 3 || f.s.Status().Generation != 6 || !f.lease.r.Active || f.cpu.keyboardWrites != 81 || source.pauses != 6 || source.resumes != 6 || source.paused {
		t.Fatal("incomplete paused-input cycle", base.cycles, f.s.Status(), source.pauses, source.resumes)
	}
}

func TestProgramPollingNativeFailuresKeepUSBDrained(t *testing.T) {
	for _, phase := range []string{"configuration", "return", "park"} {
		t.Run(phase, func(t *testing.T) {
			f, b, c := backendFixture(t)
			source := &pausableKeyboard{}
			if _, err := NewProgramInputPlatform(&programInputBase{}, b, source); err != nil {
				t.Fatal(err)
			}
			if phase == "configuration" {
				f.fault = "identity"
			}
			if phase == "park" {
				source.resumeErr = errors.New("resume failed")
				f.cpu.loseClose = true
			}
			play, err := c.Launch(context.Background(), "native-0")
			if phase == "return" {
				if err != nil {
					t.Fatal(err)
				}
				f.cpu.loseClose = true
				_, err = c.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{})
			}
			if f.s.Status().State != ProgramRebootRequired || f.lease.r.Active || !source.paused || b.inputFault == nil {
				t.Fatal("failure resumed resources", err, f.s.Status(), source.paused)
			}
			if phase == "configuration" && source.resumes != 0 || phase == "return" && source.resumes != 1 {
				t.Fatal("failed native path resumed input", source.resumes)
			}
			calls, pauses, resumes := f.cpu.calls, source.pauses, source.resumes
			c.Launch(context.Background(), "native-1")
			if f.cpu.calls != calls || source.pauses != pauses || source.resumes != resumes {
				t.Fatal("retired transition retried")
			}
		})
	}
}

func (s *pausableKeyboard) PausePolling(context.Context) error {
	s.pauses++
	if s.duringPause != nil {
		s.duringPause()
	}
	if s.pauseErr != nil {
		return s.pauseErr
	}
	if s.paused {
		return errors.New("duplicate pause")
	}
	s.paused = true
	s.reports = nil
	return nil
}
func (s *pausableKeyboard) ResumePolling(context.Context) error {
	s.resumes++
	if s.resumeErr != nil {
		return s.resumeErr
	}
	if !s.paused {
		return errors.New("unpaired resume")
	}
	s.paused = false
	return nil
}
func (s *pausableKeyboard) PollKeyboard() ([]byte, error) {
	if s.paused {
		return nil, errors.New("polled during transition")
	}
	return s.programKeyboardReports.PollKeyboard()
}
func TestProgramPollingBoundariesCoverLaunchAndReturn(t *testing.T) {
	f, b, c := backendFixture(t)
	source := &pausableKeyboard{}
	p, err := NewProgramInputPlatform(&programInputBase{}, b, source)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range 3 {
		source.duringPause = func() {
			if _, err := p.Next(ctx); err != nil {
				t.Fatal(err)
			}
		}
		play, err := c.Launch(ctx, "native-0")
		if err != nil {
			t.Fatal(err)
		}
		if source.paused {
			t.Fatal("not resumed")
		}
		source.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}, make([]byte, 8), {0, 0, 4, 0, 0, 0, 0, 0}}
		if frame, err := p.Next(ctx); err != nil || len(frame.Commands) != 0 || f.cpu.keyboardRows[0] != 1<<4 {
			t.Fatal("core input fence", frame, err)
		}
		if _, err := c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{}); err != nil {
			t.Fatal(err)
		}
		source.reports = [][]byte{{0, 0, 20, 0, 0, 0, 0, 0}}
		if frame, err := p.Next(ctx); err != nil || len(frame.Commands) != 0 {
			t.Fatal("held input crossed return", frame, err)
		}
	}
	if source.pauses != 6 || source.resumes != 6 || b.inputBoundary != 6 {
		t.Fatal(source.pauses, source.resumes, b.inputBoundary)
	}
}
func TestProgramPollingFailedPreflightRequiresFreshMenuNeutral(t *testing.T) {
	f, b, c := backendFixture(t)
	source := &pausableKeyboard{}
	p, err := NewProgramInputPlatform(&programInputBase{}, b, source)
	if err != nil {
		t.Fatal(err)
	}
	b.entries[0].Request.PackageID = strings.Repeat("0", 64)
	calls := f.cpu.calls
	result, err := c.Launch(context.Background(), "native-0")
	if err == nil && result.ErrorCode == "" {
		t.Fatal("invalid package launched")
	}
	if f.cpu.calls != calls || f.s.Status().State != ProgramReady || source.paused {
		t.Fatal("preflight changed ownership")
	}
	source.reports = [][]byte{{0, 0, 20, 0, 0, 0, 0, 0}}
	if frame, err := p.Next(context.Background()); err != nil || len(frame.Commands) != 0 {
		t.Fatal("held transition key became menu command", frame, err)
	}
	source.reports = [][]byte{make([]byte, 8), {0, 0, 20, 0, 0, 0, 0, 0}}
	if frame, err := p.Next(context.Background()); err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdQuit {
		t.Fatal("fresh input lost", frame, err)
	}
}
func TestProgramPollingFailureParksExactPlayOrBlocksLaunch(t *testing.T) {
	for _, phase := range []string{"before-launch", "after-launch", "before-return", "after-return"} {
		t.Run(phase, func(t *testing.T) {
			f, b, c := backendFixture(t)
			source := &pausableKeyboard{}
			_, err := NewProgramInputPlatform(&programInputBase{}, b, source)
			if err != nil {
				t.Fatal(err)
			}
			fault := errors.New("polling failure")
			ctx := context.Background()
			if phase == "before-launch" {
				source.pauseErr = fault
			}
			if phase == "after-launch" {
				source.resumeErr = fault
			}
			play, err := c.Launch(ctx, "native-0")
			if strings.Contains(phase, "return") {
				if err != nil {
					t.Fatal(err)
				}
				if phase == "before-return" {
					source.pauseErr = fault
				} else {
					source.resumeErr = fault
				}
				_, err = c.StopExpectedStamped(ctx, play, hostclient.ClientStamp{})
			}
			if b.inputFault == nil {
				t.Fatal("input failure not latched", err)
			}
			state := f.s.Status().State
			if phase == "before-launch" && state != ProgramReady {
				t.Fatal(state)
			}
			if (phase == "after-launch" || phase == "before-return") && (state != ProgramParked || f.lease.r.Active) {
				t.Fatal("failed input left core running or SD resumed", state, f.lease.r)
			}
			if phase == "after-return" && state != ProgramReady {
				t.Fatal(state)
			}
			calls := f.cpu.calls
			result, err := c.Launch(ctx, "native-1")
			if err == nil && result.ErrorCode == "" || f.cpu.calls != calls {
				t.Fatal("retired source allowed another load")
			}
		})
	}
}
