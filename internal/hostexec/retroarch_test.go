package hostexec_test

import (
	"bytes"
	"context"
	"os"
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
