//go:build linux && fpgadev

package fpgadev

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

const productionProgrammingToolPath = "/usr/bin/mister-fpga-dev"

// productionQualificationEvidence is the dynamic evidence source used by
// loader qualification after Main has exited. It is deliberately separate
// from DevelopmentAdmissionVerifier: normal development admission consumes
// only ReadyRecordV3 and PID/start-time pairs, and must not inherit this
// qualification observer's executable, descriptor, network, or resolved-
// inventory requirements.
//
// The procRoot, processRoot, readProcessIdentity, and resolveInventory fields
// are private fixture seams. The ARM production composition leaves them at
// their Linux defaults, so the target still observes descriptor-bound paths,
// the complete /proc population, and exact /proc/net endpoint joins.
type productionQualificationEvidence struct {
	mu       sync.Mutex
	observer *Observer
	proof    *BootProofStore
	ready    ReadyStoreV3
	bootID   func() (string, error)

	procRoot             string
	processRoot          string
	requireMainFIFOOwner bool
	readProcessIdentity  func(context.Context, string, int) (ProcessIdentity, error)
	resolveInventory     func(context.Context, InventoryV1, InventoryResolutionOptions) (ResolvedInventoryV1, error)
	observeProgramming   func(context.Context) (ProgrammingAbsenceProof, error)

	status  MaintenanceStatus
	owner   hardwareowner.Record
	binding ArtifactBinding
	hasBind bool
}

type productionQualificationEvidenceOptions struct {
	observer             *Observer
	proof                *BootProofStore
	ready                ReadyStoreV3
	bootID               func() (string, error)
	procRoot             string
	processRoot          string
	allowNonRootFIFO     bool
	readProcessIdentity  func(context.Context, string, int) (ProcessIdentity, error)
	resolveInventory     func(context.Context, InventoryV1, InventoryResolutionOptions) (ResolvedInventoryV1, error)
	observeProgramming   func(context.Context) (ProgrammingAbsenceProof, error)
	authoritativeWorkers bool // legacy fixture field; never consulted by production proof
}

func newProductionQualificationEvidence(observer *Observer, proof *BootProofStore, bootID func() (string, error)) *productionQualificationEvidence {
	return newProductionQualificationEvidenceWithOptions(productionQualificationEvidenceOptions{
		observer: observer,
		proof:    proof,
		bootID:   bootID,
	})
}

func newProductionQualificationEvidenceWithOptions(options productionQualificationEvidenceOptions) *productionQualificationEvidence {
	procRoot := options.procRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	processRoot := options.processRoot
	if processRoot == "" {
		processRoot = procRoot
	}
	readIdentity := options.readProcessIdentity
	if readIdentity == nil {
		readIdentity = readInventoryProcessIdentity
	}
	resolve := options.resolveInventory
	if resolve == nil {
		resolve = ResolveInventoryAt
	}
	observeProgramming := options.observeProgramming
	if observeProgramming == nil {
		observeProgramming = func(ctx context.Context) (ProgrammingAbsenceProof, error) {
			return observeLinuxProgrammingAbsence(ctx, procRoot, productionProgrammingToolPath, os.Getpid())
		}
	}
	return &productionQualificationEvidence{
		observer:             options.observer,
		proof:                options.proof,
		ready:                options.ready,
		bootID:               options.bootID,
		procRoot:             procRoot,
		processRoot:          processRoot,
		requireMainFIFOOwner: !options.allowNonRootFIFO,
		readProcessIdentity:  readIdentity,
		resolveInventory:     resolve,
		observeProgramming:   observeProgramming,
	}
}

func (e *productionQualificationEvidence) setStatus(status MaintenanceStatus, owner hardwareowner.Record) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.status, e.owner = status, owner
	e.mu.Unlock()
}

func (e *productionQualificationEvidence) setBinding(binding ArtifactBinding) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.binding, e.hasBind = binding, binding.state != nil
	e.mu.Unlock()
}

func (e *productionQualificationEvidence) snapshot() (MaintenanceStatus, hardwareowner.Record, ArtifactBinding, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status, e.owner, e.binding, e.hasBind
}

