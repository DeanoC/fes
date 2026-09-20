package fogcast

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestColecoApplicationLibraryMediaBoundaries(t *testing.T) {
	for _, coreID := range []string{"fes.coleco", "other.core"} {
		for _, stream := range []bool{false, true} {
			for _, size := range []int{16384, 24576, 32768, 32769} {
				t.Run(fmt.Sprintf("%s/stream=%v/bytes=%d", coreID, stream, size), func(t *testing.T) {
					interfaces := ""
					ids := []string{"fes.gamepad.ports", "fes.keypad.ports"}
					if stream {
						ids = append(ids, "fes.media.blob-stream")
					}
					for _, id := range ids {
						interfaces += fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = true\n", id)
					}
					raw := colecoLibraryPackageContractsFixture(t, "fes.application", interfaces)
					raw = capabilitiesFixtureReplace(t, raw, `id = "fes.coleco"`, `id = "`+coreID+`"`)
					s, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Controller stream")
					ctx := context.Background()
					payload := bytes.Repeat([]byte{0x5a}, size)
					// Distinguish upper cartridge addresses from the old 16 KiB range.
					for i := 16384; i < len(payload); i++ {
						payload[i] = byte(i >> 8)
					}
					media := importServiceCoreMedia(t, s, payload)
					selected, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, entry.MediaID, "blob", media.MediaID)
					max := 16384
					if stream {
						max = 32768
					}
					capabilities, capErr := s.CoreMediaCapabilities(ctx, inspection.PackageID)
					if capErr != nil || len(capabilities.Media) != 1 || capabilities.Media[0].MaxBytes != int64(max) {
						t.Fatalf("declared media capabilities=%+v error=%v", capabilities, capErr)
					}
					if size > max {
						if err == nil || client.coreCalls != 0 || client.mediaCalls != 0 {
							t.Fatalf("oversize selected or programmed: err=%v core=%d media=%d", err, client.coreCalls, client.mediaCalls)
						}
						// Independently check launch admission against a stale/corrupt catalog
						// reference, rather than relying only on the selection endpoint.
						forged := entry
						forged.MediaRole, forged.MediaID = "blob", media.MediaID
						s.catalog = &capabilitiesMediaCatalog{Store: s.catalog.(*catalog.Store), forged: &forged}
						if _, err = s.Launch(ctx, entry.GameID, nil); err == nil || client.coreCalls != 0 || client.mediaCalls != 0 {
							t.Fatalf("oversize launch programmed: err=%v core=%d media=%d", err, client.coreCalls, client.mediaCalls)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					active := coreEntryActiveStatus(inspection, 9, true)
					active.CorePackage.Gamepad = true
					for _, id := range ids {
						active.CorePackage.ActiveInterfaces = append(active.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: id, Major: 1})
					}
					if stream {
						active.CorePackage.MediaStream = &protocol.MediaStreamCapability{Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512}
					}
					client.mediaStatus = active
					client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
						client.statusResult = active
						return active, nil
					}
					result, err := s.Launch(ctx, selected.GameID, nil)
					if err != nil || client.coreCalls != 1 || client.mediaCalls != 1 || client.mediaBinding.Stream != stream || !bytes.Equal(client.mediaBody, payload) {
						t.Fatalf("launch core=%d media=%d binding=%+v err=%v", client.coreCalls, client.mediaCalls, client.mediaBinding, err)
					}
					if result.Status.CorePackage == nil || !result.Status.CorePackage.Gamepad {
						t.Fatal("media launch lost controller capability")
					}
				})
			}
		}
	}
}
