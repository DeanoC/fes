package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/appliance"
)

func TestReadReleaseManifestRequiresSelectedBoard(t *testing.T) {
	raw := strings.Replace(releaseJSON("", ""), appliance.Board, appliance.BoardDE25, 1)
	dir := writeRelease(t, raw)
	var log bytes.Buffer
	if _, err := readReleaseManifest(dir, &log); err == nil {
		t.Fatal("DE10 CLI default admitted DE25")
	}
	m, err := readReleaseManifestForBoard(dir, &log, appliance.BoardDE25)
	if err != nil || m.Board != appliance.BoardDE25 {
		t.Fatalf("explicit DE25 selection failed: %+v %v", m, err)
	}
	if _, err = readReleaseManifestForBoard(dir, &log, "unknown"); err == nil {
		t.Fatal("unknown board admitted")
	}
}
