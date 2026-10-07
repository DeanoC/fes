package agent_test

import (
	"context"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/protocol"
	"strings"
	"testing"
	"time"
)

type ejectDeadlineRuntime struct {
	unitRuntime
	remaining time.Duration
	calls     int
}

func (r *ejectDeadlineRuntime) EjectMedia(ctx context.Context, b protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	r.calls++
	d, _ := ctx.Deadline()
	r.remaining = time.Until(d)
	return nil, &protocol.APIError{Code: protocol.CodeBusy, Phase: "request"}
}
func TestBoundDiskEjectKeepsFullCaptureBudgetAndOrdinaryBudget(t *testing.T) {
	for _, bound := range []bool{false, true} {
		before := computerAgentStatus()
		p := before.CorePackage
		p.MediaUnits = []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: "ready"}}
		p.ActiveInterfaces = []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}
		if bound {
			p.PersistenceMode = "persistent"
			p.MediaUnits[0].Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}
		}
		r := &ejectDeadlineRuntime{unitRuntime: unitRuntime{fakeRuntime: fakeRuntime{reconciled: before}}}
		c := agent.New(r, time.Second, 200*time.Millisecond)
		c.Initialize(context.Background())
		_, e := c.EjectMedia(context.Background(), protocol.MediaUnitBinding{PackageID: p.PackageID, Generation: p.Generation})
		if e == nil || r.calls != 1 {
			t.Fatal(e, r.calls)
		}
		if bound {
			if r.remaining < 134*time.Second || r.remaining > 135*time.Second {
				t.Fatal(r.remaining)
			}
		} else if r.remaining <= 0 || r.remaining > 200*time.Millisecond {
			t.Fatal(r.remaining)
		}
	}
}
