package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionDisplayRequiresObservedCapabilityAndExactLiveBinding(t *testing.T) {
	b := DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}
	p := &CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation,
		ABI:              RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []RuntimeInterface{{ID: "fes.media.blob", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}
	s := Status{State: StateActive, Development: true, CorePackage: p}
	if !b.MatchesSessionDisplay(s) {
		t.Fatal("observed active session rejected")
	}
	p.Generation++
	if b.MatchesSessionDisplay(s) {
		t.Fatal("replacement generation admitted")
	}
	p.Generation--
	p.ActiveInterfaces = p.ActiveInterfaces[:2]
	if SessionDisplayCapable(p) || b.MatchesSessionDisplay(s) {
		t.Fatal("DDR alone granted launcher display")
	}
	p.ActiveInterfaces = append(p.ActiveInterfaces, RuntimeInterface{ID: "fes.video.session-display", Major: 1, Minor: 1})
	if SessionDisplayCapable(p) {
		t.Fatal("unknown display version admitted")
	}
	tape := Zx81TapeInterface()
	computer := &CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation,
		ABI: RuntimeContract{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []RuntimeInterface{
			{ID: tape.ID, Major: tape.Major, Minor: tape.Minor},
			{ID: "fes.memory.hps-ddr", Major: 1},
			{ID: "fes.video.session-display", Major: 1},
		}}
	cs := Status{State: StateActive, Development: true, CorePackage: computer}
	if !SessionDisplayCapable(computer) || !b.MatchesSessionDisplay(cs) {
		t.Fatal("fes.computer ZX81 plane was not admitted")
	}
	computer.ActiveInterfaces = computer.ActiveInterfaces[:1]
	if SessionDisplayCapable(computer) || b.MatchesSessionDisplay(cs) {
		t.Fatal("tape unit alone granted launcher display")
	}
}

// Hosts and kits ship separately. A request names focus only for a surround,
// so a launcher open or a close reads the same to a peer that predates focus,
// and a request without focus means what visibility alone used to mean.
func TestSessionDisplayFocusDefaultsFromVisibility(t *testing.T) {
	for _, c := range []struct {
		display SessionDisplay
		wire    string
	}{
		{SessionDisplayFocused, `{"visible":true}`},
		{SessionDisplayClosed, `{"visible":false}`},
		{SessionDisplayShown, `{"visible":true,"focus":"machine"}`},
	} {
		raw, err := json.Marshal(NewSessionDisplayRequest(c.display))
		if err != nil || string(raw) != c.wire {
			t.Fatalf("%+v encoded %s, want %s", c.display, raw, c.wire)
		}
		var request SessionDisplayRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Fatal(err)
		}
		if got, ok := request.Display(); !ok || got != c.display {
			t.Fatalf("%s decoded %+v %v", raw, got, ok)
		}
	}
	// Only an omitted focus takes the default. A null or empty focus names no
	// owner, so it is refused like an unknown one.
	for _, wire := range []string{`{}`, `{"visible":false,"focus":"launcher"}`, `{"visible":true,"focus":"menu"}`,
		`{"visible":true,"focus":null}`, `{"visible":true,"focus":""}`, `{"visible":false,"focus":null}`, `{"visible":false,"focus":""}`} {
		var request SessionDisplayRequest
		if json.Unmarshal([]byte(wire), &request) != nil {
			continue
		}
		if _, ok := request.Display(); ok {
			t.Fatalf("%s admitted", wire)
		}
	}
}

