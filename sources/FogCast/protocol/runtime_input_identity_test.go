package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeInputIdentityRequiresCompleteTypedBinding(t *testing.T) {
	instance := strings.Repeat("a", 32)
	packageID := strings.Repeat("b", 64)
	valid := []string{
		`{"kind":"development","runtime_instance":"` + instance + `","package_id":"","generation":1}`,
		`{"kind":"described","runtime_instance":"` + instance + `","package_id":"` + packageID + `","generation":1}`,
	}
	for _, raw := range valid {
		var got RuntimeInputIdentity
		if err := json.Unmarshal([]byte(raw), &got); err != nil || got.Validate() != nil {
			t.Fatalf("valid identity: %v", err)
		}
		encoded, err := json.Marshal(got)
		if err != nil || !strings.Contains(string(encoded), `"package_id":`) {
			t.Fatalf("package identity omitted: %s %v", encoded, err)
		}
	}
	invalid := []string{
		`{"kind":"development","runtime_instance":"` + instance + `","generation":1}`,
		`{"kind":"development","runtime_instance":"` + instance + `","package_id":null,"generation":1}`,
		`{"kind":"development","runtime_instance":"` + instance + `","package_id":"","generation":0}`,
		`{"kind":"development","runtime_instance":"` + strings.Repeat("0", 32) + `","package_id":"","generation":1}`,
		`{"kind":"development","runtime_instance":"` + strings.Repeat("A", 32) + `","package_id":"","generation":1}`,
		`{"kind":"development","runtime_instance":"` + instance + `","package_id":"` + packageID + `","generation":1}`,
		`{"kind":"described","runtime_instance":"` + instance + `","package_id":"","generation":1}`,
		`{"kind":"development","kind":"development","runtime_instance":"` + instance + `","package_id":"","generation":1}`,
		`{"kind":"development","runtime_instance":"` + instance + `","package_id":"","generation":1,"extra":true}`,
	}
	for _, raw := range invalid {
		var got RuntimeInputIdentity
		if err := json.Unmarshal([]byte(raw), &got); err == nil {
			t.Fatalf("accepted invalid identity %s", raw)
		}
	}
}
