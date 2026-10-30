package targetclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
	"github.com/DeanoC/misteross/expansion"
)

func TestSTROMPartsClientUsesOrdinaryLibraryLoadAndRejectsMixedReceipts(t *testing.T) {
	for _, name := range []string{"paired", "persistent disk", "persistent disk without parts", "bad disk revision", "read-only disk", "missing ROM", "wrong ABI", "foreign layout", "wrong identity", "persistent without disk", "persistent without disk or parts", "ROM-less parts endpoint"} {
		t.Run(name, func(t *testing.T) {
			status := libraryPartsStatus()
			value := status.CorePackage
			value.ABI = protocol.RuntimeContract{ID: "fes.computer", Major: 1}
			c := value.PartsComposition
			c.Layout = expansion.AtariStVideoLayout
			c.ID, _ = expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
			value.ROMLink = &corepackage.ROMLinkIdentity{ROMID: "atari-st-firmware", MapSHA256: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64), SourceSize: 192 << 10, ProgrammedSHA256: strings.Repeat("f", 64), ProgrammedSize: 50000}
			if name == "persistent disk" || name == "persistent disk without parts" || name == "bad disk revision" || name == "read-only disk" {
				value.PersistenceMode = "persistent"
				value.ActiveInterfaces = []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}, {ID: "fes.video.fixed-720p60", Major: 1}}
				value.MediaUnits = []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: uint32(protocol.AtariStFloppyBytes), MaxBytes: uint32(protocol.AtariStFloppyBytes), ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("9", 64), Revision: "absent"}}}
				if name == "bad disk revision" {
					value.MediaUnits[0].Persistence.Revision = "invalid"
				}
				if name == "read-only disk" {
					value.ActiveInterfaces = append(value.ActiveInterfaces[:1:1], value.ActiveInterfaces[2])
				}
			}
			switch name {
			case "missing ROM":
				value.ROMLink = nil
			case "wrong ABI":
				value.ABI.ID = "fes.application"
			case "foreign layout":
				c.Layout = expansion.ColecoVideoLayout
				c.ID, _ = expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
			case "wrong identity":
				c.PayloadSHA256 = strings.Repeat("9", 64)
			case "persistent without disk":
				value.PersistenceMode = "persistent"
			case "persistent without disk or parts":
				value.PersistenceMode = "persistent"
				value.PartsComposition = nil
			case "persistent disk without parts":
				value.PartsComposition = nil
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				want := "/v1/library/core/load"
				if name == "ROM-less parts endpoint" {
					want = "/v1/library/core/parts"
				}
				if r.URL.Path != want || r.Header.Get("X-FogCast-Package-ID") != value.PackageID {
					t.Error("wrong ordinary library request", r.URL)
				}
				_ = json.NewEncoder(w).Encode(status)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := targetclient.NewClient(base, "secret", server.Client())
			var err error
			if name == "ROM-less parts endpoint" {
				_, err = client.LoadLibraryPartsCore(context.Background(), 5, strings.NewReader("input"), value.PackageID)
			} else {
				_, err = client.LoadLibraryCore(context.Background(), 5, strings.NewReader("input"), value.PackageID)
			}
			if (err == nil) != (name == "paired" || name == "persistent disk" || name == "persistent disk without parts") || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}
