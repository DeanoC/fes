package controller

import (
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestMouseSYNAtomicAndBounds(t *testing.T) {
	m := NewMouseMapper()
	for _, v := range [][3]int32{{2, 0, 11}, {2, 1, -7}, {1, 272, 1}, {1, 273, 1}, {2, 8, 99}} {
		if _, ok := m.Map(uint16(v[0]), uint16(v[1]), v[2]); ok {
			t.Fatal("partial packet")
		}
	}
	e, ok := m.Map(0, 0, 0)
	x, y, b, valid := remoteinput.MouseVector(e)
	if !ok || !valid || x != 11 || y != -7 || b != 3 {
		t.Fatalf("%+v", e)
	}
	if _, ok := m.Map(0, 0, 0); ok {
		t.Fatal("duplicate SYN")
	}
	m.Map(2, 0, 32767)
	m.Map(2, 0, 1)
	if _, ok := m.Map(0, 0, 0); ok || !m.mouseFault {
		t.Fatal("overflow silently wrapped")
	}
}
