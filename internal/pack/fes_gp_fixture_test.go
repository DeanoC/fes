package pack

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type fesGPExchangeFixture struct {
	Name string   `json:"name"`
	GPO  []uint32 `json:"gpo"`
	GPI  uint32   `json:"gpi"`
	Data uint16   `json:"data"`
}

type fesGPFixture struct {
	InitialRequestToggle bool                   `json:"initial_request_toggle"`
	BuildID              string                 `json:"build_id"`
	Exchanges            []fesGPExchangeFixture `json:"exchanges"`
}

func TestFesGpWireVersionConstantsRejectMetadataDrift(t *testing.T) {
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages", "abi", "fes_simple_game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFesGpWireVersionConstants(abi); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ABIFile){
		"tag":   func(abi *ABIFile) { abi.Tag++ },
		"major": func(abi *ABIFile) { abi.Major++ },
		"minor": func(abi *ABIFile) { abi.Minor++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := *abi
			mutate(&changed)
			if err := validateFesGpWireVersionConstants(&changed); err == nil {
				t.Fatal("wire constants accepted divergent ABI metadata")
			}
		})
	}
}

func validateFesGpWireVersionConstants(abi *ABIFile) error {
	for _, field := range []struct {
		constant string
		metadata uint32
	}{
		{"FesGpAbiTag", uint32(abi.Tag)},
		{"FesGpAbiMajor", uint32(abi.Major)},
		{"FesGpAbiMinor", uint32(abi.Minor)},
	} {
		value, ok := abi.Constant(field.constant)
		if !ok {
			return fmt.Errorf("missing wire constant %q", field.constant)
		}
		if value != field.metadata {
			return fmt.Errorf("wire constant %s = %d, metadata = %d", field.constant, value, field.metadata)
		}
	}
	return nil
}

