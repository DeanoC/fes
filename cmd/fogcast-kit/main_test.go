package main

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/fbgrid"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
	"github.com/DeanoC/FogCast/kitlauncher"
	"github.com/DeanoC/FogCast/remoteinput"
	"time"
)

func TestPadsSelftestReportsIdentityBeforeOpen(t *testing.T) {
	r := inputmap.IdentityRemapper()
	err := runPadsSelftest(r)
	if err == nil {
		return
	}
	if !strings.Contains(err.Error(), "Linux") && !strings.Contains(err.Error(), "gamepad") {
		t.Fatalf("err %v", err)
	}
}

func TestLoadKitRemapperDefaultsToIdentity(t *testing.T) {
	r, err := loadKitRemapper("", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Profile().Name != inputmap.NameIdentity {
		t.Fatalf("profile %q", r.Profile().Name)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if r.Apply(a).Code != remoteinput.ButtonA {
		t.Fatal("identity mutated A")
	}
}

func TestLoadKitRemapperFlagBeatsConfig(t *testing.T) {
	r, err := loadKitRemapper("swap-ab", "identity")
	if err != nil {
		t.Fatal(err)
	}
	if r.Profile().Name != inputmap.NameSwapAB {
		t.Fatalf("profile %q", r.Profile().Name)
	}
}

func TestModelGridUsesLiveGamesAndPages(t *testing.T) {
	games := make([]tenfoot.Game, 13)
	for i := range games {
		games[i] = tenfoot.Game{ID: "game-" + string(rune('a'+i)), Title: "Title " + string(rune('A'+i)), System: "snes", Launchable: true}
	}
	m := kitlauncher.Model{Games: games, Focus: 12, Connected: true, TargetReady: true, ControllerConnected: true}
	g := modelGrid(m, 640, 480, nil, nil, theme.Default())
	if len(g.Tiles) != 1 || g.Focus != 0 {
		t.Fatalf("page tiles=%d focus=%d", len(g.Tiles), g.Focus)
	}
	if g.Tiles[0].Name != "Title M" {
		t.Fatalf("tile %q", g.Tiles[0].Name)
	}
	if g.Header != "FOGCAST  ALL 13/13" || g.Footer == "" {
		t.Fatalf("header=%q footer=%q", g.Header, g.Footer)
	}
}

func TestModelGridFollowsTwoDimensionalFocus(t *testing.T) {
	games := make([]tenfoot.Game, 25)
	for i := range games {
		games[i] = tenfoot.Game{ID: "g", Title: "T", System: "snes", Launchable: true}
	}
	m := kitlauncher.Model{Games: games, Connected: true, TargetReady: true}
	right, _ := remoteinput.NormalizeGamepad("dpad-right", true)
	down, _ := remoteinput.NormalizeGamepad("dpad-down", true)
	now := time.Now()
	m.Input(right, now)
	g := modelGrid(m, 640, 480, nil, nil, theme.Default())
	if m.Focus != 1 || g.Focus != 1 || len(g.Tiles) != 12 {
		t.Fatalf("right focus=%d local=%d tiles=%d", m.Focus, g.Focus, len(g.Tiles))
	}
	m.Focus = 11
	m.Input(down, now)
	start, end := catalogPage(m.Focus, len(m.Games))
	g = modelGrid(m, 640, 480, nil, nil, theme.Default())
	if m.Focus != 15 || start != 12 || end != 24 || g.Focus != 3 || len(g.Tiles) != 12 {
		t.Fatalf("page-cross focus=%d page=%d:%d local=%d tiles=%d", m.Focus, start, end, g.Focus, len(g.Tiles))
	}
}

func TestExerciseNavGridSamplesHighlight(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseNavGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "right focus=1") || !strings.Contains(report, "page-cross focus=15 page=12:24") || !strings.Contains(report, "stick-right focus=13") {
		t.Fatalf("report %s", report)
	}
	if !strings.Contains(report, "selftest-nav PASS") {
		t.Fatalf("missing pass: %s", report)
	}
}

func TestCatalogPageAndPrefetchWindow(t *testing.T) {
	start, end := catalogPage(0, 25)
	if start != 0 || end != 12 {
		t.Fatalf("page0 %d:%d", start, end)
	}
	start, end = catalogPage(12, 25)
	if start != 12 || end != 24 {
		t.Fatalf("page1 %d:%d", start, end)
	}
	start, end = catalogPage(24, 25)
	if start != 24 || end != 25 {
		t.Fatalf("page2 %d:%d", start, end)
	}
}

func TestGameTileLeavesLogoEmptyUntilReady(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	cache := tenfoot.NewCoverCache()
	pres := tenfoot.Presentation{Presentation: &tenfoot.PresentationInfo{LogoID: handle}}
	tile := gameTile(tenfoot.Game{Title: "Sonic", System: "megadrive"}, cache, pres, theme.Default())
	if tile.Logo != nil {
		t.Fatal("uncached logo should keep text fallback")
	}
	if tile.Name != "Sonic" {
		t.Fatalf("name %q", tile.Name)
	}
	plain := gameTile(tenfoot.Game{Title: "Pong", System: "pong"}, cache, tenfoot.Presentation{}, theme.Default())
	if plain.Logo != nil {
		t.Fatal("text-fallback tile gained a logo")
	}
	if plain.Name != "Pong" {
		t.Fatalf("plain name %q", plain.Name)
	}
}

func TestGameTileLeavesCoverEmptyUntilCached(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	tile := gameTile(tenfoot.Game{Title: "Sonic", System: "megadrive", Cover: handle}, tenfoot.NewCoverCache(), tenfoot.Presentation{}, theme.Default())
	if tile.Cover != nil {
		t.Fatal("uncached cover should stay fallback")
	}
	if tile.CoverKind != fbgrid.CoverMissing {
		t.Fatalf("uncached kind %d", tile.CoverKind)
	}
	flat := gameTile(tenfoot.Game{Title: "Pong", System: "pong"}, tenfoot.NewCoverCache(), tenfoot.Presentation{}, theme.Default())
	if flat.Cover != nil {
		t.Fatal("missing handle should stay fallback")
	}
	if flat.CoverKind != fbgrid.CoverMissing {
		t.Fatalf("missing kind %d", flat.CoverKind)
	}
	pres := tenfoot.Presentation{Presentation: &tenfoot.PresentationInfo{CoverArtworkID: handle}}
	fromMeta := gameTile(tenfoot.Game{Title: "Sonic", System: "megadrive"}, tenfoot.NewCoverCache(), pres, theme.Default())
	if fromMeta.Cover != nil {
		t.Fatal("uncached presentation cover should stay fallback")
	}
	if fromMeta.CoverKind != fbgrid.CoverMissing {
		t.Fatalf("presentation kind %d", fromMeta.CoverKind)
	}
}

func TestModelFooterReportsConnectionBeforeController(t *testing.T) {
	m := kitlauncher.Model{Message: "Host unavailable - reconnecting", ControllerConnected: false}
	if got := modelFooter(m); got != "Host unavailable - reconnecting" {
		t.Fatalf("footer %q", got)
	}
	m = kitlauncher.Model{Connected: true, TargetReady: true, Message: "", ControllerConnected: false}
	if got := modelFooter(m); got != "Connect USB gamepad" {
		t.Fatalf("controller footer %q", got)
	}
	m = kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	if got := modelFooter(m); got != "A play | B detail | L/R shelf" {
		t.Fatalf("play footer %q", got)
	}
}

func TestModelGridShowsActiveShelfChrome(t *testing.T) {
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog([]tenfoot.Game{
		{ID: "pong", Title: "Pong", System: "pong", Launchable: true},
		{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true},
		{ID: "mario", Title: "Mario", System: "snes", Launchable: true},
	})
	r, _ := remoteinput.NormalizeGamepad("r", true)
	r2, _ := remoteinput.NormalizeGamepad("r", true)
	now := time.Now()
	m.Input(r, now)
	m.Input(r2, now)
	g := modelGrid(m, 640, 480, nil, nil, theme.Default())
	if m.Shelf != "megadrive" || len(g.Tiles) != 1 || g.Tiles[0].Name != "Sonic" {
		t.Fatalf("shelf=%q tiles=%d name=%v", m.Shelf, len(g.Tiles), g.Tiles)
	}
	if g.Header != "FOGCAST  MEGADRIVE 1/3" {
		t.Fatalf("header %q", g.Header)
	}
	if g.Footer != "A play | B detail | L/R shelf" {
		t.Fatalf("footer %q", g.Footer)
	}
}

func TestExerciseShelfGridCyclesVisibleSet(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseShelfGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-shelf PASS") || !strings.Contains(report, "shoulder-r-megadrive") || !strings.Contains(report, "header=\"FOGCAST  MEGADRIVE 5/15\"") {
		t.Fatalf("report %s", report)
	}
}

