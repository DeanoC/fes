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
}

func TestCompleteFillsMissingTokens(t *testing.T) {
	t.Parallel()
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

func TestFormatColor(t *testing.T) {
	t.Parallel()
	if got := FormatColor(gfx.RGB(255, 220, 0)); got != "#ffdc00" {
		t.Fatalf("got %s", got)
	}
	if got := FormatColor(gfx.RGBA(1, 2, 3, 4)); got != "#01020304" {
		t.Fatalf("got %s", got)
	}
}
