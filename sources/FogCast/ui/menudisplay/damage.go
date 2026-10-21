package menudisplay

import "bytes"

const (
	// MaxDamageRects is the most rectangles one damaged present may name.
	MaxDamageRects = 16
	// damageAlign is one 32-byte cache line of pixels; the slot is written
	// and written back in whole lines.
	damageAlign = 8
	// damageGapRows lets nearby changed row bands share one rectangle.
	damageGapRows = 4
)

// Rect is a pixel rectangle inside the menu frame.
type Rect struct{ X, Y, W, H int }

// Damage names the rectangles that differ from the frame the runtime is
// showing as BaseSequence. The client writes only those, plus what the
// slot it draws into missed, into the granted window.
type Damage struct {
	BaseSequence uint32
	Rects        []Rect
}

// Pixels is the area the damage covers.
func (d Damage) Pixels() int {
	n := 0
	for _, r := range d.Rects {
		n += r.W * r.H
	}
	return n
}

// Diff returns the rectangles where next differs from prev, as at most
// MaxDamageRects row bands whose horizontal extent is aligned to the runtime
// store granule. Identical frames return nil. Both frames must be FrameBytes.
func Diff(prev, next []byte) []Rect {
	if len(prev) != FrameBytes || len(next) != FrameBytes {
		return []Rect{{0, 0, Width, Height}}
	}
	const chunk = damageAlign * 4
	var bands []Rect
	for y := 0; y < Height; y++ {
		row := y * Stride
		a, b := prev[row:row+Stride], next[row:row+Stride]
		if bytes.Equal(a, b) {
			continue
		}
		left := 0
		for bytes.Equal(a[left:left+chunk], b[left:left+chunk]) {
			left += chunk
		}
		right := Stride
		for bytes.Equal(a[right-chunk:right], b[right-chunk:right]) {
			right -= chunk
		}
		x0, x1 := left/4, right/4
		if n := len(bands); n > 0 && y-(bands[n-1].Y+bands[n-1].H) <= damageGapRows {
			last := &bands[n-1]
			lx1 := last.X + last.W
			last.X, last.W, last.H = min(last.X, x0), max(lx1, x1)-min(last.X, x0), y+1-last.Y
			continue
		}
		bands = append(bands, Rect{x0, y, x1 - x0, 1})
	}
	for len(bands) > MaxDamageRects {
		// Join the two neighbouring bands whose union adds the fewest pixels.
		best, cost := 0, -1
		for i := 0; i+1 < len(bands); i++ {
			u := union(bands[i], bands[i+1])
			if c := u.W*u.H - bands[i].W*bands[i].H - bands[i+1].W*bands[i+1].H; cost < 0 || c < cost {
				best, cost = i, c
			}
		}
		bands[best] = union(bands[best], bands[best+1])
		bands = append(bands[:best+1], bands[best+2:]...)
	}
	return bands
}

func union(a, b Rect) Rect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// valid reports whether the damage can be sent to the runtime.
func (d Damage) valid() bool {
	if d.BaseSequence == 0 || len(d.Rects) == 0 || len(d.Rects) > MaxDamageRects {
		return false
	}
	for _, r := range d.Rects {
		if r.X < 0 || r.Y < 0 || r.W <= 0 || r.H <= 0 || r.X+r.W > Width || r.Y+r.H > Height {
			return false
		}
	}
	return true
}
