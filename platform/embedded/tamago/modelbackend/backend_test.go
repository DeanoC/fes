// SPDX-License-Identifier: GPL-3.0-or-later
package modelbackend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

func fixtures(t *testing.T) (runtimeadmission.Policy, Config, func(string) ([]byte, error)) {
	t.Helper()
	dir := os.Getenv("FES_MODEL_FIXTURES")
	if dir == "" {
		t.Skip("FES_MODEL_FIXTURES must name a run.py play package-fixtures directory")
	}
	read := func(path string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, filepath.Base(path))) }
	raw, err := read("RUNTIME.JSON")
	if err != nil {
		t.Fatal(err)
	}
	var policy runtimeadmission.Policy
	if err = json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	raw, err = read("PLAY.JSON")
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	return policy, config, read
}
func closeBackend(t *testing.T, b *Backend) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
func wait(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("model did not reach expected state")
		}
		time.Sleep(time.Millisecond)
	}
}

type preferences struct{}

func (preferences) ReadPreferences() ([]byte, error) {
	return []byte(`{"home":"library","attract_enabled":false}`), nil
}
func (preferences) WritePreferences([]byte) error { return nil }

type scriptPlatform struct {
	app                  *tenfoot.App
	backend              *Backend
	entries              []Entry
	index, stage, frames int
	before               View
	active               View
	frame                *Frame
	old                  Frame
	priorGeneration      uint64
	sawPixels            bool
	device               *gfx.Software
}

