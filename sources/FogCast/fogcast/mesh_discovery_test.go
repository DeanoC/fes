package fogcast

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/discovery"
)

func TestHostParsesPhase0OmissionAndIgnoresForeignExecuteForReady(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	phase0 := discovery.ParseTXT(map[string]string{"protocol": "1", "target_id": id})
	if !phase0.DirectBindable() || phase0.Mesh != nil || phase0.NodeID != id {
		t.Fatalf("phase 0 bind = %#v", phase0)
	}
	target := TargetConfig{Name: "kit", TargetID: id}
	if target.NodeID() != target.TargetID {
		t.Fatalf("node id %q target %q", target.NodeID(), target.TargetID)
	}
	text, err := discovery.EncodeTXT(id, discovery.Capabilities{
		Execute:     []discovery.Execute{{Kind: discovery.ExecuteFPGANative, ABIs: []discovery.ABI{{ID: "fes.application", Major: 1}}}},
		DisplaySink: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign := discovery.ParseTXT(txtPairs(text))
	if !foreign.Capabilities.DisplaySink || foreign.PictureUp() {
		t.Fatalf("display sink was treated as picture-up: %#v picture %v", foreign.Capabilities, foreign.PictureUp())
	}
	if ready, _ := discovery.ReadyForBoundExecutor(false, foreign, nil); ready {
		t.Fatal("another node's Execute advertisement flipped Ready")
	}
	if ready, _ := discovery.ReadyForBoundExecutor(true, foreign, nil); !ready {
		t.Fatal("bound composition lost Ready")
	}
	if foreign.SilenceReleasesLease() || (foreign.TTLSeconds != nil) {
		t.Fatal("host reader armed a lease clock from an advertisement")
	}
}

func txtPairs(text []string) map[string]string {
	out := make(map[string]string, len(text))
	for _, item := range text {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
