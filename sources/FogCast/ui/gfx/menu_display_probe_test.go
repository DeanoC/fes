package gfx

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

type menuClock struct {
	mu sync.Mutex
	t  time.Time
}

func newMenuClock() *menuClock {
	return &menuClock{t: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)}
}

func (c *menuClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *menuClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func (c *menuClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func waitMenuStatus(t *testing.T, d *MenuDisplay, client *testMenuClient, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		d.mu.Lock()
		idle := d.flight == nil && len(d.frames) == 0 && !d.probeQueued
		d.mu.Unlock()
		if idle && client.statusCount() >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("status calls %d, want >= %d", client.statusCount(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMenuDisplayProbeRedrawsNewGeneration(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	clock.Advance(menuProbeInterval - time.Nanosecond)
	statusBefore := client.statusCount()
	d.Present()
	waitMenuIdle(t, d, client, 1)
	if client.statusCount() != statusBefore || d.FramePending() {
		t.Fatal("probed before the interval")
	}

	client.mu.Lock()
	client.generation = 9
	client.mu.Unlock()
	clock.Advance(time.Nanosecond)
	d.Present()
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 2)
	calls, gen, n, prefix := client.snapshot()
	if calls != 2 || gen != 9 || n != menudisplay.FrameBytes || prefix[0] != 1 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}
}

func TestMenuDisplayProbeSameGenerationPresentsNothing(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)
	statusBefore := client.statusCount()

	clock.Advance(menuProbeInterval)
	d.Present()
	waitMenuIdle(t, d, client, 1)
	if client.statusCount() != statusBefore+1 {
		t.Fatalf("status calls %d, want %d", client.statusCount(), statusBefore+1)
	}
	if calls, _, _, _ := client.snapshot(); calls != 1 {
		t.Fatalf("same generation presented, calls=%d", calls)
	}

	d.Present()
	waitMenuIdle(t, d, client, 1)
	if client.statusCount() != statusBefore+1 {
		t.Fatal("probed again before the next interval")
	}
}

// An available status is not evidence that anything is on the wire: before
// the runtime's idle health check nothing re-read the display after the
// first confirmed present. A probe may only acknowledge an unchanged frame
// while the live reads still describe this client's frame being scanned out.
func TestMenuDisplayProbeAcceptsHealthyLiveState(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)
	client.mu.Lock()
	client.live = &menudisplay.Live{Enabled: true, DisplayedSequence: client.displayed}
	client.mu.Unlock()

	clock.Advance(menuProbeInterval)
	d.Present()
	waitMenuIdle(t, d, client, 1)
	if calls, _, _, _ := client.snapshot(); calls != 1 {
		t.Fatalf("healthy live state forced a present, calls=%d", calls)
	}
	if err := d.LastError(); err != nil {
		t.Fatalf("healthy probe reported %v", err)
	}
}

func TestMenuDisplayProbeForcesPresentWhenScanoutIsLost(t *testing.T) {
	for _, test := range []struct {
		name string
		live *menudisplay.Live
	}{
		{"disabled", &menudisplay.Live{Enabled: false}},
		{"faulted", &menudisplay.Live{Enabled: true, Faulted: true}},
		{"quiesced", &menudisplay.Live{Enabled: true, Quiesced: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, client := newChangeDrivenMenu(t, 3, true)
			clock := newMenuClock()
			d.now = clock.Now
			d.Clear(RGB(1, 2, 3))
			presentMenu(t, d, client)
			waitMenuIdle(t, d, client, 1)
			client.mu.Lock()
			client.live = test.live
			client.mu.Unlock()

			clock.Advance(menuProbeInterval)
			d.Present()
			waitMenuIdle(t, d, client, 2)
			if calls, _, n, _ := client.snapshot(); calls != 2 || n != menudisplay.FrameBytes {
				t.Fatalf("lost scanout was acknowledged, calls=%d bytes=%d", calls, n)
			}
		})
	}
}

// The cached sequence is the only form of this loss an older runtime can
// report: it has no live reads at all.
func TestMenuDisplayProbeForcesPresentOnStaleDisplayedSequence(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)
	client.mu.Lock()
	client.displayed += 4
	client.mu.Unlock()

	clock.Advance(menuProbeInterval)
	d.Present()
	waitMenuIdle(t, d, client, 2)
	if calls, _, n, _ := client.snapshot(); calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("stale sequence was acknowledged, calls=%d bytes=%d", calls, n)
	}
}

