package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPc386CoreDataUsesPerGameMachineVariant(t *testing.T) {
	machine := PC386MachineSettings{Preset: 0, SBBase: 0x220, SBIRQ: 7, SBDMA: 1}
	data := CoreData{PackageID: strings.Repeat("a", 64), CoreID: "fes.pc386",
		Layout: &RuntimeContract{ID: "fes.pc386.machine", Major: 1}, Mode: "persistent", Revision: "absent",
		GameID: "ultima-vi", Machine: &machine}
	if !data.Valid() {
		t.Fatal("valid pc386 per-game record rejected")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 7 || string(fields["game_id"]) != `"ultima-vi"` || fields["machine"] == nil || fields["paddle_speed"] != nil || fields["best_rally"] != nil {
		t.Fatalf("pc386 core-data wire shape %s", encoded)
	}
	for name, mutate := range map[string]func(*CoreData){
		"missing game":    func(d *CoreData) { d.GameID = "" },
		"missing machine": func(d *CoreData) { d.Machine = nil },
		"invalid DMA":     func(d *CoreData) { d.Machine.SBDMA = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := data
			machineCopy := machine
			bad.Machine = &machineCopy
			mutate(&bad)
			if bad.Valid() {
				t.Fatal("invalid pc386 core data accepted")
			}
		})
	}
}
