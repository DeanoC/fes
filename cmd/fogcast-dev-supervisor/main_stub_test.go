//go:build !linux || !fpgadev

package main

import (
	"bytes"
	"context"
	"testing"
)

func TestUnsupportedSupervisorCommandIsNotCallable(t *testing.T) {
	var out, errOut bytes.Buffer
	called := false
	if got := runSupervisor(nil, &out, &errOut, func(context.Context, any) error { called = true; return nil }); got != supervisorExitUsage || called {
		t.Fatalf("exit=%d called=%v", got, called)
	}
}
