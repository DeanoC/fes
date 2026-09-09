package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
)

const testFlightID = "de305d54-75b4-431b-adb2-eb6b9e546014"

func TestDiagnosticDumpAndSnapshotUseExistingAuthenticatedLease(t *testing.T) {
	recorder := flightdiag.NewRecorder(t.TempDir())
	recorder.Append(flightdiag.Event{
		FlightID: testFlightID,
		LeaseGen: "placeholder",
		RunID:    "run-205",
		Layer:    flightdiag.LayerTarget,
		Kind:     flightdiag.KindFenceRecovery,
		Severity: "warn",
	})
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	recorder.Append(flightdiag.Event{
		FlightID: testFlightID,
		LeaseGen: grant.Status.Generation,
		RunID:    "run-205",
		Layer:    flightdiag.LayerTarget,
		Kind:     flightdiag.KindLeaseClaim,
		Severity: "ok",
	})
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil,
		httpapi.WithKitLease(manager), httpapi.WithDiagnostics(recorder))

	dump := httptest.NewRequest(http.MethodGet, "/v1/kit/debug/events?limit=10", nil)
	dump.Header.Set("Authorization", "Bearer bearer")
	dumpResponse := httptest.NewRecorder()
	handler.ServeHTTP(dumpResponse, dump)
	if dumpResponse.Code != http.StatusOK {
		t.Fatalf("dump status = %d: %s", dumpResponse.Code, dumpResponse.Body.String())
	}
	var dumped struct {
		Events []flightdiag.Event `json:"events"`
	}
	if err := json.Unmarshal(dumpResponse.Body.Bytes(), &dumped); err != nil {
		t.Fatal(err)
	}
	if len(dumped.Events) == 0 {
		t.Fatal("diagnostic dump was empty")
	}

	body, err := json.Marshal(flightdiag.SnapshotRequest{
		RunID:    "run-205",
		LeaseGen: grant.Status.Generation,
		FlightID: testFlightID,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := httptest.NewRequest(http.MethodPost, "/v1/kit/debug/snapshot-before-reboot", bytes.NewReader(body))
	snapshot.Header.Set("Authorization", "Bearer bearer")
	snapshot.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	snapshot.Header.Set("Content-Type", "application/json")
	snapshotResponse := httptest.NewRecorder()
	handler.ServeHTTP(snapshotResponse, snapshot)
	if snapshotResponse.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d: %s", snapshotResponse.Code, snapshotResponse.Body.String())
	}
	var result flightdiag.SnapshotResult
	if err := json.Unmarshal(snapshotResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.EvidenceClass != flightdiag.DiagnosticEvidence || result.RunID != "run-205" || result.FlightID != testFlightID {
		t.Fatalf("snapshot result = %+v", result)
	}
	if _, err := os.Stat(result.Manifest); err != nil {
		t.Fatalf("manifest %q: %v", result.Manifest, err)
	}
}
