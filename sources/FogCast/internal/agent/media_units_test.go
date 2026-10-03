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

type unitRuntime struct {
	fakeRuntime
	inserts, ejects, observations, reconciles int
	insertErr                                 *protocol.APIError
	observed                                  []protocol.MediaUnitStatus
	inserted                                  []byte
	unit                                      protocol.MediaUnitStatus
}

func floppyUnit(state string) []protocol.MediaUnitStatus {
	return []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.Apple2FloppyInterface(), MinBytes: 143360, MaxBytes: 143360, ChunkBytes: 512, State: state}}
}

func (r *unitRuntime) units(state string) []protocol.MediaUnitStatus {
	if r.unit.Interface.ID == "" {
		return floppyUnit(state)
	}
	unit := r.unit
	unit.State = state
	return []protocol.MediaUnitStatus{unit}
}

func (r *unitRuntime) InsertMedia(ctx, owner context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	r.inserts++
	r.inserted, _ = io.ReadAll(body)
	if r.insertErr != nil {
		return nil, r.insertErr
	}
	return r.units("ready"), nil
}

func (r *unitRuntime) EjectMedia(context.Context, protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	r.ejects++
	return r.units("empty"), nil
}

func (r *unitRuntime) MediaUnits(context.Context, string, uint64) ([]protocol.MediaUnitStatus, bool) {
	r.observations++
	return r.observed, r.observed != nil
}

func (r *unitRuntime) Reconcile(ctx context.Context) protocol.Status {
	r.reconciles++
	return r.fakeRuntime.Reconcile(ctx)
}

func computerAgentStatus() protocol.Status {
	return protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: strings.Repeat("a", 64), Generation: 6, BuildID: strings.Repeat("b", 32), ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.media.apple2-floppy", Major: 1}},
		MediaUnits:       floppyUnit("empty"),
	}}
}

func TestCoordinatorMediaUnitsPublishLiveState(t *testing.T) {
	for _, media := range []struct {
		iface protocol.RuntimeContract
		bytes int64
	}{{protocol.Apple2FloppyInterface(), protocol.Apple2FloppyBytes}, {protocol.AtariStFloppyInterface(), protocol.AtariStFloppyBytes}} {
		t.Run(media.iface.ID, func(t *testing.T) { testCoordinatorMediaUnitsPublishLiveState(t, media.iface, media.bytes) })
	}
}

func testCoordinatorMediaUnitsPublishLiveState(t *testing.T, iface protocol.RuntimeContract, size int64) {
	t.Helper()
	statusBefore := computerAgentStatus()
	statusBefore.CorePackage.ActiveInterfaces[1] = protocol.RuntimeInterface{ID: iface.ID, Major: iface.Major, Minor: iface.Minor}
	unit := protocol.MediaUnitStatus{Interface: iface, MinBytes: uint32(size), MaxBytes: uint32(size), ChunkBytes: 512, State: "empty"}
	statusBefore.CorePackage.MediaUnits = []protocol.MediaUnitStatus{unit}
	runtime := &unitRuntime{fakeRuntime: fakeRuntime{reconciled: statusBefore}, unit: unit}
	c := agent.New(runtime, time.Second, time.Second)
	c.Initialize(context.Background())
	b := protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 6}
	disk := strings.Repeat("d", int(size))
	status, apiErr := c.InsertMedia(context.Background(), int64(len(disk)), strings.NewReader(disk), b)
	if apiErr != nil || runtime.inserts != 1 || len(runtime.inserted) != len(disk) {
		t.Fatalf("insert %v %d", apiErr, runtime.inserts)
	}
	if unit, ok := protocol.MediaUnit(status.CorePackage, 0); !ok || unit.State != "ready" {
		t.Fatalf("published %+v", status.CorePackage)
	}
	for name, call := range map[string]func() *protocol.APIError{
		"short": func() *protocol.APIError {
			_, err := c.InsertMedia(context.Background(), 1024, strings.NewReader(disk[:1024]), b)
			return err
		},
		"stale": func() *protocol.APIError {
			_, err := c.InsertMedia(context.Background(), int64(len(disk)), strings.NewReader(disk), protocol.MediaUnitBinding{PackageID: b.PackageID, Generation: 5})
			return err
		},
		"absent unit": func() *protocol.APIError {
			_, err := c.InsertMedia(context.Background(), int64(len(disk)), strings.NewReader(disk), protocol.MediaUnitBinding{PackageID: b.PackageID, Generation: 6, Unit: 1})
			return err
		},
	} {
		if err := call(); err == nil || runtime.inserts != 1 {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	status, apiErr = c.EjectMedia(context.Background(), b)
	if unit, ok := protocol.MediaUnit(status.CorePackage, 0); apiErr != nil || runtime.ejects != 1 || !ok || unit.State != "empty" {
		t.Fatalf("eject %v %+v", apiErr, status.CorePackage)
	}
	// A failed transfer leaves the machine running; the published unit
	// follows the runtime's live observation without replacing the session.
	runtime.insertErr = &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media transfer failed", Phase: "transport"}
	runtime.observed = runtime.units("loading")
	reconciles := runtime.reconciles
	status, apiErr = c.InsertMedia(context.Background(), int64(len(disk)), strings.NewReader(disk), b)
	if apiErr == nil || runtime.observations != 1 || runtime.reconciles != reconciles || status.CorePackage == nil || status.CorePackage.Generation != 6 {
		t.Fatalf("failed insert %v observations=%d status=%+v", apiErr, runtime.observations, status)
	}
	if unit, _ := protocol.MediaUnit(status.CorePackage, 0); unit.State != "loading" {
		t.Fatalf("unit after failure %+v", status.CorePackage.MediaUnits)
	}
}
