package menudisplay

import "encoding/binary"

// writeRect copies r from frame, premultiplied RGBA bytes, into slot, whose
// words are B, G, R, A bytes. Idle MENU scans them as XRGB8888 and ignores
// A; a session plane blends with A as premultiplied alpha. Both use Stride.
func writeRect(slot, frame []byte, r Rect) {
	for y := r.Y; y < r.Y+r.H; y++ {
		at := y*Stride + r.X*4
		writeRow(slot[at:at+r.W*4], frame[at:at+r.W*4])
	}
}

// writeRowScalar converts one row of RGBA pixels into B, G, R, A words.
func writeRowScalar(dst, src []byte) {
	for i := 0; i < len(src); i += 4 {
		v := binary.LittleEndian.Uint32(src[i:])
		binary.LittleEndian.PutUint32(dst[i:], (v&0xff)<<16|v&0xff00ff00|(v>>16)&0xff)
	}
}

var fullFrame = []Rect{{0, 0, Width, Height}}

// slotUpdate is what to write into a slot that holds the frame before
// prev, to bring it to a frame that differs from prev by damage: both
// frames' damage. Nil damage means the whole frame.
func slotUpdate(damage, prev []Rect) []Rect {
	if damage == nil || prev == nil {
		return fullFrame
	}
	rects := append(append(make([]Rect, 0, len(damage)+len(prev)), damage...), prev...)
	if len(rects) <= MaxDamageRects {
		return rects
	}
	return []Rect{bounds(rects)}
}

func bounds(rects []Rect) Rect {
	x0, y0, x1, y1 := Width, Height, 0, 0
	for _, r := range rects {
		x0, y0 = min(x0, r.X), min(y0, r.Y)
		x1, y1 = max(x1, r.X+r.W), max(y1, r.Y+r.H)
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}
