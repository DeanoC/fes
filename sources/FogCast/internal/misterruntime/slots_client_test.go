package misterruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

func slotResponse(t *testing.T) (Protocol2Response, expansion.SlotComposition) {
	t.Helper()
	r := computerResponse(t)
	active := r.ActivePackage
	c := expansion.SlotComposition{PackageID: active.PackageID, ShellSHA256: active.Descriptor.Payload.SHA256,
		Expansions:    []expansion.SlotExpansion{{Slot: 2, ExpansionID: strings.Repeat("c", 64)}, {Slot: 7, ExpansionID: strings.Repeat("d", 64)}},
		PayloadSHA256: strings.Repeat("e", 64), PayloadSize: 40408}
	c.ID, _ = expansion.SlotCompositionID(c.PackageID, c.Expansions, c.PayloadSHA256)
	active.SlotComposition = &c
	return r, c
}

func TestSlotCompositionWireFormRoundTrips(t *testing.T) {
	r, c := slotResponse(t)
	line := responseLine(t, r)
	if !strings.Contains(line, `"composition":{"composition_id":"`+c.ID+`","package_id":"`+c.PackageID+`","expansions":[{"slot":2,"expansion_id":"`) {
		t.Fatalf("v2 tuple not under composition: %s", line)
	}
	decoded, err := decodeProtocol2Response([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ActivePackage.Composition != nil || decoded.ActivePackage.SlotComposition == nil || decoded.ActivePackage.SlotComposition.ID != c.ID {
		t.Fatalf("decoded %+v", decoded.ActivePackage)
	}
	status := corePackageStatus(activationFromProtocol2(c.PackageID, decoded.ActivePackage.Descriptor, decoded))
	if status.SlotComposition == nil || len(status.SlotComposition.Expansions) != 2 || status.Composition != nil {
		t.Fatalf("status %+v", status)
	}
	for name, bad := range map[string]string{
		"unknown tuple field": strings.Replace(line, `"payload_size":40408`, `"payload_size":40408,"extra":0`, 1),
		"unknown card field":  strings.Replace(line, `{"slot":2,`, `{"extra":1,"slot":2,`, 1),
		"string slot":         strings.Replace(line, `"slot":2`, `"slot":"2"`, 1),
		"both forms":          strings.Replace(line, `"expansions":[`, `"expansion_id":"`+strings.Repeat("c", 64)+`","expansions":[`, 1),
		"wrong identity":      strings.Replace(line, c.ID, strings.Repeat("0", 64), 1),
		"logical-only slot":   strings.Replace(line, `{"slot":2,`, `{"slot":3,`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProtocol2Response([]byte(bad)); err == nil {
				t.Fatal("invalid v2 tuple accepted")
			}
		})
	}
	required := r
	pkg := *r.ActivePackage
	required.ActivePackage = &pkg
	pkg.Descriptor.Interfaces = append([]corepackage.Interface(nil), pkg.Descriptor.Interfaces...)
	pkg.Descriptor.Interfaces[5].Required = true
	if validActivePackage(pkg, required.Capabilities) {
		t.Fatal("v2 tuple accepted for a required bus")
	}
	pkg.Descriptor.Interfaces[5].Required = false
	pkg.PersistenceMode = "persistent"
	if validActivePackage(pkg, required.Capabilities) {
		t.Fatal("persistent slot composition accepted")
	}
}

func TestSlotComposedLoadRequestShapes(t *testing.T) {
	r, c := slotResponse(t)
	id := c.PackageID
	paths := []SlotExpansionPath{{Slot: 2, Path: "/stage/slot-2-x"}, {Slot: 7, Path: "/stage/slot-7-x"}}
	idle := fixtureLines(t, "protocol-v2.jsonl")[1] + "\n"
	fixture := newSequenceSocketFixture(t, []string{idle, responseLine(t, r) + "\n"})
	if _, err := NewClient(fixture.path).LoadSlotComposedCore(context.Background(), "/stage/base", id, paths, "/stage/composition-x/linked.rbf", c); err != nil {
		t.Fatal(err)
	}
	requests := fixture.wait(t)
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(requests[1]), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 || string(got["operation"]) != `"load_composed_core"` || string(got["expansions"]) != `[{"slot":2,"path":"/stage/slot-2-x"},{"slot":7,"path":"/stage/slot-7-x"}]` ||
		string(got["payload_path"]) != `"/stage/composition-x/linked.rbf"` || got["expansion_path"] != nil || !strings.Contains(string(got["composition"]), `"expansions":[{"slot":2`) {
		t.Fatalf("request %s", requests[1])
	}

	identity := corepackage.ROMLinkIdentity{ROMID: "apple2-firmware", MapSHA256: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64), SourceSize: 16384, ProgrammedSHA256: strings.Repeat("f", 64), ProgrammedSize: 50000}
	romIdle := strings.Replace(idle, `"capabilities":{`, `"capabilities":{"rom_linking":1,`, 1)
	romReply := r
	romReply.Capabilities.ROMLinking = 1
	romPackage := *r.ActivePackage
	romPackage.Descriptor.Format = 3
	romPackage.Descriptor.ROM = &corepackage.ROM{ID: identity.ROMID, Role: "firmware", SourceSize: identity.SourceSize, File: "rom-map.json", Size: 100, SHA256: identity.MapSHA256}
	romPackage.ROMLink = &identity
	romReply.ActivePackage = &romPackage
	romFixture := newSequenceSocketFixture(t, []string{romIdle, responseLine(t, romReply) + "\n"})
	if _, err := NewClient(romFixture.path).LoadROMSlotComposedCore(context.Background(), "/stage/base", id, paths, "/stage/composition-x/linked.rbf", c, "/stage/rom-link-x/programmed.rbf", identity); err != nil {
		t.Fatal(err)
	}
	requests = romFixture.wait(t)
	got = nil
	if err := json.Unmarshal([]byte(requests[1]), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 9 || string(got["operation"]) != `"load_rom_composed_core"` || string(got["programmed_path"]) != `"/stage/rom-link-x/programmed.rbf"` || got["rom_link"] == nil || got["expansions"] == nil {
		t.Fatalf("ROM request %s", requests[1])
	}

	initFixture := newSequenceSocketFixture(t, []string{idle, responseLine(t, r) + "\n"})
	if _, err := NewClient(initFixture.path).LoadInitializedSlotComposedCore(context.Background(), "/stage/base", id, paths, "/stage/composition-x/linked.rbf", c, "/stage/programmed.rbf", strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	if request := initFixture.wait(t)[1]; !strings.Contains(request, `"operation":"load_initialized_composed_core"`) || !strings.Contains(request, `"programmed_sha256":"`+strings.Repeat("9", 64)+`"`) {
		t.Fatalf("initialized request %s", request)
	}

	client := NewClient("/not/opened")
	for name, bad := range map[string][]SlotExpansionPath{
		"none":       nil,
		"unordered":  {paths[1], paths[0]},
		"duplicate":  {paths[0], paths[0]},
		"relative":   {{Slot: 2, Path: "slot-2"}, paths[1]},
		"other slot": {{Slot: 4, Path: "/stage/slot-4"}, paths[1]},
	} {
		if _, err := client.LoadSlotComposedCore(context.Background(), "/stage/base", id, bad, "/stage/linked.rbf", c); err != errInvalidRuntimeRequest {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	forged := c
	forged.PayloadSHA256 = strings.Repeat("1", 64)
	if _, err := client.LoadSlotComposedCore(context.Background(), "/stage/base", id, paths, "/stage/linked.rbf", forged); err != errInvalidRuntimeRequest {
		t.Fatal("forged composition identity accepted")
	}
	mismatch := newSequenceSocketFixture(t, []string{idle, responseLine(t, r) + "\n"})
	other := c
	other.Expansions = []expansion.SlotExpansion{c.Expansions[0]}
	other.ID, _ = expansion.SlotCompositionID(other.PackageID, other.Expansions, other.PayloadSHA256)
	if _, err := NewClient(mismatch.path).LoadSlotComposedCore(context.Background(), "/stage/base", id, paths[:1], "/stage/linked.rbf", other); err == nil {
		t.Fatal("reply with another composition accepted")
	}
}
