package pack

import (
	"path/filepath"
	"testing"
)

func TestCorePersistenceWireContract(t *testing.T) {
	abi, err := LoadABI(filepath.Join(repoRoot(t), "packages", "abi", "fes_simple_game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if abi.Major != 1 || abi.Minor != 0 {
		t.Fatalf("base ABI changed: %+v", abi)
	}
	for name, want := range map[string]uint32{
		"FesGpOpcodeDataControl": 4, "FesGpOpcodeDataRead": 5,
		"FesGpOpcodeDataWrite": 6, "FesGpOpcodeDataInfo": 7,
		"FesGpDataFreeze": 0, "FesGpDataBegin": 1,
		"FesGpDataCommit": 2, "FesGpDataResume": 3,
		"FesGpErrorInvalidState":      4,
		"FesGpDataInfoWordCountIndex": 0, "FesGpDataInfoLayoutTagIndex": 1,
		"FesGpDataInfoLayoutMajorIndex": 2, "FesGpDataInfoLayoutMinorIndex": 3,
		"FesGpDataMaximumWords": 256, "FesGpPongProgressTag": 1,
		"FesGpPongProgressWordCount": 2, "FesGpPongPaddleSpeedIndex": 0,
		"FesGpPongBestRallyIndex": 1, "FesGpPongPaddleSpeedSlow": 0,
		"FesGpPongPaddleSpeedNormal": 1, "FesGpPongPaddleSpeedFast": 2,
	} {
		got, ok := abi.Constant(name)
		if !ok || got != want {
			t.Errorf("%s = %d (present %t), want %d", name, got, ok, want)
		}
	}
	for id, bit := range map[string]uint8{"fes.persistence.words": 2, "fes.pong.progress": 3} {
		found := false
		for _, iface := range abi.Interfaces {
			if iface.ID == id {
				found = true
				if iface.Major != 1 || iface.Minor != 0 || iface.CapabilityBit != bit {
					t.Errorf("%s contract = %+v", id, iface)
				}
			}
		}
		if !found {
			t.Errorf("missing persistence interface %s", id)
		}
	}
}
