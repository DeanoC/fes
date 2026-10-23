//go:build linux

package menudisplay

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func newBlitRuntime(t *testing.T) (*fakeRuntime, string) {
	f, path := newFakeRuntime(t)
	f.mu.Lock()
	f.blitter, f.blitterMinor = true, MinBlitterMinor
	f.mu.Unlock()
	return f, path
}

// expectFrame draws ops straight from their sources, the way the blitter
// draws them, over prev (a B, G, R, X slot).
func expectFrame(prev []byte, ops []BlitOp) []byte {
	out := bytes.Clone(prev)
	for _, op := range ops {
		for y := 0; y < op.H; y++ {
			for x := 0; x < op.W; x++ {
				d := out[(op.Y+y)*Stride+(op.X+x)*4:]
				var r, g, b, a uint32
				if op.Kind == BlitFill || op.Kind == BlitFillBlend || op.Kind == BlitExpand {
					a, r, g, b = op.Color>>24, op.Color>>16&0xff, op.Color>>8&0xff, op.Color&0xff
					if op.Kind == BlitExpand {
						a = a * uint32(op.Source.Pix[(op.SourceY+y)*op.Source.Stride+op.SourceX+x]) / 255
					}
				} else {
					s := op.Source.Pix[(op.SourceY+y)*op.Source.Stride+(op.SourceX+x)*4:]
					r, g, b, a = uint32(s[0]), uint32(s[1]), uint32(s[2]), uint32(s[3])
				}
				if op.Kind == BlitFill || op.Kind == BlitCopy || a == 255 {
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
	return out
}

// pattern is a w×h RGBA source whose alpha runs through every value.
func pattern(w, h int, seed byte) *BlitSource {
	s := &BlitSource{Pix: make([]byte, (w+3)*4*h), Stride: (w + 3) * 4, W: w, H: h}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := s.Pix[y*s.Stride+x*4:]
			p[0], p[1], p[2], p[3] = byte(x)+seed, byte(y)*3, seed^byte(x*y), byte(x+y*w)
		}
	}
	return s
}

// coverage is a w×h coverage source whose bytes run through every value.
func coverage(w, h int, seed byte) *BlitSource {
	s := &BlitSource{Pix: make([]byte, (w+5)*h), Stride: w + 5, W: w, H: h, Coverage: true}
	for i := range s.Pix {
		s.Pix[i] = byte(i*7) + seed
	}
	return s
}

func TestPresentBlitDrawsTheListFromTheHeap(t *testing.T) {
	runtime, path := newBlitRuntime(t)
	client := New(path)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	grant, err := client.Grant(ctx, 7)
	if err != nil || !grant.Blitter || grant.BlitterMinor != MinBlitterMinor {
		t.Fatalf("grant %+v %v", grant, err)
	}
	a, b, m := pattern(40, 30, 1), pattern(300, 7, 9), coverage(33, 9, 3)
	ops := []BlitOp{
		{Kind: BlitFill, W: Width, H: Height, Color: 0xff203040},
		{Kind: BlitFillBlend, X: 10, Y: 10, W: 500, H: 200, Color: 0x80ff0000},
		{Kind: BlitCopy, X: 100, Y: 100, W: 40, H: 30, Source: a},
		{Kind: BlitBlend, X: 1000, Y: 700, W: 280, H: 7, Source: b, SourceX: 20},
		{Kind: BlitBlend, X: 0, Y: 0, W: 16, H: 16, Source: a, SourceX: 3, SourceY: 2},
		{Kind: BlitExpand, X: 601, Y: 300, W: 33, H: 9, Source: m, Color: 0xff40c080},
		{Kind: BlitExpand, X: 700, Y: 300, W: 20, H: 6, Source: m, SourceX: 7, SourceY: 3, Color: 0x9a102030},
	}
	result, err := client.PresentBlit(ctx, 7, BlitFrame{Ops: ops})
	if err != nil || result.DisplayedSequence != 1 {
		t.Fatalf("result %+v %v", result, err)
	}
	if got, want := runtime.slot(1), expectFrame(make([]byte, FrameBytes), ops); !bytes.Equal(got, want) {
		t.Fatal("the slot differs from the list")
	}
	commit := runtime.commit(0)
	if list := commit["list"].(map[string]any); list["first"] != 0.0 || list["count"] != float64(len(ops)) {
		t.Fatalf("list %v", list)
	}
	// The textures were placed back to back, so one span writes them back;
	// the coverage starts on the next cache line, past a gap.
	if len(commit["uploaded"].([]any)) != 2 || result.Timing.Commands != int64(len(ops)) ||
		result.Timing.UploadBytes != int64(40*30*4+300*7*4+33*9) {
		t.Fatalf("uploaded %v timing %+v", commit["uploaded"], result.Timing)
	}
	// The next frame reads the resident sources without uploading them;
	// the fake reads only written-back heap bytes, so a source the client
	// wrongly thought resident would draw stale pixels.
	ops[0].Color = 0xff000000
	if _, err := client.PresentBlit(ctx, 7, BlitFrame{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	if _, uploaded := runtime.commit(1)["uploaded"]; uploaded {
		t.Fatal("resident sources were uploaded again")
	}
	if got, want := runtime.slot(0), expectFrame(make([]byte, FrameBytes), ops); !bytes.Equal(got, want) {
		t.Fatal("the second slot differs from the list")
	}
	// After a list the grant refuses CPU frames, as the runtime does.
	if _, err := client.Present(ctx, 7, filled(1, 2, 3)); err == nil || !strings.Contains(err.Error(), "only lists") {
		t.Fatalf("a CPU frame after a list: %v", err)
	}
	// An empty frame would show the slot's frame from two presents ago.
	if _, err := client.PresentBlit(ctx, 7, BlitFrame{}); err == nil {
		t.Fatal("an empty frame was committed")
	}
}

func TestPresentBlitUploadsAgainAfterAFailedCommit(t *testing.T) {
	runtime, path := newBlitRuntime(t)
	client := New(path)
	defer client.Close()
	ctx := context.Background()
	a := pattern(64, 64, 5)
	ops := []BlitOp{{Kind: BlitFill, W: Width, H: Height, Color: 0xff000000}, {Kind: BlitBlend, X: 8, Y: 8, W: 64, H: 64, Source: a}}
	runtime.mu.Lock()
	runtime.refuse = "menu blitter is not idle"
	runtime.mu.Unlock()
	if _, err := client.PresentBlit(ctx, 7, BlitFrame{Ops: ops}); err == nil {
		t.Fatal("a refused commit succeeded")
	}
	// The refused commit wrote nothing back, so the source is uploaded again.
	if _, err := client.PresentBlit(ctx, 7, BlitFrame{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	if _, uploaded := runtime.commit(1)["uploaded"]; !uploaded {
		t.Fatal("a source whose upload was never written back was trusted")
	}
	if got, want := runtime.slot(1), expectFrame(make([]byte, FrameBytes), ops); !bytes.Equal(got, want) {
		t.Fatal("the slot differs from the list")
	}
	// A new generation is a new grant with an empty heap.
	runtime.mu.Lock()
	runtime.generation = 8
	runtime.mu.Unlock()
	if _, err := client.PresentBlit(ctx, 8, BlitFrame{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	if _, uploaded := runtime.commit(2)["uploaded"]; !uploaded || runtime.grantCount() != 2 {
		t.Fatal("a new grant trusted the old grant's heap")
	}
}

func TestPresentBlitUploadsAgainAfterAnotherPeerDrew(t *testing.T) {
	runtime, path := newBlitRuntime(t)
	ctx := context.Background()
	first, second := New(path), New(path)
	defer first.Close()
	defer second.Close()
	a, b := pattern(64, 64, 5), pattern(64, 64, 200)
	opsA := []BlitOp{{Kind: BlitFill, W: Width, H: Height, Color: 0xff000000}, {Kind: BlitCopy, X: 8, Y: 8, W: 64, H: 64, Source: a}}
	opsB := []BlitOp{{Kind: BlitFill, W: Width, H: Height, Color: 0xff000000}, {Kind: BlitCopy, X: 8, Y: 8, W: 64, H: 64, Source: b}}
	for i, step := range []struct {
		client *Client
		ops    []BlitOp
	}{{first, opsA}, {first, opsA}, {second, opsB}, {first, opsA}} {
		if _, err := step.client.PresentBlit(ctx, 7, BlitFrame{Ops: step.ops}); err != nil {
			t.Fatal(err)
		}
		// The second client's upload took the first's heap offset, so the
		// first client's next frame must upload its source again.
		_, uploaded := runtime.commit(i)["uploaded"]
		if want := i != 1; uploaded != want {
			t.Fatalf("frame %d uploaded=%v", i, uploaded)
		}
		if got, want := runtime.slot(1-i%2), expectFrame(make([]byte, FrameBytes), step.ops); !bytes.Equal(got, want) {
			t.Fatalf("frame %d differs from its list", i)
		}
	}
}

func TestPresentBlitTransientSourcesAreUploadedEveryFrame(t *testing.T) {
	runtime, path := newBlitRuntime(t)
	client := New(path)
	defer client.Close()
	ctx := context.Background()
	frame := &BlitSource{Pix: filled(9, 8, 7), Stride: Stride, W: Width, H: Height, Transient: true}
	ops := []BlitOp{{Kind: BlitCopy, W: Width, H: Height, Source: frame}}
	for i := 0; i < 3; i++ {
		frame.Pix[i*4] = byte(100 + i)
		if _, err := client.PresentBlit(ctx, 7, BlitFrame{Ops: ops}); err != nil {
			t.Fatal(err)
		}
		if _, uploaded := runtime.commit(i)["uploaded"]; !uploaded {
			t.Fatalf("frame %d reused a transient source", i)
		}
		if got := runtime.slot(1 - i%2)[:12]; got[i*4+2] != byte(100+i) {
			t.Fatalf("frame %d shows % x", i, got)
		}
	}
}

func TestPresentBlitNeedsABlitterGrant(t *testing.T) {
	runtime, path := newFakeRuntime(t)
	client := New(path)
	defer client.Close()
	ctx := context.Background()
	if grant, err := client.Grant(ctx, 7); err != nil || grant.Blitter {
		t.Fatalf("grant %+v %v", grant, err)
	}
	if _, err := client.PresentBlit(ctx, 7, BlitFrame{Ops: []BlitOp{{Kind: BlitFill, W: 1, H: 1}}}); err == nil {
		t.Fatal("a list presented on a grant without the blitter")
	}
	runtime.mu.Lock()
	commits := len(runtime.commits)
	runtime.mu.Unlock()
	if commits != 0 {
		t.Fatal("a refused list reached the runtime")
	}
}

func TestPresentBlitRefusesExpandBelowMinimumBlitterMinor(t *testing.T) {
	runtime, path := newBlitRuntime(t)
	runtime.mu.Lock()
	runtime.blitterMinor = 0
	runtime.mu.Unlock()
	client := New(path)
	defer client.Close()
	ctx := context.Background()
	if grant, err := client.Grant(ctx, 7); err != nil || !grant.Blitter || grant.BlitterMinor != 0 {
		t.Fatalf("grant %+v %v", grant, err)
	}
	expand := BlitFrame{Ops: []BlitOp{{Kind: BlitExpand, W: 4, H: 4, Source: coverage(4, 4, 0), Color: 0xffffffff}}}
	if _, err := client.PresentBlit(ctx, 7, expand); err == nil {
		t.Fatal("an expand was sent to a blitter that faults it")
	}
	tooMany := BlitFrame{Ops: make([]BlitOp, ListCommands+1)}
	if _, err := client.PresentBlit(ctx, 7, tooMany); !errors.Is(err, ErrBlitTooLarge) {
		t.Fatalf("too many commands: %v", err)
	}
	runtime.mu.Lock()
	commits := len(runtime.commits)
	runtime.mu.Unlock()
	if commits != 0 {
		t.Fatal("a refused list reached the runtime")
	}
}
