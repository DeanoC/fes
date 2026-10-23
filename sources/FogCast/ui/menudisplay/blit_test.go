package menudisplay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"testing"
)

// blitReference runs a list as a menu blitter of minor does
// (sources/misteross/cores/fes-menu/sim/blit_reference.hpp): window holds
// the heap at its window offsets, slot is B, G, R, X with X written 0. It
// refuses what the blitter faults on.
func blitReference(list, window, slot []byte, minor int) error {
	for i := 0; i+CommandBytes <= len(list); i += CommandBytes {
		w0 := binary.LittleEndian.Uint64(list[i:])
		w1 := binary.LittleEndian.Uint64(list[i+8:])
		w2 := binary.LittleEndian.Uint64(list[i+16:])
		w3 := binary.LittleEndian.Uint64(list[i+24:])
		op := int(w0 & 0xff)
		if op == 0 {
			if w0|w1|w2|w3 != 0 {
				return fmt.Errorf("command %d: END with reserved bits", i/CommandBytes)
			}
			return nil
		}
		colour := uint32(w0 >> 32)
		x, y, w, h := int(w1&0xffff), int(w1>>16&0xffff), int(w1>>32&0xffff), int(w1>>48)
		source, stride := uint32(w2), int(w2>>32&0xffff)
		if w0&0xffffff00 != 0 || w2>>48 != 0 || w3 != 0 {
			return fmt.Errorf("command %d: reserved bits", i/CommandBytes)
		}
		if w < 1 || h < 1 || x+w > Width || y+h > Height {
			return fmt.Errorf("command %d: rectangle outside the frame", i/CommandBytes)
		}
		texture := op == int(BlitCopy) || op == int(BlitBlend)
		expand := op == int(BlitExpand) && minor >= MinBlitterMinor
		switch {
		case op == int(BlitFill) || op == int(BlitFillBlend):
			if w2 != 0 {
				return fmt.Errorf("command %d: fill with a source", i/CommandBytes)
			}
		case texture:
			if colour != 0 {
				return fmt.Errorf("command %d: texture op with a colour", i/CommandBytes)
			}
			end := int(source) + (h-1)*stride + w*4
			if source%4 != 0 || stride%4 != 0 || stride < 4*w || int(source) < HeapOffset || end > HeapOffset+HeapBytes {
				return fmt.Errorf("command %d: source outside the heap", i/CommandBytes)
			}
		case expand:
			end := int(source) + (h-1)*stride + w
			if stride < w || int(source) < HeapOffset || end > HeapOffset+HeapBytes {
				return fmt.Errorf("command %d: coverage outside the heap", i/CommandBytes)
			}
		default:
			return fmt.Errorf("command %d: unknown op %d", i/CommandBytes, op)
		}
		c := [4]byte{byte(colour), byte(colour >> 8), byte(colour >> 16), byte(colour >> 24)}
		for row := 0; row < h; row++ {
			for col := 0; col < w; col++ {
				d := slot[(y+row)*Stride+(x+col)*4:]
				s := c[:]
				if texture {
					at := int(source) + row*stride + col*4
					s = window[at : at+4]
				}
				if expand {
					m := uint32(window[int(source)+row*stride+col])
					s = []byte{c[0], c[1], c[2], byte(uint32(c[3]) * m / 255)}
				}
				if op == int(BlitFill) || op == int(BlitCopy) {
					d[0], d[1], d[2], d[3] = s[0], s[1], s[2], 0
					continue
				}
				a := uint32(s[3])
				if a != 0 {
					for k := 0; k < 3; k++ {
						if a == 255 {
							d[k] = s[k]
						} else {
							d[k] = byte((uint32(s[k])*a + uint32(d[k])*(255-a)) / 255)
						}
					}
				}
				d[3] = 0
			}
		}
	}
	return nil
}

func TestEncodeCommandFollowsTheWireContract(t *testing.T) {
	var got [CommandBytes]byte
	encodeCommand(got[:], BlitOp{Kind: BlitFillBlend, X: 1, Y: 2, W: 3, H: 4, Color: 0x80112233}, 0, 0)
	want := [4]uint64{2 | 0x80112233<<32, 1 | 2<<16 | 3<<32 | 4<<48, 0, 0}
	for i, w := range want {
		if v := binary.LittleEndian.Uint64(got[i*8:]); v != w {
			t.Fatalf("fill word %d = %#x, want %#x", i, v, w)
		}
	}
	encodeCommand(got[:], BlitOp{Kind: BlitBlend, X: 5, Y: 6, W: 7, H: 8, Color: 0xffffffff}, HeapOffset+64, 28)
	want = [4]uint64{4, 5 | 6<<16 | 7<<32 | 8<<48, HeapOffset + 64 | 28<<32, 0}
	for i, w := range want {
		if v := binary.LittleEndian.Uint64(got[i*8:]); v != w {
			t.Fatalf("blend word %d = %#x, want %#x (a texture op carries no colour)", i, v, w)
		}
	}
	encodeCommand(got[:], BlitOp{Kind: BlitExpand, X: 5, Y: 6, W: 7, H: 8, Color: 0xc0102030}, HeapOffset+67, 9)
	want = [4]uint64{5 | 0xc0102030<<32, 5 | 6<<16 | 7<<32 | 8<<48, HeapOffset + 67 | 9<<32, 0}
	for i, w := range want {
		if v := binary.LittleEndian.Uint64(got[i*8:]); v != w {
			t.Fatalf("expand word %d = %#x, want %#x", i, v, w)
		}
	}
}

