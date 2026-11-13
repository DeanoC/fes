package rooms

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/testwait"
)

// Hold an older successful empty read across a newer refresh. The old
// response can arrive even after target readiness has supplied real inventory.
type refreshingHomeCores struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (f *refreshingHomeCores) List(context.Context) ([]localcores.Core, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()
	if call == 2 {
		close(f.started)
		<-f.release
	}
	if call < 3 {
		return []localcores.Core{}, nil
	}
	return []localcores.Core{{PackageID: coreZXID, CoreID: "fes.zx81", Launchable: true}}, nil
}

func TestHomeRefreshesKitInventoryOnResumeAndIgnoresOlderRead(t *testing.T) {
	local := &refreshingHomeCores{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(local.release) }) })
	r := loadHome(t, Options{Services: &homeServices{}, Local: local, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	if d := r.Destination(); d.Label != "Sinclair ZX81" || d.Availability != AvailUnavailable {
		t.Fatalf("initial empty inventory: %+v", d)
	}

	r.Resume()
	select {
	case <-local.started:
	case <-time.After(testwait.Timeout):
		t.Fatal("refresh did not ask for kit inventory")
	}
	r.Resume()
	stepUntil(t, r, func(Frame) bool { return r.Destination().Availability == AvailReady })
	if d := r.Destination(); d.Label != "Sinclair ZX81" {
		t.Fatalf("refresh moved focus: %+v", d)
	}

	release.Do(func() { close(local.release) })
	// Wait for delivery without stepping the room, then drain the older
	// empty response. A newer successful refresh must survive that callback.
	deadline := time.Now().Add(testwait.Timeout)
	for {
		r.pendingMu.Lock()
		var delivered bool
		for _, result := range r.pending {
			delivered = delivered || result.cores != nil && len(result.cores) == 0
		}
		r.pendingMu.Unlock()
		if delivered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("older kit response was not delivered")
		}
		time.Sleep(time.Millisecond)
	}
	r.Step(time.Now())
	if d := r.Destination(); d.Availability != AvailReady || d.Label != "Sinclair ZX81" {
		t.Fatalf("old empty response replaced inventory: %+v", d)
	}
}

// A failed library refresh must not republish the old kit readiness while the
// new kit read is still in flight.
type heldSecondInventory struct {
	mu      sync.Mutex
	reads   int
	started chan struct{}
	release chan struct{}
}

func (s *heldSecondInventory) List(context.Context) ([]localcores.Core, error) {
	s.mu.Lock()
	s.reads++
	n := s.reads
	s.mu.Unlock()
	if n == 1 {
		return []localcores.Core{{PackageID: coreZXID, CoreID: "fes.zx81", Launchable: true}}, nil
	}
	if n == 2 {
		close(s.started)
		<-s.release
	}
	return []localcores.Core{}, nil
}
func TestHomeFailedRefreshDoesNotShowOldKitWhileReadIsPending(t *testing.T) {
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("old-machine", "fes.zx81", "")}, fakeServices: fakeServices{games: []hostclient.Game{machineEntry("old-machine", "fes.zx81", "")}}}
	local := &heldSecondInventory{started: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(local.release) }) })
	r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) && r.Destination().GameID == "old-machine" })
	svc.setLibraryError(errors.New("host offline"))
	svc.mu.Lock()
	svc.computers = nil
	svc.hardwareErr = errors.New("host offline")
	svc.mu.Unlock()
	r.Resume()
	select {
	case <-local.started:
	case <-time.After(testwait.Timeout):
		t.Fatal("new inventory did not start")
	}
	stepUntil(t, r, func(Frame) bool { return len(svc.recorded()) >= 8 })
	// Drain any remaining asynchronous library failures while the kit read stays held.
	for i := 0; i < 50; i++ {
		r.Step(time.Now())
		time.Sleep(time.Millisecond)
	}
	d := r.Destination()
	if d.Availability == AvailReady {
		t.Fatalf("stale old kit readiness while fresh inventory is pending: %+v", d)
	}
	once.Do(func() { close(local.release) })
	stepUntil(t, r, func(Frame) bool { return r.Destination().Availability == AvailUnavailable })
	if d := r.Destination(); d.GameID != "" {
		t.Fatalf("fresh empty kit retained machine: %+v", d)
	}
}

