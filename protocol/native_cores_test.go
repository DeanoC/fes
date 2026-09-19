package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeCoreAvailabilityContract(t *testing.T) {
	for _, tc := range []struct {
		body           string
		present, valid bool
	}{
		{`{"api_version":"v1"}`, false, false},
		{`{"native_cores":{"version":1,"systems":[]}}`, true, true},
		{`{"native_cores":{"version":1,"systems":["pong","nes"]}}`, true, true},
		{`{"native_cores":{"version":2,"systems":["pong"]}}`, true, false},
		{`{"native_cores":{"version":1,"systems":null}}`, true, false},
		{`{"native_cores":{"version":1,"systems":["unknown"]}}`, true, false},
		{`{"native_cores":{"version":1,"systems":["pong","pong"]}}`, true, false},
	} {
		var health Health
		if err := json.Unmarshal([]byte(tc.body), &health); err != nil {
			t.Fatal(err)
		}
		if (health.NativeCores != nil) != tc.present || health.NativeCores.Valid() != tc.valid {
			t.Fatalf("unexpected contract: %s", tc.body)
		}
	}
	bytes, err := json.Marshal(Health{NativeCores: &NativeCoreAvailability{Version: 1, Systems: []System{}}})
	if err != nil || !strings.Contains(string(bytes), `"systems":[]`) {
		t.Fatalf("empty set lost: %s %v", bytes, err)
	}
}
