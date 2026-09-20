package shared

import (
	"strings"
	"testing"
)

func TestMaskSecretUsesBullets(t *testing.T) {
	t.Parallel()
	if got := maskSecret(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := maskSecret("s3cret"); got != "••••••" {
		t.Fatalf("got %q", got)
	}
}

func TestOSKMovesFocusAndWraps(t *testing.T) {
	t.Parallel()
	var k OSK
	if got := k.Focused(); got.ID != "char-q" {
		t.Fatalf("start = %q", got.ID)
	}
	k.Move(1, 0)
	if got := k.Focused(); got.ID != "char-w" {
		t.Fatalf("right = %q", got.ID)
	}
	k.Move(-1, 0)
	k.Move(-1, 0)
	if got := k.Focused(); got.ID != "char-p" {
		t.Fatalf("wrap left = %q", got.ID)
	}
	k.Reset()
	k.Move(0, 1)
	if got := k.Focused(); got.ID != "char-a" {
		t.Fatalf("down = %q", got.ID)
	}
	k.Move(0, -1)
	k.Move(0, -1)
	if got := k.Focused(); got.Kind != OSKPage {
		t.Fatalf("wrap up to actions, got %#v", got)
	}
}

func TestOSKCyclePageAndSelectID(t *testing.T) {
	t.Parallel()
	var k OSK
	k.CyclePage(1)
	if got := k.Focused(); got.ID != "char-1" {
		t.Fatalf("symbols page = %q", got.ID)
	}
	if !k.SelectID("char-q") {
		t.Fatal("select q")
	}
	if k.page != oskPageLetters || k.Focused().ID != "char-q" {
		t.Fatalf("select q = page %d id %q", k.page, k.Focused().ID)
	}
	if !k.SelectID("done") {
		t.Fatal("select done")
	}
	if k.Focused().Kind != OSKDone {
		t.Fatalf("done = %#v", k.Focused())
	}
	if !k.SelectID("char-/") {
		t.Fatal("select slash")
	}
	if k.page != oskPageSymbols || k.Focused().Text != "/" {
		t.Fatalf("slash = page %d %#v", k.page, k.Focused())
	}
	if !k.SelectID("page") {
		t.Fatal("select page on symbols")
	}
	if k.page != oskPageSymbols || k.Focused().Kind != OSKPage {
		t.Fatalf("symbols page key jumped to page %d %#v", k.page, k.Focused())
	}
}

func TestTextFieldActivateInsertBackspaceClearDone(t *testing.T) {
	t.Parallel()
	var field TextField
	if !field.OSK.SelectID("char-s") {
		t.Fatal("s")
	}
	if got := field.Activate(); !got.Changed || field.Buffer != "s" {
		t.Fatalf("insert s: %#v buffer=%q", got, field.Buffer)
	}
	field.OSK.SelectID("char-o")
	field.Activate()
	if field.Buffer != "so" {
		t.Fatalf("buffer = %q", field.Buffer)
	}
	field.OSK.SelectID("bksp")
	if got := field.Activate(); !got.Changed || field.Buffer != "s" {
		t.Fatalf("bksp: %#v buffer=%q", got, field.Buffer)
	}
	field.OSK.SelectID("clear")
	if got := field.Activate(); !got.Changed || field.Buffer != "" {
		t.Fatalf("clear: %#v buffer=%q", got, field.Buffer)
	}
	field.OSK.SelectID("done")
	if got := field.Activate(); !got.Done || got.Changed {
		t.Fatalf("done: %#v", got)
	}
	field.OSK.SelectID("page")
	field.Activate()
	if field.OSK.page != oskPageSymbols {
		t.Fatalf("page = %d", field.OSK.page)
	}
}

func TestOSKKitHintFitsKitFooter(t *testing.T) {
	t.Parallel()
	letters := OSKKitHint(oskPageLetters)
	symbols := OSKKitHint(oskPageSymbols)
	if !strings.Contains(letters, "START done") || !strings.Contains(letters, "L/R abc") {
		t.Fatalf("letters %q", letters)
	}
	if !strings.Contains(symbols, "L/R 123") || strings.Contains(letters, "quit") {
		t.Fatalf("symbols %q letters %q", symbols, letters)
	}
	if len(letters) > 40 || len(symbols) > 40 {
		t.Fatalf("hint too long letters=%d symbols=%d", len(letters), len(symbols))
	}
}

func TestTextFieldPhysicalInsertStaysIndependentOfFocus(t *testing.T) {
	t.Parallel()
	var field TextField
	field.Insert("Sonic")
	if field.Buffer != "Sonic" {
		t.Fatalf("buffer = %q", field.Buffer)
	}
	if field.OSK.Focused().ID != "char-q" {
		t.Fatalf("focus moved: %q", field.OSK.Focused().ID)
	}
}
