package rooms

import (
	"fmt"
	"image"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestSystemsControlsFitViewport(t *testing.T) {
	for _, viewport := range [][2]int{{1024, 600}, {1280, 720}} {
		w, h := viewport[0], viewport[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			r := newRoom(t, examplePack(t, "example.computers"), Options{Services: &fakeServices{}, Width: w, Height: h})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			f := stepUntil(t, r, func(f Frame) bool { return len(focusedHit(f, "setup")) == 1 })
			for _, hit := range f.Hits {
				if hit.ID == "setup" && (hit.X < 0 || hit.X+hit.W > float32(w) || hit.Y < 0 || hit.Y+hit.H > 124) {
					t.Fatalf("setup outside header: %+v", hit)
				}
			}
			if !r.Input("right") || !r.Input("select") {
				t.Fatal("header action no longer reachable by direction and confirm")
			}
			actions := r.TakeActions()
			if len(actions) != 1 || actions[0].Kind != ActionOpenRoom || actions[0].RoomID != "example.system" {
				t.Fatalf("add system actions %+v", actions)
			}
		})
	}
}

// Installed rooms use these public widgets independently of the built-in packs.
const visualGridFixture = `
local Grid = require "widgets.grid"
local cover = image.cover("grid-art")
local grid = Grid.new{
  id="grid", x=32, y=152, w=room.width-64, h=room.height-282,
  cell_w=200, cell_h=236, gap=24, items={{title="Grid title"}},
}
function draw()
  grid:draw{
    cover=function() return cover end,
    plate="#20242c", color="#edf3e9",
  }
end`

func visualGridRoom(t *testing.T, w, h int) *Instance {
	t.Helper()
	r := newRoom(t, memPack(t, "visual-grid", visualGridFixture, nil), Options{Width: w, Height: h})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	return r
}

// The selected card's ring must survive the first row and column's clip.
func TestGridFocusFitsClip(t *testing.T) {
	for _, viewport := range [][2]int{{1024, 600}, {1280, 720}} {
		t.Run(fmt.Sprintf("%dx%d", viewport[0], viewport[1]), func(t *testing.T) {
			r := visualGridRoom(t, viewport[0], viewport[1])
			f := stepUntil(t, r, func(f Frame) bool { return len(f.Hits) > 0 })
			hit := f.Hits[0]
			var clips []Op
			found := false
			for _, op := range f.Ops {
				switch op.Kind {
				case OpClipPush:
					clips = append(clips, op)
				case OpClipPop:
					clips = clips[:len(clips)-1]
				case OpRect:
					if op.X != hit.X-4 || op.Y != hit.Y-4 || op.W != hit.W+8 || op.H != hit.H+8 {
						continue
					}
					found = true
					for _, clip := range clips {
						if op.X < clip.X || op.Y < clip.Y || op.X+op.W > clip.X+clip.W || op.Y+op.H > clip.Y+clip.H {
							t.Fatalf("selected card ring is clipped: ring=%+v clip=%+v", op, clip)
						}
					}
				}
			}
			if !found {
				t.Fatal("selected card has no focus ring")
			}
		})
	}
}

// Cover contents and selection cannot change the title's opaque surface.
func TestGridLabelsKeepControlledSurface(t *testing.T) {
	r := visualGridRoom(t, 1024, 600)
	f := stepUntil(t, r, func(f Frame) bool { return len(f.Hits) > 0 })
	check := func(f Frame) {
		t.Helper()
		for i, op := range f.Ops {
			if op.Kind != OpText || op.Text != "Grid title" {
				continue
			}
			if i == 0 || op.Size < 22 {
				t.Fatalf("unreadable card title: %+v", op)
			}
			plate := f.Ops[i-1]
			th := float32(gfx.TextHeightWeight(op.Size, gfx.WeightBold))
			if plate.Kind != OpRect || plate.Color.A != 255 || op.X < plate.X || op.Y < plate.Y || op.X+float32(op.MaxW) > plate.X+plate.W || op.Y+th > plate.Y+plate.H {
				t.Fatalf("title lacks a controlled surface: title=%+v plate=%+v", op, plate)
			}
			if contrastRatio(op.Color, plate.Color) < 4.5 {
				t.Fatalf("title contrast changed with selected card: title=%+v plate=%+v", op, plate)
			}
			return
		}
		t.Fatal("missing card title")
	}
	check(f)
	r.DeliverCover("grid-art", image.NewRGBA(image.Rect(0, 0, 80, 120)), nil)
	f = stepUntil(t, r, func(f Frame) bool { return countKinds(f)[OpImage] > 0 })
	check(f)
}
