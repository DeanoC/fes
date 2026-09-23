package misterruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func TestAdoptionDistinguishesROMsOnSamePackage(t *testing.T) {
	first := &corepackage.ROMLinkIdentity{ROMID: "machine", SourceSHA256: strings.Repeat("a", 64), ProgrammedSHA256: strings.Repeat("b", 64)}
	second := *first
	second.SourceSHA256 = strings.Repeat("c", 64)
	staged := []corepackage.Staged{{PackageID: "same", ROMLink: first}, {PackageID: "same", ROMLink: &second}}
	if got := matchingAdoptedPackage(staged, Protocol2ActivePackage{PackageID: "same", ROMLink: &second}); got != 1 {
		t.Fatalf("adopted ROM %d, want second", got)
	}
	if got := matchingAdoptedPackage(staged, Protocol2ActivePackage{PackageID: "same"}); got != -1 {
		t.Fatal("adopted without ROM identity")
	}
}

func TestROMLinkClientRequiresCapabilityBeforeMutation(t *testing.T) {
	fixture := newSequenceSocketFixture(t, []string{fixtureLines(t, "protocol-v2.jsonl")[1] + "\n"})
	identity := corepackage.ROMLinkIdentity{ROMID: "machine", MapSHA256: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64), SourceSize: 8192, ProgrammedSHA256: strings.Repeat("c", 64), ProgrammedSize: 2000}
	if _, err := NewClient(fixture.path).LoadROMLinkedCore(context.Background(), "/tmp/package", fixturePackageID, "", "", "", nil, "/tmp/programmed.rbf", identity); err == nil {
		t.Fatal("ROM load accepted without negotiated capability")
	}
	requests := fixture.wait(t)
	if len(requests) != 1 || requests[0] != `{"protocol":2,"operation":"status"}` {
		t.Fatalf("mutated without capability: %v", requests)
	}
}

func TestROMLinkProtocolRoundTrip(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-rom-package-responses.jsonl")
	active, err := decodeProtocol2Response([]byte(lines[1]))
	if err != nil {
		t.Fatal(err)
	}
	if active.ActivePackage == nil || active.ActivePackage.ROMLink == nil {
		t.Fatal("missing runtime ROM identity")
	}
	identity := *active.ActivePackage.ROMLink
	for _, root := range []string{"", "/tmp/data"} {
		fixture := newSequenceSocketFixture(t, []string{lines[1] + "\n", lines[1] + "\n"})
		response, err := NewClient(fixture.path).LoadROMLinkedCore(context.Background(), "/tmp/package", active.ActivePackage.PackageID, root, "", "", nil, "/tmp/programmed.rbf", identity)
		if err != nil || response.ActivePackage == nil || *response.ActivePackage.ROMLink != identity {
			t.Fatalf("roundtrip: %+v %v", response, err)
		}
		requests := fixture.wait(t)
		var request map[string]json.RawMessage
		if err := json.Unmarshal([]byte(requests[1]), &request); err != nil {
			t.Fatal(err)
		}
		var got corepackage.ROMLinkIdentity
		if err := json.Unmarshal(request["rom_link"], &got); err != nil || got != identity {
			t.Fatalf("identity changed: %+v %v", got, err)
		}
		var op string
		_ = json.Unmarshal(request["operation"], &op)
		want := "load_rom_core"
		if root != "" {
			want = "load_rom_library_core"
		}
		if op != want {
			t.Fatalf("operation %s, want %s", op, want)
		}
	}
	changed := active
	copyActive := *active.ActivePackage
	copyROM := identity
	copyROM.SourceSHA256 = strings.Repeat("f", 64)
	copyActive.ROMLink = &copyROM
	changed.ActivePackage = &copyActive
	if sameProtocol2RuntimeState(active, changed) {
		t.Fatal("different ROM reported as unchanged state")
	}
	wrong, _ := json.Marshal(changed)
	fixture := newSequenceSocketFixture(t, []string{lines[1] + "\n", string(wrong) + "\n"})
	_, err = NewClient(fixture.path).LoadROMLinkedCore(context.Background(), "/tmp/package", active.ActivePackage.PackageID, "", "", "", nil, "/tmp/programmed.rbf", identity)
	if err == nil {
		t.Fatal("accepted wrong ROM response")
	}
	fixture.wait(t)
}

func TestROMLinkStatusRejectsMalformedIdentity(t *testing.T) {
	line := fixtureLines(t, "protocol-v2-rom-package-responses.jsonl")[1]
	for _, mutation := range []func(map[string]any){
		func(v map[string]any) { delete(v["active_package"].(map[string]any), "rom_link") },
		func(v map[string]any) { v["active_package"].(map[string]any)["rom_link"] = nil },
		func(v map[string]any) { v["capabilities"].(map[string]any)["rom_linking"] = float64(0) },
		func(v map[string]any) {
			v["active_package"].(map[string]any)["rom_link"].(map[string]any)["source_size"] = "1024"
		},
		func(v map[string]any) {
			v["active_package"].(map[string]any)["rom_link"].(map[string]any)["map_sha256"] = strings.Repeat("0", 64)
		},
		func(v map[string]any) {
			v["active_package"].(map[string]any)["rom_link"].(map[string]any)["source_size"] = float64(2048)
		},
		func(v map[string]any) {
			v["active_package"].(map[string]any)["rom_link"].(map[string]any)["unsealed_map"] = "/tmp/map"
		},
	} {
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			t.Fatal(err)
		}
		mutation(object)
		data, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = decodeProtocol2Response(data); err == nil {
			t.Fatalf("accepted malformed ROM identity: %s", data)
		}
	}
}
