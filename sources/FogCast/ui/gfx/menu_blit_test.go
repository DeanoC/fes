package gfx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

// blitTestClient is a runtime whose grant may include the blitter. It draws
// list frames into slot the way the blitter does: B, G, R, X with X = 0.
type blitTestClient struct {
	mu         sync.Mutex
	generation uint64
	session    bool
	packageID  string
	blitter    bool
	// minor is the blitter's menu-blitter minor.
	minor int
	// tooLarge refuses lists longer than this, as the client refuses a
	// frame the blitter cannot hold.
	tooLarge int
	grants   int
	lists    [][]menudisplay.BlitOp
	cpu      [][]byte
	slot     []byte
	seq      uint64
}

func newBlitTestClient(blitter bool) *blitTestClient {
	client := &blitTestClient{generation: 3, blitter: blitter, slot: make([]byte, menudisplay.FrameBytes)}
	if blitter {
		client.minor = menudisplay.MinBlitterMinor
	}
	return client
}

func (c *blitTestClient) Status(context.Context) (menudisplay.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return menudisplay.Status{Available: true, Generation: c.generation, Session: c.session, PackageID: c.packageID, Width: 1280, Height: 720,
		Stride: 5120, ByteCount: menudisplay.FrameBytes, SlotBytes: menudisplay.SlotBytes, DisplayedSequence: c.seq}, nil
}

func (c *blitTestClient) Present(_ context.Context, generation uint64, pixels []byte) (menudisplay.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cpu = append(c.cpu, bytes.Clone(pixels))
	c.seq++
	return menudisplay.Result{Generation: generation, DisplayedSequence: c.seq}, nil
}

func (c *blitTestClient) Grant(context.Context, uint64) (menudisplay.Grant, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.grants++
	return menudisplay.Grant{Blitter: c.blitter, BlitterMinor: c.minor}, nil
}

func (c *blitTestClient) PresentBlit(_ context.Context, generation uint64, frame menudisplay.BlitFrame) (menudisplay.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.blitter {
		return menudisplay.Result{}, errors.New("menu grant has no blitter")
	}
	if c.tooLarge > 0 && len(frame.Ops) > c.tooLarge {
		return menudisplay.Result{}, menudisplay.ErrBlitTooLarge
	}
	for i, op := range frame.Ops {
		if op.W < 1 || op.H < 1 || op.X < 0 || op.Y < 0 || op.X+op.W > 1280 || op.Y+op.H > 720 {
			return menudisplay.Result{}, fmt.Errorf("op %d outside the frame: %+v", i, op)
		}
		reads := op.Kind == menudisplay.BlitCopy || op.Kind == menudisplay.BlitBlend || op.Kind == menudisplay.BlitExpand
		if s := op.Source; reads &&
			(s == nil || op.SourceX < 0 || op.SourceY < 0 || op.SourceX+op.W > s.W || op.SourceY+op.H > s.H) {
			return menudisplay.Result{}, fmt.Errorf("op %d reads outside its source: %+v", i, op)
		}
		if reads && op.Source.Coverage != (op.Kind == menudisplay.BlitExpand) {
			return menudisplay.Result{}, fmt.Errorf("op %d reads the wrong format: %+v", i, op)
		}
		if op.Kind == menudisplay.BlitExpand && c.minor < menudisplay.MinBlitterMinor {
			return menudisplay.Result{}, fmt.Errorf("op %d: blitter 1.%d has no expand", i, c.minor)
		}
	}
	blitInto(c.slot, frame.Ops)
	c.lists = append(c.lists, frame.Ops)
	c.seq++
	return menudisplay.Result{Generation: generation, DisplayedSequence: c.seq}, nil
}

