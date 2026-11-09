package rooms

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/testwait"
)

// Hold an older successful empty read across a newer refresh. The old
// response can arrive even after target readiness has supplied real inventory.
type refreshingHomeCores struct {
	fakeLocalCores
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