// A new runtime sets status.DisplayedSequence from the same value it
// returned to this client's present, so the two can never disagree. The live
// register read is the one that can, and the probe must prefer it: scanout
// showing some other frame is a loss however healthy the cached field looks.
func TestMenuDisplayProbeForcesPresentOnStaleLiveSequence(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)
	client.mu.Lock()
	// Healthy in every other respect, and the cached sequence still matches.
	client.live = &menudisplay.Live{Enabled: true, DisplayedSequence: client.displayed + 4}
	client.mu.Unlock()

	clock.Advance(menuProbeInterval)
	d.Present()
	waitMenuIdle(t, d, client, 2)
	if calls, _, n, _ := client.snapshot(); calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("stale live sequence was acknowledged, calls=%d bytes=%d", calls, n)
	}
}

// An old runtime measures nothing. Absent live reads mean "not measured",
// never "healthy", so the probe keeps working from the cached sequence
// instead of acknowledging anything it cannot see.
func TestMenuDisplayProbeAcceptsAbsentLiveStateFromAnOlderRuntime(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	clock.Advance(menuProbeInterval)
	d.Present()
	waitMenuIdle(t, d, client, 1)
	if calls, _, _, _ := client.snapshot(); calls != 1 {
		t.Fatalf("an older runtime's status forced a present, calls=%d", calls)
	}
}

func TestMenuDisplayQueuedProbeClearsTransientPresentError(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	// A probe can already be queued when an in-flight frame is rejected by
	// a concurrent runtime media operation. Its healthy status must clear
	// that transient error without presenting the unchanged generation.
	gate := make(chan struct{})
	client.mu.Lock()
	client.fail = true
	client.presentBlock = gate
	client.mu.Unlock()
	d.Clear(RGB(4, 5, 6))
	presentMenu(t, d, client)
	clock.Advance(menuProbeInterval)
	d.Present()
	d.mu.Lock()
	queued := d.probeQueued
	d.mu.Unlock()
	close(gate)
	if !queued {
		t.Fatal("healthy probe was not queued behind the rejected frame")
	}
	waitMenuStatus(t, d, client, 3)
	if err := d.LastError(); err != nil {
		t.Fatalf("healthy probe retained presentation error: %v", err)
	}
	d.mu.Lock()
	backoff, until := d.backoff, d.backoffUntil
	d.mu.Unlock()
	if backoff != 0 || !until.IsZero() {
		t.Fatalf("healthy probe retained backoff %s until %s", backoff, until)
	}
	if calls, _, _, _ := client.snapshot(); calls != 2 {
		t.Fatalf("healthy probe re-presented an unchanged generation: calls=%d", calls)
	}
}

func TestMenuDisplayQueuedProbeDoesNotAcknowledgeFailedFrame(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	// Frame A is on screen. Frame B, the loading frame, fails while a probe
	// for B is already queued. The probe reads A's generation and must not
	// treat A's presentation as B having been shown.
	gate := make(chan struct{})
	client.mu.Lock()
	client.fail = true
	client.presentBlock = gate
	client.mu.Unlock()
	d.Clear(RGB(4, 5, 6))
	presentMenu(t, d, client)
	clock.Advance(menuProbeInterval)
	d.Present()
	d.mu.Lock()
	queued := d.probeQueued
	d.mu.Unlock()
	close(gate)
	if !queued {
		t.Fatal("probe was not queued behind the failing frame")
	}
	waitMenuStatus(t, d, client, 3)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := d.WaitIdle(ctx)
	if err == nil || !strings.Contains(err.Error(), "stale menu") {
		t.Fatalf("WaitIdle after probed failure = %v, want present error", err)
	}
	if calls, _, _, _ := client.snapshot(); calls != 2 {
		t.Fatalf("probe presented the failed frame: calls=%d", calls)
	}
}

