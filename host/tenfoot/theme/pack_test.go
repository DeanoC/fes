package theme

import "testing"

func TestPackRosterHasClassicNeonAndSofaDim(t *testing.T) {
	t.Parallel()
	packs := Packs()
	if len(packs) != 3 {
		t.Fatalf("len %d", len(packs))
	}
	if packs[0].ID != PackClassic || packs[1].ID != PackNeon || packs[2].ID != PackSofaDim {
		t.Fatalf("ids %#v", packs)
	}
	classic, ok := PackTheme(PackClassic)
	if !ok || !classic.Equal(Default()) {
		t.Fatal("classic")
	}
	neon, ok := PackTheme(PackNeon)
	if !ok || !neon.Equal(Arcade()) {
		t.Fatal("neon")
	}
	dim, ok := PackTheme(PackSofaDim)
	if !ok || !dim.Equal(Night()) {
		t.Fatal("sofa-dim")
	}
	if classic.Equal(neon) || classic.Equal(dim) || neon.Equal(dim) {
		t.Fatal("packs must paint different tokens")
	}
	if neon.TitlePx() == classic.TitlePx() && neon.StatusPx() == classic.StatusPx() {
		t.Fatal("neon type roles should differ from classic")
	}
	if neon.Highlight == dim.Highlight || neon.Background == dim.Background {
		t.Fatal("neon and sofa-dim colors overlap")
	}
	if classic.Transition != "curtain" || neon.Transition != "glitch" || dim.Transition != "wipe" {
		t.Fatalf("pack transitions classic=%q neon=%q dim=%q", classic.Transition, neon.Transition, dim.Transition)
	}
}

func TestNormalizePackAliases(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":            "",
		"classic":     PackClassic,
		"default":     PackClassic,
		"DEFAULT":     PackClassic,
		"neon":        PackNeon,
		"arcade":      PackNeon,
		"sofa-dim":    PackSofaDim,
		"night":       PackSofaDim,
		"sofa":        PackSofaDim,
		"sunset":      "",
		"/tmp/x.json": "",
	}
	for spec, want := range cases {
		if got := NormalizePack(spec); got != want {
			t.Fatalf("%q: got %q want %q", spec, got, want)
		}
	}
}

func TestNextPackCyclesRoster(t *testing.T) {
	t.Parallel()
	if got := NextPack(""); got != PackNeon {
		t.Fatalf("empty %q", got)
	}
	if got := NextPack("sunset"); got != PackNeon {
		t.Fatalf("custom %q", got)
	}
	if got := NextPack(PackClassic); got != PackNeon {
		t.Fatalf("classic %q", got)
	}
	if got := NextPack(PackNeon); got != PackSofaDim {
		t.Fatalf("neon %q", got)
	}
	if got := NextPack(PackSofaDim); got != PackClassic {
		t.Fatalf("dim %q", got)
	}
	if got := NextPack(NameArcade); got != PackSofaDim {
		t.Fatalf("arcade alias %q", got)
	}
}

func TestPackChromeTags(t *testing.T) {
	t.Parallel()
	if PackTag(PackClassic) != "" || PackTag("") != "" {
		t.Fatal("classic stays untagged")
	}
	if PackTag(PackNeon) != "NEON" || PackTag(NameArcade) != "NEON" {
		t.Fatal("neon tag")
	}
	if PackTag(PackSofaDim) != "DIM" {
		t.Fatal("dim tag")
	}
	if PackLabel(PackSofaDim) != "Sofa Dim" || PackShort(PackNeon) != "neon" {
		t.Fatal("label/short")
	}
}

func TestBuiltinAcceptsPackAliases(t *testing.T) {
	t.Parallel()
	classic, ok := Builtin(PackClassic)
	if !ok || classic.Name != NameDefault {
		t.Fatalf("classic builtin %+v ok=%v", classic, ok)
	}
	neon, ok := Builtin(PackNeon)
	if !ok || neon.Name != NameArcade {
		t.Fatal("neon builtin")
	}
	dim, ok := Builtin(PackSofaDim)
	if !ok || dim.Name != NameNight {
		t.Fatal("sofa-dim builtin")
	}
}
