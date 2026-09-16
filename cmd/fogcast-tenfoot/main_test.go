package main

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestParseArgsDefaultsToLoopbackHostAPI(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != hostclient.DefaultAPIBase || opts.Width != 1280 || opts.Height != 720 || opts.Fullscreen || opts.Smoke {
		t.Fatalf("opts = %#v", opts)
	}
	if opts.SafeAreaSet || opts.NoAttract {
		t.Fatalf("living-room defaults = %#v", opts)
	}
}

func TestParseArgsSmokeAndAPI(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-api", "http://127.0.0.1:8787", "-smoke", "-smoke-timeout", "12s", "-max-games", "50"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != "http://127.0.0.1:8787" || !opts.Smoke || opts.SmokeTimeout != 12*time.Second || opts.MaxGames != 50 {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsAPIHost(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-api", "http://host.docker.internal:8787", "-api-host", "127.0.0.1:8787"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != "http://host.docker.internal:8787" || opts.APIHost != "127.0.0.1:8787" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsSafeAreaAndNoAttract(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-safe-area", "0", "-no-attract"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.SafeAreaSet || opts.SafeAreaPct != 0 || !opts.NoAttract || !opts.NoAttractSet {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsInputProfile(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-input-profile", "swap-ab"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.InputProfile != "swap-ab" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsTheme(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-theme", "arcade"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Theme != "arcade" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsGFX(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-gfx", "fpga"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.GFX != "fpga" {
		t.Fatalf("opts = %#v", opts)
	}
	opts, err = parseArgs([]string{"-gfx", "fpga-stub"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.GFX != "fpga-stub" {
		t.Fatalf("stub opts = %#v", opts)
	}
}

func TestParseArgsLayout(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-layout", "shelf"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.LayoutSet || opts.Layout != "shelf" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsNoAttractFalseIsSet(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-no-attract=false"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.NoAttract || !opts.NoAttractSet {
		t.Fatalf("explicit false = %#v", opts)
	}
}

func TestParseArgsRejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	_, err := parseArgs([]string{"-bogus"})
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v", err)
	}
}