func TestMenuDisplayStaleProbeDoesNotChangeReboundState(t *testing.T) {
	gate := make(chan struct{})
	var block atomic.Bool
	client := &testMenuClient{generation: 4, ready: make(chan struct{}, 4), statusSeen: make(chan uint64, 8)}
	client.holdStatus = func() {
		if block.Load() {
			<-gate
		}
	}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		block.Store(false)
		if !closed {
			close(gate)
		}
		d.Close()
	})
	d.SetChangeDriven(true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	block.Store(true)
	clock.Advance(menuProbeInterval)
	d.Present()
	deadline := time.Now().Add(time.Second)
	for client.statusCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("probe did not reach status")
		}
		time.Sleep(time.Millisecond)
	}

	d.BindSession("new-package", 77)
	d.mu.Lock()
	d.generation = 91
	d.known = true
	d.lastErr = errors.New("new binding error")
	d.hasPresented = false
	d.presentedGen = 92
	d.backoff = 3 * time.Second
	d.backoffUntil = clock.Now().Add(3 * time.Second)
	d.lastProbe = clock.Now().Add(-time.Minute)
	// Model a probe queued for the new epoch while the old one is still
	// completing. The old completion must not clear its queued marker.
	d.probeQueued = true
	d.probeEpoch = d.bindingEpoch
	wantEpoch := d.bindingEpoch
	d.mu.Unlock()

	closed = true
	close(gate)
	block.Store(false)
	deadline = time.Now().Add(time.Second)
	for {
		d.mu.Lock()
		idle := d.flight == nil
		d.mu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stale probe did not finish")
		}
		time.Sleep(time.Millisecond)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.bindingEpoch != wantEpoch || d.generation != 91 || !d.known || d.lastErr == nil || d.lastErr.Error() != "new binding error" ||
		d.hasPresented || d.presentedGen != 92 || d.backoff != 3*time.Second || !d.backoffUntil.Equal(clock.Now().Add(3*time.Second)) ||
		!d.lastProbe.Equal(clock.Now().Add(-time.Minute)) || !d.probeQueued || d.probeEpoch != wantEpoch {
		t.Fatalf("stale probe changed rebound state: epoch=%d generation=%d known=%t err=%v presented=%t/%d backoff=%s until=%s lastProbe=%s queued=%t probeEpoch=%d",
			d.bindingEpoch, d.generation, d.known, d.lastErr, d.hasPresented, d.presentedGen, d.backoff, d.backoffUntil, d.lastProbe, d.probeQueued, d.probeEpoch)
	}
}

