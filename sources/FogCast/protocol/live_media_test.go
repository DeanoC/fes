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
		{"game.o", false},
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
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"dragons-add.o", true},
		{"Dragons.O", true},
		{"game.p", false},
		{"game.O.p", false},
		{"dir/dragons.o", false},
		{"", false},
	} {
		if AdmitZx80TapeMediaName(tc.name) != tc.ok {
			t.Fatalf("zx80 %q ok=%v", tc.name, AdmitZx80TapeMediaName(tc.name))
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

func TestLiveMediaCapableAdmitsComputerZx81Tape(t *testing.T) {
	tape := Zx81TapeInterface()
	active := []RuntimeInterface{{ID: tape.ID, Major: tape.Major, Minor: tape.Minor}}
	computer := &CorePackageStatus{ABI: RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: active}
	if !LiveMediaCapable(computer) || !ComputerZx81TapeCapable(computer) {
		t.Fatal("fes.computer zx81 tape should be live-media capable")
	}
	blob := &CorePackageStatus{ABI: RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}
	if ComputerZx81TapeCapable(blob) {
		t.Fatal("legacy blob is not the computer tape unit")
	}
	if LiveMediaCapable(&CorePackageStatus{ABI: RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}) {
		t.Fatal("computer blob is not the ZX81 tape unit")
	}
	if LiveMediaCapable(&CorePackageStatus{ABI: RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []RuntimeInterface{{ID: SpectrumTapeInterface().ID, Major: 2}}}) {
		t.Fatal("spectrum tape must not open the ZX81 tape picker")
	}
	if LiveMediaCapable(&CorePackageStatus{ABI: RuntimeContract{ID: "fes.computer", Major: 1, Minor: 2}, ActiveInterfaces: active}) {
		t.Fatal("a later computer minor is a different contract")
	}
	later := []RuntimeInterface{{ID: tape.ID, Major: tape.Major, Minor: tape.Minor + 1}}
	if LiveMediaCapable(&CorePackageStatus{ABI: RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: later}) {
		t.Fatal("a later tape interface is not this unit")
	}
	id := strings.Repeat("a", 64)
	b := DevelopmentMediaBinding{PackageID: id, Generation: 3}
	status := Status{State: StateActive, Development: true, CorePackage: &CorePackageStatus{
		PackageID: id, Generation: 3, ABI: RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: active,
	}}
	if b.MatchesLive(status) {
		t.Fatal("computer tape insertion must stay off the blob mailbox")
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
