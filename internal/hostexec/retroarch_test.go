package hostexec_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hostexec"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type fakeProcess struct {
	killed bool
	done   chan struct{}
}

func (p *fakeProcess) Wait() error {
	if p.done != nil {
		<-p.done
	}
	return nil
}
func (p *fakeProcess) Kill() error {
	p.killed = true
	if p.done != nil {
		close(p.done)
	}
	return nil
}

func testIdentity() protocol.ContentIdentity {
	return protocol.ContentIdentity{SHA256: "1a0806c20104d3461d8ede70362f16734dbd6a17db24005d1841a7387c9b2405", Size: 3, Extension: "sfc"}
}

func TestRetroArchAdapterBuildsArgumentVectorWithoutShell(t *testing.T) {
	var got []string
	adapter := hostexec.NewRetroArchAdapter("/Applications/RetroArch.app/Contents/MacOS/RetroArch", "/cores/snes_libretro.dylib", func(_ context.Context, name string, args ...string) (hostexec.Process, error) {
		got = append([]string{name}, args...)
		return hostexec.NoopProcess{}, nil
	})
	if _, err := adapter.Launch(context.Background(), bytes.NewReader([]byte("rom")), testIdentity()); err != nil {
		t.Fatal(err)
	}
	want := []string{"/Applications/RetroArch.app/Contents/MacOS/RetroArch", "-L", "/cores/snes_libretro.dylib"}
	if len(got) != len(want)+1 {
		t.Fatalf("args = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %#v, want prefix %#v", got, want)
		}
	}
	if got[3] == "" {
		t.Fatal("empty content path")
	}
}

func TestRetroArchAdapterRejectsEmptyContent(t *testing.T) {
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", nil)
	if _, err := adapter.Launch(context.Background(), bytes.NewReader(nil), protocol.ContentIdentity{}); err == nil {
		t.Fatal("empty content accepted")
	}
}

func TestRetroArchAdapterCapabilitiesAreExplicit(t *testing.T) {
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", nil)
	caps := adapter.Capabilities()
	if len(caps) != 1 || caps[0] != hostexec.HostOnly {
		t.Fatalf("capabilities = %#v", caps)
	}
}

func TestRetroArchAdapterStatusAndStop(t *testing.T) {
	proc := &fakeProcess{done: make(chan struct{})}
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", func(context.Context, string, ...string) (hostexec.Process, error) { return proc, nil })
	status, err := adapter.Status(context.Background())
	if err != nil || status.State != hostexec.Idle {
		t.Fatalf("initial status = %#v, %v", status, err)
	}
	if _, err := adapter.Launch(context.Background(), bytes.NewReader([]byte("rom")), testIdentity()); err != nil {
		t.Fatal(err)
	}
	status, err = adapter.Status(context.Background())
	if err != nil || status.State != hostexec.Active {
		t.Fatalf("active status = %#v, %v", status, err)
	}
	if err := adapter.Stop(context.Background()); err != nil || !proc.killed {
		t.Fatalf("stop = %v killed=%v", err, proc.killed)
	}
	status, err = adapter.Status(context.Background())
	if err != nil || status.State != hostexec.Idle {
		t.Fatalf("final status = %#v, %v", status, err)
	}
}

func TestRetroArchAdapterOwnsPreparedContentUntilStop(t *testing.T) {
	var path string
	proc := &fakeProcess{done: make(chan struct{})}
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", func(_ context.Context, _ string, args ...string) (hostexec.Process, error) {
		path = args[len(args)-1]
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		return proc, nil
	})
	if _, err := adapter.Launch(context.Background(), bytes.NewReader([]byte("rom")), testIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("content removed before Stop: %v", err)
	}
	if err := adapter.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("content after Stop err=%v", err)
	}
}

