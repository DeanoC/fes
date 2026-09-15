package audioreact

import (
	"strings"
	"testing"
)

func TestParseALSADummyIsNotAMeter(t *testing.T) {
	t.Parallel()
	cards := parseALSACards(" 0 [Dummy          ]: Dummy - Dummy\n                      Dummy 1\n")
	if len(cards) != 1 || cards[0] != "Dummy" {
		t.Fatalf("cards %#v", cards)
	}
	if !dummyOnly(cards) {
		t.Fatal("expected dummy-only")
	}
	notes := notesFor(cards, "00-00: Dummy PCM : Dummy PCM : playback 8 : capture 8", false)
	if !strings.Contains(notes, "Dummy") || !strings.Contains(notes, "not a game-audio meter") {
		t.Fatalf("notes %q", notes)
	}
	r := Report{ALSACards: cards, ALSAPCM: "00-00: Dummy PCM", Notes: notes, Measured: false}
	s := r.String()
	if !strings.Contains(s, "measured=0") || !strings.Contains(s, "alsa=Dummy") {
		t.Fatalf("report %q", s)
	}
}

func TestParseALSAEmptyAndNamed(t *testing.T) {
	t.Parallel()
	if parseALSACards("") != nil && len(parseALSACards("")) != 0 {
		t.Fatal("empty")
	}
	cards := parseALSACards(" 1 [HDMI           ]: HDA-Intel - HDA Intel HDMI\n")
	if dummyOnly(cards) {
		t.Fatal("hdmi is not dummy-only")
	}
	notes := notesFor(cards, "", false)
	if !strings.Contains(notes, "FPGA HDMI audio is not measured") {
		t.Fatalf("notes %q", notes)
	}
}

func TestProbeDoesNotClaimAMeter(t *testing.T) {
	t.Parallel()
	r := Probe()
	if r.Measured {
		t.Fatalf("probe claimed a meter: %s", r)
	}
	if strings.TrimSpace(r.Notes) == "" {
		t.Fatal("missing notes")
	}
}