func TestGameTileUsesSystemPaletteAndASCIILabel(t *testing.T) {
	tile := gameTile(tenfoot.Game{Title: "Márío", System: "snes"}, nil, tenfoot.Presentation{}, theme.Default())
	if tile.Name != "M?r?o" {
		t.Fatalf("label %q", tile.Name)
	}
	if tile.Color != gfx.RGB(156, 52, 60) {
		t.Fatalf("color %+v", tile.Color)
	}
	arcade := gameTile(tenfoot.Game{Title: "Márío", System: "snes"}, nil, tenfoot.Presentation{}, theme.Arcade())
	if arcade.Color != theme.Arcade().SystemColor("snes") {
		t.Fatalf("arcade color %+v", arcade.Color)
	}
	if arcade.Color == tile.Color {
		t.Fatal("arcade palette must differ")
	}
}

func TestLoadKitThemeFlagBeatsConfigAndEnv(t *testing.T) {
	t.Setenv("FOGCAST_THEME", "night")
	th, err := loadKitTheme("arcade", "default")
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != theme.NameArcade {
		t.Fatalf("flag %q", th.Name)
	}
	th, err = loadKitTheme("", "night")
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != theme.NameNight {
		t.Fatalf("config %q", th.Name)
	}
	th, err = loadKitTheme("", "")
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != theme.NameNight {
		t.Fatalf("env %q", th.Name)
	}
}