func TestSessionDisplayAdmitsOnlyObservedSupportedComputerMedia(t *testing.T) {
	for _, tc := range []struct {
		contract           RuntimeContract
		minBytes, maxBytes uint32
	}{
		{Apple2FloppyInterface(), 143360, 143360},
		{C64DiskInterface(), 174848, 174848},
		{AtariStFloppyInterface(), 737280, 737280},
		{SpectrumTapeInterface(), uint32(SpectrumTapeMinBytes), uint32(SpectrumTapeMaxBytes)},
	} {
		t.Run(tc.contract.ID, func(t *testing.T) {
			b := DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}
			p := &CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation, ABI: RuntimeContract{ID: "fes.computer", Major: 1},
				ActiveInterfaces: []RuntimeInterface{{ID: tc.contract.ID, Major: tc.contract.Major, Minor: tc.contract.Minor}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}},
				MediaUnits:       []MediaUnitStatus{{Unit: 0, Interface: tc.contract, MinBytes: tc.minBytes, MaxBytes: tc.maxBytes, ChunkBytes: 512, State: MediaUnitEmpty}}}
			s := Status{State: StateActive, Development: true, CorePackage: p}
			if !b.MatchesSessionDisplay(s) {
				t.Fatal("supported observed unit rejected")
			}
			p.Generation++
			if b.MatchesSessionDisplay(s) {
				t.Fatal("replacement generation admitted")
			}
			p.Generation--
			for _, mutate := range []func(*CorePackageStatus){
				func(p *CorePackageStatus) { p.MediaUnits = nil },
				func(p *CorePackageStatus) { p.MediaUnits[0].ChunkBytes = 0 },
				func(p *CorePackageStatus) { p.ActiveInterfaces = p.ActiveInterfaces[1:] },
				func(p *CorePackageStatus) { p.ActiveInterfaces = p.ActiveInterfaces[:2] },
				func(p *CorePackageStatus) { p.ActiveInterfaces[1].Minor = 1 },
				func(p *CorePackageStatus) {
					p.MediaUnits[0].Interface = RuntimeContract{ID: "fes.media.unknown", Major: 1}
					p.ActiveInterfaces[0] = RuntimeInterface{ID: "fes.media.unknown", Major: 1}
				},
			} {
				copy := *p
				copy.MediaUnits = CloneMediaUnits(p.MediaUnits)
				copy.ActiveInterfaces = append([]RuntimeInterface(nil), p.ActiveInterfaces...)
				mutate(&copy)
				s.CorePackage = &copy
				if b.MatchesSessionDisplay(s) {
					t.Fatalf("incomplete or unsupported media/display identity admitted: %+v", copy)
				}
			}
		})
	}
}

// A running application machine, such as the Coleco native shell, has no
// removable media. Its observed plane and DDR are the whole admission.
func TestSessionDisplayAdmitsAnApplicationMachineWithoutMedia(t *testing.T) {
	b := DevelopmentMediaBinding{PackageID: strings.Repeat("c", 64), Generation: 4}
	p := &CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation, ABI: RuntimeContract{ID: "fes.application", Major: 1},
		ActiveInterfaces: []RuntimeInterface{{ID: "fes.gamepad.ports", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}
	s := Status{State: StateActive, Development: true, CorePackage: p}
	if !SessionDisplayCapable(p) || !b.MatchesSessionDisplay(s) {
		t.Fatal("application machine with an observed plane rejected")
	}
	for name, mutate := range map[string]func(*CorePackageStatus){
		"no plane":          func(p *CorePackageStatus) { p.ActiveInterfaces = p.ActiveInterfaces[:2] },
		"no DDR":            func(p *CorePackageStatus) { p.ActiveInterfaces = append(p.ActiveInterfaces[:1], p.ActiveInterfaces[2]) },
		"newer plane minor": func(p *CorePackageStatus) { p.ActiveInterfaces[2].Minor = 1 },
		"other ABI":         func(p *CorePackageStatus) { p.ABI = RuntimeContract{ID: "fes.simple-game", Major: 1} },
		"replaced":          func(p *CorePackageStatus) { p.Generation++ },
	} {
		copy := *p
		copy.ActiveInterfaces = append([]RuntimeInterface(nil), p.ActiveInterfaces...)
		mutate(&copy)
		s.CorePackage = &copy
		if b.MatchesSessionDisplay(s) {
			t.Fatalf("%s admitted", name)
		}
	}
}
