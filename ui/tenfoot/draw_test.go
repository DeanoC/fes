package tenfoot

import (
	"image"
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot/theme"
)

func testDrawGrid() Grid {
	var g Grid
	g.Layout(1280, 720)
	return g
}

func testDrawRGBA(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	img.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	return img
}

func TestPresentFrameParkKeepsPreviewDestroysCovers(t *testing.T) {
	rec := gfx.NewRecorder()
	cover, err := rec.CreateRGBA(testDrawRGBA(8, 8))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := rec.CreateRGBA(testDrawRGBA(16, 9))
	if err != nil {
		t.Fatal(err)
	}
	textures := map[string]gpuTexture{
		"sonic":   {tex: cover, w: 8, h: 8},
		"preview": {tex: preview, w: 16, h: 9},
	}
	labels := map[string]gpuTexture{}
	snap := Snapshot{
		GPUParked: true,
		Grid:      testDrawGrid(),
		Preview:   PreviewSnapshot{Live: true, Image: testDrawRGBA(16, 9), FrameSeq: 1},
	}
	if parked := presentFrame(rec, snap, textures, labels, false); !parked {
		t.Fatal("expected parked")
	}
	if _, ok := textures["sonic"]; ok {
		t.Fatal("cover texture should be destroyed while parked")
	}
	if _, ok := textures["preview"]; !ok {
		t.Fatal("preview texture must survive GPU park")
	}
	if rec.Alive(cover) {
		t.Fatal("cover GPU resource still alive")
	}
	if !rec.Alive(preview) {
		t.Fatal("preview GPU resource destroyed while parked")
	}
}

func TestPresentFrameUnparkDropsPreview(t *testing.T) {
	rec := gfx.NewRecorder()
	preview, err := rec.CreateRGBA(testDrawRGBA(16, 9))
	if err != nil {
		t.Fatal(err)
	}
	coverImg := testDrawRGBA(8, 8)
	textures := map[string]gpuTexture{
		"preview": {tex: preview, w: 16, h: 9},
	}
	labels := map[string]gpuTexture{}
	g := testDrawGrid()
	g.Count = 1
	snap := Snapshot{
		GPUParked: false,
		Grid:      g,
		Games:     []Game{{ID: "sonic", Title: "Sonic"}},
		Covers:    map[string]*image.RGBA{"sonic": coverImg},
	}
	if parked := presentFrame(rec, snap, textures, labels, true); parked {
		t.Fatal("expected unparked")
	}
	if _, ok := textures["preview"]; ok {
		t.Fatal("preview texture should drop on unpark")
	}
	if rec.Alive(preview) {
		t.Fatal("preview GPU resource survived unpark")
	}
	if _, ok := textures["sonic"]; !ok {
		t.Fatal("visible cover should upload after unpark")
	}
}

func TestPresentFrameAttractTearsDownOtherTextures(t *testing.T) {
	rec := gfx.NewRecorder()
	cover, err := rec.CreateRGBA(testDrawRGBA(8, 8))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := rec.CreateRGBA(testDrawRGBA(16, 9))
	if err != nil {
		t.Fatal(err)
	}
	attractImg := testDrawRGBA(32, 18)
	attract, err := rec.CreateRGBA(attractImg)
	if err != nil {
		t.Fatal(err)
	}
	textures := map[string]gpuTexture{
		"sonic":   {tex: cover, w: 8, h: 8},
		"preview": {tex: preview, w: 16, h: 9},
		"attract": {tex: attract, w: 32, h: 18, src: attractImg, seq: 3},
	}
	snap := Snapshot{
		Grid: testDrawGrid(),
		Attract: AttractSnapshot{
			Active:   true,
			Title:    "Attract",
			Image:    attractImg,
			FrameSeq: 3,
		},
	}
	if parked := presentFrame(rec, snap, textures, map[string]gpuTexture{}, true); !parked {
		t.Fatal("attract must leave the parked flag unchanged")
	}
	if _, ok := textures["sonic"]; ok {
		t.Fatal("cover should be destroyed on attract")
	}
	if _, ok := textures["preview"]; ok {
		t.Fatal("preview should be destroyed on attract entry")
	}
	if _, ok := textures["attract"]; !ok {
		t.Fatal("attract texture should remain")
	}
	if rec.Alive(cover) || rec.Alive(preview) {
		t.Fatal("non-attract GPU resources survived attract")
	}
	if !rec.Alive(attract) {
		t.Fatal("attract GPU resource destroyed")
	}
}

