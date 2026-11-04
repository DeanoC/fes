// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

func backendFixture(t *testing.T) (*serviceFixture, *ProgramBackend, *hostclient.Client) {
	t.Helper()
	f := serviceTest(t)
	var entries []ProgramEntry
	for i := range 2 {
		r, _, _ := addProgram(t, f.loader, f.files, i)
		entries = append(entries, ProgramEntry{ID: fmt.Sprintf("native-%d", i), Title: fmt.Sprintf("Native %d", i), Request: r})
	}
	b, err := NewProgramBackend(f.s, entries)
	if err != nil {
		t.Fatal(err)
	}
	return f, b, hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
}
func TestProgramBackendOwnedCyclesAndStaleStop(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx := context.Background()
	var prior hostclient.SessionResult
	for i := range 3 {
		result, err := c.Launch(ctx, fmt.Sprintf("native-%d", i%2))
		if err != nil || result.State != "active" || result.CorePackage == nil || result.CorePackage.PackageID != f.s.Status().Identity.PackageID {
			t.Fatalf("launch: %+v %v", result, err)
		}
		if f.lease.r.Active || result.CorePackage.Generation != uint64(2*i+1) {
			t.Fatal("running published without ownership")
		}
		if i > 0 {
			calls := f.cpu.calls
			stale, err := c.StopExpectedStamped(ctx, prior, hostclient.ClientStamp{})
			if err == nil && stale.ErrorCode == "" {
				t.Fatal("stale stop accepted")
			}
			if f.cpu.calls != calls {
				t.Fatal("stale stop touched firmware")
			}
		}
		prior = result
		result, err = c.StopExpectedStamped(ctx, result, hostclient.ClientStamp{})
		if err != nil || result.State != "idle" || !f.lease.r.Active {
			t.Fatalf("return: %+v %v", result, err)
		}
	}
	if _, err := NewProgramBackend(f.s, b.entries); err == nil {
		t.Fatal("duplicate frontend adoption")
	}
}
func TestProgramBackendFailuresAndMalformedRequests(t *testing.T) {
	for _, fault := range []string{"identity", "reserve", "write"} {
		t.Run(fault, func(t *testing.T) {
			f, _, c := backendFixture(t)
			f.fault = fault
			result, err := c.Launch(context.Background(), "native-0")
			if err == nil && result.ErrorCode == "" {
				t.Fatal("native failure hidden")
			}
			if f.s.Status().State != ProgramRebootRequired || f.lease.r.Active {
				t.Fatal("uncertain load did not contain")
			}
			calls := f.cpu.calls
			_, _ = c.Launch(context.Background(), "native-1")
			if f.cpu.calls != calls {
				t.Fatal("retired service retried")
			}
		})
	}
	f, b, c := backendFixture(t)
	for _, data := range []string{`{"game_id":"native-0","game_id":"native-1"}`, `{"game_id":"native-0","path":"evil"}`, `{"game_id":"native-0"} {}`, `null`, `{"expected_session":{"id":"a","id":"b"}}`, strings.Repeat("x", 4097)} {
		req, _ := http.NewRequest("POST", "http://native.invalid/api/v1/session/launch", strings.NewReader(data))
		result, err := b.RoundTrip(req)
		if err != nil || result.StatusCode != 400 {
			t.Fatalf("%q: %v %v", data, result, err)
		}
		result.Body.Close()
	}
	if f.cpu.configurations != 0 {
		t.Fatal("invalid requests touched hardware")
	}
	// Missing retained content is recoverable before containment.
	saved := b.entries[0].Request.Path
	b.entries[0].Request.Path = "missing"
	result, err := c.Launch(context.Background(), "native-0")
	if err == nil && result.ErrorCode == "" {
		t.Fatal("missing package accepted")
	}
	if f.s.Status().State != ProgramReady || !f.lease.r.Active {
		t.Fatal("preflight rejection lost healthy SD")
	}
	b.entries[0].Request.Path = saved
	if _, err := c.Launch(context.Background(), "native-0"); err != nil {
		t.Fatal(err)
	}
}

type nativePreferences struct{}

func (nativePreferences) ReadPreferences() ([]byte, error) {
	return []byte(`{"home":"library","attract_enabled":false}`), nil
}
func (nativePreferences) WritePreferences([]byte) error { return nil }

type nativeAppPlatform struct {
	app                   *tenfoot.App
	service               *ProgramService
	device                *gfx.Software
	stage, cycles, frames int
	failure               string
	fixture               *serviceFixture
	sawFailure            bool
	pixels                bool
	backend               *ProgramBackend
	keyboard              ProgramKeyboard
	keyboardCycles        int
}

