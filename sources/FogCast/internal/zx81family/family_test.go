package zx81family

import "testing"

func TestVariantsStayInSetupOrder(t *testing.T) {
	got := Variants()
	want := []string{"fes.zx81", "fes.zx80", "fes.ts1000", "fes.ts1500"}
	if len(got) != len(want) {
		t.Fatalf("variants %d", len(got))
	}
	for i, id := range want {
		if got[i].CoreID != id || got[i].Label == "" || got[i].Detail == "" {
			t.Fatalf("variant %d %+v", i, got[i])
		}
		found, ok := ByID(id)
		if !ok || found != got[i] || !Known(id) {
			t.Fatalf("lookup %s %+v %v", id, found, ok)
		}
	}
	if _, ok := ByID("fes.pong"); ok || Known("fes.pong") {
		t.Fatal("pong is not a ZX81-family machine")
	}
	if !Saves("fes.zx81") || !Saves("fes.ts1000") || !Saves("fes.ts1500") {
		t.Fatal("ZX81 ROM models save")
	}
	if Saves("fes.zx80") || Saves("fes.pong") {
		t.Fatal("ZX80 and other cores have no ZX81 SAVE")
	}
}
