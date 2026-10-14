package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
	contractlease "github.com/DeanoC/FogCast/kitlease"
)

func TestProductionMaintenanceConfiguration(t *testing.T) {
	dependencies, err := productionRunDependencies()
	if err != nil {
		t.Fatal(err)
	}
	if dependencies.maintenanceFile != maintenanceMarkerFile || dependencies.maintenanceLock != maintenanceLockFile || dependencies.maintenanceBootIDFile != bootIDFile || maintenanceContract != "fes-kit-maintenance-v1" {
		t.Fatal("production maintenance admission is not configured")
	}
}
func TestMaintenanceBootIdentityReadBeforeAdmission(t *testing.T) {
	root := t.TempDir()
	boot := filepath.Join(root, "boot_id")
	dependencies := runDependencies{maintenanceFile: filepath.Join(root, "kit-maintenance.json"), maintenanceLock: filepath.Join(root, "kit-maintenance.lock"), maintenanceBootIDFile: boot}
	if _, err := maintenanceLeaseOptions(dependencies); err == nil {
		t.Fatal("missing boot identity accepted")
	}
	if err := os.WriteFile(boot, []byte("12345678-1234-1234-1234-123456789abc\n"), 0600); err != nil {
		t.Fatal(err)
	}
	options, err := maintenanceLeaseOptions(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	m := kitlease.New(time.Minute, func(context.Context) error { return nil }, options...)
	defer m.Close()
	deadline := time.Now().Add(time.Second)
	for m.Status().State != "free" {
		if time.Now().After(deadline) {
			t.Fatal(m.Status())
		}
		time.Sleep(time.Millisecond)
	}
	g, err := m.Claim(kitlease.ClaimRequest{RequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Owner: "test-host", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := m.StartMaintenance(g.Token, contractlease.MaintenanceStartRequest{RequestID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Purpose: "test maintenance"})
	if err != nil || reservation.Status.BootID != "12345678-1234-1234-1234-123456789abc" {
		t.Fatal(reservation.Status, err)
	}
}
