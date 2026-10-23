package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func removableSession(contract protocol.RuntimeContract, min, max uint32) SessionResult {
	return SessionResult{ID: "play-a", State: "active", Target: "kit-a", CorePackage: &SessionCorePackage{
		PackageID: strings.Repeat("a", 64), Generation: 9, ABI: SessionCoreABI{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []SessionCoreInterface{{ID: contract.ID, Major: contract.Major, Minor: contract.Minor}},
		MediaUnits:       []protocol.MediaUnitStatus{{Unit: 0, Interface: contract, MinBytes: min, MaxBytes: max, ChunkBytes: 512, State: "empty"}},
	}}
}

func TestRemovableMediaUsesObservedFormatAndBounds(t *testing.T) {
	cases := []struct {
		contract    protocol.RuntimeContract
		min, max    uint32
		name, wrong string
	}{
		{protocol.Zx81TapeInterface(), 1, 16384, "game.p", "game.tap"},
		{protocol.SpectrumTapeInterface(), 1, 65536, "game.tap", "game.p"},
		{protocol.Apple2FloppyInterface(), 143360, 143360, "game.do", "game.d64"},
		{protocol.C64DiskInterface(), 174848, 174848, "game.d64", "game.st"},
		{protocol.AtariStFloppyInterface(), 737280, 737280, "game.st", "game.dsk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := removableSession(tc.contract, tc.min, tc.max).CorePackage
			if !RemovableMediaCapable(p) || !AdmitRemovableMedia(p, tc.name, int64(tc.min)) {
				t.Fatal("valid observed unit refused")
			}
			if AdmitRemovableMedia(p, tc.wrong, int64(tc.min)) || AdmitRemovableMedia(p, tc.name, int64(tc.max)+1) {
				t.Fatal("wrong media admitted")
			}
			p.ActiveInterfaces = nil
			if RemovableMediaCapable(p) {
				t.Fatal("unobserved interface admitted")
			}
		})
	}
}

func TestRemovableMediaKeepsCapturedSessionAndRejectsReplacementReply(t *testing.T) {
	prior := removableSession(protocol.C64DiskInterface(), 174848, 174848)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		b, ok := protocol.DevelopmentMediaHeaders(r.Header)
		if !ok || b.PackageID != prior.CorePackage.PackageID || b.Generation != 9 || b.Target != "kit-a" {
			t.Errorf("binding %+v", b)
		}
		result := prior
		if calls == 2 {
			result.ID = "replacement"
		}
		json.NewEncoder(w).Encode(map[string]any{"id": result.ID, "state": result.State, "target": result.Target, "target_id": result.TargetID, "core_package": result.CorePackage})
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	if _, err := client.ReplaceRemovableMediaForSession(context.Background(), prior, strings.Repeat("b", 64), "disk.d64"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ClearRemovableMediaForSession(context.Background(), prior); err == nil {
		t.Fatal("replacement reply accepted")
	}
	if calls != 2 {
		t.Fatalf("mutation replayed: %d calls", calls)
	}
}
