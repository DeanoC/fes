package rooms

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableRoomRoundTripUpdateAndRollback(t *testing.T) {
	root := t.TempDir()
	source := installSource(t, "Portable", "function draw() end", "first art")
	first, err := Install(root, source)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "portable.room.zip")
	if err := Export(source, archive); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(archive)
	if err := Export(source, archive); err != nil {
		t.Fatal(err)
	}
	repeated, _ := os.ReadFile(archive)
	if !bytes.Equal(original, repeated) {
		t.Fatal("unchanged room export is not deterministic")
	}
	imported, err := Install(t.TempDir(), archive)
	if err != nil || imported.ID != first.ID || imported.Revision != first.Revision {
		t.Fatalf("round trip differs: %v %v", imported, err)
	}
	if err := os.WriteFile(filepath.Join(source, "art.txt"), []byte("new art"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Export(source, archive); err != nil {
		t.Fatal(err)
	}
	second, err := Install(root, archive)
	if err != nil || second.Revision == first.Revision {
		t.Fatalf("update: %v %v", second, err)
	}
	oldArt, _ := fs.ReadFile(first.FS, "art.txt")
	newArt, _ := fs.ReadFile(second.FS, "art.txt")
	if string(oldArt) != "first art" || string(newArt) != "new art" {
		t.Fatal("snapshots lost asset isolation")
	}
	if err := Rollback(root, first.ID, ""); err != nil {
		t.Fatal(err)
	}
	packs, err := LoadDir(root)
	if err != nil || len(packs) != 1 || packs[0].Revision != first.Revision {
		t.Fatalf("rollback: %v %v", packs, err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.lua"), []byte("function broken("), 0600); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(archive)
	if err := Export(source, archive); err == nil {
		t.Fatal("exported invalid Lua")
	}
	retained, _ := os.ReadFile(archive)
	if !bytes.Equal(good, retained) {
		t.Fatal("failed export replaced the published archive")
	}
}

func TestPortableRoomRejectsInvalidArchivesWithoutChangingSelection(t *testing.T) {
	root := t.TempDir()
	good, err := Install(root, installSource(t, "Working", "function draw() end", "art"))
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		name, data string
		mode       fs.FileMode
	}
	cases := map[string][]entry{
		"traversal":               {{"../escape", "escape", 0644}},
		"absolute":                {{"/escape", "escape", 0644}},
		"backslash":               {{`..\escape`, "escape", 0644}},
		"drive":                   {{"C:/escape", "escape", 0644}},
		"unclean":                 {{"assets/../escape", "escape", 0644}},
		"duplicate":               {{"room.toml", "one", 0644}, {"room.toml", "two", 0644}},
		"symlink":                 {{"linked", "/etc/passwd", fs.ModeSymlink | 0777}},
		"special":                 {{"fifo", "", fs.ModeNamedPipe | 0600}},
		"file-directory-conflict": {{"assets", "file", 0644}, {"assets/art", "art", 0644}},
		"missing-manifest":        {{"main.lua", "function draw() end", 0644}},
		"invalid-lua":             {{"room.toml", "id = 'test.room'\ntitle = 'Broken'", 0644}, {"main.lua", "function broken(", 0644}},
		"oversized-member":        {{"large", strings.Repeat("x", maxAssetBytes+1), 0644}},
	}
	for n := 0; n < maxInstallEntries+1; n++ {
		cases["too-many-entries"] = append(cases["too-many-entries"], entry{fmt.Sprintf("entry-%d", n), "", 0644})
	}
	chunk := strings.Repeat("x", maxAssetBytes)
	for n := 0; n < 5; n++ {
		cases["expanded-total"] = append(cases["expanded-total"], entry{fmt.Sprintf("large-%d", n), chunk, 0644})
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "invalid.room.zip")
			output, err := os.Create(file)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(output)
			for _, e := range entries {
				header := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
				header.SetMode(e.mode)
				member, err := writer.CreateHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := member.Write([]byte(e.data)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := output.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := Install(root, file); err == nil {
				t.Fatal("invalid archive installed")
			}
			packs, err := LoadDir(root)
			if err != nil || len(packs) != 1 || packs[0].Revision != good.Revision {
				t.Fatalf("failed import changed selection: %v %v", packs, err)
			}
		})
	}
	// Damage a stored member while leaving its central directory parseable, then
	// truncate the archive. Both must preserve the working selection.
	file := filepath.Join(t.TempDir(), "damaged.room.zip")
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	member, _ := writer.CreateHeader(&zip.FileHeader{Name: "room.toml", Method: zip.Store})
	member.Write([]byte("id = 'test.room'\ntitle = 'Payload'\n"))
	writer.Close()
	corrupt := append([]byte(nil), data.Bytes()...)
	offset := bytes.Index(corrupt, []byte("Payload"))
	corrupt[offset] = 'X'
	for _, payload := range [][]byte{corrupt, data.Bytes()[:data.Len()/2]} {
		if err := os.WriteFile(file, payload, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Install(root, file); err == nil {
			t.Fatal("damaged archive installed")
		}
		packs, err := LoadDir(root)
		if err != nil || len(packs) != 1 || packs[0].Revision != good.Revision {
			t.Fatal("damaged import changed selection")
		}
	}
}
