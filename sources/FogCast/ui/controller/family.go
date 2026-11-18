package controller

import "strings"

// Family is whose button markings a pad carries, which picks the glyphs
// its prompts are drawn with.
type Family int

const (
	// FamilyGeneric is a pad whose markings are unknown.
	FamilyGeneric Family = iota
	FamilyXbox
	FamilyPlayStation
	FamilyNintendo
)

func (f Family) String() string {
	switch f {
	case FamilyXbox:
		return "xbox"
	case FamilyPlayStation:
		return "playstation"
	case FamilyNintendo:
		return "nintendo"
	default:
		return "generic"
	}
}

// Family reads the pad's markings from its USB vendor, then from its evdev
// name and its GameControllerDB name. A pad that presents itself as another
// maker's, as many do in XInput mode, is taken at its word.
func (d Device) Family() Family {
	switch d.Vendor {
	case 0x045e:
		return FamilyXbox
	case 0x054c:
		return FamilyPlayStation
	case 0x057e:
		return FamilyNintendo
	}
	name := strings.ToLower(d.Name)
	if e, ok := lookupSDL(d); ok {
		name += " " + strings.ToLower(e.name)
	}
	for _, m := range []struct {
		family Family
		words  []string
	}{
		{FamilyXbox, []string{"xbox", "x-box", "xinput", "x-input"}},
		{FamilyPlayStation, []string{"playstation", "dualshock", "dualsense", "ps3", "ps4", "ps5"}},
		{FamilyNintendo, []string{"nintendo", "switch", "joy-con"}},
	} {
		for _, w := range m.words {
			if strings.Contains(name, w) {
				return m.family
			}
		}
	}
	return FamilyGeneric
}
