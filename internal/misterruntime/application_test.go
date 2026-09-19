package misterruntime

import (
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestApplicationDecoderConsumesRuntimeSerializerFixtures(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-application-responses.jsonl")
	if len(lines) != 4 {
		t.Fatalf("application fixture rows=%d", len(lines))
	}
	for mode, line := range lines {
		response, err := decodeProtocol2Response([]byte(line))
		if err != nil {
			t.Fatalf("mode %d: %v", mode, err)
		}
		p := response.ActivePackage
		activation := activationFromProtocol2(p.PackageID, p.Descriptor, response)
		status := corePackageStatus(activation)
		media := mode == 1 || mode == 2
		if activation.Gamepad != (mode > 0) || protocol.DevelopmentMediaCapable(status) != media || protocol.MediaStreamCapable(status) != (mode == 2) {
			t.Fatalf("mode %d capability projection: %+v", mode, status)
		}
		audio := false
		for _, contract := range status.ActiveInterfaces {
			if contract.ID == "fes.audio.pcm-s16-stereo-48k" {
				audio = true
			}
			if contract.ID == "fes.keyboard" {
				t.Fatal("application controller misrepresented as keyboard")
			}
		}
		if audio != (mode == 3) {
			t.Fatalf("mode %d audio interface propagation: %+v", mode, status)
		}
		binding := protocol.DevelopmentMediaBinding{PackageID: p.PackageID, Generation: activation.Generation, Stream: mode == 2}
		if mediaResponseMatches(response, binding) != media {
			t.Fatalf("mode %d media binding mismatch", mode)
		}
		binding.Generation++
		if mediaResponseMatches(response, binding) {
			t.Fatal("stale application media binding accepted")
		}
	}
}
