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
