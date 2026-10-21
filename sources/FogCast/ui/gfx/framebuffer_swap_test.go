package gfx

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

func pixelAt(pix []byte, w, x, y int) [4]byte {
	at := (y*w + x) * 4
	return [4]byte{pix[at], pix[at+1], pix[at+2], pix[at+3]}
}

func TestSoftwareSwapRestoresOnlyForIncrementalFrames(t *testing.T) {
	s, err := NewSoftware(16, 8)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(RGB(200, 0, 0))
	s.FillRect(Rect{X: 2, Y: 2, W: 2, H: 2}, RGB(0, 200, 0))
	first := s.swapFramebuffer(make([]byte, len(s.pix)))
	// No copy: the finished frame is handed over as is.
	if pixelAt(first, 16, 0, 0) != [4]byte{200, 0, 0, 255} || pixelAt(first, 16, 2, 2) != [4]byte{0, 200, 0, 255} {
		t.Fatal("swapped-out frame changed")
	}
	// An incremental frame builds on the previous picture.
	s.FillRect(Rect{X: 8, Y: 4, W: 2, H: 2}, RGB(0, 0, 200))
	fb := s.Framebuffer().Pix
	if pixelAt(fb, 16, 0, 0) != [4]byte{200, 0, 0, 255} || pixelAt(fb, 16, 2, 2) != [4]byte{0, 200, 0, 255} ||
		pixelAt(fb, 16, 8, 4) != [4]byte{0, 0, 200, 255} {
		t.Fatal("incremental frame lost the previous picture")
	}
	second := s.swapFramebuffer(first)
	// A frame that starts with Clear never needs the previous pixels.
	first[0] = 77
	s.Clear(RGB(9, 9, 9))
	if s.previous != nil || !bytes.Equal(s.Framebuffer().Pix, bytes.Repeat([]byte{9, 9, 9, 255}, 16*8)) {
		t.Fatal("clear did not replace the frame")
	}
	if pixelAt(second, 16, 8, 4) != [4]byte{0, 0, 200, 255} {
		t.Fatal("second frame changed after it was handed over")
	}
}

// wholeFramePresenter keeps every presented frame.
type wholeFramePresenter struct {
	mu     sync.Mutex
	frames [][]byte
	ready  chan struct{}
}

func (p *wholeFramePresenter) Status(context.Context) (menudisplay.Status, error) {
	return menudisplay.Status{Available: true, Generation: 1}, nil
}

func (p *wholeFramePresenter) Present(_ context.Context, _ uint64, pixels []byte) (menudisplay.Result, error) {
	p.mu.Lock()
	p.frames = append(p.frames, append([]byte(nil), pixels...))
	n := len(p.frames)
	p.mu.Unlock()
	p.ready <- struct{}{}
	return menudisplay.Result{Generation: 1, DisplayedSequence: uint64(n)}, nil
}

func (p *wholeFramePresenter) frame(i int) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frames[i]
}

func TestMenuDisplayHandsOverFramesWithoutCopyingAndKeepsIncrementalDrawing(t *testing.T) {
	client := &wholeFramePresenter{ready: make(chan struct{}, 4)}
	d, err := newMenuDisplayWithClient(client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	wait := func() {
		t.Helper()
		select {
		case <-client.ready:
		case <-time.After(time.Second):
			t.Fatal("frame not presented")
		}
	}
	w := menudisplay.Width
	d.Clear(RGB(200, 0, 0))
	d.FillRect(Rect{X: 10, Y: 10, W: 8, H: 8}, RGB(0, 200, 0))
	d.Present()
	wait()
	// The next frame draws only a new rect; the picture underneath stays.
	d.FillRect(Rect{X: 100, Y: 50, W: 8, H: 8}, RGB(0, 0, 200))
	d.Present()
	wait()
	d.Clear(RGB(5, 6, 7))
	d.Present()
	wait()
	first, second, third := client.frame(0), client.frame(1), client.frame(2)
	if pixelAt(first, w, 12, 12) != [4]byte{0, 200, 0, 255} || pixelAt(first, w, 100, 50) != [4]byte{200, 0, 0, 255} {
		t.Fatal("first frame wrong")
	}
	if pixelAt(second, w, 0, 0) != [4]byte{200, 0, 0, 255} || pixelAt(second, w, 12, 12) != [4]byte{0, 200, 0, 255} ||
		pixelAt(second, w, 100, 50) != [4]byte{0, 0, 200, 255} {
		t.Fatal("incremental frame lost the previous picture")
	}
	if pixelAt(third, w, 12, 12) != [4]byte{5, 6, 7, 255} {
		t.Fatal("cleared frame wrong")
	}
}
