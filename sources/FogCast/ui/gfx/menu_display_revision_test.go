package gfx

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

func TestMenuDisplayRevisionProbeRetryAndResume(t *testing.T) {
	d, client := newChangeDrivenMenu(t, 3, true)
	clock := newMenuClock()
	d.now = clock.Now
	d.Clear(RGB(1, 2, 3))
	d.PresentRevision(1)
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 1)
	d.PresentRevision(1)
	if d.FramePending() {
		t.Fatal("unchanged revision queued")
	}
	client.mu.Lock()
	client.generation = 4
	client.mu.Unlock()
	clock.Advance(menuProbeInterval)
	d.PresentRevision(1)
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 2)
	client.mu.Lock()
	client.fail = true
	client.mu.Unlock()
	d.Clear(RGB(8, 9, 10))
	d.PresentRevision(2)
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 3)
	client.mu.Lock()
	client.fail = false
	client.mu.Unlock()
	clock.Advance(menuBackoffInitial)
	d.PresentRevision(2)
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 4)
	if err := d.Pause(t.Context()); err != nil {
		t.Fatal(err)
	}
	d.Resume()
	d.PresentRevision(2)
	presentMenuWait(t, client)
	waitMenuIdle(t, d, client, 5)
	_, gen, _, pixel := client.snapshot()
	if gen != 4 || !bytes.Equal(pixel, []byte{8, 9, 10, 255}) {
		t.Fatalf("gen %d pixel %v", gen, pixel)
	}
}

// A worker keeps its bytes immutable even as queued frames are replaced and
// pooled storage is reused. Compare the whole frame after the producer advances.
type heldPixelsClient struct {
	*testMenuClient
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	changed bool
	calls   int
}

func (c *heldPixelsClient) Present(_ context.Context, gen uint64, pix []byte) (menudisplay.Result, error) {
	before := append([]byte(nil), pix...)
	c.mu.Lock()
	c.calls++
	first := c.calls == 1
	c.mu.Unlock()
	if first {
		close(c.entered)
		<-c.release
	}
	c.mu.Lock()
	c.changed = c.changed || !bytes.Equal(before, pix)
	c.mu.Unlock()
	return menudisplay.Result{Generation: gen}, nil
}

func TestMenuDisplayPooledFramesRemainImmutable(t *testing.T) {
	client := &heldPixelsClient{testMenuClient: &testMenuClient{generation: 1}, entered: make(chan struct{}), release: make(chan struct{})}
	d, err := NewMenuDisplayWithPresenter(client)
	if err != nil {
		t.Fatal(err)
	}
	var gateOnce sync.Once
	defer func() { gateOnce.Do(func() { close(client.release) }); d.Close() }()
	d.SetChangeDriven(true)
	d.Clear(RGB(1, 1, 1))
	d.PresentRevision(1)
	select {
	case <-client.entered:
	case <-time.After(time.Second):
		t.Fatal("worker not entered")
	}
	for i := 2; i < 40; i++ {
		d.Clear(RGB(uint8(i), 2, 3))
		d.PresentRevision(uint64(i))
	}
	gateOnce.Do(func() { close(client.release) })
	deadline := time.Now().Add(time.Second)
	for d.FramePending() {
		if time.Now().After(deadline) {
			t.Fatal("worker did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	client.mu.Lock()
	changed := client.changed
	client.mu.Unlock()
	if changed {
		t.Fatal("in-flight immutable pixels overwritten")
	}
	d.mu.Lock()
	latest := d.submitted != nil && d.submitted.pixels[0] == 39
	free := len(d.freeBuffers)
	d.mu.Unlock()
	if !latest {
		t.Fatal("latest frame lost")
	}
	if free > 3 {
		t.Fatal("unbounded free frame pool")
	}
}
