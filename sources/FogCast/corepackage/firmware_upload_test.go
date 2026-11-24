package corepackage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/misteross/expansion"
)

// de25UploadManifest rewrites the shared mailbox-upload fixture into the
// descriptor the DE25 runtime admits for one standard machine: the shell, the
// machine's media and the upload interface. Nothing exports such a package yet.
func de25UploadManifest(t *testing.T, core string, size int) []byte {
	t.Helper()
	text, _ := nanoUploadFixture(t, size)
	start, end := strings.Index(text, "[[interfaces]]"), strings.Index(text, "[build]")
	if start < 0 || end < start {
		t.Fatal("fixture manifest layout changed")
	}
	interfaces := ""
	add := func(id string, major, minor int) {
		interfaces += fmt.Sprintf("[[interfaces]]\nid = %q\nmajor = %d\nminor = %d\nrequired = true\n\n", id, major, minor)
	}
	for _, id := range []string{"fes.video.fixed-720p60", "fes.keyboard.hid", "fes.gamepad.ports",
		"fes.audio.pcm-s16-stereo-48k", "fes.video.session-display", "fes.fabric.video.display-plane"} {
		add(id, 1, 0)
	}
	add("fes.fabric.de25-scanout", 1, 3)
	if core == "fes.atari-st" {
		for _, id := range []string{"fes.mouse.relative", "fes.media.atari-st-floppy", "fes.media.atari-st-floppy-write",
			"fes.media.atari-st-floppy-geometry"} {
			add(id, 1, 0)
		}
	} else if core == "fes.c64" {
		add("fes.media.c64-disk", 1, 0)
	} else if core == "fes.pcxt-dos" {
		add("fes.media.pcxt-floppy", 1, 0)
		add("fes.media.pcxt-floppy-write", 1, 0)
	} else {
		add("fes.media.spectrum-tape", 2, 0)
	}
	text = text[:start] + interfaces + text[end:]
	if abi := DE25ComputerABI(core); abi.Minor != 0 {
		text = strings.Replace(text, "id = \"fes.simple-game\"\nmajor = 1\nminor = 0\n", fmt.Sprintf("id = \"fes.simple-game\"\nmajor = 1\nminor = %d\n", abi.Minor), 1)
	}
	for old, replacement := range map[string]string{
		`id = "fes.pong"`:                   fmt.Sprintf("id = %q", core),
		`programming_profile = "fes-gp-v1"`: `programming_profile = "de25-linux-computer-v1"`,
		`id = "fes.simple-game"`:            `id = "fes.computer"`,
	} {
		if !strings.Contains(text, old) {
			t.Fatal("fixture manifest lacks", old)
		}
		text = strings.Replace(text, old, replacement, 1)
	}
	return []byte(text)
}

// nanoUploadFixture is the shared mailbox-upload manifest and map for one
// image size. The C64's 36 KiB and the PC/XT's 68 KiB have no shared fixture;
// each is the next smaller one with the size, and the map's length and
// digest, rewritten.
func nanoUploadFixture(t *testing.T, size int) (string, []byte) {
	t.Helper()
	root := filepath.Join("testdata", "core-bundle-v3")
	source := size
	if size == 36864 {
		source = 32768
	}
	if size == 69632 {
		source = 65536
	}
	raw, err := os.ReadFile(filepath.Join(root, "manifests", fmt.Sprintf("valid-nano-upload-%d.toml", source)))
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := os.ReadFile(filepath.Join(root, "maps", fmt.Sprintf("valid-nano-upload-%d.json", source)))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if source != size {
		before := fmt.Sprintf("\"source_size\":%d}\n", source)
		if !bytes.HasSuffix(mapping, []byte(before)) {
			t.Fatal("fixture map layout changed")
		}
		original := mapping
		mapping = append(bytes.TrimSuffix(append([]byte(nil), mapping...), []byte(before)),
			fmt.Sprintf("\"source_size\":%d}\n", size)...)
		for old, replacement := range map[string]string{
			fmt.Sprintf("source_size = %d\n", source):       fmt.Sprintf("source_size = %d\n", size),
			fmt.Sprintf("size = %d\n", len(original)):       fmt.Sprintf("size = %d\n", len(mapping)),
			fmt.Sprintf("sha256 = %q", romDigest(original)): fmt.Sprintf("sha256 = %q", romDigest(mapping)),
		} {
			if strings.Count(text, old) != 1 {
				t.Fatal("fixture manifest lacks", old)
			}
			text = strings.Replace(text, old, replacement, 1)
		}
	}
	return text, mapping
}

