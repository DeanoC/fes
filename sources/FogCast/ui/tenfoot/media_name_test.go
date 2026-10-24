package tenfoot

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

func TestCurrentMediaLabel(t *testing.T) {
	iface := protocol.C64DiskInterface()
	pkg := &hostclient.SessionCorePackage{PackageID: "package", Generation: 9, ABI: hostclient.SessionCoreABI{ID: "fes.computer", Major: 1}, ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: iface.ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Unit: 0, Interface: iface, MinBytes: 174848, MaxBytes: 174848, ChunkBytes: 512, State: protocol.MediaUnitReady}}}
	session := hostclient.SessionResult{CorePackage: pkg, MediaName: "disk two.d64"}
	if got := currentMediaLabel(session); got != "Inserted: disk two.d64" {
		t.Fatal(got)
	}
	pkg.MediaUnits[0].State = protocol.MediaUnitEmpty
	if got := currentMediaLabel(session); got != "No media inserted" {
		t.Fatal(got)
	}
	pkg.MediaUnits[0].State = protocol.MediaUnitLoading
	if got := currentMediaLabel(session); got != "Media loading…" {
		t.Fatal(got)
	}
	session.MediaName = ""
	pkg.MediaUnits[0].State = protocol.MediaUnitReady
	if got := currentMediaLabel(session); got != "Inserted media name unavailable" {
		t.Fatal(got)
	}
}
