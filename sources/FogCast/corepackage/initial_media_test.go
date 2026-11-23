package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

func initialSTInput(t *testing.T) ROMInput {
	t.Helper()
	in := stVideoROMFixture(t)
	manifest, base, mapping, err := readArchive(in.Package)
	if err != nil {
		t.Fatal(err)
	}
	manifest = append(manifest, []byte("\n[[interfaces]]\nid = \"fes.media.atari-st-floppy\"\nmajor = 1\nminor = 0\nrequired = true\n\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n")...)
	in.Package = romArchive(manifest, base, mapping)
	for n, part := range in.Parts {
		m := part.Manifest
		m.ShellPackageID = packageIdentity(manifest, base, mapping)
		in.Parts[n], err = expansion.NewAsset(m, part.Cart)
		if err != nil {
			t.Fatal(err)
		}
	}
	disk := bytes.Repeat([]byte{0xa5}, InitialMediaBytes)
	in.InitialMedia = &InitialMedia{GameID: "st-desktop", BaseMediaID: romDigest(disk), Bytes: disk}
	return in
}

func TestInitialSTMediaRoundTripStageAdopt(t *testing.T) {
	for _, kind := range []string{"plain", "slot", "video"} {
		t.Run(kind, func(t *testing.T) {
			in := initialSTInput(t)
			if kind == "plain" {
				in.Parts = nil
			}
			if kind == "slot" {
				in.SlotExpansions = in.Parts[1:]
				in.Parts = nil
			}
			prepared, err := PrepareROMInput(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			without := in
			without.InitialMedia = nil
			diskless, err := PrepareROMInput(context.Background(), without)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(prepared.PartsComposition, diskless.PartsComposition) || !reflect.DeepEqual(prepared.SlotComposition, diskless.SlotComposition) {
				t.Fatal("initial media changed programmed identity")
			}
			linked, err := linkROMInput(context.Background(), in, romInputReceipt{})
			if err != nil {
				t.Fatal(err)
			}
			linkedDiskless, err := linkROMInput(context.Background(), without, romInputReceipt{})
			if err != nil || !bytes.Equal(linked.programmed, linkedDiskless.programmed) || linked.identity != linkedDiskless.identity {
				t.Fatal("initial disk changed linked ROM", err)
			}
			root := t.TempDir()
			envelope := prepared.Bytes()
			staged, err := StageROMInputBytes(context.Background(), root, envelope)
			if err != nil {
				t.Fatal(err)
			}
			if staged.InitialMedia == nil || staged.InitialMedia.BaseMediaID != in.InitialMedia.BaseMediaID {
				t.Fatal("initial identity missing")
			}
			actual, err := os.ReadFile(staged.InitialMedia.Path)
			if err != nil || !bytes.Equal(actual, in.InitialMedia.Bytes) {
				t.Fatal("initial source differs", err)
			}
			for path, mode := range map[string]os.FileMode{staged.InitialMedia.Path: 0400, filepath.Dir(staged.InitialMedia.Path): 0500} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != mode {
					t.Fatal("unsealed initial source", path, err)
				}
			}
			// The borrowing entry point must leave independent source publications.
			clear(envelope)
			adopted, err := Adopt(root)
			if err != nil || len(adopted) != 1 || !reflect.DeepEqual(adopted[0].InitialMedia, staged.InitialMedia) {
				t.Fatal("initial source not independently adopted", err)
			}
			if err := staged.Cleanup(); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("cleanup leaked companions", err)
			}
		})
	}
}

