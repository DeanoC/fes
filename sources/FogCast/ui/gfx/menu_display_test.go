package gfx

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

type testMenuClient struct {
	mu           sync.Mutex
	generation   uint64
	underflows   uint64
	pixels       []byte
	calls        int
	lastGen      uint64
	lastLen      int
	fail         bool
	unavailable  bool
	statusErr    error
	statusCalls  int
	ready        chan struct{}
	statusSeen   chan uint64
	presentBlock <-chan struct{}
}

func (c *testMenuClient) Status(context.Context) (menudisplay.Status, error) {
	c.mu.Lock()
	c.statusCalls++
	generation := c.generation
	underflows := c.underflows
	unavailable := c.unavailable
	statusErr := c.statusErr
	c.mu.Unlock()
	if c.statusSeen != nil {
		select {
		case c.statusSeen <- generation:
		default:
		}
	}
	if statusErr != nil {
		return menudisplay.Status{}, statusErr
	}
	return menudisplay.Status{Available: !unavailable, Generation: generation, Width: 1280, Height: 720, Stride: 5120, ByteCount: menudisplay.FrameBytes, SlotBytes: menudisplay.SlotBytes, Underflows: underflows}, nil
}
func (c *testMenuClient) Present(_ context.Context, generation uint64, pixels []byte) (menudisplay.Result, error) {
	c.mu.Lock()
	c.calls++
	c.lastGen = generation
	c.lastLen = len(pixels)
	n := min(4, len(pixels))
	c.pixels = append([]byte(nil), pixels[:n]...)
	fail := c.fail
	c.mu.Unlock()
	select {
	case c.ready <- struct{}{}:
	default:
	}
	if c.presentBlock != nil {
		<-c.presentBlock
	}
	if fail {
		return menudisplay.Result{}, errors.New("stale menu")
	}
	return menudisplay.Result{Generation: generation, DisplayedSequence: uint64(c.calls)}, nil
}

func TestMenuDisplayPauseDrainsInflightAndDropsQueuedFrames(t *testing.T) {
	block := make(chan struct{})
	client := &testMenuClient{generation: 3, ready: make(chan struct{}, 3), presentBlock: block}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("first frame did not begin")
	}
	d.Present() // A queued idle frame must not cross the launch boundary.
	paused := make(chan error, 1)
	go func() { paused <- d.Pause(context.Background()) }()
	select {
	case err := <-paused:
		t.Fatalf("pause returned with a commit in flight: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(block)
	select {
	case err := <-paused:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("pause did not drain the commit")
	}
	d.Present() // Paused submissions are discarded as well.
	select {
	case <-client.ready:
		t.Fatal("menu frame committed while paused")
	case <-time.After(30 * time.Millisecond):
	}
	d.Resume()
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("menu did not resume")
	}
	client.mu.Lock()
	calls := client.calls
	client.mu.Unlock()
	if calls != 2 {
		t.Fatalf("commits=%d, want one before and one after pause", calls)
	}
}

func TestMenuDisplayRendersAndRetriesAfterFailure(t *testing.T) {
	client := &testMenuClient{generation: 3, fail: true, ready: make(chan struct{}, 3)}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.Config().Width != 1280 || d.Config().Height != 720 {
		t.Fatal("wrong menu geometry")
	}
	d.BeginFrame()
	d.Clear(RGB(12, 34, 56))
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("first frame not sent")
	}
	client.mu.Lock()
	got := append([]byte(nil), client.pixels...)
	client.fail = false
	client.mu.Unlock()
	if len(got) != 4 || got[0] != 12 || got[1] != 34 || got[2] != 56 {
		t.Fatalf("pixels=%v", got)
	}
	d.BeginFrame()
	d.Clear(RGB(80, 90, 100))
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("retry not sent")
	}
	client.mu.Lock()
	calls := client.calls
	got = append([]byte(nil), client.pixels...)
	client.mu.Unlock()
	if calls != 2 || got[0] != 80 {
		t.Fatalf("calls=%d pixels=%v", calls, got)
	}
}

func TestMenuDisplayToleratesTransientUnderflow(t *testing.T) {
	client := &testMenuClient{generation: 3, underflows: 40, ready: make(chan struct{}, 1)}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("transient underflow blocked presentation")
	}
	if client.calls != 1 {
		t.Fatalf("commits=%d", client.calls)
	}
}

func TestMenuDisplayRejectsUnderflowAboveCap(t *testing.T) {
	client := &testMenuClient{
		generation: 3,
		underflows: menudisplay.TransientUnderflowCap + 1,
		ready:      make(chan struct{}, 1),
		statusSeen: make(chan uint64, 1),
	}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Present()
	select {
	case <-client.statusSeen:
	case <-time.After(time.Second):
		t.Fatal("status was not read")
	}
	select {
	case <-client.ready:
		t.Fatal("underflow above the cap was presented")
	case <-time.After(50 * time.Millisecond):
	}
	if client.calls != 0 || d.LastError() == nil || d.LastError().Error() != "menu scanout underflow" {
		t.Fatalf("calls=%d err=%v", client.calls, d.LastError())
	}
}

