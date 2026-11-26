package agent_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/protocol"
)

// pc386UnitRuntime answers a pc386 insert or eject with the runtime's units.
type pc386UnitRuntime struct {
	unitRuntime
	after []protocol.MediaUnitStatus
}

func (r *pc386UnitRuntime) InsertLibraryMedia(_, _ context.Context, _ int64, body io.Reader, _ protocol.LibraryMediaBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	r.inserts++
	r.inserted, _ = io.ReadAll(body)
	return protocol.CloneMediaUnits(r.after), nil
}

func (r *pc386UnitRuntime) SaveMedia(context.Context, protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	panic("pc386 floppies are saved at Stop and on swap, never live")
}

func (r *pc386UnitRuntime) EjectMedia(context.Context, protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	r.ejects++
	return protocol.CloneMediaUnits(r.after), nil
}

func pc386Units(disk bool, floppyA *protocol.MediaDataStatus) []protocol.MediaUnitStatus {
	noSave := false
	ata := protocol.MediaUnitStatus{Unit: protocol.AtaDiskUnit, Interface: protocol.AtaDiskInterface(),
		MinBytes: uint32(protocol.AtaDiskMinBytes), MaxBytes: uint32(protocol.AtaDiskMaxBytes),
		ChunkBytes: protocol.AtaDiskChunkBytes, State: protocol.MediaUnitEmpty, Saveable: &noSave}
	if disk {
		ata.State = protocol.MediaUnitReady
		ata.Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: "msdos-622", BaseMediaID: strings.Repeat("c", 64), Revision: "absent"}
	}
	units := []protocol.MediaUnitStatus{ata}
	for unit := uint8(protocol.PcFloppyUnitA); unit <= protocol.PcFloppyUnitB; unit++ {
		f := protocol.MediaUnitStatus{Unit: unit, Interface: protocol.PcFloppyInterface(), MinBytes: uint32(protocol.PcFloppyMinBytes),
			MaxBytes: uint32(protocol.PcFloppyMaxBytes), ChunkBytes: 512, State: protocol.MediaUnitEmpty, Saveable: &noSave}
		if unit == protocol.PcFloppyUnitA && floppyA != nil {
			f.State = protocol.MediaUnitReady
			f.Persistence = floppyA
		}
		units = append(units, f)
	}
	return units
}

func pc386AgentStatus(units []protocol.MediaUnitStatus) protocol.Status {
	s := computerAgentStatus()
	p := s.CorePackage
	p.ABI = protocol.RuntimeContract{ID: "fes.computer", Major: 1, Minor: 1}
	p.ActiveInterfaces = []protocol.RuntimeInterface{{ID: protocol.AtaDiskInterface().ID, Major: 1},
		{ID: protocol.PcFloppyInterface().ID, Major: 1}}
	p.PersistenceMode = "persistent" // the pc386 machine record
	p.MediaUnits = units
	return s
}

// A pc386 floppy insert or eject keeps the runtime's persistent mode: the
// host refuses a unit carrying a binding in a status that is not persistent.
func TestPc386FloppyUnitsKeepPersistentMode(t *testing.T) {
	floppy := &protocol.MediaDataStatus{Mode: "persistent", GameID: "msdos-622", BaseMediaID: strings.Repeat("e", 64), Revision: "absent"}
	for _, disk := range []bool{false, true} {
		runtime := &pc386UnitRuntime{unitRuntime: unitRuntime{fakeRuntime: fakeRuntime{reconciled: pc386AgentStatus(pc386Units(disk, nil))}}}
		c := agent.New(runtime, time.Second, time.Second)
		c.Initialize(context.Background())
		b := protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 6, Unit: protocol.PcFloppyUnitA}
		runtime.after = pc386Units(disk, floppy)
		image := strings.Repeat("f", 1474560)
		status, apiErr := c.InsertLibraryMedia(context.Background(), int64(len(image)), strings.NewReader(image),
			protocol.LibraryMediaBinding{MediaUnitBinding: b, GameID: "msdos-622", BaseMediaID: floppy.BaseMediaID})
		if apiErr != nil || runtime.inserts != 1 || status.CorePackage.PersistenceMode != "persistent" ||
			!protocol.PersistentMediaBound(status.CorePackage) {
			t.Fatalf("disk=%v insert %v %+v", disk, apiErr, status.CorePackage)
		}
		runtime.after = pc386Units(disk, nil)
		status, apiErr = c.EjectMedia(context.Background(), b)
		if apiErr != nil || runtime.ejects != 1 || status.CorePackage.PersistenceMode != "persistent" {
			t.Fatalf("disk=%v eject %v %+v", disk, apiErr, status.CorePackage)
		}
	}
}
