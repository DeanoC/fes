//go:build linux && arm && fpgadev

package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

// supervisorDependencyOptions is the host-fixture seam for the otherwise
// fixed ARM composition. The protected profile remains the authority for
// FIFO, Menu, owner paths, and the agent configuration argument; only the
// executable paths and reset adapter are replaceable for software tests.
type supervisorDependencyOptions struct {
	ConfigPath         string
	MainExecutable     string
	MainArguments      []string
	MainEnvironment    []string
	MainProcessScanner fpgadev.ProcessScanner
	ManagerState       string
	AgentExecutable    string
	AgentEnvironment   []string
	ProcRoot           string
	Reset              any
}

// supervisorResetAdapter and supervisorRebootRequester intentionally have
// their real methods in deps_arm.go.  On a tagged Linux/amd64 build these are
// inert values with no hardware or reboot method, so production composition
// cannot accidentally reach target-only boundaries.
type supervisorResetAdapter struct{}
type supervisorRebootRequester struct{}

const (
	defaultSupervisorConfigPath = "/etc/fogcast/agent.toml"
	defaultSupervisorMainPath   = "/media/fat/MiSTer"
	defaultSupervisorManager    = "/sys/class/fpga_manager/fpga0/state"
	defaultSupervisorAgentPath  = "/usr/bin/mister-agent"
)

func productionSupervisorDependencies(inheritedFD int) fpgadev.Dependencies {
	if goruntime.GOARCH != "arm" {
		// Tagged Linux/amd64 is a host-test build.  Keep command parsing and
		// injected fixture composition available, but do not expose a production
		// reset, child-start, or reboot constructor on the host architecture.
		return fpgadev.Dependencies{}
	}
	deps, err := newSupervisorDependencies(supervisorDependencyOptions{
		ConfigPath:      defaultSupervisorConfigPath,
		MainExecutable:  defaultSupervisorMainPath,
		ManagerState:    defaultSupervisorManager,
		AgentExecutable: defaultSupervisorAgentPath,
	}, inheritedFD)
	if err != nil {
		// A production command must never run from a partially composed
		// dependency set. The zero value is rejected by Supervisor.Run.
		return fpgadev.Dependencies{}
	}
	return deps
}

// newSupervisorDependencies performs the complete protected-profile
// admission before returning any lifecycle callback. An invalid profile,
// runtime, or supervisor identity returns no partially usable dependency
// set, which makes the command's zero-value fallback fail closed.
func newSupervisorDependencies(options supervisorDependencyOptions, inheritedFD int) (fpgadev.Dependencies, error) {
	if options.ConfigPath == "" {
		options.ConfigPath = defaultSupervisorConfigPath
	}
	if options.MainExecutable == "" {
		options.MainExecutable = defaultSupervisorMainPath
	}
	if options.ManagerState == "" {
		options.ManagerState = defaultSupervisorManager
	}
	if options.AgentExecutable == "" {
		options.AgentExecutable = defaultSupervisorAgentPath
	}
	if inheritedFD < -1 || inheritedFD > 1<<20 {
		return fpgadev.Dependencies{}, errors.New("inherited lock fd is outside the bounded range")
	}

	cfg, profileHash, err := fpgadev.LoadProtectedAgentConfig(options.ConfigPath)
	if err != nil {
		return fpgadev.Dependencies{}, fmt.Errorf("load protected agent profile: %w", err)
	}
	if err := cfg.ValidateProfile(true); err != nil {
		return fpgadev.Dependencies{}, fmt.Errorf("validate protected agent profile: %w", err)
	}
	if err := cfg.ValidateDevelopmentInventory(); err != nil {
		return fpgadev.Dependencies{}, fmt.Errorf("validate protected development inventory: %w", err)
	}
	identity, err := fpgadev.CurrentProcessAttestation()
	if err != nil {
		return fpgadev.Dependencies{}, fmt.Errorf("attest supervisor executable: %w", err)
	}
	if err := validateSupervisorIdentity(identity); err != nil {
		return fpgadev.Dependencies{}, err
	}

	runtime, err := fpgadev.NewSupervisorRuntime(fpgadev.SupervisorRuntimeConfig{
		MainExecutable:     options.MainExecutable,
		MainArguments:      append([]string(nil), options.MainArguments...),
		MainEnvironment:    append([]string(nil), options.MainEnvironment...),
		MainProcessScanner: options.MainProcessScanner,
		MainFIFO:           cfg.CommandPipe,
		RequireRootFIFO:    goruntime.GOARCH == "arm",
		FPGAManagerState:   options.ManagerState,
		MenuPath:           cfg.MenuRBF,
		CoreNameFile:       cfg.CoreNameFile,
		AgentExecutable:    options.AgentExecutable,
		// The protected config is the only agent argument supplied by the
		// supervisor. SupervisorRuntime appends its private fd3 argument.
		AgentArguments:   []string{"--config", options.ConfigPath},
		AgentEnvironment: append([]string(nil), options.AgentEnvironment...),
		ProfileSHA256:    profileHash,
	})
	if err != nil {
		return fpgadev.Dependencies{}, fmt.Errorf("construct supervisor runtime: %w", err)
	}
	reset := options.Reset
	if reset == nil {
		reset = supervisorResetAdapter{}
	}
	return fpgadev.Dependencies{
		Journal:            fpgadev.NewProductionInstallJournal(),
		OwnerStore:         hardwareowner.NewStore(cfg.HardwareOwnerPath),
		InstallLocker:      fpgadev.NewInstallLocker(fpgadev.InstallLockPath),
		OwnerLocker:        hardwareowner.NewLocker(cfg.HardwareOwnerLock),
		ReadyStore:         fpgadev.NewProductionReadyStoreV3(),
		BootID:             readSupervisorBootID,
		Reset:              reset,
		RebootRequester:    supervisorRebootRequester{},
		InheritedLockFD:    inheritedFD,
		InheritedLockFDSet: true,
		StartMain:          runtime.StartMain,
		MainReadiness:      runtime.WaitMainReady,
		StartAgent:         runtime.StartAgent,
		ReadinessReceipt:   runtime.ReadinessReceipt,
		Children:           runtime,
		AgentAlive:         runtime.AgentAlive,
		SupervisorIdentity: identity,
		ProfileSHA256:      profileHash,
		WaitLiveness:       runtime.WaitChildren,
		// A terminal journal may outlive an owner record across a reboot. The
		// supervisor is the protected composition that can create the current
		// boot's no_owner checkpoint under both admission locks.
		AllowAbsentOwner: true,
	}, nil
}

func validateSupervisorIdentity(identity fpgadev.ProcessAttestation) error {
	if identity.PID == 0 || identity.StartTime == 0 || identity.Device == 0 || identity.Inode == 0 {
		return errors.New("supervisor executable identity is incomplete")
	}
	decoded, err := hex.DecodeString(identity.SHA256)
	if err != nil || len(decoded) != 32 {
		return errors.New("supervisor executable hash is not canonical")
	}
	return nil
}

func readSupervisorBootID() (string, error) {
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
