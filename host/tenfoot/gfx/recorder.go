package gfx

import (
	"fmt"
	"image"
)

// Call is one recorded Device operation.
type Call struct {
	Op     string
	Tex    Texture
	Src    *Rect
	Dst    Rect
	Color  Color
	Blend  BlendMode
	Text   string
	X, Y   int
	Scale  int
	Width  int
	Height int
}

// Recorder is an in-memory Device for tests. It does not draw pixels.
type Recorder struct {
	Calls     []Call
	next      uint64
	alive     map[uint64]image.Point
	updateErr error
	createErr error
}

// NewRecorder returns an empty recording backend.
func NewRecorder() *Recorder {
	return &Recorder{alive: map[uint64]image.Point{}}
}

// FailCreate makes the next CreateRGBA calls return err until cleared with nil.
func (r *Recorder) FailCreate(err error) { r.createErr = err }

// FailUpdate makes the next UpdateRGBA calls return err until cleared with nil.
func (r *Recorder) FailUpdate(err error) { r.updateErr = err }

// Alive reports whether tex is still allocated on this recorder.
func (r *Recorder) Alive(tex Texture) bool {
	if r == nil || tex.id == 0 {
		return false
	}
	_, ok := r.alive[tex.id]
	return ok
}

// AliveCount is the number of textures not yet Destroyed.
func (r *Recorder) AliveCount() int { return len(r.alive) }

// Ops returns the recorded operation names in order.
func (r *Recorder) Ops() []string {
	out := make([]string, len(r.Calls))
	for i, c := range r.Calls {
		out[i] = c.Op
	}
	return out
}

func (r *Recorder) record(c Call) {
	r.Calls = append(r.Calls, c)
}

func (r *Recorder) BeginFrame() { r.record(Call{Op: "BeginFrame"}) }

func (r *Recorder) Clear(c Color) { r.record(Call{Op: "Clear", Color: c}) }

func (r *Recorder) Present() { r.record(Call{Op: "Present"}) }

func (r *Recorder) CreateRGBA(img *image.RGBA) (Texture, error) {
	if img == nil {
		r.record(Call{Op: "CreateRGBA"})
		return Texture{}, fmt.Errorf("empty image")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	r.record(Call{Op: "CreateRGBA", Width: w, Height: h})
	if w < 1 || h < 1 || len(img.Pix) == 0 {
		return Texture{}, fmt.Errorf("empty image")
	}
	if r.createErr != nil {
		return Texture{}, r.createErr
	}
	r.next++
	id := r.next
	r.alive[id] = image.Pt(w, h)
	return Texture{id: id, w: w, h: h}, nil
}

func (r *Recorder) UpdateRGBA(tex Texture, img *image.RGBA) error {
	call := Call{Op: "UpdateRGBA", Tex: tex}
	if img != nil {
		b := img.Bounds()
		call.Width, call.Height = b.Dx(), b.Dy()
	}
	r.record(call)
	if !r.Alive(tex) {
		return fmt.Errorf("invalid texture")
	}
	if img == nil || len(img.Pix) == 0 {
		return fmt.Errorf("empty image")
	}
	b := img.Bounds()
	size := r.alive[tex.id]
	if b.Dx() != size.X || b.Dy() != size.Y {
		return fmt.Errorf("size mismatch")
	}
	if r.updateErr != nil {
		return r.updateErr
	}
	return nil
}

func (r *Recorder) Destroy(tex Texture) {
	r.record(Call{Op: "Destroy", Tex: tex})
	if tex.id == 0 {
		return
	}
	delete(r.alive, tex.id)
}

func (r *Recorder) FillRect(rect Rect, c Color) {
	r.record(Call{Op: "FillRect", Dst: rect, Color: c})
}

func (r *Recorder) Draw(tex Texture, src *Rect, dst Rect) {
	var srcCopy *Rect
	if src != nil {
		cp := *src
		srcCopy = &cp
	}
	r.record(Call{Op: "Draw", Tex: tex, Src: srcCopy, Dst: dst})
}

func (r *Recorder) SetBlend(mode BlendMode) {
	r.record(Call{Op: "SetBlend", Blend: mode})
}

func (r *Recorder) DebugText(x, y int, text string, scale int) {
	r.record(Call{Op: "DebugText", X: x, Y: y, Text: text, Scale: scale})
}

func (r *Recorder) Close() {
	r.record(Call{Op: "Close"})
	for id := range r.alive {
		delete(r.alive, id)
	}
}

var _ Device = (*Recorder)(nil)
