package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaStopRuntime struct {
	ownedDevelopmentContextRuntime
	budget time.Duration
}

func (r *mediaStopRuntime) StopOwned(_ context.Context, operation context.Context) (string, *protocol.APIError) {
	deadline, _ := operation.Deadline()
	r.budget = time.Until(deadline)
	// A capture exceeding the ordinary Stop budget must remain admitted.
	select {
	case <-time.After(20 * time.Millisecond):
		return "MENU", nil
	case <-operation.Done():
		return "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable}
	}
}
func mediaStopPackage() *protocol.CorePackageStatus {
	return &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}}
}
func TestStopBudgetAllowsBoundDiskCaptureOnly(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(map[bool]string{false: "volatile", true: "bound"}[bound], func(t *testing.T) {
			p := mediaStopPackage()
			if !bound {
				p.MediaUnits[0].Persistence = nil
			}
			r := &mediaStopRuntime{}
			r.reconciled = protocol.Status{State: protocol.StateActive, Development: true, CorePackage: p}
			c := agent.New(r, time.Second, 5*time.Millisecond)
			c.Initialize(context.Background())
			status, err := c.Stop(context.Background())
			if bound {
				if err != nil || status.State != protocol.StateIdle || r.budget < 134*time.Second || r.budget > 135*time.Second {
					t.Fatalf("status=%+v err=%v budget=%v", status, err, r.budget)
				}
			} else if err == nil || r.budget > 5*time.Millisecond {
				t.Fatalf("volatile budget=%v err=%v", r.budget, err)
			}
		})
	}
}
