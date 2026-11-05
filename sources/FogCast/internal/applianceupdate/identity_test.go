package applianceupdate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
)

func TestBootTicketMustMatchCurrentLinuxBoot(t *testing.T) {
	dir := t.TempDir()
	ticket := filepath.Join(dir, "boot.json")
	proc := filepath.Join(dir, "boot_id")
	id := "11111111-2222-4333-8444-555555555555"
	if e := os.WriteFile(proc, []byte(id+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, body := range []string{
		`{"boot_id":"old","image_sha256":"` + strings.Repeat("a", 64) + `","trial":true}`,
		`{"boot_id":"` + id + `","image_sha256":"` + strings.Repeat("a", 64) + `","trial":true,"trial":false}`,
		`{"boot_id":"` + id + `","image_sha256":"` + strings.Repeat("a", 64) + `"}`,
		`{"boot_id":"` + id + `","image_sha256":"` + strings.Repeat("a", 64) + `","trial":true,"fallback_image_sha256":"bad"}`,
	} {
		if e := os.WriteFile(ticket, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := applianceupdate.ReadBootIdentity(ticket, proc); e == nil {
			t.Fatalf("invalid ticket accepted: %s", body)
		}
	}
	body := `{"boot_id":"` + id + `","image_sha256":"` + strings.Repeat("a", 64) + `","trial":true}`
	if e := os.WriteFile(ticket, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	got, e := applianceupdate.ReadBootIdentity(ticket, proc)
	if e != nil || !got.Trial || got.BootID != id {
		t.Fatalf("ticket: %+v %v", got, e)
	}
	body = `{"boot_id":"` + id + `","image_sha256":"` + strings.Repeat("b", 64) + `","trial":true,"fallback_image_sha256":"` + strings.Repeat("a", 64) + `"}`
	if e := os.WriteFile(ticket, []byte(body), 0o600); e != nil {
		t.Fatal(e)
	}
	got, e = applianceupdate.ReadBootIdentity(ticket, proc)
	if e != nil || !got.Trial || got.ImageSHA256 != strings.Repeat("b", 64) || got.FallbackImageSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("ticket with fallback: %+v %v", got, e)
	}
}

func TestBootIdentityReadsMatchingFallbackSidecar(t *testing.T) {
	dir := t.TempDir()
	ticket := filepath.Join(dir, "boot.json")
	proc := filepath.Join(dir, "boot_id")
	image := strings.Repeat("a", 64)
	fallback := strings.Repeat("b", 64)
	if err := os.WriteFile(proc, []byte("boot-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteBootTicket(ticket, "boot-a", image, true, fallback); err != nil {
		t.Fatal(err)
	}
	got, err := applianceupdate.ReadBootIdentity(ticket, proc)
	if err != nil || !got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != image || got.FallbackImageSHA256 != fallback {
		t.Fatalf("ticket with fallback sidecar: %+v %v", got, err)
	}

	stale := `{"boot_id":"old-boot","image_sha256":"` + image + `","fallback_image_sha256":"` + fallback + `"}`
	if err := os.WriteFile(filepath.Join(dir, store.BootFallbackFilename), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = applianceupdate.ReadBootIdentity(ticket, proc)
	if err != nil || got.FallbackImageSHA256 != "" {
		t.Fatalf("stale sidecar was used: %+v %v", got, err)
	}
}
