package fogcast

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func twoROMLibraryPackageFixture(t *testing.T) []byte {
	t.Helper()
	base := "../corepackage/testdata/core-bundle-v2/"
	manifest, err := os.ReadFile(base + "manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(base + "payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	m := expansion.ROMMap{Format: 1, Device: expansion.Device, Encoding: "m10k-1024x10-v1", BaseSHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), SourceSize: 139264}
	for block := 0; block < 136; block++ {
		b := expansion.ROMBlock{BEL: fmt.Sprintf("M10K.005.%03d", block), SourceOffset: block * 1024, WordBits: make([][]uint32, 256)}
		for word := range b.WordBits {
			b.WordBits[word] = make([]uint32, 40)
			for bit := range b.WordBits[word] {
				b.WordBits[word][bit] = uint32(32*7605 + block*10240 + word*40 + bit)
			}
		}
		m.Blocks = append(m.Blocks, b)
	}
	mapping, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte("format = 2"), []byte("format = 4"), 1)
	manifest = bytes.Replace(manifest, []byte(`id = "fes.pong"`), []byte(`id = "fes.coleco"`), 1)
	manifest = bytes.Replace(manifest, []byte(`id = "fes.simple-game"`), []byte(`id = "fes.application"`), 1)
	manifest = append(manifest, []byte(fmt.Sprintf("\n[[roms]]\nid = \"coleco-bios\"\nrole = \"firmware\"\nsource_size = 8192\nsource_offset = 0\n\n[[roms]]\nid = \"coleco-cart\"\nrole = \"cartridge\"\nsource_size = 131072\nsource_offset = 8192\n\n[rom_map]\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(mapping), sha256.Sum256(mapping)))...)
	var out bytes.Buffer
	for _, member := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}, {"rom-map.json", mapping}} {
		h := make([]byte, 512)
		copy(h, member.name)
		copy(h[100:], "0000644\x00")
		copy(h[108:], "0000000\x00")
		copy(h[116:], "0000000\x00")
		copy(h[124:], fmt.Sprintf("%011o\x00", len(member.data)))
		copy(h[136:], "00000000000\x00")
		copy(h[148:], "        ")
		h[156] = '0'
		copy(h[257:], "ustar\x00")
		copy(h[263:], "00")
		sum := 0
		for _, v := range h {
			sum += int(v)
		}
		copy(h[148:], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(h)
		out.Write(member.data)
		out.Write(make([]byte, (512-len(member.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

func TestColecoTwoROMLaunchRequiresBothPrivateSources(t *testing.T) {
	ctx := context.Background()
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, twoROMLibraryPackageFixture(t), "MegaCart test", 30*time.Second)
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || client.coreCalls != 0 {
		t.Fatalf("missing inputs reached target: %v", err)
	}
	cartridge := bytes.Repeat([]byte{0x34}, 131072)
	cart, _, err := s.ImportCoreMedia(ctx, int64(len(cartridge)), bytes.NewReader(cartridge))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "coleco-cart", "", cart.MediaID); err != nil {
		t.Fatal(err)
	}
	comps, err := s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !comps[entry.GameID].ROMReady || comps[entry.GameID].FirmwareReady || !comps[entry.GameID].FirmwareRequired {
		t.Fatalf("separate readiness: %+v %v", comps, err)
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || client.coreCalls != 0 {
		t.Fatalf("missing BIOS reached target: %v", err)
	}
	bios := bytes.Repeat([]byte{0x12}, 8192)
	firmware, _, err := s.ImportCoreMedia(ctx, int64(len(bios)), bytes.NewReader(bios))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, firmware.MediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, cart.MediaID); err == nil || client.coreCalls != 0 {
		t.Fatalf("cartridge selected as BIOS: %v", err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "coleco-cart", cart.MediaID, firmware.MediaID); err == nil || client.coreCalls != 0 {
		t.Fatalf("BIOS selected as cartridge: %v", err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, strings.Repeat("0", 64), "coleco-cart", cart.MediaID, cart.MediaID); err == nil || client.coreCalls != 0 {
		t.Fatalf("wrong package selected for cartridge: %v", err)
	}
	snapshot, err := s.captureLaunchSnapshot(entry.GameID, "")
	if err != nil {
		t.Fatal(err)
	}
	otherCartridge := bytes.Repeat([]byte{0x78}, 131072)
	otherCart, _, err := s.ImportCoreMedia(ctx, int64(len(otherCartridge)), bytes.NewReader(otherCartridge))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "coleco-cart", cart.MediaID, otherCart.MediaID); err != nil {
		t.Fatal(err)
	}
	if err := s.revalidateLaunchSnapshot(snapshot); !errors.Is(err, ErrLaunchSnapshot) || client.coreCalls != 0 {
		t.Fatalf("changed title cartridge was not rejected before load: %v", err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "coleco-cart", otherCart.MediaID, cart.MediaID); err != nil {
		t.Fatal(err)
	}
	comps, err = s.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !comps[entry.GameID].ROMReady || !comps[entry.GameID].FirmwareReady {
		t.Fatalf("both ready: %+v %v", comps, err)
	}
	snapshot, err = s.captureLaunchSnapshot(entry.GameID, "")
	if err != nil {
		t.Fatal(err)
	}
	otherBIOS := bytes.Repeat([]byte{0x56}, 8192)
	other, _, err := s.ImportCoreMedia(ctx, int64(len(otherBIOS)), bytes.NewReader(otherBIOS))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, other.MediaID); err != nil {
		t.Fatal(err)
	}
	if err := s.revalidateLaunchSnapshot(snapshot); !errors.Is(err, ErrLaunchSnapshot) || client.coreCalls != 0 {
		t.Fatalf("changed household BIOS was not rejected before load: %v", err)
	}
	if _, err := s.SelectCoreFirmware(ctx, protocol.FirmwareRole, firmware.MediaID); err != nil {
		t.Fatal(err)
	}
	active := coreEntryActiveStatus(inspection, 7, false)
	active.CorePackage.ROMLinks = &corepackage.ROMLinksIdentity{Sources: []corepackage.ROMSourceIdentity{{ID: "coleco-bios", Role: "firmware", SourceSHA256: firmware.MediaID, SourceSize: 8192}, {ID: "coleco-cart", Role: "cartridge", SourceSHA256: cart.MediaID, SourceSize: 131072}}, MapSHA256: inspection.Descriptor.ROMMap.SHA256, ProgrammedSHA256: inspection.Descriptor.Payload.SHA256, ProgrammedSize: inspection.Descriptor.Payload.Size}
	client.coreLoad = func(_ context.Context, n int64, r io.Reader) (protocol.Status, error) {
		body, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(body)) != n || !corepackage.IsROMInputV2(body) {
			t.Fatal("wrong two-source transport")
		}
		client.statusResult = active
		return active, nil
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatalf("two-source launch: %v", err)
	}
	if client.coreCalls != 1 {
		t.Fatalf("target calls: %d", client.coreCalls)
	}
}

func libraryROMPackageFixture(t *testing.T, role string, transforms ...func([]byte) []byte) []byte {
	t.Helper()
	root := "../corepackage/testdata/core-bundle-v3/"
	manifest := "valid-basic.toml"
	if role == "cartridge" {
		manifest = "valid-cartridge.toml"
	}
	var out bytes.Buffer
	for _, member := range []struct{ name, path string }{{"manifest.toml", "manifests/" + manifest}, {"core.rbf", "payloads/fes-fixture.rbf"}, {"rom-map.json", "maps/valid-basic.json"}} {
		data, err := os.ReadFile(root + member.path)
		if err != nil {
			t.Fatal(err)
		}
		if member.name == "manifest.toml" {
			data = bytes.Replace(data, []byte(`id = "fes.pong"`), []byte(`id = "fes.zx81"`), 1)
			for _, transform := range transforms {
				data = transform(data)
			}
		}
		header := make([]byte, 512)
		copy(header, member.name)
		copy(header[100:], "0000644\x00")
		copy(header[108:], "0000000\x00")
		copy(header[116:], "0000000\x00")
		copy(header[124:], fmt.Sprintf("%011o\x00", len(data)))
		copy(header[136:], "00000000000\x00")
		copy(header[148:], "        ")
		header[156] = '0'
		copy(header[257:], "ustar\x00")
		copy(header[263:], "00")
		sum := 0
		for _, v := range header {
			sum += int(v)
		}
		copy(header[148:], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(header)
		out.Write(data)
		out.Write(make([]byte, (512-len(data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

func TestLibraryROMLaunchSendsSourceInputs(t *testing.T) {
	for _, role := range []string{"firmware", "cartridge"} {
		t.Run(role, func(t *testing.T) {
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
			archive := libraryROMPackageFixture(t, role)
			installed, _, err := s.ImportCorePackage(ctx, int64(len(archive)), bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			entry, err := s.CreateCoreEntry(ctx, "ROM title", installed.PackageID)
			if err != nil {
				t.Fatal(err)
			}
			readiness, err := s.CoreCompositions(ctx, []string{entry.GameID})
			if err != nil || !readiness[entry.GameID].ROMRequired || readiness[entry.GameID].ROMReady {
				t.Fatalf("missing readiness %+v %v", readiness, err)
			}
			if _, err = s.Launch(ctx, entry.GameID, nil); err == nil {
				t.Fatal("missing ROM launched")
			}
			if client.coreCalls != 0 {
				t.Fatal("missing ROM contacted loader")
			}
			wrong, _, err := store.ImportCoreMedia(ctx, []byte("wrong size"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, installed.Descriptor.ROM.ID, "", wrong.MediaID); err == nil {
				t.Fatal("wrong size selected")
			}
			rom := bytes.Repeat([]byte{0x5a}, 1024)
			media, _, err := store.ImportCoreMedia(ctx, rom)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "wrong-name", "", media.MediaID); err == nil {
				t.Fatal("wrong ROM name selected")
			}
			if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, installed.Descriptor.ROM.ID, "", media.MediaID); err != nil {
				t.Fatal(err)
			}
			readiness, err = s.CoreCompositions(ctx, []string{entry.GameID})
			if err != nil || !readiness[entry.GameID].ROMReady || readiness[entry.GameID].ROMMediaID != media.MediaID {
				t.Fatalf("selected readiness %+v %v", readiness, err)
			}
			if role == "cartridge" {
				if _, err = store.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", "blob", media.MediaID); err != nil {
					t.Fatal(err)
				}
			}
			client.coreLoad = func(_ context.Context, size int64, r io.Reader) (protocol.Status, error) {
				body, err := io.ReadAll(r)
				if err != nil {
					t.Fatal(err)
				}
				if int64(len(body)) != size {
					t.Fatal("transport size mismatch")
				}
				members := map[string][]byte{}
				tr := tar.NewReader(bytes.NewReader(body))
				for {
					h, err := tr.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					members[h.Name], err = io.ReadAll(tr)
					if err != nil {
						t.Fatal(err)
					}
				}
				if len(members["rom-link.json"]) == 0 {
					t.Fatalf("missing source marker: %v", members)
				}
				foundPackage, foundROM := false, false
				for name, data := range members {
					if strings.Contains(name, "programmed") {
						t.Fatal("host sent programmed image")
					}
					foundPackage = foundPackage || bytes.Equal(data, archive)
					foundROM = foundROM || bytes.Equal(data, rom)
				}
				if !foundPackage || !foundROM {
					t.Fatal("transport did not preserve exact source package and ROM")
				}
				core := installed.Descriptor.Core.ID
				return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{PackageID: entry.PackageID, Generation: 4, ABI: protocol.RuntimeContract{ID: installed.Descriptor.ABI.ID, Major: 1}, BuildID: installed.Descriptor.Build.ID, Gamepad: true, ROMLink: &corepackage.ROMLinkIdentity{ROMID: installed.Descriptor.ROM.ID, MapSHA256: installed.Descriptor.ROM.SHA256, SourceSHA256: media.MediaID, SourceSize: 1024, ProgrammedSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("linked"))), ProgrammedSize: 6}}}, nil
			}
			response, err := s.Launch(ctx, entry.GameID, nil)
			if err != nil || response.Status.GameID == nil || *response.Status.GameID != entry.GameID {
				t.Fatalf("launch %+v %v", response, err)
			}
			if client.mediaCalls != 0 || linker.calls != 0 {
				t.Fatalf("unexpected media delivery=%d host link=%d", client.mediaCalls, linker.calls)
			}
			if client.coreCalls != 1 {
				t.Fatalf("loads=%d", client.coreCalls)
			}
			// Changing the selected package never silently reuses a ROM binding.
			next, err := store.SelectCoreEntry(ctx, entry.GameID, entry.CoreID, entry.PackageID, strings.Repeat("b", 64))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.readCoreEntryROM(ctx, next, installed.Descriptor); err == nil {
				t.Fatal("stale package ROM binding accepted")
			}
		})
	}
}

func TestROMLoadIdentityRejectsDifferentSource(t *testing.T) {
	source := coreLoadSource{romID: "machine", romMediaID: strings.Repeat("a", 64), romMapSHA256: strings.Repeat("b", 64), romSourceSize: 1024}
	status := &protocol.CorePackageStatus{ROMLink: &corepackage.ROMLinkIdentity{ROMID: source.romID, SourceSHA256: source.romMediaID, MapSHA256: source.romMapSHA256, SourceSize: source.romSourceSize}}
	if !source.matchesLoadedIdentity(status) {
		t.Fatal("exact source rejected")
	}
	status.ROMLink.SourceSHA256 = strings.Repeat("c", 64)
	if source.matchesLoadedIdentity(status) {
		t.Fatal("different ROM accepted")
	}
}

type forbiddenHostROMLinker struct{ calls int }

func (f *forbiddenHostROMLinker) Link(context.Context, []byte) ([]byte, string, error) {
	f.calls++
	return nil, "", errors.New("format 3 must never invoke host initializer")
}

func TestCartridgeROMRejectsMediaStartupContractBeforeLoad(t *testing.T) {
	for _, tc := range []struct {
		name, abi, endpoint string
		required            bool
	}{
		{"application blob", "fes.application", "fes.media.blob", true},
		{"application optional blob", "fes.application", "fes.media.blob", false},
		{"stream", "fes.simple-computer", "fes.media.blob-stream", true},
		{"optional stream", "fes.simple-computer", "fes.media.blob-stream", false},
		{"firmware blob", "fes.simple-computer", "fes.firmware.blob", true},
		{"optional firmware blob", "fes.simple-game", "fes.firmware.blob", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			client := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}
			s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			s.corePackages = packages
			raw := libraryROMPackageFixture(t, "cartridge", func(manifest []byte) []byte {
				manifest = bytes.Replace(manifest, []byte(`id = "fes.simple-game"`), []byte(`id = "`+tc.abi+`"`), 1)
				return append(manifest, []byte(fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = %t\n", tc.endpoint, tc.required))...)
			})
			installed, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			var entry catalog.CoreEntry
			if tc.endpoint == "fes.firmware.blob" {
				entry, err = store.CreateFirmwareRequiredEntry(ctx, "Cartridge", installed.Descriptor.Core.ID, installed.PackageID, "", "")
			} else {
				entry, err = store.CreateCoreEntry(ctx, "Cartridge", installed.Descriptor.Core.ID, installed.PackageID)
			}
			if err != nil {
				t.Fatal(err)
			}
			media, _, err := store.ImportCoreMedia(ctx, bytes.Repeat([]byte{1}, 1024))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, installed.Descriptor.ROM.ID, "", media.MediaID); err != nil {
				t.Fatal(err)
			}
			readiness, err := s.CoreCompositions(ctx, []string{entry.GameID})
			if err != nil {
				t.Fatal(err)
			}
			if !readiness[entry.GameID].ROMRequired || readiness[entry.GameID].ROMReady {
				t.Fatalf("incompatible cartridge ready: %+v", readiness)
			}
			_, err = s.Launch(ctx, entry.GameID, nil)
			var api *protocol.APIError
			if !errors.As(err, &api) || api.Code != protocol.CodeUnsupportedOperation || api.Phase != "request" {
				t.Fatalf("launch: %v", err)
			}
			if client.coreCalls != 0 || client.stopCalls != 0 || client.inspections != 0 {
				t.Fatalf("target contacted: %+v", client)
			}
			descriptor := installed.Descriptor
			rom := *descriptor.ROM
			rom.Role = "firmware"
			descriptor.ROM = &rom
			if err := validateROMMediaContract(descriptor); err != nil {
				t.Fatalf("firmware media rejected: %v", err)
			}
		})
	}
}

func TestCartridgeROMMediaContractVersionBounds(t *testing.T) {
	for _, tc := range []struct {
		name, role, abi, endpoint string
		major, minor              int64
		reject                    bool
	}{
		{"application newer minor", "cartridge", "fes.application", "fes.media.blob", 1, 2, true},
		{"legacy computer blob", "cartridge", "fes.simple-computer", "fes.media.blob", 1, 0, false},
		{"firmware tape", "firmware", "fes.application", "fes.media.blob", 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			descriptor := corepackage.Descriptor{ROM: &corepackage.ROM{Role: tc.role}, ABI: corepackage.Contract{ID: tc.abi, Major: tc.major, Minor: tc.minor}, Interfaces: []corepackage.Interface{{ID: tc.endpoint, Major: 1, Minor: 0}}}
			if err := validateROMMediaContract(descriptor); (err != nil) != tc.reject {
				t.Fatalf("rejected=%v error=%v", tc.reject, err)
			}
		})
	}
}
