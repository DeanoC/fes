package main

import "testing"

func TestAcknowledgeRequiresExplicitOperatorIdentity(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--operation", "failed"},
		{"--operation", "failed", "--selected", "prior", "--base", "base", "--boot", "boot", "--action", "action"},
		{"--operation", "failed", "--generation", "-1", "--selected", "prior", "--base", "base", "--boot", "boot", "--action", "action"},
		{"--operation", "failed", "--generation", "0", "--selected", "prior", "--base", "base", "--boot", "boot", "--action", "action", "extra"},
	} {
		if err := acknowledge(args); err == nil {
			t.Fatalf("incomplete operator context accepted: %v", args)
		}
	}
}