func TestMenuDisplayPresentCompletionDoesNotChangeReboundState(t *testing.T) {
	for _, probe := range []bool{false, true} {
		name := "frame"
		if probe {
			name = "probe"
		}
		t.Run(name, func(t *testing.T) {
			gate := make(chan struct{})
			client := &testMenuClient{generation: 4, ready: make(chan struct{}, 4)}
			d, err := newMenuDisplayWithClient(client)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			d.SetChangeDriven(true)
			clock := newMenuClock()
			d.now = clock.Now
			d.Clear(RGB(1, 2, 3))
			presentMenu(t, d, client)
			waitMenuIdle(t, d, client, 1)

			if probe {
				// A changed runtime generation makes a real scheduled probe
				// re-present the last submitted pixels.
				clock.Advance(menuProbeInterval)
				client.mu.Lock()
				client.generation = 5
				client.mu.Unlock()
				client.presentBlock = gate
				d.Present()
			} else {
				client.presentBlock = gate
				d.Clear(RGB(4, 5, 6))
				d.Present()
			}
			select {
			case <-client.ready:
			case <-time.After(time.Second):
				t.Fatal("runtime Present did not start after Status")
			}

			d.BindSession("new-package", 77)
			wantErr := errors.New("new binding error")
			d.mu.Lock()
			wantSubmission := &submittedMenuFrame{seq: d.nextSeq + 100}
			d.generation = 91
			d.known = true
			d.lastErr = wantErr
			d.submitted = wantSubmission
			d.hasPresented = true
			d.presentedGen = 92
			d.backoff = 3 * time.Second
			d.backoffUntil = clock.Now().Add(3 * time.Second)
			d.lastProbe = clock.Now().Add(-time.Minute)
			d.mu.Unlock()

			close(gate)
			deadline := time.Now().Add(time.Second)
			for {
				d.mu.Lock()
				idle := d.flight == nil
				d.mu.Unlock()
				if idle {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("stale Present completion did not release its flight")
				}
				time.Sleep(time.Millisecond)
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if d.pending != 0 || d.flight != nil || d.generation != 91 || !d.known || d.lastErr != wantErr ||
				d.submitted != wantSubmission || !d.hasPresented || d.presentedGen != 92 || d.backoff != 3*time.Second ||
				!d.backoffUntil.Equal(clock.Now().Add(3*time.Second)) || !d.lastProbe.Equal(clock.Now().Add(-time.Minute)) {
				t.Fatalf("stale %s completion changed rebound state: pending=%d flight=%v generation=%d known=%t err=%v submitted=%p presented=%t/%d backoff=%s until=%s lastProbe=%s",
					name, d.pending, d.flight, d.generation, d.known, d.lastErr, d.submitted, d.hasPresented, d.presentedGen, d.backoff, d.backoffUntil, d.lastProbe)
			}
		})
	}
}

func TestMenuDisplaySameEpochProbeResultApplies(t *testing.T) {
	d, _ := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	t.Cleanup(d.Close)
	d.mu.Lock()
	epoch := d.bindingEpoch
	frame := queuedMenuFrame{epoch: epoch, seq: 1}
	d.probeQueued = true
	d.probeEpoch = epoch
	d.mu.Unlock()
	d.finishProbeLocked(frame, menudisplay.Status{Available: true, Generation: 12}, 7, nil, true)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.probeQueued || !d.known || d.generation != 12 || !d.hasPresented || d.presentedGen != 12 || d.lastErr != nil || d.backoff != 0 || !d.backoffUntil.IsZero() {
		t.Fatalf("same-epoch probe result not applied: queued=%t known=%t generation=%d presented=%t/%d err=%v backoff=%s until=%s", d.probeQueued, d.known, d.generation, d.hasPresented, d.presentedGen, d.lastErr, d.backoff, d.backoffUntil)
	}
}

func TestMenuDisplayProbePresentsWhenNeverPresented(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(4, 5, 6))
	pix := append([]byte(nil), d.Software.Framebuffer().Pix...)
	d.mu.Lock()
	d.submitted = &submittedMenuFrame{pixels: pix, generation: 0, seq: 1}
	d.generation = 0
	d.lastProbe = clock.Now().Add(-menuProbeInterval)
	d.hasPresented = false
	d.mu.Unlock()
	d.Present()
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 1)
	calls, gen, n, prefix := client.snapshot()
	if calls != 1 || gen != 3 || n != menudisplay.FrameBytes || prefix[0] != 4 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}
}

func TestMenuDisplayBackoffSuppressesQueue(t *testing.T) {
	cases := []struct {
		name     string
		arm      func(*testMenuClient)
		presents int
	}{
		{name: "status", arm: func(c *testMenuClient) { c.statusErr = errors.New("socket missing") }},
		{name: "unavailable", arm: func(c *testMenuClient) { c.unavailable = true }},
		{name: "present", arm: func(c *testMenuClient) { c.fail = true }, presents: 1},
		{name: "underflow", arm: func(c *testMenuClient) { c.underflows = menudisplay.TransientUnderflowCap + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, client := newChangeDrivenMenu(t, 7, true)
			clock := newMenuClock()
			d.now = clock.Now
			tc.arm(client)
			d.Clear(RGB(1, 2, 3))
			d.Present()
			waitMenuIdle(t, d, client, tc.presents)
			if tc.name == "underflow" {
				if err := d.LastError(); err == nil || err.Error() != "menu scanout underflow" {
					t.Fatalf("last error %v", err)
				}
			}
			beforeStatus := client.statusCount()
			beforeCalls, _, _, _ := client.snapshot()
			d.Clear(RGB(9, 8, 7))
			d.Present()
			if d.FramePending() || client.statusCount() != beforeStatus {
				t.Fatalf("queued during backoff (status %d)", client.statusCount())
			}
			if calls, _, _, _ := client.snapshot(); calls != beforeCalls {
				t.Fatalf("present calls %d, want %d", calls, beforeCalls)
			}
		})
	}
}

