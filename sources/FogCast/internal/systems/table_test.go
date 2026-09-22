package systems

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestSMBFolderUsesMappedAlias(t *testing.T) {
	snes, ok := SMBFolder(DefaultSMBShareRoot, protocol.SystemSNES)
	if !ok || snes != "//deano-clawz/Games/Games/SNES" {
		t.Fatalf("SNES folder = %q, %v", snes, ok)
	}
	mega, ok := SMBFolder(DefaultSMBShareRoot+"/", protocol.SystemMegaDrive)
	if !ok || mega != "//deano-clawz/Games/Games/Genesis" {
		t.Fatalf("Mega Drive folder = %q, %v", mega, ok)
	}
	nes, ok := SMBFolder(DefaultSMBShareRoot, protocol.SystemNES)
	if !ok || nes != "//deano-clawz/Games/Games/NES" {
		t.Fatalf("NES folder = %q, %v", nes, ok)
	}
	sms, ok := SMBFolder(DefaultSMBShareRoot, protocol.SystemSMS)
	if !ok || sms != "//deano-clawz/Games/Games/SMS" {
		t.Fatalf("SMS folder = %q, %v", sms, ok)
	}
	for _, test := range []struct {
		system protocol.System
		alias  string
	}{
		{protocol.SystemGameBoy, "gb"},
		{protocol.SystemGBA, "gba"},
		{protocol.SystemPCE, "pce"},
		{protocol.SystemGameGear, "gg"},
		{protocol.SystemGameBoyColor, "Game Boy Color"},
		{protocol.SystemAtari2600, "Atari2600"},
		{protocol.SystemColecoVision, "ColecoVision"},
		{protocol.SystemAtariLynx, "AtariLynx"},
		{protocol.SystemWonderSwan, "WonderSwan"},
		{protocol.SystemWonderSwanColor, "WonderSwan Color"},
		{protocol.SystemAtari7800, "Atari7800"},
		{protocol.SystemIntellivision, "Intellivision"},
	} {
		folder, ok := SMBFolder(DefaultSMBShareRoot, test.system)
		want := DefaultSMBShareRoot + "/" + test.alias
		if !ok || folder != want {
			t.Fatalf("%s folder = %q, %v; want %q", test.system, folder, ok, want)
		}
	}
}

func TestTagsAreClassifiedAndCopied(t *testing.T) {
	row, ok := Lookup(protocol.SystemColecoVision)
	if !ok {
		t.Fatal("colecovision missing")
	}
	want := map[string]bool{"cpu:z80": true, "vdp:tms9918": true, "vdp:tms9918-family": true}
	for _, tag := range row.Tags {
		delete(want, tag)
	}
	if len(want) != 0 {
		t.Fatalf("colecovision missing tags %v (have %v)", want, row.Tags)
	}
	row.Tags[0] = "mutated"
	again, _ := Lookup(protocol.SystemColecoVision)
	if again.Tags[0] == "mutated" {
		t.Fatal("Lookup must return a defensive copy of Tags")
	}
	for _, r := range Rows() {
		for _, tag := range r.Tags {
			if !strings.Contains(tag, ":") && tag != "handheld" && tag != "discrete-logic" {
				t.Errorf("%s: tag %q should be namespaced", r.PlatformID, tag)
			}
		}
	}
}

func TestCatalogRowsHaveNoImplicitFPGALaunch(t *testing.T) {
	for _, row := range Rows() {
		if row.Capability != CapabilityCatalog && row.Capability != CapabilityHostOnly {
			t.Fatalf("unexpected capability for %s: %s", row.PlatformID, row.Capability)
		}
	}
}