func TestRetroArchAdapterIgnoresCanceledLaunchContext(t *testing.T) {
	var got context.Context
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", func(ctx context.Context, _ string, _ ...string) (hostexec.Process, error) {
		got = ctx
		return hostexec.NoopProcess{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := adapter.Launch(ctx, bytes.NewReader([]byte("rom")), testIdentity()); err != nil {
		t.Fatal(err)
	}
	cancel()
	status, err := adapter.Status(context.Background())
	if err != nil || status.State != hostexec.Active || got.Err() != nil {
		t.Fatalf("status=%#v err=%v launchctx=%v", status, err, got.Err())
	}
}

func TestRetroArchAdapterReapsExitedProcess(t *testing.T) {
	done := make(chan struct{})
	proc := &waitProcess{done: done}
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", func(context.Context, string, ...string) (hostexec.Process, error) { return proc, nil })
	if _, err := adapter.Launch(context.Background(), bytes.NewReader([]byte("rom")), testIdentity()); err != nil {
		t.Fatal(err)
	}
	close(done)
	for i := 0; i < 100; i++ {
		status, _ := adapter.Status(context.Background())
		if status.State == hostexec.Idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("process exit was not reaped")
}

type waitProcess struct{ done <-chan struct{} }

func (p *waitProcess) Wait() error { <-p.done; return nil }
func (p *waitProcess) Kill() error { return nil }

func TestRetroArchAdapterLaunchPathUsesPlatformCoreAndKeepsLibraryFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "game.cue")
	if err := os.WriteFile(source, []byte("FILE"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []string
	adapter := hostexec.NewRetroArchAdapterWithCores("retroarch", "", map[protocol.System]string{"psx": "/cores/psx.dylib"}, func(_ context.Context, name string, args ...string) (hostexec.Process, error) {
		got = append([]string{name}, args...)
		return hostexec.NoopProcess{}, nil
	})
	if _, err := adapter.LaunchPath(context.Background(), "psx", source); err != nil {
		t.Fatal(err)
	}
	want := []string{"retroarch", "-L", "/cores/psx.dylib", source}
	if len(got) != len(want) {
		t.Fatalf("args = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("library file removed: %v", err)
	}
}

func TestRetroArchAdapterLaunchOwnedPathCleansCopyAndKeepsLibraryFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "game.cue")
	if err := os.WriteFile(source, []byte("FILE"), 0o600); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(t.TempDir(), "owned")
	if err := os.Mkdir(owned, 0o700); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(owned, "game.cue")
	if err := os.WriteFile(copyPath, []byte("FILE"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleaned := false
	proc := &fakeProcess{done: make(chan struct{})}
	adapter := hostexec.NewRetroArchAdapterWithCores("retroarch", "", map[protocol.System]string{"psx": "/cores/psx.dylib"}, func(context.Context, string, ...string) (hostexec.Process, error) {
		return proc, nil
	})
	if _, err := adapter.LaunchOwnedPath(context.Background(), "psx", copyPath, func() {
		cleaned = true
		_ = os.RemoveAll(owned)
	}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !cleaned {
		t.Fatal("owned copy was not cleaned")
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Fatalf("owned copy after Stop err=%v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("library file removed: %v", err)
	}
}

func TestRetroArchAdapterLaunchForSelectsConfiguredCore(t *testing.T) {
	var core string
	adapter := hostexec.NewRetroArchAdapterWithCores("retroarch", "/cores/default.dylib", map[protocol.System]string{"nes": "/cores/nes.dylib"}, func(_ context.Context, _ string, args ...string) (hostexec.Process, error) {
		core = args[1]
		return hostexec.NoopProcess{}, nil
	})
	if _, err := adapter.LaunchFor(context.Background(), "nes", bytes.NewReader([]byte("rom")), testIdentity()); err != nil {
		t.Fatal(err)
	}
	if core != "/cores/nes.dylib" {
		t.Fatalf("core = %q", core)
	}
}

func TestRetroArchAdapterHonorsCanceledPrepareContext(t *testing.T) {
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", func(context.Context, string, ...string) (hostexec.Process, error) {
		t.Fatal("start after canceled prepare")
		return hostexec.NoopProcess{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := adapter.Launch(ctx, bytes.NewReader([]byte("rom")), testIdentity())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled prepare = %v", err)
	}
}

func TestRetroArchAdapterHonorsCancelDuringPrepareCopy(t *testing.T) {
	gate := make(chan struct{})
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", func(context.Context, string, ...string) (hostexec.Process, error) {
		t.Fatal("start after canceled copy")
		return hostexec.NoopProcess{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	reader := &gatedByteReader{data: []byte("rom"), gate: gate}
	errCh := make(chan error, 1)
	go func() {
		_, err := adapter.Launch(ctx, reader, testIdentity())
		errCh <- err
	}()
	gate <- struct{}{}
	cancel()
	close(gate)
	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled copy = %v", err)
	}
}

type gatedByteReader struct {
	data []byte
	n    int
	gate <-chan struct{}
}

func (r *gatedByteReader) Read(p []byte) (int, error) {
	if r.n >= len(r.data) {
		return 0, io.EOF
	}
	_, ok := <-r.gate
	if !ok && r.n > 0 {
		return 0, context.Canceled
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.data[r.n]
	r.n++
	return 1, nil
}
