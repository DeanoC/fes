package shared

import (
	"strings"
	"testing"
)

func TestPrefetchOrderDedupesAndKeepsPriority(t *testing.T) {
	t.Parallel()
	focus := strings.Repeat("aa", 32)
	page := strings.Repeat("bb", 32)
	next := strings.Repeat("cc", 32)
	got := PrefetchOrder([]string{focus, page}, []string{page, next}, []string{focus, strings.Repeat("dd", 32)})
	if len(got) != 4 || got[0] != focus || got[1] != page || got[2] != next || got[3] != strings.Repeat("dd", 32) {
		t.Fatalf("order %#v", got)
	}
}
