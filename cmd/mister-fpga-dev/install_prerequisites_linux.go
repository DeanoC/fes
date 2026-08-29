//go:build linux && fpgadev

package main

import (
	"errors"
	"fmt"

	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

var productionInstallPrerequisiteError error

// productionInstallPrerequisites is a mutation-free active-profile gate.
// The package's example is never treated as an active configuration.
func productionInstallPrerequisites(path string) (agentconfig.Config, string, error) {
	cfg, hash, err := fpgadev.LoadProtectedAgentConfig(path)
	if err != nil {
		return agentconfig.Config{}, "", fmt.Errorf("development profile configuration unavailable: %w", err)
	}
	if err := cfg.ValidateDevelopmentInventory(); err != nil {
		return agentconfig.Config{}, "", fmt.Errorf("development profile configuration invalid: %w", err)
	}
	if cfg.Token == "REPLACE_WITH_A_TARGET_LOCAL_TOKEN" {
		return agentconfig.Config{}, "", errors.New("development profile configuration invalid: placeholder token")
	}
	return cfg, hash, nil
}
