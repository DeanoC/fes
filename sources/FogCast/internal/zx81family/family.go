// Package zx81family names the machines built from the ZX81 shell.
// The setup room, package admission and the hardware shelf share this list.
package zx81family

// Variant is one compile of the ZX81 shell. Detail is the video standard,
// BASIC size and internal RAM, without a second copy of the machine name.
type Variant struct {
	CoreID string
	Label  string
	Detail string
}

// Variants returns the family in setup-room order.
func Variants() []Variant {
	return []Variant{
		{CoreID: "fes.zx81", Label: "Sinclair ZX81", Detail: "PAL · 8 KiB BASIC · 1 KiB RAM"},
		{CoreID: "fes.zx80", Label: "Sinclair ZX80", Detail: "PAL · 4 KiB BASIC · 1 KiB RAM"},
		{CoreID: "fes.ts1000", Label: "Timex Sinclair 1000", Detail: "NTSC · 8 KiB BASIC · 2 KiB RAM"},
		{CoreID: "fes.ts1500", Label: "Timex Sinclair 1500", Detail: "NTSC · 8 KiB BASIC · 16 KiB RAM"},
	}
}

// ByID returns the variant for a package core id.
func ByID(coreID string) (Variant, bool) {
	for _, variant := range Variants() {
		if variant.CoreID == coreID {
			return variant, true
		}
	}
	return Variant{}, false
}

// Known reports whether coreID is a ZX81-family package.
func Known(coreID string) bool {
	_, ok := ByID(coreID)
	return ok
}

// Saves reports whether coreID runs the ZX81 ROM BASIC SAVE path.
// The ZX80 compile uses $0207 as its cassette entry, so it has no tape write.
func Saves(coreID string) bool {
	switch coreID {
	case "fes.zx81", "fes.ts1000", "fes.ts1500":
		return true
	default:
		return false
	}
}
