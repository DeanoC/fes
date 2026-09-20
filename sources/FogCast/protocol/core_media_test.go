package protocol

import (
	"encoding/json"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func TestDeclaredCoreMediaCapabilitiesFollowContractNotCoreID(t *testing.T) {
	for _, id := range []string{"fes.coleco", "fes.zx81", "fes.sms", "example.new-core"} {
		for _, required := range []bool{false, true} {
			descriptor := corepackage.Descriptor{
				Core:       corepackage.Core{ID: id},
				ABI:        corepackage.Contract{ID: "fes.simple-computer", Major: 1},
				Interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1, Required: required}},
			}
			got := DeclaredCoreMediaCapabilities(descriptor)
			want := CoreMediaCapability{Role: "blob", Format: "raw", MinBytes: 1, MaxBytes: 16384,
				Interface: RuntimeContract{ID: "fes.media.blob", Major: 1}, Transport: "fes-simple-computer-mailbox-v1"}
			if len(got) != 1 || got[0] != want {
				t.Fatalf("%s required=%v: %+v", id, required, got)
			}
			// A caller must not be able to mutate a shared capability template.
			got[0].MaxBytes = 999999
			if DeclaredCoreMediaCapabilities(descriptor)[0] != want {
				t.Fatal("capability state leaked across calls")
			}
		}
	}
}

func TestDeclaredCoreMediaCapabilitiesUnknownContractsStayUnsupported(t *testing.T) {
	cases := []struct {
		name  string
		abi   corepackage.Contract
		media corepackage.Interface
	}{
		{"missing", corepackage.Contract{}, corepackage.Interface{}},
		{"wrong-abi", corepackage.Contract{ID: "fes.simple-game", Major: 1}, corepackage.Interface{ID: "fes.media.blob", Major: 1}},
		{"future-abi", corepackage.Contract{ID: "fes.simple-computer", Major: 2}, corepackage.Interface{ID: "fes.media.blob", Major: 1}},
		{"future-abi-minor", corepackage.Contract{ID: "fes.simple-computer", Major: 1, Minor: 1}, corepackage.Interface{ID: "fes.media.blob", Major: 1}},
		{"future-media", corepackage.Contract{ID: "fes.simple-computer", Major: 1}, corepackage.Interface{ID: "fes.media.blob", Major: 2}},
		{"future-media-minor", corepackage.Contract{ID: "fes.simple-computer", Major: 1}, corepackage.Interface{ID: "fes.media.blob", Major: 1, Minor: 1}},
		{"other-role", corepackage.Contract{ID: "fes.simple-computer", Major: 1}, corepackage.Interface{ID: "example.disk", Major: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeclaredCoreMediaCapabilities(corepackage.Descriptor{ABI: tc.abi, Interfaces: []corepackage.Interface{tc.media}})
			raw, err := json.Marshal(got)
			if err != nil || string(raw) != "[]" {
				t.Fatalf("unknown contract projection = %s, err=%v", raw, err)
			}
		})
	}
}
