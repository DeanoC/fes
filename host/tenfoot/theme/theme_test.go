package theme

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

func TestBuiltinNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", NameDefault, "DEFAULT"} {
		th, ok := Builtin(name)
		if !ok || th.Name != NameDefault {
			t.Fatalf("builtin %q = %+v ok=%v", name, th, ok)
		}
	}
	arcade, ok := Builtin(NameArcade)
	if !ok || arcade.Name != NameArcade {
		t.Fatal("arcade")
	}
	night, ok := Builtin(NameNight)
	if !ok || night.Name != NameNight {
		t.Fatal("night")
	}
	if _, ok := Builtin("missing"); ok {
		t.Fatal("missing should fail")
	}
}

func TestDefaultPreservesKitTokens(t *testing.T) {
	t.Parallel()
	th := Default()
	if th.Background != gfx.RGB(16, 16, 24) {
		t.Fatalf("bg %+v", th.Background)
	}
	if th.Highlight != gfx.RGB(255, 220, 0) {
		t.Fatalf("highlight %+v", th.Highlight)
	}
	if th.Flash != gfx.RGB(255, 255, 255) {
		t.Fatalf("flash %+v", th.Flash)
	}
	if th.LabelBar != gfx.RGB(8, 8, 12) {
		t.Fatalf("label bar %+v", th.LabelBar)
	}
	if th.SofaBackground != gfx.RGB(12, 14, 20) {
		t.Fatalf("sofa %+v", th.SofaBackground)
	}
	if th.AttractBackground != gfx.RGB(8, 8, 12) {
		t.Fatalf("attract %+v", th.AttractBackground)
	}
	if th.SystemColor("snes") != gfx.RGB(156, 52, 60) {
		t.Fatalf("snes %+v", th.SystemColor("snes"))
	}
	if th.SystemColor("MegaDrive") != gfx.RGB(44, 96, 156) {
		t.Fatalf("megadrive %+v", th.SystemColor("MegaDrive"))
	}
	if th.SystemColor("pong") != gfx.RGB(196, 148, 36) {
		t.Fatalf("pong %+v", th.SystemColor("pong"))
	}
	if th.SystemColor("unknown") != gfx.RGB(84, 76, 132) {
		t.Fatalf("fallback %+v", th.SystemColor("unknown"))
	}
	if th.Pad != 16 || th.Gap != 8 || th.Border != 4 || th.HeaderH != 36 || th.FooterH != 28 {
		t.Fatalf("spacing %+v", th)
	}
	if th.HeaderScale != 2 || th.LabelScale != 1 || th.StatusScale != 2 {
		t.Fatalf("legacy scales %+v", th)
	}
	if th.TitlePx() != 20 || th.BodyPx() != 13 || th.CaptionPx() != 12 || th.StatusPx() != 14 {
		t.Fatalf("type roles title=%d body=%d caption=%d status=%d", th.TitlePx(), th.BodyPx(), th.CaptionPx(), th.StatusPx())
	}
	if th.Transition != "curtain" {
		t.Fatalf("default transition %q", th.Transition)
	}
	if th.TitleWeight() != gfx.WeightBold || th.HeaderWeight() != gfx.WeightBold {
		t.Fatalf("default title/header weight %s/%s", th.TitleWeight(), th.HeaderWeight())
	}
	if th.BodyWeight() != gfx.WeightRegular || th.CaptionWeight() != gfx.WeightRegular || th.StatusWeight() != gfx.WeightRegular {
		t.Fatalf("default body/caption/status should stay regular")
	}
}

func TestArcadeDiffersFromDefault(t *testing.T) {
	t.Parallel()
	d, a := Default(), Arcade()
	if d.Equal(a) {
		t.Fatal("arcade must differ")
	}
	if a.Highlight == d.Highlight || a.Background == d.Background || a.Flash == d.Flash {
		t.Fatalf("arcade tokens overlap default: hl=%+v bg=%+v flash=%+v", a.Highlight, a.Background, a.Flash)
	}
	if a.SystemColor("snes") == d.SystemColor("snes") {
		t.Fatal("arcade snes palette")
	}
	if a.HeaderBar == d.HeaderBar || a.CoverFrameWidth == 0 {
		t.Fatal("arcade chrome")
	}
	if a.Transition != "glitch" || Night().Transition != "wipe" {
		t.Fatalf("pack transitions arcade=%q night=%q", a.Transition, Night().Transition)
	}
}

