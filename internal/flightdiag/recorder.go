// Package flightdiag records target-side diagnostic events and durable
// pre-reboot evidence without creating another control plane.
package flightdiag

import (
	"sync"
	"time"
)

const (
	DefaultRingCapacity = 10_000
	DiagnosticEvidence  = "diagnostic"

	LayerTarget  = "target"
	LayerRuntime = "runtime"
	LayerFPGA    = "fpga"
	LayerBot     = "bot"

	KindLeaseClaim     = "lease.claim"
	KindLeaseRenew     = "lease.renew"
	KindLeaseRelease   = "lease.release"
	KindLeaseTakeover  = "lease.takeover"
	KindFIFODispatch   = "fifo.dispatch"
	KindFIFOConsume    = "fifo.consume"
	KindCapFDOpen      = "cap.fd.open"
	KindFPGAManager    = "fpga_manager.state"
	KindCoreNameChange = "corename.change"
	KindMainStart      = "main.start"
	KindMainExit       = "main.exit"
	KindMainAppRestart = "main.app_restart"
	KindFenceOwnership = "fence.ownership"
	KindFenceHandoff   = "fence.handoff"
	KindFenceProgram   = "fence.program"
	KindFenceABI       = "fence.abi"
	KindFenceRecovery  = "fence.recovery"
)

// Event is the stable target-side wire shape consumed by fog-flight. LeaseGen
// is intentionally a string: the target's existing lease generation is an
// opaque value and must not be reinterpreted by the debug plane.
type Event struct {
	TSUTC    string         `json:"ts_utc"`
	MonoMS   int64          `json:"mono_ms"`
	FlightID string         `json:"flight_id,omitempty"`
	LeaseGen string         `json:"lease_gen,omitempty"`
	RunID    string         `json:"run_id,omitempty"`
	Layer    string         `json:"layer"`
	Kind     string         `json:"kind"`
	Severity string         `json:"severity"`
	Detail   map[string]any `json:"detail"`
}

// Sink is deliberately small so existing target/runtime call sites can emit
// events without depending on the HTTP API or the evidence vault.
type Sink interface {
	Append(Event)
}

// Ring is a bounded, concurrency-safe in-memory event ring.
type Ring struct {
	mu       sync.Mutex
	capacity int
	events   []Event
	started  time.Time
}

func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = DefaultRingCapacity
	}
	return &Ring{capacity: capacity, started: time.Now()}
}

func (r *Ring) Append(event Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.TSUTC == "" {
		event.TSUTC = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if event.MonoMS == 0 {
		event.MonoMS = time.Since(r.started).Milliseconds()
	}
	if event.Detail == nil {
		event.Detail = map[string]any{}
	}
	event.Detail = cloneDetail(event.Detail)
	r.events = append(r.events, event)
	if len(r.events) > r.capacity {
		keep := r.events[len(r.events)-r.capacity:]
		r.events = append([]Event(nil), keep...)
	}
}

// Add appends an event using the recorder's clocks. It is useful at call sites
// that do not need to override the optional join fields.
func (r *Ring) Add(layer, kind, severity string, detail map[string]any) {
	r.Append(Event{Layer: layer, Kind: kind, Severity: severity, Detail: detail})
}

// Snapshot returns the newest limit events in chronological order. A zero or
// negative limit means the complete retained ring.
func (r *Ring) Snapshot(limit int) []Event {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	start := 0
	if limit > 0 && limit < len(r.events) {
		start = len(r.events) - limit
	}
	result := make([]Event, len(r.events)-start)
	for index, event := range r.events[start:] {
		result[index] = event
		result[index].Detail = cloneDetail(event.Detail)
	}
	return result
}

func (r *Ring) Capacity() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.capacity
}

type Recorder struct {
	Ring  *Ring
	Vault *Vault
}

func NewRecorder(root string) *Recorder {
	ring := NewRing(DefaultRingCapacity)
	return &Recorder{Ring: ring, Vault: NewVault(root, ring)}
}

func (r *Recorder) Append(event Event) {
	if r != nil && r.Ring != nil {
		r.Ring.Append(event)
	}
}

func (r *Recorder) Events(limit int) []Event {
	if r == nil || r.Ring == nil {
		return nil
	}
	return r.Ring.Snapshot(limit)
}

func (r *Recorder) SnapshotBeforeReboot(request SnapshotRequest) (SnapshotResult, error) {
	if r == nil || r.Vault == nil {
		return SnapshotResult{}, &unavailableError{}
	}
	return r.Vault.SnapshotBeforeReboot(request)
}

type unavailableError struct{}

func (*unavailableError) Error() string { return "diagnostic recorder is unavailable" }

func cloneDetail(detail map[string]any) map[string]any {
	if detail == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(detail))
	for key, value := range detail {
		result[key] = value
	}
	return result
}
