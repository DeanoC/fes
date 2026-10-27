package menudisplay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"weak"
)

// The window layout of fes.video.menu-blitter 1.x. A blitter grant's
// menu_frame.blitter must name exactly these.
const (
	ListOffset   = 8 << 20
	ListCommands = 32768
	CommandBytes = 32
	HeapOffset   = 16 << 20
	HeapBytes    = 64 << 20
	// BlitWindowBytes is how much of a blitter grant the client maps.
	BlitWindowBytes = HeapOffset + HeapBytes
	// MaxUploadSpans is how many heap ranges one commit can name.
	MaxUploadSpans = 16
	// heapAlign starts every texture on its own cache lines.
	heapAlign = 64
	// maxSourceStride is the command's 16-bit source stride in bytes.
	maxSourceStride = 0xffff
)

// BlitKind is a blitter op. END is not a kind: a list ends at its count.
type BlitKind uint8

const (
	// BlitFill replaces the rectangle with the colour.
	BlitFill BlitKind = 1
	// BlitFillBlend draws the colour source-over.
	BlitFillBlend BlitKind = 2
	// BlitCopy replaces the rectangle with the source.
	BlitCopy BlitKind = 3
	// BlitBlend draws the source source-over with its straight alpha.
	BlitBlend BlitKind = 4
	// BlitExpand draws Color source-over with its alpha scaled by the
	// coverage source: A·m/255. Blitter minor 1 added it.
	BlitExpand BlitKind = 5
)

// MinBlitterMinor is the lowest minor selected for menu rendering. Minor 1
// adds BlitExpand, which tenfoot uses to draw text coverage masks.
const MinBlitterMinor = 1

// BlitSource is a bitmap of R, G, B, A bytes that copies and blends read,
// or with Coverage one byte a pixel that expands read. The client uploads it
// into the grant's texture heap, R, G, B, A as B, G, R, A, and keeps it there
// while the source is reachable and the heap has room, so a source must not
// change once a frame has used it. Transient marks pixels that will change,
// such as a pooled frame buffer: they are uploaded for every frame that
// names them and never kept.
type BlitSource struct {
	Pix       []byte
	Stride    int
	W, H      int
	Coverage  bool
	Transient bool
}

// pixelBytes is the bytes of one source pixel.
func (s *BlitSource) pixelBytes() int {
	if s.Coverage {
		return 1
	}
	return 4
}

func (s *BlitSource) row() int   { return s.W * s.pixelBytes() }
func (s *BlitSource) bytes() int { return s.row() * s.H }

// reads reports whether the op reads a source.
func (k BlitKind) reads() bool { return k == BlitCopy || k == BlitBlend || k == BlitExpand }

// BlitOp is one command. X, Y, W and H are the destination inside the frame.
// Fills and expands use Color, 0xAARRGGBB; copies, blends and expands read
// W×H pixels of Source from SourceX, SourceY.
type BlitOp struct {
	Kind             BlitKind
	X, Y, W, H       int
	Color            uint32
	Source           *BlitSource
	SourceX, SourceY int
}

// BlitFrame is a frame the blitter draws whole into the slot, in order.
type BlitFrame struct {
	Ops []BlitOp
}

// Grant is what a generation's grant offers this client.
type Grant struct {
	// Blitter means the grant covers the blitter's lists and heap.
	Blitter bool
	// BlitterMinor is the menu-blitter minor the firmware declares.
	BlitterMinor int
}

// ErrBlitTooLarge means a frame needs more commands or heap than the
// blitter has, or a source too wide for a command. Nothing was sent; the
// caller can draw the frame on the CPU and present it as one copy.
var ErrBlitTooLarge = errors.New("menu blit frame exceeds the blitter's list or heap")

func alignHeap(n int) int { return (n + heapAlign - 1) &^ (heapAlign - 1) }

// check validates every op before anything touches the window. minor is
// the grant's blitter minor.
func (f BlitFrame) check(minor int) error {
	if len(f.Ops) > ListCommands {
		return ErrBlitTooLarge
	}
	for i, op := range f.Ops {
		if op.W < 1 || op.H < 1 || op.X < 0 || op.Y < 0 || op.X+op.W > Width || op.Y+op.H > Height {
			return fmt.Errorf("menu blit op %d: rectangle is outside the frame", i)
		}
		switch op.Kind {
		case BlitFill, BlitFillBlend:
		case BlitCopy, BlitBlend, BlitExpand:
			if op.Kind == BlitExpand && minor < MinBlitterMinor {
				return fmt.Errorf("menu blit op %d: blitter %d.%d has no expand", i, 1, minor)
			}
			s := op.Source
			if s == nil || s.W < 1 || s.H < 1 || s.Stride < s.row() || len(s.Pix) < (s.H-1)*s.Stride+s.row() {
				return fmt.Errorf("menu blit op %d: invalid source", i)
			}
			if s.Coverage != (op.Kind == BlitExpand) {
				return fmt.Errorf("menu blit op %d: source format does not match the op", i)
			}
			if op.SourceX < 0 || op.SourceY < 0 || op.SourceX+op.W > s.W || op.SourceY+op.H > s.H {
				return fmt.Errorf("menu blit op %d: source rectangle is outside the source", i)
			}
			if s.row() > maxSourceStride {
				return ErrBlitTooLarge
			}
		default:
			return fmt.Errorf("menu blit op %d: unknown kind %d", i, op.Kind)
		}
	}
	return nil
}

