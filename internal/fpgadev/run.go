package fpgadev

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

const (
	mainHandoffTimeout     = 5 * time.Second
	rebootTimeout          = 5 * time.Second
	maxStagedManifestBytes = 4 * 1024
)

var (
	// ErrRunPreflight identifies a failure before durable recovering_intent.
	// It is intentionally separate from a post-intent result, which is
	// authoritative in the returned Result and durable result store.
	ErrRunPreflight                      = errors.New("run did not commit durable intent")
	ErrRunnerConfiguration               = errors.New("FPGA development runner is not configured")
	ErrProfileDisabled                   = errors.New("FPGA development profile is disabled")
	ErrResultUnavailable                 = errors.New("terminal result is unavailable")
	ErrMaintenanceGateLock               = errors.New("maintenance gate lock acquisition failed")
	ErrMaintenanceGateJournalLoad        = errors.New("maintenance gate journal load failed")
	ErrMaintenanceGateJournalNotTerminal = errors.New("maintenance gate journal is not terminal")
	ErrMaintenanceGateStatusValidation   = errors.New("maintenance gate status validation failed")
	errOwnerStateStore                   = errors.New("owner state store operation failed")
)

type MaintenanceStatus struct {
	TerminalJournalSHA256 string
	Inventory             InventoryV1
}

func (s MaintenanceStatus) Validate() error {
	if !manifestHashPattern.MatchString(s.TerminalJournalSHA256) {
		return ErrRunnerConfiguration
	}
	return s.Inventory.Validate()
}

// MaintenanceUnlock releases the shared install lock. Implementations must
// be idempotent because every pre-intent cleanup path calls it exactly once.
type MaintenanceUnlock interface{ Unlock() error }

type MaintenanceGate interface {
	Enter(context.Context) (MaintenanceStatus, MaintenanceUnlock, error)
}

type QuiescenceVerifier interface {
	VerifyPreDispatch(context.Context, MaintenanceStatus, hardwareowner.Record) error
	VerifyPostMain(context.Context, MaintenanceStatus, hardwareowner.Record) (PolicySubsystemProof, error)
	VerifyPressedInput(context.Context, MaintenanceStatus, hardwareowner.Record) error
}

// priorBootQuiescenceVerifier is the narrow migration-only proof required to
// rebind a canonical compatibility-Main owner record left by a prior boot. It
// is deliberately separate from ordinary quiescence so generic fixtures and
// hardware admission gates cannot silently treat a stale boot as current.
type priorBootQuiescenceVerifier interface {
	VerifyPriorBoot(context.Context, MaintenanceStatus, hardwareowner.Record, string) error
}

// priorBootPostMainVerifier keeps the successor-boot exception explicit at
// post-Main qualification. Implementations must retain all dynamic absence
// proofs while replacing only the unavailable boot-local ready provenance.
type priorBootPostMainVerifier interface {
	VerifyPriorBootPostMain(context.Context, MaintenanceStatus, hardwareowner.Record, hardwareowner.Record, string) (PolicySubsystemProof, error)
}

// Request names the staged bundle.  The production binder accepts only the
// exact manifest.json/top.rbf pair below a private misteross staging root.
type Request struct {
	ManifestPath string
	ArtifactPath string
}

// Failure is the path-free, stable command failure returned by RunCommand and
// Preflight.  Underlying errors remain available through Unwrap to package
// tests, but Error never prints paths, identities, or syscall text.
type Failure struct {
	Code            Code
	Detail          string
	IntentCommitted bool
	cause           error
}

func (f *Failure) Error() string {
	if f == nil {
		return "FPGA development command failed"
	}
	if f.Detail == "" {
		return string(f.Code)
	}
	return string(f.Code) + ": " + f.Detail
}

func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.cause
}

func (f *Failure) Is(target error) bool {
	return target == ErrRunPreflight && (f == nil || !f.IntentCommitted)
}

func (f *Failure) PreIntent() bool { return f != nil && !f.IntentCommitted }

type runnerClock interface{ Now() time.Time }

type ownerStore interface {
	Load() (hardwareowner.Record, bool, error)
	Replace(hardwareowner.Record) error
}

type ownerLocker interface {
	Lock(context.Context) (hardwareowner.Unlock, error)
}

type artifactBinder interface {
	Bind(Manifest, string) (ArtifactBinding, error)
}

// productionArtifactBinder consumes the validated staging directory retained
// by manifest admission. Its package-private method prevents productionBind
// from resolving the mutable staging pathname a second time between manifest
// and artifact validation.
type productionArtifactBinder interface {
	bindProductionStagingDirectory(Manifest, *productionStagingDirectory) (ArtifactBinding, error)
}

// productionStagingDirectory owns a descriptor validated during production
// manifest admission. The artifact binder consumes file ownership only after
// it has opened and validated top.rbf beneath this same descriptor.
type productionStagingDirectory struct {
	file *os.File
	path string
}

func (d *productionStagingDirectory) close() error {
	if d == nil || d.file == nil {
		return nil
	}
	file := d.file
	d.file = nil
	return file.Close()
}

type fifoDispatcher interface {
	Dispatch(context.Context, string) (Attempt, error)
}

type processObserver interface {
	Snapshot() ([]ProcessIdentity, error)
	WaitStableAbsent(context.Context, []ProcessIdentity, time.Duration) error
}

type contextProcessSnapshotter interface {
	SnapshotContext(context.Context) ([]ProcessIdentity, error)
}

type qualificationRunner interface {
	Qualify(context.Context, ArtifactBinding) (Receipt, error)
}

type mailboxMapper interface {
	OpenMailbox() (Registers, error)
}

type resultWriter interface {
	Create(Result) error
	MarkRecoveryFailed(Result) error
}

type readinessVerifier interface{ Verify(context.Context) error }
type programmedHandoffVerifier interface{ WaitProgrammed(context.Context) error }
type profileVerifier interface{ Verify(context.Context) error }
type rebooter interface{ Request(context.Context) error }

type runnerDependencies struct {
	maintenance     MaintenanceGate
	quiescence      QuiescenceVerifier
	designation     Designation
	profile         profileVerifier
	store           ownerStore
	locker          ownerLocker
	artifact        artifactBinder
	fifo            fifoDispatcher
	observer        processObserver
	qualifier       qualificationRunner
	mapper          mailboxMapper
	results         resultWriter
	readiness       readinessVerifier
	install         readinessVerifier
	reboot          rebooter
	clock           runnerClock
	bootID          func() (string, error)
	privilege       func() bool
	tool            func(context.Context) error
	bind            func(context.Context, Request) (Manifest, ArtifactBinding, error)
	revalidate      func(context.Context, *ArtifactBinding) error
	dispatchPath    func(context.Context, *ArtifactBinding) (string, error)
	mailbox         func(context.Context, Registers, Clock) (Observation, error)
	mailboxProgress func(context.Context, Registers, Clock, mailboxProgressCallback) (Observation, error)
	newSession      func() (string, error)
	closeBinding    func(context.Context, *ArtifactBinding) error
	fault           any
	manifestUID     uint32
}

// Runner is opaque to production callers.  Its only production constructor
// owns the concrete store, locker, mapper, policy, artifact access, and fixed
// timing.  Anonymous dependency injection is package-private in
// newFixtureRunner and is used only by this package's fixture tests.
type Runner struct {
	implementation  *runnerImplementation
	constructionErr error
}

type runnerImplementation struct{ dependencies runnerDependencies }

// NewRunner is the production constructor used by the target command.
func NewRunner() *Runner { return NewProductionRunner() }

// NewProductionRunner owns the platform-specific production composition.
// The selected build supplies either the concrete linux/arm development
// runner or a fail-closed stub; fixture injection remains package-private.
func NewProductionRunner() *Runner {
	dependencies, err := newProductionRunnerDependencies()
	if err != nil {
		return &Runner{constructionErr: errors.Join(ErrRunnerConfiguration, err)}
	}
	runner, err := newProductionRunnerWithOptions(productionRunnerOptions{dependencies: dependencies})
	if err != nil {
		return &Runner{constructionErr: errors.Join(ErrRunnerConfiguration, err)}
	}
	return runner
}

// productionRunnerOptions is the platform-neutral constructor seam used by
// package fixtures. The ARM constructor supplies the real adapters; fixture
// tests may substitute anonymous adapters without exposing them through the
// stable command API.
type productionRunnerOptions struct{ dependencies runnerDependencies }

