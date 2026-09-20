package hostclient

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSharedLaunchEligibility(t *testing.T) {
	data, err := os.ReadFile("testdata/launch-eligibility.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string
		Game  Game
		Block LaunchBlock
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty shared fixture")
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := tc.Game.LaunchBlock(); got != tc.Block {
				t.Fatalf("block = %q, want %q", got, tc.Block)
			}
			if got := tc.Game.LaunchEligible(); got != (tc.Block == "") {
				t.Fatalf("eligible = %v, block %q", got, tc.Block)
			}
		})
	}
}
