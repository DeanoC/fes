package misterruntime

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

func stVideoResponse(t *testing.T) (Protocol2Response, expansion.PartsComposition, corepackage.ROMLinkIdentity) {
	t.Helper()
	r := atariStComputerResponse(t)
	a := r.ActivePackage
	a.Descriptor.Format = 3
	link := corepackage.ROMLinkIdentity{ROMID: "atari-st-firmware", MapSHA256: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64), SourceSize: 192 << 10, ProgrammedSHA256: strings.Repeat("f", 64), ProgrammedSize: 50000}
	a.Descriptor.ROM = &corepackage.ROM{ID: link.ROMID, Role: "firmware", File: "rom-map.json", Size: 100, SHA256: link.MapSHA256, SourceSize: link.SourceSize}
	a.Descriptor.Interfaces = append(a.Descriptor.Interfaces, corepackage.Interface{ID: expansion.VideoSlot, Major: 1})
	c := expansion.PartsComposition{PackageID: a.PackageID, Layout: expansion.AtariStVideoLayout,
		Parts:       []expansion.PartSelection{{Role: "expansion", PartID: strings.Repeat("c", 64)}, {Role: "video", PartID: strings.Repeat("d", 64)}},
		ShellSHA256: a.Descriptor.Payload.SHA256, PayloadSHA256: strings.Repeat("e", 64), PayloadSize: 40408}
	var err error
	c.ID, err = expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
	if err != nil {
		t.Fatal(err)
	}
	a.PartsComposition, a.ROMLink = clonePartsComposition(&c), &link
	r.Capabilities.ROMLinking = 1
	return r, c, link
}

func TestSTVideoROMClientRetainsBothReceiptsAndExactRequestShape(t *testing.T) {
	r, c, link := stVideoResponse(t)
	line := responseLine(t, r)
	decoded, err := decodeProtocol2Response([]byte(line))
	if err != nil || !reflect.DeepEqual(decoded.ActivePackage.PartsComposition, &c) || !reflect.DeepEqual(decoded.ActivePackage.ROMLink, &link) {
		t.Fatalf("paired status: %v", err)
	}
	idle := strings.Replace(fixtureLines(t, "protocol-v2.jsonl")[1], `"capabilities":{`, `"capabilities":{"rom_linking":1,`, 1) + "\n"
	paths := []PartPath{{"expansion", "/stage/card"}, {"video", "/stage/video"}}
	for _, mismatch := range []bool{false, true} {
		reply := line
		if mismatch {
			reply = strings.Replace(reply, link.SourceSHA256, strings.Repeat("9", 64), 1)
		}
		socket := newSequenceSocketFixture(t, []string{idle, reply + "\n"})
		_, err := NewClient(socket.path).LoadROMPartsComposedCore(context.Background(), "/stage/shell", c.PackageID, paths, "/stage/parts/linked.rbf", c, "/stage/rom/programmed.rbf", link)
		if (err != nil) != mismatch {
			t.Fatalf("mismatch=%v error=%v", mismatch, err)
		}
		requests := socket.wait(t)
		var got map[string]json.RawMessage
		if len(requests) != 2 || json.Unmarshal([]byte(requests[1]), &got) != nil || len(got) != 9 || string(got["operation"]) != `"load_rom_composed_core"` || got["parts"] == nil || got["rom_link"] == nil || got["expansions"] != nil || got["data_root"] != nil {
			t.Fatalf("request: %v", requests)
		}
		var parts []PartPath
		var composition expansion.PartsComposition
		var receipt corepackage.ROMLinkIdentity
		if json.Unmarshal(got["parts"], &parts) != nil || json.Unmarshal(got["composition"], &composition) != nil || json.Unmarshal(got["rom_link"], &receipt) != nil || !reflect.DeepEqual(parts, paths) || !reflect.DeepEqual(composition, c) || receipt != link {
			t.Fatal("request changed retained identities")
		}
	}
	noROM := newSequenceSocketFixture(t, []string{fixtureLines(t, "protocol-v2.jsonl")[1] + "\n"})
	if _, err := NewClient(noROM.path).LoadROMPartsComposedCore(context.Background(), "/stage/shell", c.PackageID, paths, "/stage/parts/linked.rbf", c, "/stage/rom/programmed.rbf", link); err == nil {
		t.Fatal("missing ROM capability accepted")
	}
	if requests := noROM.wait(t); len(requests) != 1 {
		t.Fatal("mutated before capability admission", requests)
	}
	for _, mutate := range []func(*Protocol2Response){
		func(r *Protocol2Response) {
			r.ActivePackage.PartsComposition.Layout = expansion.ColecoVideoLayout
			r.ActivePackage.PartsComposition.ID, _ = expansion.PartsCompositionID(c.PackageID, expansion.ColecoVideoLayout, c.Parts, c.PayloadSHA256)
		},
		func(r *Protocol2Response) { r.ActivePackage.ROMLink = nil },
		func(r *Protocol2Response) {
			r.ActivePackage.Descriptor.Interfaces[len(r.ActivePackage.Descriptor.Interfaces)-1].ID = expansion.NativeVideoSlot
		},
		func(r *Protocol2Response) { r.ActivePackage.PartsComposition.PayloadSHA256 = strings.Repeat("9", 64) },
	} {
		var bad Protocol2Response
		_ = json.Unmarshal([]byte(line), &bad)
		mutate(&bad)
		if _, err := decodeProtocol2Response([]byte(responseLine(t, bad))); err == nil {
			t.Fatal("invalid paired ST receipt accepted")
		}
	}
}
