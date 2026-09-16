package shared

import "testing"

func TestPrefetchOrderKeepsPriorityAcrossGroups(t *testing.T) {
	got := PrefetchOrder([]string{" focus ", "page"}, []string{"page", "next"})
	want := []string{"focus", "page", "next"}
	if len(got) != len(want) {
		t.Fatalf("PrefetchOrder() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PrefetchOrder() = %#v, want %#v", got, want)
		}
	}
}

func TestOSKKitHintUsesSharedKeyboardContract(t *testing.T) {
	if got := OSKKitHint(0); got != "A type | B clear | L/R abc | START done" {
		t.Fatalf("OSKKitHint() = %q", got)
	}
}

func TestAttractPreviewHandlesReturnsNilWithoutMedia(t *testing.T) {
	t.Parallel()
	if got := AttractPreviewHandles(AttractItem{}, Presentation{}); got != nil {
		t.Fatalf("AttractPreviewHandles() = %#v, want nil", got)
	}
}
