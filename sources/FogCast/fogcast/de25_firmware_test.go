package fogcast

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

// de25UploadPackageFixture is the shared mailbox-upload fixture rewritten into
// the descriptor the DE25 runtime admits for one standard machine. Nothing
// exports such a package yet.
func de25UploadPackageFixture(t *testing.T, core string, size int) []byte {
	t.Helper()
	root := "../corepackage/testdata/core-bundle-v3/"
	read := func(name string) []byte {
		raw, err := os.ReadFile(root + name)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	text := string(read(fmt.Sprintf("manifests/valid-nano-upload-%d.toml", size)))
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
	} else {
		add("fes.media.spectrum-tape", 2, 0)
	}
	text = text[:start] + interfaces + text[end:]
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
	return libraryROMPackageArchive(t, []byte(text), read("payloads/fes-fixture.rbf"),
		read(fmt.Sprintf("maps/valid-nano-upload-%d.json", size)))
}

// A DE25 Spectrum or Atari ST needs its ROM like the DE10 packages do: the
// title is blocked until one of the sealed size is chosen, and the launch
// then sends the ROM source, which the kit stages for the runtime to upload.
func TestDE25FirmwareUploadTitleNeedsItsROMThenSendsTheSource(t *testing.T) {
	for _, machine := range []struct {
		core string
		size int
	}{{"fes.spectrum", 16384}, {"fes.spectrum", 32768}, {"fes.atari-st", 196608}} {
		t.Run(fmt.Sprint(machine.core, "-", machine.size), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
			if err != nil {
				t.Fatal(err)
			}
			client := &defaultMediaPackageClient{packageLibraryClient: &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}}
			s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			s.corePackages = packages
			linker := &forbiddenHostROMLinker{}
			s.SetMachineROMLinker(linker)
			archive := de25UploadPackageFixture(t, machine.core, machine.size)
			installed, _, err := s.ImportCorePackage(ctx, int64(len(archive)), bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			d := installed.Descriptor
			if !corepackage.FirmwareUpload(d) {
				t.Fatal("fixture is not a firmware-upload package")
			}
			entry, err := s.CreateCoreEntry(ctx, "DE25 title", installed.PackageID)
			if err != nil {
				t.Fatal(err)
			}
			readiness, err := s.CoreCompositions(ctx, []string{entry.GameID})
			missing := readiness[entry.GameID]
			if err != nil || !missing.FirmwareRequired || missing.FirmwareReady || missing.ROMRequired ||
				missing.ROMRole != protocol.FirmwareRole || missing.ROMSourceSize != int64(machine.size) {
				t.Fatalf("readiness without a ROM: %+v %v", missing, err)
			}
			game := hostclient.Game{State: "available", RootOnline: true, Launchable: true, FirmwareRequired: missing.FirmwareRequired, FirmwareReady: missing.FirmwareReady, ROMRole: missing.ROMRole}
			if game.LaunchBlock() != hostclient.LaunchMissingFirmware {
				t.Fatalf("title without its ROM is not blocked on firmware: %q", game.LaunchBlock())
			}
			if _, err = s.Launch(ctx, entry.GameID, nil); err == nil || client.coreCalls != 0 {
				t.Fatal("title without its ROM launched or contacted the kit", err)
			}
			// Another machine's image size is not this package's ROM.
			other := 32768
			if machine.size == other {
				other = 16384
			}
			wrong, _, err := store.ImportCoreMedia(ctx, bytes.Repeat([]byte{0x33}, other))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, d.ROM.ID, "", wrong.MediaID); err == nil {
				t.Fatal("wrong-size ROM selected")
			}
			rom := bytes.Repeat([]byte{0x5a}, machine.size)
			media, _, err := store.ImportCoreMedia(ctx, rom)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, d.ROM.ID, "", media.MediaID); err != nil {
				t.Fatal(err)
			}
			readiness, err = s.CoreCompositions(ctx, []string{entry.GameID})
			selected := readiness[entry.GameID]
			if err != nil || !selected.FirmwareRequired || !selected.FirmwareReady || selected.ROMMediaID != media.MediaID {
				t.Fatalf("readiness with a ROM: %+v %v", selected, err)
			}
			staging := t.TempDir()
			var link corepackage.ROMLinkIdentity
			client.coreLoad = func(_ context.Context, size int64, r io.Reader) (protocol.Status, error) {
				body, err := io.ReadAll(r)
				if err != nil || int64(len(body)) != size {
					t.Fatal("transport size mismatch", err)
				}
				// The kit's own staging of this envelope: the ROM source
				// beside the unchanged package, and no programmed bitstream.
				staged, err := corepackage.StageROMInputBytes(ctx, staging, body)
				if err != nil {
					t.Fatal(err)
				}
				defer staged.Cleanup()
				delivered, err := os.ReadFile(staged.FirmwarePath)
				if err != nil || !bytes.Equal(delivered, rom) || staged.ProgrammedPath != "" || staged.PackageID != entry.PackageID {
					t.Fatal("kit did not stage the selected ROM source", err)
				}
				link = *staged.ROMLink
				core := d.Core.ID
				return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{
					PackageID: entry.PackageID, Generation: 4, ABI: protocol.RuntimeContract{ID: d.ABI.ID, Major: 1}, BuildID: d.Build.ID, ROMLink: &link}}, nil
			}
			response, err := s.Launch(ctx, entry.GameID, nil)
			if err != nil || response.Status.GameID == nil || *response.Status.GameID != entry.GameID {
				t.Fatalf("launch %+v %v", response, err)
			}
			if client.coreCalls != 1 || linker.calls != 0 || link.SourceSHA256 != media.MediaID ||
				link.ProgrammedSHA256 != d.Payload.SHA256 || link.ProgrammedSize != d.Payload.Size {
				t.Fatalf("loads=%d host links=%d receipt=%+v", client.coreCalls, linker.calls, link)
			}
		})
	}
}
