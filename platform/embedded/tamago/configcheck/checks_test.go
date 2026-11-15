package configcheck

import "testing"

func TestContainedConfigurationChecks(t *testing.T) {
	if n := Run(func(name string, err error) {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}); n != 0 {
		t.Fatalf("%d checks failed", n)
	}
}
