package inputmap

import "github.com/DeanoC/FogCast/remoteinput"

// Remapper applies a Profile to normalized events. Physical evdev/js bindings
// override the device mapper's logical code; logical bindings then remap once.
type Remapper struct {
	profile Profile
	logical map[remoteinput.Code]remoteinput.Code
	evdev   map[uint16]remoteinput.Code
	js      map[uint16]remoteinput.Code
}

// NewRemapper compiles a profile. Identity and empty bindings are a no-op.
func NewRemapper(p Profile) (*Remapper, error) {
	if p.Bindings == nil {
		p.Bindings = map[string]string{}
	}
	if err := validateBindings(p.Bindings); err != nil {
		return nil, err
	}
	if stringsEmpty(p.Name) {
		p.Name = NameIdentity
	}
	r := &Remapper{
		profile: p,
		logical: map[remoteinput.Code]remoteinput.Code{},
		evdev:   map[uint16]remoteinput.Code{},
		js:      map[uint16]remoteinput.Code{},
	}
	for from, to := range p.Bindings {
		dst, err := parseLogical(to)
		if err != nil {
			return nil, err
		}
		if isPhysical(from) {
			kind, code, err := parsePhysical(from)
			if err != nil {
				return nil, err
			}
			if kind == "js" {
				r.js[code] = dst
			} else {
				r.evdev[code] = dst
			}
			continue
		}
		src, err := parseLogical(from)
		if err != nil {
			return nil, err
		}
		r.logical[src] = dst
	}
	return r, nil
}

func stringsEmpty(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

// Profile returns the compiled profile.
func (r *Remapper) Profile() Profile {
	if r == nil {
		return Identity()
	}
	return r.profile
}

// Apply remaps a normalized logical event. Unmapped codes pass through.
func (r *Remapper) Apply(e remoteinput.Event) remoteinput.Event {
	if r == nil || len(r.logical) == 0 {
		return e
	}
	if dst, ok := r.logical[e.Code]; ok {
		e.Code = dst
	}
	return e
}

// PhysicalButton reports a profile override for a raw evdev key code.
func (r *Remapper) PhysicalButton(code uint16) (remoteinput.Code, bool) {
	if r == nil {
		return 0, false
	}
	dst, ok := r.evdev[code]
	return dst, ok
}

// PhysicalJS reports a profile override for a raw joystick button number.
func (r *Remapper) PhysicalJS(number uint8) (remoteinput.Code, bool) {
	if r == nil {
		return 0, false
	}
	dst, ok := r.js[uint16(number)]
	return dst, ok
}

// IdentityRemapper is the compiled pass-through profile.
func IdentityRemapper() *Remapper {
	return &Remapper{
		profile: Identity(),
		logical: map[remoteinput.Code]remoteinput.Code{},
		evdev:   map[uint16]remoteinput.Code{},
		js:      map[uint16]remoteinput.Code{},
	}
}
