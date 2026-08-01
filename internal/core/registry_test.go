package core_test

import (
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestRegistry(t *testing.T) {
	t.Parallel()
	registry := core.DefaultRegistry()
	mega, ok := registry.Lookup(protocol.SystemMegaDrive)
	if !ok || mega.ExpectedCore != "MegaDrive" || mega.RBFSelector != "_Console/MegaDrive" || mega.FileIndex != 1 || mega.FileDelay != 1 || mega.FileType != "f" {
		t.Fatalf("unexpected Mega Drive spec: %#v, ok=%v", mega, ok)
	}
	snes, ok := registry.Lookup(protocol.SystemSNES)
	if !ok || snes.ExpectedCore != "SNES" || snes.RBFSelector != "_Console/SNES" || snes.FileIndex != 0 || snes.FileDelay != 2 || snes.FileType != "f" {
		t.Fatalf("unexpected SNES spec: %#v, ok=%v", snes, ok)
	}
	if _, ok := registry.Lookup("nes"); ok {
		t.Fatal("unexpected NES registry entry")
	}
}

func TestLookupObserved(t *testing.T) {
	t.Parallel()
	registry := core.DefaultRegistry()
	spec, ok := registry.LookupObserved("MegaDrive")
	if !ok || spec.System != protocol.SystemMegaDrive {
		t.Fatalf("LookupObserved(MegaDrive) = %#v, %v", spec, ok)
	}
	if _, ok := registry.LookupObserved("Genesis"); ok {
		t.Fatal("deprecated Genesis core must not match the POC registry")
	}
}

func TestLookupReturnsDefensiveExtensionCopy(t *testing.T) {
	t.Parallel()
	registry := core.DefaultRegistry()
	first, ok := registry.Lookup(protocol.SystemSNES)
	if !ok {
		t.Fatal("SNES registry entry missing")
	}
	delete(first.Extensions, ".sfc")
	second, ok := registry.Lookup(protocol.SystemSNES)
	if !ok {
		t.Fatal("SNES registry entry missing after mutation")
	}
	if _, ok := second.Extensions[".sfc"]; !ok {
		t.Fatal("caller mutation changed the registry")
	}
}
