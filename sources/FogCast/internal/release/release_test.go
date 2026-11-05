package release

import (
	"encoding/json"
	"os"
	"testing"
)

// docs/support-matrix.json records each package's release scope. It must
// agree with the table tenfoot filters by.
func TestScopesMatchTheSupportMatrix(t *testing.T) {
	data, err := os.ReadFile("../../../../docs/support-matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	var matrix struct {
		Cores []struct {
			ID      string `json:"id"`
			Release *Scope `json:"release"`
		} `json:"cores"`
	}
	if err := json.Unmarshal(data, &matrix); err != nil {
		t.Fatal(err)
	}
	rows := map[string]bool{}
	for _, row := range matrix.Cores {
		if row.Release == nil {
			continue
		}
		rows[row.ID] = true
		if got := Of(row.ID); got != *row.Release {
			t.Errorf("%s: matrix release %q, table %q", row.ID, *row.Release, got)
		}
	}
	for id := range scopes {
		if !rows[id] {
			t.Errorf("%s has a release scope but no matrix release", id)
		}
	}
}

func TestShown(t *testing.T) {
	for _, tc := range []struct {
		id   string
		all  bool
		want bool
	}{
		{"fes.atari-st", false, true},
		{"fes.pc386", false, false},
		{"fes.pong", false, false},
		{"fes.unknown", false, false},
		{"fes.pong", true, true},
	} {
		if got := Shown(tc.id, tc.all); got != tc.want {
			t.Errorf("Shown(%q, %v) = %v", tc.id, tc.all, got)
		}
	}
}
