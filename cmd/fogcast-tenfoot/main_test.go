package main

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
)

func TestParseArgsDefaultsToLoopbackHostAPI(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != tenfoot.DefaultAPIBase || opts.Width != 1280 || opts.Height != 720 || opts.Fullscreen || opts.Smoke {
		t.Fatalf("opts = %#v", opts)
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

func TestParseArgsRejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	_, err := parseArgs([]string{"-bogus"})
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v", err)
	}
}
