package tenfoot

// InputKind is a sofa input class. Hints and focus ownership follow the
// last-used kind; a newly plugged keyboard, mouse, or gamepad claims affinity.
type InputKind int

const (
	InputNone InputKind = iota
	InputKeyboard
	InputMouse
	InputGamepad
)

func (k InputKind) String() string {
	switch k {
	case InputKeyboard:
		return "keyboard"
	case InputMouse:
		return "mouse"
	case InputGamepad:
		return "gamepad"
	default:
		return "none"
	}
}

// Affinity is the device that currently owns sofa hints and focus.
type Affinity struct {
	Kind InputKind
	ID   int
}

type affinityTracker struct {
	present map[InputKind]map[int]struct{}
	current Affinity
	recents []Affinity
}

func (t *affinityTracker) set(kind InputKind) map[int]struct{} {
	if t.present == nil {
		t.present = map[InputKind]map[int]struct{}{}
	}
	set := t.present[kind]
	if set == nil {
		set = map[int]struct{}{}
		t.present[kind] = set
	}
	return set
}

func (t *affinityTracker) has(kind InputKind, id int) bool {
	_, ok := t.present[kind][id]
	return ok
}

func (t *affinityTracker) count(kind InputKind) int {
	return len(t.present[kind])
}

func (t *affinityTracker) anyID(kind InputKind) (int, bool) {
	set := t.present[kind]
	if len(set) == 0 {
		return 0, false
	}
	first := true
	best := 0
	for id := range set {
		if first || id < best {
			best = id
			first = false
		}
	}
	return best, true
}

func (t *affinityTracker) Seed(kind InputKind, id int) {
	if kind == InputNone {
		return
	}
	t.set(kind)[id] = struct{}{}
}

func (t *affinityTracker) Attach(kind InputKind, id int) {
	if kind == InputNone {
		return
	}
	set := t.set(kind)
	_, existed := set[id]
	set[id] = struct{}{}
	if !existed {
		t.claim(kind, id)
	}
}

func (t *affinityTracker) Detach(kind InputKind, id int) {
	if kind == InputNone {
		return
	}
	delete(t.set(kind), id)
	t.dropRecent(kind, id)
	if t.current.Kind == kind && t.current.ID == id {
		t.restore()
	}
}

func (t *affinityTracker) Note(kind InputKind, id int) {
	if kind == InputNone {
		return
	}
	t.set(kind)[id] = struct{}{}
	t.claim(kind, id)
}

func (t *affinityTracker) claim(kind InputKind, id int) {
	a := Affinity{Kind: kind, ID: id}
	t.current = a
	t.pushRecent(a)
}

func (t *affinityTracker) pushRecent(a Affinity) {
	next := make([]Affinity, 0, len(t.recents)+1)
	next = append(next, a)
	for _, old := range t.recents {
		if old.Kind == a.Kind && old.ID == a.ID {
			continue
		}
		next = append(next, old)
	}
	t.recents = next
}

func (t *affinityTracker) dropRecent(kind InputKind, id int) {
	next := make([]Affinity, 0, len(t.recents))
	for _, old := range t.recents {
		if old.Kind == kind && old.ID == id {
			continue
		}
		next = append(next, old)
	}
	t.recents = next
}

func (t *affinityTracker) restore() {
	for _, a := range t.recents {
		if t.has(a.Kind, a.ID) {
			t.current = a
			return
		}
	}
	for _, kind := range []InputKind{InputKeyboard, InputMouse, InputGamepad} {
		if id, ok := t.anyID(kind); ok {
			t.claim(kind, id)
			return
		}
	}
	t.current = Affinity{}
}

func (t *affinityTracker) preferStartup() {
	if t.current.Kind != InputNone {
		return
	}
	if id, ok := t.anyID(InputGamepad); ok {
		t.claim(InputGamepad, id)
		return
	}
	if id, ok := t.anyID(InputKeyboard); ok {
		t.claim(InputKeyboard, id)
		return
	}
	if id, ok := t.anyID(InputMouse); ok {
		t.claim(InputMouse, id)
	}
}