func newProductionRunnerWithOptions(options productionRunnerOptions) (*Runner, error) {
	if err := validateProductionRunnerDependencies(options.dependencies); err != nil {
		return nil, err
	}
	return &Runner{implementation: &runnerImplementation{dependencies: options.dependencies}}, nil
}

// ConstructionError reports a production-composition failure recorded by
// NewProductionRunner. A non-nil result is permanently inert: no preflight or
// run call can reach an adapter after construction failed.
func (r *Runner) ConstructionError() error {
	if r == nil {
		return ErrRunnerConfiguration
	}
	return r.constructionErr
}

func validateProductionRunnerDependencies(d runnerDependencies) error {
	missing := make([]string, 0, 24)
	if d.maintenance == nil {
		missing = append(missing, "maintenance")
	}
	if d.quiescence == nil {
		missing = append(missing, "quiescence")
	}
	if d.designation.implementation == nil {
		missing = append(missing, "designation")
	}
	if d.profile == nil {
		missing = append(missing, "profile")
	}
	if d.store == nil {
		missing = append(missing, "owner store")
	}
	if d.locker == nil {
		missing = append(missing, "owner lock")
	}
	if d.artifact == nil {
		missing = append(missing, "artifact")
	}
	if d.bind == nil {
		missing = append(missing, "artifact bind")
	}
	if d.fifo == nil {
		missing = append(missing, "FIFO")
	}
	if d.observer == nil {
		missing = append(missing, "process observer")
	}
	if d.qualifier == nil {
		missing = append(missing, "qualifier")
	} else if concrete, ok := d.qualifier.(*Qualifier); ok {
		if concrete == nil || concrete.implementation == nil {
			missing = append(missing, "qualifier implementation")
		} else {
			qd := concrete.implementation.dependencies
			if qd.Mapper == nil {
				missing = append(missing, "qualification mapper")
			}
			if qd.MainAbsent == nil {
				missing = append(missing, "qualification Main observer")
			}
			if qd.Policy == nil {
				missing = append(missing, "qualification policy")
			}
			if qd.Subsystems == nil {
				missing = append(missing, "qualification subsystem observer")
			}
			if qd.PressedInput == nil {
				missing = append(missing, "qualification pressed-input observer")
			}
			if qd.Bridge == nil {
				missing = append(missing, "qualification bridge observer")
			}
			if qd.Programming == nil {
				missing = append(missing, "qualification programming observer")
			}
			if qd.Clock == nil {
				missing = append(missing, "qualification clock")
			}
		}
	}
	if d.mapper == nil {
		missing = append(missing, "mailbox mapper")
	}
	if d.results == nil {
		missing = append(missing, "result store")
	}
	if d.readiness == nil {
		missing = append(missing, "Main readiness")
	} else if _, ok := d.readiness.(programmedHandoffVerifier); !ok {
		missing = append(missing, "programmed handoff")
	}
	if d.reboot == nil {
		missing = append(missing, "reboot")
	}
	if d.clock == nil {
		missing = append(missing, "clock")
	}
	if d.bootID == nil {
		missing = append(missing, "boot ID")
	}
	if d.privilege == nil {
		missing = append(missing, "privilege")
	}
	if d.tool == nil {
		missing = append(missing, "tool identity")
	}
	if d.revalidate == nil {
		missing = append(missing, "artifact revalidation")
	}
	if d.dispatchPath == nil {
		missing = append(missing, "artifact dispatch path")
	}
	if d.mailbox == nil && d.mailboxProgress == nil {
		missing = append(missing, "mailbox")
	}
	if d.newSession == nil {
		missing = append(missing, "session allocator")
	}
	if len(missing) != 0 {
		return fmt.Errorf("%w: missing production adapters: %s", ErrRunnerConfiguration, strings.Join(missing, ", "))
	}
	return nil
}

// newFixtureRunner is deliberately package-private.  It is the only
// dependency-injection path and is scoped to anonymous software fixtures.
func newFixtureRunner(dependencies runnerDependencies) *Runner {
	return &Runner{implementation: &runnerImplementation{dependencies: dependencies}}
}

// Preflight performs every pre-intent check and no durable owner/result/FIFO
// operation.  A successful return is the sole indication that the exact
// preflight grammar may be emitted by the CLI.
func (r *Runner) Preflight(ctx context.Context, request Request) error {
	if ctx == nil {
		ctx = context.Background()
	}
	prepared, failure := r.prepare(ctx, request, true)
	if failure != nil {
		return failure
	}
	if err := r.releasePrepared(ctx, prepared, nil); err != nil {
		return newFailure(CodeOwnershipConflict, "preflight cleanup failed", false, err)
	}
	if err := ctx.Err(); err != nil {
		return newFailure(CodeOwnershipConflict, "operation canceled", false, err)
	}
	return nil
}

// Run implements the required one-result lifecycle surface.  Pre-intent
// failures are returned as an invalid, path-free Result carrying the stable
// primary code; callers needing the process status use RunCommand.
func (r *Runner) Run(ctx context.Context, request Request) Result {
	result, _ := r.RunCommand(ctx, request)
	return result
}

// RunCommand is the CLI-facing detailed operation. Before durable intent it
// returns a typed Failure and no result is created. After intent it returns
// the primary Result; a non-nil Failure then denotes the first terminal
// recovery/store error rather than replacing that result's primary outcome.
func (r *Runner) RunCommand(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	prepared, failure := r.prepare(ctx, request, false)
	if failure != nil {
		return failureResult(failure), failure
	}
	return r.executePrepared(ctx, request, prepared)
}

type preparedRun struct {
	manifest          Manifest
	binding           ArtifactBinding
	baseline          []ProcessIdentity
	started           time.Time
	previous          hardwareowner.Record
	currentBootID     string
	priorBootReclaim  bool
	unlock            hardwareowner.Unlock
	maintenance       MaintenanceUnlock
	maintenanceStatus MaintenanceStatus
	intent            hardwareowner.Record
	current           hardwareowner.Record
	intentCommitted   bool
	lockLost          bool
}

