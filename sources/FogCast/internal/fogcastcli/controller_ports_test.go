package fogcastcli

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func TestControllerPortsMatchInspectedPackage(t *testing.T) {
	inspection := corepackage.Inspection{PackageID: strings.Repeat("a", 64), Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.application", Major: 1}, Interfaces: []corepackage.Interface{{ID: "fes.gamepad.ports", Major: 1, Required: true}}}}
	inspection.Descriptor.Build.ID = strings.Repeat("b", 32)
	status := &protocol.CorePackageStatus{PackageID: inspection.PackageID, Generation: 1, ABI: protocol.RuntimeContract{ID: "fes.application", Major: 1}, BuildID: inspection.Descriptor.Build.ID, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad.ports", Major: 1}}, Gamepad: true}
	if !corePackageMatchesInspection(status, inspection) {
		t.Fatal("negotiated ports rejected")
	}
	status.Gamepad = false
	if corePackageMatchesInspection(status, inspection) {
		t.Fatal("missing gamepad projection accepted")
	}
}