// blitInto is the blitter: fills and copies replace, blends are
// gfx.blendOver, expands blend the colour at A·m/255, and X is always
// written 0.
func blitInto(slot []byte, ops []menudisplay.BlitOp) {
	for _, op := range ops {
		for y := 0; y < op.H; y++ {
			for x := 0; x < op.W; x++ {
				d := slot[(op.Y+y)*menudisplay.Stride+(op.X+x)*4:]
				var r, g, b, a uint32
				if op.Kind == menudisplay.BlitFill || op.Kind == menudisplay.BlitFillBlend || op.Kind == menudisplay.BlitExpand {
					a, r, g, b = op.Color>>24, op.Color>>16&0xff, op.Color>>8&0xff, op.Color&0xff
					if op.Kind == menudisplay.BlitExpand {
						a = a * uint32(op.Source.Pix[(op.SourceY+y)*op.Source.Stride+op.SourceX+x]) / 255
					}
				} else {
					s := op.Source.Pix[(op.SourceY+y)*op.Source.Stride+(op.SourceX+x)*4:]
					r, g, b, a = uint32(s[0]), uint32(s[1]), uint32(s[2]), uint32(s[3])
				}
				if op.Kind == menudisplay.BlitFill || op.Kind == menudisplay.BlitCopy || a == 255 {
					d[0], d[1], d[2] = byte(b), byte(g), byte(r)
				} else if a != 0 {
					d[0] = byte((b*a + uint32(d[0])*(255-a)) / 255)
					d[1] = byte((g*a + uint32(d[1])*(255-a)) / 255)
					d[2] = byte((r*a + uint32(d[2])*(255-a)) / 255)
				}
				d[3] = 0
			}
		}
	}
}

func (c *blitTestClient) lastList() []menudisplay.BlitOp {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.lists) == 0 {
		return nil
	}
	return c.lists[len(c.lists)-1]
}

func (c *blitTestClient) counts() (lists, cpu int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.lists), len(c.cpu)
}

// sameRGB reports whether a blitter slot shows what Software drew.
func sameRGB(slot, rgba []byte) error {
	for i := 0; i < len(rgba); i += 4 {
		if slot[i] != rgba[i+2] || slot[i+1] != rgba[i+1] || slot[i+2] != rgba[i] {
			p := i / 4
			return fmt.Errorf("pixel %d,%d: blitter % x, software % x", p%1280, p/1280, slot[i:i+3], rgba[i:i+3])
		}
	}
	return nil
}

func waitIdle(t *testing.T, d *MenuDisplay) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

// pair draws the same calls on a Software device and a MenuDisplay.
type pair struct {
	sw  *Software
	d   *MenuDisplay
	tex [][2]Texture
}

func (p *pair) each(f func(Device)) { f(p.sw); f(p.d) }

func randomImage(r *rand.Rand, w, h int, opaque bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(r.IntN(256))
	}
	for i := 3; i < len(img.Pix); i += 4 {
		switch {
		case opaque:
			img.Pix[i] = 255
		case r.IntN(4) == 0:
			img.Pix[i] = 0
		case r.IntN(4) == 0:
			img.Pix[i] = 255
		}
	}
	return img
}

func randomCoord(r *rand.Rand, span int) float32 {
	v := float32(r.IntN(span+200) - 100)
	if r.IntN(3) == 0 {
		v += float32(r.IntN(8)) / 8
	}
	return v
}

func randomColor(r *rand.Rand) Color {
	c := RGBA(uint8(r.IntN(256)), uint8(r.IntN(256)), uint8(r.IntN(256)), uint8(r.IntN(256)))
	switch r.IntN(3) {
	case 0:
		c.A = 255
	case 1:
		c.A = 0
	}
	return c
}

