package audioreact

import (
	"bufio"
	"strings"
)

// Report is what Probe found. Measured stays false unless a real peak
// source exists; Dummy ALSA and Pulse sockets are not game-audio meters.
type Report struct {
	ALSACards []string
	ALSAPCM   string
	Pulse     bool
	Measured  bool
	Notes     string
}

func (r Report) String() string {
	var b strings.Builder
	b.WriteString("measured=")
	if r.Measured {
		b.WriteString("1")
	} else {
		b.WriteString("0")
	}
	if len(r.ALSACards) > 0 {
		b.WriteString(" alsa=")
		b.WriteString(strings.Join(r.ALSACards, ","))
	} else {
		b.WriteString(" alsa=none")
	}
	if strings.TrimSpace(r.ALSAPCM) != "" {
		b.WriteString(" pcm=")
		b.WriteString(strings.TrimSpace(strings.ReplaceAll(r.ALSAPCM, "\n", ";")))
	}
	if r.Pulse {
		b.WriteString(" pulse=1")
	} else {
		b.WriteString(" pulse=0")
	}
	if notes := strings.TrimSpace(r.Notes); notes != "" {
		b.WriteString(" notes=")
		b.WriteString(notes)
	}
	return b.String()
}

func parseALSACards(text string) []string {
	var cards []string
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// "0 [Dummy          ]: Dummy - Dummy"
		lb := strings.IndexByte(line, '[')
		rb := strings.IndexByte(line, ']')
		if lb < 0 || rb <= lb {
			continue
		}
		name := strings.TrimSpace(line[lb+1 : rb])
		if name == "" {
			continue
		}
		cards = append(cards, name)
	}
	return cards
}

func dummyOnly(cards []string) bool {
	if len(cards) == 0 {
		return false
	}
	for _, name := range cards {
		if !strings.Contains(strings.ToLower(name), "dummy") {
			return false
		}
	}
	return true
}

func notesFor(cards []string, _ string, pulse bool) string {
	switch {
	case dummyOnly(cards):
		return "ALSA Dummy card only; not a game-audio meter. FPGA HDMI audio is not measured."
	case len(cards) == 0 && !pulse:
		return "no ALSA cards and no Pulse; no measured audio level."
	case !pulse:
		return "no ALSA peak meter and no Pulse; FPGA HDMI audio is not measured."
	default:
		return "no peak meter wired; Pulse presence is not a level source. FPGA HDMI audio is not measured."
	}
}