func (e *productionQualificationEvidence) Verify(ctx context.Context) (PolicySubsystemProof, error) {
	status, owner, binding, hasBinding := e.snapshot()
	if !hasBinding {
		return PolicySubsystemProof{}, errors.New("production subsystem evidence has no retained artifact")
	}
	policy, err := (staticPolicyVerifier{}).Verify(ctx, binding)
	if err != nil {
		return PolicySubsystemProof{}, err
	}
	return e.verifyDynamic(ctx, status, owner, binding, policy)
}

func (e *productionQualificationEvidence) VerifyWithPolicy(ctx context.Context, binding ArtifactBinding, policy PolicyProof) (PolicySubsystemProof, error) {
	manifest, metadata, err := validateQualificationBinding(&binding)
	if err != nil {
		return PolicySubsystemProof{}, err
	}
	if err := policy.Validate(manifest, metadata); err != nil {
		return PolicySubsystemProof{}, err
	}
	e.setBinding(binding)
	status, owner, _, _ := e.snapshot()
	return e.verifyDynamic(ctx, status, owner, binding, policy)
}

func (e *productionQualificationEvidence) VerifyPriorBootWithPolicy(ctx context.Context, binding ArtifactBinding, policy PolicyProof) (PolicySubsystemProof, error) {
	manifest, metadata, err := validateQualificationBinding(&binding)
	if err != nil {
		return PolicySubsystemProof{}, err
	}
	if err := policy.Validate(manifest, metadata); err != nil {
		return PolicySubsystemProof{}, err
	}
	e.setBinding(binding)
	status, owner, _, _ := e.snapshot()
	return e.verifyDynamicMode(ctx, status, owner, binding, policy, false)
}

func (e *productionQualificationEvidence) NeutralizePressedInput(ctx context.Context) error {
	return e.neutralizePressedInputMode(ctx, true)
}

func (e *productionQualificationEvidence) NeutralizePriorBootPressedInput(ctx context.Context) error {
	return e.neutralizePressedInputMode(ctx, false)
}

func (e *productionQualificationEvidence) neutralizePressedInputMode(ctx context.Context, requireReady bool) error {
	status, owner, binding, hasBinding := e.snapshot()
	if !hasBinding {
		return errors.New("pressed-input evidence has no retained artifact")
	}
	policy, err := (staticPolicyVerifier{}).Verify(ctx, binding)
	if err != nil {
		return err
	}
	proof, err := e.verifyDynamicMode(ctx, status, owner, binding, policy, requireReady)
	if err != nil {
		return err
	}
	if !proof.PressedInputCleared {
		return errors.New("pressed-input route was not proven clear")
	}
	return nil
}

func (e *productionQualificationEvidence) VerifyProgramming(ctx context.Context) (ProgrammingAbsenceProof, error) {
	return e.verifyProgrammingMode(ctx, true)
}

func (e *productionQualificationEvidence) VerifyPriorBootProgramming(ctx context.Context) (ProgrammingAbsenceProof, error) {
	return e.verifyProgrammingMode(ctx, false)
}

func (e *productionQualificationEvidence) verifyProgrammingMode(ctx context.Context, requireReady bool) (ProgrammingAbsenceProof, error) {
	status, owner, binding, hasBinding := e.snapshot()
	if !hasBinding {
		return ProgrammingAbsenceProof{}, errors.New("programming evidence has no retained artifact")
	}
	policy, err := (staticPolicyVerifier{}).Verify(ctx, binding)
	if err != nil {
		return ProgrammingAbsenceProof{}, err
	}
	if requireReady && e.ready == nil {
		return ProgrammingAbsenceProof{}, ErrQuiescenceEvidenceUnavailable
	}
	if _, err := e.verifyDynamicMode(ctx, status, owner, binding, policy, requireReady); err != nil {
		return ProgrammingAbsenceProof{}, err
	}
	if e.observeProgramming == nil {
		return ProgrammingAbsenceProof{}, ErrQuiescenceEvidenceUnavailable
	}
	return e.observeProgramming(ctx)
}

