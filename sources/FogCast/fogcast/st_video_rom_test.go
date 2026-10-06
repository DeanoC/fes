package fogcast

import (
	"bytes"
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSTVideoNormalPlayCarriesFirmwareVideoAndCartridge(t *testing.T) {
	for _, tc := range []struct {
		name          string
		initialDisk   bool
		uploadTimeout time.Duration
		parentTimeout time.Duration
	}{
		{"diskless-configured-budget", false, time.Minute, 0},
		{"initial-disk-media-budget", true, time.Minute, 0},
		{"initial-disk-longer-configured-budget", true, 400 * time.Second, 0},
		{"initial-disk-earlier-caller-deadline", true, time.Minute, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testSTVideoNormalPlay(t, tc.initialDisk, tc.uploadTimeout, tc.parentTimeout)
		})
	}
}

func testSTVideoNormalPlay(t *testing.T, initialDisk bool, uploadTimeout, parentTimeout time.Duration) {
	ctx := context.Background()
	pkg, firmware := apple2LibraryPackageFixture(t, true, func(text string) string {
		// Required interfaces describe implemented hardware, not inserted media.
		// Both cases use the writable ST package; drive A may start empty.
		text += "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n"
		return strings.ReplaceAll(text, "apple2", "atari-st") + "\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n"
	})
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, pkg, "ST video", uploadTimeout)
	s.targets[0].Enabled = true
	s.targets[0].Address = "http://example.invalid:8182"
	s.targets[0].Agent = "test-token"
	firmwareMedia, _, err := s.ImportCoreMedia(ctx, int64(len(firmware)), bytes.NewReader(firmware))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "atari-st-firmware", "", firmwareMedia.MediaID); err != nil {
		t.Fatal(err)
	}
	var disk []byte
	var diskID string
	if initialDisk {
		disk = bytes.Repeat([]byte{0xA5}, int(protocol.AtariStFloppyBytes))
		image, _, err := s.ImportCoreMedia(ctx, int64(len(disk)), bytes.NewReader(disk))
		if err != nil {
			t.Fatal(err)
		}
		diskID = image.MediaID
		if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.DiskRole, diskID); err != nil {
			t.Fatal(err)
		}
	}
	base := misterossROMFixture(t, "blank.rbf")
	var selected []expansion.Asset
	for _, role := range []string{expansion.PartRoleVideo, expansion.PartRoleExpansion} {
		m := expansion.Manifest{CartSHA256: inspection.Descriptor.Payload.SHA256, CartSize: int64(len(base)), Device: expansion.Device, Format: 1, Map: expansion.AtariStVideoMap, RecipeSHA256: strings.Repeat("b", 64), Revision: strings.Repeat("c", 40), ShellBuildID: inspection.Descriptor.Build.ID, ShellPackageID: entry.PackageID, ShellSHA256: inspection.Descriptor.Payload.SHA256, Slot: expansion.VideoSlot, SlotMajor: 1}
		if role == expansion.PartRoleExpansion {
			m.Slot, m.Map, m.SlotIndex = expansion.AtariStSlot, expansion.AtariStMap, 1
		}
		asset, err := expansion.NewAsset(m, base)
		if err != nil {
			t.Fatal(err)
		}
		selected = append(selected, asset)
		if role == expansion.PartRoleVideo {
			importVideoFixture(t, s, asset, "scanlines")
		} else {
			var encoded bytes.Buffer
			if err := asset.Write(&encoded); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ImportCoreExpansion(ctx, int64(encoded.Len()), &encoded); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SelectCoreEntrySlotExpansion(ctx, entry.GameID, entry.PackageID, 1, "", asset.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	profile := "scanlines"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	s.targetClients[s.selectedTarget] = client
	choices, err := s.CoreEntryVideo(ctx, entry.GameID)
	if err != nil || choices.Builtin || choices.PartID != selected[0].ID {
		t.Fatalf("video selection %+v %v", choices, err)
	}
	root := t.TempDir()
	var received *expansion.PartsComposition
	var launchStarted time.Time
	var callerDeadline time.Time
	client.coreLoad = func(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("core activation has no deadline")
		}
		if parentTimeout != 0 {
			if !deadline.Equal(callerDeadline) {
				t.Fatalf("activation deadline %v exceeded caller deadline %v", deadline, callerDeadline)
			}
		} else {
			want := uploadTimeout
			if initialDisk {
				want = max(want, 300*time.Second)
			}
			if deadline.Before(launchStarted.Add(want)) || deadline.After(time.Now().Add(want)) {
				t.Fatalf("activation deadline %v does not preserve %v budget", deadline, want)
			}
		}
		data, err := io.ReadAll(body)
		if err != nil || int64(len(data)) != size || !corepackage.IsROMInput(data) || !bytes.Contains(data, []byte("part-video.tar")) || !bytes.Contains(data, []byte("part-expansion.tar")) {
			t.Fatal("normal Play discarded ROM or parts", err)
		}
		staged, err := corepackage.StageROMInput(ctx, root, size, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		defer staged.Cleanup()
		if initialDisk {
			m := staged.InitialMedia
			if m == nil || m.GameID != entry.GameID || m.BaseMediaID != diskID || m.Unit != 0 {
				t.Fatal("initial disk binding missing", m)
			}
			actual, err := os.ReadFile(m.Path)
			if err != nil || !bytes.Equal(actual, disk) {
				t.Fatal("initial disk bytes changed", err)
			}
		} else if staged.InitialMedia != nil {
			t.Fatal("diskless launch acquired initial media")
		}
		received = staged.PartsComposition
		status := coreEntryActiveStatus(inspection, 3, true)
		status.CorePackage.ROMLink = staged.ROMLink
		status.CorePackage.PartsComposition = staged.PartsComposition
		status.CorePackage.PersistenceMode = "volatile"
		status.CorePackage.ActiveInterfaces = append(status.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, protocol.RuntimeInterface{ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1})
		status.CorePackage.MediaUnits = []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.AtariStFloppyInterface(), MinBytes: uint32(protocol.AtariStFloppyBytes), MaxBytes: uint32(protocol.AtariStFloppyBytes), ChunkBytes: 512, State: protocol.MediaUnitEmpty}}
		if initialDisk {
			status.CorePackage.PersistenceMode = "persistent"
			status.CorePackage.MediaUnits = []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.AtariStFloppyInterface(), MinBytes: uint32(protocol.AtariStFloppyBytes), MaxBytes: uint32(protocol.AtariStFloppyBytes), ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: entry.GameID, BaseMediaID: diskID, Revision: "absent"}}}
		}
		client.statusResult = status
		return status, nil
	}
	if parentTimeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, parentTimeout)
		defer cancel()
		callerDeadline, _ = ctx.Deadline()
	}
	launchStarted = time.Now()
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || client.coreCalls != 1 {
		t.Fatalf("Play %+v %v calls%d", response, err, client.coreCalls)
	}
	if !initialDisk {
		unit, ok := protocol.MediaUnit(response.Status.CorePackage, 0)
		if !ok || unit.State != protocol.MediaUnitEmpty || unit.Persistence != nil || response.Status.CorePackage.PersistenceMode != "volatile" {
			t.Fatal("diskless writable ST must launch with empty volatile drive A", response.Status.CorePackage)
		}
	}
	want, err := corepackage.ComposePartsArchive(ctx, pkg, selected)
	if err != nil {
		t.Fatal(err)
	}
	if received == nil || !reflect.DeepEqual(received, &want.Composition) || !reflect.DeepEqual(response.Status.CorePackage.PartsComposition, received) || response.Status.CorePackage.ROMLink == nil || response.Status.CorePackage.ROMLink.SourceSHA256 != firmwareMedia.MediaID || response.Status.CorePackage.SlotComposition != nil {
		t.Fatal("Play confirmation lost paired identities", fmt.Sprintf("%+v", response.Status.CorePackage))
	}
	if response.Status.GameID == nil || *response.Status.GameID != entry.GameID {
		t.Fatal("ST parts lost normal library association")
	}
}

