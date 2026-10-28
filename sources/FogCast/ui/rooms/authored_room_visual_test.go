package rooms

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
)

// The destination strip owns the bottom 130 pixels in these authored rooms.
// Keep their primary controls and map labels visible at both supported sizes.
func TestAuthoredRoomControlsFitViewport(t *testing.T) {
	for _, viewport := range [][2]int{{1024, 600}, {1280, 720}} {
		w, h := viewport[0], viewport[1]
		t.Run(fmt.Sprintf("systems-%dx%d", w, h), func(t *testing.T) {
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
		t.Run(fmt.Sprintf("world-%dx%d", w, h), func(t *testing.T) {
			r := newRoom(t, examplePack(t, "example.mario-world"), Options{Services: &fakeServices{}, Width: w, Height: h, ReducedMotion: true})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			f := stepUntil(t, r, func(f Frame) bool { return len(focusedHit(f, "world:")) == 8 })
			for _, hit := range f.Hits {
				if strings.HasPrefix(hit.ID, "world:") && (hit.X < 0 || hit.X+hit.W > float32(w) || hit.Y < 128 || hit.Y+hit.H > float32(h-130)) {
					t.Fatalf("map control outside content: %+v", hit)
				}
			}
			r.Activate("world:island")
			f = stepUntil(t, r, func(f Frame) bool {
				return r.Destination().RoomID == "example.mario-sports" && strings.Contains(frameText(f), "Sports Island")
			})
			for _, op := range f.Ops {
				if op.Kind == OpText && strings.Contains(op.Text, "Sports Island") {
					if op.Size < 22 || op.Y+float32(gfx.TextHeightWeight(op.Size, gfx.WeightBold)) > float32(h-130) {
						t.Fatalf("island label hidden by destination strip: %+v", op)
					}
				}
			}
		})
	}
}

// The selected grid card has a four-pixel focus ring outside its hit region.
// The grid's own clip must retain that ring at the first row and column.
func TestAuthoredGridFocusFitsClip(t *testing.T) {
	for _, viewport := range [][2]int{{1024, 600}, {1280, 720}} {
		for _, id := range []string{"example.console-snes", "example.mario-sports", "example.workbench"} {
			t.Run(fmt.Sprintf("%s-%dx%d", id, viewport[0], viewport[1]), func(t *testing.T) {
				game := hostclient.Game{ID: "mario-tennis", Title: "Mario Tennis", System: "snes", State: "available", RootOnline: true, Launchable: true}
				r := newRoom(t, examplePack(t, id), Options{Services: &fakeServices{games: []hostclient.Game{game}}, Width: viewport[0], Height: viewport[1]})
				if err := r.Load(); err != nil {
					t.Fatal(err)
				}
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
}

// Labels have their own opaque surface even when cover art arrives, so the
// selected card's accent and the image contents cannot change title contrast.
func TestAuthoredGridLabelsKeepControlledSurface(t *testing.T) {
	for _, id := range []string{"example.console-snes", "example.mario-sports", "example.workbench"} {
		t.Run(id, func(t *testing.T) {
			game := hostclient.Game{ID: "mario-tennis", Title: "Mario Tennis", System: "snes", State: "available", RootOnline: true, Launchable: true}
			r := newRoom(t, examplePack(t, id), Options{Services: &fakeServices{games: []hostclient.Game{game}}, Width: 1024, Height: 600})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), game.Title) && len(f.Hits) > 0 })
			check := func(f Frame) {
				t.Helper()
				for i, op := range f.Ops {
					if op.Kind != OpText || op.Text != game.Title {
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
			r.DeliverCover(game.ID, image.NewRGBA(image.Rect(0, 0, 80, 120)), nil)
			f = stepUntil(t, r, func(f Frame) bool { return countKinds(f)[OpImage] > 0 })
			check(f)
		})
	}
}

func TestTMSCoverCannotCoverGameRows(t *testing.T) {
	for _, w := range []int{1024, 1280} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			game := hostclient.Game{ID: "coleco-dk", Title: "Donkey Kong", System: "coleco", State: "available", RootOnline: true, Launchable: true}
			svc := &fakeServices{games: []hostclient.Game{game}, platforms: []hostclient.Platform{{ID: "coleco", Label: "ColecoVision", Tags: []string{"vdp:tms9918-family"}}}}
			r := newRoom(t, examplePack(t, "example.tms-vdp"), Options{Services: svc, Width: w, Height: 600})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			stepUntil(t, r, func(f Frame) bool { return len(focusedHit(f, "game:")) == 1 })
			r.DeliverCover(game.ID, image.NewRGBA(image.Rect(0, 0, 80, 120)), nil)
			// A step drains the delivered cover before drawing the next complete frame.
			f := r.Step(time.Unix(2, 0))
			if r.Err() != nil {
				t.Fatal(r.Err())
			}
			for _, op := range f.Ops {
				if op.Kind != OpImage {
					continue
				}
				for _, hit := range f.Hits {
					if strings.HasPrefix(hit.ID, "game:") && op.X < hit.X+hit.W && op.X+op.W > hit.X && op.Y < hit.Y+hit.H && op.Y+op.H > hit.Y {
						t.Fatalf("cover overlays game row: image=%+v row=%+v", op, hit)
					}
				}
			}
			if w >= 1180 && countKinds(f)[OpImage] == 0 {
				t.Fatal("wide layout lost its reserved cover column")
			}
		})
	}
}
