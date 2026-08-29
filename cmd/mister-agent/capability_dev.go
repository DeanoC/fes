//go:build fpgadev

package main

import (
	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

// fpgadevCapability is the compile-time half of the restricted development
// profile gate. Configuration alone can never enable this capability.
const (
	fpgadevCapability      = true
	expectedFPGACapability = true
)

func newNormalGate(cfg agentconfig.Config, profilePath ...string) hardwareowner.NormalGate {
	installLocker := fpgadev.NewInstallLocker(fpgadev.InstallLockPath)
	gate := hardwareowner.NewGate(
		hardwareowner.NewStore(cfg.HardwareOwnerPath),
		hardwareowner.NewLocker(cfg.HardwareOwnerLock),
		fpgadev.NewMaintenanceGate(fpgadev.NewProductionInstallJournal(), installLocker),
	)
	gate.InstallLocker = installLocker
	gate.AdmissionVerifier = fpgadev.NewProductionDevelopmentAdmissionVerifier(profilePath...)
	return gate
}
