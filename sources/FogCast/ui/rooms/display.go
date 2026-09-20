// Package rooms runs creator-defined menu "rooms": sandboxed Lua scripts that
// describe a themed browse/launch screen. A room never touches a gfx.Device;
// each frame it appends primitives to a Frame that the launcher replays, so
// the same script works on every backend and hit-testing reads the frame.
package rooms

import "github.com/DeanoC/FogCast/ui/gfx"

// OpKind is one display-list primitive.
type OpKind uint8

const (
	OpRect OpKind = iota + 1
	OpImage
	OpText
	OpClipPush
	OpClipPop
	OpBlend
)

// Op is one recorded draw call in logical pixels.
type Op struct {
	Kind  OpKind
	X, Y  float32
	W, H  float32
	Color gfx.Color

	// Image is the handle key for OpImage; Src is an optional source crop.
	Image string
	Src   *gfx.Rect

	// Text fields for OpText. MaxW 0 means unbounded; Align is left, center
	// or right and is resolved against X by the replayer.
	Text  string
	Size  int
	Bold  bool
	MaxW  int
	Align string

	Blend gfx.BlendMode
}

// Hit is a pointer region registered by the script for one frame.
type Hit struct {
	ID         string
	X, Y, W, H float32
}

// Frame is everything a room drew this tick.
type Frame struct {
	Clear    gfx.Color
	HasClear bool
	Ops      []Op
	Hits     []Hit
}

// HitAt returns the topmost (last registered) hit region containing (x, y).
func (f Frame) HitAt(x, y float32) (Hit, bool) {
	for i := len(f.Hits) - 1; i >= 0; i-- {
		h := f.Hits[i]
		if x >= h.X && y >= h.Y && x < h.X+h.W && y < h.Y+h.H {
			return h, true
		}
	}
	return Hit{}, false
}

// Action is a request a script made of the launcher during the last call.
type Action struct {
	Kind       ActionKind
	GameID     string
	RoomID     string
	Platform   string
	Collection string
	Layout     string
}

// ActionKind names a launcher request.
type ActionKind uint8

const (
	ActionLaunch ActionKind = iota + 1
	ActionOpenRoom
	ActionBack
	ActionOpenLibrary
)