func TestMenuDisplayDropsQueuedFrameAfterGenerationChange(t *testing.T) {
	client := &testMenuClient{generation: 3, ready: make(chan struct{}, 3), statusSeen: make(chan uint64, 3)}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Clear(RGB(1, 2, 3))
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("initial frame not sent")
	}
	<-client.statusSeen
	client.mu.Lock()
	client.generation = 4
	client.mu.Unlock()
	d.Clear(RGB(4, 5, 6))
	d.Present()
	select {
	case <-client.statusSeen:
	case <-time.After(time.Second):
		t.Fatal("new generation not checked")
	}
	select {
	case <-client.ready:
		t.Fatal("stale frame crossed generation")
	case <-time.After(50 * time.Millisecond):
	}
	d.Clear(RGB(7, 8, 9))
	d.Present()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("fresh frame not sent")
	}
}

func TestMenuDisplayReportsMenuGeometry(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, false)
	cfg := d.Config()
	if d.BackendName() != BackendMenuDisplay || cfg.Width != 1280 || cfg.Height != 720 || cfg.Stride != 5120 || cfg.BPP != 32 {
		t.Fatalf("backend %q config %+v", d.BackendName(), cfg)
	}
	if cfg.Width != menudisplay.Width || cfg.Height != menudisplay.Height || cfg.Stride != menudisplay.Stride {
		t.Fatalf("config %+v", cfg)
	}
	d.Clear(RGB(12, 34, 56))
	presentMenu(t, d, client)
	calls, gen, n, prefix := client.snapshot()
	if calls != 1 || gen != 3 || n != menudisplay.FrameBytes || len(prefix) != 4 || prefix[0] != 12 || prefix[1] != 34 || prefix[2] != 56 || prefix[3] != 255 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}
}

func TestMenuDisplayChangeDrivenOffSubmitsIdenticalFrames(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, false)
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	presentMenu(t, d, client)
	calls, _, n, _ := client.snapshot()
	if calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("calls=%d bytes=%d", calls, n)
	}
}

func TestMenuDisplayChangeDrivenSkipsIdenticalFrame(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuSubmitted(t, d, 3)
	d.Present()
	waitMenuIdle(t, d, client, 1)
}

func TestMenuDisplayChangeDrivenSubmitsChangedFrame(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuSubmitted(t, d, 3)
	d.Clear(RGB(4, 5, 6))
	presentMenu(t, d, client)
	calls, gen, n, prefix := client.snapshot()
	if calls != 2 || gen != 3 || n != menudisplay.FrameBytes || prefix[0] != 4 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}
}

func TestMenuDisplayChangeDrivenResubmitsAfterGenerationChange(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuSubmitted(t, d, 3)
	drainMenuStatus(client)

	// An unchanged frame does not poll, so it cannot observe the new generation.
	client.mu.Lock()
	client.generation = 4
	client.mu.Unlock()
	d.Present()
	waitMenuIdle(t, d, client, 1)
	select {
	case gen := <-client.statusSeen:
		t.Fatalf("unchanged frame polled status at generation %d", gen)
	default:
	}

	// A different frame is submitted, rejected as stale, and forgotten.
	d.Clear(RGB(4, 5, 6))
	d.Present()
	select {
	case <-client.statusSeen:
	case <-time.After(time.Second):
		t.Fatal("stale frame was not checked")
	}
	waitMenuIdle(t, d, client, 1)
	waitMenuForgotten(t, d)
	if err := d.LastError(); err == nil || !strings.Contains(err.Error(), "generation changed") {
		t.Fatalf("last error %v", err)
	}
	if calls, _, _, _ := client.snapshot(); calls != 1 {
		t.Fatalf("stale frame reached the client, calls=%d", calls)
	}

	// The same pixels are submitted again and presented at the new generation.
	d.Present()
	presentMenuWait(t, client)
	calls, gen, n, prefix := client.snapshot()
	if calls != 2 || gen != 4 || n != menudisplay.FrameBytes || prefix[0] != 4 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}
}

func TestMenuDisplayChangeDrivenResubmitsAfterPresentError(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuSubmitted(t, d, 3)
	client.mu.Lock()
	client.fail = true
	client.mu.Unlock()
	d.Clear(RGB(8, 9, 10))
	d.Present()
	presentMenuWait(t, client)
	waitMenuForgotten(t, d)
	waitMenuIdle(t, d, client, 2)
	client.mu.Lock()
	client.fail = false
	client.mu.Unlock()
	// A present failure backs off. The same pixels are not retried until it expires.
	d.Present()
	if d.FramePending() {
		t.Fatal("present queued during backoff")
	}
	if calls, _, _, _ := client.snapshot(); calls != 2 {
		t.Fatalf("calls=%d during backoff", calls)
	}
	clock.Advance(menuBackoffInitial)
	d.Present()
	presentMenuWait(t, client)
	calls, gen, n, prefix := client.snapshot()
	if calls != 3 || gen != 3 || n != menudisplay.FrameBytes || prefix[0] != 8 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}
}

