// SPDX-License-Identifier: GPL-3.0-or-later
package scanout

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"sync"
	"testing"
)

type window struct {
	bytes             []byte
	writes, publishes int
	failWrite         int
	failPublish       bool
	offsets           []uint64
	lengths           []int
}

func (m *window) Size() uint64 { return uint64(len(m.bytes)) }
func (m *window) Write(offset uint64, src []byte) error {
	m.writes++
	m.offsets = append(m.offsets, offset)
	m.lengths = append(m.lengths, len(src))
	if offset > uint64(len(m.bytes)) || uint64(len(src)) > uint64(len(m.bytes))-offset {
		panic("outside reserved memory")
	}
	if m.writes == m.failWrite {
		copy(m.bytes[int(offset):], src[:len(src)/2])
		return errors.New("partial write")
	}
	copy(m.bytes[int(offset):], src)
	return nil
}
func (m *window) Publish() error {
	m.publishes++
	if m.failPublish {
		return errors.New("publication unknown")
	}
	return nil
}
func TestPixelOrderStrideAndSubimage(t *testing.T) {
	parent := image.NewRGBA(image.Rect(0, 0, 15, 20))
	frame := parent.SubImage(image.Rect(3, 4, 10, 13)).(*image.RGBA)
	for y := 4; y < 13; y++ {
		for x := 3; x < 10; x++ {
			frame.SetRGBA(x, y, color.RGBA{R: byte(x * 11), G: byte(y * 17), B: byte(x + y), A: 128})
		}
	}
	m := &window{bytes: bytes.Repeat([]byte{0xaa}, 40*9+64)}
	s, err := New(7, 9, 40, m)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Present(frame); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 9; y++ {
		for x := 0; x < 7; x++ {
			pixel := frame.RGBAAt(x+3, y+4)
			want := []byte{pixel.B, pixel.G, pixel.R, 0xff}
			if got := m.bytes[y*40+x*4 : y*40+x*4+4]; !bytes.Equal(got, want) {
				t.Fatalf("pixel (%d,%d)=%x want=%x", x, y, got, want)
			}
		}
		if !bytes.Equal(m.bytes[y*40+28:y*40+40], bytes.Repeat([]byte{0xaa}, 12)) {
			t.Fatal("padding overwritten")
		}
	}
	if !bytes.Equal(m.bytes[40*9:], bytes.Repeat([]byte{0xaa}, 64)) {
		t.Fatal("bytes past frame overwritten")
	}
	snapshot := bytes.Clone(m.bytes)
	clear(frame.Pix)
	if !bytes.Equal(m.bytes, snapshot) {
		t.Fatal("caller frame retained as scanout")
	}
	if m.publishes != 1 {
		t.Fatal("CPU stores not published exactly once")
	}
}
func TestBoundedContiguousChunks(t *testing.T) {
	m := &window{bytes: make([]byte, 1280*720*4)}
	s, err := New(1280, 720, 5120, m)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Present(image.NewRGBA(image.Rect(0, 0, 1280, 720))); err != nil {
		t.Fatal(err)
	}
	if m.writes != 180 {
		t.Fatal("unbounded/incorrect row chunking", m.writes)
	}
	for i, length := range m.lengths {
		if length != 20480 || m.offsets[i] != uint64(i*20480) {
			t.Fatal("noncontiguous copy")
		}
	}
}
func TestInvalidFramesDoNotTouchMemory(t *testing.T) {
	m := &window{bytes: make([]byte, 16*6)}
	s, err := New(4, 6, 16, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range []*image.RGBA{nil, image.NewRGBA(image.Rect(0, 0, 3, 6)), {Pix: make([]byte, 90), Stride: 16, Rect: image.Rect(0, 0, 4, 6)}, {Pix: make([]byte, 100), Stride: -1, Rect: image.Rect(0, 0, 4, 6)}, {Pix: make([]byte, 100), Stride: 1 << 30, Rect: image.Rect(0, 0, 4, 6)}} {
		if err := s.Present(frame); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
	if m.writes != 0 || m.publishes != 0 {
		t.Fatal("invalid input touched scanout")
	}
	if err := s.Present(image.NewRGBA(image.Rect(0, 0, 4, 6))); err != nil {
		t.Fatal("argument rejection poisoned a valid surface", err)
	}
	for _, g := range [][3]int{{0, 6, 16}, {4, 0, 16}, {4, 6, 15}, {4, 7, 16}, {4097, 1, 16388}} {
		if _, err := New(g[0], g[1], g[2], m); err == nil {
			t.Fatal("invalid memory/geometry", g)
		}
	}
}
func TestAmbiguousFailureRetainsWindowAndStopsWrites(t *testing.T) {
	for _, publication := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial-copy", true: "publication"}[publication], func(t *testing.T) {
			m := &window{bytes: bytes.Repeat([]byte{0xaa}, 32*9), failWrite: 2, failPublish: publication}
			if publication {
				m.failWrite = 0
			}
			s, err := New(8, 9, 32, m)
			if err != nil {
				t.Fatal(err)
			}
			frame := image.NewRGBA(image.Rect(0, 0, 8, 9))
			if err = s.Present(frame); err == nil {
				t.Fatal("ambiguous memory completion accepted")
			}
			snapshot := bytes.Clone(m.bytes)
			writes, publishes := m.writes, m.publishes
			if err = s.Present(frame); err == nil {
				t.Fatal("failed surface reused")
			}
			if writes != m.writes || publishes != m.publishes || !bytes.Equal(snapshot, m.bytes) {
				t.Fatal("failed window modified/reclaimed")
			}
		})
	}
}
func TestConcurrentFramesSerialize(t *testing.T) {
	m := &window{bytes: make([]byte, 16*9)}
	s, err := New(4, 9, 16, m)
	if err != nil {
		t.Fatal(err)
	}
	frames := []*image.RGBA{image.NewRGBA(image.Rect(0, 0, 4, 9)), image.NewRGBA(image.Rect(0, 0, 4, 9))}
	for n, f := range frames {
		for i := range f.Pix {
			f.Pix[i] = byte(n + 10)
		}
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for _, f := range frames {
		wg.Add(1)
		go func(f *image.RGBA) { defer wg.Done(); failures <- s.Present(f) }(f)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, offset := range m.offsets {
		if offset != uint64(i%3*64) {
			t.Fatal("frame copies interleaved", m.offsets)
		}
	}
	first := m.bytes[0]
	for i, v := range m.bytes {
		if (i%4 == 3 && v != 0xff) || (i%4 != 3 && v != first) {
			t.Fatal("final frame mixes callers")
		}
	}
}

func TestPresentRectSubimageStrideBoundsFaultAndStats(t *testing.T) {
	const w, h, stride = 8, 6, 40 // 8 pixels * 4 = 32 bytes/row + 8 bytes firmware padding
	m := &window{bytes: make([]byte, h*stride)}
	for i := range m.bytes {
		m.bytes[i] = 0xaa
	}
	s, err := New(w, h, stride, m)
	if err != nil {
		t.Fatal(err)
	}

	// Empty rect validates frame without writing or publishing.
	full := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := s.PresentRect(full, image.Rectangle{}); err != nil {
		t.Fatalf("empty rect: %v", err)
	}
	if m.writes != 0 || m.publishes != 0 {
		t.Fatalf("empty rect touched window: writes=%d publishes=%d", m.writes, m.publishes)
	}

	// Out-of-bounds rect rejects cleanly and increments RejectedValidation.
	if err := s.PresentRect(full, image.Rect(4, 2, 9, 4)); err == nil {
		t.Fatal("expected out-of-bounds rect error")
	}
	if err := s.PresentRect(nil, image.Rect(0, 0, 2, 2)); err == nil {
		t.Fatal("expected nil frame error")
	}
	if st := s.Stats(); st.RejectedValidation != 2 || st.EmptyRects != 1 {
		t.Fatalf("stats after validation/empty=%+v", st)
	}

	// Initial full frame present (solid black XRGB: 0,0,0,0xff).
	if err := s.Present(full); err != nil {
		t.Fatal(err)
	}
	writesAfterFull := m.writes
	publishesAfterFull := m.publishes

	// Dirty rect update from a padded subimage with non-zero Rect.Min.
	backing := image.NewRGBA(image.Rect(0, 0, w+6, h+4))
	sub := backing.SubImage(image.Rect(3, 2, 3+w, 2+h)).(*image.RGBA)
	dirty := image.Rect(2, 1, 5, 4) // width=3, height=3
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := y*sub.Stride + x*4
			if x >= dirty.Min.X && x < dirty.Max.X && y >= dirty.Min.Y && y < dirty.Max.Y {
				sub.Pix[off+0] = byte(10 + x)
				sub.Pix[off+1] = byte(20 + y)
				sub.Pix[off+2] = byte(30 + x + y)
				sub.Pix[off+3] = 0x11
			} else {
				sub.Pix[off+0] = 0xee
				sub.Pix[off+1] = 0xdd
				sub.Pix[off+2] = 0xcc
				sub.Pix[off+3] = 0x99
			}
		}
	}
	if err := s.PresentRect(sub, dirty); err != nil {
		t.Fatalf("PresentRect subimage: %v", err)
	}
	if m.publishes != publishesAfterFull+1 {
		t.Fatalf("publishes=%d want %d", m.publishes, publishesAfterFull+1)
	}
	if m.writes-writesAfterFull != dirty.Dy() {
		t.Fatalf("partial rect writes=%d want %d per-row writes", m.writes-writesAfterFull, dirty.Dy())
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := y*stride + x*4
			if x >= dirty.Min.X && x < dirty.Max.X && y >= dirty.Min.Y && y < dirty.Max.Y {
				r, g, b, xrgb := byte(10+x), byte(20+y), byte(30+x+y), byte(0xff)
				if m.bytes[off] != b || m.bytes[off+1] != g || m.bytes[off+2] != r || m.bytes[off+3] != xrgb {
					t.Fatalf("pixel (%d,%d) got [%x %x %x %x], want [%x %x %x %x]", x, y, m.bytes[off], m.bytes[off+1], m.bytes[off+2], m.bytes[off+3], b, g, r, xrgb)
				}
			} else if m.bytes[off] != 0 || m.bytes[off+1] != 0 || m.bytes[off+2] != 0 || m.bytes[off+3] != 0xff {
				t.Fatalf("outside pixel (%d,%d) modified: %v", x, y, m.bytes[off:off+4])
			}
		}
		for pad := w * 4; pad < stride; pad++ {
			if m.bytes[y*stride+pad] != 0xaa {
				t.Fatalf("row %d padding byte %d corrupted: %#x", y, pad, m.bytes[y*stride+pad])
			}
		}
	}

	wantBytes := uint64(w*h*4 + dirty.Dx()*dirty.Dy()*4)
	if st := s.Stats(); st.FramesPresented != 1 || st.RectsPresented != 1 || st.EmptyRects != 1 || st.BytesWritten != wantBytes || st.Faulted {
		t.Fatalf("stats=%+v want bytes=%d", st, wantBytes)
	}

	// Steady-state PresentRect must not heap-allocate.
	const allocationRuns = 20
	// AllocsPerRun performs one warm-up call before its measured runs. Reserve
	// enough space for every logged row write so the window fixture doesn't
	// contribute allocations to the surface measurement.
	logCapacity := (allocationRuns + 1) * dirty.Dy()
	m.offsets = make([]uint64, 0, logCapacity)
	m.lengths = make([]int, 0, logCapacity)
	if allocs := testing.AllocsPerRun(allocationRuns, func() {
		if err := s.PresentRect(sub, dirty); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("PresentRect allocated %.1f times per call", allocs)
	}

	// Write fault poisons the surface and sets Faulted in Stats.
	m.failWrite = m.writes + 1
	if err := s.PresentRect(sub, dirty); err == nil {
		t.Fatal("expected write fault")
	}
	if !s.Stats().Faulted {
		t.Fatal("expected Faulted=true in Stats")
	}
	if err := s.PresentRect(sub, image.Rectangle{}); err == nil {
		t.Fatal("expected retained fault on empty rect after poison")
	}
}