func TestCompleteFillsMissingTokens(t *testing.T) {
	t.Parallel()
	if !(Theme{}).Complete().Equal(Default()) {
		t.Fatal("zero theme should complete to default including type roles")
	}
	th := Theme{Highlight: gfx.RGB(0, 255, 0)}.Complete()
	if th.Name != NameDefault {
		t.Fatalf("name %q", th.Name)
	}
	if th.Highlight != gfx.RGB(0, 255, 0) {
		t.Fatalf("highlight %+v", th.Highlight)
	}
	if th.Background != Default().Background || th.Flash != Default().Flash {
		t.Fatalf("inherit %+v", th)
	}
	if th.SystemColor("snes") != Default().SystemColor("snes") {
		t.Fatal("systems inherit")
	}
	if th.TitlePx() != Default().TitlePx() || th.BodyPx() != Default().BodyPx() || th.CaptionPx() != Default().CaptionPx() || th.StatusPx() != Default().StatusPx() {
		t.Fatalf("incomplete roles title=%d body=%d caption=%d status=%d", th.TitlePx(), th.BodyPx(), th.CaptionPx(), th.StatusPx())
	}
	if th.TitleWeight() != gfx.WeightBold || th.HeaderWeight() != gfx.WeightBold || th.BodyWeight() != gfx.WeightRegular {
		t.Fatalf("incomplete weights title=%s header=%s body=%s", th.TitleWeight(), th.HeaderWeight(), th.BodyWeight())
	}
}

func TestResolveBuiltinAndMissing(t *testing.T) {
	t.Parallel()
	th, err := Resolve("")
	if err != nil || th.Name != NameDefault {
		t.Fatalf("empty: %+v %v", th, err)
	}
	th, err = Resolve("arcade")
	if err != nil || th.Name != NameArcade {
		t.Fatalf("arcade: %+v %v", th, err)
	}
	_, err = Resolve("no-such-theme")
	if err == nil {
		t.Fatal("missing name")
	}
	_, err = Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
}

