// SPDX-License-Identifier: GPL-3.0-or-later
package misterruntime

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"
)

// de25Pc386Status rewrites the recorded DE10 pc386 session (fes.computer 1.1,
// persistent, its ATA disk unit live) as the DE25 runtime reports it: the
// standard DE25 computer profile, the board target, and the window and
// scanout fabric beside the DE10 interfaces.
func de25Pc386Status(t *testing.T, coreID string) string {
	t.Helper()
	var response map[string]any
	if err := json.Unmarshal([]byte(fixtureLines(t, "protocol-v2-pc386-responses.jsonl")[2]), &response); err != nil {
		t.Fatal(err)
	}
	fabric := []map[string]any{
		{"id": "fes.fabric.de25-hps-window", "major": 1, "minor": 0},
		{"id": "fes.fabric.de25-scanout", "major": 1, "minor": 3},
	}
	active := response["active_package"].(map[string]any)
	descriptor := active["descriptor"].(map[string]any)
	if descriptor["core"].(map[string]any)["id"] != "fes.pc386" {
		t.Fatal("fixture is not the pc386 session")
	}
	descriptor["core"].(map[string]any)["id"] = coreID
	descriptor["target"] = map[string]any{"platform": "de25_nano", "device": "A5EB013BB23BE4SCS",
		"programming_profile": "de25-linux-computer-v1"}
	interfaces := descriptor["interfaces"].([]any)
	for _, f := range fabric {
		interfaces = append(interfaces, map[string]any{"id": f["id"], "major": f["major"], "minor": f["minor"], "required": true})
	}
	descriptor["interfaces"] = interfaces
	capabilities := response["capabilities"].(map[string]any)
	capabilities["programming_profiles"] = []any{"de25-linux-computer-v1"}
	for _, raw := range capabilities["abis"].([]any) {
		abi := raw.(map[string]any)
		if abi["id"] == "fes.computer" && abi["minor"] == float64(1) {
			listed := abi["interfaces"].([]any)
			for _, f := range fabric {
				listed = append(listed, f)
			}
			sort.Slice(listed, func(i, j int) bool {
				return listed[i].(map[string]any)["id"].(string) < listed[j].(map[string]any)["id"].(string)
			})
			abi["interfaces"] = listed
		}
	}
	live := capabilities["active_interfaces"].([]any)
	for _, f := range fabric {
		live = append(live, f)
	}
	// The runtime reports its interface lists in identifier order.
	sort.Slice(live, func(i, j int) bool {
		return live[i].(map[string]any)["id"].(string) < live[j].(map[string]any)["id"].(string)
	})
	capabilities["active_interfaces"] = live
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestProtocol2DE25Pc386SessionIsAdmitted(t *testing.T) {
	response, err := decodeProtocol2Response([]byte(de25Pc386Status(t, "fes.pc386")))
	if err != nil || response.ActivePackage == nil {
		t.Fatalf("DE25 pc386 session refused: %v", err)
	}
	units := response.Capabilities.MediaUnits
	if len(units) != 1 || units[0].Interface.ID != "fes.media.ata-disk" {
		t.Fatalf("DE25 pc386 ATA disk unit lost: %+v", units)
	}
}

func TestProtocol2DE25ComputerABIIsPerMachine(t *testing.T) {
	// fes.computer 1.1 belongs to the machines that declare its interfaces;
	// any other DE25 machine reporting it is refused.
	if _, err := decodeProtocol2Response([]byte(de25Pc386Status(t, "fes.spectrum"))); !errors.Is(err, errInvalidRuntimeResponse) {
		t.Fatalf("a 1.0 DE25 machine was admitted at 1.1: %v", err)
	}
}
