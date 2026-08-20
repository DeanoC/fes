package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
)

func TestReferencedMediaNamesParsesCueAndGDI(t *testing.T) {
	cue := catalog.ReferencedMediaNames("game.cue", strings.NewReader(`FILE "track 1.bin" BINARY
FILE track02.bin WAVE
`))
	if len(cue) != 2 || cue[0] != "track 1.bin" || cue[1] != "track02.bin" {
		t.Fatalf("cue = %#v", cue)
	}
	gdi := catalog.ReferencedMediaNames("disc.gdi", strings.NewReader(`3
1 0 4 2352 "track01.bin" 0
2 0 0 2352 track02.raw 0
`))
	if len(gdi) != 2 || gdi[0] != "track01.bin" || gdi[1] != "track02.raw" {
		t.Fatalf("gdi = %#v", gdi)
	}
	if names := catalog.ReferencedMediaNames("game.chd", strings.NewReader("unused")); len(names) != 0 {
		t.Fatalf("chd = %#v", names)
	}
}

func TestReferencedMediaNamesKeepsSubdirectoryAndDropsEscapes(t *testing.T) {
	names := catalog.ReferencedMediaNames("game.cue", strings.NewReader(`FILE "tracks/track.bin" BINARY
FILE "../outside.bin" BINARY
FILE "/tmp/abs.bin" BINARY
`))
	if len(names) != 1 || names[0] != "tracks/track.bin" {
		t.Fatalf("names = %#v", names)
	}
	_, err := catalog.ParseReferencedMedia("game.cue", strings.NewReader(`FILE "../outside.bin" BINARY
FILE "game.bin" BINARY
`))
	if !errors.Is(err, catalog.ErrEscapingMediaReference) {
		t.Fatalf("parse escaping = %v", err)
	}
}

func TestParseReferencedMediaRejectsOversizedSheet(t *testing.T) {
	body := strings.Repeat("A", 1<<20+8) + "\nFILE \"../outside.bin\" BINARY\n"
	_, err := catalog.ParseReferencedMedia("game.cue", strings.NewReader(body))
	if !errors.Is(err, catalog.ErrUnvalidatedMediaSheet) {
		t.Fatalf("oversized parse = %v", err)
	}
	if names := catalog.ReferencedMediaNames("game.cue", strings.NewReader(body)); len(names) != 0 {
		t.Fatalf("oversized skip names = %#v", names)
	}
}
