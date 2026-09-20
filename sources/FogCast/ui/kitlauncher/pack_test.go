package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/fbgrid"
	"github.com/DeanoC/FogCast/ui/theme"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestXCyclesPacksWithoutStealingDpadOrY(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: makeGames(25)}
	now := time.Unix(1, 0)
	if m.Pack != "" || m.Browse != fbgrid.BrowseGrid {
		t.Fatalf("origin pack=%q browse=%s", m.Pack, m.Browse)
	}
	pressNamed(&m, "x", now)
	if m.Pack != theme.PackNeon || m.Browse != fbgrid.BrowseGrid {
		t.Fatalf("x1 pack=%q browse=%s", m.Pack, m.Browse)
	}
	pressNamed(&m, "dpad-right", now)
	if m.Focus != 1 || m.Pack != theme.PackNeon || m.Browse != fbgrid.BrowseGrid {
		t.Fatalf("dpad after x focus=%d pack=%q browse=%s", m.Focus, m.Pack, m.Browse)
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseCoverflow || m.Pack != theme.PackNeon || m.Focus != 1 {
		t.Fatalf("y after x browse=%s pack=%q focus=%d", m.Browse, m.Pack, m.Focus)
	}
	pressNamed(&m, "x", now)
	if m.Pack != theme.PackSofaDim || m.Browse != fbgrid.BrowseCoverflow || m.Focus != 1 {
		t.Fatalf("x2 pack=%q browse=%s focus=%d", m.Pack, m.Browse, m.Focus)
	}
	pressNamed(&m, "x", now)
	if m.Pack != theme.PackClassic || m.Browse != fbgrid.BrowseCoverflow {
		t.Fatalf("x3 pack=%q browse=%s", m.Pack, m.Browse)
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseWall || m.Pack != theme.PackClassic {
		t.Fatalf("y2 browse=%s pack=%q", m.Browse, m.Pack)
	}
	pressNamed(&m, "y", now)
	if m.Browse != fbgrid.BrowseSplit || m.Pack != theme.PackClassic {
		t.Fatalf("y3 browse=%s pack=%q", m.Browse, m.Pack)
	}
}

func TestXCyclesOnWheelAndDetail(t *testing.T) {
	now := time.Unix(1, 0)
	wheel := Model{Connected: true, TargetReady: true, WheelOpen: true}
	wheel.SetCatalog(mixedCatalog())
	pressNamed(&wheel, "x", now)
	if wheel.Pack != theme.PackNeon || !wheel.WheelOpen {
		t.Fatalf("wheel x pack=%q open=%v", wheel.Pack, wheel.WheelOpen)
	}
	pressNamed(&wheel, "dpad-right", now)
	if wheel.Shelf == "" || !wheel.WheelOpen || wheel.Pack != theme.PackNeon {
		t.Fatalf("wheel dpad shelf=%q pack=%q", wheel.Shelf, wheel.Pack)
	}
	detail := Model{Connected: true, TargetReady: true, Games: makeGames(4), DetailOpen: true}
	pressNamed(&detail, "x", now)
	if detail.Pack != theme.PackNeon || !detail.DetailOpen {
		t.Fatalf("detail x pack=%q open=%v", detail.Pack, detail.DetailOpen)
	}
}

func TestXDismissesAttractWithoutCycling(t *testing.T) {
	now := time.Unix(1, 0)
	m := Model{Connected: true, TargetReady: true, Games: makeGames(4), AttractActive: true}
	pressNamed(&m, "x", now)
	if m.Pack != "" {
		t.Fatalf("attract x cycled pack=%q", m.Pack)
	}
	if m.AttractActive {
		t.Fatal("attract x should dismiss like any pad input")
	}
}

func TestXDoesNotLaunchOrStop(t *testing.T) {
	m := Model{Connected: true, TargetReady: true, Games: []hostclient.Game{{ID: "pong", Launchable: true}}}
	now := time.Unix(1, 0)
	x, _ := remoteinput.NormalizeGamepad("x", true)
	if action := m.Input(x, now); action != "" {
		t.Fatalf("x launched %q", action)
	}
	if m.Pack != theme.PackNeon {
		t.Fatalf("x pack %q", m.Pack)
	}
	m.Session.State = "active"
	if action := m.Input(x, now); action != "" {
		t.Fatalf("x stopped %q", action)
	}
}

func TestWheelHintNamesNextPack(t *testing.T) {
	m := Model{WheelOpen: true}
	if got := m.WheelHint(); got != "A open | L/R platform | X neon" {
		t.Fatalf("origin %q", got)
	}
	m.Pack = theme.PackNeon
	if got := m.WheelHint(); got != "A open | L/R platform | X dim" {
		t.Fatalf("neon %q", got)
	}
	m.Pack = theme.PackSofaDim
	if got := m.WheelHint(); got != "A open | L/R platform | X classic" {
		t.Fatalf("dim %q", got)
	}
	m.Pack = theme.PackClassic
	if got := m.WheelHint(); got != "A open | L/R platform | X neon" {
		t.Fatalf("classic %q", got)
	}
}

func TestGridHintUnchangedByPack(t *testing.T) {
	m := Model{fromWheel: true, Browse: fbgrid.BrowseGrid, Pack: theme.PackNeon}
	if got := m.GridHint(); got != "A play | B platforms | L/R | Y flow" {
		t.Fatalf("grid %q", got)
	}
}

func TestPersistPackRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(cfg)
	persistPack(c, theme.PackNeon)
	got, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != theme.PackNeon {
		t.Fatalf("theme %q", got.Theme)
	}
	persistPack(c, theme.PackNeon)
	persistPack(c, theme.PackSofaDim)
	got, err = LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != theme.PackSofaDim || got.API != cfg.API {
		t.Fatalf("round trip %+v", got)
	}
}

func TestPackTagClassicUntagged(t *testing.T) {
	m := Model{Pack: theme.PackClassic}
	if m.PackTag() != "" {
		t.Fatalf("classic %q", m.PackTag())
	}
	m.Pack = theme.PackNeon
	if m.PackTag() != "NEON" {
		t.Fatalf("neon %q", m.PackTag())
	}
}
