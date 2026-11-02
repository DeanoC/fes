package misterruntime

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func initialROMResponse(t *testing.T, kind string) Protocol2Response {
	t.Helper()
	r := writableSTResponse(t, true)
	_, parts, link := stVideoResponse(t)
	r.ActivePackage.Descriptor.Format = 3
	r.ActivePackage.Descriptor.ROM = &corepackage.ROM{ID: link.ROMID, Role: "firmware", File: "rom-map.json", Size: 100, SHA256: link.MapSHA256, SourceSize: link.SourceSize}
	r.ActivePackage.ROMLink = &link
	r.Capabilities.ROMLinking = 1
	if kind == "video" {
		r.ActivePackage.Descriptor.Interfaces = append(r.ActivePackage.Descriptor.Interfaces, corepackage.Interface{ID: expansion.VideoSlot, Major: 1})
		r.ActivePackage.PartsComposition = &parts
	}
	if kind == "slot" {
		c := expansion.SlotComposition{PackageID: r.ActivePackage.PackageID, ShellSHA256: r.ActivePackage.Descriptor.Payload.SHA256, Expansions: []expansion.SlotExpansion{{Slot: 1, ExpansionID: strings.Repeat("c", 64)}}, PayloadSHA256: strings.Repeat("e", 64), PayloadSize: 40408}
		c.ID, _ = expansion.SlotCompositionID(c.PackageID, c.Expansions, c.PayloadSHA256)
		r.ActivePackage.SlotComposition = &c
	}
	if _, err := decodeProtocol2Response([]byte(responseLine(t, r))); err != nil {
		t.Fatal("invalid initial fixture", err)
	}
	return r
}

