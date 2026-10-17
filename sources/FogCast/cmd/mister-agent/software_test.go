package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/softwareagent"
	contract "github.com/DeanoC/FogCast/kitlease"
)

func waitLeaseFree(t *testing.T, leases *kitlease.Manager) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if leases.Status().State == "free" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("lease manager did not become free: %+v", leases.Status())
}

func TestRecoveryFenceRejectsClaimsBeforeServe(t *testing.T) {
	t.Setenv("FES_SOFTWARE_RECOVERY_FENCE", "1")
	leases := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(leases.Close)
	waitLeaseFree(t, leases)
	if _, _, err := prepareSoftwareAdmission(context.Background(), leases, &agent.Coordinator{}, nil, "bundle", "", softwareagent.Request{}); err != nil {
		t.Fatal(err)
	}
	_, err := leases.Claim(contract.ClaimRequest{RequestID: strings.Repeat("ab", 16), Owner: "player", Purpose: "game"})
	if !errors.Is(err, kitlease.ErrBlocked) {
		t.Fatalf("recovery start admitted a claim: %v", err)
	}
}

func TestOrdinaryStartLeavesExecutionClaimsOpen(t *testing.T) {
	t.Setenv("FES_SOFTWARE_RECOVERY_FENCE", "")
	leases := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(leases.Close)
	waitLeaseFree(t, leases)
	if _, _, err := prepareSoftwareAdmission(context.Background(), leases, &agent.Coordinator{}, nil, "bundle", "", softwareagent.Request{}); err != nil {
		t.Fatal(err)
	}
	if _, err := leases.Claim(contract.ClaimRequest{RequestID: strings.Repeat("cd", 16), Owner: "player", Purpose: "game"}); err != nil {
		t.Fatal(err)
	}
}