func TestMenuDisplayBackoffDoublesToCap(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	client.mu.Lock()
	client.statusErr = errors.New("socket missing")
	client.mu.Unlock()
	want := menuBackoffInitial
	for step := 0; step < 7; step++ {
		d.Present()
		waitMenuIdle(t, d, client, 0)
		d.mu.Lock()
		got := d.backoff
		until := d.backoffUntil
		d.mu.Unlock()
		if got != want {
			t.Fatalf("step %d backoff %s, want %s", step, got, want)
		}
		clock.Set(until.Add(-time.Millisecond))
		before := client.statusCount()
		d.Present()
		if d.FramePending() || client.statusCount() != before {
			t.Fatalf("step %d queued before backoff elapsed", step)
		}
		clock.Set(until)
		next := want * 2
		if next > menuBackoffCap {
			next = menuBackoffCap
		}
		want = next
	}
	if want != menuBackoffCap {
		t.Fatalf("cap walk ended at %s", want)
	}
}

func TestMenuDisplayBackoffResetsOnSuccess(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	client.mu.Lock()
	client.statusErr = errors.New("socket missing")
	client.mu.Unlock()
	d.Present()
	waitMenuIdle(t, d, client, 0)
	d.mu.Lock()
	until := d.backoffUntil
	d.mu.Unlock()
	clock.Set(until)
	d.Present()
	waitMenuIdle(t, d, client, 0)
	d.mu.Lock()
	if d.backoff != menuBackoffInitial*2 {
		t.Fatalf("backoff %s, want doubled", d.backoff)
	}
	until = d.backoffUntil
	d.mu.Unlock()

	client.mu.Lock()
	client.statusErr = nil
	client.mu.Unlock()
	clock.Set(until)
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)
	d.mu.Lock()
	if d.backoff != 0 || !d.backoffUntil.IsZero() {
		t.Fatalf("backoff %s until %s after success", d.backoff, d.backoffUntil)
	}
	d.mu.Unlock()

	client.mu.Lock()
	client.statusErr = errors.New("socket missing")
	client.mu.Unlock()
	d.Clear(RGB(8, 8, 8))
	d.Present()
	waitMenuIdle(t, d, client, 1)
	d.mu.Lock()
	got := d.backoff
	d.mu.Unlock()
	if got != menuBackoffInitial {
		t.Fatalf("backoff %s, want reset to %s", got, menuBackoffInitial)
	}
}

func TestMenuDisplayBackoffRecoveryRedraws(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	client.mu.Lock()
	client.statusErr = errors.New("socket missing")
	client.mu.Unlock()
	clock.Advance(menuProbeInterval)
	d.Present()
	waitMenuIdle(t, d, client, 1)
	d.mu.Lock()
	kept := d.submitted != nil && d.generation == d.submitted.generation
	until := d.backoffUntil
	d.mu.Unlock()
	if !kept {
		t.Fatal("failed probe forgot the frame or moved its generation")
	}
	client.mu.Lock()
	client.statusErr = nil
	client.mu.Unlock()
	d.Present()
	if d.FramePending() {
		t.Fatal("unchanged frame queued during backoff")
	}
	if calls, _, _, _ := client.snapshot(); calls != 1 {
		t.Fatalf("calls=%d during backoff", calls)
	}
	clock.Set(until)
	d.Present()
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 2)
	calls, gen, n, prefix := client.snapshot()
	if calls != 2 || gen != 3 || n != menudisplay.FrameBytes || prefix[0] != 1 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}
}

func TestMenuDisplayPauseResumeClearsBackoff(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	client.mu.Lock()
	client.statusErr = errors.New("socket missing")
	client.mu.Unlock()
	d.Present()
	waitMenuIdle(t, d, client, 0)
	if err := d.Pause(t.Context()); err != nil {
		t.Fatal(err)
	}
	d.Resume()
	before := client.statusCount()
	d.Present()
	waitMenuStatus(t, d, client, before+1)
}

