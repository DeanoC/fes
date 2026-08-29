//go:build linux && arm && fpgadev

package fpgadev

import (
	"context"
	"errors"
	"os"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

const (
	productionAgentConfigPath = "/etc/fogcast/agent.toml"
	productionMainPath        = "/media/fat/MiSTer"
	productionManagerPath     = "/sys/class/fpga_manager/fpga0/state"
	productionToolPath        = "/usr/bin/mister-fpga-dev"
)

// protectedProfileVerifier re-reads the protected profile through the
// descriptor-bound loader. The composition records the digest it admitted so
// a profile replacement cannot silently change the runner after construction.
type protectedProfileVerifier struct {
	path string
	hash string
}

func (v protectedProfileVerifier) Verify(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	cfg, hash, err := LoadProtectedAgentConfig(v.path)
	if err != nil {
		return err
	}
	if v.hash == "" || hash != v.hash {
		return errors.New("protected development profile changed")
	}
	if err := cfg.ValidateProfile(true); err != nil {
		return err
	}
	return contextError(ctx)
}

func verifyProductionTool(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if executableIdentityForPath == nil {
		return ErrProcessScannerUnsupported
	}
	current, err := CurrentProcessAttestation()
	if err != nil {
		return err
	}
	expected, err := executableIdentityForPath(productionToolPath)
	if err != nil {
		return err
	}
	if current.Device != expected.Device || current.Inode != expected.Inode || current.SHA256 != expected.SHA256 {
		return errors.New("development tool executable identity does not match protected package")
	}
	return contextError(ctx)
}

// productionRuntimeReadiness adapts the cumulative SupervisorRuntime checks
// to the Task 7 readinessVerifier boundary without exposing runtime handles.
type productionRuntimeReadiness = CompatibilityMainReadiness

func newProductionRunnerDependencies() (runnerDependencies, error) {
	cfg, profileHash, err := LoadProtectedAgentConfig(productionAgentConfigPath)
	if err != nil {
		return runnerDependencies{}, err
	}
	if err := cfg.ValidateProfile(true); err != nil {
		return runnerDependencies{}, err
	}
	mainObserver, err := NewObserverForExecutable(productionMainPath)
	if err != nil {
		return runnerDependencies{}, err
	}
	runtime, err := NewSupervisorRuntime(SupervisorRuntimeConfig{
		MainExecutable:   productionMainPath,
		MainFIFO:         cfg.CommandPipe,
		FPGAManagerState: productionManagerPath,
		MenuPath:         cfg.MenuRBF,
		CoreNameFile:     cfg.CoreNameFile,
		AgentExecutable:  "/usr/bin/mister-agent",
	})
	if err != nil {
		return runnerDependencies{}, err
	}
	mapper := NewMapper()
	readyStore := NewProductionReadyStoreV3()
	evidence := newProductionQualificationEvidenceWithOptions(productionQualificationEvidenceOptions{
		observer: mainObserver, ready: readyStore, bootID: readKernelBootID,
	})
	quiescence := &readyRecordQuiescence{evidence: evidence, admission: NewProductionDevelopmentAdmissionVerifier(productionAgentConfigPath)}
	artifact := NewArtifactAccess()
	return runnerDependencies{
		maintenance: NewProductionMaintenanceGate(),
		quiescence:  quiescence,
		designation: NewDesignation(cfg.DesignationPath, cfg.TargetIdentityPath),
		profile:     protectedProfileVerifier{path: productionAgentConfigPath, hash: profileHash},
		store:       hardwareowner.NewStore(cfg.HardwareOwnerPath),
		locker:      hardwareowner.NewLocker(cfg.HardwareOwnerLock),
		artifact:    artifact,
		fifo:        NewFIFO(cfg.CommandPipe),
		observer:    mainObserver,
		qualifier: NewQualifier(mapper, NewStaticPolicy(), QualificationObservers{
			MainAbsent:   mainObserver,
			Subsystems:   evidence,
			PressedInput: evidence,
			Programming:  evidence,
		}),
		mapper:    mapper,
		results:   NewProductionResultStore(),
		readiness: NewCompatibilityMainReadiness(runtime, mainObserver),
		install:   NewCompatibilityMainReadiness(runtime, mainObserver),
		reboot:    commandRebooter{},
		clock:     realRunnerClock{},
		bootID:    readKernelBootID,
		privilege: func() bool { return os.Geteuid() == 0 },
		tool:      verifyProductionTool,
		bind: func(ctx context.Context, request Request) (Manifest, ArtifactBinding, error) {
			manifest, binding, bindErr := productionBind(artifact, productionBindOptions{})(ctx, request)
			if bindErr == nil {
				evidence.setBinding(binding)
			}
			return manifest, binding, bindErr
		},
		revalidate: func(ctx context.Context, binding *ArtifactBinding) error {
			if err := contextError(ctx); err != nil {
				return err
			}
			if binding == nil {
				return ErrRunnerConfiguration
			}
			return binding.Revalidate()
		},
		dispatchPath: func(ctx context.Context, binding *ArtifactBinding) (string, error) {
			if err := contextError(ctx); err != nil {
				return "", err
			}
			if binding == nil {
				return "", ErrRunnerConfiguration
			}
			return binding.DispatchPath()
		},
		mailboxProgress: runMailboxWithProgress,
		newSession:      randomSession,
	}, nil
}
