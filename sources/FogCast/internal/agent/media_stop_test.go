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
	for _, tc := range []struct {
		name  string
		bound bool
		ata   bool
	}{{name: "volatile"}, {name: "bound ST", bound: true}, {name: "bound ATA", bound: true, ata: true}} {
		t.Run(tc.name, func(t *testing.T) {
			p := mediaStopPackage()
			if tc.ata {
				saveable := false
				p.ABI.Minor = 1
				p.PersistenceMode = "persistent"
				p.ActiveInterfaces = []protocol.RuntimeInterface{{ID: protocol.AtaDiskInterface().ID, Major: protocol.AtaDiskInterface().Major}}
				p.MediaUnits = []protocol.MediaUnitStatus{{Unit: protocol.AtaDiskUnit, Interface: protocol.AtaDiskInterface(),
					MinBytes: uint32(protocol.AtaDiskMinBytes), MaxBytes: uint32(protocol.AtaDiskMaxBytes), ChunkBytes: protocol.AtaDiskChunkBytes,
					State: protocol.MediaUnitReady, Saveable: &saveable,
					Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "ultima-vi", BaseMediaID: strings.Repeat("c", 64), Revision: "absent"}}}
			} else if !tc.bound {
				p.MediaUnits[0].Persistence = nil
			}
			r := &mediaStopRuntime{}
			r.reconciled = protocol.Status{State: protocol.StateActive, Development: true, CorePackage: p}
			c := agent.New(r, time.Second, 5*time.Millisecond)
			c.Initialize(context.Background())
			status, err := c.Stop(context.Background())
			if tc.bound {
				if err != nil || status.State != protocol.StateIdle || r.budget < 134*time.Second || r.budget > 135*time.Second {
					t.Fatalf("status=%+v err=%v budget=%v", status, err, r.budget)
				}
			} else if err == nil || r.budget > 5*time.Millisecond {
				t.Fatalf("volatile budget=%v err=%v", r.budget, err)
			}
		})
	}
}