func TestMenuDisplayStopRedrawsWithinProbeInterval(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	client.mu.Lock()
	client.unavailable = true
	client.mu.Unlock()
	clock.Advance(menuProbeInterval)
	before := client.statusCount()
	d.Present()
	waitMenuStatus(t, d, client, before+1)

	want := menuBackoffInitial
	for step := 0; step < 6; step++ {
		d.mu.Lock()
		got := d.backoff
		until := d.backoffUntil
		d.mu.Unlock()
		if got != want {
			t.Fatalf("step %d unavailable backoff %s, want %s", step, got, want)
		}
		if got > menuProbeInterval {
			t.Fatalf("step %d unavailable backoff %s exceeds the probe interval", step, got)
		}
		clock.Set(until.Add(-time.Millisecond))
		before = client.statusCount()
		d.Present()
		if d.FramePending() || client.statusCount() != before {
			t.Fatalf("step %d queued during unavailable backoff", step)
		}
		clock.Set(until)
		next := want * 2
		if next > menuProbeInterval {
			next = menuProbeInterval
		}
		want = next
		before = client.statusCount()
		d.Present()
		waitMenuStatus(t, d, client, before+1)
	}

	client.mu.Lock()
	client.unavailable = false
	client.generation = 11
	client.mu.Unlock()
	d.mu.Lock()
	until := d.backoffUntil
	d.mu.Unlock()
	clock.Set(until.Add(-time.Millisecond))
	beforeStatus := client.statusCount()
	d.Present()
	if d.FramePending() || client.statusCount() != beforeStatus {
		t.Fatal("stop redraw queued before the probe interval")
	}
	if calls, _, _, _ := client.snapshot(); calls != 1 {
		t.Fatal("stop redraw presented before the probe interval")
	}
	// One attempt can be rejected because Stop moved the generation.
	// That does not arm a new wait. The following Present submits.
	clock.Set(until)
	beforeStatus = client.statusCount()
	d.Present()
	waitMenuStatus(t, d, client, beforeStatus+1)
	if calls, _, _, _ := client.snapshot(); calls != 1 {
		t.Fatal("generation mismatch presented")
	}
	d.Present()
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 2)
	calls, gen, n, prefix := client.snapshot()
	if calls != 2 || gen != 11 || n != menudisplay.FrameBytes || prefix[0] != 1 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}

	// Socket loss still doubles past the probe interval.
	client.mu.Lock()
	client.statusErr = errors.New("socket missing")
	client.mu.Unlock()
	d.Clear(RGB(4, 5, 6))
	socketWant := menuBackoffInitial
	for step := 0; step < 4; step++ {
		beforeStatus = client.statusCount()
		d.Present()
		waitMenuStatus(t, d, client, beforeStatus+1)
		d.mu.Lock()
		got := d.backoff
		until := d.backoffUntil
		d.mu.Unlock()
		if got != socketWant {
			t.Fatalf("socket step %d backoff %s, want %s", step, got, socketWant)
		}
		clock.Set(until)
		next := socketWant * 2
		if next > menuBackoffCap {
			next = menuBackoffCap
		}
		socketWant = next
	}
	if socketWant <= menuProbeInterval {
		t.Fatalf("socket-missing backoff %s did not pass the probe interval", socketWant)
	}
}

func TestMenuDisplayDiscardKeepsInFlightProbe(t *testing.T) {
	gate := make(chan struct{})
	var block atomic.Bool
	client := &testMenuClient{
		generation: 4,
		ready:      make(chan struct{}, 4),
		statusSeen: make(chan uint64, 8),
	}
	client.holdStatus = func() {
		if block.Load() {
			<-gate
		}
	}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		block.Store(false)
		if !closed {
			close(gate)
		}
		d.Close()
	})
	d.SetChangeDriven(true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	block.Store(true)
	clock.Advance(menuProbeInterval)
	d.Present()
	deadline := time.Now().Add(time.Second)
	for client.statusCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("probe did not reach status")
		}
		time.Sleep(time.Millisecond)
	}
	d.Clear(RGB(8, 8, 8))
	d.Present()
	d.Clear(RGB(9, 9, 9))
	d.Present()
	d.mu.Lock()
	kept := d.probeQueued
	d.mu.Unlock()
	if !kept {
		t.Fatal("discarded non-probe frame cleared probeQueued")
	}
	closed = true
	close(gate)
	block.Store(false)
}

