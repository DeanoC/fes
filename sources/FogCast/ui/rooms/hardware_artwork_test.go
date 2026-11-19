package rooms

import (
	"testing"
	"time"
)

func hasZX81Artwork(f Frame) bool {
	for _, op := range f.Ops {
		if op.Kind == OpImage && op.Image == "asset:assets/zx81.png" {
			return true
		}
	}
	return false
}

// Artwork decodes on a background goroutine and can become ready after the
// first picture is on screen. Its repaint must use the same staged upper and
// lower draws as the cold picture rather than rebuilding both at once.
func TestHardwareRoomArtworkReadyStagesRepaint(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := openWorkshop(t, w, h, false)
	art := r.images["asset:assets/zx81.png"]
	if art == nil || art.Img == nil || !art.Ready {
		t.Fatal("workbench artwork did not load")
	}
	now := time.Unix(7000, 0)
	art.Ready = false
	r.Step(now)
	now = now.Add(16 * time.Millisecond)
	r.Step(now)
	wm := &workMeter{}
	r.work = wm
	art.Ready = true
	now = now.Add(16 * time.Millisecond)
	upper := r.Step(now)
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if !frameHas(upper, "Setup preview") || !frameHas(upper, "Import expansion") {
		t.Fatal("artwork completion did not keep the complete previous picture")
	}
	for _, c := range wm.calls {
		if c.kind == "draw" && c.instructions > 7000 {
			t.Fatalf("artwork upper draw ran %d Lua instructions, want <= 7000", c.instructions)
		}
	}
	wm.calls = nil
	now = now.Add(16 * time.Millisecond)
	full := r.Step(now)
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if !frameHas(full, "Import expansion") || !frameHas(full, "Setup preview") {
		t.Fatal("staged lower picture did not complete")
	}
	if !hasZX81Artwork(full) {
		t.Fatal("completed picture did not use the decoded artwork")
	}
	for _, c := range wm.calls {
		if c.kind == "draw" && c.instructions > 7000 {
			t.Fatalf("artwork lower draw ran %d Lua instructions, want <= 7000", c.instructions)
		}
	}
}

func TestHardwareRoomArtworkRepaintRestartsAfterFocusReturns(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := openWorkshop(t, w, h, false)
	art := r.images["asset:assets/zx81.png"]
	if art == nil || art.Img == nil || !art.Ready {
		t.Fatal("workbench artwork did not load")
	}
	now := time.Unix(10000, 0)
	art.Ready = false
	r.Step(now)
	r.Step(now.Add(16 * time.Millisecond))
	art.Ready = true
	r.Step(now.Add(32 * time.Millisecond))
	r.Hover("back")
	r.Step(now.Add(48 * time.Millisecond)) // discard the unfinished scene
	r.Hover("socket")                      // return to its focus
	r.Step(now.Add(64 * time.Millisecond))
	full := r.Step(now.Add(80 * time.Millisecond))
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if !hasZX81Artwork(full) || !frameHas(full, "Import expansion") {
		t.Fatal("returning to the staged focus kept the old picture without artwork")
	}
}

func TestHardwareRoomArtworkAndFocusChangeRedrawTogether(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := openWorkshop(t, w, h, false)
	art := r.images["asset:assets/zx81.png"]
	if art == nil || art.Img == nil || !art.Ready {
		t.Fatal("workbench artwork did not load")
	}
	now := time.Unix(11000, 0)
	art.Ready = false
	r.Step(now)
	r.Step(now.Add(16 * time.Millisecond))
	art.Ready = true
	r.Hover("back")
	redrawn := r.Step(now.Add(32 * time.Millisecond))
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if !hasZX81Artwork(redrawn) || !workshopText(t, redrawn, "Back").Bold || !frameHas(redrawn, "Import expansion") {
		t.Fatal("artwork and focus change did not appear together")
	}
}

func TestHardwareRoomArtworkRepaintKeepsVisibleControls(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := openWorkshop(t, w, h, false)
	art := r.images["asset:assets/zx81.png"]
	if art == nil || art.Img == nil || !art.Ready {
		t.Fatal("workbench artwork did not load")
	}
	now := time.Unix(8000, 0)
	art.Ready = false
	r.Step(now)
	r.Step(now.Add(16 * time.Millisecond))
	art.Ready = true
	r.Step(now.Add(32 * time.Millisecond))
	// The still-visible old picture must keep its controls until the new
	// picture is presented; an upper-only control table would lose this.
	r.Activate("power")
	if actions := r.TakeActions(); len(actions) != 1 || actions[0].Kind != ActionLaunch {
		t.Fatalf("visible Start machine lost its action while staging artwork: %+v", actions)
	}
}

func TestHardwareRoomArtworkRepaintDiscardsStaleScene(t *testing.T) {
	w, h := workshopKitSize(1280, 720)
	r := openWorkshop(t, w, h, false)
	art := r.images["asset:assets/zx81.png"]
	if art == nil || art.Img == nil || !art.Ready {
		t.Fatal("workbench artwork did not load")
	}
	now := time.Unix(9000, 0)
	art.Ready = false
	r.Step(now)
	r.Step(now.Add(16 * time.Millisecond))
	art.Ready = true
	r.Step(now.Add(32 * time.Millisecond))
	r.Hover("back")
	r.Step(now.Add(48 * time.Millisecond))
	redrawn := r.Step(now.Add(64 * time.Millisecond))
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if !workshopText(t, redrawn, "Back").Bold || !frameHas(redrawn, "Import expansion") {
		t.Fatal("staged stale picture replaced the new focus")
	}
}