func TestUploadSpansMergeTheClosestNeighboursFirst(t *testing.T) {
	var spans []Span
	for i := 0; i < 20; i++ {
		spans = append(spans, Span{HeapOffset + i*1000 + i*i, 100})
	}
	spans = append(spans, Span{HeapOffset + 50, 100}) // overlaps the first
	merged := uploadSpans(spans)
	if len(merged) != MaxUploadSpans {
		t.Fatalf("%d spans", len(merged))
	}
	for _, s := range spans {
		covered := false
		for _, m := range merged {
			covered = covered || s.Offset >= m.Offset && s.Offset+s.Bytes <= m.Offset+m.Bytes
		}
		if !covered {
			t.Fatalf("%+v is not written back by %+v", s, merged)
		}
	}
	// The widest gaps, between the last sources, stay unwritten.
	if last := merged[len(merged)-1]; last.Offset != HeapOffset+19*1000+19*19 {
		t.Fatalf("last span %+v merged a wide gap", last)
	}
	if uploadSpans(nil) != nil {
		t.Fatal("no uploads named a span")
	}
}

func source(w, h int) *BlitSource {
	return &BlitSource{Pix: make([]byte, w*h*4), Stride: w * 4, W: w, H: h}
}

func copyOf(s *BlitSource) BlitOp {
	return BlitOp{Kind: BlitCopy, W: min(s.W, Width), H: min(s.H, Height), Source: s}
}

func TestHeapKeepsSourcesResidentAndEvictsTheLeastRecentlyDrawn(t *testing.T) {
	h := newBlitHeap()
	quarter := source(2048, 2048) // 16 MiB
	a, b, c := quarter, source(2048, 2048), source(2048, 2048)
	p, err := h.place(BlitFrame{Ops: []BlitOp{copyOf(a), copyOf(b), copyOf(a)}})
	if err != nil || len(p.uploads) != 2 {
		t.Fatalf("first frame uploads %v err %v", p, err)
	}
	h.finish(p)
	p, _ = h.place(BlitFrame{Ops: []BlitOp{copyOf(b)}})
	if len(p.uploads) != 0 {
		t.Fatal("a resident source was uploaded again")
	}
	h.finish(p)
	// c and d fill the heap; a was drawn longest ago and goes first.
	d := source(2048, 2048)
	p, _ = h.place(BlitFrame{Ops: []BlitOp{copyOf(c), copyOf(d)}})
	h.finish(p)
	e := source(2048, 2048)
	p, _ = h.place(BlitFrame{Ops: []BlitOp{copyOf(e), copyOf(b)}})
	if len(p.uploads) != 1 || p.uploads[0] != e {
		t.Fatalf("uploads %v", p.uploads)
	}
	h.finish(p)
	p, _ = h.place(BlitFrame{Ops: []BlitOp{copyOf(a)}})
	if len(p.uploads) != 1 {
		t.Fatal("an evicted source was still resident")
	}
	if _, err := h.place(BlitFrame{Ops: []BlitOp{copyOf(source(4096, 4097))}}); !errors.Is(err, ErrBlitTooLarge) {
		t.Fatalf("a frame larger than the heap: %v", err)
	}
	runtime.KeepAlive([]*BlitSource{a, b, c, d, e})
}

func TestHeapFreesTransientAndUnreachableSources(t *testing.T) {
	h := newBlitHeap()
	frame := &BlitSource{Pix: make([]byte, FrameBytes), Stride: Stride, W: Width, H: Height, Transient: true}
	for i := 0; i < 40; i++ { // 40 whole frames would not fit if kept
		p, err := h.place(BlitFrame{Ops: []BlitOp{copyOf(frame)}})
		if err != nil || len(p.uploads) != 1 {
			t.Fatalf("frame %d: %v %v", i, p, err)
		}
		h.finish(p)
	}
	if len(h.free) != 1 || h.free[0] != (Span{0, HeapBytes}) {
		t.Fatalf("transient frames left %v", h.free)
	}
	func() {
		p, _ := h.place(BlitFrame{Ops: []BlitOp{copyOf(source(64, 64))}})
		h.finish(p)
	}()
	if len(h.resident) != 1 {
		t.Fatal("a source was not kept")
	}
	runtime.GC()
	runtime.GC()
	p, _ := h.place(BlitFrame{})
	h.finish(p)
	if len(h.resident) != 0 || len(h.free) != 1 || h.free[0] != (Span{0, HeapBytes}) {
		t.Fatalf("an unreachable source stayed resident: %d entries, free %v", len(h.resident), h.free)
	}
}

