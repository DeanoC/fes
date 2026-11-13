package nativefabriccheck

import "testing"

func TestNativeFabricChecks(t *testing.T) {
	if failures := Run(func(name string, err error) {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}); failures != 0 {
		t.Fatalf("%d failures", failures)
	}
}