// observeLinuxProgrammingAbsence performs the intentionally small, direct
// conflict check required by the development profile. It excludes this
// command's own controlled /dev/mem mapping, rejects another instance of the
// protected programming helper, and rejects any other process retaining a
// /dev/mem descriptor or mapping.
func observeLinuxProgrammingAbsence(ctx context.Context, procRoot, toolPath string, selfPID int) (ProgrammingAbsenceProof, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProgrammingAbsenceProof{}, err
	}
	if procRoot == "" || !filepath.IsAbs(procRoot) || filepath.Clean(procRoot) != procRoot || toolPath == "" || !filepath.IsAbs(toolPath) || filepath.Clean(toolPath) != toolPath || selfPID <= 0 {
		return ProgrammingAbsenceProof{}, errors.New("programming observation configuration is invalid")
	}
	tool, err := executableIdentityForPath(toolPath)
	if err != nil {
		return ProgrammingAbsenceProof{}, fmt.Errorf("read protected programming tool identity: %w", err)
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return ProgrammingAbsenceProof{}, fmt.Errorf("scan programming process population: %w", err)
	}
	proof := ProgrammingAbsenceProof{NoProgrammingProcess: true, NoProgrammingMapping: true}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return ProgrammingAbsenceProof{}, err
		}
		pid, ok := parseInventoryPID(entry.Name())
		if !ok || pid == selfPID {
			continue
		}
		processRoot := filepath.Join(procRoot, strconv.Itoa(pid))
		device, inode, identityErr := executableDeviceInode(ctx, filepath.Join(processRoot, "exe"))
		if identityErr == nil {
			if device == tool.Device && inode == tool.Inode {
				proof.NoProgrammingProcess = false
			}
		} else if !errors.Is(identityErr, os.ErrNotExist) {
			return ProgrammingAbsenceProof{}, fmt.Errorf("observe programming process executable: %w", identityErr)
		}
		mapping, err := processUsesDevMem(ctx, processRoot)
		if err != nil {
			return ProgrammingAbsenceProof{}, err
		}
		if mapping {
			proof.NoProgrammingMapping = false
		}
	}
	return proof, ctx.Err()
}