func TestInitialPcxtLocalRequestAndBinding(t *testing.T) {
	response := func() Protocol2Response {
		r := writableSTResponse(t, true)
		r.ActivePackage.Descriptor.Core.ID = "fes.pcxt-dos"
		r.ActivePackage.Descriptor.ABI.Minor = 1
		r.ActivePackage.Observed.ABI.Minor = 1
		r.Capabilities.ABIs[0].Minor = 1
		*r.Core = "fes.pcxt-dos"
		for i := range r.ActivePackage.Descriptor.Interfaces {
			switch r.ActivePackage.Descriptor.Interfaces[i].ID {
			case protocol.AtariStFloppyInterface().ID:
				r.ActivePackage.Descriptor.Interfaces[i].ID = protocol.PcxtFloppyInterface().ID
			case protocol.AtariStFloppyWriteInterface().ID:
				r.ActivePackage.Descriptor.Interfaces[i].ID = protocol.PcxtFloppyWriteInterface().ID
			}
		}
		for _, list := range []*[]Protocol2Interface{&r.Capabilities.ActiveInterfaces, &r.Capabilities.ABIs[0].Interfaces} {
			for i := range *list {
				switch (*list)[i].ID {
				case protocol.AtariStFloppyInterface().ID:
					(*list)[i].ID = protocol.PcxtFloppyInterface().ID
				case protocol.AtariStFloppyWriteInterface().ID:
					(*list)[i].ID = protocol.PcxtFloppyWriteInterface().ID
				}
			}
			sort.Slice(*list, func(i, j int) bool { return (*list)[i].ID < (*list)[j].ID })
		}
		u := &r.Capabilities.MediaUnits[0]
		u.Interface, u.MinBytes, u.MaxBytes = protocol.PcxtFloppyInterface(), uint32(protocol.PcxtFloppyBytes), uint32(protocol.PcxtFloppyBytes)
		return r
	}
	media := &InitialMediaRequest{Path: "/stage/initial-media.img", Size: 368640, Unit: 0, DataRoot: MediaDataRoot, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
	wrong := initialROMResponse(t, "plain")
	wrong.Capabilities.MediaUnits[0].MinBytes, wrong.Capabilities.MediaUnits[0].MaxBytes = 368640, 368640
	wrong.Capabilities.ActiveInterfaces = append(wrong.Capabilities.ActiveInterfaces, Protocol2Interface{ID: protocol.AtariStFloppyGeometryInterface().ID, Major: 1})
	if initialPcxtRequestMatches(wrong, media) {
		t.Fatal("ST response accepted for PC/XT launch")
	}
	pcxt := response()
	if initialRequestMatches(pcxt, media) {
		t.Fatal("PC/XT response accepted for ST launch")
	}
	badDescriptor := response()
	badDescriptor.ActivePackage.Descriptor.Core.ID = "fes.atari-st"
	if initialPcxtRequestMatches(badDescriptor, media) {
		t.Fatal("wrong descriptor core accepted")
	}
	badDescriptor = response()
	badDescriptor.ActivePackage.Descriptor.Interfaces = []corepackage.Interface{{ID: protocol.PcxtFloppyInterface().ID, Major: 1, Required: true}}
	if initialPcxtRequestMatches(badDescriptor, media) {
		t.Fatal("descriptor without write extension accepted")
	}
	for _, bad := range []bool{false, true} {
		before, reply := response(), response()
		nextGeneration := *before.Generation + 1
		reply.Generation = &nextGeneration
		if _, err := decodeProtocol2Response([]byte(responseLine(t, before))); err != nil {
			t.Fatalf("invalid PC/XT response fixture: %v", err)
		}
		if bad {
			reply.Capabilities.MediaUnits[0].Persistence.GameID = "other-game"
		}
		socket := newSequenceSocketFixture(t, []string{responseLine(t, before) + "\n", responseLine(t, reply) + "\n"})
		_, err := NewClient(socket.path).LoadCoreWithInitialMedia(context.Background(), "/stage/shell", before.ActivePackage.PackageID, media)
		if (err != nil) != bad || (bad && !protocol2MutationAttempted(err)) {
			t.Fatalf("bad=%v error=%v", bad, err)
		}
		requests := socket.wait(t)
		var request struct {
			Operation string              `json:"operation"`
			Initial   InitialMediaRequest `json:"initial_media"`
		}
		if len(requests) != 2 || json.Unmarshal([]byte(requests[1]), &request) != nil || request.Operation != "load_core" || request.Initial != *media {
			t.Fatal("initial load changed", requests)
		}
	}
}
func initialClientCall(ctx context.Context, c *Client, r Protocol2Response, kind string, media *InitialMediaRequest) (Protocol2Response, error) {
	a := r.ActivePackage
	switch kind {
	case "video":
		return c.LoadROMPartsComposedCoreWithInitialMedia(ctx, "/stage/shell", a.PackageID, []PartPath{{Role: "expansion", Path: "/stage/card"}, {Role: "video", Path: "/stage/video"}}, "/stage/overlay.rbf", *a.PartsComposition, "/stage/programmed.rbf", *a.ROMLink, media)
	case "slot":
		return c.LoadROMSlotComposedCoreWithInitialMedia(ctx, "/stage/shell", a.PackageID, []SlotExpansionPath{{Slot: 1, Path: "/stage/card"}}, "/stage/overlay.rbf", *a.SlotComposition, "/stage/programmed.rbf", *a.ROMLink, media)
	default:
		return c.LoadROMLinkedCoreWithInitialMedia(ctx, "/stage/shell", a.PackageID, CoreDataRoot, "", "", nil, "/stage/programmed.rbf", *a.ROMLink, media)
	}
}
func TestInitialSTMediaLocalRequestsAndResultBinding(t *testing.T) {
	for _, kind := range []string{"plain", "slot", "video"} {
		t.Run(kind, func(t *testing.T) {
			r := initialROMResponse(t, kind)
			media := &InitialMediaRequest{Path: "/stage/initial-media.st", Size: 737280, Unit: 0, DataRoot: MediaDataRoot, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
			for _, bad := range []bool{false, true} {
				reply := initialROMResponse(t, kind)
				if bad {
					reply.Capabilities.MediaUnits[0].Persistence.GameID = "other-game"
				}
				socket := newSequenceSocketFixture(t, []string{responseLine(t, r) + "\n", responseLine(t, reply) + "\n"})
				_, err := initialClientCall(context.Background(), NewClient(socket.path), r, kind, media)
				if (err != nil) != bad || (bad && !protocol2MutationAttempted(err)) {
					t.Fatalf("bad=%v error=%v", bad, err)
				}
				requests := socket.wait(t)
				var request map[string]json.RawMessage
				if len(requests) != 2 || json.Unmarshal([]byte(requests[1]), &request) != nil {
					t.Fatal(requests)
				}
				var got InitialMediaRequest
				if json.Unmarshal(request["initial_media"], &got) != nil || got != *media {
					t.Fatal("initial disk request changed", requests)
				}
				if len(request["initial_media"]) == 0 || len(request["rom_link"]) == 0 {
					t.Fatal("missing atomic inputs")
				}
			}
			for name, change := range map[string]func(*InitialMediaRequest){"path": func(m *InitialMediaRequest) { m.Path = "relative" }, "size": func(m *InitialMediaRequest) { m.Size-- }, "unit": func(m *InitialMediaRequest) { m.Unit = 1 }, "root": func(m *InitialMediaRequest) { m.DataRoot = "/tmp/data" }, "game": func(m *InitialMediaRequest) { m.GameID = "../escape" }, "base": func(m *InitialMediaRequest) { m.BaseMediaID = "absent" }} {
				t.Run(name, func(t *testing.T) {
					bad := *media
					change(&bad)
					_, err := initialClientCall(context.Background(), NewClient("/must-not-connect"), r, kind, &bad)
					if err != errInvalidRuntimeRequest {
						t.Fatal("mutated invalid initial request", err)
					}
				})
			}
		})
	}
}

func TestInitialSTMediaLaunchMatchAndMutableAdoption(t *testing.T) {
	requested := &corepackage.StagedInitialMedia{GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
	if !initialMediaMatches(initialROMResponse(t, "video"), requested) {
		t.Fatal("first save absent revision must remain valid")
	}
	for name, change := range map[string]func(*Protocol2Response){
		"game": func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].Persistence.GameID = "other-game" },
		"base": func(r *Protocol2Response) {
			r.Capabilities.MediaUnits[0].Persistence.BaseMediaID = strings.Repeat("c", 64)
		},
		"empty": func(r *Protocol2Response) {
			r.Capabilities.MediaUnits[0].State = "empty"
			r.Capabilities.MediaUnits[0].Persistence = nil
			r.ActivePackage.PersistenceMode = "volatile"
		},
		"failed":   func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].State = "failed" },
		"volatile": func(r *Protocol2Response) { r.ActivePackage.PersistenceMode = "volatile" },
		"error": func(r *Protocol2Response) {
			r.Error = &Protocol2Error{Code: "save_failed", Phase: "save", Message: "capture failed"}
		},
		"readonly": func(r *Protocol2Response) { r.Capabilities.ActiveInterfaces = nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := initialROMResponse(t, "video")
			change(&r)
			if initialMediaMatches(r, requested) {
				t.Fatal("confirmed different/unready disk")
			}
		})
	}
	for _, ejected := range []bool{false, true} {
		r := initialROMResponse(t, "video")
		a := r.ActivePackage
		staged := []corepackage.Staged{{PackageID: a.PackageID, Descriptor: a.Descriptor, PartsComposition: a.PartsComposition, ROMLink: a.ROMLink, InitialMedia: requested}}
		if ejected {
			r.Capabilities.MediaUnits[0].State = "empty"
			r.Capabilities.MediaUnits[0].Persistence = nil
			a.PersistenceMode = "volatile"
		} else {
			r.Capabilities.MediaUnits[0].Persistence.GameID = "replacement-game"
			r.Capabilities.MediaUnits[0].Persistence.BaseMediaID = strings.Repeat("d", 64)
		}
		if !validProtocol2Response(r) || matchingAdoptedPackage(staged, *a) != 0 {
			t.Fatal("valid live media mutation prevented restart adoption", ejected)
		}
		if initialMediaMatches(r, requested) {
			t.Fatal("mutable adoption accidentally satisfies original launch")
		}
		if !reflect.DeepEqual(staged[0].InitialMedia, requested) {
			t.Fatal("source companion changed")
		}
	}
}
