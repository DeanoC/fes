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

// The format-4 Coleco video shell links the BIOS and cartridge with its parts
// through the ordinary library load, so its reply carries ROMLinks with parts.
func TestColecoROMLinksPartsClientUsesOrdinaryLibraryLoad(t *testing.T) {
	accepted := map[string]bool{"native": true, "raster": true}
	for _, name := range []string{"native", "raster", "single ROM link", "both ROM receipts", "ST layout", "persistent", "computer ABI", "parts endpoint", "with CPU composition"} {
		t.Run(name, func(t *testing.T) {
			status := libraryPartsStatus()
			value := status.CorePackage
			c := value.PartsComposition
			c.Layout = expansion.ColecoNativeVideoLayout
			links := &corepackage.ROMLinksIdentity{MapSHA256: strings.Repeat("a", 64), ProgrammedSHA256: strings.Repeat("f", 64), ProgrammedSize: 50000,
				Sources: []corepackage.ROMSourceIdentity{
					{ID: "coleco-bios", Role: "firmware", SourceSHA256: strings.Repeat("b", 64), SourceSize: 8192},
					{ID: "coleco-cart", Role: "cartridge", SourceSHA256: strings.Repeat("e", 64), SourceSize: 16384},
				}}
			value.ROMLinks = links
			single := &corepackage.ROMLinkIdentity{ROMID: "cartridge-rom", MapSHA256: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64), SourceSize: 16384, ProgrammedSHA256: strings.Repeat("f", 64), ProgrammedSize: 50000}
			switch name {
			case "raster":
				c.Layout = expansion.ColecoVideoLayout
			case "single ROM link":
				value.ROMLinks, value.ROMLink = nil, single
			case "both ROM receipts":
				value.ROMLink = single
			case "ST layout":
				c.Layout = expansion.AtariStVideoLayout
			case "persistent":
				value.PersistenceMode = "persistent"
			case "computer ABI":
				value.ABI.ID = "fes.computer"
			case "with CPU composition":
				value.Composition = &expansion.Composition{}
			}
			c.ID, _ = expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(status)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := targetclient.NewClient(base, "secret", server.Client())
			var err error
			var loaded protocol.Status
			if name == "parts endpoint" {
				loaded, err = client.LoadLibraryPartsCore(context.Background(), 5, strings.NewReader("input"), value.PackageID)
			} else {
				loaded, err = client.LoadLibraryCore(context.Background(), 5, strings.NewReader("input"), value.PackageID)
			}
			if (err == nil) != accepted[name] {
				t.Fatalf("error=%v", err)
			}
			if err == nil && (loaded.CorePackage.ROMLinks == nil || loaded.CorePackage.PartsComposition == nil || loaded.CorePackage.PartsComposition.Layout != c.Layout) {
				t.Fatal("client dropped the paired receipts")
			}
		})
	}
}