func processUsesDevMem(ctx context.Context, processRoot string) (bool, error) {
	fds, err := os.ReadDir(filepath.Join(processRoot, "fd"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("scan programming process descriptors: %w", err)
	}
	for _, fd := range fds {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		target, err := os.Readlink(filepath.Join(processRoot, "fd", fd.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("read programming process descriptor: %w", err)
		}
		if normalizeInventoryDescriptorTarget(target) == "/dev/mem" {
			return true, nil
		}
	}
	maps, err := os.Open(filepath.Join(processRoot, "maps"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open programming process mappings: %w", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(maps, InstallJournalMaxBytes+1))
	closeErr := maps.Close()
	if readErr != nil {
		return false, fmt.Errorf("read programming process mappings: %w", readErr)
	}
	if closeErr != nil {
		return false, fmt.Errorf("close programming process mappings: %w", closeErr)
	}
	if len(raw) > InstallJournalMaxBytes {
		return false, errors.New("programming process mappings exceed bound")
	}
	for lineNumber, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return false, fmt.Errorf("programming process mappings line %d is malformed", lineNumber+1)
		}
		if len(fields) >= 6 && normalizeInventoryDescriptorTarget(strings.Join(fields[5:], " ")) == "/dev/mem" {
			return true, nil
		}
	}
	return false, ctx.Err()
}

func (e *productionQualificationEvidence) verifyPostMain(ctx context.Context) (PolicySubsystemProof, error) {
	return e.verifyPostMainWithReady(ctx, true)
}

func (e *productionQualificationEvidence) verifyPriorBootPostMain(ctx context.Context) (PolicySubsystemProof, error) {
	return e.verifyPostMainWithReady(ctx, false)
}

func (e *productionQualificationEvidence) verifyPostMainWithReady(ctx context.Context, requireReady bool) (PolicySubsystemProof, error) {
	status, owner, binding, hasBinding := e.snapshot()
	if !hasBinding {
		return PolicySubsystemProof{}, errors.New("post-Main evidence has no retained artifact")
	}
	policy, err := (staticPolicyVerifier{}).Verify(ctx, binding)
	if err != nil {
		return PolicySubsystemProof{}, err
	}
	return e.verifyDynamicMode(ctx, status, owner, binding, policy, requireReady)
}

func (e *productionQualificationEvidence) verifyPressed(ctx context.Context) error {
	return e.NeutralizePressedInput(ctx)
}

func (e *productionQualificationEvidence) verifyDynamic(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record, binding ArtifactBinding, policy PolicyProof) (PolicySubsystemProof, error) {
	return e.verifyDynamicMode(ctx, status, owner, binding, policy, true)
}

func (e *productionQualificationEvidence) verifyDynamicMode(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record, binding ArtifactBinding, policy PolicyProof, requireReady bool) (PolicySubsystemProof, error) {
	if err := contextError(ctx); err != nil {
		return PolicySubsystemProof{}, err
	}
	if err := status.Validate(); err != nil {
		return PolicySubsystemProof{}, err
	}
	if err := owner.Validate(); err != nil {
		return PolicySubsystemProof{}, err
	}
	if owner.State != hardwareowner.StateRecoveringIntent && owner.State != hardwareowner.StateNormalMain {
		return PolicySubsystemProof{}, errors.New("owner is not in a quiescing development phase")
	}
	if owner.State == hardwareowner.StateRecoveringIntent && (owner.QuiescingOwner != hardwareowner.OwnerCompatMain || owner.CandidateOwner != hardwareowner.OwnerFPGADev) {
		return PolicySubsystemProof{}, errors.New("owner quiescing tuple is invalid")
	}
	manifest, metadata, err := validateQualificationBinding(&binding)
	if err != nil {
		return PolicySubsystemProof{}, err
	}
	if err := policy.Validate(manifest, metadata); err != nil {
		return PolicySubsystemProof{}, err
	}
	if e.bootID == nil {
		return PolicySubsystemProof{}, ErrQuiescenceEvidenceUnavailable
	}
	var ready ReadyRecordV3
	var boot BootProof
	if requireReady && e.ready != nil {
		var exists bool
		ready, exists, err = e.ready.Load()
		if err != nil || !exists {
			if err == nil {
				err = errors.New("ready record is absent")
			}
			return PolicySubsystemProof{}, err
		}
		if err := ready.Validate(); err != nil {
			return PolicySubsystemProof{}, err
		}
	} else if requireReady && e.proof == nil {
		return PolicySubsystemProof{}, ErrQuiescenceEvidenceUnavailable
	} else if requireReady {
		var exists bool
		boot, exists, err = e.proof.Load()
		if err != nil || !exists {
			if err == nil {
				err = errors.New("boot proof is absent")
			}
			return PolicySubsystemProof{}, err
		}
		if err := boot.Validate(); err != nil {
			return PolicySubsystemProof{}, err
		}
	}
	bootID, err := e.bootID()
	if err != nil {
		return PolicySubsystemProof{}, err
	}
	if !requireReady && owner.BootID != bootID {
		return PolicySubsystemProof{}, errors.New("owner does not match current production boot")
	}
	if requireReady && e.ready != nil && (ready.BootID != bootID || ready.JournalSHA256 != status.TerminalJournalSHA256) {
		return PolicySubsystemProof{}, errors.New("boot proof does not match production evidence")
	}
	if requireReady && e.ready == nil && (boot.BootID != bootID || boot.JournalSHA256 != status.TerminalJournalSHA256 || !boot.ResolvedInventoryMatches(status.Inventory) || !sameCapabilities(boot.Capabilities)) {
		return PolicySubsystemProof{}, errors.New("boot proof does not match production evidence")
	}
	if e.observer == nil {
		return PolicySubsystemProof{}, ErrProcessScannerUnsupported
	}
	main, err := snapshotMainBaseline(ctx, e.observer)
	if err != nil {
		return PolicySubsystemProof{}, err
	}
	if len(main) != 0 {
		return PolicySubsystemProof{}, errors.New("Main process remains present")
	}
	readIdentity := e.readProcessIdentity
	if readIdentity == nil {
		readIdentity = readInventoryProcessIdentity
	}
	expectedProcesses := []ProcessIdentity{}
	if requireReady && e.ready != nil {
		expectedProcesses = []ProcessIdentity{{PID: int(ready.SupervisorPID), StartTime: ready.SupervisorStartTime}, {PID: int(ready.AgentPID), StartTime: ready.AgentStartTime}}
	} else if requireReady {
		expectedProcesses = []ProcessIdentity{{PID: int(boot.SupervisorPID), StartTime: boot.SupervisorStartTime, Device: boot.SupervisorExecutableDevice, Inode: boot.SupervisorExecutableInode, SHA256: boot.SupervisorExecutableSHA256}, {PID: int(boot.AgentPID), StartTime: boot.AgentStartTime, Device: boot.AgentExecutableDevice, Inode: boot.AgentExecutableInode, SHA256: boot.AgentExecutableSHA256}}
	}
	for _, expected := range expectedProcesses {
		actual, identityErr := readIdentity(ctx, e.processRoot, expected.PID)
		matches := actual.equal(expected)
		if requireReady && e.ready != nil {
			matches = actual.PID == expected.PID && actual.StartTime == expected.StartTime
		}
		if identityErr != nil || !matches {
			if identityErr == nil {
				identityErr = errors.New("live production process identity does not match boot proof")
			}
			return PolicySubsystemProof{}, identityErr
		}
	}
	resolve := e.resolveInventory
	if resolve == nil {
		resolve = ResolveInventoryAt
	}
	resolved, err := resolve(ctx, status.Inventory, InventoryResolutionOptions{
		ProcRoot: e.procRoot,
		Phase:    InventoryPhaseMainAbsent,
		Prior: func() *ResolvedInventoryV1 {
			if !requireReady || e.ready != nil {
				return nil
			}
			return &boot.ResolvedInventory
		}(),
		RequireProcessEvidence: false,
		ScanProcessDescriptors: true,
		RequireNetworkEvidence: true,
		RequireNetworkAbsence:  !requireReady || e.ready != nil,
		RequireMainFIFOOwner:   e.requireMainFIFOOwner,
	})
	if err != nil {
		return PolicySubsystemProof{}, err
	}
	if err := resolved.Validate(); err != nil {
		return PolicySubsystemProof{}, err
	}
	if requireReady && e.ready == nil {
		return PolicySubsystemProof{}, ErrQuiescenceEvidenceUnavailable
	}
	return PolicySubsystemProof{MainAbsent: true, InputWorkersAbsent: true, OffloadWorkersAbsent: true, PresentationWorkersAbsent: true, DeviceDescriptorsAbsent: true, PressedInputCleared: true, NoInput: true, NoOffload: true, NoPresentation: true, NoSave: true, NoStorage: true, NoVideo: true, NoAudio: true, NoPLL: true, NoSDRAM: true, NoExternalOutput: true, NoSharedMemory: true}, nil
}

// productionRunnerQuiescence projects an in-flight recovering_intent owner
// onto its still-live compat_main tuple for boot-proof validation. The actual
// owner/status are retained by productionQualificationEvidence and validated
// before dynamic proof is returned.
type productionRunnerQuiescence struct {
	base     QuiescenceVerifier
	evidence *productionQualificationEvidence
}

// readyRecordQuiescence is the production adapter. ReadyRecordV3 plus the
// current terminal journal is the authoritative supervisor handoff; the
// legacy BootProofStore is not part of production composition.
type readyRecordQuiescence struct {
	evidence  *productionQualificationEvidence
	admission *DevelopmentAdmissionVerifier
}

func (q *readyRecordQuiescence) VerifyPreDispatch(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) error {
	if q == nil || q.evidence == nil || q.admission == nil {
		return ErrRunnerConfiguration
	}
	q.evidence.setStatus(status, owner)
	if err := contextError(ctx); err != nil {
		return err
	}
	return q.admission.Verify(ctx, projectAdmissionOwner(owner))
}

func (q *readyRecordQuiescence) VerifyPriorBoot(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record, currentBootID string) error {
	if q == nil || q.evidence == nil || q.admission == nil {
		return ErrRunnerConfiguration
	}
	if owner.State != hardwareowner.StateNormalMain {
		return hardwareowner.ErrOwnerNotNormal
	}
	q.evidence.setStatus(status, owner)
	if err := contextError(ctx); err != nil {
		return err
	}
	return q.admission.VerifyPriorBoot(ctx, projectAdmissionOwner(owner), currentBootID, status)
}

func (q *readyRecordQuiescence) VerifyPriorBootPostMain(ctx context.Context, status MaintenanceStatus, previous, current hardwareowner.Record, currentBootID string) (PolicySubsystemProof, error) {
	if q == nil || q.evidence == nil || q.admission == nil {
		return PolicySubsystemProof{}, ErrRunnerConfiguration
	}
	if current.State != hardwareowner.StateRecoveringIntent || current.BootID != currentBootID ||
		current.ActiveSession != previous.ActiveSession || current.ActiveGeneration != previous.ActiveGeneration ||
		current.ActiveMode != previous.ActiveMode || current.ActiveOwner != hardwareowner.OwnerCompatMain ||
		!sameStringSlice(current.ActiveLeases, previous.ActiveLeases) || current.QuiescingOwner != hardwareowner.OwnerCompatMain ||
		current.CandidateOwner != hardwareowner.OwnerFPGADev {
		return PolicySubsystemProof{}, hardwareowner.ErrOwnerNotNormal
	}
	if err := q.admission.VerifyPriorBoot(ctx, projectAdmissionOwner(previous), currentBootID, status); err != nil {
		return PolicySubsystemProof{}, err
	}
	q.evidence.setStatus(status, current)
	return q.evidence.verifyPriorBootPostMain(ctx)
}

func (q *readyRecordQuiescence) VerifyPostMain(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) (PolicySubsystemProof, error) {
	if q == nil || q.evidence == nil {
		return PolicySubsystemProof{}, ErrRunnerConfiguration
	}
	q.evidence.setStatus(status, owner)
	return q.evidence.verifyPostMain(ctx)
}

func (q *readyRecordQuiescence) VerifyPressedInput(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) error {
	if q == nil || q.evidence == nil {
		return ErrRunnerConfiguration
	}
	q.evidence.setStatus(status, owner)
	return q.evidence.verifyPressed(ctx)
}

var _ QuiescenceVerifier = (*readyRecordQuiescence)(nil)

func (q *productionRunnerQuiescence) projected(owner hardwareowner.Record) hardwareowner.Record {
	return projectAdmissionOwner(owner)
}

func projectAdmissionOwner(owner hardwareowner.Record) hardwareowner.Record {
	if owner.State != hardwareowner.StateRecoveringIntent {
		return owner
	}
	owner.State = hardwareowner.StateNormalMain
	owner.Phase = ""
	owner.RunID = ""
	owner.GenerationHighWater = owner.ActiveGeneration
	owner.CandidateSession = ""
	owner.CandidateGeneration = 0
	owner.CandidateMode = hardwareowner.ModeNone
	owner.CandidateOwner = hardwareowner.OwnerNone
	owner.QuiescingOwner = hardwareowner.OwnerNone
	owner.RequestedResources = []string{}
	return owner
}

func (q *productionRunnerQuiescence) VerifyPreDispatch(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) error {
	if q == nil || q.base == nil || q.evidence == nil {
		return ErrRunnerConfiguration
	}
	q.evidence.setStatus(status, owner)
	return q.base.VerifyPreDispatch(ctx, status, q.projected(owner))
}

func (q *productionRunnerQuiescence) VerifyPostMain(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) (PolicySubsystemProof, error) {
	if q == nil || q.base == nil || q.evidence == nil {
		return PolicySubsystemProof{}, ErrRunnerConfiguration
	}
	q.evidence.setStatus(status, owner)
	return q.base.VerifyPostMain(ctx, status, q.projected(owner))
}

func (q *productionRunnerQuiescence) VerifyPressedInput(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) error {
	if q == nil || q.base == nil || q.evidence == nil {
		return ErrRunnerConfiguration
	}
	q.evidence.setStatus(status, owner)
	return q.base.VerifyPressedInput(ctx, status, q.projected(owner))
}

var _ QuiescenceVerifier = (*productionRunnerQuiescence)(nil)

func (realRunnerClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }
