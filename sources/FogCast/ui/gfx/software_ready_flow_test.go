package gfx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

func softwareReadyFlow(t *testing.T) (*MenuDisplay, *testMenuClient, *menuClock, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ready.json")
	t.Setenv("FES_SOFTWARE_READY_FILE", path)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", "bundle")
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)
	requireSoftwareReady(t, path, true)
	return d, client, clock, path
}
func requireSoftwareReady(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Stat(path)
	if want && err != nil {
		t.Fatalf("readiness missing: %v", err)
	}
	if !want && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("readiness retained: %v", err)
	}
}
func TestSoftwareReadinessRecoversOnUnchangedHealthyProbe(t *testing.T) {
	d, client, clock, path := softwareReadyFlow(t)
	frameGate, probeGate, probeEntered := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	var frameOnce, probeOnce sync.Once
	releaseFrame := func() { frameOnce.Do(func() { close(frameGate) }) }
	releaseProbe := func() { probeOnce.Do(func() { close(probeGate) }) }
	defer releaseFrame()
	defer releaseProbe()
	client.mu.Lock()
	client.fail = true
	client.presentBlock = frameGate
	client.mu.Unlock()
	d.Clear(RGB(4, 5, 6))
	presentMenu(t, d, client)
	client.mu.Lock()
	client.holdStatus = func() { probeEntered <- struct{}{}; <-probeGate }
	client.mu.Unlock()
	clock.Advance(menuProbeInterval)
	d.Present()
	releaseFrame()
	select {
	case <-probeEntered:
	case <-time.After(time.Second):
		t.Fatal("healthy probe did not follow rejected frame")
	}
	requireSoftwareReady(t, path, false)
	releaseProbe()
	waitMenuStatus(t, d, client, 3)
	requireSoftwareReady(t, path, true)
	if calls, _, _, _ := client.snapshot(); calls != 2 {
		t.Fatal("healthy unchanged probe unnecessarily re-presented frame", calls)
	}
}
func TestSoftwareReadinessRejectsUnavailableAndUnderflow(t *testing.T) {
	for _, name := range []string{"unavailable", "underflow"} {
		t.Run(name, func(t *testing.T) {
			d, client, clock, path := softwareReadyFlow(t)
			client.mu.Lock()
			if name == "unavailable" {
				client.unavailable = true
			} else {
				client.underflows = menudisplay.TransientUnderflowCap + 1
			}
			client.mu.Unlock()
			clock.Advance(menuProbeInterval)
			d.Present()
			waitMenuStatus(t, d, client, 2)
			requireSoftwareReady(t, path, false)
			client.mu.Lock()
			client.unavailable = false
			client.underflows = 0
			client.mu.Unlock()
			clock.Advance(menuBackoffCap)
			d.Present()
			waitMenuStatus(t, d, client, 3)
			requireSoftwareReady(t, path, true)
		})
	}
}
func TestSoftwareReadinessInvalidatedByPauseBindingAndClose(t *testing.T) {
	d, client, _, path := softwareReadyFlow(t)
	if err := d.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireSoftwareReady(t, path, false)
	d.Resume()
	requireSoftwareReady(t, path, false)
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 2)
	requireSoftwareReady(t, path, true)
	d.BindSession("session", 8)
	requireSoftwareReady(t, path, false)
	d.ClearSessionBinding()
	requireSoftwareReady(t, path, false)
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 3)
	requireSoftwareReady(t, path, true)
	d.Close()
	requireSoftwareReady(t, path, false)
}
