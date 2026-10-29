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

// colecoVideoROMResponse is a running format-4 Coleco native shell with its
// linked BIOS and cartridge and one Direct video part.
func colecoVideoROMResponse(t *testing.T) (Protocol2Response, expansion.PartsComposition, corepackage.ROMLinksIdentity) {
	t.Helper()
	r, c := partsResponseLayout(t, expansion.ColecoNativeVideoLayout)
	a := r.ActivePackage
	links := corepackage.ROMLinksIdentity{MapSHA256: strings.Repeat("a", 64), ProgrammedSHA256: strings.Repeat("f", 64), ProgrammedSize: 50000,
		Sources: []corepackage.ROMSourceIdentity{
			{ID: "coleco-bios", Role: "firmware", SourceSHA256: strings.Repeat("b", 64), SourceSize: 8192},
			{ID: "coleco-cart", Role: "cartridge", SourceSHA256: strings.Repeat("e", 64), SourceSize: 16384},
		}}
	a.Descriptor.Format = 4
	a.Descriptor.ROMs = []corepackage.ROMRequirement{{ID: "coleco-bios", Role: "firmware", SourceSize: 8192}, {ID: "coleco-cart", Role: "cartridge", SourceSize: 16384, SourceOffset: 8192}}
	a.Descriptor.ROMMap = &corepackage.ROMMapDescriptor{File: "rom-map.json", Size: 100, SHA256: links.MapSHA256}
	a.ROMLinks = &links
	r.Capabilities.ROMLinking = 1
	return r, c, links
}

func TestColecoVideoROMLinksClientSendsPairedRequest(t *testing.T) {
	r, c, links := colecoVideoROMResponse(t)
	line := responseLine(t, r)
	decoded, err := decodeProtocol2Response([]byte(line))
	if err != nil || !reflect.DeepEqual(decoded.ActivePackage.PartsComposition, &c) || !reflect.DeepEqual(decoded.ActivePackage.ROMLinks, &links) {
		t.Fatalf("paired status: %v", err)
	}
	idle := strings.Replace(fixtureLines(t, "protocol-v2.jsonl")[1], `"capabilities":{`, `"capabilities":{"rom_linking":1,`, 1) + "\n"
	paths := []PartPath{{"video", "/stage/video"}}
	for _, mismatch := range []bool{false, true} {
		reply := line
		if mismatch {
			reply = strings.Replace(reply, links.Sources[1].SourceSHA256, strings.Repeat("9", 64), 1)
		}
		socket := newSequenceSocketFixture(t, []string{idle, reply + "\n"})
		_, err := NewClient(socket.path).LoadROMLinksPartsComposedCore(context.Background(), "/stage/shell", c.PackageID, paths, "/stage/parts/linked.rbf", c, "/stage/rom/programmed.rbf", links)
		if (err != nil) != mismatch {
			t.Fatalf("mismatch=%v error=%v", mismatch, err)
		}
		requests := socket.wait(t)
		var got map[string]json.RawMessage
		if len(requests) != 2 || json.Unmarshal([]byte(requests[1]), &got) != nil || len(got) != 9 || string(got["operation"]) != `"load_rom_composed_core"` || got["parts"] == nil || got["rom_links"] == nil || got["rom_link"] != nil || got["expansions"] != nil || got["data_root"] != nil {
			t.Fatalf("request: %v", requests)
		}
		var sentParts []PartPath
		var composition expansion.PartsComposition
		var receipt corepackage.ROMLinksIdentity
		if json.Unmarshal(got["parts"], &sentParts) != nil || json.Unmarshal(got["composition"], &composition) != nil || json.Unmarshal(got["rom_links"], &receipt) != nil || !reflect.DeepEqual(sentParts, paths) || !reflect.DeepEqual(composition, c) || !reflect.DeepEqual(receipt, links) {
			t.Fatal("request changed retained identities")
		}
	}
	// Format-3 ROM parts are the ST shell only; Coleco layouts use two sources.
	single := corepackage.ROMLinkIdentity{ROMID: "cartridge-rom", Role: "cartridge", MapSHA256: links.MapSHA256, SourceSHA256: links.Sources[1].SourceSHA256, SourceSize: 16384, ProgrammedSHA256: links.ProgrammedSHA256, ProgrammedSize: links.ProgrammedSize}
	if _, err := NewClient("/absent.sock").LoadROMPartsComposedCore(context.Background(), "/stage/shell", c.PackageID, paths, "/stage/parts/linked.rbf", c, "/stage/rom/programmed.rbf", single); err != errInvalidRuntimeRequest {
		t.Fatalf("Coleco layout sent with a single ROM link: %v", err)
	}
	st := c
	st.Layout = expansion.AtariStVideoLayout
	st.ID, _ = expansion.PartsCompositionID(st.PackageID, st.Layout, st.Parts, st.PayloadSHA256)
	if _, err := NewClient("/absent.sock").LoadROMLinksPartsComposedCore(context.Background(), "/stage/shell", c.PackageID, paths, "/stage/parts/linked.rbf", st, "/stage/rom/programmed.rbf", links); err != errInvalidRuntimeRequest {
		t.Fatalf("ST layout sent with two ROM sources: %v", err)
	}
	for name, mutate := range map[string]func(*Protocol2Response){
		"missing links": func(r *Protocol2Response) { r.ActivePackage.ROMLinks = nil },
		"single link": func(r *Protocol2Response) {
			r.ActivePackage.ROMLinks = nil
			r.ActivePackage.ROMLink = &single
		},
		"missing parts": func(r *Protocol2Response) { r.ActivePackage.PartsComposition = nil },
		"megacart descriptor": func(r *Protocol2Response) {
			r.ActivePackage.Descriptor.ROMs[1].SourceSize = 131072
			r.ActivePackage.ROMLinks.Sources[1].SourceSize = 131072
		},
		"wrong layout": func(r *Protocol2Response) {
			p := r.ActivePackage.PartsComposition
			p.Layout = expansion.ColecoVideoLayout
			p.ID, _ = expansion.PartsCompositionID(p.PackageID, p.Layout, p.Parts, p.PayloadSHA256)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var bad Protocol2Response
			if err := json.Unmarshal([]byte(line), &bad); err != nil {
				t.Fatal(err)
			}
			mutate(&bad)
			if validProtocol2Response(bad) {
				t.Fatal("invalid paired Coleco receipt accepted")
			}
		})
	}
}
