package appliancedata

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
)

func TestSelectedBoardUpdateGate(t *testing.T) {
	root := t.TempDir()
	image := make([]byte, 4096)
	image[1080], image[1081], image[1120] = 0x53, 0xef, 0x40
	m := testManifest(image)
	m.Board = release.BoardDE25
	paths := GatePaths{Board: release.BoardDE25, Factory: filepath.Join(root, "factory.json"),
		MountInfo: filepath.Join(root, "mountinfo"), ReleaseRoot: filepath.Join(root, "releases"),
		BootJSON: filepath.Join(root, "releases/boot.json"), BootIDPath: filepath.Join(root, "boot_id")}
	writeFactory(t, paths.Factory, m)
	writeBootTicket(t, paths.BootJSON, paths.BootIDPath, "de25-boot", m.ImageSHA256, false)
	s, err := store.NewForBoard(paths.ReleaseRoot, m, release.BoardDE25)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(image)); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadUpdateGate(paths); err != nil || got != (Gate{}) {
		t.Fatalf("selected board gate: %+v %v", got, err)
	}
	paths.Board = release.Board
	if _, err = ReadUpdateGate(paths); err == nil {
		t.Fatal("DE10 gate admitted DE25 factory")
	}
	paths.Board = release.BoardDE25
	writeBootTicket(t, paths.BootJSON, paths.BootIDPath, "de25-boot", m.ImageSHA256, true)
	if got, err := ReadUpdateGate(paths); err != nil || !got.Trial {
		t.Fatalf("selected board bypassed trial gate: %+v %v", got, err)
	}
}