func TestLoadKitThemeMissingFile(t *testing.T) {
	t.Setenv("FOGCAST_THEME", "")
	_, err := loadKitTheme("/no/such/theme.json", "")
	if err == nil {
		t.Fatal("missing theme")
	}
}

func TestExerciseFPGAAnimProof(t *testing.T) {
	report, err := anim.RunFPGAProof()
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-fpga PASS") || !strings.Contains(report, "HW=not-yet") || !strings.Contains(report, "backend=fpga") {
		t.Fatalf("report %s", report)
	}
}

func TestExerciseWheelGridPaintsHeroEntersAndNestsMotion(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseWheelGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-wheel PASS") || !strings.Contains(report, "selftest-motion PASS") || !strings.Contains(report, "enter-grid") {
		t.Fatalf("report %s", report)
	}
	if !strings.Contains(report, "hero=") || !strings.Contains(report, "back-wheel-megadrive") {
		t.Fatalf("missing wheel evidence: %s", report)
	}
}

func TestExerciseMotionGridPopsAndPulsesThenNestsDetail(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseMotionGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-motion PASS") || !strings.Contains(report, "selftest-detail PASS") || !strings.Contains(report, "selftest-attract PASS") || !strings.Contains(report, "selftest-nav PASS") {
		t.Fatalf("report %s", report)
	}
	if !strings.Contains(report, "motion pop-mid") || !strings.Contains(report, "motion confirm-mid") {
		t.Fatalf("missing motion evidence: %s", report)
	}
}

func TestExerciseDetailGridOpensPaintsAndNestsAttract(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseDetailGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-detail PASS") || !strings.Contains(report, "selftest-attract PASS") || !strings.Contains(report, "selftest-cover PASS") || !strings.Contains(report, "selftest-text PASS") || !strings.Contains(report, "selftest-nav PASS") || !strings.Contains(report, "selftest-shelf PASS") {
		t.Fatalf("report %s", report)
	}
	if !strings.Contains(report, "detail title-ink=1") || !strings.Contains(report, "detail cover=") || !strings.Contains(report, "detail logo=") || !strings.Contains(report, "logo=1") {
		t.Fatalf("missing detail paint evidence: %s", report)
	}
	if !strings.Contains(report, "meta=1") || !strings.Contains(report, "description=1") || !strings.Contains(report, "omit-empty=1") || !strings.Contains(report, "hedgehog=1") {
		t.Fatalf("missing detail meta evidence: %s", report)
	}
	if !strings.Contains(report, "video-preview=1") || !strings.Contains(report, "still-only=1") || !strings.Contains(report, "poster=1") || !strings.Contains(report, "detail video-preview") {
		t.Fatalf("missing detail video evidence: %s", report)
	}
}

