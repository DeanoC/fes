package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

// decodeGameRecord is an independent decoder of the fes.computer core-data
// record (docs/computer-io.md, "Persistence words").
func decodeGameRecord(data []byte, core, game, wantLayout string, wantCount int, valid func([]uint16) bool) ([]uint16, bool) {
	if len(data) < 113 || len(data) > 720 || string(data[:8]) != "FESGAME1" {
		return nil, false
	}
	sum := sha256.Sum256(data[:len(data)-32])
	coreHash, gameHash := sha256.Sum256([]byte(core)), sha256.Sum256([]byte(game))
	if !bytes.Equal(sum[:], data[len(data)-32:]) || !bytes.Equal(data[8:40], coreHash[:]) ||
		!bytes.Equal(data[40:72], gameHash[:]) {
		return nil, false
	}
	length := int(binary.LittleEndian.Uint16(data[72:]))
	count := int(binary.LittleEndian.Uint16(data[78:]))
	if length == 0 || length > 96 || count == 0 || count > 256 || len(data) != 112+length+2*count {
		return nil, false
	}
	if string(data[80:80+length]) != wantLayout || binary.LittleEndian.Uint16(data[74:]) != 1 ||
		binary.LittleEndian.Uint16(data[76:]) != 0 || count != wantCount {
		return nil, false
	}
	words := make([]uint16, count)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(data[80+length+2*i:])
	}
	return words, valid(words)
}

func checkGameRecordFixture(t *testing.T, name, wantLayout string, wantCount, minInvalid int, valid func([]uint16) bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "core-persistence-v1", name))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Core      string `json:"core_id"`
		Game      string `json:"game_id"`
		Namespace string
		Valid     []struct {
			Name, Hex, Revision string
			Words               []uint16
		}
		Invalid []struct{ Name, Hex string }
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	namespace := sha256.Sum256([]byte("fes-core-game-data-v1\x00" + fixture.Core + "\x00" + fixture.Game))
	if hex.EncodeToString(namespace[:]) != fixture.Namespace {
		t.Fatal("namespace differs")
	}
	for _, row := range fixture.Valid {
		data, _ := hex.DecodeString(row.Hex)
		words, ok := decodeGameRecord(data, fixture.Core, fixture.Game, wantLayout, wantCount, valid)
		revision := sha256.Sum256(data)
		if !ok || !reflect.DeepEqual(words, row.Words) || hex.EncodeToString(revision[:]) != row.Revision {
			t.Errorf("valid %s rejected", row.Name)
		}
	}
	if len(fixture.Invalid) < minInvalid {
		t.Fatal("too few invalid records")
	}
	for _, row := range fixture.Invalid {
		data, _ := hex.DecodeString(row.Hex)
		if _, ok := decodeGameRecord(data, fixture.Core, fixture.Game, wantLayout, wantCount, valid); ok {
			t.Errorf("invalid %s accepted", row.Name)
		}
	}
}

func TestPc386MachineRecords(t *testing.T) {
	checkGameRecordFixture(t, "pc386-machine.json", "fes.pc386.machine", 72, 20, machineValid)
}

func TestPcxtMachineRecords(t *testing.T) {
	checkGameRecordFixture(t, "pcxt-machine.json", "fes.pcxt.machine", 4, 15, pcxtMachineValid)
}