func TestHomeUnavailableInventoryIsNotReportedAsNotInstalled(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{err: errors.New("host offline")}}
	local := &fakeLocalCores{listErr: errors.New("kit offline")}
	r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	d := r.Destination()
	if d.Status != "Can't check this kit" || d.Availability == AvailReady {
		t.Fatalf("unknown kit reported as installed or absent: %+v", d)
	}
	if strings.Contains(frameText(r.frame), " on this kit") {
		t.Fatalf("unknown inventory reported an installed count: %s", frameText(r.frame))
	}
}

func TestHomeUsesFreshAdmittedComputerWhenKitInventoryFails(t *testing.T) {
	svc := &homeServices{computers: []hostclient.ShelfComputer{computerEntry("fresh-machine", "fes.zx81", "")}, fakeServices: fakeServices{games: []hostclient.Game{machineEntry("fresh-machine", "fes.zx81", "")}}}
	local := &fakeLocalCores{listErr: errors.New("kit offline")}
	r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) && r.Destination().GameID == "fresh-machine" })
	if d := r.Destination(); d.Availability != AvailReady {
		t.Fatalf("fresh library machine lost: %+v", d)
	}
	if strings.Contains(frameText(r.frame), " on this kit") {
		t.Fatalf("unknown inventory reported an installed count: %s", frameText(r.frame))
	}
}

type heldFamilyRefresh struct {
	homeServices
	holdMu      sync.Mutex
	hold        bool
	started     chan struct{}
	startedOnce sync.Once
	release     chan struct{}
}

func (s *heldFamilyRefresh) Hardware(ctx context.Context) (hostclient.HardwareSnapshot, error) {
	s.holdMu.Lock()
	held := s.hold
	s.holdMu.Unlock()
	if held {
		select {
		case <-s.release:
			return hostclient.HardwareSnapshot{}, errors.New("host offline")
		case <-ctx.Done():
			return hostclient.HardwareSnapshot{}, ctx.Err()
		}
	}
	return s.homeServices.Hardware(ctx)
}

func (s *heldFamilyRefresh) QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, bool, error) {
	s.holdMu.Lock()
	held := s.hold
	s.holdMu.Unlock()
	if held && q.HomeFamily {
		s.startedOnce.Do(func() { close(s.started) })
		select {
		case <-s.release:
			return nil, false, errors.New("host offline")
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	return s.homeServices.QueryGames(ctx, q, max)
}

type failedSecondInventory struct {
	mu    sync.Mutex
	reads int
	done  chan struct{}
}

func (s *failedSecondInventory) List(context.Context) ([]localcores.Core, error) {
	s.mu.Lock()
	s.reads++
	n := s.reads
	s.mu.Unlock()
	if n == 1 {
		return []localcores.Core{{PackageID: coreZXID, CoreID: "fes.zx81", Launchable: true}}, nil
	}
	if n == 2 {
		close(s.done)
	}
	return nil, errors.New("kit offline")
}

func TestHomeRefreshDoesNotShowOldMachineWhileNewReadsPending(t *testing.T) {
	svc := &heldFamilyRefresh{homeServices: homeServices{computers: []hostclient.ShelfComputer{computerEntry("old-machine", "fes.zx81", "")}, fakeServices: fakeServices{games: []hostclient.Game{machineEntry("old-machine", "fes.zx81", "")}}}, started: make(chan struct{}), release: make(chan struct{})}
	local := &failedSecondInventory{done: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(svc.release) }) })
	r := loadHome(t, Options{Services: svc, Local: local, ReleaseOnly: true})
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) && r.Destination().GameID == "old-machine" })
	svc.holdMu.Lock()
	svc.hold = true
	svc.holdMu.Unlock()
	r.Resume()
	select {
	case <-svc.started:
	case <-time.After(testwait.Timeout):
		t.Fatal("refresh library did not start")
	}
	select {
	case <-local.done:
	case <-time.After(testwait.Timeout):
		t.Fatal("refresh kit read did not return")
	}
	for i := 0; i < 50; i++ {
		r.Step(time.Now())
		time.Sleep(time.Millisecond)
	}
	if d := r.Destination(); d.Availability == AvailReady || d.GameID != "" {
		t.Fatalf("stale library readiness while refresh pending: %+v", d)
	}
	release.Do(func() { close(svc.release) })
}
