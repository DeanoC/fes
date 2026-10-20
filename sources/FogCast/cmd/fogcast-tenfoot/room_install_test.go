package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestRoomCommandsInstallUpdateRollbackWithoutStartingLauncher(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "room.toml"), []byte("id = 'cli.room'\ntitle = 'First'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.lua"), []byte("function draw() end"), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(command, arg string) int {
		var out, err bytes.Buffer
		code := run([]string{command, "--rooms", root, arg}, &out, &err)
		if code != 0 {
			t.Log(err.String())
		}
		return code
	}
	if code := invoke("room-install", source); code != 0 {
		t.Fatal(code)
	}
	packs, _ := rooms.LoadDir(root)
	first := packs[0].Revision
	if err := os.WriteFile(filepath.Join(source, "main.lua"), []byte("function draw() gfx.text('updated', 1, 1) end"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := invoke("room-install", source); code != 0 {
		t.Fatal(code)
	}
	packs, _ = rooms.LoadDir(root)
	if packs[0].Revision == first {
		t.Fatal("update not installed")
	}
	if code := invoke("room-rollback", "cli.room"); code != 0 {
		t.Fatal(code)
	}
	packs, _ = rooms.LoadDir(root)
	if packs[0].Revision != first {
		t.Fatal("rollback lost original")
	}
	if err := os.WriteFile(filepath.Join(source, "main.lua"), []byte("function invalid("), 0600); err != nil {
		t.Fatal(err)
	}
	if code := invoke("room-install", source); code == 0 {
		t.Fatal("broken update accepted")
	}
	packs, _ = rooms.LoadDir(root)
	if packs[0].Revision != first {
		t.Fatal("broken update changed selection")
	}
}
