package tenfoot

import (
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

func TestNormalizedGFXDefaultsToSDL(t *testing.T) {
	t.Setenv("TENFOOT_GFX", "")
	opts := Options{PrefsPath: t.TempDir() + "/missing.json"}.normalized()
	name, err := gfx.ParseBackend(opts.GFX)
	if err != nil {
		t.Fatal(err)
	}
	if name != gfx.BackendSDL {
		t.Fatalf("default gfx %q", name)
	}
}

func TestNormalizedGFXFromEnv(t *testing.T) {
	t.Setenv("TENFOOT_GFX", "software")
	opts := Options{PrefsPath: t.TempDir() + "/missing.json"}.normalized()
	if opts.GFX != "software" {
		t.Fatalf("env gfx %q", opts.GFX)
	}
	t.Setenv("TENFOOT_GFX", "fpga-stub")
	opts = Options{GFX: "software", PrefsPath: t.TempDir() + "/missing.json"}.normalized()
	if opts.GFX != "software" {
		t.Fatalf("explicit GFX should win over env, got %q", opts.GFX)
	}
}

func TestNormalizedGFXFPGA(t *testing.T) {
	t.Setenv("TENFOOT_GFX", "fpga")
	opts := Options{PrefsPath: t.TempDir() + "/missing.json"}.normalized()
	name, err := gfx.ParseBackend(opts.GFX)
	if err != nil {
		t.Fatal(err)
	}
	if name != gfx.BackendFPGA {
		t.Fatalf("fpga gfx %q", name)
	}
	stub, err := gfx.ParseBackend("fpga-stub")
	if err != nil || stub != gfx.BackendFPGAStub {
		t.Fatalf("fpga-stub %q %v", stub, err)
	}
}
