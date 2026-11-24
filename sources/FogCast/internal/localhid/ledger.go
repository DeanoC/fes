// Package localhid models physical-key ownership for a single admitted local
// keyboard source. It performs no I/O; callers serialize it with source
// admission, lifecycle transitions and confirmed sink writes.
package localhid

import (
	"errors"
	"sort"

	"github.com/DeanoC/FogCast/internal/hidkeys"
)

const MaxDevices = 16

type DeviceID [16]byte

type DeviceSnapshot struct {
	ID   DeviceID
	Held []uint8
}

type edgeState uint8

const (
	up edgeState = iota
	delivered
	suppressed
)

// Delta is a change to the union of delivered contributors. A suppressed
// physical hold never contributes a guest press.
type Delta struct {
	Usage uint8
	Down  bool
}

// Ledger belongs to exactly one validated source token and must be used by
// its serialized reader only. It must never be shared between source tokens.
type Ledger struct {
	devices             map[DeviceID]map[uint8]edgeState
	sequence            uint64
	fenced              bool
	quarantined         bool
	pending             bool
	pendingID           uint64
	lastID              uint64
	quarantineRequested bool
}

var ErrInvalid = errors.New("invalid local physical state")
var ErrQuarantined = errors.New("local physical source is quarantined")
var ErrPending = errors.New("local physical sink result is pending")

func validUsage(usage uint8) bool  { _, ok := hidkeys.Code(usage); return ok }
func validDevice(id DeviceID) bool { return id != (DeviceID{}) }

// New installs a complete authoritative snapshot. Keys already held at the
// boundary are suppressed until physical release and are never posted.
func New(sequence uint64, snapshot []DeviceSnapshot) (*Ledger, error) {
	if len(snapshot) == 0 || len(snapshot) > MaxDevices {
		return nil, ErrInvalid
	}
	l := &Ledger{devices: make(map[DeviceID]map[uint8]edgeState), sequence: sequence}
	for _, d := range snapshot {
		if !validDevice(d.ID) || l.devices[d.ID] != nil || len(d.Held) > 256 {
			return nil, ErrInvalid
		}
		state := make(map[uint8]edgeState)
		for _, usage := range d.Held {
			if !validUsage(usage) || state[usage] != up {
				return nil, ErrInvalid
			}
			state[usage] = suppressed
		}
		l.devices[d.ID] = state
	}
	return l, nil
}

func (l *Ledger) accept(sequence uint64) error {
	if l == nil || l.quarantined {
		return ErrQuarantined
	}
	if l.pending {
		return ErrPending
	}
	if l.sequence == ^uint64(0) || sequence != l.sequence+1 {
		l.quarantined = true
		return ErrInvalid
	}
	return nil
}
func (l *Ledger) delivered(usage uint8) bool {
	for _, states := range l.devices {
		if states[usage] == delivered {
			return true
		}
	}
	return false
}

func (l *Ledger) startPending() error {
	if l.lastID == ^uint64(0) {
		l.quarantined = true
		return ErrInvalid
	}
	l.lastID++
	l.pendingID = l.lastID
	l.pending = true
	return nil
}

// Pending identifies the single sink operation awaiting confirmation. The
// identifier never repeats within a source, including across focus fences.
func (l *Ledger) Pending() (uint64, bool) {
	if l == nil || !l.pending || l.quarantined {
		return 0, false
	}
	return l.pendingID, true
}

// Edge accounts once for a physical edge. A source must be quarantined if the
// returned sink delta has an ambiguous outcome; callers must not replay it.
func (l *Ledger) Edge(sequence uint64, id DeviceID, usage uint8, down bool) ([]Delta, error) {
	if err := l.accept(sequence); err != nil {
		return nil, err
	}
	state, ok := l.devices[id]
	if !ok || !validUsage(usage) {
		l.quarantined = true
		return nil, ErrInvalid
	}
	before := l.delivered(usage)
	if down {
		if state[usage] == up {
			if l.fenced {
				state[usage] = suppressed
			} else {
				state[usage] = delivered
			}
		}
	} else {
		delete(state, usage)
	}
	l.sequence = sequence
	after := l.delivered(usage)
	if before != after {
		if err := l.startPending(); err != nil {
			return nil, err
		}
		return []Delta{{Usage: usage, Down: after}}, nil
	}
	return nil, nil
}

