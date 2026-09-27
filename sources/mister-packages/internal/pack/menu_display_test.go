package pack_test

import (
	"github.com/DeanoC/mister-packages/internal/emitcpp"
	"github.com/DeanoC/mister-packages/internal/pack"
	"strings"
	"testing"
)

func TestMenuDisplayContract(t *testing.T) {
	app, err := pack.LoadABI("../../packages/abi/fes_application.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uint32{
		"OpcodeMenuInfo": 18, "OpcodeMenuConfigure": 19, "OpcodeMenuControl": 20, "OpcodeMenuSubmit": 21,
		"MenuWidth": 1280, "MenuHeight": 720, "MenuStride": 5120, "MenuFrameBytes": 3686400,
		"MenuSlotBytes": 4194304, "MenuSlotCount": 2, "MenuPixelFormat": 1, "MenuLayout": 1,
		"MenuStateConfigured": 1, "MenuStateEnabled": 2, "MenuStatePending": 4, "MenuStateQuiesced": 8, "MenuStateFaulted": 16,
		"MenuInfoStateIndex": 9, "MenuInfoSequenceLoIndex": 10, "MenuInfoSequenceHiIndex": 11,
		"MenuInfoUnderflowsLoIndex": 12, "MenuInfoUnderflowsHiIndex": 13,
		"MenuSubmitSequenceLoIndex": 0, "MenuSubmitSequenceHiIndex": 1, "MenuSubmitCommitIndex": 2,
	}
	for suffix, value := range want {
		got, ok := app.Constant("FesApplication" + suffix)
		if !ok || got != value {
			t.Errorf("%s: got %d present %v want %d", suffix, got, ok, value)
		}
	}
	found := false
	for _, iface := range app.Interfaces {
		if iface.ID == "fes.video.menu-display" {
			found = true
			if iface.CapabilityBit != 9 || iface.Major != 1 || iface.Minor != 0 {
				t.Fatalf("menu interface: %+v", iface)
			}
		}
	}
	if !found {
		t.Error("menu interface missing")
	}
	cpp, err := emitcpp.GenerateABI(app)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cpp, "FesApplicationCapabilityVideoMenuDisplay = 0x200u") {
		t.Error("missing emitted menu capability")
	}
}
