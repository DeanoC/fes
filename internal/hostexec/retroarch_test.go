package hostexec_test

import (
	"context"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/hostexec"
)

type fakeProcess struct{ killed bool }

func (p *fakeProcess) Wait() error { return nil }
func (p *fakeProcess) Kill() error { p.killed = true; return nil }

func TestRetroArchAdapterBuildsArgumentVectorWithoutShell(t *testing.T) {
	var got []string
	adapter := hostexec.NewRetroArchAdapter("/Applications/RetroArch.app/Contents/MacOS/RetroArch", "/cores/snes_libretro.dylib", func(_ context.Context, name string, args ...string) (hostexec.Process, error) {
		got = append([]string{name}, args...)
		return hostexec.NoopProcess{}, nil
	})
	if adapter.ID() != "retroarch" {
		t.Fatalf("id = %q", adapter.ID())
	}
	if _, err := adapter.Launch(context.Background(), "/private/game.sfc"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/Applications/RetroArch.app/Contents/MacOS/RetroArch", "-L", "/cores/snes_libretro.dylib", "/private/game.sfc"}
	if len(got) != len(want) {
		t.Fatalf("args = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	}
}

func TestRetroArchAdapterRejectsEmptyContentPath(t *testing.T) {
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", nil)
	if _, err := adapter.Launch(context.Background(), ""); err == nil {
		t.Fatal("empty content path accepted")
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
	proc := &fakeProcess{}
	adapter := hostexec.NewRetroArchAdapter("retroarch", "core", func(context.Context, string, ...string) (hostexec.Process, error) { return proc, nil })
	status, err := adapter.Status(context.Background())
	if err != nil || status.State != hostexec.Idle {
		t.Fatalf("initial status = %#v, %v", status, err)
	}
	if _, err := adapter.Launch(context.Background(), "/tmp/game.sfc"); err != nil {
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