func (r *Runner) prepare(ctx context.Context, request Request, preflight bool) (*preparedRun, *Failure) {
	d, ok := r.dependencies()
	if !ok {
		return nil, newFailure(CodeProfileDisabled, "runner is unavailable", false, ErrRunnerConfiguration)
	}
	if err := ctx.Err(); err != nil {
		return nil, newFailure(CodeProfileDisabled, "operation canceled", false, err)
	}
	if d.maintenance == nil {
		return nil, newFailure(CodeOwnershipConflict, "maintenance gate is unavailable", false, ErrRunnerConfiguration)
	}
	maintenanceStatus, maintenanceUnlock, err := d.maintenance.Enter(ctx)
	if err != nil || maintenanceUnlock == nil {
		var unlockErr error
		if maintenanceUnlock != nil {
			unlockErr = maintenanceUnlock.Unlock()
		}
		return nil, newFailure(CodeOwnershipConflict, "maintenance gate is unavailable", false, errors.Join(err, unlockErr))
	}
	if err := maintenanceStatus.Validate(); err != nil {
		return nil, newFailure(CodeOwnershipConflict, "maintenance status is unavailable", false, errors.Join(err, maintenanceUnlock.Unlock()))
	}
	prepared := &preparedRun{maintenance: maintenanceUnlock, maintenanceStatus: maintenanceStatus}
	failMaintenance := func(code Code, detail string, cause error) (*preparedRun, *Failure) {
		if unlockErr := maintenanceUnlock.Unlock(); unlockErr != nil {
			cause = errors.Join(cause, unlockErr)
		}
		return nil, newFailure(code, detail, false, cause)
	}
	if err := ctx.Err(); err != nil {
		return failMaintenance(CodeOwnershipConflict, "operation canceled", err)
	}
	if d.profile == nil {
		return failMaintenance(CodeProfileDisabled, "development profile is disabled", ErrProfileDisabled)
	}
	if err := callContext(ctx, d.profile.Verify); err != nil {
		return failMaintenance(CodeProfileDisabled, "development profile is disabled", err)
	}
	if d.designation.implementation == nil {
		return failMaintenance(CodeDesignationFailed, "private designation could not be verified", ErrRunnerConfiguration)
	}
	if err := callContext(ctx, d.designation.Verify); err != nil {
		return failMaintenance(CodeDesignationFailed, "private designation could not be verified", err)
	}
	if d.privilege == nil || !d.privilege() {
		return failMaintenance(CodePrivilegeRequired, "root privilege is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return failMaintenance(CodePrivilegeRequired, "operation canceled", err)
	}
	if d.locker == nil || d.store == nil {
		return failMaintenance(CodeOwnershipConflict, "owner admission is unavailable", ErrRunnerConfiguration)
	}
	unlock, err := d.locker.Lock(ctx)
	if err != nil || unlock == nil {
		if unlock != nil {
			err = errors.Join(err, unlock())
		}
		return failMaintenance(CodeOwnershipConflict, "owner admission is unavailable", err)
	}
	started := time.Time{}
	if d.clock != nil {
		started = d.clock.Now()
	}
	prepared.unlock, prepared.started = unlock, started
	fail := func(code Code, detail string, cause error) (*preparedRun, *Failure) {
		cleanupErr := r.releasePrepared(ctx, prepared, nil)
		cause = errors.Join(cause, cleanupErr)
		return nil, newFailure(code, detail, false, cause)
	}
	if err := ctx.Err(); err != nil {
		return fail(CodeOwnershipConflict, "operation canceled", err)
	}
	record, exists, err := d.store.Load()
	if err != nil {
		return fail(CodeOwnershipConflict, "owner record is unavailable", err)
	}
	if err := ctx.Err(); err != nil {
		return fail(CodeOwnershipConflict, "operation canceled", err)
	}
	if !exists {
		return fail(CodeOwnershipConflict, "owner record is unavailable", hardwareowner.ErrOwnerRecordAbsent)
	}
	if err := record.Validate(); err != nil {
		return fail(CodeOwnershipConflict, "owner record is invalid", err)
	}
	if d.quiescence == nil {
		return fail(CodeOwnershipConflict, "quiescence proof is unavailable", ErrRunnerConfiguration)
	}
	bootID, err := r.currentBootID(d)
	if err != nil {
		return fail(CodeOwnershipConflict, "owner admission is fenced", err)
	}
	if record.BootID != bootID {
		if !isPriorBootCompatMainOwner(record) {
			return fail(CodeOwnershipConflict, "owner admission is fenced", hardwareowner.ErrOwnerWrongBoot)
		}
		verifier, ok := d.quiescence.(priorBootQuiescenceVerifier)
		if !ok {
			return fail(CodeOwnershipConflict, "owner admission is fenced", hardwareowner.ErrOwnerWrongBoot)
		}
		if err := verifier.VerifyPriorBoot(ctx, prepared.maintenanceStatus, record, bootID); err != nil {
			return fail(CodeOwnershipConflict, "quiescence proof is unavailable", err)
		}
		prepared.priorBootReclaim = true
	} else if err := d.quiescence.VerifyPreDispatch(ctx, prepared.maintenanceStatus, record); err != nil {
		return fail(CodeOwnershipConflict, "quiescence proof is unavailable", err)
	}
	if err := ctx.Err(); err != nil {
		return fail(CodeOwnershipConflict, "operation canceled", err)
	}
	if record.State != hardwareowner.StateNormalMain && !prepared.priorBootReclaim {
		return fail(CodeOwnershipConflict, "owner admission is fenced", hardwareowner.ErrOwnerNotNormal)
	}
	if d.readiness == nil {
		return fail(CodeOwnershipConflict, "Main readiness is unavailable", ErrRunnerConfiguration)
	}
	if err := callContext(ctx, d.readiness.Verify); err != nil {
		return fail(CodeOwnershipConflict, "Main readiness is unavailable", err)
	}
	if d.tool == nil {
		return fail(CodeOwnershipConflict, "development tool identity is unavailable", ErrRunnerConfiguration)
	}
	if err := callContext(ctx, d.tool); err != nil {
		return fail(CodeOwnershipConflict, "development tool identity is unavailable", err)
	}
	manifest, binding, err := r.bindRequest(ctx, d, request)
	if err != nil {
		if binding.state != nil {
			err = errors.Join(err, r.closeBinding(ctx, &binding))
		}
		return fail(CodeManifestRejected, "staged manifest is invalid", err)
	}
	if err := ctx.Err(); err != nil {
		if binding.state != nil {
			err = errors.Join(err, r.closeBinding(ctx, &binding))
		}
		return fail(CodeManifestRejected, "operation canceled", err)
	}
	if err := manifest.Validate(); err != nil {
		return fail(CodeManifestRejected, "staged manifest is invalid", errors.Join(err, r.closeBinding(ctx, &binding)))
	}
	if binding.state == nil {
		return fail(CodeManifestRejected, "staged artifact binding is unavailable", ErrRunnerConfiguration)
	}
	metadata := binding.Metadata()
	if metadata.Device == 0 || metadata.Inode == 0 || metadata.Size != int64(manifest.ArtifactSize) || metadata.SHA256 != manifest.ArtifactSHA256 || metadata.Path != request.ArtifactPath {
		cleanupErr := r.closeBinding(ctx, &binding)
		return fail(CodeManifestRejected, "staged artifact binding is invalid", errors.Join(ErrInvalidReceipt, cleanupErr))
	}
	prepared.manifest, prepared.binding = manifest, binding
	if prepared.priorBootReclaim && record.RunID != "" && manifest.RunID == record.RunID {
		return fail(CodeResultConflict, "run identity is not fresh", nil)
	}
	if d.results == nil {
		return fail(CodeResultConflict, "result store is unavailable", ErrRunnerConfiguration)
	}
	if conflict, err := resultConflict(d.results, manifest.RunID); err != nil {
		return fail(CodeResultConflict, "result path is unavailable", err)
	} else if conflict {
		return fail(CodeResultConflict, "result already exists", nil)
	}
	prepared.previous, prepared.currentBootID = record, bootID
	if preflight {
		return prepared, nil
	}
	if d.observer == nil {
		return fail(CodeOwnershipConflict, "Main process observer is unavailable", ErrRunnerConfiguration)
	}
	baseline, err := snapshotMainBaseline(ctx, d.observer)
	if err != nil {
		return fail(CodeOwnershipConflict, "Main process baseline is unavailable", err)
	}
	if err := ctx.Err(); err != nil {
		return fail(CodeOwnershipConflict, "operation canceled", err)
	}
	if err := validateQualificationBaseline(baseline); err != nil {
		return fail(CodeOwnershipConflict, "Main process baseline is invalid", err)
	}
	prepared.baseline = append([]ProcessIdentity(nil), baseline...)
	if err := ctx.Err(); err != nil {
		return fail(CodeOwnershipConflict, "operation canceled", err)
	}
	return prepared, nil
}

func snapshotMainBaseline(ctx context.Context, observer processObserver) ([]ProcessIdentity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if contextAware, ok := observer.(contextProcessSnapshotter); ok {
		baseline, err := contextAware.SnapshotContext(ctx)
		if err != nil {
			return nil, err
		}
		return baseline, ctx.Err()
	}
	baseline, err := observer.Snapshot()
	if err != nil {
		return nil, err
	}
	return baseline, ctx.Err()
}

func (r *Runner) bindRequest(ctx context.Context, d runnerDependencies, request Request) (Manifest, ArtifactBinding, error) {
	if d.bind != nil {
		return d.bind(ctx, request)
	}
	if d.artifact == nil {
		return Manifest{}, ArtifactBinding{}, ErrRunnerConfiguration
	}
	access, ok := d.artifact.(productionArtifactBinder)
	if !ok {
		return Manifest{}, ArtifactBinding{}, ErrRunnerConfiguration
	}
	return productionBind(access, productionBindOptions{ExpectedUID: d.manifestUID})(ctx, request)
}

func (r *Runner) executePrepared(ctx context.Context, request Request, prepared *preparedRun) (Result, error) {
	d, _ := r.dependencies()
	manifest := prepared.manifest
	previous := prepared.previous
	// Allocate the new development tuple before writing intent.  Overflow or a
	// failed fresh-session source cannot alter normal_main.
	if previous.GenerationHighWater == ^uint64(0) || d.newSession == nil {
		return r.preIntentAfterPrepared(ctx, prepared, newFailure(CodeGenerationFailed, "fresh generation is unavailable", false, nil))
	}
	session, err := d.newSession()
	if err != nil || !manifestRunIDPattern.MatchString(session) || session == previous.ActiveSession || session == previous.CandidateSession {
		return r.preIntentAfterPrepared(ctx, prepared, newFailure(CodeGenerationFailed, "fresh session is unavailable", false, err))
	}
	if err := ctx.Err(); err != nil {
		return r.preIntentAfterPrepared(ctx, prepared, newFailure(CodeGenerationFailed, "operation canceled", false, err))
	}
	generation := previous.GenerationHighWater + 1
	intent := previous
	// A prior-boot compat_main owner is admitted only through the current
	// journal and live-Main proof in prepare. This includes the exact
	// recovery_required residue left when a prior development handoff failed.
	// Commit its migration-only reclaim at the existing first durable boundary
	// so Preflight remains read-only and every later fence, including FaultKill,
	// belongs to the current boot.
	intent.BootID = prepared.currentBootID
	intent.State = hardwareowner.StateRecoveringIntent
	intent.Phase = hardwareowner.PhaseIntentCommitted
	intent.RunID = manifest.RunID
	intent.GenerationHighWater = generation
	intent.CandidateSession = session
	intent.CandidateGeneration = generation
	intent.CandidateMode = hardwareowner.ModeUpdating
	intent.QuiescingOwner = hardwareowner.OwnerCompatMain
	intent.CandidateOwner = hardwareowner.OwnerFPGADev
	intent.ActiveOwner = hardwareowner.OwnerCompatMain
	intent.RequestedResources = append([]string(nil), hardwareowner.DevelopmentLeases()...)
	intent.FirstFailure = ""
	if err := r.replaceRecord(previous, intent); err != nil {
		return r.preIntentAfterPrepared(ctx, prepared, newFailure(CodeStateStoreFailed, "owner intent could not be persisted", false, err))
	}
	// A successful replacement is the durable intent boundary. Any
	// cancellation observed now is handled by the fenced post-intent path.
	prepared.intentCommitted, prepared.intent, prepared.current = true, intent, intent
	primaryCode := CodeOK
	primaryDetail := ""
	current := intent
	var observation Observation
	var operationErr error
	var secondaryCleanupErr error
	var secondaryCauseErr error
	dispatchAttempted := false
	// The synchronous FIFO is invoked exactly once.  Every returned error is
	// conservatively considered potentially consumed.
	dispatchCtx, dispatchCancel := context.WithTimeout(ctx, mainHandoffTimeout)
	defer dispatchCancel()
	var handoffCtx context.Context
	if d.revalidate == nil || d.dispatchPath == nil || d.fifo == nil || d.quiescence == nil {
		operationErr = ErrRunnerConfiguration
		primaryCode = CodeLoadDispatchFailed
		primaryDetail = "Main load dispatch failed"
	} else if err := callContext(dispatchCtx, func(c context.Context) error { return d.revalidate(c, &prepared.binding) }); err != nil {
		operationErr, primaryCode, primaryDetail = err, CodeLoadDispatchFailed, "artifact binding could not be revalidated"
	} else if proofErr := callContext(dispatchCtx, func(proofCtx context.Context) error {
		// Re-verify the retained semantic maintenance proof after the
		// durable intent and artifact revalidation, immediately before the
		// FIFO handoff. A proof that changes in this window must remain at
		// intent_committed and enter recovery without invoking Main.
		if prepared.priorBootReclaim {
			verifier, ok := d.quiescence.(priorBootQuiescenceVerifier)
			if !ok {
				return hardwareowner.ErrOwnerWrongBoot
			}
			if err := verifier.VerifyPriorBoot(proofCtx, prepared.maintenanceStatus, prepared.previous, prepared.currentBootID); err != nil {
				return err
			}
			if d.readiness == nil {
				return ErrRunnerConfiguration
			}
			return d.readiness.Verify(proofCtx)
		}
		return d.quiescence.VerifyPreDispatch(proofCtx, prepared.maintenanceStatus, current)
	}); proofErr != nil {
		operationErr, primaryCode, primaryDetail = proofErr, CodeLoadDispatchFailed, "quiescence proof is unavailable"
	} else if err := callContext(dispatchCtx, func(c context.Context) error {
		// This is deliberately after the semantic quiescence proof. It closes
		// the artifact replacement window immediately before publication of the
		// named capability and the single synchronous FIFO write.
		return d.revalidate(c, &prepared.binding)
	}); err != nil {
		operationErr, primaryCode, primaryDetail = err, CodeLoadDispatchFailed, "artifact binding changed before dispatch"
	} else {
		var capability string
		capErr := callContext(dispatchCtx, func(c context.Context) error {
			var err error
			capability, err = d.dispatchPath(c, &prepared.binding)
			return err
		})
		if capErr != nil {
			operationErr, primaryCode, primaryDetail = capErr, CodeLoadDispatchFailed, "artifact binding could not be dispatched"
		} else {
			// From this point Main may hold or reopen the named capability even
			// if the synchronous adapter reports an ambiguous failure. Every
			// later binding close therefore preserves the pathname.
			prepared.binding.retainDispatchPathForMain()
			command := "load_core " + capability + "\n"
			// Mark the invocation immediately before entering the synchronous
			// adapter. A pre-dispatch failure remains at intent_committed;
			// once the adapter is called, even NotInvoked/partial attempts are
			// conservatively fenced by load_attempted.
			dispatchAttempted = true
			_, dispatchErr := boundedFIFO(dispatchCtx, d.fifo, command)
			dispatchCancel()
			operationErr = dispatchErr
			if dispatchErr != nil {
				primaryCode, primaryDetail = CodeLoadDispatchFailed, "Main load dispatch failed"
			} else {
				// The protocol's Main-exit bound starts when the synchronous
				// FIFO dispatch completes. Mandatory load-attempted persistence
				// and the post-dispatch checkpoint consume this fresh window;
				// pre-dispatch validation does not.
				freshHandoffCtx, handoffCancel := context.WithTimeout(ctx, mainHandoffTimeout)
				defer handoffCancel()
				handoffCtx = freshHandoffCtx
			}
		}
	}
	// The phase write is mandatory immediately after an attempted dispatch,
	// even when that invocation failed. No observation occurs before this
	// write. If validation/path acquisition failed before entering the FIFO,
	// preserve the durable intent phase and go straight to recovery.
	if dispatchAttempted {
		loadAttempted := current
		loadAttempted.Phase = hardwareowner.PhaseLoadAttempted
		phaseErr := r.replaceRecord(current, loadAttempted)
		if phaseErr != nil {
			hadPrimary := operationErr != nil
			operationErr = errors.Join(operationErr, phaseErr)
			if !hadPrimary {
				operationErr, primaryCode, primaryDetail = phaseErr, CodeStateStoreFailed, "load-attempted state could not be persisted"
			} else {
				secondaryCauseErr = errors.Join(secondaryCauseErr, phaseErr)
			}
		}
		if phaseErr == nil {
			current = loadAttempted
			prepared.current = current
		}
	}
	if checkpointErr := r.afterDispatch(ctx, d, prepared, current); checkpointErr != nil {
		hadPrimary := operationErr != nil
		operationErr = errors.Join(operationErr, checkpointErr)
		if !hadPrimary {
			primaryCode, primaryDetail = CodeLoadDispatchFailed, "development load handoff failed"
		} else {
			secondaryCauseErr = errors.Join(secondaryCauseErr, checkpointErr)
		}
	}
	if prepared.lockLost {
		// A canceled fault checkpoint that cannot reacquire the owner lock has
		// no authority for any further durable mutation. Close the retained
		// artifact descriptor, return the fenced in-memory outcome, and leave
		// recovery/reboot to the next owner of the durable record.
		operationErr = errors.Join(operationErr, r.releasePrepared(ctx, prepared, nil))
		return makeResult(manifest, current, primaryCode, primaryDetail, observation, r.elapsedMSFrom(prepared.started)), newFailure(primaryCode, primaryDetail, true, operationErr)
	}
	if operationErr == nil {
		handoff, ok := d.readiness.(programmedHandoffVerifier)
		var waitErr error
		if !ok {
			waitErr = ErrRunnerConfiguration
		} else {
			waitErr = handoff.WaitProgrammed(handoffCtx)
			if waitErr == nil {
				waitErr = handoffCtx.Err()
			}
		}
		if waitErr != nil {
			operationErr, primaryCode, primaryDetail = waitErr, CodeMainHandoffTimeout, "FPGA programming handoff timed out"
		}
		if operationErr == nil {
			qualificationCtx, qualificationCancel := context.WithTimeout(ctx, qualificationTimeout)
			noOwner, qualifyErr := r.qualifyAndNoOwner(qualificationCtx, d, current, manifest, prepared)
			// Replace is the authoritative boundary even when the injected store
			// does not observe contexts. Adopt its successfully persisted record
			// before sampling the deadline so recovery can never resurrect the
			// compat_main tuple after durable no_owner.
			if qualifyErr == nil {
				current = noOwner
				prepared.current = noOwner
			}
			qualificationDeadlineErr := qualificationCtx.Err()
			qualificationCancel()
			if qualifyErr == nil && qualificationDeadlineErr != nil {
				qualifyErr = qualificationDeadlineErr
			}
			if qualifyErr != nil {
				operationErr, primaryDetail = qualifyErr, "no-owner qualification failed"
				if errors.Is(qualifyErr, errOwnerStateStore) {
					primaryCode = CodeStateStoreFailed
				} else {
					primaryCode = CodeNoOwnerQualificationFailed
				}
			} else {
				if err := ctx.Err(); err != nil {
					operationErr, primaryCode, primaryDetail = err, CodeNoOwnerQualificationFailed, "no-owner qualification failed"
				}
			}
		}
	}
	if operationErr == nil {
		if err := ctx.Err(); err != nil {
			operationErr, primaryCode, primaryDetail = err, CodeMessageTimeout, "mailbox operation timed out"
		}
	}
	if operationErr == nil {
		active := current
		active.State = hardwareowner.StateFPGADefaultActive
		active.Phase = hardwareowner.PhaseLeaseActive
		active.ActiveSession = current.CandidateSession
		active.ActiveGeneration = current.CandidateGeneration
		active.ActiveMode = hardwareowner.ModeUpdating
		active.ActiveOwner = hardwareowner.OwnerFPGADev
		active.ActiveLeases = append([]string(nil), hardwareowner.DevelopmentLeases()...)
		active.CandidateSession = ""
		active.CandidateGeneration = 0
		active.CandidateMode = hardwareowner.ModeNone
		active.CandidateOwner = hardwareowner.OwnerNone
		active.QuiescingOwner = hardwareowner.OwnerNone
		active.RequestedResources = []string{}
		if err := r.replaceRecord(current, active); err != nil {
			operationErr, primaryCode, primaryDetail = err, CodeStateStoreFailed, "development lease could not be persisted"
		} else {
			current = active
			prepared.current = active
			if err := ctx.Err(); err != nil {
				operationErr, primaryCode, primaryDetail = err, CodeMessageTimeout, "mailbox operation timed out"
			}
		}
	}
	if operationErr == nil {
		if d.mapper == nil || (d.mailbox == nil && d.mailboxProgress == nil) {
			operationErr, primaryCode, primaryDetail = ErrRunnerConfiguration, CodeMMIOFailed, "mailbox mapping is unavailable"
		} else if err := ctx.Err(); err != nil {
			operationErr, primaryCode, primaryDetail = err, CodeMessageTimeout, "mailbox operation timed out"
		} else {
			registers, openErr := d.mapper.OpenMailbox()
			ctxErr := ctx.Err()
			if openErr != nil {
				var closeErr error
				if registers != nil {
					closeErr = registers.Close()
				}
				operationErr = errors.Join(openErr, closeErr, ctxErr)
				if ctxErr != nil {
					primaryCode, primaryDetail = CodeMessageTimeout, "mailbox operation timed out"
				} else {
					primaryCode, primaryDetail = CodeMMIOFailed, "mailbox mapping failed"
				}
			} else if ctxErr != nil {
				var closeErr error
				if registers != nil {
					closeErr = registers.Close()
				}
				operationErr, primaryCode, primaryDetail = errors.Join(ctxErr, closeErr), CodeMessageTimeout, "mailbox operation timed out"
			} else if registers == nil {
				operationErr, primaryCode, primaryDetail = ErrRunnerConfiguration, CodeMMIOFailed, "mailbox mapping failed"
			} else {
				owned := &ownedRegisters{Registers: registers}
				mailboxCtx, cancel := context.WithTimeout(ctx, mailboxTotalTimeout)
				var mailboxClock Clock = mailboxRealClock{}
				if candidate, ok := d.clock.(Clock); ok {
					mailboxClock = candidate
				}
				var mailboxErr error
				if d.mailboxProgress != nil {
					observation, mailboxErr = r.runMailboxWithProgress(mailboxCtx, d, owned, mailboxClock, &current, prepared)
				} else {
					observation, mailboxErr = d.mailbox(mailboxCtx, owned, mailboxClock)
				}
				mailboxOperationErr, mailboxCleanupErr := splitMailboxExecutionError(mailboxErr)
				// A legacy or hostile mailbox seam must not make returned
				// evidence claim a protocol phase that the owner record never
				// durably reached. The callback-aware path already carries the
				// exact accepted DONE boundary; this guard covers the retained
				// compatibility seam as well.
				if current.Phase != hardwareowner.PhaseDoneObserved {
					observation.TerminalWord = 0
				}
				mailboxDeadlineErr := mailboxCtx.Err()
				cancel()
				if mailboxOperationErr == nil && mailboxDeadlineErr != nil {
					mailboxOperationErr = mailboxDeadlineErr
				}
				closeErr := owned.Close()
				if mailboxCleanupErr == nil && closeErr != nil {
					// Injected package seams may leave Close to the runner. Keep that
					// separately observed cleanup occurrence independent too.
					mailboxCleanupErr = closeErr
				}
				operationErr = errors.Join(mailboxOperationErr, mailboxCleanupErr)
				if mailboxOperationErr != nil && mailboxCleanupErr != nil {
					secondaryCleanupErr = errors.Join(secondaryCleanupErr, mailboxCleanupErr)
				}
				if operationErr != nil {
					classificationErr := mailboxOperationErr
					if classificationErr == nil {
						classificationErr = mailboxCleanupErr
					}
					if errors.Is(classificationErr, errOwnerStateStore) {
						primaryCode, primaryDetail = CodeStateStoreFailed, "result phase could not be persisted"
					} else {
						primaryCode, primaryDetail = classifyMailboxFailure(classificationErr)
					}
				}
			}
		}
	}
	// Preserve the binding until Main handoff and qualification have consumed
	// it.  There is exactly one close call on every path from this point.
	if prepared.binding.state != nil {
		if closeErr := r.closeBinding(ctx, &prepared.binding); closeErr != nil {
			closeErr = cleanupErrorDelta(closeErr)
			if closeErr != nil {
				wasOperationError := operationErr != nil
				operationErr = errors.Join(operationErr, closeErr)
				if !wasOperationError {
					primaryCode, primaryDetail = CodeMMIOFailed, "artifact binding cleanup failed"
				} else {
					secondaryCleanupErr = errors.Join(secondaryCleanupErr, closeErr)
				}
			}
		}
	}
	if operationErr == nil {
		if err := ctx.Err(); err != nil {
			operationErr, primaryCode, primaryDetail = err, CodeMessageTimeout, "mailbox operation timed out"
		}
	}
	prepared.current = current
	if operationErr == nil {
		if err := ctx.Err(); err != nil {
			operationErr, primaryCode, primaryDetail = err, CodeMessageTimeout, "mailbox operation timed out"
		}
	}
	if err := ctx.Err(); err != nil && primaryCode == CodeOK {
		operationErr, primaryCode, primaryDetail = err, CodeMessageTimeout, "development operation timed out"
	}
	// Fence the final owner state before creating the sole result.  The result
	// must describe the last durable owner phase, and a failed fence must never
	// be hidden by a later successful-looking result.
	if deadlineErr := ctx.Err(); deadlineErr != nil && primaryCode == CodeOK {
		primaryCode, primaryDetail = CodeMessageTimeout, "development operation timed out"
		operationErr = errors.Join(operationErr, deadlineErr)
	}
	priorPrimary := primaryCode
	var recoveryErr error
	var recoveryCode Code
	var recoveryDetail string
	recovery := recoveryRecord(current, priorPrimary)
	if err := r.replaceRecord(current, recovery); err != nil {
		if priorPrimary == CodeOK {
			primaryCode, primaryDetail = CodeStateStoreFailed, "recovery fence could not be persisted"
		}
		operationErr = errors.Join(operationErr, err)
		recoveryErr, recoveryCode, recoveryDetail = err, CodeStateStoreFailed, "recovery fence could not be persisted"
	} else {
		current = recovery
		prepared.current = recovery
	}
	// Once recovery_required is durable the fence itself protects every
	// admission path, so release the shared owner lock before the independent
	// result create and reboot. An unlock failure is still a terminal
	// state-store failure and is known before the result is constructed.
	if unlockErr := r.releasePrepared(ctx, prepared, nil); unlockErr != nil {
		if priorPrimary == CodeOK && recoveryErr == nil {
			primaryCode, primaryDetail = CodeStateStoreFailed, "owner lock cleanup failed"
		}
		operationErr = errors.Join(operationErr, unlockErr)
		if recoveryErr == nil {
			recoveryErr, recoveryCode, recoveryDetail = unlockErr, CodeStateStoreFailed, "owner lock cleanup failed"
		}
	}

	markDeadlinePrimary := func() bool {
		if deadlineErr := ctx.Err(); deadlineErr != nil && primaryCode == CodeOK {
			primaryCode, primaryDetail = CodeMessageTimeout, "development operation timed out"
			operationErr = errors.Join(operationErr, deadlineErr)
			if recoveryErr == nil {
				recoveryErr, recoveryCode, recoveryDetail = deadlineErr, CodeMessageTimeout, "development operation timed out"
			}
			return true
		}
		return false
	}
	// The caller's deadline remains authoritative until the exclusive result
	// create starts. A canceled successful run must be persisted as a failed
	// terminal outcome rather than allowing a success to escape at the
	// deadline.
	markDeadlinePrimary()
	result := makeResult(manifest, current, primaryCode, primaryDetail, observation, r.elapsedMSFrom(prepared.started))
	if err := result.Validate(); err != nil {
		if primaryCode == CodeOK {
			primaryCode, primaryDetail = CodeStateStoreFailed, "terminal result is invalid"
		}
		if recoveryErr == nil {
			recoveryErr, recoveryCode, recoveryDetail = err, CodeStateStoreFailed, "terminal result is invalid"
		}
		operationErr = errors.Join(operationErr, err)
		result = makeResult(manifest, current, primaryCode, primaryDetail, observation, r.elapsedMSFrom(prepared.started))
	}
	if markDeadlinePrimary() {
		result = makeResult(manifest, current, primaryCode, primaryDetail, observation, r.elapsedMSFrom(prepared.started))
		if err := result.Validate(); err != nil {
			operationErr = errors.Join(operationErr, err)
			if recoveryErr == nil {
				recoveryErr, recoveryCode, recoveryDetail = err, CodeStateStoreFailed, "terminal result is invalid"
			}
		}
	}
	// Check immediately before entering the synchronous result writer too;
	// cleanup and validation may have consumed the remaining deadline.
	if markDeadlinePrimary() {
		result = makeResult(manifest, current, primaryCode, primaryDetail, observation, r.elapsedMSFrom(prepared.started))
	}
	var resultCreateErr error
	if d.results == nil {
		resultCreateErr = ErrRunnerConfiguration
	} else {
		// Create is deliberately called exactly once.  A failed create has no
		// substitute write; callers must reconnect and verify recovery.
		resultCreateErr = d.results.Create(result)
	}
	resultCreated := resultCreateErr == nil
	if resultCreateErr != nil {
		// A missing terminal result is itself the only result callers can
		// observe. Make that in-memory outcome unambiguous regardless of an
		// earlier operation failure; the earlier cause remains private in the
		// returned error chain and the durable owner fence.
		primaryCode, primaryDetail = CodeStateStoreFailed, "terminal result could not be persisted"
		result = makeResult(manifest, current, primaryCode, primaryDetail, observation, r.elapsedMSFrom(prepared.started))
		operationErr = errors.Join(operationErr, ErrResultUnavailable, resultCreateErr)
	}

	var rebootErr error
	if d.reboot == nil {
		rebootErr = ErrRunnerConfiguration
	} else {
		// Reboot is a recovery action, not part of the caller's cancelable
		// operation. Always provide it a fresh bounded context.
		rebootCtx, cancel := context.WithTimeout(context.Background(), rebootTimeout)
		rebootErr = d.reboot.Request(rebootCtx)
		if rebootErr == nil {
			rebootErr = rebootCtx.Err()
		}
		cancel()
	}
	if rebootErr != nil {
		if recoveryErr == nil {
			recoveryErr, recoveryCode, recoveryDetail = rebootErr, CodeRebootRequestFailed, "reboot request failed"
		}
		if resultCreated {
			if markErr := d.results.MarkRecoveryFailed(result); markErr != nil {
				operationErr = errors.Join(operationErr, markErr)
			} else {
				result.RecoveryRequest = "failed"
			}
		}
		operationErr = errors.Join(operationErr, rebootErr)
	}
	if resultCreateErr != nil {
		return result, newFailure(CodeStateStoreFailed, "terminal result could not be persisted", true, operationErr)
	}
	if recoveryErr != nil {
		return result, newFailure(recoveryCode, recoveryDetail, true, operationErr)
	}
	if secondaryCleanupErr != nil || secondaryCauseErr != nil {
		// Cleanup after an established primary must not rewrite the durable
		// result. Later phase/checkpoint errors follow the same rule: callers
		// inspecting the private chain must not lose any independently observed
		// cause merely because recovery and reboot succeeded.
		return result, newFailure(primaryCode, primaryDetail, true, operationErr)
	}
	return result, nil
}

func isPriorBootCompatMainOwner(record hardwareowner.Record) bool {
	if record.State == hardwareowner.StateNormalMain {
		return true
	}
	return record.State == hardwareowner.StateRecoveryRequired &&
		(record.Phase == hardwareowner.PhaseIntentCommitted || record.Phase == hardwareowner.PhaseLoadAttempted) &&
		record.FirstFailure != "" &&
		record.ActiveOwner == hardwareowner.OwnerCompatMain &&
		record.ActiveMode == hardwareowner.ModeFPGANative &&
		record.QuiescingOwner == hardwareowner.OwnerCompatMain &&
		record.CandidateSession != "" &&
		record.CandidateSession != record.ActiveSession &&
		record.CandidateGeneration > record.ActiveGeneration &&
		record.CandidateOwner == hardwareowner.OwnerFPGADev &&
		record.CandidateMode == hardwareowner.ModeUpdating &&
		equalPriorBootResources(record.RequestedResources, hardwareowner.DevelopmentLeases())
}

func equalPriorBootResources(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (r *Runner) runMailboxWithProgress(ctx context.Context, d runnerDependencies, regs Registers, clock Clock, current *hardwareowner.Record, prepared *preparedRun) (Observation, error) {
	if d.mailboxProgress == nil || current == nil || prepared == nil {
		return Observation{}, ErrRunnerConfiguration
	}
	progressIndex := 0
	doneAccepted := false
	var doneTerminalWord uint32
	var doneObservation Observation
	wantProgress := []string{mailboxProgressHello, mailboxProgressData, mailboxProgressEnd, mailboxProgressDone}
	callback := func(progress mailboxProgress) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if progressIndex >= len(wantProgress) || progress.Phase != wantProgress[progressIndex] {
			return fmt.Errorf("%w: mailbox progress boundary %q is out of order", errMailboxProtocol, progress.Phase)
		}
		if progress.Phase == mailboxProgressDone {
			if progress.TerminalWord != uint32(0xd3130c00) || progress.Observation.TerminalWord != 0 {
				return fmt.Errorf("%w: DONE progress lacks zero-until-accepted terminal evidence", errMailboxProtocol)
			}
		}
		phase, ok := mailboxHardwarePhase(progress.Phase)
		if !ok {
			return fmt.Errorf("%w: unknown mailbox progress phase %q", errMailboxProtocol, progress.Phase)
		}
		next := *current
		next.Phase = phase
		if err := r.replaceRecord(*current, next); err != nil {
			return err
		}
		*current = next
		prepared.current = next
		progressIndex++
		if progress.Phase == mailboxProgressDone {
			doneAccepted = true
			doneTerminalWord = progress.TerminalWord
			doneObservation = cloneObservation(progress.Observation)
		}
		// The owner replacement above is the commit point for this protocol
		// boundary. Do not turn a cancellation observed after that successful
		// persistence into a callback failure or discard its evidence.
		return nil
	}
	observation, err := d.mailboxProgress(ctx, regs, clock, callback)
	if err != nil {
		if doneAccepted {
			observation = doneObservation
			observation.TerminalWord = doneTerminalWord
		} else if doneTerminalWord == 0 {
			observation.TerminalWord = 0
		} else if observation.TerminalWord == 0 {
			observation.TerminalWord = doneTerminalWord
		}
		return observation, err
	}
	if !doneAccepted {
		observation.TerminalWord = 0
		return observation, fmt.Errorf("%w: mailbox completed without a durable DONE boundary", errMailboxProtocol)
	}
	if observation.TerminalWord != doneTerminalWord || !bytes.Equal(observation.Payload, doneObservation.Payload) {
		// The callback's observation is the last accepted durable protocol
		// evidence.  Keep that evidence self-consistent when a package-local
		// adapter returns a conflicting summary, but report the adapter
		// violation so callers cannot treat it as a successful transaction.
		observation = doneObservation
		observation.TerminalWord = doneTerminalWord
		return observation, fmt.Errorf("%w: mailbox completion summary differs from the accepted DONE evidence", errMailboxProtocol)
	}
	return observation, nil
}

func mailboxHardwarePhase(value string) (hardwareowner.Phase, bool) {
	switch value {
	case mailboxProgressHello:
		return hardwareowner.PhaseHelloObserved, true
	case mailboxProgressData:
		return hardwareowner.PhaseMessagePartial, true
	case mailboxProgressEnd:
		return hardwareowner.PhaseEndAckWritten, true
	case mailboxProgressDone:
		return hardwareowner.PhaseDoneObserved, true
	default:
		return "", false
	}
}

func (r *Runner) qualifyAndNoOwner(ctx context.Context, d runnerDependencies, current hardwareowner.Record, manifest Manifest, prepared *preparedRun) (hardwareowner.Record, error) {
	if d.qualifier == nil {
		return hardwareowner.Record{}, ErrRunnerConfiguration
	}
	if d.quiescence == nil {
		return hardwareowner.Record{}, ErrRunnerConfiguration
	}
	var proof PolicySubsystemProof
	var err error
	if prepared.priorBootReclaim {
		verifier, ok := d.quiescence.(priorBootPostMainVerifier)
		if !ok {
			return hardwareowner.Record{}, hardwareowner.ErrOwnerWrongBoot
		}
		proof, err = verifier.VerifyPriorBootPostMain(ctx, prepared.maintenanceStatus, prepared.previous, current, prepared.currentBootID)
	} else {
		proof, err = d.quiescence.VerifyPostMain(ctx, prepared.maintenanceStatus, current)
	}
	if err != nil {
		return hardwareowner.Record{}, err
	}
	if err := proof.Validate(); err != nil {
		return hardwareowner.Record{}, err
	}
	// The migration-only post-Main proof includes the same pressed-input
	// observation in its validated subsystem proof. Ordinary same-boot
	// qualification retains the separate verifier call unchanged.
	if !prepared.priorBootReclaim {
		if err := d.quiescence.VerifyPressedInput(ctx, prepared.maintenanceStatus, current); err != nil {
			return hardwareowner.Record{}, err
		}
	}
	var receipt Receipt
	if concrete, ok := d.qualifier.(*Qualifier); ok && concrete != nil && concrete.implementation != nil {
		clone := *concrete.implementation
		clone.dependencies.Baseline = append([]ProcessIdentity(nil), prepared.baseline...)
		if prepared.priorBootReclaim {
			if err := enablePriorBootQualification(&clone.dependencies); err != nil {
				return hardwareowner.Record{}, err
			}
		}
		receipt, err = (Qualifier{implementation: &clone}).Qualify(ctx, prepared.binding)
	} else {
		receipt, err = d.qualifier.Qualify(ctx, prepared.binding)
	}
	if err != nil {
		return hardwareowner.Record{}, err
	}
	if err := receipt.Validate(); err != nil {
		return hardwareowner.Record{}, err
	}
	if err := ctx.Err(); err != nil {
		return hardwareowner.Record{}, err
	}
	metadata := prepared.binding.Metadata()
	if receipt.Artifact() != metadata || receipt.Manifest() != manifest {
		return hardwareowner.Record{}, ErrInvalidReceipt
	}
	if err := ctx.Err(); err != nil {
		return hardwareowner.Record{}, err
	}
	noOwner := current
	noOwner.State = hardwareowner.StateNoOwner
	noOwner.Phase = hardwareowner.PhaseMainAbsent
	noOwner.ActiveSession = ""
	noOwner.ActiveGeneration = 0
	noOwner.ActiveMode = hardwareowner.ModeNone
	noOwner.ActiveOwner = hardwareowner.OwnerNone
	noOwner.ActiveLeases = []string{}
	noOwner.QuiescingOwner = hardwareowner.OwnerNone
	if err := r.replaceRecord(current, noOwner); err != nil {
		return hardwareowner.Record{}, err
	}
	return noOwner, nil
}

func (r *Runner) preIntentAfterPrepared(ctx context.Context, prepared *preparedRun, failure *Failure) (Result, error) {
	if cleanupErr := r.releasePrepared(ctx, prepared, nil); cleanupErr != nil {
		failure = newFailure(failure.Code, failure.Detail, false, errors.Join(failure, cleanupErr))
	}
	return failureResult(failure), failure
}

func (r *Runner) releasePrepared(ctx context.Context, prepared *preparedRun, _ *Failure) error {
	if prepared == nil {
		return nil
	}
	d, _ := r.dependencies()
	var errs []error
	if prepared.binding.state != nil {
		if err := r.closeBinding(ctx, &prepared.binding); err != nil {
			errs = append(errs, err)
		}
	}
	if prepared.unlock != nil {
		if err := prepared.unlock(); err != nil {
			errs = append(errs, err)
		}
		prepared.unlock = nil
	}
	if prepared.maintenance != nil {
		if err := prepared.maintenance.Unlock(); err != nil {
			errs = append(errs, err)
		}
		prepared.maintenance = nil
	}
	_ = d
	return errors.Join(errs...)
}

func (r *Runner) closeBinding(ctx context.Context, binding *ArtifactBinding) error {
	d, _ := r.dependencies()
	if binding == nil || binding.state == nil {
		return nil
	}
	if d.closeBinding != nil {
		err := d.closeBinding(ctx, binding)
		binding.state = nil
		return err
	}
	err := binding.Close()
	binding.state = nil
	return err
}

func (r *Runner) replaceRecord(previous, next hardwareowner.Record) error {
	d, ok := r.dependencies()
	if !ok || d.store == nil {
		return ErrRunnerConfiguration
	}
	if err := next.ValidateTransition(previous); err != nil {
		return err
	}
	if err := d.store.Replace(next); err != nil {
		return fmt.Errorf("%w: %w", errOwnerStateStore, err)
	}
	return nil
}

func (r *Runner) currentBootID(d runnerDependencies) (string, error) {
	if d.bootID == nil {
		return "", ErrRunnerConfiguration
	}
	return d.bootID()
}

func (r *Runner) dependencies() (runnerDependencies, bool) {
	if r == nil || r.implementation == nil || r.constructionErr != nil {
		return runnerDependencies{}, false
	}
	return r.implementation.dependencies, true
}

func (r *Runner) elapsedMSFrom(start time.Time) uint64 {
	if start.IsZero() {
		return 0
	}
	d, ok := r.dependencies()
	if !ok || d.clock == nil {
		return 0
	}
	now := d.clock.Now()
	if now.Before(start) {
		return 0
	}
	return uint64(now.Sub(start) / time.Millisecond)
}

func newFailure(code Code, detail string, intent bool, cause error) *Failure {
	if detail == "" {
		detail = string(code)
	}
	return &Failure{Code: code, Detail: detail, IntentCommitted: intent, cause: cause}
}

func failureResult(failure *Failure) Result {
	if failure == nil {
		return Result{}
	}
	return Result{PrimaryCode: string(failure.Code), PrimaryDetail: failure.Detail}
}

func callContext(ctx context.Context, fn func(context.Context) error) error {
	if fn == nil {
		return ErrRunnerConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := fn(ctx)
	if err != nil {
		return err
	}
	return ctx.Err()
}

func boundedFIFO(ctx context.Context, fifo fifoDispatcher, command string) (Attempt, error) {
	if err := ctx.Err(); err != nil {
		return NotInvoked, err
	}
	attempt, err := fifo.Dispatch(ctx, command)
	if err != nil {
		return attempt, err
	}
	// Dispatch is synchronous. A nil result means its write and close
	// completed before return, so a concurrent context expiry cannot turn the
	// already-consumed command into a dispatch failure.
	return attempt, nil
}

func classifyMailboxFailure(err error) (Code, string) {
	switch {
	case errors.Is(err, errMailboxPayloadMismatch):
		return CodePayloadMismatch, "mailbox payload did not match"
	case errors.Is(err, errMailboxProtocol):
		return CodeProtocolViolation, "mailbox protocol violation"
	case errors.Is(err, errMailboxTimeout), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return CodeMessageTimeout, "mailbox message timed out"
	default:
		return CodeMMIOFailed, "mailbox register operation failed"
	}
}

// syntheticCleanupContextError marks only the package-private fixture's
// deliberate repetition of ctx.Err during cleanup. Equal syscall sentinels
// from independent operations are never deduplicated by errors.Is.
type syntheticCleanupContextError struct{ cause error }

func (e syntheticCleanupContextError) Error() string { return e.cause.Error() }
func (e syntheticCleanupContextError) Unwrap() error { return e.cause }

func markSyntheticCleanupContext(err error) error {
	if err == nil {
		return nil
	}
	return syntheticCleanupContextError{cause: err}
}

func cleanupErrorDelta(cleanup error) error {
	if cleanup == nil {
		return nil
	}
	if _, synthetic := cleanup.(syntheticCleanupContextError); synthetic {
		return nil
	}
	if joined, ok := cleanup.(interface{ Unwrap() []error }); ok {
		var delta error
		for _, child := range joined.Unwrap() {
			delta = errors.Join(delta, cleanupErrorDelta(child))
		}
		return delta
	}
	return cleanup
}

func makeResult(manifest Manifest, record hardwareowner.Record, code Code, detail string, observation Observation, elapsed uint64) Result {
	session, generation := record.CandidateSession, record.CandidateGeneration
	if session == "" {
		session, generation = record.ActiveSession, record.ActiveGeneration
	}
	if detail == "" && code != CodeOK {
		detail = string(code)
	}
	payload := append([]byte(nil), observation.Payload...)
	digest := sha256.Sum256(payload)
	terminal := "00000000"
	if observation.TerminalWord != 0 {
		terminal = fmt.Sprintf("%08x", observation.TerminalWord)
	}
	phase := string(record.Phase)
	if observation.TerminalWord != 0 {
		// A stable DONE word is terminal protocol evidence even when a
		// subsequent owner-record phase write failed. Keep the result
		// self-consistent so the accepted terminal evidence is not discarded.
		phase = ResultPhaseDoneObserved
	}
	if phase == "" {
		phase = ResultPhaseIntentCommitted
	}
	return Result{
		Schema: ResultSchemaVersion, RunID: manifest.RunID, Generation: generation,
		Session: session, Mode: ResultModeUpdating, Experiment: manifest.Experiment,
		BuildLane: manifest.BuildLane, ArtifactSHA256: manifest.ArtifactSHA256,
		SourceCommit: manifest.SourceCommit, Phase: phase, PrimaryCode: string(code),
		PrimaryDetail: detail, PayloadHex: hex.EncodeToString(payload), PayloadLength: uint64(len(payload)),
		PayloadSHA256: hex.EncodeToString(digest[:]), TerminalWord: terminal,
		RecoveryRequest: "pending", ElapsedMS: elapsed,
	}
}

func recoveryRecord(current hardwareowner.Record, code Code) hardwareowner.Record {
	recovery := current
	recovery.State = hardwareowner.StateRecoveryRequired
	if code == CodeOK {
		recovery.FirstFailure = ""
	} else {
		recovery.FirstFailure = string(code)
	}
	if current.State == hardwareowner.StateFPGADefaultActive {
		recovery.CandidateSession = current.ActiveSession
		recovery.CandidateGeneration = current.ActiveGeneration
		recovery.CandidateMode = current.ActiveMode
		recovery.CandidateOwner = current.ActiveOwner
		recovery.RequestedResources = append([]string(nil), hardwareowner.DevelopmentLeases()...)
	}
	if current.ActiveGeneration != 0 {
		recovery.QuiescingOwner = current.ActiveOwner
	}
	return recovery
}

type productionBindOptions struct {
	ExpectedUID       uint32
	AfterManifestOpen func()
}

func productionBind(access productionArtifactBinder, supplied ...productionBindOptions) func(context.Context, Request) (Manifest, ArtifactBinding, error) {
	options := productionBindOptions{ExpectedUID: 0}
	if len(supplied) == 1 {
		options = supplied[0]
	} else if len(supplied) > 1 {
		return func(context.Context, Request) (Manifest, ArtifactBinding, error) {
			return Manifest{}, ArtifactBinding{}, ErrRunnerConfiguration
		}
	}
	return func(ctx context.Context, request Request) (Manifest, ArtifactBinding, error) {
		if access == nil {
			return Manifest{}, ArtifactBinding{}, ErrRunnerConfiguration
		}
		if err := ctx.Err(); err != nil {
			return Manifest{}, ArtifactBinding{}, err
		}
		if err := validatePrivatePath(request.ManifestPath); err != nil {
			return Manifest{}, ArtifactBinding{}, errors.New("staged paths are invalid")
		}
		if err := validatePrivatePath(request.ArtifactPath); err != nil {
			return Manifest{}, ArtifactBinding{}, errors.New("staged paths are invalid")
		}
		if filepath.Base(request.ManifestPath) != "manifest.json" || filepath.Base(request.ArtifactPath) != ManifestArtifact || filepath.Dir(request.ManifestPath) != filepath.Dir(request.ArtifactPath) {
			return Manifest{}, ArtifactBinding{}, errors.New("staged paths are invalid")
		}
		staging := filepath.Dir(request.ArtifactPath)
		if filepath.Dir(staging) != "/tmp" {
			return Manifest{}, ArtifactBinding{}, errors.New("staging directory is invalid")
		}
		raw, directory, err := readProductionManifest(ctx, staging, options.ExpectedUID, options.AfterManifestOpen)
		if err != nil {
			return Manifest{}, ArtifactBinding{}, err
		}
		manifest, err := ParseManifest(raw)
		if err != nil {
			return Manifest{}, ArtifactBinding{}, errors.Join(err, directory.close())
		}
		if filepath.Base(staging) != "misteross-fpgadev-"+manifest.RunID {
			return Manifest{}, ArtifactBinding{}, errors.Join(errors.New("staging directory is invalid"), directory.close())
		}
		binding, err := access.bindProductionStagingDirectory(manifest, directory)
		if err != nil {
			return manifest, binding, errors.Join(err, binding.Close(), directory.close())
		}
		if binding.Metadata().Path != request.ArtifactPath {
			closeErr := binding.Close()
			return Manifest{}, ArtifactBinding{}, errors.Join(errors.New("artifact binding path differs"), closeErr)
		}
		if _, evidenceErr := binding.ResourceEvidence(); evidenceErr != nil {
			closeErr := binding.Close()
			return manifest, binding, errors.Join(fmt.Errorf("resource evidence is invalid: %w", evidenceErr), closeErr)
		}
		return manifest, binding, nil
	}
}

func resultConflict(writer resultWriter, runID string) (bool, error) {
	if probe, ok := writer.(interface{ Exists(string) (bool, error) }); ok {
		return probe.Exists(runID)
	}
	if store, ok := writer.(*ResultStore); ok {
		path := store.PathFor(runID)
		if path == "" {
			return false, errors.New("invalid result ID")
		}
		_, err := os.Lstat(path)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return false, errors.New("result collision probe is unavailable")
}

type realRunnerClock struct{}

func (realRunnerClock) Now() time.Time { return time.Now() }

type commandRebooter struct{}

func (commandRebooter) Request(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "/sbin/reboot")
	if err := command.Run(); err != nil {
		return err
	}
	return ctx.Err()
}

type ownedRegisters struct {
	Registers
	once     sync.Once
	closeErr error
}

func (r *ownedRegisters) Close() error {
	r.once.Do(func() { r.closeErr = r.Registers.Close() })
	return r.closeErr
}

func randomSession() (string, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func readKernelBootID() (string, error) {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	boot := strings.TrimSpace(string(raw))
	if boot == "" {
		return "", errors.New("boot ID is unavailable")
	}
	return boot, nil
}
