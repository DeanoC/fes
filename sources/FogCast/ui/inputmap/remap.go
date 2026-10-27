package inputmap

import (
	"fmt"

	"github.com/DeanoC/FogCast/remoteinput"
)

// Remapper applies a Profile to a pad's normalized events.
type Remapper struct {
	logical map[remoteinput.Code]remoteinput.Code
}

// NewRemapper compiles a profile. An empty profile passes every code through.
func NewRemapper(p Profile) (*Remapper, error) {
	r := &Remapper{logical: map[remoteinput.Code]remoteinput.Code{}}
	switch p.Preset {
	case "":
	case PresetSwapAB:
		r.logical[remoteinput.ButtonA] = remoteinput.ButtonB
		r.logical[remoteinput.ButtonB] = remoteinput.ButtonA
	default:
		return nil, fmt.Errorf("inputmap: unknown preset %q", p.Preset)
	}
	for from, to := range p.Bindings {
		src, err := parseLogical(from)
		if err != nil {
			return nil, err
		}
		dst, err := parseLogical(to)
		if err != nil {
			return nil, err
		}
		r.logical[src] = dst
	}
	return r, nil
}

// Apply remaps a normalized event. Unbound codes pass through.
func (r *Remapper) Apply(e remoteinput.Event) remoteinput.Event {
	if r == nil {
		return e
	}
	if dst, ok := r.logical[e.Code]; ok {
		e.Code = dst
	}
	return e
}
