package targetclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func TestROMLaunchClientUploadLimits(t *testing.T) {
	for _, kind := range []string{"development", "library", "inspect", "data", "settings"} {
		limit := int64(corepackage.MaxArchiveSize)
		if kind == "development" || kind == "library" {
			limit = corepackage.MaxROMInputSize
		}
		for _, size := range []int64{limit, limit + 1} {
			t.Run(kind+"/"+fmt.Sprint(size), func(t *testing.T) {
				transport := &countingResponseTransport{}
				client := contentClientWithTransport(transport)
				id := strings.Repeat("a", 64)
				body := strings.NewReader("sentinel")
				switch kind {
				case "development":
					_, _ = client.LoadCore(context.Background(), size, body)
				case "library":
					_, _ = client.LoadLibraryCore(context.Background(), size, body, id)
				case "inspect":
					_, _ = client.InspectCore(context.Background(), size, body)
				case "data":
					_, _ = client.InspectCoreData(context.Background(), size, body, id)
				case "settings":
					_, _ = client.UpdateCoreSettings(context.Background(), size, body, protocol.CoreSettingsUpdate{ExpectedPackageID: id, ExpectedRevision: "absent"})
				}
				want := 0
				if size == limit {
					want = 1
				}
				if transport.calls != want {
					t.Fatalf("request calls=%d want=%d", transport.calls, want)
				}
			})
		}
	}
}
func TestROMWithExpansionUsesOrdinaryLibraryLoad(t *testing.T) {
	good := corepackage.ROMLinkIdentity{ROMID: "machine", MapSHA256: strings.Repeat("b", 64), SourceSHA256: strings.Repeat("c", 64), SourceSize: 8192, ProgrammedSHA256: strings.Repeat("d", 64), ProgrammedSize: 4000}
	for _, kind := range []string{"valid", "missing ROM identity", "bad id", "bad map", "bad source", "bad source size", "bad programmed hash", "bad programmed size"} {
		t.Run(kind, func(t *testing.T) {
			link := good
			switch kind {
			case "bad id":
				link.ROMID = "Invalid"
			case "bad map":
				link.MapSHA256 = ""
			case "bad source":
				link.SourceSHA256 = strings.Repeat("A", 64)
			case "bad source size":
				link.SourceSize++
			case "bad programmed hash":
				link.ProgrammedSHA256 = ""
			case "bad programmed size":
				link.ProgrammedSize = corepackage.MaxPayloadSize + 1
			}
			status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 1, BuildID: strings.Repeat("b", 32), ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, PersistenceMode: "volatile", ROMLink: &link, Composition: &expansion.Composition{ID: strings.Repeat("f", 64)}}}
			if kind == "missing ROM identity" {
				status.CorePackage.ROMLink = nil
			}
			data, err := json.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			_, err = contentClientWithTransport(&staticResponseTransport{body: string(data)}).LoadLibraryCore(context.Background(), 1, strings.NewReader("x"), status.CorePackage.PackageID)
			if (err == nil) != (kind == "valid") {
				t.Fatalf("kind=%s err=%v", kind, err)
			}
		})
	}
}
