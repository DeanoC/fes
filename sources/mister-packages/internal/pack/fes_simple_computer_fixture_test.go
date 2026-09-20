package pack

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFesSimpleComputerWireVersionConstantsRejectMetadataDrift(t *testing.T) {
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages", "abi", "fes_simple_computer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFesSimpleComputerWireVersionConstants(abi); err != nil {
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
			if err := validateFesSimpleComputerWireVersionConstants(&changed); err == nil {
				t.Fatal("wire constants accepted divergent ABI metadata")
			}
		})
	}
}

func validateFesSimpleComputerWireVersionConstants(abi *ABIFile) error {
	for _, field := range []struct {
		constant string
		metadata uint32
	}{
		{"FesSimpleComputerAbiTag", uint32(abi.Tag)},
		{"FesSimpleComputerAbiMajor", uint32(abi.Major)},
		{"FesSimpleComputerAbiMinor", uint32(abi.Minor)},
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

func TestFesSimpleComputerGoldenExchangeFixture(t *testing.T) {
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages", "abi", "fes_simple_computer.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFesSimpleComputerWireVersionConstants(abi); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repoRoot(t), "testdata", "fes-simple-computer-v1", "exchanges.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtureSet fesGPFixture
	if err := json.Unmarshal(data, &fixtureSet); err != nil {
		t.Fatal(err)
	}
	fixtures := fixtureSet.Exchanges
	if len(fixtures) != 38 {
		t.Fatalf("exchange count = %d, want 38", len(fixtures))
	}
	constants := func(name string) uint32 {
		t.Helper()
		value, ok := abi.Constant(name)
		if !ok {
			t.Fatalf("missing ABI constant %q", name)
		}
		return value
	}
	requestMask := constants("FesSimpleComputerRequestMask")
	opcodeMask := constants("FesSimpleComputerOpcodeMask")
	indexMask := constants("FesSimpleComputerIndexMask")
	argumentMask := constants("FesSimpleComputerArgumentMask")
	ackMask := constants("FesSimpleComputerAckMask")
	errorMask := constants("FesSimpleComputerErrorMask")
	responseMask := constants("FesSimpleComputerResponseMask")
	signature := constants("FesSimpleComputerSignature")
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
	identityWords := make([]uint16, constants("FesSimpleComputerIdentityWordCount"))
	identityWords[constants("FesSimpleComputerIdentityMagic0Index")] = uint16(constants("FesSimpleComputerIdentityMagic0"))
	identityWords[constants("FesSimpleComputerIdentityMagic1Index")] = uint16(constants("FesSimpleComputerIdentityMagic1"))
	identityWords[constants("FesSimpleComputerIdentityTransportMajorIndex")] = uint16(constants("FesSimpleComputerTransportMajor"))
	identityWords[constants("FesSimpleComputerIdentityTransportMinorIndex")] = uint16(constants("FesSimpleComputerTransportMinor"))
	identityWords[constants("FesSimpleComputerIdentityAbiTagIndex")] = uint16(constants("FesSimpleComputerAbiTag"))
	identityWords[constants("FesSimpleComputerIdentityAbiMajorIndex")] = uint16(constants("FesSimpleComputerAbiMajor"))
	identityWords[constants("FesSimpleComputerIdentityAbiMinorIndex")] = uint16(constants("FesSimpleComputerAbiMinor"))
	var capabilities uint16
	for _, iface := range abi.Interfaces {
		// This immutable fixture represents a legacy package, not every
		// interface now available in the registry.
		if iface.ID == "fes.media.blob-stream" {
			continue
		}
		capabilities |= 1 << iface.CapabilityBit
	}
	if capabilities != 7 {
		t.Fatalf("capability mask = %d, want 7", capabilities)
	}
	identityWords[constants("FesSimpleComputerIdentityCapabilitiesIndex")] = capabilities
	buildID, err := hex.DecodeString(fixtureSet.BuildID)
	if err != nil || len(buildID) != 16 {
		t.Fatalf("synthetic build ID %q: %v", fixtureSet.BuildID, err)
	}
	buildStart := constants("FesSimpleComputerIdentityBuildIDStartIndex")
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
		requestToggle = assertExchange(t, fixtures[index], constants("FesSimpleComputerOpcodeIdentity"), uint32(index), 0, false, identityWords[index], requestToggle)
	}
	offset := 16
	requestToggle = assertExchange(t, fixtures[offset], constants("FesSimpleComputerOpcodeIdentity"), 0, 0, false, identityWords[0], requestToggle)
	if fixtures[offset].Name != "toggle-back" {
		t.Fatalf("fixture %d = %q, want toggle-back", offset, fixtures[offset].Name)
	}
	offset++
	for _, test := range []struct {
		name                          string
		opcode, index, argument, data uint32
		failed                        bool
	}{
		{"invalid-opcode", 0x7f, 0, 0, 1, true},
		{"invalid-index", constants("FesSimpleComputerOpcodeIdentity"), 16, 0, 2, true},
		{"invalid-argument", constants("FesSimpleComputerOpcodeIdentity"), 0, 1, 3, true},
		{"execution-hold-reset", constants("FesSimpleComputerOpcodeExecution"), 0, constants("FesSimpleComputerExecutionHoldReset"), 0, false},
	} {
		fixture := fixtures[offset]
		if fixture.Name != test.name {
			t.Fatalf("fixture %d = %q, want %q", offset, fixture.Name, test.name)
		}
		requestToggle = assertExchange(t, fixture, test.opcode, test.index, test.argument, test.failed, uint16(test.data), requestToggle)
		offset++
	}
	for row := uint32(0); row < constants("FesSimpleComputerKeyboardRowCount"); row++ {
		want := fmt.Sprintf("keyboard-row-%d", row)
		if fixtures[offset].Name != want {
			t.Fatalf("fixture %d = %q, want %q", offset, fixtures[offset].Name, want)
		}
		requestToggle = assertExchange(t, fixtures[offset], constants("FesSimpleComputerOpcodeKeyboard"), row, constants("FesSimpleComputerKeyboardNeutralRow"), false, 0, requestToggle)
		offset++
	}
	for _, test := range []struct {
		name                          string
		opcode, index, argument, data uint32
		failed                        bool
	}{
		{"keyboard-invalid-index", constants("FesSimpleComputerOpcodeKeyboard"), 8, constants("FesSimpleComputerKeyboardNeutralRow"), 2, true},
		{"media-begin-zero", constants("FesSimpleComputerOpcodeMediaBegin"), 0, 0, 3, true},
		{"media-data-before-begin", constants("FesSimpleComputerOpcodeMediaData"), constants("FesSimpleComputerMediaDataPairIndex"), 0x0201, 4, true},
		{"media-begin-3", constants("FesSimpleComputerOpcodeMediaBegin"), 0, 3, 0, false},
		{"media-data-pair", constants("FesSimpleComputerOpcodeMediaData"), constants("FesSimpleComputerMediaDataPairIndex"), 0x0201, 0, false},
		{"media-data-tail", constants("FesSimpleComputerOpcodeMediaData"), constants("FesSimpleComputerMediaDataTailIndex"), 0x0003, 0, false},
		{"media-commit", constants("FesSimpleComputerOpcodeMediaCommit"), 0, 0, 0, false},
		{"media-data-after-commit", constants("FesSimpleComputerOpcodeMediaData"), constants("FesSimpleComputerMediaDataPairIndex"), 0x0201, 4, true},
		{"execution-release", constants("FesSimpleComputerOpcodeExecution"), 0, constants("FesSimpleComputerExecutionRelease"), 0, false},
	} {
		fixture := fixtures[offset]
		if fixture.Name != test.name {
			t.Fatalf("fixture %d = %q, want %q", offset, fixture.Name, test.name)
		}
		requestToggle = assertExchange(t, fixture, test.opcode, test.index, test.argument, test.failed, uint16(test.data), requestToggle)
		offset++
	}
	if offset != len(fixtures) {
		t.Fatalf("consumed %d exchanges, have %d", offset, len(fixtures))
	}
}
