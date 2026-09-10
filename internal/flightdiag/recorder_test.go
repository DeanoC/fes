package flightdiag

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const testFlightID = "de305d54-75b4-431b-adb2-eb6b9e546014"

func TestRingAppendAndSnapshotBeforeRebootKeepJoinFields(t *testing.T) {
	root := t.TempDir()
	coreName := filepath.Join(root, "CORENAME")
	fatNote := filepath.Join(root, "fat-note")
	if err := os.WriteFile(coreName, []byte("MEGADRIVE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fatNote, []byte("diagnostic note\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ring := NewRing(3)
	ring.Append(Event{
		TSUTC:    "2026-09-09T15:00:00Z",
		MonoMS:   42,
		FlightID: testFlightID,
		LeaseGen: "lease-7",
		RunID:    "run-205",
		Layer:    LayerTarget,
		Kind:     KindLeaseClaim,
		Severity: "ok",
		Detail:   map[string]any{"owner": "caster"},
	})
	ring.Append(Event{Layer: LayerRuntime, Kind: KindFIFODispatch, Severity: "ok"})
	ring.Append(Event{Layer: LayerFPGA, Kind: KindFPGAManager, Severity: "warn"})

	vault := NewVault(filepath.Join(root, "vault"), ring)
	vault.KeyPaths = []KeyPath{
		{Name: "CORENAME", Path: coreName},
		{Name: "FAT note", Path: fatNote},
	}
	result, err := vault.SnapshotBeforeReboot(SnapshotRequest{
		RunID:    "run-205",
		LeaseGen: "lease-7",
		FlightID: testFlightID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.EvidenceClass != DiagnosticEvidence || result.EventCount != 3 || result.PathCount != 2 {
		t.Fatalf("snapshot result = %+v", result)
	}
	manifestBytes, err := os.ReadFile(result.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var manifest SnapshotManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.RunID != "run-205" || manifest.LeaseGen != "lease-7" || manifest.FlightID != testFlightID {
		t.Fatalf("join fields = %+v", manifest)
	}
	if len(manifest.Events) != 3 || manifest.Events[0].FlightID != testFlightID || manifest.Events[0].LeaseGen != "lease-7" {
		t.Fatalf("events = %+v", manifest.Events)
	}
	for _, path := range manifest.Paths {
		if path.State != "copied" || path.Artifact == "" {
			t.Fatalf("path evidence = %+v", path)
		}
		if content, err := os.ReadFile(filepath.Join(filepath.Dir(result.Manifest), path.Artifact)); err != nil || len(content) == 0 {
			t.Fatalf("artifact %q: %v", path.Artifact, err)
		}
	}
}

func TestSnapshotRejectsNonHostFlightID(t *testing.T) {
	vault := NewVault(t.TempDir(), NewRing(1))
	_, err := vault.SnapshotBeforeReboot(SnapshotRequest{
		RunID: "run-205", LeaseGen: "lease-7", FlightID: "flight-205",
	})
	if err == nil {
		t.Fatal("non-host flight_id was accepted")
	}
}
