package inputmap

import (
	"sort"

	"github.com/DeanoC/FogCast/remoteinput"
)

// DeviceInfo is one live pad contributing to a merged stream.
type DeviceInfo struct {
	ID   string
	Name string
}

// Source is one pad's poll result. Err marks the pad as gone; its events are
// discarded and it is omitted from Live.
type Source struct {
	ID     string
	Name   string
	Events []remoteinput.Event
	Err    error
}

// Sourced is a remapped event tagged with the pad that produced it.
type Sourced struct {
	DeviceID   string
	DeviceName string
	Event      remoteinput.Event
}

// Mux merges polls from multiple pads in stable device-id order and applies
// one remapper. Hub is the kit caller; linuxinput applies the remapper per record.
type Mux struct {
	remap *Remapper
}

// NewMux returns a mux that applies remap. A nil remapper is identity.
func NewMux(remap *Remapper) *Mux {
	if remap == nil {
		remap = IdentityRemapper()
	}
	return &Mux{remap: remap}
}

// Merge concatenates live sources sorted by ID, then Name. Each source keeps
// its own poll order. Failed sources are omitted from live.
func (m *Mux) Merge(sources []Source) (out []Sourced, live []Source) {
	sorted := append([]Source(nil), sources...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].ID != sorted[j].ID {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Name < sorted[j].Name
	})
	for _, src := range sorted {
		if src.Err != nil {
			continue
		}
		live = append(live, src)
		for _, e := range src.Events {
			if m != nil && m.remap != nil {
				e = m.remap.Apply(e)
			}
			out = append(out, Sourced{DeviceID: src.ID, DeviceName: src.Name, Event: e})
		}
	}
	return out, live
}

// Events strips device metadata for callers that consume remoteinput.Event.
func Events(sourced []Sourced) []remoteinput.Event {
	if len(sourced) == 0 {
		return nil
	}
	out := make([]remoteinput.Event, len(sourced))
	for i, s := range sourced {
		out[i] = s.Event
	}
	return out
}
