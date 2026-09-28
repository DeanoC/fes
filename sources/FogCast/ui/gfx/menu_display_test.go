package gfx

import (
	"context"
	"errors"
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
	fail         bool
	ready        chan struct{}
	statusSeen   chan uint64
	presentBlock <-chan struct{}
}

func (c *testMenuClient) Status(context.Context) (menudisplay.Status, error) {
	c.mu.Lock()
	generation := c.generation
	underflows := c.underflows
	c.mu.Unlock()
	if c.statusSeen != nil {
		select {
		case c.statusSeen <- generation:
		default:
		}
	}
	return menudisplay.Status{Available: true, Generation: generation, Width: 1280, Height: 720, Stride: 5120, ByteCount: menudisplay.FrameBytes, SlotBytes: menudisplay.SlotBytes, Underflows: underflows}, nil
}
func (c *testMenuClient) Present(_ context.Context, generation uint64, pixels []byte) (menudisplay.Result, error) {
	c.mu.Lock()
	c.calls++
	c.pixels = append([]byte(nil), pixels[:4]...)
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
