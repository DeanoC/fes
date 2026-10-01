package protocol

import (
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
}
