package protocol

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func TestApplicationMediaComposition(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		blob, stream, optional bool
		major, minor           int64
		want                   int64
	}{
		{name: "video only", major: 1},
		{name: "blob", blob: true, major: 1, want: 16384},
		{name: "stream", blob: true, stream: true, major: 1, want: 32768},
		{name: "stream without blob", stream: true, major: 1},
		{name: "optional blob", blob: true, optional: true, major: 1},
		{name: "future major", blob: true, major: 2},
		{name: "future minor", blob: true, major: 1, minor: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := corepackage.Descriptor{Core: corepackage.Core{ID: "example.palette"},
				ABI: corepackage.Contract{ID: "fes.application", Major: tc.major, Minor: tc.minor},
				Interfaces: []corepackage.Interface{{ID: "fes.video.fixed-720p60", Major: 1, Required: true},
					{ID: "fes.gamepad", Major: 1, Required: true}}}
			if tc.blob {
				d.Interfaces = append(d.Interfaces, corepackage.Interface{ID: "fes.media.blob", Major: 1, Required: !tc.optional})
			}
			if tc.stream {
				d.Interfaces = append(d.Interfaces, corepackage.Interface{ID: "fes.media.blob-stream", Major: 1, Required: true})
			}
			caps := DeclaredCoreMediaCapabilities(d)
			if tc.want == 0 {
				if len(caps) != 0 || RequiresCoreMedia(d) {
					t.Fatalf("unsupported composition: %+v", caps)
				}
				return
			}
			transport := "fes-application-mailbox-v1"
			if tc.stream {
				transport = "fes-application-mailbox-stream-v1"
			}
			if len(caps) != 1 || caps[0].MaxBytes != tc.want || caps[0].Transport != transport || !RequiresCoreMedia(d) {
				t.Fatalf("capabilities=%+v required=%v", caps, RequiresCoreMedia(d))
			}
		})
	}
}

func TestApplicationMediaBindingRequiresActiveInterfaceAndExactGeneration(t *testing.T) {
	binding := DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 7}
	s := Status{State: StateActive, Development: true, CorePackage: &CorePackageStatus{
		PackageID: binding.PackageID, Generation: binding.Generation,
		ABI:              RuntimeContract{ID: "fes.application", Major: 1},
		ActiveInterfaces: []RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}
	if !binding.Matches(s) || !binding.AcceptsSize(s, 16384) || binding.AcceptsSize(s, 16385) {
		t.Fatal("valid application blob binding rejected or oversized blob accepted")
	}
	s.CorePackage.Generation++
	if binding.Matches(s) {
		t.Fatal("stale generation accepted")
	}
	s.CorePackage.Generation--
	s.CorePackage.ActiveInterfaces = nil
	if binding.Matches(s) {
		t.Fatal("video-only application accepted media")
	}
}
