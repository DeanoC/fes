package fogcast

import (
	"bytes"
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSTVideoNormalPlayCarriesFirmwareVideoAndCartridge(t *testing.T) {
	ctx := context.Background()
	pkg, firmware := apple2LibraryPackageFixture(t, true, func(text string) string {
		return strings.ReplaceAll(text, "apple2", "atari-st") + "\n[[interfaces]]\nid = \"fes.fabric.video.raster-rgb888\"\nmajor = 1\nminor = 0\nrequired = false\n"
	})
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, pkg, "ST video", time.Minute)
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
	client.coreLoad = func(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
		data, err := io.ReadAll(body)
		if err != nil || int64(len(data)) != size || !corepackage.IsROMInput(data) || !bytes.Contains(data, []byte("part-video.tar")) || !bytes.Contains(data, []byte("part-expansion.tar")) {
			t.Fatal("normal Play discarded ROM or parts", err)
		}
		staged, err := corepackage.StageROMInput(ctx, root, size, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		defer staged.Cleanup()
		received = staged.PartsComposition
		status := coreEntryActiveStatus(inspection, 3, true)
		status.CorePackage.ROMLink = staged.ROMLink
		status.CorePackage.PartsComposition = staged.PartsComposition
		client.statusResult = status
		return status, nil
	}
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || client.coreCalls != 1 {
		t.Fatalf("Play %+v %v calls%d", response, err, client.coreCalls)
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