func TestFesGpGoldenExchangeFixture(t *testing.T) {
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages", "abi", "fes_simple_game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFesGpWireVersionConstants(abi); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repoRoot(t), "testdata", "fes-gp-v1", "exchanges.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtureSet fesGPFixture
	if err := json.Unmarshal(data, &fixtureSet); err != nil {
		t.Fatal(err)
	}
	fixtures := fixtureSet.Exchanges
	if len(fixtures) != 22 {
		t.Fatalf("exchange count = %d, want 22", len(fixtures))
	}
	constants := func(name string) uint32 {
		t.Helper()
		value, ok := abi.Constant(name)
		if !ok {
			t.Fatalf("missing ABI constant %q", name)
		}
		return value
	}
	requestMask := constants("FesGpRequestMask")
	opcodeMask := constants("FesGpOpcodeMask")
	indexMask := constants("FesGpIndexMask")
	argumentMask := constants("FesGpArgumentMask")
	ackMask := constants("FesGpAckMask")
	errorMask := constants("FesGpErrorMask")
	responseMask := constants("FesGpResponseMask")
	signature := constants("FesGpSignature")
	assertExchange := func(t *testing.T, fixture fesGPExchangeFixture, opcode, index, argument uint32, failed bool, wantData uint16, wantInitialToggle bool) bool {
		t.Helper()
		if len(fixture.GPO) != 2 {
			t.Fatalf("%s: gpo writes = %#v, want field-only and toggle writes", fixture.Name, fixture.GPO)
		}
		for write, value := range fixture.GPO {
			if value&opcodeMask != opcode<<24 || value&indexMask != index<<16 || value&argumentMask != argument {
				t.Fatalf("%s: gpo[%d] = 0x%08x, want opcode=%d index=%d argument=%d", fixture.Name, write, value, opcode, index, argument)
			}
		}
		if fixture.GPO[0]^fixture.GPO[1] != requestMask {
			t.Fatalf("%s: gpo writes do not differ only by request toggle: %#x", fixture.Name, fixture.GPO[0]^fixture.GPO[1])
		}
		if got := fixture.GPO[0]&requestMask != 0; got != wantInitialToggle {
			t.Fatalf("%s: initial request toggle = %t, want %t", fixture.Name, got, wantInitialToggle)
		}
		finalToggle := fixture.GPO[1]&requestMask != 0
		if finalToggle == wantInitialToggle {
			t.Fatalf("%s: final request toggle did not change", fixture.Name)
		}
		if fixture.GPI&0xff000000 != signature {
			t.Fatalf("%s: GPI signature = 0x%08x, want 0x%08x", fixture.Name, fixture.GPI&0xff000000, signature)
		}
		if got := fixture.GPI&ackMask != 0; got != finalToggle {
			t.Fatalf("%s: ack = %t, want final request toggle %t", fixture.Name, got, finalToggle)
		}
		if got := fixture.GPI&errorMask != 0; got != failed {
			t.Fatalf("%s: error = %t, want %t", fixture.Name, got, failed)
		}
		if fixture.GPI&0x003f0000 != 0 {
			t.Fatalf("%s: reserved GPI bits = 0x%08x", fixture.Name, fixture.GPI&0x003f0000)
		}
		if fixture.GPI&responseMask != uint32(wantData) || fixture.Data != wantData {
			t.Fatalf("%s: response GPI=0x%04x data=0x%04x, want 0x%04x", fixture.Name, fixture.GPI&responseMask, fixture.Data, wantData)
		}
		return finalToggle
	}
	identityWords := make([]uint16, constants("FesGpIdentityWordCount"))
	identityWords[constants("FesGpIdentityMagic0Index")] = uint16(constants("FesGpIdentityMagic0"))
	identityWords[constants("FesGpIdentityMagic1Index")] = uint16(constants("FesGpIdentityMagic1"))
	identityWords[constants("FesGpIdentityTransportMajorIndex")] = uint16(constants("FesGpTransportMajor"))
	identityWords[constants("FesGpIdentityTransportMinorIndex")] = uint16(constants("FesGpTransportMinor"))
	identityWords[constants("FesGpIdentityAbiTagIndex")] = uint16(constants("FesGpAbiTag"))
	identityWords[constants("FesGpIdentityAbiMajorIndex")] = uint16(constants("FesGpAbiMajor"))
	identityWords[constants("FesGpIdentityAbiMinorIndex")] = uint16(constants("FesGpAbiMinor"))
	var capabilities uint16
	for _, iface := range abi.Interfaces {
		// This retained v1 fixture describes the original volatile core, not
		// every interface that a newer runtime can support.
		if iface.ID == "fes.gamepad" || iface.ID == "fes.video.fixed-720p60" {
			capabilities |= 1 << iface.CapabilityBit
		}
	}
	identityWords[constants("FesGpIdentityCapabilitiesIndex")] = capabilities
	buildID, err := hex.DecodeString(fixtureSet.BuildID)
	if err != nil || len(buildID) != 16 {
		t.Fatalf("synthetic build ID %q: %v", fixtureSet.BuildID, err)
	}
	buildStart := constants("FesGpIdentityBuildIDStartIndex")
	for i := 0; i < len(buildID)/2; i++ {
		identityWords[buildStart+uint32(i)] = uint16(buildID[i*2]) | uint16(buildID[i*2+1])<<8
	}
	requestToggle := fixtureSet.InitialRequestToggle
	for index := 0; index < 16; index++ {
		want := fmt.Sprintf("identity-word-%02d", index)
		if index == 0 {
			want = "identity-word-zero"
		}
		if fixtures[index].Name != want {
			t.Fatalf("discovery[%d].name = %q, want %q", index, fixtures[index].Name, want)
		}
		requestToggle = assertExchange(t, fixtures[index], constants("FesGpOpcodeIdentity"), uint32(index), 0, false, identityWords[index], requestToggle)
	}
	requestToggle = assertExchange(t, fixtures[16], constants("FesGpOpcodeIdentity"), 0, 0, false, identityWords[0], requestToggle)
	if fixtures[16].Name != "toggle-back" {
		t.Fatalf("fixture 16 = %q, want toggle-back", fixtures[16].Name)
	}
	for offset, test := range []struct {
		name                          string
		opcode, index, argument, data uint32
		failed                        bool
	}{
		{"invalid-opcode", 0x7f, 0, 0, 1, true},
		{"invalid-index", constants("FesGpOpcodeIdentity"), 16, 0, 2, true},
		{"invalid-argument", constants("FesGpOpcodeIdentity"), 0, 1, 3, true},
		{"gameplay-hold-reset", constants("FesGpOpcodeGameplay"), 0, constants("FesGpGameplayHoldReset"), 0, false},
		{"buttons-neutral", constants("FesGpOpcodeButtons"), 0, 0, 0, false},
	} {
		fixture := fixtures[17+offset]
		if fixture.Name != test.name {
			t.Fatalf("fixture %d = %q, want %q", 17+offset, fixture.Name, test.name)
		}
		requestToggle = assertExchange(t, fixture, test.opcode, test.index, test.argument, test.failed, uint16(test.data), requestToggle)
	}
}