// encodeCommand writes op as four little-endian words. source is the
// window offset of the op's first source pixel and stride its row pitch.
func encodeCommand(dst []byte, op BlitOp, source uint32, stride int) {
	var w0, w2 uint64
	w0 = uint64(op.Kind)
	if op.Kind == BlitFill || op.Kind == BlitFillBlend || op.Kind == BlitExpand {
		w0 |= uint64(op.Color) << 32
	}
	if op.Kind.reads() {
		w2 = uint64(source) | uint64(stride)<<32
	}
	w1 := uint64(op.X) | uint64(op.Y)<<16 | uint64(op.W)<<32 | uint64(op.H)<<48
	binary.LittleEndian.PutUint64(dst[0:], w0)
	binary.LittleEndian.PutUint64(dst[8:], w1)
	binary.LittleEndian.PutUint64(dst[16:], w2)
	binary.LittleEndian.PutUint64(dst[24:], 0)
}

// Span is a byte range of the window.
type Span struct{ Offset, Bytes int }

// uploadSpans merges written heap ranges into at most MaxUploadSpans,
// joining the closest neighbours first. Writing back the gap between two
// uploads only writes back bytes the blitter may read anyway.
func uploadSpans(spans []Span) []Span {
	if len(spans) == 0 {
		return nil
	}
	out := slices.Clone(spans)
	slices.SortFunc(out, func(a, b Span) int { return a.Offset - b.Offset })
	merged := out[:1]
	for _, s := range out[1:] {
		last := &merged[len(merged)-1]
		if s.Offset <= last.Offset+last.Bytes {
			last.Bytes = max(last.Bytes, s.Offset+s.Bytes-last.Offset)
			continue
		}
		merged = append(merged, s)
	}
	for len(merged) > MaxUploadSpans {
		best := 0
		for i := 1; i+1 < len(merged); i++ {
			if gap(merged[i], merged[i+1]) < gap(merged[best], merged[best+1]) {
				best = i
			}
		}
		merged[best].Bytes = merged[best+1].Offset + merged[best+1].Bytes - merged[best].Offset
		merged = slices.Delete(merged, best+1, best+2)
	}
	return merged
}

func gap(a, b Span) int { return b.Offset - (a.Offset + a.Bytes) }

// blitHeap places sources in the grant's texture heap. Offsets are relative
// to the heap. A source stays resident while it is reachable, until the
// space is needed for a frame; the least recently drawn go first.
type blitHeap struct {
	free     []Span // sorted, coalesced
	resident map[weak.Pointer[BlitSource]]*heapEntry
	frame    uint64
}

type heapEntry struct {
	at, bytes int
	drawn     uint64
}

// placement is one frame's sources: where each is, and which were uploaded.
type placement struct {
	at      map[*BlitSource]int
	uploads []*BlitSource
	// transient regions are freed once the frame is committed.
	transient []Span
}

func newBlitHeap() *blitHeap {
	return &blitHeap{free: []Span{{0, HeapBytes}}, resident: map[weak.Pointer[BlitSource]]*heapEntry{}}
}

