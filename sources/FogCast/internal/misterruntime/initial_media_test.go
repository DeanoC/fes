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