// Fence suppresses every currently held physical contributor. Deltas are
// neutralization only; no input press is replayed across a transition.
func (l *Ledger) Fence() ([]Delta, error) {
	if l == nil || l.quarantined {
		return nil, ErrQuarantined
	}
	if l.pending {
		return nil, ErrPending
	}
	out := l.suppress()
	if len(out) != 0 {
		if err := l.startPending(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (l *Ledger) suppress() []Delta {
	keys := make(map[uint8]bool)
	for _, state := range l.devices {
		for usage, value := range state {
			if value == delivered {
				keys[usage] = true
				state[usage] = suppressed
			}
		}
	}
	l.fenced = true
	sorted := make([]int, 0, len(keys))
	for usage := range keys {
		sorted = append(sorted, int(usage))
	}
	sort.Ints(sorted)
	out := make([]Delta, 0, len(sorted))
	for _, usage := range sorted {
		out = append(out, Delta{Usage: uint8(usage)})
	}
	return out
}

// Resume permits only fresh presses. Holds present before/during Fence remain
// suppressed until their own physical release.
func (l *Ledger) Resume() error {
	if l == nil || l.quarantined {
		return ErrQuarantined
	}
	if l.pending {
		return ErrPending
	}
	l.fenced = false
	return nil
}

// Quarantine refuses further edges. If a sink call is still in flight, it
// returns ErrPending and requires the owner to drain that call through Confirm
// or Fail before any neutralization can start. Once drained, callers must
// neutralize the entire source even if the bookkeeping deltas are empty: an
// ambiguous release may have failed. Never replay the original operation.
func (l *Ledger) Quarantine() ([]Delta, error) {
	if l == nil {
		return nil, ErrQuarantined
	}
	if l.pending {
		l.quarantineRequested = true
		return nil, ErrPending
	}
	out := l.suppress()
	l.quarantined = true
	return out, nil
}

// Confirm commits the immediately preceding sink delta only after the caller
// has confirmed its outcome. A failure or uncertain outcome calls Quarantine
// and neutralizes the complete source instead of replaying the input.
func (l *Ledger) Confirm(id uint64) error {
	if l == nil || l.quarantined {
		return ErrQuarantined
	}
	if !l.pending || id != l.pendingID {
		return ErrInvalid
	}
	l.pending = false
	if l.quarantineRequested {
		l.suppress()
		l.quarantined = true
		return ErrQuarantined // The completed operation must now be neutralized.
	}
	return nil
}

// Fail is called only after the identified sink call has returned a failed
// or ambiguous outcome. It requires full-source neutralization; it never
// retries the uncertain original input.
func (l *Ledger) Fail(id uint64) error {
	if l == nil || l.quarantined {
		return ErrQuarantined
	}
	if !l.pending || id != l.pendingID {
		return ErrInvalid
	}
	l.pending = false
	l.suppress()
	l.quarantined = true
	return nil
}

// Remove retires one physical device without releasing other contributors.
func (l *Ledger) Remove(sequence uint64, id DeviceID) ([]Delta, error) {
	if err := l.accept(sequence); err != nil {
		return nil, err
	}
	state, ok := l.devices[id]
	if !ok {
		l.quarantined = true
		return nil, ErrInvalid
	}
	before := make(map[uint8]bool)
	for usage, value := range state {
		if value == delivered {
			before[usage] = true
		}
	}
	delete(l.devices, id)
	l.sequence = sequence
	keys := make([]int, 0, len(before))
	for usage := range before {
		if !l.delivered(usage) {
			keys = append(keys, int(usage))
		}
	}
	sort.Ints(keys)
	out := make([]Delta, 0, len(keys))
	for _, usage := range keys {
		out = append(out, Delta{Usage: uint8(usage)})
	}
	if len(out) != 0 {
		if err := l.startPending(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Sequence returns the last accounted physical-edge sequence for a barrier.
func (l *Ledger) Sequence() uint64 {
	if l == nil {
		return 0
	}
	return l.sequence
}

// Ready reports whether a barrier can be acknowledged without a pending or
// quarantined sink result. It is not an authority or hardware-readiness test.
func (l *Ledger) Ready() bool { return l != nil && !l.pending && !l.quarantined && !l.fenced }
