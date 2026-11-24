package input

import (
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/kitlease"
)

func TestLocalKeyboardGrantRejectsReusedCoreIdentityAfterRuntimeReplacement(t *testing.T) {
	binding := KeyboardHIDBinding{RuntimeInstance: strings.Repeat("d", 32), PackageID: strings.Repeat("c", 64), Generation: 3}
	leaseGeneration := strings.Repeat("b", 64)
	observed := binding
	c := &TargetController{
		active: &lease{session: 7, executionGeneration: leaseGeneration, keyboardBinding: &binding},
		observe: func(context.Context) (CoreObservation, error) {
			return CoreObservation{Active: true, KeyboardHID: &observed}, nil
		},
	}
	ctx := kitlease.WithAdmission(kitlease.WithGeneration(context.Background(), leaseGeneration))
	if err := c.GrantLocalKeyboard(ctx, 7, binding); err != nil {
		t.Fatalf("exact admitted binding rejected: %v", err)
	}
	c.localKeyboardGrant = nil
	stale := binding
	stale.RuntimeInstance = strings.Repeat("e", 32)
	if err := c.GrantLocalKeyboard(ctx, 7, stale); err == nil || c.localKeyboardGrant != nil {
		t.Fatal("a different offered runtime acquired the existing session")
	}
	observed = stale
	if err := c.GrantLocalKeyboard(ctx, 7, binding); err == nil || c.localKeyboardGrant != nil {
		t.Fatal("replaced runtime with the same package and generation acquired the stale grant")
	}
}
