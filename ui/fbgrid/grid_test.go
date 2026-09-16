package fbgrid

import (
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/linuxinput"
)

func TestLayout640x480(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	if g.Columns != 4 || g.count() != 12 {
		t.Fatalf("cols %d count %d", g.Columns, g.count())
	}
	if g.CellW < 80 || g.CellH < 60 {
		t.Fatalf("cell %dx%d", g.CellW, g.CellH)
	}
	x0, y0, ok := g.CellOrigin(0)
	if !ok || x0 != g.Pad || y0 != g.HeaderH+g.Pad {
		t.Fatalf("origin0 %d,%d ok=%v", x0, y0, ok)
	}
	x1, y1, ok := g.CellOrigin(1)
	if !ok || x1 <= x0 || y1 != y0 {
		t.Fatalf("origin1 %d,%d", x1, y1)
	}
	_, _, ok = g.CellOrigin(12)
	if ok {
		t.Fatal("tile 12")
	}
}

func TestNewWithTilesRelayoutsAndCopies(t *testing.T) {
	t.Parallel()
	tiles := []Tile{
		{Name: "ONE", Color: gfx.RGB(1, 2, 3)},
		{Name: "TWO", Color: gfx.RGB(4, 5, 6)},
	}
	g := NewWithTiles(640, 480, tiles)
	if g.count() != len(tiles) || g.CellW < 100 || g.CellH < 250 {
		t.Fatalf("tiles=%d cell=%dx%d", g.count(), g.CellW, g.CellH)
	}
	tiles[0].Name = "mutated"
	if g.Tiles[0].Name != "ONE" {
		t.Fatalf("constructor retained caller slice: %q", g.Tiles[0].Name)
	}
}

func TestMoveFocusCatalogPagesAndEnds(t *testing.T) {
	t.Parallel()
	const cols = DefaultColumns
	if got := MoveFocus(0, 25, cols, 1, 0); got != 1 {
		t.Fatalf("right %d", got)
	}
	if got := MoveFocus(0, 25, cols, 10, 0); got != 3 {
		t.Fatalf("row clamp %d", got)
	}
	if got := MoveFocus(0, 25, cols, -1, 0); got != 0 {
		t.Fatalf("left end %d", got)
	}
	if got := MoveFocus(0, 25, cols, 0, -1); got != 0 {
		t.Fatalf("up end %d", got)
	}
	if got := MoveFocus(0, 25, cols, 0, 1); got != 4 {
		t.Fatalf("down %d", got)
	}
	if got := MoveFocus(11, 25, cols, 0, 1); got != 15 {
		t.Fatalf("page cross %d", got)
	}
	if got := MoveFocus(24, 25, cols, 1, 0); got != 24 {
		t.Fatalf("last row right %d", got)
	}
	if got := MoveFocus(24, 25, cols, 0, 1); got != 24 {
		t.Fatalf("last row down %d", got)
	}
	if got := MoveFocus(23, 25, cols, 0, 1); got != 24 {
		t.Fatalf("short last row %d", got)
	}
	if got := MoveFocus(0, 0, cols, 1, 1); got != 0 {
		t.Fatalf("empty %d", got)
	}
}

func TestMoveStaysOnRowAndClamps(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Move(1, 0)
	if g.Focus != 1 {
		t.Fatalf("right %d", g.Focus)
	}
	g.Move(10, 0)
	if g.Focus != 3 {
		t.Fatalf("row clamp %d", g.Focus)
	}
	g.Move(0, 1)
	if g.Focus != 7 {
		t.Fatalf("down %d", g.Focus)
	}
	g.Move(0, 10)
	if g.Focus != 11 {
		t.Fatalf("last %d", g.Focus)
	}
	g.Move(0, -10)
	if g.Focus != 3 {
		t.Fatalf("top %d", g.Focus)
	}
	g.Move(-10, 0)
	if g.Focus != 0 {
		t.Fatalf("left clamp %d", g.Focus)
	}
}

func TestApplyMoveConfirmQuit(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true, Source: "dpad"})
	if g.Focus != 1 {
		t.Fatalf("right %d", g.Focus)
	}
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: true, Source: "a"})
	if g.Selected != "STREETS" || g.ConfirmLeft != ConfirmFrames {
		t.Fatalf("confirm %q left=%d", g.Selected, g.ConfirmLeft)
	}
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: true, Source: "a"})
	if g.ConfirmLeft != ConfirmFrames {
		t.Fatal("held confirm retriggered")
	}
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: false, Source: "a"})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionDown, Active: true})
	if g.Focus != 5 {
		t.Fatalf("down %d", g.Focus)
	}
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionQuit, Active: true, Source: "start"})
	if !g.Quit {
		t.Fatal("quit")
	}
}

func TestAnalogDoesNotOverstep(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	right := linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true, Analog: true, Source: "abs"}
	g.Apply(right)
	g.Apply(right)
	g.Apply(right)
	if g.Focus != 1 {
		t.Fatalf("analog stream %d", g.Focus)
	}
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true, Repeat: true})
	if g.Focus != 1 {
		t.Fatalf("repeat %d", g.Focus)
	}
}

func TestAnalogIdleDoesNotClearDpad(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true, Source: "dpad"})
	if g.Focus != 1 {
		t.Fatalf("right %d", g.Focus)
	}
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionLeft, Active: false, Analog: true, Source: "stick"})
	for i := 0; i < repeatDelay; i++ {
		g.Tick()
	}
	if g.Focus != 2 {
		t.Fatalf("analog idle cancelled d-pad hold %d", g.Focus)
	}
}

func TestTickRepeatAfterDelay(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true})
	if g.Focus != 1 {
		t.Fatal("rising")
	}
	for i := 0; i < repeatDelay-1; i++ {
		g.Tick()
		if g.Focus != 1 {
			t.Fatalf("early tick %d focus %d", i, g.Focus)
		}
	}
	g.Tick()
	if g.Focus != 2 {
		t.Fatalf("repeat %d", g.Focus)
	}
}

func TestConfirmFlashStaysOnSelectedAfterMove(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: false})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: false})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true})
	if g.Focus != 2 || g.ConfirmIndex != 1 || g.Selected != "STREETS" {
		t.Fatalf("focus=%d confirm=%d selected=%q", g.Focus, g.ConfirmIndex, g.Selected)
	}
}

func TestConfirmFlashDecays(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: true})
	for i := 0; i < ConfirmFrames; i++ {
		g.Tick()
	}
	if g.ConfirmLeft != 0 {
		t.Fatalf("left %d", g.ConfirmLeft)
	}
	if g.Selected != "SONIC 2" {
		t.Fatalf("selected %q", g.Selected)
	}
	if g.Status() != "WAS SONIC 2  SONIC 2" {
		t.Fatalf("status %q", g.Status())
	}
}

func TestIgnoresNone(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Apply(linuxinput.Mapped{})
	if g.Focus != 0 || g.Quit || g.Selected != "" {
		t.Fatal("none mutated")
	}
}
