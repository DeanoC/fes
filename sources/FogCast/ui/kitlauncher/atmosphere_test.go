package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"strings"
	"testing"
)

func TestAtmosphereHandlePrefersBackdropThenAttract(t *testing.T) {
	cover := strings.Repeat("11", 32)
	backdrop := strings.Repeat("22", 32)
	still := strings.Repeat("33", 32)
	m := Model{Connected: true, TargetReady: true}
	m.SetCatalog([]hostclient.Game{
		{ID: "sonic", Title: "Sonic", System: "megadrive", Cover: cover, Launchable: true},
	})
	pres := hostclient.Presentation{Presentation: &hostclient.PresentationInfo{
		CoverArtworkID:    cover,
		BackdropArtworkID: backdrop,
	}}
	if got := m.AtmosphereHandle(pres); got != backdrop {
		t.Fatalf("presentation backdrop %q", got)
	}
	if got := m.AtmosphereHandle(hostclient.Presentation{}); got != "" {
		t.Fatalf("cover-only %q", got)
	}
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{{
		GameID: "sonic", Title: "Sonic", Platform: "megadrive", Backdrop: still, Cover: cover, Launchable: true,
	}}})
	if got := m.AtmosphereHandle(hostclient.Presentation{}); got != still {
		t.Fatalf("attract backdrop %q", got)
	}
	m.SetAttractPlaylist(hostclient.AttractPlaylist{Items: []hostclient.AttractItem{{
		GameID: "sonic", Title: "Sonic", Platform: "megadrive", Cover: cover, Launchable: true,
	}}})
	if got := m.AtmosphereHandle(hostclient.Presentation{}); got != "" {
		t.Fatalf("cover-only attract %q", got)
	}
}
