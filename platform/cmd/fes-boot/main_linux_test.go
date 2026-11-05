//go:build linux

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
)

func TestWriteTicketPersistsFallbackImage(t *testing.T) {
	image := strings.Repeat("a", 64)
	fallback := strings.Repeat("b", 64)
	path := filepath.Join(t.TempDir(), "boot.json")
	choice := store.Selection{
		Manifest:            release.Manifest{ImageSHA256: image},
		Trial:               true,
		BootID:              "boot-a",
		FallbackImageSHA256: fallback,
	}
	if err := writeTicket(path, choice); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ticket map[string]any
	if err := json.Unmarshal(body, &ticket); err != nil {
		t.Fatal(err)
	}
	if ticket["fallback_image_sha256"] != fallback || ticket["image_sha256"] != image || ticket["boot_id"] != "boot-a" || ticket["trial"] != true {
		t.Fatalf("boot ticket = %s", body)
	}
}
