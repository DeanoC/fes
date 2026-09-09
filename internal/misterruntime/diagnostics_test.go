package misterruntime_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type recordingSink struct {
	events []flightdiag.Event
}

func (s *recordingSink) Append(event flightdiag.Event) {
	s.events = append(s.events, event)
}

func TestNativeLaunchDispatchesAndDrainsRuntimeDumpJoinFields(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "events.json")
	const flightID = "de305d54-75b4-431b-adb2-eb6b9e546014"
	payload, err := json.Marshal(map[string]any{
		"events": []map[string]any{{
			"ts_utc":    "2026-09-09T15:00:00.000000000Z",
			"mono_ms":   7,
			"flight_id": flightID,
			"lease_gen": "lease-7",
			"run_id":    "run-205",
			"layer":     "fpga",
			"kind":      "fpga_manager.state",
			"severity":  "ok",
			"detail":    map[string]any{"mode": "user", "ok": true},
		}},
		"count": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dump, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	control := &recordingControl{
		statuses: []misterruntime.Response{runtimeResponse("idle", "none")},
		launch:   runtimeResponse("running_game", "game"),
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second,
		misterruntime.WithDiagnosticEventsPath(dump))
	sink := &recordingSink{}
	runtime.ConfigureDiagnostics(sink)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, _, apiErr := runtime.Launch(context.Background(), prepared); apiErr != nil {
		t.Fatal(apiErr)
	}

	if len(sink.events) != 2 {
		t.Fatalf("events = %#v", sink.events)
	}
	if sink.events[0].Kind != flightdiag.KindFIFODispatch || sink.events[0].Detail["operation"] != "launch" {
		t.Fatalf("dispatch = %#v", sink.events[0])
	}
	imported := sink.events[1]
	if imported.Kind != flightdiag.KindFPGAManager || imported.FlightID != flightID ||
		imported.LeaseGen != "lease-7" || imported.RunID != "run-205" {
		t.Fatalf("imported = %#v", imported)
	}
}

func TestNativeDrainOmitsJoinFieldsWhenTheDumpHasNone(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "events.json")
	payload, err := json.Marshal(map[string]any{
		"events": []map[string]any{{
			"ts_utc":   "2026-09-09T15:00:00.000000000Z",
			"mono_ms":  1,
			"layer":    "runtime",
			"kind":     "fifo.consume",
			"severity": "ok",
			"detail":   map[string]any{"operation": "load_core", "ok": true},
		}},
		"count": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dump, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	control := &recordingControl{
		statuses: []misterruntime.Response{runtimeResponse("idle", "none")},
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second,
		misterruntime.WithDiagnosticEventsPath(dump))
	sink := &recordingSink{}
	runtime.ConfigureDiagnostics(sink)
	_ = runtime.Health("test")
	if len(sink.events) != 1 || sink.events[0].Kind != flightdiag.KindFIFOConsume {
		t.Fatalf("events = %#v", sink.events)
	}
	if sink.events[0].FlightID != "" || sink.events[0].LeaseGen != "" || sink.events[0].RunID != "" {
		t.Fatalf("invented join fields: %#v", sink.events[0])
	}
}
