package fbgrid

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestPaintOSKDrawsQueryKeysAndHint(t *testing.T) {
	const w, h = 640, 480
	d, err := gfx.NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var field tenfoot.TextField
	field.Insert("so")
	snap := field.Snapshot()
	snap.Open = true
	snap.Prompt = "Search"
	snap.Hint = tenfoot.OSKKitHint(snap.Page)
	g := NewWithTiles(w, h, []Tile{{Name: "SONIC", Color: gfx.RGB(40, 90, 200)}})
	ApplyTheme(&g, theme.Default())
	g.Header = "FOGCAST  SEARCH"
	g.EmptyLabel = ""
	Paint(d, g)
	PaintOSK(d, OSKFrame{Width: w, Height: h, HeaderH: g.HeaderH, FooterH: g.FooterH, OSK: snap, Theme: theme.Default()})
	rec := gfx.NewRecorder()
	Paint(rec, g)
	PaintOSK(rec, OSKFrame{Width: w, Height: h, HeaderH: g.HeaderH, FooterH: g.FooterH, OSK: snap, Theme: theme.Default()})
	var sawQuery, sawQ, sawHint bool
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if strings.Contains(c.Text, "Search:") && strings.Contains(c.Text, "so") {
			sawQuery = true
		}
		if c.Text == "Q" {
			sawQ = true
		}
		if strings.Contains(c.Text, "START done") {
			sawHint = true
		}
	}
	if !sawQuery || !sawQ || !sawHint {
		t.Fatalf("query=%v q=%v hint=%v ops=%v", sawQuery, sawQ, sawHint, rec.Ops())
	}
}

func TestPaintEmptyLabelWhenNoTiles(t *testing.T) {
	const w, h = 640, 480
	g := NewWithTiles(w, h, nil)
	ApplyTheme(&g, theme.Default())
	g.EmptyLabel = "No matches"
	rec := gfx.NewRecorder()
	Paint(rec, g)
	var saw bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "No matches" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("missing empty label ops=%v", rec.Ops())
	}
	plain := NewWithTiles(w, h, nil)
	ApplyTheme(&plain, theme.Default())
	plainRec := gfx.NewRecorder()
	Paint(plainRec, plain)
	for _, c := range plainRec.Calls {
		if c.Op == "DrawText" && c.Text == "No matches" {
			t.Fatal("empty catalog painted search miss copy")
		}
	}
}
