package protocol

import (
	"strings"
	"testing"
)

func TestAdmitTapeMediaName(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"game.p", true},
		{"Game.P", true},
		{"game.P", true},
		{"game.tzx", false},
		{"game.tap", false},
		{".p", true},
		{"", false},
		{"dir/game.p", false},
		{`dir\game.p`, false},
		{"game.p.bin", false},
	} {
		if AdmitTapeMediaName(tc.name) != tc.ok {
			t.Fatalf("%q ok=%v", tc.name, AdmitTapeMediaName(tc.name))
		}
	}
}

func TestLiveMediaCapableRequiresSimpleComputerBlob(t *testing.T) {
	blob := []RuntimeInterface{{ID: "fes.media.blob", Major: 1}}
	ok := &CorePackageStatus{ABI: RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: blob}
	if !LiveMediaCapable(ok) {
		t.Fatal("simple-computer blob should be live-media capable")
	}
	app := &CorePackageStatus{ABI: RuntimeContract{ID: "fes.application", Major: 1}, ActiveInterfaces: blob}
	if LiveMediaCapable(app) {
		t.Fatal("application must not use replace_live_media")
	}
	if LiveMediaCapable(&CorePackageStatus{ABI: RuntimeContract{ID: "fes.simple-computer", Major: 1}}) {
		t.Fatal("missing blob interface")
	}
}

func TestDevelopmentMediaBindingMatchesLive(t *testing.T) {
	id := strings.Repeat("a", 64)
	b := DevelopmentMediaBinding{PackageID: id, Generation: 3}
	status := Status{State: StateActive, Development: true, CorePackage: &CorePackageStatus{
		PackageID: id, Generation: 3, ABI: RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}
	if !b.MatchesLive(status) {
		t.Fatal("expected live match")
	}
	status.CorePackage.Generation = 4
	if b.MatchesLive(status) {
		t.Fatal("generation mismatch must fail closed")
	}
}