// The runtime gives a loss up after a reactivation that did not clear it, or
// once its bounded budget is spent, and then deliberately leaves the display
// alone. A probe that answered that sample with a complete present would
// spend from the present path the very budget the runtime withheld, or clear
// the menu package and drop the kit to splash. So the probe sends nothing
// while the flag is set. It must not answer "not lost" in scanoutLostLocked
// instead: that routes the probe into the fast path that acknowledges the
// handoff. What it must not do either is pull managed readiness down, which
// would restart the kit's software group every few minutes and reprogram the
// same dead display on each restart -- the loop the runtime gave the loss up
// to avoid.
func TestMenuDisplayProbeSendsNothingWhileRecoveryIsAbandoned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready.json")
	t.Setenv("FES_SOFTWARE_READY_FILE", path)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", "bundle")
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)
	requireSoftwareReady(t, path, true)

	// Scanout is gone and the runtime has stopped trying to bring it back.
	client.mu.Lock()
	client.live = &menudisplay.Live{RecoveryAbandoned: true}
	client.mu.Unlock()
	for i := 0; i < 2; i++ {
		statusBefore := client.statusCount()
		clock.Advance(menuProbeInterval)
		d.Present()
		waitMenuIdle(t, d, client, 1)
		if calls, _, _, _ := client.snapshot(); calls != 1 {
			t.Fatalf("probe %d presented into an abandoned loss, calls=%d", i, calls)
		}
		if client.statusCount() != statusBefore+1 {
			t.Fatalf("probe %d stopped reading status: %d, want %d", i, client.statusCount(), statusBefore+1)
		}
		// The frame the runtime acknowledged is still acknowledged, so the
		// supervisor's marker stays exactly where it was.
		requireSoftwareReady(t, path, true)
	}

	// The next budget window reopens: the loss is eligible for recovery
	// again, the flag clears, and ordinary presenting resumes at once.
	client.mu.Lock()
	client.live = &menudisplay.Live{}
	client.mu.Unlock()
	clock.Advance(menuProbeInterval)
	d.Present()
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 2)
	if calls, _, n, _ := client.snapshot(); calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("cleared flag did not resume presenting, calls=%d bytes=%d", calls, n)
	}
}

// An unchanged frame reaches the runtime as a probe only while no backoff is
// armed. The present after a backoff expires is a whole frame, and the
// failure that armed that backoff is also what can hide the abandoned verdict
// from the launcher until the socket answers again -- so the verdict has to
// be read from that present's own status, in the worker, rather than from
// whatever the launcher last heard. Without that, a failing present re-arms
// the backoff and the launcher sends the whole idle frame into the abandoned
// loss once per backoff cap, forever.
func TestMenuDisplayRecoveryPresentStopsAtAnAbandonedLoss(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)

	// A socket hiccup fails the probe and arms the backoff. The launcher
	// hears nothing about the display while it lasts.
	client.mu.Lock()
	client.statusErr = errors.New("socket missing")
	client.mu.Unlock()
	clock.Advance(menuProbeInterval)
	d.Present()
	waitMenuIdle(t, d, client, 1)
	d.mu.Lock()
	until := d.backoffUntil
	d.mu.Unlock()
	if until.IsZero() {
		t.Fatal("failed probe armed no backoff")
	}

	// The socket comes back, and by then the runtime has given the loss up.
	client.mu.Lock()
	client.statusErr = nil
	client.live = &menudisplay.Live{RecoveryAbandoned: true}
	client.mu.Unlock()
	for i := 0; i < 3; i++ {
		statusBefore := client.statusCount()
		clock.Set(until)
		d.Present()
		// The whole frame is queued and the worker reads status for it; the
		// refusal is this present's own verdict on that status, so wait for
		// the read rather than for a submission count that does not move.
		waitMenuStatus(t, d, client, statusBefore+1)
		if calls, _, _, _ := client.snapshot(); calls != 1 {
			t.Fatalf("recovery %d presented into an abandoned loss, calls=%d", i, calls)
		}
		if client.statusCount() != statusBefore+1 {
			t.Fatalf("recovery %d read status %d times, want one", i, client.statusCount()-statusBefore)
		}
		// Refusing to send is not a failure, so nothing arms a new backoff:
		// the launcher falls back to the probe interval it keeps running on.
		d.mu.Lock()
		armed, probeDue := !d.backoffUntil.IsZero(), d.probeDueLocked(clock.Now())
		d.mu.Unlock()
		if armed {
			t.Fatalf("recovery %d armed a backoff", i)
		}
		if probeDue {
			t.Fatalf("recovery %d left a probe due immediately", i)
		}
		until = clock.Now().Add(menuProbeInterval)
	}

	// The display comes back and ordinary presenting resumes with a complete
	// frame, which is the only thing that was waiting on the flag.
	client.mu.Lock()
	client.live = &menudisplay.Live{}
	client.mu.Unlock()
	clock.Set(until)
	d.Present()
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 2)
	if calls, _, n, _ := client.snapshot(); calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("cleared flag did not resume presenting, calls=%d bytes=%d", calls, n)
	}
}

