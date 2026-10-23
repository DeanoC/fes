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

func TestMenuBlitterContract(t *testing.T) {
	app, err := pack.LoadABI("../../packages/abi/fes_application.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uint32{
		"OpcodeMenuBlit": 22, "MenuBlitFirstIndex": 0, "MenuBlitCountIndex": 1, "MenuBlitRunIndex": 2,
		"MenuBlitStateIndex": 3, "MenuBlitCompletedIndex": 4,
		"MenuBlitListOffset": 0x800000, "MenuBlitListBytes": 0x100000, "MenuBlitCommandBytes": 32,
		"MenuBlitCommandCount": 32768, "MenuBlitHeapOffset": 0x1000000, "MenuBlitHeapBytes": 0x4000000,
		"MenuBlitOpEnd": 0, "MenuBlitOpFill": 1, "MenuBlitOpFillBlend": 2, "MenuBlitOpCopy": 3, "MenuBlitOpBlend": 4, "MenuBlitOpExpand": 5,
		"MenuBlitStateBusy": 1, "MenuBlitStateFaulted": 2, "MenuBlitStateFaultShift": 8,
		"MenuBlitFaultList": 1, "MenuBlitFaultOp": 2, "MenuBlitFaultRect": 3, "MenuBlitFaultSource": 4,
		"MenuBlitFaultReserved": 5,
	}
	for suffix, value := range want {
		got, ok := app.Constant("FesApplication" + suffix)
		if !ok || got != value {
			t.Errorf("%s: got %d present %v want %d", suffix, got, ok, value)
		}
	}
	listBytes, _ := app.Constant("FesApplicationMenuBlitListBytes")
	commandBytes, _ := app.Constant("FesApplicationMenuBlitCommandBytes")
	commands, _ := app.Constant("FesApplicationMenuBlitCommandCount")
	if listBytes != commandBytes*commands {
		t.Errorf("list region %d is not %d commands of %d bytes", listBytes, commands, commandBytes)
	}
	// The slots, list region and heap stay inside the HPS DDR window, in order.
	slots, _ := app.Constant("FesApplicationMenuSlotBytes")
	listOffset, _ := app.Constant("FesApplicationMenuBlitListOffset")
	heapOffset, _ := app.Constant("FesApplicationMenuBlitHeapOffset")
	heapBytes, _ := app.Constant("FesApplicationMenuBlitHeapBytes")
	window, _ := app.Constant("FesApplicationHpsDdrWindowBytes")
	if 2*slots > listOffset || listOffset+listBytes > heapOffset || uint64(heapOffset)+uint64(heapBytes) > uint64(window) {
		t.Error("blitter regions overlap the slots or leave the window")
	}
	found := false
	for _, iface := range app.Interfaces {
		if iface.ID == "fes.video.menu-blitter" {
			found = true
			if iface.CapabilityBit != 10 || iface.Major != 1 || iface.Minor != 1 {
				t.Fatalf("menu blitter interface: %+v", iface)
			}
		}
	}
	if !found {
		t.Error("menu blitter interface missing")
	}
	cpp, err := emitcpp.GenerateABI(app)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cpp, "FesApplicationCapabilityVideoMenuBlitter = 0x400u") {
		t.Error("missing emitted menu blitter capability")
	}
}
