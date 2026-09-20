package misterruntime

import (
	"encoding/json"
	"os"

	"github.com/DeanoC/FogCast/internal/flightdiag"
)

// DefaultEventsPath is the native mister-runtime dump that matches the
// FogCast #206 event envelope. Missing files stay absent rather than
// fabricating host schema.
const DefaultEventsPath = "/run/mister-runtime.events.json"

func WithDiagnosticEventsPath(path string) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.eventsPath = path
	}
}

func (r *Runtime) ConfigureDiagnostics(sink flightdiag.Sink) {
	r.eventsMu.Lock()
	defer r.eventsMu.Unlock()
	r.events = sink
	if r.eventsPath == "" {
		r.eventsPath = DefaultEventsPath
	}
}

func (r *Runtime) record(kind, severity string, detail map[string]any) {
	if r == nil {
		return
	}
	r.eventsMu.Lock()
	sink := r.events
	r.eventsMu.Unlock()
	if sink == nil {
		return
	}
	sink.Append(flightdiag.Event{
		Layer:    flightdiag.LayerRuntime,
		Kind:     kind,
		Severity: severity,
		Detail:   detail,
	})
}

func (r *Runtime) noteDispatch(operation string, ok bool) {
	severity := "ok"
	if !ok {
		severity = "error"
	}
	r.record(flightdiag.KindFIFODispatch, severity, map[string]any{
		"operation": operation,
		"ok":        ok,
	})
	r.drainEvents()
}

func (r *Runtime) drainEvents() {
	if r == nil {
		return
	}
	r.eventsMu.Lock()
	sink := r.events
	path := r.eventsPath
	imported := r.imported
	r.eventsMu.Unlock()
	if sink == nil || path == "" {
		return
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var dump struct {
		Events []flightdiag.Event `json:"events"`
	}
	if json.Unmarshal(payload, &dump) != nil {
		return
	}
	if len(dump.Events) < imported {
		imported = 0
	}
	for _, event := range dump.Events[imported:] {
		sink.Append(event)
	}
	r.eventsMu.Lock()
	r.imported = len(dump.Events)
	r.eventsMu.Unlock()
}
