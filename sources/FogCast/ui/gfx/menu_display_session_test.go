package gfx

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"
	"time"
)

func settleSessionFrame(t *testing.T, d *MenuDisplay) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for d.FramePending() {
		if time.Now().After(deadline) {
			t.Fatal("session frame did not complete")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestIdlePresenterRejectsSessionPixels(t *testing.T) {
	client := &testMenuClient{generation: 5, session: true, packageID: strings.Repeat("a", 64), coreGeneration: 9}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Present()
	settleSessionFrame(t, d)
	if client.snapshotCalls() != 0 || !errors.Is(d.LastError(), ErrSessionDisplayChanged) {
		t.Fatal("unbound idle frame was submitted to a running machine")
	}
}

func TestSessionPresenterPinsPackageCoreAndDisplayGenerations(t *testing.T) {
	for _, change := range []string{"package", "core generation", "display generation", "closed", "idle"} {
		t.Run(change, func(t *testing.T) {
			pkg := strings.Repeat("a", 64)
			client := &testMenuClient{generation: 5, session: true, packageID: pkg, coreGeneration: 9}
			d, err := newMenuDisplayWithClient(client)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			d.BindSession(pkg, 9)
			d.Present()
			settleSessionFrame(t, d)
			if client.snapshotCalls() != 1 || d.LastError() != nil {
				t.Fatal("captured machine could not present")
			}
			client.mu.Lock()
			switch change {
			case "package":
				client.packageID = strings.Repeat("b", 64)
			case "core generation":
				client.coreGeneration++
			case "display generation":
				client.generation++
			case "closed":
				client.unavailable = true
			case "idle":
				client.session = false
			}
			client.mu.Unlock()
			for range 2 {
				d.Present()
				settleSessionFrame(t, d)
				if client.snapshotCalls() != 1 || !errors.Is(d.LastError(), ErrSessionDisplayChanged) {
					t.Fatal("session presenter followed a replacement or closed display")
				}
			}
			if err := d.Pause(context.Background()); err != nil {
				t.Fatal(err)
			}
			client.mu.Lock()
			client.session, client.unavailable, client.generation = false, false, 20
			client.mu.Unlock()
			d.ClearSessionBinding()
			d.Resume()
			d.Present()
			settleSessionFrame(t, d)
			if client.snapshotCalls() != 2 || d.LastError() != nil {
				t.Fatal("returning to idle did not restore idle generation following")
			}
		})
	}
}

func TestSessionProbeNeverReplaysFrameOnReplacement(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	client := &testMenuClient{generation: 5, session: true, packageID: pkg, coreGeneration: 9}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.BindSession(pkg, 9)
	d.SetChangeDriven(true)
	clock := time.Unix(1700000000, 0)
	d.now = func() time.Time { return clock }
	d.Present()
	settleSessionFrame(t, d)
	client.mu.Lock()
	client.generation, client.coreGeneration = 6, 10
	client.mu.Unlock()
	clock = clock.Add(menuProbeInterval)
	d.Present()
	settleSessionFrame(t, d)
	if client.snapshotCalls() != 1 || !errors.Is(d.LastError(), ErrSessionDisplayChanged) {
		t.Fatal("change-driven probe redrew a captured frame on another session")
	}
}

// The bound session's picture comes from the runtime status its frames were
// accepted under. A rebind forgets it, and an out-of-frame picture is unknown.
func TestSessionPictureFollowsTheBoundPlane(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	client := &testMenuClient{generation: 5, session: true, packageID: pkg, coreGeneration: 9, picture: []int{312, 90, 656, 540}}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Present()
	settleSessionFrame(t, d)
	if got := d.SessionPicture(); !got.Empty() {
		t.Fatalf("idle presenter reported a session picture: %v", got)
	}
	d.BindSession(pkg, 9)
	d.Present()
	settleSessionFrame(t, d)
	if got := d.SessionPicture(); got != image.Rect(312, 90, 968, 630) {
		t.Fatalf("picture = %v", got)
	}
	d.BindSession(pkg, 9)
	if got := d.SessionPicture(); !got.Empty() {
		t.Fatalf("rebind kept the old picture: %v", got)
	}
	client.mu.Lock()
	client.picture = []int{1000, 90, 656, 540}
	client.mu.Unlock()
	d.Present()
	settleSessionFrame(t, d)
	if got := d.SessionPicture(); !got.Empty() {
		t.Fatalf("out-of-frame picture accepted: %v", got)
	}
}
