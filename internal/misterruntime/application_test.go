package misterruntime

import (
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestApplicationDecoderConsumesRuntimeSerializerFixtures(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-application-responses.jsonl")
	if len(lines) != 3 {
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
		if activation.Gamepad != (mode > 0) || protocol.DevelopmentMediaCapable(status) != (mode > 0) || protocol.MediaStreamCapable(status) != (mode == 2) {
			t.Fatalf("mode %d capability projection: %+v", mode, status)
		}
		for _, contract := range status.ActiveInterfaces {
			if contract.ID == "fes.keyboard" {
				t.Fatal("application controller misrepresented as keyboard")
			}
		}
		binding := protocol.DevelopmentMediaBinding{PackageID: p.PackageID, Generation: activation.Generation, Stream: mode == 2}
		if mediaResponseMatches(response, binding) != (mode > 0) {
			t.Fatalf("mode %d media binding mismatch", mode)
		}
		binding.Generation++
		if mediaResponseMatches(response, binding) {
			t.Fatal("stale application media binding accepted")
		}
	}
}
