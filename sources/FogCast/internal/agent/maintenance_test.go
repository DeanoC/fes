package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"

	"github.com/DeanoC/FogCast/protocol"
)

type maintenanceRuntime struct {
	fakeRuntime
	idle bool
}

func (r *maintenanceRuntime) ConfirmIdle(context.Context) bool { return r.idle }

func TestUpdateMaintenanceRetainsLaunchExclusionAndRawIdle(t *testing.T) {
	r := &maintenanceRuntime{fakeRuntime: fakeRuntime{health: protocol.Health{Ready: true}}, idle: true}
	c := agent.New(r, time.Second, time.Second)
	c.SetUpdateBlocked(true)
	if c.Health("").Ready || !c.RawIdleReady(context.Background()) {
		t.Fatal("trial must hide readiness without hiding raw idle")
	}
	release, err := c.BeginMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, e := c.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); e == nil || e.Code != protocol.CodeBusy {
		t.Fatalf("launch entered maintenance: %v", e)
	}
	release()
	c.SetUpdateBlocked(false)
	if !c.Health("").Ready {
		t.Fatal("readiness did not recover")
	}
}

func TestUpdateMaintenanceRejectsFailedSaveAndNonNativeIdle(t *testing.T) {
	r := &maintenanceRuntime{fakeRuntime: fakeRuntime{health: protocol.Health{Ready: true}, reconciled: protocol.Status{State: protocol.StateActive}, stopErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "save failed"}}, idle: true}
	c := agent.New(r, time.Second, time.Second)
	c.Initialize(context.Background())
	if release, err := c.BeginMaintenance(context.Background()); err == nil {
		release()
		t.Fatal("failed Stop accepted")
	}
	legacy := agent.New(&fakeRuntime{health: protocol.Health{Ready: true}}, time.Second, time.Second)
	if release, err := legacy.BeginMaintenance(context.Background()); err == nil {
		release()
		t.Fatal("legacy health accepted as native idle")
	}
}
