package applianceboot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
)

func applianceBootTestImage(version string, marker byte) ([]byte, release.Manifest) {
	image := make([]byte, 4096)
	image[0] = marker
	image[1080], image[1081] = 0x53, 0xef
	image[1120] = 0x40
	manifest := release.Manifest{
		Format:          release.Format,
		Board:           release.Board,
		BootABI:         release.BootABI,
		Version:         version,
		KernelSHA256:    strings.Repeat("a", 64),
		ImageSHA256:     fmt.Sprintf("%x", sha256.Sum256(image)),
		ImageSize:       int64(len(image)),
		FESRevision:     strings.Repeat("b", 40),
		FogCastRevision: strings.Repeat("c", 40),
		RuntimeRevision: strings.Repeat("d", 40),
	}
	return image, manifest
}

func stageApplianceBootTestImage(t *testing.T, s *store.Store, image []byte, manifest release.Manifest) {
	t.Helper()
	if err := s.Stage(context.Background(), manifest, manifest.ImageSize, bytes.NewReader(image)); err != nil {
		t.Fatal(err)
	}
}

func confirmApplianceBootTestImage(t *testing.T, s *store.Store, manifest release.Manifest, bootID string) {
	t.Helper()
	if err := s.Activate(manifest.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot(bootID); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(bootID, manifest.ImageSHA256); err != nil {
		t.Fatal(err)
	}
}

func TestPreviousImageBootsWhenCurrentCannotBePrepared(t *testing.T) {
	factoryImage, factory := applianceBootTestImage("factory", 1)
	s, err := store.New(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	stageApplianceBootTestImage(t, s, factoryImage, factory)

	previousImage, previous := applianceBootTestImage("previous", 2)
	stageApplianceBootTestImage(t, s, previousImage, previous)
	confirmApplianceBootTestImage(t, s, previous, "confirm-previous")

	currentImage, current := applianceBootTestImage("current", 3)
	stageApplianceBootTestImage(t, s, currentImage, current)
	confirmApplianceBootTestImage(t, s, current, "confirm-current")

	currentPath, err := s.ImagePath(current.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	previousPath, err := s.ImagePath(previous.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	p := &fixturePlatform{bad: map[string]bool{currentPath: true}}
	if err := Run(context.Background(), s, factory.ImageSHA256, "recovery-boot", p); err == nil || !strings.Contains(err.Error(), "execute selected init") {
		t.Fatalf("Run error = %v, want the previous image to reach exec", err)
	}

	want := []string{
		"prepare:" + currentPath,
		"prepare:" + previousPath,
		"ticket:" + previous.ImageSHA256,
		"exec:" + previousPath,
		"close:" + previousPath,
	}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
	if p.ticket.Manifest.ImageSHA256 != previous.ImageSHA256 || p.ticket.FallbackImageSHA256 != factory.ImageSHA256 {
		t.Fatalf("recovery ticket = %+v, want previous with factory fallback", p.ticket)
	}
	status, err := s.Status()
	if err != nil || status.Good != previous.ImageSHA256 || status.Previous != "" {
		t.Fatalf("recovery state = %+v, %v; want previous as Good and no failed predecessor", status, err)
	}
}

func TestConfirmedImageBootRefreshesTicketAndFallbackForNextKernelBoot(t *testing.T) {
	factoryImage, factory := applianceBootTestImage("factory", 1)
	s, err := store.New(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	stageApplianceBootTestImage(t, s, factoryImage, factory)

	previousImage, previous := applianceBootTestImage("previous", 2)
	stageApplianceBootTestImage(t, s, previousImage, previous)
	confirmApplianceBootTestImage(t, s, previous, "confirm-previous")

	currentImage, current := applianceBootTestImage("current", 3)
	stageApplianceBootTestImage(t, s, currentImage, current)
	confirmApplianceBootTestImage(t, s, current, "confirm-current")

	const nextBootID = "after-reboot"
	ticketPath := filepath.Join(t.TempDir(), "releases", "boot.json")
	if err := os.MkdirAll(filepath.Dir(ticketPath), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &fixturePlatform{ticketPath: ticketPath}
	if err := Run(context.Background(), s, factory.ImageSHA256, nextBootID, p); err == nil || !strings.Contains(err.Error(), "execute selected init") {
		t.Fatalf("Run error = %v, want selected image to reach init", err)
	}
	if p.ticket.BootID != nextBootID || p.ticket.Manifest.ImageSHA256 != current.ImageSHA256 || p.ticket.FallbackImageSHA256 != previous.ImageSHA256 || p.ticket.Trial {
		t.Fatalf("next-boot ticket selection = %+v", p.ticket)
	}
	body, err := os.ReadFile(ticketPath)
	if err != nil {
		t.Fatal(err)
	}
	var ticket store.BootTicket
	if err = json.Unmarshal(body, &ticket); err != nil || ticket.BootID != nextBootID || ticket.ImageSHA256 != current.ImageSHA256 || ticket.Trial {
		t.Fatalf("boot.json after reboot = %+v, %v", ticket, err)
	}
	fallback, err := store.ReadBootFallback(filepath.Join(filepath.Dir(ticketPath), store.BootFallbackFilename), nextBootID, current.ImageSHA256)
	if err != nil || fallback != previous.ImageSHA256 {
		t.Fatalf("next-boot fallback = %q, %v; want %s", fallback, err, previous.ImageSHA256)
	}
	status, err := s.Status()
	if err != nil || status.Good != current.ImageSHA256 || status.Previous != previous.ImageSHA256 {
		t.Fatalf("selection after normal reboot = %+v, %v", status, err)
	}
}

func TestUnverifiableGoodTriesPreviousBeforeFactory(t *testing.T) {
	factoryImage, factory := applianceBootTestImage("factory", 1)
	s, err := store.New(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	stageApplianceBootTestImage(t, s, factoryImage, factory)

	previousImage, previous := applianceBootTestImage("previous", 2)
	stageApplianceBootTestImage(t, s, previousImage, previous)
	confirmApplianceBootTestImage(t, s, previous, "confirm-previous")

	currentImage, current := applianceBootTestImage("current", 3)
	stageApplianceBootTestImage(t, s, currentImage, current)
	confirmApplianceBootTestImage(t, s, current, "confirm-current")

	currentPath, err := s.ImagePath(current.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	previousPath, err := s.ImagePath(previous.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	factoryPath, err := s.ImagePath(factory.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentPath, []byte("unverifiable current image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(current.ImageSHA256); err == nil {
		t.Fatal("corrupt current image unexpectedly verified")
	}

	p := &fixturePlatform{
		bad: map[string]bool{previousPath: true},
	}
	p.beforePrepare = func(path string) {
		if path != factoryPath {
			return
		}
		status, err := s.Status()
		if err != nil {
			t.Fatalf("read selection before preparing factory: %v", err)
		}
		if status.Good != current.ImageSHA256 || status.Previous != previous.ImageSHA256 {
			t.Fatalf("selection before factory = %+v; want Good and Previous preserved until Previous is tried", status)
		}
	}
	if err := Run(context.Background(), s, factory.ImageSHA256, "unverifiable-good-boot", p); err == nil || !strings.Contains(err.Error(), "execute selected init") {
		t.Fatalf("Run error = %v, want factory to reach exec after Previous fails preparation", err)
	}

	want := []string{
		"prepare:" + previousPath,
		"prepare:" + factoryPath,
		"ticket:" + factory.ImageSHA256,
		"exec:" + factoryPath,
		"close:" + factoryPath,
	}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
	if p.ticket.Manifest.ImageSHA256 != factory.ImageSHA256 {
		t.Fatalf("running image = %s, want factory after Previous preparation fails", p.ticket.Manifest.ImageSHA256)
	}
}
