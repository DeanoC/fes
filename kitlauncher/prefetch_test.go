package kitlauncher

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot"
)

func TestPrefetchArtworkHandlesOrdersFocusPageNextStripAttract(t *testing.T) {
	focus := strings.Repeat("aa", 32)
	page := strings.Repeat("bb", 32)
	next := strings.Repeat("cc", 32)
	strip := strings.Repeat("dd", 32)
	attract := strings.Repeat("ee", 32)
	m := Model{
		Games: []tenfoot.Game{
			{ID: "focus", Cover: focus, Launchable: true},
			{ID: "page", Cover: page, Launchable: true},
			{ID: "next", Cover: next, Launchable: true},
		},
		Strip: []tenfoot.Game{{ID: "strip", Cover: strip, Launchable: true}},
		Focus: 0,
	}
	got := PrefetchArtworkHandles(m, nil, 0, 2, 3, nil, []string{attract})
	if len(got) != 5 || got[0] != focus || got[1] != page || got[2] != next || got[3] != strip || got[4] != attract {
		t.Fatalf("order %#v", got)
	}
}