// de25UploadPackage is that manifest with the fixture's payload and map.
func de25UploadPackage(t *testing.T, core string, size int, transforms ...func(string) string) []byte {
	t.Helper()
	root := filepath.Join("testdata", "core-bundle-v3")
	payload, err := os.ReadFile(filepath.Join(root, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	_, mapping := nanoUploadFixture(t, size)
	manifest := string(de25UploadManifest(t, core, size))
	for _, transform := range transforms {
		manifest = transform(manifest)
	}
	return canonicalArchiveWithROMMap([]byte(manifest), payload, mapping)
}

func firmwareImage(size int) []byte {
	image := make([]byte, size)
	for i := range image {
		image[i] = byte(i*13 + 7)
	}
	return image
}

// The ROM source reaches the kit as itself: nothing is linked, the receipt
// binds it to the package's unchanged payload, and a restart adopts it.
func TestFirmwareUploadStagesTheSourceBesideTheUnchangedPackage(t *testing.T) {
	for _, machine := range []struct {
		core string
		size int
	}{{"fes.spectrum", 16384}, {"fes.spectrum", 32768}, {"fes.spectrum128", 32768}, {"fes.spectrum-plus3", 65536},
		{"fes.pentagon", 49152}, {"fes.atari-st", 196608},
		{"fes.c64", 36864}} {
		t.Run(fmt.Sprint(machine.core, "-", machine.size), func(t *testing.T) {
			pkg, rom := de25UploadPackage(t, machine.core, machine.size), firmwareImage(machine.size)
			in := ROMInput{Package: pkg, ROM: rom}
			if machine.core == "fes.atari-st" {
				disk := bytes.Repeat([]byte{0xa5}, InitialMediaBytes)
				in.InitialMedia = &InitialMedia{GameID: "desk", BaseMediaID: romDigest(disk), Bytes: disk}
			}
			data, err := WriteROMInput(in)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			staged, err := StageROMInputBytes(context.Background(), root, data)
			if err != nil {
				t.Fatal(err)
			}
			d := staged.Descriptor
			if !FirmwareUpload(d) || staged.ProgrammedPath != "" || staged.FirmwarePath == "" || staged.ROMLink == nil ||
				!staged.ROMLink.ValidFor(d) || staged.ROMLink.SourceSHA256 != romDigest(rom) ||
				staged.ROMLink.ProgrammedSHA256 != d.Payload.SHA256 || staged.ROMLink.ProgrammedSize != d.Payload.Size ||
				staged.Composition != nil || staged.SlotComposition != nil || staged.PartsComposition != nil ||
				(staged.InitialMedia != nil) != (in.InitialMedia != nil) {
				t.Fatalf("staged upload package: %+v", staged)
			}
			delivered, err := os.ReadFile(staged.FirmwarePath)
			if err != nil || !bytes.Equal(delivered, rom) {
				t.Fatal("staged firmware is not the source", err)
			}
			if _, err = os.Stat(filepath.Join(filepath.Dir(staged.FirmwarePath), "programmed.rbf")); !os.IsNotExist(err) {
				t.Fatal("upload package staged a programmed bitstream", err)
			}
			core, err := os.ReadFile(filepath.Join(staged.Directory, "core.rbf"))
			if err != nil || romDigest(core) != d.Payload.SHA256 {
				t.Fatal("staged payload changed", err)
			}
			adopted, err := Adopt(root)
			if err != nil || len(adopted) != 1 || adopted[0].FirmwarePath != staged.FirmwarePath ||
				adopted[0].ProgrammedPath != "" || *adopted[0].ROMLink != *staged.ROMLink {
				t.Fatalf("adopt: %+v %v", adopted, err)
			}
			// A second delivered file, or changed source bytes, is not adopted.
			directory := filepath.Dir(staged.FirmwarePath)
			if err = os.Chmod(directory, 0700); err != nil {
				t.Fatal(err)
			}
			extra := filepath.Join(directory, "programmed.rbf")
			if staged.InitialMedia == nil {
				if err = os.WriteFile(extra, core, 0400); err != nil {
					t.Fatal(err)
				}
				if err = os.Chmod(directory, 0500); err != nil {
					t.Fatal(err)
				}
				if _, err = Adopt(root); err == nil {
					t.Fatal("publication with both a bitstream and a source adopted")
				}
				if err = os.Chmod(directory, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(extra); err != nil {
					t.Fatal(err)
				}
			}
			delivered[len(delivered)/2] ^= 1
			if err = os.Remove(staged.FirmwarePath); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(staged.FirmwarePath, delivered, 0400); err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(directory, 0500); err != nil {
				t.Fatal(err)
			}
			if _, err = Adopt(root); err == nil {
				t.Fatal("changed firmware source adopted")
			}
			if err = adopted[0].Cleanup(); err != nil {
				t.Fatal(err)
			}
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Fatal("cleanup leaked")
			}
		})
	}
}

func TestFirmwareUploadRefusesOtherMachinesSizesAndCycloneVInputs(t *testing.T) {
	ctx := context.Background()
	replace := func(old, replacement string) func(string) string {
		return func(text string) string {
			if !strings.Contains(text, old) {
				t.Fatal("manifest lacks", old)
			}
			return strings.Replace(text, old, replacement, 1)
		}
	}
	spectrum := de25UploadPackage(t, "fes.spectrum", 16384)
	for name, in := range map[string]ROMInput{
		"short ROM":                  {Package: spectrum, ROM: firmwareImage(16384 - 1024)},
		"long ROM":                   {Package: spectrum, ROM: firmwareImage(32768)},
		"ST image on a Spectrum":     {Package: de25UploadPackage(t, "fes.spectrum", 196608), ROM: firmwareImage(196608)},
		"48K image on Spectrum 128K": {Package: de25UploadPackage(t, "fes.spectrum128", 16384), ROM: firmwareImage(16384)},
		"Spectrum image on an ST":    {Package: de25UploadPackage(t, "fes.atari-st", 16384), ROM: firmwareImage(16384)},
		"Spectrum image on a C64":    {Package: de25UploadPackage(t, "fes.c64", 32768), ROM: firmwareImage(32768)},
		"smallest format size":       {Package: de25UploadPackage(t, "fes.spectrum", 1024), ROM: firmwareImage(1024)},
		"another machine":            {Package: de25UploadPackage(t, "fes.zx81", 16384), ROM: firmwareImage(16384)},
		"another profile":            {Package: de25UploadPackage(t, "fes.spectrum", 16384, replace(`"de25-linux-computer-v1"`, `"de25-hps-first-v1"`)), ROM: firmwareImage(16384)},
		"another ABI":                {Package: de25UploadPackage(t, "fes.spectrum", 16384, replace(`id = "fes.computer"`, `id = "fes.application"`)), ROM: firmwareImage(16384)},
		"expansion":                  {Package: spectrum, ROM: firmwareImage(16384), Expansion: &expansion.Asset{}},
		"slot card":                  {Package: spectrum, ROM: firmwareImage(16384), SlotExpansions: []expansion.Asset{{}}},
		"video part":                 {Package: spectrum, ROM: firmwareImage(16384), Parts: []expansion.Asset{{}}},
		"ST disk on a Spectrum":      {Package: spectrum, ROM: firmwareImage(16384), InitialMedia: &InitialMedia{GameID: "desk", BaseMediaID: romDigest(bytes.Repeat([]byte{0xa5}, InitialMediaBytes)), Bytes: bytes.Repeat([]byte{0xa5}, InitialMediaBytes)}},
		"missing upload interface":   {Package: de25UploadPackage(t, "fes.spectrum", 16384, replace("[[interfaces]]\nid = \"fes.fabric.firmware-upload\"\nmajor = 1\nminor = 0\nrequired = true\n", "")), ROM: firmwareImage(16384)},
		"held ROM interface":         {Package: de25UploadPackage(t, "fes.spectrum", 16384, replace(`"fes.fabric.firmware-upload"`, `"fes.embedded.rom"`)), ROM: firmwareImage(16384)},
	} {
		if _, err := PrepareROMInput(ctx, in); err == nil {
			t.Error("accepted:", name)
		}
	}
	// A DE25 package cannot carry a Cyclone V bitstream map, whatever it declares.
	root := filepath.Join("testdata", "core-bundle-v3")
	payload, err := os.ReadFile(filepath.Join(root, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	cyclone, err := os.ReadFile(filepath.Join(root, "maps", "valid-basic.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(de25UploadManifest(t, "fes.spectrum", 16384))
	sealed, err := os.ReadFile(filepath.Join(root, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest = manifest[:strings.Index(manifest, "[rom]")] + string(sealed)[strings.Index(string(sealed), "[rom]"):]
	if _, err = PrepareROMInput(ctx, ROMInput{Package: canonicalArchiveWithROMMap([]byte(manifest), payload, cyclone), ROM: firmwareImage(8192)}); err == nil {
		t.Error("DE25 package with a Cyclone V ROM map accepted")
	}
}

// The envelope is closed: its receipt is derived from the members, and an
// upload has no link evidence to carry.
func TestFirmwareUploadTransportBindsTheSourceIdentity(t *testing.T) {
	ctx := context.Background()
	in := ROMInput{Package: de25UploadPackage(t, "fes.spectrum", 16384), ROM: firmwareImage(16384)}
	prepared, err := prepareROMInput(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*romInputReceipt){
		"source digest":     func(r *romInputReceipt) { r.SourceSHA256 = romDigest([]byte("another ROM")) },
		"source size":       func(r *romInputReceipt) { r.SourceSize = 32768 },
		"map digest":        func(r *romInputReceipt) { r.MapSHA256 = romDigest([]byte("another map")) },
		"package":           func(r *romInputReceipt) { r.PackageID = romDigest([]byte("another package")) },
		"programmed digest": func(r *romInputReceipt) { r.ProgrammedSHA256, r.ProgrammedSize = romDigest(in.ROM), 16384 },
	} {
		receipt := prepared.receipt
		mutate(&receipt)
		data, err := writeROMInputReceiptBytes(in, receipt)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = StageROMInputBytes(ctx, t.TempDir(), data); err == nil {
			t.Error("staged with a changed receipt:", name)
		}
	}
	if _, err = linkPreparedROMInput(ctx, prepared, romInputReceipt{ProgrammedSHA256: romDigest(in.ROM), ProgrammedSize: 16384}); err == nil {
		t.Error("upload accepted link evidence")
	}
	other := in
	other.ROM = firmwareImage(16384)
	other.ROM[0] ^= 1
	first, err := linkROMInput(ctx, in, romInputReceipt{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := linkROMInput(ctx, other, romInputReceipt{})
	if err != nil || first.identity.SourceSHA256 == second.identity.SourceSHA256 ||
		first.identity.ProgrammedSHA256 != second.identity.ProgrammedSHA256 || first.programmed != nil {
		t.Fatal("identity does not follow the source alone", err)
	}
}
