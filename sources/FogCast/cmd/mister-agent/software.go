package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/softwareagent"
)

func startSoftwareAdmission(ctx context.Context, leases *kitlease.Manager, coordinator *agent.Coordinator, updater *applianceupdate.Service) (*softwareagent.Fence, error) {
	id := os.Getenv("FES_SOFTWARE_BUNDLE_ID")
	if id == "" {
		return nil, nil
	}
	var request softwareagent.Request
	pending := os.Getenv("FES_SOFTWARE_REQUEST")
	if pending != "" {
		if err := json.Unmarshal([]byte(pending), &request); err != nil || request.RequestID == "" || len(request.Credential) != 64 {
			return nil, fmt.Errorf("invalid managed software startup admission")
		}
	}
	fence, admission, err := prepareSoftwareAdmission(ctx, leases, coordinator, updater, id, pending, request)
	if err != nil || admission == nil {
		return fence, err
	}
	go func() {
		if err := localcores.Serve(ctx, softwareagent.Socket, admission.Handler()); err != nil && ctx.Err() == nil {
			slog.Error("software admission socket failed", "error", err)
		}
	}()
	return fence, nil
}

// prepareSoftwareAdmission builds the admission fence before any socket is
// served. A recovery restart sets FES_SOFTWARE_RECOVERY_FENCE so execution
// claims are rejected before the admission socket listens.
func prepareSoftwareAdmission(ctx context.Context, leases *kitlease.Manager, coordinator *agent.Coordinator, updater *applianceupdate.Service, id, pending string, request softwareagent.Request) (*softwareagent.Fence, *softwareagent.Admission, error) {
	fence := softwareagent.NewFence(pending != "")
	admission := &softwareagent.Admission{Leases: leases, Fence: fence, BundleID: id, Ready: coordinator.RawIdleReady, UpdateBlocked: func(ctx context.Context) bool {
		if updater == nil {
			return false
		}
		status, err := updater.Status(ctx)
		return err != nil || status.Trial || status.Pending != "" || status.Corrupt
	}}
	if os.Getenv("FES_SOFTWARE_RECOVERY_FENCE") == "1" {
		if err := leases.SetExecutionFence(true); err != nil {
			return nil, nil, err
		}
	}
	// The local and HTTP fences are installed before any external launch path.
	// Queue registration waits for the existing startup cleanup to settle.
	if pending != "" {
		admission.QueueStartup(ctx, request)
	}
	return fence, admission, nil
}
