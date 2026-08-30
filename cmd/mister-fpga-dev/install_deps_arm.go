//go:build linux && arm && fpgadev

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

const (
	productionAgentConfig      = "/etc/fogcast/agent.toml"
	productionAgentTemplate    = "/etc/fogcast/agent.toml.example"
	productionMainExecutable   = "/media/fat/MiSTer"
	productionManagerState     = "/sys/class/fpga_manager/fpga0/state"
	productionRecoveryHelper   = "/usr/bin/mister-fpga-dev"
	productionSupervisorBinary = "/usr/bin/fogcast-dev-supervisor"
	productionSupervisorSource = "/media/fat/linux/fogcast-dev-supervisor.sh"
	productionStartScript      = "/media/fat/linux/fogcast-dev-start.sh"
	productionInstallTimeout   = 30 * time.Second
)

// productionInstallManager is the only target constructor for the three
// root-only install commands. It refuses to return a partially configured
// callback manager: every protected authority, source mutation, old-agent
// proof, owner store, readiness adapter, and reboot/exec handoff is assembled
// before the command can touch the journal.
func productionInstallManager() *fpgadev.InstallManager {
	productionInstallPrerequisiteError = nil
	ctx, cancel := context.WithTimeout(context.Background(), productionInstallTimeout)
	defer cancel()
	cfg, _, err := productionInstallPrerequisites(productionAgentConfig)
	if err != nil {
		productionInstallPrerequisiteError = err
		return nil
	}
	journal := fpgadev.NewProductionInstallJournal()
	if err := ctx.Err(); err != nil {
		productionInstallPrerequisiteError = fmt.Errorf("development profile construction timed out: %w", err)
		return nil
	}
	mainObserver, err := fpgadev.NewObserverForExecutable(productionMainExecutable)
	if err != nil {
		productionInstallPrerequisiteError = fmt.Errorf("compatibility Main observer unavailable: %w", err)
		return nil
	}
	agentTargets, err := productionAgentTargets()
	if err != nil {
		productionInstallPrerequisiteError = fmt.Errorf("legacy agent observer unavailable: %w", err)
		return nil
	}
	stopAgent := stopRecordedAgents(agentTargets, stopProductionLegacyAgentAuthority, signalProductionAgent)
	proveAgentAbsent := func(ctx context.Context) error {
		return stopRecordedTargets(ctx, agentTargets, signalProductionAgent, productionAgentStableAbsence)
	}
	runtime, err := fpgadev.NewSupervisorRuntime(fpgadev.SupervisorRuntimeConfig{
		MainExecutable:   productionMainExecutable,
		MainFIFO:         cfg.CommandPipe,
		FPGAManagerState: productionManagerState,
		MenuPath:         cfg.MenuRBF,
		CoreNameFile:     cfg.CoreNameFile,
		AgentExecutable:  productionAgentExecutable,
	})
	if err != nil {
		productionInstallPrerequisiteError = fmt.Errorf("supervisor runtime configuration invalid: %w", err)
		return nil
	}
	bounded := func(operation func(context.Context) error) func(context.Context) error {
		return func(parent context.Context) error {
			if parent == nil {
				parent = context.Background()
			}
			boundedContext, cancel := context.WithTimeout(parent, productionInstallTimeout)
			defer cancel()
			return operation(boundedContext)
		}
	}
	manager, err := fpgadev.NewProtectedInstallManager(fpgadev.ProtectedInstallManagerOptions{
		ConfigPath:       productionAgentConfig,
		MainExecutable:   productionMainExecutable,
		MainFIFO:         cfg.CommandPipe,
		BackupDir:        journal.BackupDir,
		JournalPath:      journal.Path,
		InstallLockPath:  fpgadev.InstallLockPath,
		OwnerPath:        cfg.HardwareOwnerPath,
		OwnerLockPath:    cfg.HardwareOwnerLock,
		SupervisorSource: productionSupervisorSource,
		StageRoot:        "/var/lib/fogcast/fpgadev-staging",
		FixedMembers: map[string]string{
			"bin/fogcast-dev-supervisor":        productionSupervisorBinary,
			"bin/mister-agent":                  productionAgentExecutable,
			"bin/mister-fpga-dev":               productionRecoveryHelper,
			"deploy/fpgadev/agent.toml.example": productionAgentTemplate,
			"deploy/fpgadev/start.sh":           productionStartScript,
		},
		SupervisorExecutable: productionSupervisorBinary,
		PackageSHA256:        productionSelfSHA256(),
		ExpectedUID:          0,
		BootID:               func() (string, error) { return readProductionBootID() },
		MainReadiness:        bounded(fpgadev.NewCompatibilityMainReadiness(runtime, mainObserver).Verify),
		MainObserver:         mainObserver,
		StopAgent:            bounded(stopAgent),
		// Re-run the identity-bound stop during proof so an agent that appears
		// after the first stop is terminated, not merely observed until timeout.
		ProveAgentAbsent: bounded(proveAgentAbsent),
		RequestReboot:    requestProductionReboot,
	})
	if err != nil {
		productionInstallPrerequisiteError = fmt.Errorf("development profile configuration invalid: %w", err)
		return nil
	}
	productionInstallPrerequisiteError = nil
	return manager
}

func stopProductionLegacyAgentAuthority(ctx context.Context) error {
	observer, err := newScriptSupervisorObserver(
		productionLegacySupervisor,
		[]string{"mister-agent", productionLegacyAgent, "--config", productionLegacyAgentConfig},
		[]string{"mister-agent", productionAgentFATExecutable, "--config", productionLegacyAgentConfig},
	)
	if err != nil {
		return err
	}
	return stopRecordedTargets(ctx, []recordedAgentTarget{{path: productionLegacySupervisor, observer: observer}}, signalProductionAgent, productionAgentStableAbsence)
}

func readProductionBootID() (string, error) {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	bootID := strings.TrimSpace(string(raw))
	if bootID == "" {
		return "", errors.New("boot ID is unavailable")
	}
	return bootID, nil
}

func productionSelfSHA256() string {
	identity, err := fpgadev.CurrentProcessAttestation()
	if err == nil && identity.SHA256 != "" {
		return identity.SHA256
	}
	raw, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func requestProductionReboot(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return exec.CommandContext(ctx, "/sbin/reboot").Run()
}