func TestExerciseAttractGridPaintsStillAndDismisses(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseAttractGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-attract PASS") || !strings.Contains(report, "selftest-cover PASS") || !strings.Contains(report, "selftest-text PASS") || !strings.Contains(report, "selftest-nav PASS") || !strings.Contains(report, "selftest-shelf PASS") {
		t.Fatalf("report %s", report)
	}
	if !strings.Contains(report, "attract still=") || !strings.Contains(report, "empty-panel") {
		t.Fatalf("missing still/empty evidence: %s", report)
	}
}

func TestExerciseCoverGridPaintsArtAndPlaceholder(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseCoverGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-cover PASS") || !strings.Contains(report, "selftest-text PASS") || !strings.Contains(report, "selftest-nav PASS") || !strings.Contains(report, "selftest-shelf PASS") {
		t.Fatalf("report %s", report)
	}
}

func TestExerciseBoldGridNestsDetailAndText(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseBoldGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-bold PASS") || !strings.Contains(report, "selftest-detail PASS") || !strings.Contains(report, "selftest-text PASS") || !strings.Contains(report, "header-bold=1") {
		t.Fatalf("report %s", report)
	}
}

func TestExerciseTextGridUsesUIFace(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseTextGrid(d, theme.Default())
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-text PASS") || !strings.Contains(report, "header-not-debug=1") || !strings.Contains(report, "header-bold=1") || !strings.Contains(report, "selftest-shelf PASS") || !strings.Contains(report, "selftest-nav PASS") {
		t.Fatalf("report %s", report)
	}
}

func TestExerciseThemeGridSamplesBothLooks(t *testing.T) {
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	report, err := exerciseThemeGrid(d)
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	if !strings.Contains(report, "selftest-theme PASS") || !strings.Contains(report, "theme=default") || !strings.Contains(report, "theme=arcade") {
		t.Fatalf("report %s", report)
	}
	if !strings.Contains(report, "roles theme=default") || !strings.Contains(report, "compat scale-only") || !strings.Contains(report, "compat px-override") {
		t.Fatalf("missing type-role evidence: %s", report)
	}
}

func TestModelDetailFrameVideoPreviewVersusStillOnly(t *testing.T) {
	th := theme.Default()
	m := kitlauncher.Model{Connected: true, TargetReady: true, ControllerConnected: true}
	m.SetCatalog([]tenfoot.Game{
		{ID: "pong", Title: "Pong", System: "pong", Launchable: true},
	})
	now := time.Unix(1, 0)
	b, _ := remoteinput.NormalizeGamepad("b", true)
	m.Input(b, now)
	if !m.DetailOpen {
		t.Fatal("expected detail")
	}
	m.ApplyPresentation("pong", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{ScreenshotIDs: []string{strings.Repeat("aa", 32), strings.Repeat("bb", 32)}},
	})
	still := modelDetailFrame(m, nil, nil, th, 640, 480)
	if still.VideoBadge || strings.Contains(still.ShotCaption, "preview") {
		t.Fatalf("still-only frame %+v", still)
	}
	if still.ShotCaption != "1 / 2" {
		t.Fatalf("still caption %q", still.ShotCaption)
	}

	m.ApplyPresentation("pong", tenfoot.Presentation{
		Presentation: &tenfoot.PresentationInfo{
			VideoID:       strings.Repeat("ee", 32),
			ScreenshotIDs: []string{strings.Repeat("aa", 32), strings.Repeat("bb", 32)},
		},
	})
	video := modelDetailFrame(m, nil, nil, th, 640, 480)
	if !video.VideoBadge || video.ShotCaption != "preview 1 / 2" {
		t.Fatalf("video frame badge=%v caption=%q", video.VideoBadge, video.ShotCaption)
	}
	if previewCaption(0, 1) != "preview" || previewCaption(1, 3) != "preview 2 / 3" {
		t.Fatalf("previewCaption %q %q", previewCaption(0, 1), previewCaption(1, 3))
	}
}

func TestKitMetaLineUsesASCIISeparator(t *testing.T) {
	got := kitMetaLine("MEGADRIVE  \u00b7  1991  \u00b7  USA")
	if got != "MEGADRIVE | 1991 | USA" {
		t.Fatalf("kit meta %q", got)
	}
	if kitMetaLine("") != "" {
		t.Fatal("empty")
	}
}