func TestHeapStartsOverWhenTheFrameItselfFragmentsIt(t *testing.T) {
	h := newBlitHeap()
	small := make([]*BlitSource, 64)
	var ops []BlitOp
	for i := range small {
		small[i] = source(1024, 256) // 1 MiB each: the whole heap
		ops = append(ops, copyOf(small[i]))
	}
	p, err := h.place(BlitFrame{Ops: ops})
	if err != nil || len(p.uploads) != 64 {
		t.Fatalf("%d uploads, %v", len(p.uploads), err)
	}
	h.finish(p)
	// Keep every other one: the free space is all 1 MiB holes.
	var next []BlitOp
	for i := 0; i < 64; i += 2 {
		next = append(next, copyOf(small[i]))
	}
	big := source(2048, 1024) // 8 MiB
	next = append(next, copyOf(big))
	p, err = h.place(BlitFrame{Ops: next})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.at[big]; !ok || len(p.uploads) == 0 {
		t.Fatal("the large source was not placed")
	}
	seen := map[int]bool{}
	for s, at := range p.at {
		for o := at; o < at+alignHeap(s.bytes()); o += 1 << 20 {
			if seen[o] {
				t.Fatalf("sources overlap at %d", o)
			}
			seen[o] = true
		}
	}
	runtime.KeepAlive(small)
}

func TestFrameCheckRefusesWhatTheBlitterWouldFaultOn(t *testing.T) {
	s := source(8, 8)
	coverage := &BlitSource{Pix: make([]byte, 8), Stride: 8, W: 8, H: 1, Coverage: true}
	for _, op := range []BlitOp{
		{Kind: BlitFill, X: 0, Y: 0, W: 0, H: 1},
		{Kind: BlitFill, X: 1279, Y: 0, W: 2, H: 1},
		{Kind: BlitFill, X: -1, Y: 0, W: 2, H: 1},
		{Kind: 9, W: 1, H: 1},
		{Kind: BlitCopy, W: 1, H: 1},
		{Kind: BlitBlend, W: 4, H: 4, Source: s, SourceX: 5},
		// Coverage is only for expands, and expands read only coverage.
		{Kind: BlitBlend, W: 1, H: 1, Source: coverage},
		{Kind: BlitExpand, W: 1, H: 1, Source: s},
		{Kind: BlitExpand, W: 4, H: 1, Source: coverage, SourceX: 5},
		{Kind: BlitExpand, W: 1, H: 1, Source: &BlitSource{Pix: make([]byte, 8), Stride: 3, W: 4, H: 2, Coverage: true}},
	} {
		if err := (BlitFrame{Ops: []BlitOp{op}}).check(MinBlitterMinor); err == nil || errors.Is(err, ErrBlitTooLarge) {
			t.Fatalf("%+v: %v", op, err)
		}
	}
	expand := BlitFrame{Ops: []BlitOp{{Kind: BlitExpand, W: 8, H: 1, Source: coverage}}}
	if err := expand.check(MinBlitterMinor); err != nil {
		t.Fatal(err)
	}
	if err := expand.check(0); err == nil {
		t.Fatal("an expand passed below the minimum blitter minor")
	}
	wide := source(16384, 1)
	if err := (BlitFrame{Ops: []BlitOp{{Kind: BlitCopy, W: 1, H: 1, Source: wide}}}).check(MinBlitterMinor); !errors.Is(err, ErrBlitTooLarge) {
		t.Fatalf("a source too wide for the stride field: %v", err)
	}
	wideCoverage := &BlitSource{Pix: make([]byte, 65536), Stride: 65536, W: 65536, H: 1, Coverage: true}
	if err := (BlitFrame{Ops: []BlitOp{{Kind: BlitExpand, W: 1, H: 1, Source: wideCoverage}}}).check(MinBlitterMinor); !errors.Is(err, ErrBlitTooLarge) {
		t.Fatalf("coverage too wide for the stride field: %v", err)
	}
	if err := (BlitFrame{Ops: make([]BlitOp, ListCommands+1)}).check(MinBlitterMinor); !errors.Is(err, ErrBlitTooLarge) {
		t.Fatalf("a list longer than the region: %v", err)
	}
}
