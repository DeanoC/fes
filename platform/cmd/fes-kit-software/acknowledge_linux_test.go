package main

import "testing"

func TestOperatorRepairRequiresExplicitIdentity(t *testing.T) {
	for name, run := range map[string]func([]string) error{"acknowledge": acknowledge, "prepare-power-cycle": preparePowerCycle, "select-factory": selectFactory} {
		t.Run(name, func(t *testing.T) {
			for _, args := range [][]string{
				nil,
				{"--operation", "failed"},
				{"--operation", "failed", "--selected", "prior", "--base", "base", "--boot", "boot", "--action", "action"},
				{"--operation", "failed", "--generation", "-1", "--selected", "prior", "--base", "base", "--boot", "boot", "--action", "action"},
				{"--operation", "failed", "--generation", "0", "--selected", "prior", "--base", "base", "--boot", "boot", "--action", "action", "extra"},
			} {
				if err := run(args); err == nil {
					t.Fatalf("incomplete operator context accepted: %v", args)
				}
			}
		})
	}
}
