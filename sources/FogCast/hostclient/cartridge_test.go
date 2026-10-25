package hostclient

import (
	"strings"
	"testing"
)

// The swap admits a single linked cartridge, or the cartridge source after a
// linked BIOS, and sizes the dump by that cartridge alone.
func TestCartridgeSwapSizesTheLinkedCartridge(t *testing.T) {
	base := func() *SessionCorePackage {
		return &SessionCorePackage{
			PackageID: strings.Repeat("a", 64), Generation: 1,
			ABI: SessionCoreABI{ID: "fes.application", Major: 1},
			ActiveInterfaces: []SessionCoreInterface{
				{ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}},
		}
	}
	single := base()
	single.ROMLink = &SessionROMLink{ROMID: "cartridge-rom", SourceSize: 16384}
	linked := base()
	linked.ROMLinks = &SessionROMLinks{Sources: []SessionROMSource{
		{ID: "coleco-bios", Role: "firmware", SourceSize: 8192},
		{ID: "coleco-cart", Role: "cartridge", SourceSize: 16384}}}
	for name, p := range map[string]*SessionCorePackage{"single": single, "bios-and-cartridge": linked} {
		if !CartridgeSwapCapable(p) || !AdmitCartridge(p, "game.col", 16384) {
			t.Fatalf("%s: linked cartridge not swappable", name)
		}
		if AdmitCartridge(p, "game.col", 8192) || AdmitCartridge(p, "game.col", 24576) {
			t.Fatalf("%s: dump admitted at the wrong size", name)
		}
	}
	firmwareOnly := base()
	firmwareOnly.ROMLinks = &SessionROMLinks{Sources: []SessionROMSource{
		{ID: "a", Role: "firmware", SourceSize: 8192}, {ID: "b", Role: "firmware", SourceSize: 16384}}}
	wrongFirstRole := base()
	wrongFirstRole.ROMLinks = &SessionROMLinks{Sources: []SessionROMSource{
		{ID: "a", Role: "cartridge", SourceSize: 8192}, {ID: "b", Role: "cartridge", SourceSize: 16384}}}
	missingFirmwareSize := base()
	missingFirmwareSize.ROMLinks = &SessionROMLinks{Sources: []SessionROMSource{
		{ID: "a", Role: "firmware"}, {ID: "b", Role: "cartridge", SourceSize: 16384}}}
	missingCartridgeSize := base()
	missingCartridgeSize.ROMLinks = &SessionROMLinks{Sources: []SessionROMSource{
		{ID: "a", Role: "firmware", SourceSize: 8192}, {ID: "b", Role: "cartridge"}}}
	both := base()
	both.ROMLink, both.ROMLinks = single.ROMLink, linked.ROMLinks
	short := base()
	short.ROMLinks = &SessionROMLinks{Sources: linked.ROMLinks.Sources[1:]}
	for name, p := range map[string]*SessionCorePackage{"no-rom": base(), "firmware-only": firmwareOnly,
		"wrong-first-role": wrongFirstRole, "missing-firmware-size": missingFirmwareSize, "missing-cartridge-size": missingCartridgeSize,
		"both-forms": both, "one-source": short} {
		if CartridgeSwapCapable(p) {
			t.Fatalf("%s: admitted as a cartridge machine", name)
		}
	}
}