func TestInitialSTDiskLaunchRequiresReadySelectedBinding(t *testing.T) {
	selected := coreLoadSource{initialMedia: &corepackage.InitialMedia{
		GameID: "st-boot", BaseMediaID: strings.Repeat("a", 64), Unit: 0,
	}}
	good := protocol.CorePackageStatus{ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}}, PersistenceMode: "persistent", MediaUnits: []protocol.MediaUnitStatus{{
		Unit: 0, Interface: protocol.AtariStFloppyInterface(), MinBytes: uint32(protocol.AtariStFloppyBytes), MaxBytes: uint32(protocol.AtariStFloppyBytes), ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{
			Mode: "persistent", GameID: "st-boot", BaseMediaID: strings.Repeat("a", 64), Revision: "absent",
		},
	}}}
	if !selected.matchesLoadedIdentity(&good) {
		t.Fatal("ready selected binding rejected")
	}
	for _, mutation := range []func(*protocol.CorePackageStatus){
		func(p *protocol.CorePackageStatus) { p.MediaUnits[0].State = protocol.MediaUnitEmpty },
		func(p *protocol.CorePackageStatus) { p.MediaUnits[0].Persistence = nil },
		func(p *protocol.CorePackageStatus) { p.MediaUnits[0].Persistence.GameID = "other-game" },
		func(p *protocol.CorePackageStatus) { p.MediaUnits[0].Persistence.BaseMediaID = strings.Repeat("b", 64) },
		func(p *protocol.CorePackageStatus) { p.PersistenceMode = "volatile" },
	} {
		candidate := good
		candidate.MediaUnits = append([]protocol.MediaUnitStatus(nil), good.MediaUnits...)
		binding := *good.MediaUnits[0].Persistence
		candidate.MediaUnits[0].Persistence = &binding
		mutation(&candidate)
		if selected.matchesLoadedIdentity(&candidate) {
			t.Fatal("different or incomplete initial disk accepted")
		}
	}
}
