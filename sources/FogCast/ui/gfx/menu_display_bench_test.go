package gfx

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

func BenchmarkMenuDisplayChangeDetect(b *testing.B) {
	client := &testMenuClient{generation: 1, ready: make(chan struct{}, 1)}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(d.Close)
	d.SetChangeDriven(true)
	frozen := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return frozen }
	d.Clear(RGB(12, 34, 56))
	d.Present()
	deadline := time.Now().Add(time.Second)
	for client.snapshotCalls() < 1 || d.FramePending() {
		if time.Now().After(deadline) {
			b.Fatal("worker did not finish the seed frame")
		}
	}
	before := client.statusCount()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Present()
	}
	b.StopTimer()
	if client.statusCount() != before || client.snapshotCalls() != 1 {
		b.Fatalf("worker ran during unchanged bench: status %d calls %d", client.statusCount(), client.snapshotCalls())
	}
}

func BenchmarkMenuDisplayChangeDetectChanged(b *testing.B) {
	block := make(chan struct{})
	client := &testMenuClient{generation: 1, ready: make(chan struct{}, 1), presentBlock: block}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		close(block)
		d.Close()
	})
	d.SetChangeDriven(true)
	frozen := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return frozen }
	pix := d.Software.Framebuffer().Pix
	if len(pix) != menudisplay.FrameBytes {
		b.Fatalf("frame bytes %d", len(pix))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pix[len(pix)-1] ^= 1
		d.Present()
	}
}

func (c *testMenuClient) snapshotCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func BenchmarkMenuDisplayRevision(b *testing.B) {
	client := &testMenuClient{generation: 1, ready: make(chan struct{}, 1)}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(d.Close)
	d.SetChangeDriven(true)
	frozen := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return frozen }
	d.Clear(RGB(12, 34, 56))
	d.PresentRevision(1)
	deadline := time.Now().Add(time.Second)
	for client.snapshotCalls() < 1 || d.FramePending() {
		if time.Now().After(deadline) {
			b.Fatal("worker did not finish seed frame")
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.PresentRevision(1)
	}
	b.StopTimer()
	if client.snapshotCalls() != 1 {
		b.Fatal("unchanged revision submitted")
	}
}

func BenchmarkMenuDisplayRevisionChanged(b *testing.B) {
	block := make(chan struct{})
	client := &testMenuClient{generation: 1, ready: make(chan struct{}, 1), presentBlock: block}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { close(block); d.Close() })
	d.SetChangeDriven(true)
	frozen := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return frozen }
	// One frame in flight, one queued and a third storage slot being reused.
	d.PresentRevision(1)
	select {
	case <-client.ready:
	case <-time.After(time.Second):
		b.Fatal("worker did not start")
	}
	for i := uint64(2); i < 6; i++ {
		d.PresentRevision(i)
	}
	pix := d.Framebuffer().Pix
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pix[len(pix)-1] ^= 1
		d.PresentRevision(uint64(i + 6))
	}
}
