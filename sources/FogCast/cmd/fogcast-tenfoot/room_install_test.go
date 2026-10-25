package main

import (
	"bytes"
	"encoding/json"
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

func TestRoomInstallUsesLauncherPreferencesBeforeEnvironment(t *testing.T) {
	configHome := t.TempDir()
	configDir := filepath.Join(configHome, "FogCast")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	preferencesRoot := filepath.Join(t.TempDir(), "preferred-rooms")
	environmentRoot := filepath.Join(t.TempDir(), "environment-rooms")
	prefs, err := json.Marshal(map[string]string{"rooms_dir": preferencesRoot})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tenfoot.json"), prefs, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("FOGCAST_ROOMS", environmentRoot)

	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "room.toml"), []byte("id = 'prefs.room'\ntitle = 'Preferences'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.lua"), []byte("function draw() end"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"room-install", source}, &stdout, &stderr); code != 0 {
		t.Fatalf("room-install failed: %s", stderr.String())
	}

	packs, err := rooms.LoadDir(preferencesRoot)
	if err != nil || len(packs) != 1 || packs[0].ID != "prefs.room" {
		t.Fatalf("preference room directory = %v, %v", packs, err)
	}
	packs, err = rooms.LoadDir(environmentRoot)
	if err != nil || len(packs) != 0 {
		t.Fatalf("environment room directory was used: %v, %v", packs, err)
	}
}

func TestRoomCommandsExportAndInstallPortableArchive(t *testing.T) {
	source, root := t.TempDir(), t.TempDir()
	for name, data := range map[string]string{"room.toml": "id = 'portable.room'\ntitle = 'Portable'", "main.lua": "function draw() end"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "portable.room.zip")
	invoke := func(args ...string) int {
		var out, err bytes.Buffer
		code := run(args, &out, &err)
		if code != 0 {
			t.Log(err.String())
		}
		return code
	}
	if code := invoke("room-export", source); code != 2 {
		t.Fatalf("missing output: %d", code)
	}
	if code := invoke("room-export", "--output", archive, source); code != 0 {
		t.Fatal(code)
	}
	if code := invoke("room-install", "--rooms", root, archive); code != 0 {
		t.Fatal(code)
	}
	packs, err := rooms.LoadDir(root)
	if err != nil || len(packs) != 1 || packs[0].ID != "portable.room" {
		t.Fatalf("archive install: %v %v", packs, err)
	}
}