// place finds heap room for every source the frame reads. Sources already
// resident keep their place; the rest are uploads.
func (h *blitHeap) place(frame BlitFrame) (*placement, error) {
	var sources []*BlitSource
	seen := map[*BlitSource]bool{}
	need := 0
	for _, op := range frame.Ops {
		if !op.Kind.reads() || seen[op.Source] {
			continue
		}
		seen[op.Source] = true
		sources = append(sources, op.Source)
		need += alignHeap(op.Source.bytes())
	}
	if need > HeapBytes {
		return nil, ErrBlitTooLarge
	}
	h.frame++
	for key, entry := range h.resident {
		if key.Value() == nil {
			h.release(Span{entry.at, alignHeap(entry.bytes)})
			delete(h.resident, key)
		}
	}
	p := &placement{at: make(map[*BlitSource]int, len(sources))}
	for _, s := range sources {
		if s.Transient {
			continue
		}
		if entry := h.resident[weak.Make(s)]; entry != nil {
			entry.drawn = h.frame
			p.at[s] = entry.at
		}
	}
	for _, s := range sources {
		if _, ok := p.at[s]; ok {
			continue
		}
		at, ok := h.allocate(alignHeap(s.bytes()))
		if !ok {
			// Fragmented around this frame's own sources: start the heap
			// over and upload everything the frame reads.
			h.reset()
			return h.placeAll(sources), nil
		}
		p.at[s] = at
		p.uploads = append(p.uploads, s)
		if s.Transient {
			p.transient = append(p.transient, Span{at, alignHeap(s.bytes())})
		} else {
			h.resident[weak.Make(s)] = &heapEntry{at: at, bytes: s.bytes(), drawn: h.frame}
		}
	}
	return p, nil
}

func (h *blitHeap) placeAll(sources []*BlitSource) *placement {
	p := &placement{at: make(map[*BlitSource]int, len(sources))}
	at := 0
	for _, s := range sources {
		size := alignHeap(s.bytes())
		p.at[s] = at
		p.uploads = append(p.uploads, s)
		if s.Transient {
			p.transient = append(p.transient, Span{at, size})
		} else {
			h.resident[weak.Make(s)] = &heapEntry{at: at, bytes: s.bytes(), drawn: h.frame}
		}
		at += size
	}
	h.free = nil
	if at < HeapBytes {
		h.free = []Span{{at, HeapBytes - at}}
	}
	return p
}

// allocate takes first-fit room, evicting the least recently drawn
// sources that this frame does not read until it fits.
func (h *blitHeap) allocate(size int) (int, bool) {
	for {
		for i, f := range h.free {
			if f.Bytes < size {
				continue
			}
			if f.Bytes == size {
				h.free = slices.Delete(h.free, i, i+1)
			} else {
				h.free[i] = Span{f.Offset + size, f.Bytes - size}
			}
			return f.Offset, true
		}
		var oldest weak.Pointer[BlitSource]
		var victim *heapEntry
		for key, entry := range h.resident {
			if entry.drawn < h.frame && (victim == nil || entry.drawn < victim.drawn) {
				oldest, victim = key, entry
			}
		}
		if victim == nil {
			return 0, false
		}
		h.release(Span{victim.at, alignHeap(victim.bytes)})
		delete(h.resident, oldest)
	}
}

// release returns a range to the free list, joining its neighbours.
func (h *blitHeap) release(s Span) {
	i, _ := slices.BinarySearchFunc(h.free, s.Offset, func(f Span, at int) int { return f.Offset - at })
	h.free = slices.Insert(h.free, i, s)
	if i+1 < len(h.free) && h.free[i].Offset+h.free[i].Bytes == h.free[i+1].Offset {
		h.free[i].Bytes += h.free[i+1].Bytes
		h.free = slices.Delete(h.free, i+1, i+2)
	}
	if i > 0 && h.free[i-1].Offset+h.free[i-1].Bytes == h.free[i].Offset {
		h.free[i-1].Bytes += h.free[i].Bytes
		h.free = slices.Delete(h.free, i, i+1)
	}
}

// finish frees the frame's transient uploads once the runtime has run it.
func (h *blitHeap) finish(p *placement) {
	for _, s := range p.transient {
		h.release(s)
	}
}

// reset forgets every resident source.
func (h *blitHeap) reset() {
	h.free = []Span{{0, HeapBytes}}
	clear(h.resident)
}

// writeFrame uploads p's sources into heap (the heap's bytes in the window)
// and encodes frame into list (the list region). It returns the uploaded
// window ranges.
func writeFrame(heap, list []byte, frame BlitFrame, p *placement) []Span {
	spans := make([]Span, 0, len(p.uploads))
	for _, s := range p.uploads {
		at, row := p.at[s], s.row()
		for y := 0; y < s.H; y++ {
			dst, src := heap[at+y*row:at+(y+1)*row], s.Pix[y*s.Stride:y*s.Stride+row]
			if s.Coverage {
				copy(dst, src)
			} else {
				writeRow(dst, src)
			}
		}
		spans = append(spans, Span{HeapOffset + at, s.bytes()})
	}
	for i, op := range frame.Ops {
		var source uint32
		stride := 0
		if op.Kind.reads() {
			stride = op.Source.row()
			source = uint32(HeapOffset + p.at[op.Source] + op.SourceY*stride + op.SourceX*op.Source.pixelBytes())
		}
		encodeCommand(list[i*CommandBytes:], op, source, stride)
	}
	return uploadSpans(spans)
}
