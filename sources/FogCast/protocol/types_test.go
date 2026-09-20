package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestHealthJSONOmitsEmptyArtifacts(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(protocol.Health{APIVersion: "v1", AgentVersion: "0.1.0", Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, "artifacts") {
		t.Fatalf("empty artifacts present: %s", got)
	}
}

func TestHealthJSONIncludesArtifacts(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(protocol.Health{
		APIVersion: "v1", AgentVersion: "0.1.0", Ready: true,
		Artifacts: &protocol.Artifacts{RuntimeCommit: strings.Repeat("1", 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"artifacts"`) || !strings.Contains(string(b), `"runtime_commit"`) {
		t.Fatalf("artifacts missing: %s", b)
	}
}

func TestIdleStatusJSONIncludesNullFields(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(protocol.Status{State: protocol.StateIdle})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, field := range []string{`"game_id":null`, `"system":null`, `"expected_core":null`, `"observed_core":null`, `"last_error":null`} {
		if !strings.Contains(got, field) {
			t.Fatalf("status JSON %s does not contain %s", got, field)
		}
	}
}
