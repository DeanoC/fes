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
	for _, wire := range []string{`{}`, `{"visible":false,"focus":"launcher"}`, `{"visible":true,"focus":"menu"}`} {
		var request SessionDisplayRequest
		if err := json.Unmarshal([]byte(wire), &request); err != nil {
			t.Fatal(err)
		}
		if _, ok := request.Display(); ok {
			t.Fatalf("%s admitted", wire)
		}
	}
}
