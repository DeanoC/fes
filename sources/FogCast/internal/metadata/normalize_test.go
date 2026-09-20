package metadata

import "testing"

func TestNormalizeTitleUsesNFCFoldAndUnicodeWhitespace(t *testing.T) {
	got, err := NormalizeTitle("  Cafe\u0301\u2003GAME\u00a0  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "café game" {
		t.Fatalf("normalized title = %q", got)
	}
}

func TestDecoratedTitleRemovesOnlyApprovedTerminalSuffix(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  string
	}{
		{"Sonic (USA)", "sonic"},
		{"Sonic [Europe]", "sonic"},
		{"Sonic (rev 2)", "sonic"},
		{"Sonic (REV a)", "sonic"},
		{"Sonic (beta)", "sonic (beta)"},
		{"Sonic (USA) extra", "sonic (usa) extra"},
	} {
		t.Run(tc.title, func(t *testing.T) {
			got, err := DecoratedTitle(tc.title)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("decorated title = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeTitleRejectsEmptyAndOversizeInput(t *testing.T) {
	for _, title := range []string{"", "\u2003", string(make([]rune, 201))} {
		if _, err := NormalizeTitle(title); err == nil {
			t.Fatalf("NormalizeTitle(%q) accepted invalid title", title)
		}
	}
}