func (p *pair) randomCall(r *rand.Rand) {
	switch r.IntN(11) {
	case 0:
		c := randomColor(r)
		p.each(func(d Device) { d.Clear(c) })
	case 1, 2:
		rect := Rect{X: randomCoord(r, 1280), Y: randomCoord(r, 720), W: randomCoord(r, 400), H: randomCoord(r, 300)}
		c := randomColor(r)
		p.each(func(d Device) { d.FillRect(rect, c) })
	case 3:
		mode := BlendMode(r.IntN(2))
		p.each(func(d Device) { d.SetBlend(mode) })
	case 4, 5, 6:
		t := p.tex[r.IntN(len(p.tex))]
		w, h := float32(t[0].Width()), float32(t[0].Height())
		var src *Rect
		if r.IntN(2) == 0 {
			src = &Rect{X: float32(r.IntN(int(w))), Y: float32(r.IntN(int(h))), W: 1 + float32(r.IntN(int(w))), H: 1 + float32(r.IntN(int(h)))}
			if r.IntN(3) == 0 {
				src.X += 0.25
			}
			w, h = src.W, src.H
		}
		dst := Rect{X: randomCoord(r, 1280), Y: randomCoord(r, 720), W: w, H: h}
		if r.IntN(2) == 0 {
			dst.W, dst.H = 1+float32(r.IntN(500)), 1+float32(r.IntN(300))
		} else {
			dst.X, dst.Y = float32(int(dst.X)), float32(int(dst.Y))
		}
		p.sw.Draw(t[0], src, dst)
		p.d.Draw(t[1], src, dst)
	case 7, 8:
		x, y := int(randomCoord(r, 1280)), int(randomCoord(r, 720))
		text := []string{"Home", "Mushroom Kingdom", "Ag", "ZX81 1.5.0"}[r.IntN(4)]
		size, weight, c := 10+r.IntN(30), Weight(r.IntN(2)), randomColor(r)
		p.each(func(d Device) { d.DrawTextWeight(x, y, text, size, weight, c) })
	case 9:
		x, y := int(randomCoord(r, 1280)), int(randomCoord(r, 720))
		scale := r.IntN(4)
		p.each(func(d Device) { d.DebugText(x, y, "FPS 30 ~", scale) })
	case 10:
		x, y := int(randomCoord(r, 1280)), int(randomCoord(r, 720))
		m, all, _ := UIText("Mushroom Kingdom", 10+r.IntN(30), Weight(r.IntN(2)), 0)
		src := image.Rect(r.IntN(all.Dx()), r.IntN(all.Dy()), all.Dx()-r.IntN(all.Dx()), all.Dy()-r.IntN(all.Dy()))
		c := randomColor(r)
		p.each(func(d Device) { d.DrawMask(m, src, x, y, c) })
	}
}