func (p *scriptPlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	select {
	case <-ctx.Done():
		return tenfoot.PlatformInput{}, ctx.Err()
	case <-time.After(time.Millisecond):
	}
	if p.index == len(p.entries) {
		return tenfoot.PlatformInput{}, io.EOF
	}
	s := p.app.Snapshot()
	view := p.backend.View()
	input := tenfoot.PlatformInput{At: time.Now()}
	if !s.Loading && len(s.Games) == len(p.entries) {
		entry := p.entries[p.index]
		switch p.stage {
		case 0:
			if s.Grid.Focus != p.index {
				command := tenfoot.CmdRight
				columns := s.Grid.Columns
				if p.index/columns > s.Grid.Focus/columns {
					command = tenfoot.CmdDown
				} else if p.index/columns < s.Grid.Focus/columns {
					command = tenfoot.CmdUp
				} else if p.index < s.Grid.Focus {
					command = tenfoot.CmdLeft
				}
				input.Commands = []tenfoot.Command{command}
				break
			}
			p.before = view
			input.Commands = []tenfoot.Command{tenfoot.CmdSelect}
			p.stage = 1
		case 1:
			if s.Launch.GameID != entry.ID || view.Busy {
				break
			}
			success := entry.Check == "return" || entry.Check == "retry"
			if success {
				if s.Session.State != "active" || !s.GPUParked || view.Status.State != fabricmodel.Running {
					break
				}
				if view.Status.Package.ActivationReceiptSHA256 != entry.ReceiptSHA256 {
					return input, errors.New("UI published wrong retained receipt")
				}
				if entry.CompositionID != "" {
					client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: p.backend})
					session, err := client.Session(ctx)
					if err != nil || session.CorePackage == nil || session.CorePackage.PartsComposition == nil || session.CorePackage.PartsComposition.ID != entry.CompositionID || view.PartsCompositionID != entry.CompositionID {
						return input, errors.New("UI session lost part composition identity")
					}
				}
				if err := p.backend.Input(ctx, view.Status.CoreGeneration, "right"); err != nil {
					return input, err
				}
				if p.priorGeneration != 0 {
					if err := p.backend.Input(ctx, p.priorGeneration, "delayed-right"); !errors.Is(err, fabricmodel.ErrStale) {
						return input, errors.New("old input generation survived relaunch")
					}
				}
				p.active = view
				p.stage = 2
			} else {
				if s.Launch.Phase != "error" && s.Launch.Phase != "host" || s.Session.State != "idle" || s.GPUParked || view.Status.State != fabricmodel.Idle {
					break
				}
				if entry.Check == "preserve" {
					if view.Status != p.before.Status || view.Programs != p.before.Programs {
						return input, errors.New("failed preparation retired current owner")
					}
				}
				p.index++
				p.stage = 0
			}
		case 2:
			input.Commands = []tenfoot.Command{tenfoot.CmdBack}
			if entry.Check == "retry" {
				p.stage = 3
			} else {
				p.stage = 4
			}
		case 3:
			if !s.Session.RetryStop || view.Busy {
				break
			}
			if view.Status.Package != p.active.Status.Package || view.Status.CoreGeneration != p.active.Status.CoreGeneration || view.Status.InputEnabled {
				return input, errors.New("save failure lost owner or restored input")
			}
			input.Commands = []tenfoot.Command{tenfoot.CmdBack}
			p.stage = 4
		case 4:
			if s.Session.State != "idle" || s.GPUParked || s.Session.RetryStop || view.Busy {
				break
			}
			if s.Grid.Focus != p.index {
				return input, errors.New("stop lost selection")
			}
			if err := p.backend.Input(ctx, p.active.Status.CoreGeneration, "late"); !errors.Is(err, fabricmodel.ErrStale) {
				return input, errors.New("stale input accepted after return")
			}
			if err := p.backend.FinishFrame(ctx, p.old); !errors.Is(err, fabricmodel.ErrStale) {
				return input, errors.New("stale frame accepted after return")
			}
			p.priorGeneration = p.active.Status.CoreGeneration
			p.index++
			p.stage = 0
		}
	}
	frame, err := p.backend.BeginFrame(ctx)
	if err == nil {
		p.frame = &frame
	} else if !errors.Is(err, fabricmodel.ErrBusy) {
		return input, err
	}
	return input, nil
}
func (p *scriptPlatform) Present(ctx context.Context, s tenfoot.Snapshot) error {
	p.frames++
	if p.frame != nil {
		if err := p.backend.FinishFrame(ctx, *p.frame); err != nil {
			return err
		}
		if s.Session.State == "active" {
			p.old = *p.frame
		}
		p.frame = nil
	}
	if !p.sawPixels {
		for _, v := range p.device.Framebuffer().Pix {
			if v != 0 {
				p.sawPixels = true
				break
			}
		}
	}
	return nil
}
func TestTenfootLaunchReturnAndFailures(t *testing.T) {
	policy, config, read := fixtures(t)
	var mu sync.Mutex
	var events []Event
	backend, err := New(policy, config, read, func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend(t, backend)
	device, err := gfx.NewSoftware(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	app := tenfoot.NewApp(tenfoot.NewClient("http://model.invalid", &http.Client{Transport: backend, Timeout: 10 * time.Second}), 640, 480, 100)
	if err = app.SetPreferenceStore(preferences{}); err != nil {
		t.Fatal(err)
	}
	p := &scriptPlatform{app: app, backend: backend, entries: config.Entries, device: device}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = tenfoot.RunPlatform(ctx, app, device, p); err != nil {
		t.Fatalf("%v stage=%d index=%d snapshot=%+v backend=%+v", err, p.stage, p.index, app.Snapshot().Launch, backend.View())
	}
	if p.index != len(config.Entries) || p.frames < 20 || !p.sawPixels {
		t.Fatalf("cases=%d frames=%d pixels=%t", p.index, p.frames, p.sawPixels)
	}
	mu.Lock()
	defer mu.Unlock()
	var operations []fabricmodel.Operation
	for _, event := range events {
		if event.Kind == "command" && event.Game == "model-normal" {
			operations = append(operations, event.Operation)
		}
	}
	want := []fabricmodel.Operation{fabricmodel.Drain, fabricmodel.Contain, fabricmodel.Program, fabricmodel.Identify, fabricmodel.Release, fabricmodel.Save, fabricmodel.Drain, fabricmodel.Contain, fabricmodel.Program, fabricmodel.Identify, fabricmodel.Release}
	if len(operations) != len(want) {
		t.Fatalf("normal cycle commands=%v", operations)
	}
	for i := range want {
		if operations[i] != want[i] {
			t.Fatal(operations)
		}
	}
}
func TestHeldFrameAndStaleStop(t *testing.T) {
	policy, config, read := fixtures(t)
	backend, err := New(policy, config, read, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend(t, backend)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
	frame, err := backend.BeginFrame(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := client.Launch(ctx, "model-normal"); done <- err }()
	wait(t, func() bool { return backend.View().Status.State == fabricmodel.Changing })
	time.Sleep(30 * time.Millisecond)
	if backend.View().Programs != 0 {
		t.Fatal("programmed while renderer retained a frame")
	}
	if err = backend.FinishFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	prior, err := client.Session(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.StopExpectedStamped(ctx, prior, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Launch(ctx, "model-normal"); err != nil {
		t.Fatal(err)
	}
	before := backend.View().Status
	if result, stopErr := client.StopExpectedStamped(ctx, prior, hostclient.ClientStamp{}); stopErr == nil && result.ErrorCode == "" {
		t.Fatal("stale stop accepted")
	}
	if backend.View().Status != before {
		t.Fatal("stale stop changed replacement")
	}
}
func TestCanceledRequestPreservesOwner(t *testing.T) {
	policy, config, read := fixtures(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	backend, err := New(policy, config, func(path string) ([]byte, error) {
		if path == config.Entries[0].Sources[0].Path {
			once.Do(func() { close(entered); <-release })
		}
		return read(path)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend(t, backend)
	before := backend.View().Status
	ctx, cancel := context.WithCancel(context.Background())
	client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
	done := make(chan error, 1)
	go func() { _, err := client.Launch(ctx, "model-normal"); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("preparation did not read source")
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	close(release)
	wait(t, func() bool { return !backend.View().Busy })
	if backend.View().Status != before || backend.View().Programs != 0 {
		t.Fatal("canceled request changed owner")
	}
}

func TestCloseRefusesOutstandingFrame(t *testing.T) {
	policy, config, read := fixtures(t)
	for _, running := range []bool{false, true} {
		backend, err := New(policy, config, read, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if running {
			client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
			if _, err = client.Launch(ctx, "model-normal"); err != nil {
				t.Fatal(err)
			}
		}
		before := backend.View().Status
		frame, err := backend.BeginFrame(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = backend.Close(ctx); !errors.Is(err, fabricmodel.ErrBusy) {
			t.Fatalf("closed over renderer ownership: %v", err)
		}
		if backend.View().Status != before {
			t.Fatal("refused close changed owner")
		}
		if err = backend.FinishFrame(ctx, frame); err != nil {
			t.Fatal(err)
		}
		closeBackend(t, backend)
		cancel()
	}
}

func TestCancelAfterAdmissionRecoversBeforeRelease(t *testing.T) {
	policy, config, read := fixtures(t)
	var mu sync.Mutex
	released := false
	backend, err := New(policy, config, read, func(e Event) {
		if e.Operation == fabricmodel.Release && e.Status.State == fabricmodel.Running {
			mu.Lock()
			released = true
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend(t, backend)
	ctx, cancel := context.WithCancel(context.Background())
	frame, err := backend.BeginFrame(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
	done := make(chan error, 1)
	go func() { _, err := client.Launch(ctx, "model-normal"); done <- err }()
	wait(t, func() bool { return backend.View().Status.State == fabricmodel.Changing })
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = backend.FinishFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !backend.View().Busy })
	if backend.View().Status.State != fabricmodel.Idle || backend.View().Status.Package.PackageID != "" {
		t.Fatal("canceled activation did not recover to idle")
	}
	mu.Lock()
	defer mu.Unlock()
	if released {
		t.Fatal("canceled target released before recovery")
	}
}

func TestBadPartPreservesRunningComposition(t *testing.T) {
	policy, config, read := fixtures(t)
	backend, err := New(policy, config, read, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend(t, backend)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
	result, err := client.Launch(ctx, "model-composed")
	if err != nil || result.ErrorCode != "" {
		t.Fatalf("composed launch: %v %+v", err, result)
	}
	before := backend.View()
	frame, err := backend.BeginFrame(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"model-part-changed", "model-part-missing", "model-part-incompatible"} {
		result, err = client.Launch(ctx, id)
		if err != nil || result.ErrorCode == "" {
			t.Fatalf("invalid part was not rejected: %s %v %+v", id, err, result)
		}
		after := backend.View()
		if after.Status != before.Status || after.PartsCompositionID != before.PartsCompositionID || after.Programs != before.Programs || after.Saves != before.Saves {
			t.Fatal("rejected part retired the running composition")
		}
		current, err := client.Session(ctx)
		if err != nil || current.State != "active" || current.CorePackage == nil || current.CorePackage.PartsComposition == nil || current.CorePackage.PartsComposition.ID != before.PartsCompositionID {
			t.Fatal("rejection changed the published composition", err)
		}
	}
	if err = backend.FinishFrame(ctx, frame); err != nil {
		t.Fatal("rejection invalidated outstanding frame", err)
	}
}