func TestPresentFrameLeavingAttractDropsStage(t *testing.T) {
	rec := gfx.NewRecorder()
	attract, err := rec.CreateRGBA(testDrawRGBA(32, 18))
	if err != nil {
		t.Fatal(err)
	}
	textures := map[string]gpuTexture{
		"attract": {tex: attract, w: 32, h: 18},
	}
	snap := Snapshot{Grid: testDrawGrid(), Attract: AttractSnapshot{Active: false}}
	_ = presentFrame(rec, snap, textures, map[string]gpuTexture{}, false)
	if _, ok := textures["attract"]; ok {
		t.Fatal("attract texture should drop when attract is inactive")
	}
	if rec.Alive(attract) {
		t.Fatal("attract GPU resource survived teardown")
	}
}

func TestAttractReuploadUsesUpdateWhenSizeMatches(t *testing.T) {
	rec := gfx.NewRecorder()
	img1 := testDrawRGBA(32, 18)
	img2 := testDrawRGBA(32, 18)
	attract, err := rec.CreateRGBA(img1)
	if err != nil {
		t.Fatal(err)
	}
	textures := map[string]gpuTexture{
		"attract": {tex: attract, w: 32, h: 18, src: img1, seq: 1},
	}
	snap := Snapshot{
		Grid: testDrawGrid(),
		Attract: AttractSnapshot{
			Active:   true,
			Image:    img2,
			FrameSeq: 2,
		},
	}
	_ = presentFrame(rec, snap, textures, map[string]gpuTexture{}, false)
	got := rec.Ops()
	var sawUpdate, sawDestroyAttract bool
	for _, op := range got {
		if op == "UpdateRGBA" {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Fatalf("expected UpdateRGBA for same-size attract frame, ops=%v", got)
	}
	if item, ok := textures["attract"]; !ok || !rec.Alive(item.tex) {
		t.Fatal("attract texture should be updated in place")
	}
	for _, c := range rec.Calls {
		if c.Op == "Destroy" && c.Tex == attract {
			sawDestroyAttract = true
		}
	}
	if sawDestroyAttract {
		t.Fatal("same-size attract frame should not destroy the stage texture")
	}
}

func TestParkEntryDestroysLabels(t *testing.T) {
	rec := gfx.NewRecorder()
	label, err := rec.CreateRGBA(testDrawRGBA(4, 4))
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]gpuTexture{"stale": {tex: label, w: 4, h: 4}}
	snap := Snapshot{GPUParked: true, Grid: testDrawGrid()}
	_ = presentFrame(rec, snap, map[string]gpuTexture{}, labels, false)
	if rec.Alive(label) {
		t.Fatal("park entry should destroy existing label textures")
	}
}

func TestUploadTextureRejectsEmpty(t *testing.T) {
	rec := gfx.NewRecorder()
	if _, err := uploadTexture(rec, image.NewRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Fatal("expected empty image error")
	}
}

func TestDrawFrameClearsWithThemeBackground(t *testing.T) {
	rec := gfx.NewRecorder()
	snap := Snapshot{Grid: testDrawGrid()}
	drawFrame(rec, snap, map[string]gpuTexture{}, map[string]gpuTexture{})
	if len(rec.Calls) < 2 || rec.Calls[0].Op != "BeginFrame" || rec.Calls[1].Op != "Clear" {
		t.Fatalf("ops %#v", rec.Ops())
	}
	if rec.Calls[1].Color != theme.Default().SofaBackground {
		t.Fatalf("default sofa clear %+v", rec.Calls[1].Color)
	}
	rec = gfx.NewRecorder()
	snap.Theme = theme.Arcade()
	drawFrame(rec, snap, map[string]gpuTexture{}, map[string]gpuTexture{})
	if rec.Calls[1].Color != theme.Arcade().SofaBackground {
		t.Fatalf("arcade sofa clear %+v", rec.Calls[1].Color)
	}
	if theme.Arcade().SofaBackground == theme.Default().SofaBackground {
		t.Fatal("arcade sofa must differ")
	}
}

func TestDrawAttractClearsWithTheme(t *testing.T) {
	rec := gfx.NewRecorder()
	snap := Snapshot{Grid: testDrawGrid(), Theme: theme.Arcade(), Attract: AttractSnapshot{Active: true}}
	drawAttract(rec, snap, map[string]gpuTexture{}, map[string]gpuTexture{})
	if len(rec.Calls) < 2 || rec.Calls[1].Op != "Clear" || rec.Calls[1].Color != theme.Arcade().AttractBackground {
		t.Fatalf("ops %#v color %+v", rec.Ops(), rec.Calls)
	}
}
