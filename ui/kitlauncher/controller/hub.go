package controller

import (
	"errors"
	"sort"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

// padSource is one physical pad. Tests inject fakes; Linux uses Device.
type padSource interface {
	Poll() ([]remoteinput.Event, error)
	Close() error
	Info() (id, name string)
}

// Hub multiplexes eligible USB gamepads into one Pad stream and applies remap.
// Hotplug rescan runs on an interval from Poll so the kit 16ms loop stays
// the caller; discovery itself is a bounded glob-and-open.
type padHold struct {
	pressed map[remoteinput.Code]bool
	axes    map[remoteinput.Code]int32
}

type Hub struct {
	pads      []padSource
	mux       *inputmap.Mux
	remap     *inputmap.Remapper
	holds     map[string]*padHold
	ports     map[string]uint8
	clock     func() time.Time
	lastScan  time.Time
	scanEvery time.Duration
	rescan    func(*Hub)
}

// NewHub merges pads with remap. A nil remapper is identity.
func NewHub(remap *inputmap.Remapper, pads []padSource) *Hub {
	if remap == nil {
		remap = inputmap.IdentityRemapper()
	}
	h := &Hub{
		pads:      append([]padSource(nil), pads...),
		mux:       inputmap.NewMux(remap),
		remap:     remap,
		holds:     map[string]*padHold{},
		ports:     map[string]uint8{},
		clock:     time.Now,
		scanEvery: time.Second,
	}
	h.lastScan = h.clock()
	return h
}

// Poll merges live pads in stable id order. A failed pad is closed and
// dropped; an empty hub returns an error so the kit loop can reopen.
func (h *Hub) Poll() ([]remoteinput.Event, error) {
	if h == nil {
		return nil, errors.New("controller closed")
	}
	h.maybeRescan()
	var sources []inputmap.Source
	var firstErr error
	for _, p := range h.pads {
		id, name := p.Info()
		events, err := p.Poll()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		sources = append(sources, inputmap.Source{ID: id, Name: name, Events: events, Err: err})
	}
	sourced, live := h.mux.Merge(sources)
	liveIDs := make(map[string]struct{}, len(live))
	for _, src := range live {
		liveIDs[src.ID] = struct{}{}
	}
	var next []padSource
	var released []remoteinput.Event
	for _, p := range h.pads {
		id, _ := p.Info()
		if _, ok := liveIDs[id]; ok {
			next = append(next, p)
			continue
		}
		released = append(released, h.release(id)...)
		_ = p.Close()
		delete(h.ports, id)
	}
	h.pads = next
	// Assign only free slots, in stable device order. Surviving pads never move.
	for _, src := range live {
		if _, ok := h.ports[src.ID]; ok {
			continue
		}
		for port := uint8(0); port < 2; port++ {
			used := false
			for _, assigned := range h.ports {
				if assigned == port {
					used = true
				}
			}
			if !used {
				h.ports[src.ID] = port
				break
			}
		}
	}
	out := released
	for _, source := range sourced {
		port, ok := h.ports[source.DeviceID]
		if !ok {
			continue
		}
		source.Event.Player = port
		h.remember(source.DeviceID, source.Event)
		out = append(out, source.Event)
	}
	if len(h.pads) == 0 {
		// Deliver final releases first; the next poll reports the empty hub.
		if len(out) != 0 {
			return out, nil
		}
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, errors.New("connect a USB gamepad")
	}
	return out, nil
}

// Devices lists pads that last Poll kept live. Before the first Poll it is
// the set Open captured.
func (h *Hub) Devices() []inputmap.DeviceInfo {
	if h == nil {
		return nil
	}
	out := make([]inputmap.DeviceInfo, 0, len(h.pads))
	for _, p := range h.pads {
		id, name := p.Info()
		out = append(out, inputmap.DeviceInfo{ID: id, Name: name})
	}
	return out
}

// Close closes every remaining pad.
func (h *Hub) Close() error {
	if h == nil {
		return nil
	}
	var err error
	for _, p := range h.pads {
		if e := p.Close(); e != nil && err == nil {
			err = e
		}
	}
	h.pads = nil
	h.holds = nil
	h.ports = nil
	return err
}

func (h *Hub) maybeRescan() {
	if h.rescan == nil || h.scanEvery <= 0 {
		return
	}
	now := h.clock()
	if !h.lastScan.IsZero() && now.Sub(h.lastScan) < h.scanEvery {
		return
	}
	h.rescan(h)
	h.lastScan = now
}

func (h *Hub) hasID(id string) bool {
	for _, p := range h.pads {
		got, _ := p.Info()
		if got == id {
			return true
		}
	}
	return false
}

func (h *Hub) remember(id string, e remoteinput.Event) {
	if h.holds == nil {
		h.holds = map[string]*padHold{}
	}
	st := h.holds[id]
	if st == nil {
		st = &padHold{pressed: map[remoteinput.Code]bool{}, axes: map[remoteinput.Code]int32{}}
		h.holds[id] = st
	}
	switch e.Kind {
	case remoteinput.KindButton, remoteinput.KindKey:
		if e.Action == remoteinput.ActionPress {
			st.pressed[e.Code] = true
		} else {
			delete(st.pressed, e.Code)
		}
	case remoteinput.KindAxis:
		st.axes[e.Code] = e.Value
	}
}

func (h *Hub) release(id string) []remoteinput.Event {
	st := h.holds[id]
	if st == nil {
		return nil
	}
	delete(h.holds, id)
	var codes []remoteinput.Code
	for c := range st.pressed {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	out := make([]remoteinput.Event, 0, len(codes)+len(st.axes))
	for _, c := range codes {
		event := releaseEvent(c)
		event.Player = h.ports[id]
		out = append(out, event)
	}
	var axes []remoteinput.Code
	for c, v := range st.axes {
		if v != 0 {
			axes = append(axes, c)
		}
	}
	sort.Slice(axes, func(i, j int) bool { return axes[i] < axes[j] })
	for _, c := range axes {
		out = append(out, remoteinput.Event{Player: h.ports[id], Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: c, Value: 0})
	}
	return out
}

func releaseEvent(c remoteinput.Code) remoteinput.Event {
	return remoteinput.EventForCode(c, remoteinput.ActionRelease)
}

func (h *Hub) add(p padSource) {
	if p == nil {
		return
	}
	id, _ := p.Info()
	if id != "" && h.hasID(id) {
		_ = p.Close()
		return
	}
	if d, ok := p.(*Device); ok && h.remap != nil {
		d.remap = h.remap
	}
	h.pads = append(h.pads, p)
}
