package corepackage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pcxtInputFixture(t *testing.T) PcxtInitialInput {
	t.Helper()
	base := filepath.Join("testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(base, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(base, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte(`id = "fes.pong"`), []byte(`id = "fes.pcxt-dos"`), 1)
	manifest = bytes.Replace(manifest, []byte(`id = "fes.simple-game"`), []byte(`id = "fes.computer"`), 1)
	manifest = bytes.Replace(manifest, []byte("[abi]\nid = \"fes.computer\"\nmajor = 1\nminor = 0"), []byte("[abi]\nid = \"fes.computer\"\nmajor = 1\nminor = 1"), 1)
	manifest = bytes.Replace(manifest, []byte(`id = "fes.gamepad"`), []byte(`id = "fes.media.pcxt-floppy"`), 1)
	manifest = append(manifest, []byte("\n[[interfaces]]\nid = \"fes.media.pcxt-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n")...)
	disk := bytes.Repeat([]byte{0x5a}, 368640)
	return PcxtInitialInput{Package: canonicalArchive(manifest, payload), Media: InitialMedia{GameID: "dos-test", BaseMediaID: romDigest(disk), Bytes: disk}}
}
func TestPcxtInitialInputSealedAndAdopted(t *testing.T) {
	in := pcxtInputFixture(t)
	data, err := WritePcxtInitialInput(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	s, err := StagePcxtInitialInput(context.Background(), root, data)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(s.InitialMedia.Path)
	if err != nil || !bytes.Equal(disk, in.Media.Bytes) {
		t.Fatal("staged disk mismatch", err)
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || adopted[0].InitialMedia == nil {
		t.Fatalf("adopt: %v %v", adopted, err)
	}
	if err = adopted[0].Cleanup(); err != nil {
		t.Fatal(err)
	}
}
func TestPcxtInitialInputRejectsBadBinding(t *testing.T) {
	for name, change := range map[string]func(*PcxtInitialInput){
		"short":       func(i *PcxtInitialInput) { i.Media.Bytes = i.Media.Bytes[:len(i.Media.Bytes)-1] },
		"wrong hash":  func(i *PcxtInitialInput) { i.Media.BaseMediaID = strings.Repeat("a", 64) },
		"wrong unit":  func(i *PcxtInitialInput) { i.Media.Unit = 1 },
		"wrong game":  func(i *PcxtInitialInput) { i.Media.GameID = "../escape" },
		"bad package": func(i *PcxtInitialInput) { i.Package[100] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			in := pcxtInputFixture(t)
			change(&in)
			if _, err := WritePcxtInitialInput(in); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	in := pcxtInputFixture(t)
	data, err := WritePcxtInitialInput(in)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1025] ^= 1
	if _, _, err = readPcxtInitialInput(data); err == nil {
		t.Fatal("modified disk accepted")
	}
}
