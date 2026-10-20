package rooms

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func installSource(t *testing.T, title, script, art string) string {
	t.Helper()
	dir := t.TempDir()
	for file, text := range map[string]string{ManifestName: "id = 'test.room'\ntitle = '" + title + "'\nversion = 1\n", "main.lua": script, "art.txt": art} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestInstalledRoomUpdatePinsAssetsAndRollback(t *testing.T) {
	root := t.TempDir()
	first, err := Install(root, installSource(t, "One", "function draw() end", "old art"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Install(root, installSource(t, "Two", "function draw() end", "new art"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == second.Revision {
		t.Fatal("different bytes reused revision")
	}
	data, _ := fs.ReadFile(first.FS, "art.txt")
	if string(data) != "old art" {
		t.Fatal("running pack changed")
	}
	packs, err := LoadDir(root)
	if err != nil || len(packs) != 1 || packs[0].Title != "Two" {
		t.Fatalf("%v %v", packs, err)
	}
	if err = Rollback(root, first.ID, ""); err != nil {
		t.Fatal(err)
	}
	packs, err = LoadDir(root)
	if err != nil || packs[0].Revision != first.Revision {
		t.Fatalf("%v %v", packs, err)
	}
}

func TestInstallRejectsBrokenLuaAndSymlinksWithoutChangingSelection(t *testing.T) {
	root := t.TempDir()
	good, err := Install(root, installSource(t, "Good", "function draw() end", "art"))
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"function broken(", "function draw() end"} {
		source := installSource(t, "Bad", script, "art")
		if strings.Contains(script, "draw") {
			if err := os.Symlink("/etc/passwd", filepath.Join(source, "escape")); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Install(root, source); err == nil {
			t.Fatal("invalid pack accepted")
		}
		packs, err := LoadDir(root)
		if err != nil || len(packs) != 1 || packs[0].Revision != good.Revision {
			t.Fatalf("selection lost: %v %v", packs, err)
		}
	}
}

func TestRollbackCannotUndoConcurrentInstall(t *testing.T) {
	root := t.TempDir()
	one, err := Install(root, installSource(t, "One", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	two, err := Install(root, installSource(t, "Two", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err = Rollback(root, one.ID, one.Revision); err == nil {
		t.Fatal("stale rollback accepted")
	}
	packs, err := LoadDir(root)
	if err != nil || packs[0].Revision != two.Revision {
		t.Fatalf("%v %v", packs, err)
	}
}

func TestInstallLockAndFirstFailurePreserveLoosePack(t *testing.T) {
	root := t.TempDir()
	unlock, err := lockInstall(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, installSource(t, "Busy", "", "")); err == nil {
		t.Fatal("parallel writer accepted")
	}
	unlock()
	p, err := Install(root, installSource(t, "First", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err = Rollback(root, p.ID, p.Revision); err != nil {
		t.Fatal(err)
	}
	packs, err := LoadDir(root)
	if err != nil || len(packs) != 0 {
		t.Fatalf("%v %v", packs, err)
	}
}

func TestInstallIdempotentKeepsPreviousAndRejectsOversize(t *testing.T) {
	root := t.TempDir()
	first, err := Install(root, installSource(t, "One", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	source := installSource(t, "Two", "", "new")
	second, err := Install(root, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Install(root, source); err != nil {
		t.Fatal(err)
	}
	if err = Rollback(root, second.ID, ""); err != nil {
		t.Fatal(err)
	}
	packs, _ := LoadDir(root)
	if packs[0].Revision != first.Revision {
		t.Fatal("idempotent install lost previous")
	}
	if err = os.WriteFile(filepath.Join(source, "large"), make([]byte, maxAssetBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Install(root, source); err == nil {
		t.Fatal("oversized file accepted")
	}
}

func TestInstalledArtworkUsesNewSnapshotWithoutChangingLiveOldRoom(t *testing.T) {
	root := t.TempDir()
	create := func(c color.RGBA) Pack {
		source := installSource(t, "Artwork", "local art; function load() art = image.load('art.png') end; function draw() end", "")
		bitmap := image.NewRGBA(image.Rect(0, 0, 1, 1))
		bitmap.SetRGBA(0, 0, c)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, bitmap); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "art.png"), encoded.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		pack, err := Install(root, source)
		if err != nil {
			t.Fatal(err)
		}
		return pack
	}
	red, blue := color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}
	old := newRoom(t, create(red), Options{})
	fresh := newRoom(t, create(blue), Options{})
	for _, check := range []struct {
		room *Instance
		want color.RGBA
	}{{old, red}, {fresh, blue}} {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			check.room.Step(time.Now())
			if bitmap := check.room.Images()["asset:art.png"]; bitmap != nil {
				if got := bitmap.RGBAAt(0, 0); got != check.want {
					t.Fatalf("artwork = %v want %v", got, check.want)
				}
				break
			}
			time.Sleep(time.Millisecond)
		}
		if check.room.Images()["asset:art.png"] == nil {
			t.Fatal("artwork did not decode")
		}
	}
}
