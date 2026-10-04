package fogcast

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type mediaStopClient struct {
	fakeServiceClient
	savedCalls int
	budget     time.Duration
}

func (c *mediaStopClient) StopWithMediaSave(ctx context.Context) (protocol.Status, error) {
	c.savedCalls++
	d, _ := ctx.Deadline()
	c.budget = time.Until(d)
	select {
	case <-time.After(20 * time.Millisecond):
		return protocol.Status{State: protocol.StateIdle}, nil
	case <-ctx.Done():
		return protocol.Status{}, ctx.Err()
	}
}
func TestStopSelectsDurableDiskBudgetAndPreservesCallerCancellation(t *testing.T) {
	for _, mode := range []string{"bound", "forgotten-package", "volatile", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			p := &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}}
			if mode == "volatile" {
				p.MediaUnits[0].Persistence = nil
			}
			client := &mediaStopClient{fakeServiceClient: fakeServiceClient{statusResult: protocol.Status{State: protocol.StateActive, CorePackage: p}, stopResult: protocol.Status{State: protocol.StateIdle}}}
			s := &Service{requestTimeout: time.Second, uploadTimeout: 10 * time.Millisecond, selectedTarget: "kit", activeTarget: "kit", activeExecution: ExecutionFPGANative, activePackageID: p.PackageID, activePackageGeneration: p.Generation, targets: []TargetConfig{{Name: "kit", Enabled: true}}, targetClients: map[string]serviceClient{"kit": client}}
			if mode == "forgotten-package" {
				s.activePackageID = ""
				s.activePackageGeneration = 0
			}
			ctx := context.Background()
			cancel := func() {}
			if mode == "canceled" {
				ctx, cancel = context.WithTimeout(ctx, 5*time.Millisecond)
			}
			defer cancel()
			status, err := s.Stop(ctx)
			if mode == "volatile" {
				if err != nil || client.stopCalls != 1 || client.savedCalls != 0 {
					t.Fatal(status, err, client.stopCalls, client.savedCalls)
				}
			} else if mode == "bound" || mode == "forgotten-package" {
				if err != nil || status.State != protocol.StateIdle || client.savedCalls != 1 || client.stopCalls != 0 || client.budget < 149*time.Second {
					t.Fatal(status, err, client.budget)
				}
			} else if err == nil || client.savedCalls != 1 || client.stopCalls != 0 || client.budget > 5*time.Millisecond || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatal(err, client.budget)
			}
		})
	}
}