func TestLoadJSONAndTOML(t *testing.T) {
	t.Parallel()
	jsonPath := filepath.Join("themes", "arcade.json")
	th, err := Load(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !th.Equal(Arcade()) {
		t.Fatalf("json arcade %+v vs builtin %+v", th, Arcade())
	}
	tomlPath := filepath.Join("testdata", "arcade.toml")
	th, err = Load(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !th.Equal(Arcade()) {
		t.Fatalf("toml arcade %+v vs builtin %+v", th, Arcade())
	}
	def, err := Load(filepath.Join("themes", "default.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !def.Equal(Default()) {
		t.Fatalf("json default %+v vs builtin %+v", def, Default())
	}
	night, err := Load(filepath.Join("themes", "night.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !night.Equal(Night()) {
		t.Fatalf("json night %+v vs builtin %+v", night, Night())
	}
}

func TestLoadNamelessFileUsesBasename(t *testing.T) {
	t.Parallel()
	th, err := Load(filepath.Join("testdata", "nameless.json"))
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "nameless" {
		t.Fatalf("name %q", th.Name)
	}
	if th.Highlight != gfx.RGB(0, 255, 0) || th.Background != Default().Background {
		t.Fatalf("tokens %+v", th)
	}
}

func TestLoadPartialJSONInheritsDefault(t *testing.T) {
	t.Parallel()
	th, err := Load(filepath.Join("testdata", "partial.json"))
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "partial" {
		t.Fatalf("name %q", th.Name)
	}
	if th.Highlight != gfx.RGB(0, 255, 0) {
		t.Fatalf("highlight %+v", th.Highlight)
	}
	if th.Background != Default().Background || th.SystemColor("megadrive") != Default().SystemColor("megadrive") {
		t.Fatalf("inherit %+v", th)
	}
	if th.TitlePx() != Default().TitlePx() || th.BodyPx() != Default().BodyPx() {
		t.Fatalf("partial type roles title=%d body=%d", th.TitlePx(), th.BodyPx())
	}
	if th.TitleWeight() != gfx.WeightBold || th.StatusWeight() != gfx.WeightRegular {
		t.Fatalf("partial weights title=%s status=%s", th.TitleWeight(), th.StatusWeight())
	}
}

func TestLoadRejectsUnknownFieldsAndBadColor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	unknown := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(unknown, []byte(`{"name":"x","nope":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unknown); err == nil {
		t.Fatal("unknown field")
	}
	bad := filepath.Join(dir, "color.json")
	if err := os.WriteFile(bad, []byte(`{"highlight":"red"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "#RRGGBB") {
		t.Fatalf("bad color: %v", err)
	}
}

func TestTypeRolesPreferPxThenScale(t *testing.T) {
	t.Parallel()
	scaleOnly, err := Load(filepath.Join("testdata", "scale_only.json"))
	if err != nil {
		t.Fatal(err)
	}
	if scaleOnly.Name != "scale-only" {
		t.Fatalf("name %q", scaleOnly.Name)
	}
	if scaleOnly.TitleSize != 0 || scaleOnly.BodySize != 0 || scaleOnly.CaptionSize != 0 || scaleOnly.StatusSize != 0 {
		t.Fatalf("scale-only stored px %+v", scaleOnly)
	}
	if scaleOnly.TitlePx() != gfx.ScalePx(3) || scaleOnly.BodyPx() != gfx.ScalePx(2) || scaleOnly.CaptionPx() != gfx.ScalePx(2) || scaleOnly.StatusPx() != gfx.ScalePx(4) {
		t.Fatalf("scale-only roles title=%d body=%d caption=%d status=%d", scaleOnly.TitlePx(), scaleOnly.BodyPx(), scaleOnly.CaptionPx(), scaleOnly.StatusPx())
	}

	px, err := Load(filepath.Join("testdata", "px_override.json"))
	if err != nil {
		t.Fatal(err)
	}
	if px.TitlePx() != 22 || px.BodyPx() != 13 || px.CaptionPx() != 11 || px.StatusPx() != 15 {
		t.Fatalf("px-override roles title=%d body=%d caption=%d status=%d", px.TitlePx(), px.BodyPx(), px.CaptionPx(), px.StatusPx())
	}
	if px.HeaderScale != 2 || gfx.ScalePx(px.HeaderScale) == px.TitlePx() {
		t.Fatal("px override must win over header_scale")
	}

	bodyOnly := Theme{BodySize: 13, HeaderScale: 2, LabelScale: 1, StatusScale: 2}.Complete()
	if bodyOnly.CaptionPx() != 13 {
		t.Fatalf("caption should follow body_px when caption_px is unset, got %d", bodyOnly.CaptionPx())
	}

	def, arcade := Default(), Arcade()
	if def.TitlePx() == arcade.TitlePx() && def.StatusPx() == arcade.StatusPx() {
		t.Fatal("arcade type roles should differ from default")
	}
}

func TestTitleBoldTokens(t *testing.T) {
	t.Parallel()
	regular, err := Load(filepath.Join("testdata", "title_regular.json"))
	if err != nil {
		t.Fatal(err)
	}
	if regular.TitleWeight() != gfx.WeightRegular || regular.HeaderWeight() != gfx.WeightRegular {
		t.Fatalf("title_bold false should keep title and header regular, got %s/%s", regular.TitleWeight(), regular.HeaderWeight())
	}
	if regular.BodyWeight() != gfx.WeightRegular {
		t.Fatal("body should stay regular")
	}

	headerOnly, err := Load(filepath.Join("testdata", "header_bold.json"))
	if err != nil {
		t.Fatal(err)
	}
	if headerOnly.TitleWeight() != gfx.WeightRegular {
		t.Fatalf("title_bold false: %s", headerOnly.TitleWeight())
	}
	if headerOnly.HeaderWeight() != gfx.WeightBold {
		t.Fatalf("header_bold true: %s", headerOnly.HeaderWeight())
	}

	bodyBold, err := Load(filepath.Join("testdata", "body_bold.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bodyBold.TitleWeight() != gfx.WeightBold || bodyBold.BodyWeight() != gfx.WeightBold || bodyBold.StatusWeight() != gfx.WeightRegular {
		t.Fatalf("body_bold title=%s body=%s status=%s", bodyBold.TitleWeight(), bodyBold.BodyWeight(), bodyBold.StatusWeight())
	}
}

func TestTransitionTokenNoneAndUnknown(t *testing.T) {
	t.Parallel()
	if (Theme{}).Complete().Transition != "curtain" {
		t.Fatal("omitted transition should inherit curtain")
	}
	none := Theme{Transition: "none"}.Complete()
	if none.Transition != "none" {
		t.Fatalf("none became %q", none.Transition)
	}
	if none.Equal(Default()) {
		t.Fatal("none should not equal default curtain")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "none.json")
	if err := os.WriteFile(path, []byte(`{"name":"quiet","transition":"none"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	th, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if th.Transition != "none" {
		t.Fatalf("loaded none %q", th.Transition)
	}
	bad := filepath.Join(dir, "bad-fx.json")
	if err := os.WriteFile(bad, []byte(`{"transition":"sparkle"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Fatal("unknown transition")
	}
	static := filepath.Join(dir, "static.json")
	if err := os.WriteFile(static, []byte(`{"transition":"static"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(static)
	if err != nil || st.Transition != "glitch" {
		t.Fatalf("static alias: %+v %v", st, err)
	}
}

func TestFormatColor(t *testing.T) {
	t.Parallel()
	if got := FormatColor(gfx.RGB(255, 220, 0)); got != "#ffdc00" {
		t.Fatalf("got %s", got)
	}
	if got := FormatColor(gfx.RGBA(1, 2, 3, 4)); got != "#01020304" {
		t.Fatalf("got %s", got)
	}
}
