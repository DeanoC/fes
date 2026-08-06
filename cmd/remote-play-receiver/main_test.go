package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/remotemedia"
)

func TestReceiverCommandRequiresSessionAndToken(t *testing.T) {
	for _, args := range [][]string{{"--rtp", ":0"}, {"--rtp", ":0", "--session", "s"}} {
		err := run(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil {
			t.Fatalf("args %v unexpectedly accepted", args)
		}
	}
}

func TestReceiverCommandReportsDecoderUnsupportedExplicitly(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"--rtp", ":0", "--session", "s", "--token", "t", "--decode", "native"}, &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "decode/display is not configured") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(out.String(), "decode_not_configured") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestReceiverCommandStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, []string{"--rtp", ":0", "--session", "s", "--token", "t", "--decode", "ffplay"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("cancelled receiver: %v", err)
	}
}

func TestAccessUnitAnnexBAddsStartCodes(t *testing.T) {
	got := accessUnitAnnexB(remotemedia.AccessUnit{NALs: [][]byte{{0x67, 1}, {0x65, 2}}})
	want := []byte{0, 0, 0, 1, 0x67, 1, 0, 0, 0, 1, 0x65, 2}
	if !bytes.Equal(got, want) {
		t.Fatalf("Annex-B = %x, want %x", got, want)
	}
}