func TestInitialSTMediaRejectsInvalidSourceAndContract(t *testing.T) {
	for name, change := range map[string]func(*ROMInput){
		"hash":  func(in *ROMInput) { in.InitialMedia.BaseMediaID = strings.Repeat("b", 64) },
		"short": func(in *ROMInput) { in.InitialMedia.Bytes = in.InitialMedia.Bytes[:InitialMediaBytes-1] },
		"unit":  func(in *ROMInput) { in.InitialMedia.Unit = 1 },
		"game":  func(in *ROMInput) { in.InitialMedia.GameID = "../escape" },
		"readonly": func(in *ROMInput) {
			manifest, base, mapping, _ := readArchive(in.Package)
			manifest = bytes.Replace(manifest, []byte("fes.media.atari-st-floppy-write"), []byte("fes.media.other"), 1)
			in.Package = romArchive(manifest, base, mapping)
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := initialSTInput(t)
			change(&in)
			if _, err := WriteROMInput(in); err == nil {
				t.Fatal("accepted invalid initial disk")
			}
		})
	}
	in := initialSTInput(t)
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	reader := &canonicalReader{data: data}
	raw, err := reader.read("rom-link.json", 4096)
	if err != nil {
		t.Fatal(err)
	}
	rest := data[reader.offset:]
	var parsed romInputReceipt
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	initialJSON, err := json.Marshal(parsed.InitialMedia)
	if err != nil {
		t.Fatal(err)
	}
	for name, receipt := range map[string][]byte{
		"unknown":   bytes.Replace(raw, []byte(`"game_id":`), []byte(`"unknown":1,"game_id":`), 1),
		"duplicate": bytes.Replace(raw, []byte(`"game_id":"st-desktop"`), []byte(`"game_id":"st-desktop","game_id":"st-desktop"`), 1),
		"null":      bytes.Replace(raw, initialJSON, []byte(`null`), 1),
		"trailing":  append(append([]byte{}, raw...), []byte(` {}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			bad := append(canonicalHeader("rom-link.json", int64(len(receipt))), receipt...)
			bad = append(bad, make([]byte, (512-len(receipt)%512)%512)...)
			bad = append(bad, rest...)
			assertInitialStageRejected(t, bad)
		})
	}
	var receipt romInputReceipt
	if json.Unmarshal(raw, &receipt) != nil {
		t.Fatal("receipt")
	}
	receipt.InitialMedia = nil
	bad, err := writeROMInputReceiptBytes(in, receipt)
	if err != nil {
		t.Fatal(err)
	}
	assertInitialStageRejected(t, bad)
	noDisk := in
	noDisk.InitialMedia = nil
	receipt.InitialMedia = &initialMediaReceipt{GameID: in.InitialMedia.GameID, BaseMediaID: in.InitialMedia.BaseMediaID, Size: InitialMediaBytes}
	bad, err = writeROMInputReceiptBytes(noDisk, receipt)
	if err != nil {
		t.Fatal(err)
	}
	assertInitialStageRejected(t, bad)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	if _, err := StageROMInput(ctx, root, int64(len(data)), bytes.NewReader(data)); err == nil {
		t.Fatal("ignored cancellation")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("cancel leaked publication")
	}
}
func assertInitialStageRejected(t *testing.T, data []byte) {
	t.Helper()
	root := t.TempDir()
	if _, err := StageROMInput(context.Background(), root, int64(len(data)), bytes.NewReader(data)); err == nil {
		t.Fatal("accepted malformed initial source envelope")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("failed admission leaked publication")
	}
}

func TestInitialSTMediaAdoptionRejectsTamperedCompanion(t *testing.T) {
	in := initialSTInput(t)
	in.Parts = nil
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged, err := StageROMInput(context.Background(), root, int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if err := os.Chmod(staged.InitialMedia.Path, 0600); err != nil {
		t.Fatal(err)
	}
	disk := append([]byte{}, in.InitialMedia.Bytes...)
	disk[0] ^= 1
	if err := os.WriteFile(staged.InitialMedia.Path, disk, 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(staged.InitialMedia.Path, 0400); err != nil {
		t.Fatal(err)
	}
	if _, err := Adopt(root); err == nil {
		t.Fatal("adopted altered initial disk")
	}
}

// The DE25 PC/XT uploads its firmware, so its start disk rides in the ROM
// input as the ST's does. The receiver learns the medium from the package,
// stages it as the PC/XT floppy, and refuses any other size or package.
func TestInitialPcxtMediaRidesWithUploadedFirmware(t *testing.T) {
	pkg, rom := de25UploadPackage(t, "fes.pcxt-dos", 69632), firmwareImage(69632)
	disk := bytes.Repeat([]byte{0xf6}, 368640)
	in := ROMInput{Package: pkg, ROM: rom, InitialMedia: &InitialMedia{InterfaceID: "fes.media.pcxt-floppy", GameID: "freedos", BaseMediaID: romDigest(disk), Bytes: disk}}
	data, err := WriteROMInput(in)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged, err := StageROMInputBytes(context.Background(), root, data)
	if err != nil {
		t.Fatal(err)
	}
	want := StagedInitialMedia{InterfaceID: "fes.media.pcxt-floppy", GameID: "freedos", BaseMediaID: romDigest(disk), Size: 368640}
	if staged.FirmwarePath == "" || staged.InitialMedia == nil {
		t.Fatalf("staged PC/XT launch: %+v", staged)
	}
	got := *staged.InitialMedia
	got.Path = ""
	if got != want {
		t.Fatalf("staged PC/XT disk = %+v, want %+v", got, want)
	}
	actual, err := os.ReadFile(staged.InitialMedia.Path)
	if err != nil || !bytes.Equal(actual, disk) {
		t.Fatal("staged PC/XT disk differs", err)
	}
	adopted, err := Adopt(root)
	if err != nil || len(adopted) != 1 || !reflect.DeepEqual(adopted[0].InitialMedia, staged.InitialMedia) {
		t.Fatal("PC/XT disk not adopted with its medium", err)
	}
	if err := staged.Cleanup(); err != nil {
		t.Fatal(err)
	}
	short := disk[:327680]
	for name, bad := range map[string]ROMInput{
		"320 KiB disk": {Package: pkg, ROM: rom, InitialMedia: &InitialMedia{GameID: "freedos", BaseMediaID: romDigest(short), Bytes: short}},
		"ST medium":    {Package: pkg, ROM: rom, InitialMedia: &InitialMedia{InterfaceID: "fes.media.atari-st-floppy", GameID: "freedos", BaseMediaID: romDigest(disk), Bytes: disk}},
		"wrong digest": {Package: pkg, ROM: rom, InitialMedia: &InitialMedia{GameID: "freedos", BaseMediaID: romDigest(short), Bytes: disk}},
		"PC/XT disk on a C64": {Package: de25UploadPackage(t, "fes.c64", 36864), ROM: firmwareImage(36864),
			InitialMedia: &InitialMedia{InterfaceID: "fes.media.pcxt-floppy", GameID: "freedos", BaseMediaID: romDigest(disk), Bytes: disk}},
	} {
		if _, err := WriteROMInput(bad); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
