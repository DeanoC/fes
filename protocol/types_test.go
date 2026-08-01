package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/clawzai2-tech/mister-remote/protocol"
)

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