func (p *nativeAppPlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	select {
	case <-ctx.Done():
		return tenfoot.PlatformInput{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	input := tenfoot.PlatformInput{At: time.Now()}
	s := p.app.Snapshot()
	if p.failure == "launch" && p.service.Status().State == ProgramRebootRequired && s.Launch.ErrorCode != "" {
		p.sawFailure = true
		input.Commands = []tenfoot.Command{tenfoot.CmdQuit}
		return input, nil
	}
	if p.failure == "return" && p.stage == 2 && p.service.Status().State == ProgramRebootRequired && s.Session.RetryStop {
		p.sawFailure = true
		input.Commands = []tenfoot.Command{tenfoot.CmdQuit}
		return input, nil
	}
	switch p.stage {
	case 0:
		if len(s.Games) > 0 {
			input.Commands = []tenfoot.Command{tenfoot.CmdSelect}
			p.stage = 1
		}
	case 1:
		if s.Session.State == "active" && s.GPUParked && p.service.Status().State == ProgramRunning {
			if p.backend != nil {
				var err error
				p.keyboard, err = p.backend.BindKeyboard(ctx, p.service.Status().Generation)
				if err != nil {
					return input, err
				}
				reports := &programKeyboardReports{reports: [][]byte{make([]byte, 8), {2, 0, 4, 0, 0, 0, 0, 0}}}
				if err := p.keyboard.Poll(ctx, reports); err != nil {
					return input, err
				}
				if p.fixture.cpu.keyboardRows != ([9]uint16{0: 1 << 4, 8: 2}) {
					return input, fmt.Errorf("App play lost USB keys: %x", p.fixture.cpu.keyboardRows)
				}
				if err := p.keyboard.Report(ctx, make([]byte, 8)); err != nil {
					return input, err
				}
				if p.fixture.cpu.keyboardRows != ([9]uint16{}) {
					return input, fmt.Errorf("App play lost USB releases")
				}
				p.keyboardCycles++
			}
			if p.failure == "return" {
				p.fixture.cpu.loseClose = true
			}
			input.Commands = []tenfoot.Command{tenfoot.CmdStop}
			p.stage = 2
		}
	case 2:
		if s.Session.State == "idle" && !s.GPUParked && p.service.Status().State == ProgramReady {
			if p.backend != nil {
				calls := p.fixture.cpu.calls
				if err := p.keyboard.Report(ctx, make([]byte, 8)); err != ErrProgramSession || calls != p.fixture.cpu.calls {
					return input, fmt.Errorf("returned App keyboard remained live: %v", err)
				}
			}
			p.cycles++
			p.stage = 0
			if p.cycles == 3 {
				input.Commands = []tenfoot.Command{tenfoot.CmdQuit}
			}
		}
	}
	return input, nil
}
func (p *nativeAppPlatform) Present(_ context.Context, _ tenfoot.Snapshot) error {
	p.frames++
	if !p.pixels {
		for _, v := range p.device.Framebuffer().Pix {
			if v != 0 {
				p.pixels = true
				break
			}
		}
	}
	return nil
}
func TestProgramBackendFullTenfootAppLaunchAndReturn(t *testing.T) {
	f, b, _ := backendFixture(t)
	device, err := gfx.NewSoftware(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: b, Timeout: 10 * time.Second}), 640, 480, 100)
	if err := app.SetPreferenceStore(nativePreferences{}); err != nil {
		t.Fatal(err)
	}
	p := &nativeAppPlatform{app: app, service: f.s, device: device, backend: b, fixture: f}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := tenfoot.RunPlatform(ctx, app, device, p); err != nil {
		t.Fatalf("%v stage=%d cycles=%d snapshot=%+v session=%+v native=%+v", err, p.stage, p.cycles, app.Snapshot().Launch, app.Snapshot().Session, f.s.Status())
	}
	if p.cycles != 3 || p.keyboardCycles != 3 || !p.pixels || f.s.Status().Generation != 6 || !f.lease.r.Active {
		t.Fatalf("cycles=%d pixels=%v status=%+v", p.cycles, p.pixels, f.s.Status())
	}
}

func TestProgramBackendTenfootNativeFailures(t *testing.T) {
	for _, failure := range []string{"launch", "return"} {
		t.Run(failure, func(t *testing.T) {
			f, b, _ := backendFixture(t)
			if failure == "launch" {
				f.fault = "identity"
			}
			device, err := gfx.NewSoftware(640, 480)
			if err != nil {
				t.Fatal(err)
			}
			defer device.Close()
			app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: b, Timeout: 10 * time.Second}), 640, 480, 100)
			if err := app.SetPreferenceStore(nativePreferences{}); err != nil {
				t.Fatal(err)
			}
			p := &nativeAppPlatform{app: app, service: f.s, device: device, failure: failure, fixture: f}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := tenfoot.RunPlatform(ctx, app, device, p); err != nil {
				t.Fatalf("%v stage=%d launch=%+v session=%+v native=%+v", err, p.stage, app.Snapshot().Launch, app.Snapshot().Session, f.s.Status())
			}
			if !p.sawFailure || f.s.Status().State != ProgramRebootRequired || f.lease.r.Active {
				t.Fatal("UI hid native failure or released uncertain ownership")
			}
			calls := f.cpu.calls
			c := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
			_, _ = c.Stop(context.Background())
			_, _ = c.Launch(context.Background(), "native-1")
			if calls != f.cpu.calls {
				t.Fatal("failed UI operation was retried in firmware")
			}
		})
	}
}

func TestProgramBackendReadsRemainAvailableDuringMutation(t *testing.T) {
	f, b, _ := backendFixture(t)
	b.operation.Lock()
	defer b.operation.Unlock()
	calls, opens := f.cpu.calls, f.files.opens
	for _, path := range []string{"/api/v1/session", "/api/v1/games", "/api/v1/platforms", "/api/v1/library/collections"} {
		request, _ := http.NewRequest("GET", "http://native.invalid"+path, nil)
		response, err := b.RoundTrip(request)
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("cached read %s: %v %v", path, response, err)
		}
		response.Body.Close()
	}
	request, _ := http.NewRequest("POST", "http://native.invalid/api/v1/session/launch", strings.NewReader(`{"game_id":"native-0"}`))
	response, err := b.RoundTrip(request)
	if err != nil || response.StatusCode != 409 {
		t.Fatal("concurrent mutation was admitted")
	}
	response.Body.Close()
	if f.cpu.calls != calls || f.files.opens != opens {
		t.Fatal("cached request touched firmware or SD")
	}
}