func TestMenuDisplayChangeDrivenResubmitsAfterPause(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuSubmitted(t, d, 3)
	if err := d.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.Resume()
	d.Present()
	presentMenuWait(t, client)
	calls, _, n, _ := client.snapshot()
	if calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("calls=%d bytes=%d", calls, n)
	}
}

func TestMenuDisplayChangeDrivenResubmitsAfterUnavailable(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	presentMenu(t, d, client)
	waitMenuSubmitted(t, d, 3)
	drainMenuStatus(client)
	client.mu.Lock()
	client.unavailable = true
	client.mu.Unlock()
	d.Clear(RGB(7, 8, 9))
	d.Present()
	select {
	case <-client.statusSeen:
	case <-time.After(time.Second):
		t.Fatal("unavailable status was not read")
	}
	waitMenuIdle(t, d, client, 1)
	waitMenuForgotten(t, d)
	client.mu.Lock()
	client.unavailable = false
	client.mu.Unlock()
	// Unavailable arms backoff, so the retry waits out the first interval.
	statusBefore := client.statusCount()
	d.Present()
	if d.FramePending() || client.statusCount() != statusBefore {
		t.Fatal("present queued during backoff")
	}
	clock.Advance(menuBackoffInitial)
	d.Present()
	presentMenuWait(t, client)
	calls, gen, n, prefix := client.snapshot()
	if calls != 2 || gen != 3 || n != menudisplay.FrameBytes || prefix[0] != 7 {
		t.Fatalf("calls=%d gen=%d bytes=%d prefix=%v", calls, gen, n, prefix)
	}
}

func newChangeDrivenMenu(t *testing.T, generation uint64, changeDriven bool) (*MenuDisplay, *testMenuClient) {
	t.Helper()
	client := &testMenuClient{generation: generation, ready: make(chan struct{}, 4), statusSeen: make(chan uint64, 4)}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	if changeDriven {
		d.SetChangeDriven(true)
	}
	return d, client
}

func (c *testMenuClient) snapshot() (calls int, gen uint64, n int, prefix []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.lastGen, c.lastLen, append([]byte(nil), c.pixels...)
}

func (c *testMenuClient) statusCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusCalls
}

func presentMenu(t *testing.T, d *MenuDisplay, client *testMenuClient) {
	t.Helper()
	d.Present()
	presentMenuWait(t, client)
}

func presentMenuWait(t *testing.T, client *testMenuClient) {
	t.Helper()
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		t.Fatal("frame not presented")
	}
}

// waitMenuIdle waits until the worker has finished every queued frame, then
// checks the client Present count. flight == nil and an empty queue are not
// enough on their own: Present bumps nextSeq before the send, and the worker
// can dequeue that frame before it publishes flight. Status runs only after
// flight is published, once per queued non-probe Present, so the status
// count has to catch nextSeq before idle is accepted. A probe calls Status
// without bumping nextSeq; probeQueued stays set until that check finishes,
// so an in-flight probe is not idle.
func waitMenuIdle(t *testing.T, d *MenuDisplay, client *testMenuClient, wantCalls int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		d.mu.Lock()
		idle := d.flight == nil && len(d.frames) == 0 && !d.probeQueued
		seq := d.nextSeq
		d.mu.Unlock()
		if idle && client.statusCount() >= int(seq) {
			break
		}
		if time.Now().After(deadline) {
			calls, _, _, _ := client.snapshot()
			t.Fatalf("menu worker did not become idle (calls=%d, want %d)", calls, wantCalls)
		}
		time.Sleep(time.Millisecond)
	}
	if calls, _, _, _ := client.snapshot(); calls != wantCalls {
		t.Fatalf("calls=%d, want %d", calls, wantCalls)
	}
}

func drainMenuStatus(client *testMenuClient) {
	for {
		select {
		case <-client.statusSeen:
		default:
			return
		}
	}
}

func waitMenuSubmitted(t *testing.T, d *MenuDisplay, generation uint64) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		d.mu.Lock()
		ok := d.submitted != nil && d.submitted.generation == generation
		d.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("submitted generation never reached %d", generation)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func waitMenuForgotten(t *testing.T, d *MenuDisplay) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		d.mu.Lock()
		cleared := d.submitted == nil
		d.mu.Unlock()
		if cleared {
			return
		}
		select {
		case <-deadline:
			t.Fatal("submitted frame was not forgotten")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
