package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func TestDeclaredFirmwareCapabilitiesFollowContractNotCoreID(t *testing.T) {
	for _, id := range []string{"fes.coleco", "example.new-core"} {
		descriptor := corepackage.Descriptor{
			Core: corepackage.Core{ID: id},
			ABI:  corepackage.Contract{ID: "fes.application", Major: 1},
			Interfaces: []corepackage.Interface{
				{ID: "fes.media.blob", Major: 1, Required: true},
				{ID: FirmwareInterfaceID, Major: 1},
			},
		}
		got := DeclaredFirmwareCapabilities(descriptor)
		want := CoreMediaCapability{Role: FirmwareRole, Format: "raw", MinBytes: FirmwareBytes, MaxBytes: FirmwareBytes,
			Interface: RuntimeContract{ID: FirmwareInterfaceID, Major: 1}, Transport: "fes-application-mailbox-firmware-v1"}
		if len(got) != 1 || got[0] != want || !DeclaresFirmwareSlot(descriptor) {
			t.Fatalf("%s: %+v", id, got)
		}
		got[0].MaxBytes = 1
		if DeclaredFirmwareCapabilities(descriptor)[0] != want {
			t.Fatal("capability state leaked across calls")
		}
	}
}

func TestDeclaredFirmwareCapabilitiesUnknownContractsStayUnsupported(t *testing.T) {
	cases := []struct {
		name string
		abi  corepackage.Contract
		slot corepackage.Interface
	}{
		{"missing", corepackage.Contract{}, corepackage.Interface{}},
		{"simple-computer", corepackage.Contract{ID: "fes.simple-computer", Major: 1}, corepackage.Interface{ID: FirmwareInterfaceID, Major: 1}},
		{"future-abi", corepackage.Contract{ID: "fes.application", Major: 2}, corepackage.Interface{ID: FirmwareInterfaceID, Major: 1}},
		{"future-abi-minor", corepackage.Contract{ID: "fes.application", Major: 1, Minor: 1}, corepackage.Interface{ID: FirmwareInterfaceID, Major: 1}},
		{"future-firmware", corepackage.Contract{ID: "fes.application", Major: 1}, corepackage.Interface{ID: FirmwareInterfaceID, Major: 2}},
		{"future-firmware-minor", corepackage.Contract{ID: "fes.application", Major: 1}, corepackage.Interface{ID: FirmwareInterfaceID, Major: 1, Minor: 1}},
		{"blob-is-not-firmware", corepackage.Contract{ID: "fes.application", Major: 1}, corepackage.Interface{ID: "fes.media.blob", Major: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := corepackage.Descriptor{ABI: tc.abi, Interfaces: []corepackage.Interface{tc.slot}}
			got := DeclaredFirmwareCapabilities(d)
			raw, err := json.Marshal(got)
			if err != nil || string(raw) != "[]" || DeclaresFirmwareSlot(d) {
				t.Fatalf("unknown contract projection = %s, err=%v", raw, err)
			}
		})
	}
}

func TestFirmwareBindingRequiresActiveInterface(t *testing.T) {
	binding := DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 3, Role: FirmwareRole}
	status := Status{State: StateActive, Development: true, CorePackage: &CorePackageStatus{
		PackageID: binding.PackageID, Generation: binding.Generation,
		ABI: RuntimeContract{ID: "fes.application", Major: 1},
		ActiveInterfaces: []RuntimeInterface{
			{ID: "fes.media.blob", Major: 1},
			{ID: FirmwareInterfaceID, Major: 1},
		},
	}}
	if !binding.Matches(status) || !FirmwareCapable(status.CorePackage) || !binding.AcceptsSize(status, FirmwareBytes) || binding.AcceptsSize(status, FirmwareBytes-1) {
		t.Fatal("firmware-capable package rejected")
	}
	media := binding
	media.Role = ""
	if !media.Matches(status) {
		t.Fatal("cartridge media should still match blob-capable package")
	}
	status.CorePackage.ActiveInterfaces = []RuntimeInterface{{ID: "fes.media.blob", Major: 1}}
	if binding.Matches(status) || FirmwareCapable(status.CorePackage) {
		t.Fatal("blob-only package accepted firmware bind")
	}
}

func TestFirmwareReadyDistinguishesFroggerAndGraphicsI(t *testing.T) {
	if !FirmwareReady(false, false, false) {
		t.Fatal("Graphics I must stay ready without household BIOS")
	}
	if FirmwareReady(true, false, true) {
		t.Fatal("Frogger with a firmware-capable package is not ready without BIOS")
	}
	if FirmwareReady(true, true, false) {
		t.Fatal("imported BIOS cannot ready Frogger on a blob-only package")
	}
	if !FirmwareReady(true, true, true) {
		t.Fatal("Frogger should be ready when household BIOS and the slot exist")
	}
}

func TestGraphicsIOmitsFirmwareSlot(t *testing.T) {
	d := corepackage.Descriptor{
		Core: corepackage.Core{ID: "fes.coleco"},
		ABI:  corepackage.Contract{ID: "fes.application", Major: 1},
		Interfaces: []corepackage.Interface{
			{ID: "fes.media.blob", Major: 1, Required: true},
			{ID: "fes.media.blob-stream", Major: 1, Required: true},
		},
	}
	if DeclaresFirmwareSlot(d) || len(DeclaredCoreMediaCapabilities(d)) == 0 {
		t.Fatal("BIOS-free Coleco must keep cartridge media and omit firmware")
	}
}
