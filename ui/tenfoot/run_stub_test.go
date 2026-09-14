//go:build !sdl3

package tenfoot

import "testing"

func TestRunWithoutSDLTagReportsRequirement(t *testing.T) {
	if err := Run(t.Context(), Options{}); err != ErrSDLRequired {
		t.Fatalf("err = %v", err)
	}
}
