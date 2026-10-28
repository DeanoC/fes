package protocol

import (
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

// A linked cartridge is swappable alone or after a linked firmware source; a
// malformed pair, or a load that carries both forms, is not.
func TestCartridgeSwapCapableAdmitsLinkedCartridgeShapes(t *testing.T) {
	base := func() *CorePackageStatus {
		return &CorePackageStatus{ABI: RuntimeContract{ID: "fes.application", Major: 1},
			ActiveInterfaces: []RuntimeInterface{{ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}
	}
	sources := []corepackage.ROMSourceIdentity{
		{ID: "coleco-bios", Role: "firmware", SourceSize: 8192},
		{ID: "coleco-cart", Role: "cartridge", SourceSize: 16384}}
	single := base()
	single.ROMLink = &corepackage.ROMLinkIdentity{ROMID: "cartridge-rom", Role: "cartridge", SourceSize: 16384}
	linked := base()
	linked.ROMLinks = &corepackage.ROMLinksIdentity{Sources: sources}
	for name, p := range map[string]*CorePackageStatus{"single": single, "bios-and-cartridge": linked} {
		if !CartridgeSwapCapable(p) {
			t.Fatalf("%s: linked cartridge machine refused", name)
		}
	}
	firmware := base()
	firmware.ROMLinks = &corepackage.ROMLinksIdentity{Sources: []corepackage.ROMSourceIdentity{sources[0], sources[0]}}
	wrongFirstRole := base()
	wrongFirstRole.ROMLinks = &corepackage.ROMLinksIdentity{Sources: []corepackage.ROMSourceIdentity{
		{ID: "coleco-bios", Role: "cartridge", SourceSize: 8192}, sources[1]}}
	missingFirmwareSize := base()
	missingFirmwareSize.ROMLinks = &corepackage.ROMLinksIdentity{Sources: []corepackage.ROMSourceIdentity{
		{ID: "coleco-bios", Role: "firmware", SourceSize: 0}, sources[1]}}
	missingCartridgeSize := base()
	missingCartridgeSize.ROMLinks = &corepackage.ROMLinksIdentity{Sources: []corepackage.ROMSourceIdentity{
		sources[0], {ID: "coleco-cart", Role: "cartridge", SourceSize: 0}}}
	missingSingleSourceSize := base()
	missingSingleSourceSize.ROMLink = &corepackage.ROMLinkIdentity{ROMID: "cartridge-rom", Role: "cartridge"}
	missingSingleRole := base()
	missingSingleRole.ROMLink = &corepackage.ROMLinkIdentity{ROMID: "cartridge-rom", SourceSize: 16384}
	firmwareROM := base()
	firmwareROM.ROMLink = &corepackage.ROMLinkIdentity{ROMID: "bios-rom", Role: "firmware", SourceSize: 8192}
	both := base()
	both.ROMLink, both.ROMLinks = single.ROMLink, linked.ROMLinks
	plain := base()
	computer := base()
	computer.ABI, computer.ROMLinks = RuntimeContract{ID: "fes.computer", Major: 1}, linked.ROMLinks
	noPlane := base()
	noPlane.ActiveInterfaces, noPlane.ROMLinks = nil, linked.ROMLinks
	for name, p := range map[string]*CorePackageStatus{"firmware-only": firmware, "wrong-first-role": wrongFirstRole,
		"missing-firmware-size": missingFirmwareSize, "missing-cartridge-size": missingCartridgeSize, "firmware-rom": firmwareROM,
		"missing-single-source-size": missingSingleSourceSize, "missing-single-role": missingSingleRole, "both-forms": both,
		"no-rom": plain, "computer": computer, "no-plane": noPlane} {
		if CartridgeSwapCapable(p) {
			t.Fatalf("%s: admitted as a cartridge machine", name)
		}
	}
}