func TestMenuDisplayChangeDrivenOffIgnoresBackoff(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, false)
	client.mu.Lock()
	client.statusErr = errors.New("socket missing")
	client.mu.Unlock()
	d.Present()
	waitMenuStatus(t, d, client, 1)
	d.Present()
	waitMenuStatus(t, d, client, 2)
	if calls, _, _, _ := client.snapshot(); calls != 0 {
		t.Fatalf("present calls %d", calls)
	}
}

// The live sample and this client's newest present describe two different
// instants. The runtime reads the display about once a second; a present
// happens whenever the UI changes, and it moves the committed frame on. So
// the sample a probe reads after any present carries the sequence from
// before it, and subtracting the two here called a perfectly healthy display
// lost: the probe sent a complete frame to recover from it, that present
// moved the sequence again, and the next probe was behind once more. One
// ordinary UI change and the menu uploaded a whole frame every second for
// the rest of the kit's uptime. The verdict is the runtime's, made where
// both numbers are read together, and a present drops it rather than letting
// a stale one speak for a frame it never saw. Three probes in a row, because
// one proves nothing about a loop that feeds itself: each turn rebuilds the
// sample from whatever is committed now, so a probe that did present would
// leave the next one behind again.
func TestMenuDisplayProbeDoesNotLoopOnAPrePresentLiveSample(t *testing.T) {
	for _, verdict := range []string{menudisplay.ScanoutFrameUnknown, menudisplay.ScanoutFrameMatching} {
		t.Run(verdict, func(t *testing.T) {
			d, client := newChangeDrivenMenu(t, 3, true)
			clock := newMenuClock()
			d.now = clock.Now
			d.Clear(RGB(1, 2, 3))
			presentMenu(t, d, client)
			waitMenuIdle(t, d, client, 1)

			for probe := 0; probe < 3; probe++ {
				client.mu.Lock()
				// What the runtime published before the committed frame
				// moved: the register as the last health check read it, one
				// behind, and either no verdict for the frame now on the
				// wire or the verdict that check made for the one before it.
				client.live = &menudisplay.Live{Enabled: true,
					DisplayedSequence: client.displayed - 1, ScanoutFrame: verdict}
				client.mu.Unlock()

				clock.Advance(menuProbeInterval)
				d.Present()
				// A present here would advance the sequence, leave the next
				// turn's sample behind again, and never stop.
				waitMenuIdle(t, d, client, 1)
			}
			if err := d.LastError(); err != nil {
				t.Fatalf("a lagging sample reported %v", err)
			}
		})
	}
}

// The verdict is still how a genuine loss reaches the launcher: scanout is
// not showing the committed frame, the runtime says so, and the probe sends
// a complete frame rather than acknowledging an unchanged one.
func TestMenuDisplayProbeForcesPresentOnAMismatchedVerdict(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuIdle(t, d, client, 1)
	client.mu.Lock()
	// Healthy in every other respect, and the sampled sequence even agrees
	// with what this client's present returned. The verdict is the loss.
	client.live = &menudisplay.Live{Enabled: true, DisplayedSequence: client.displayed,
		ScanoutFrame: menudisplay.ScanoutFrameMismatched}
	client.mu.Unlock()

	clock.Advance(menuProbeInterval)
	d.Present()
	waitMenuIdle(t, d, client, 2)
	if calls, _, n, _ := client.snapshot(); calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("a mismatched verdict was acknowledged, calls=%d bytes=%d", calls, n)
	}
}