func newPair(t *testing.T, client MenuPresenter, r *rand.Rand) *pair {
	t.Helper()
	sw, err := NewSoftware(1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewMenuDisplayWithPresenter(client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	p := &pair{sw: sw, d: d}
	for i := 0; i < 6; i++ {
		img := randomImage(r, 1+r.IntN(200), 1+r.IntN(120), i%2 == 0)
		a, err := sw.CreateRGBA(img)
		if err != nil {
			t.Fatal(err)
		}
		b, err := d.CreateRGBA(img)
		if err != nil {
			t.Fatal(err)
		}
		p.tex = append(p.tex, [2]Texture{a, b})
	}
	return p
}

func TestMenuDisplayListFramesDrawWhatSoftwareDraws(t *testing.T) {
	t.Run("supported blitter", func(t *testing.T) { testListFramesDrawWhatSoftwareDraws(t) })
}

func testListFramesDrawWhatSoftwareDraws(t *testing.T) {
	client := newBlitTestClient(true)
	client.minor = menudisplay.MinBlitterMinor
	r := rand.New(rand.NewPCG(949, 2))
	expands := 0
	p := newPair(t, client, r)
	// The first frame cannot know the grant: it is drawn on the CPU, and the
	// blitter grant takes it as one whole copy.
	p.each(func(d Device) { d.BeginFrame(); d.Clear(RGB(1, 2, 3)) })
	p.d.Present()
	waitIdle(t, p.d)
	if first := client.lastList(); len(first) != 1 || first[0].Kind != menudisplay.BlitCopy || !first[0].Source.Transient {
		t.Fatalf("first frame %+v", first)
	}
	for frame := 0; frame < 60; frame++ {
		p.each(func(d Device) { d.BeginFrame() })
		if frame%3 != 2 {
			c := randomColor(r)
			c.A = 255
			p.each(func(d Device) { d.Clear(c) })
		}
		for i := 0; i < 1+r.IntN(40); i++ {
			p.randomCall(r)
		}
		want := p.sw.Snapshot()
		if got := p.d.Snapshot(); !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("frame %d: the CPU raster of the list differs from Software", frame)
		}
		p.d.Present()
		waitIdle(t, p.d)
		lists, cpu := client.counts()
		if lists != frame+2 || cpu != 0 {
			t.Fatalf("frame %d: %d lists, %d CPU frames", frame, lists, cpu)
		}
		if err := sameRGB(client.slot, want.Pix); err != nil {
			t.Fatalf("frame %d: %v", frame, err)
		}
		for _, op := range client.lastList() {
			if op.Kind == menudisplay.BlitExpand {
				expands++
			}
		}
	}
	if expands == 0 {
		t.Fatal("supported blitter received no text expands")
	}
}

// A runtime must not expose a blitter grant below the minimum. Reject one
// before this client submits either a list or a CPU frame to the shared grant.
func TestMenuDisplayRejectsUnsupportedBlitterGrant(t *testing.T) {
	client := newBlitTestClient(true)
	client.minor = menudisplay.MinBlitterMinor - 1
	d, err := NewMenuDisplayWithPresenter(client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	status := menudisplay.Status{Generation: 3}
	frame := queuedMenuFrame{isList: true, generation: 3}
	_, _, blitted, err := d.presentFrame(frame, status, false)
	if err == nil || blitted {
		t.Fatalf("unsupported grant presented: blitted=%t err=%v", blitted, err)
	}
	if lists, cpu := client.counts(); lists != 0 || cpu != 0 {
		t.Fatalf("unsupported grant submitted %d lists and %d CPU frames", lists, cpu)
	}
}

func TestMenuDisplayRasterizesListsForAGrantWithoutTheBlitter(t *testing.T) {
	client := newBlitTestClient(true)
	r := rand.New(rand.NewPCG(7, 7))
	p := newPair(t, client, r)
	for i := 0; i < 2; i++ {
		p.each(func(d Device) { d.BeginFrame(); d.Clear(RGB(9, 9, 9)) })
		p.d.Present()
		waitIdle(t, p.d)
	}
	// Frame drawn as a list, then the menu comes back without the blitter.
	p.each(func(d Device) { d.BeginFrame(); d.Clear(RGB(20, 30, 40)) })
	for i := 0; i < 30; i++ {
		p.randomCall(r)
	}
	client.mu.Lock()
	client.blitter, client.generation = false, 4
	client.mu.Unlock()
	// The frame queued at the old generation is refused; presenting again
	// without drawing repeats it at the new one.
	p.d.Present()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.d.WaitIdle(ctx); err == nil {
		t.Fatal("a frame queued at the old generation was shown")
	}
	p.d.Present()
	waitIdle(t, p.d)
	want := p.sw.Snapshot()
	client.mu.Lock()
	got := client.cpu[len(client.cpu)-1]
	client.mu.Unlock()
	if !bytes.Equal(got, want.Pix) {
		t.Fatal("the rasterized list differs from Software")
	}
	// The next frame draws on the CPU again, building on the last list
	// frame when it does not clear.
	p.each(func(d Device) { d.BeginFrame() })
	for i := 0; i < 20; i++ {
		p.randomCall(r)
	}
	p.d.Present()
	waitIdle(t, p.d)
	if lists, cpu := client.counts(); lists != 2 || cpu != 2 {
		t.Fatalf("%d lists, %d CPU frames", lists, cpu)
	}
	client.mu.Lock()
	got = client.cpu[len(client.cpu)-1]
	client.mu.Unlock()
	if !bytes.Equal(got, p.sw.Snapshot().Pix) {
		t.Fatal("a CPU frame after a list frame lost the list frame's pixels")
	}
}

func TestMenuDisplaySessionPlanesDrawOnTheCPU(t *testing.T) {
	client := newBlitTestClient(true)
	d, err := NewMenuDisplayWithPresenter(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for i := 0; i < 2; i++ {
		d.Clear(RGB(1, 1, 1))
		d.Present()
		waitIdle(t, d)
	}
	if lists, _ := client.counts(); lists != 2 || d.latch() != frameBlit {
		t.Fatal("the idle menu did not use the blitter")
	}
	d.Present()
	waitIdle(t, d)
	client.mu.Lock()
	client.session, client.packageID, client.blitter = true, "pkg", false
	client.mu.Unlock()
	d.BindSession("pkg", 0)
	d.Clear(RGBA(0, 0, 0, 128))
	d.Present()
	waitIdle(t, d)
	if lists, cpu := client.counts(); lists != 3 || cpu != 1 {
		t.Fatalf("session frame: %d lists, %d CPU frames", lists, cpu)
	}
	client.mu.Lock()
	px := client.cpu[0][:4]
	client.mu.Unlock()
	if !bytes.Equal(px, []byte{0, 0, 0, 128}) {
		t.Fatalf("session pixel % x: the plane needs the alpha a CPU frame keeps", px)
	}
}

func TestMenuDisplayCopiesAListTooLargeForTheBlitter(t *testing.T) {
	client := newBlitTestClient(true)
	client.tooLarge = 10
	r := rand.New(rand.NewPCG(3, 1))
	p := newPair(t, client, r)
	p.each(func(d Device) { d.BeginFrame(); d.Clear(RGB(0, 0, 0)) })
	p.d.Present()
	waitIdle(t, p.d)
	p.each(func(d Device) { d.BeginFrame(); d.Clear(RGB(5, 6, 7)) })
	for i := 0; i < 50; i++ {
		rect := Rect{X: float32(i * 20), Y: float32(i * 10), W: 30, H: 30}
		p.each(func(d Device) { d.FillRect(rect, RGBA(200, 100, 50, 255)) })
	}
	p.d.Present()
	waitIdle(t, p.d)
	if last := client.lastList(); len(last) != 1 || last[0].Kind != menudisplay.BlitCopy || !last[0].Source.Transient {
		t.Fatalf("a long list was sent as %d ops", len(last))
	}
	if err := sameRGB(client.slot, p.sw.Snapshot().Pix); err != nil {
		t.Fatal(err)
	}
}

func TestMenuDisplayResubmitsAnUnchangedListFrame(t *testing.T) {
	client := newBlitTestClient(true)
	d, err := NewMenuDisplayWithPresenter(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.SetChangeDriven(true)
	now := time.Unix(100, 0)
	d.mu.Lock()
	d.now = func() time.Time { return now }
	d.mu.Unlock()
	for i := 0; i < 2; i++ {
		d.Clear(RGB(4, 5, 6))
		d.FillRect(Rect{X: 10, Y: 10, W: 10, H: 10}, RGB(255, 0, 0))
		d.Present()
		waitIdle(t, d)
	}
	lists, _ := client.counts()
	// The same commands again are not resubmitted.
	d.Clear(RGB(4, 5, 6))
	d.FillRect(Rect{X: 10, Y: 10, W: 10, H: 10}, RGB(255, 0, 0))
	d.Present()
	waitIdle(t, d)
	if again, _ := client.counts(); again != lists {
		t.Fatal("an identical list frame was resubmitted")
	}
	// A new generation shows the remembered list again from a probe.
	client.mu.Lock()
	client.generation = 9
	client.mu.Unlock()
	now = now.Add(2 * time.Second)
	d.Present()
	waitIdle(t, d)
	if again, _ := client.counts(); again != lists+1 {
		t.Fatalf("the probe sent %d lists, want %d", again, lists+1)
	}
	if last := client.lastList(); len(last) != 2 || last[1].Color != argb(RGB(255, 0, 0)) {
		t.Fatalf("probe list %+v", last)
	}
}

func TestFrameCacheBackdropOnListFrames(t *testing.T) {
	client := newBlitTestClient(true)
	r := rand.New(rand.NewPCG(11, 4))
	p := newPair(t, client, r)
	sw := NewFrameCache(p.sw)
	cache := NewFrameCache(p.d)
	draw := func(c Device, overlay int) {
		c.BeginFrame()
		c.Clear(RGB(30, 40, 50))
		c.FillRect(Rect{X: 100, Y: 100, W: 600, H: 400}, RGB(200, 10, 10))
		c.(interface{ CacheBackdrop() }).CacheBackdrop()
		c.FillRect(Rect{X: float32(overlay * 10), Y: 50, W: 100, H: 100}, RGBA(0, 255, 0, 128))
		c.Present()
	}
	for frame := 0; frame < 6; frame++ {
		draw(sw, frame)
		draw(cache, frame)
		waitIdle(t, p.d)
		if err := sameRGB(client.slot, p.sw.Snapshot().Pix); err != nil {
			t.Fatalf("frame %d: %v", frame, err)
		}
	}
	if lists, _ := client.counts(); lists != 6 {
		t.Fatalf("%d lists", lists)
	}
}

func TestMenuDisplayAnEmptyCPUFrameRepeatsTheLastListFrame(t *testing.T) {
	client := newBlitTestClient(true)
	d, err := NewMenuDisplayWithPresenter(client)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for i := 0; i < 2; i++ {
		d.Clear(RGB(10, 20, 30))
		d.FillRect(Rect{X: 5, Y: 5, W: 50, H: 50}, RGB(200, 0, 0))
		d.Present()
		waitIdle(t, d)
	}
	if lists, _ := client.counts(); lists != 2 {
		t.Fatalf("%d lists", lists)
	}
	client.mu.Lock()
	client.blitter, client.generation = false, 4
	client.mu.Unlock()
	d.Present() // refused at the old generation; the grant has no blitter now
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.WaitIdle(ctx)
	// This empty frame still records a list: the display has not yet seen
	// the new grant. Its present tells it, and the next frame is a CPU one.
	d.BeginFrame()
	d.Present()
	waitIdle(t, d)
	if d.latch() != frameCPU {
		t.Fatal("the display kept drawing lists for a grant without the blitter")
	}
	d.Present()
	waitIdle(t, d)
	client.mu.Lock()
	got := client.cpu[len(client.cpu)-1]
	frames := len(client.cpu)
	client.mu.Unlock()
	if frames != 2 {
		t.Fatalf("%d CPU frames", frames)
	}
	if got[0] != 10 || got[(10*1280+10)*4] != 200 {
		t.Fatalf("the empty CPU frame shows % x / % x", got[:4], got[(10*1280+10)*4:(10*1280+10)*4+4])
	}
}

func TestMenuDisplayListFramesThatNeverClearStayBounded(t *testing.T) {
	client := newBlitTestClient(true)
	r := rand.New(rand.NewPCG(5, 5))
	p := newPair(t, client, r)
	p.each(func(d Device) { d.BeginFrame(); d.Clear(RGB(1, 1, 1)) })
	p.d.Present()
	waitIdle(t, p.d)
	for frame := 0; frame < 40; frame++ {
		p.each(func(d Device) { d.BeginFrame() })
		for i := 0; i < 100; i++ {
			rect := Rect{X: float32(r.IntN(1200)), Y: float32(r.IntN(700)), W: 20, H: 20}
			c := RGBA(uint8(r.IntN(256)), uint8(r.IntN(256)), uint8(r.IntN(256)), uint8(r.IntN(256)))
			p.each(func(d Device) { d.FillRect(rect, c) })
		}
		p.d.Present()
		waitIdle(t, p.d)
		if n := len(client.lastList()); n > maxBaseOps+101 {
			t.Fatalf("frame %d: the list grew to %d ops", frame, n)
		}
		if err := sameRGB(client.slot, p.sw.Snapshot().Pix); err != nil {
			t.Fatalf("frame %d: %v", frame, err)
		}
	}
}
