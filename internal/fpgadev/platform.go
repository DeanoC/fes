package fpgadev

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// The platform adapter's private register offsets and qualification bounds are
// not part of the public FogCast protocol.  The physical addresses themselves
// remain in the Linux-only adapter; recovery reaches only the manager page and
// the four bridge groups listed below.
const (
	// These offsets are part of the platform adapter's private register
	// contract; the physical FPGA-manager address lives only in the Linux file.
	fpgaManagerGPOOffset     = 0x10
	fpgaManagerGPIOffset     = 0x14
	fpgaManagerStatusOffset  = 0x00
	fpgaManagerControlOffset = 0x04

	qualificationStableAbsence = 250 * time.Millisecond
	qualificationTimeout       = 2 * time.Second
	qualificationPollInterval  = 10 * time.Millisecond
	qualificationHelloWord     = uint32(0xd3100000)

	programmingUserModeMask     = uint32(0x7)
	programmingUserModeValue    = uint32(0x4)
	programmingEnableMask       = uint32(1 << 0)
	programmingNConfigPullMask  = uint32(1 << 2)
	programmingNStatusPullMask  = uint32(1 << 3)
	programmingConfDonePullMask = uint32(1 << 4)
	programmingAXICFGENMask     = uint32(1 << 8)
	programmingPullMask         = programmingNConfigPullMask | programmingNStatusPullMask | programmingConfDonePullMask
	programmingForbiddenMask    = programmingEnableMask | programmingPullMask | programmingAXICFGENMask
)

// BridgeGroup identifies one of the four and only four register groups
// allowed during no-owner recovery.  Keep this list closed: adding a group is
// a reviewed hardware-boundary change, not a caller configuration option.
type BridgeGroup string

const (
	BridgeFPGAInterface BridgeGroup = "fpga-interface"
	BridgeSDRPort       BridgeGroup = "sdr-port"
	BridgeModuleReset   BridgeGroup = "bridge-module-reset"
	BridgeNIC301Remap   BridgeGroup = "nic301-remap"
)

var pinnedBridgeGroups = [...]BridgeGroup{
	BridgeFPGAInterface,
	BridgeSDRPort,
	BridgeModuleReset,
	BridgeNIC301Remap,
}

// PinnedBridgeTuple is the exact bridge state applied during qualification.
// It is intentionally not configurable by a caller.
type BridgeTuple struct {
	FPGAInterfaceModule uint32
	SDRPort             uint32
	BridgeModuleReset   uint32
	NIC301Remap         uint32
}

var pinnedBridgeTuple = BridgeTuple{
	FPGAInterfaceModule: 0,
	SDRPort:             0,
	BridgeModuleReset:   7,
	NIC301Remap:         1,
}

// PinnedBridgeDisable returns a copy of the exact tuple used by the adapter.
func PinnedBridgeDisable() BridgeTuple { return pinnedBridgeTuple }

func (t BridgeTuple) equal(other BridgeTuple) bool {
	return t == other
}

// BridgeStateView records one independently observed Linux bridge state.  The
// logical name comes from the entry's name attribute, never from brN.
type BridgeStateView struct {
	Name  string
	State string
}

// BridgeViews records the exact Linux bridge inventory.  A true value means
// that the corresponding logical view has been positively observed disabled;
// Entries retains the source name/state evidence in the receipt.
type BridgeViews struct {
	FPGA2HPSDisabled   bool
	HPS2FPGADisabled   bool
	LWHPS2FPGADisabled bool
	FPGASDRAMDisabled  bool
	Entries            []BridgeStateView
}

func (v BridgeViews) allDisabled() bool {
	if !v.FPGA2HPSDisabled || !v.HPS2FPGADisabled || !v.LWHPS2FPGADisabled || !v.FPGASDRAMDisabled || len(v.Entries) != 4 {
		return false
	}
	want := map[string]bool{
		"hps2fpga":   false,
		"lwhps2fpga": false,
		"fpga2hps":   false,
		"fpga2sdram": false,
	}
	for _, entry := range v.Entries {
		if entry.State != "disabled\n" || want[entry.Name] {
			return false
		}
		if _, ok := want[entry.Name]; !ok {
			return false
		}
		want[entry.Name] = true
	}
	for _, present := range want {
		if !present {
			return false
		}
	}
	return true
}

func cloneBridgeViews(views BridgeViews) BridgeViews {
	views.Entries = append([]BridgeStateView(nil), views.Entries...)
	return views
}

func cloneBridgeProof(proof BridgeProof) BridgeProof {
	proof.Views = cloneBridgeViews(proof.Views)
	return proof
}

// BridgeProof is returned after the four pinned writes, the three readable
// readbacks, the separate write-only L3 issued-write record, and independent
// Linux view inventory have completed. A Receipt embeds only a copy of this
// proof.
type BridgeProof struct {
	Requested     BridgeTuple
	Readback      BridgeTuple
	L3RemapIssued uint32
	Views         BridgeViews
}

func (p BridgeProof) Validate() error {
	if !p.Requested.equal(pinnedBridgeTuple) {
		return errors.New("bridge proof requested tuple is not the pinned disable tuple")
	}
	if p.Readback.FPGAInterfaceModule != pinnedBridgeTuple.FPGAInterfaceModule ||
		p.Readback.SDRPort != pinnedBridgeTuple.SDRPort ||
		p.Readback.BridgeModuleReset != pinnedBridgeTuple.BridgeModuleReset {
		return errors.New("bridge disable readback differs from the pinned readable tuple")
	}
	if p.Readback.NIC301Remap != 0 {
		return errors.New("write-only L3 remap was presented as a readback")
	}
	if p.L3RemapIssued != pinnedBridgeTuple.NIC301Remap {
		return errors.New("write-only L3 remap issued value is not the pinned value")
	}
	if !p.Views.allDisabled() {
		return errors.New("bridge proof does not prove all Linux bridge views disabled")
	}
	return nil
}

// ProgrammingProof is positive evidence that the FPGA manager is in USERMODE
// with its configuration drive released and no programming owner remains.
type ProgrammingProof struct {
	UserMode                bool
	DriveReleased           bool
	EnableClear             bool
	AXICFGENClear           bool
	ConfigurationPullsClear bool
	NoProgrammingProcess    bool
	NoProgrammingMapping    bool
	Status                  uint32
	Control                 uint32
}

func (p ProgrammingProof) Validate() error {
	if p.Status&programmingUserModeMask != programmingUserModeValue {
		return errors.New("FPGA manager status does not prove USERMODE")
	}
	if p.Control&programmingEnableMask != 0 {
		return errors.New("FPGA manager CTRL.EN is set")
	}
	if p.Control&programmingAXICFGENMask != 0 {
		return errors.New("FPGA manager CTRL.AXICFGEN is set")
	}
	if p.Control&programmingPullMask != 0 {
		return errors.New("FPGA manager configuration pull control is set")
	}
	if !p.UserMode {
		return errors.New("FPGA manager is not in USERMODE")
	}
	if !p.DriveReleased {
		return errors.New("FPGA manager configuration drive is not released")
	}
	if !p.EnableClear {
		return errors.New("FPGA manager CTRL.EN is not clear")
	}
	if !p.AXICFGENClear {
		return errors.New("FPGA manager CTRL.AXICFGEN is not clear")
	}
	if !p.ConfigurationPullsClear {
		return errors.New("FPGA manager configuration pull controls are not clear")
	}
	if !p.NoProgrammingProcess {
		return errors.New("programming process remains present")
	}
	if !p.NoProgrammingMapping {
		return errors.New("programming mapping remains present")
	}
	return nil
}

// PolicyProof is the hash-bound static resource policy for the one inert
// experiment accepted by this task.  ExactlyOneHPSGeneralPurpose and
// NoForbiddenResources are positive attestations, not defaults; zero values
// are rejected so a partial proof cannot become a lease.
type PolicyProof struct {
	Experiment       string
	Board            string
	BuildLane        string
	ArtifactFilename string
	ArtifactSHA256   string
	SourceCommit     string
	// Evidence is the exact unsigned schema-2 producer record from which the
	// static policy booleans are derived.
	Evidence ResourceEvidenceV2

	ExactlyOneHPSGeneralPurpose bool
	NoForbiddenResources        bool
	ForbiddenResources          []string
	ExternalResources           bool
}

func clonePolicyProof(proof PolicyProof) PolicyProof {
	proof.ForbiddenResources = append([]string(nil), proof.ForbiddenResources...)
	return proof
}

func (p PolicyProof) Validate(manifest Manifest, metadata ArtifactMetadata) error {
	if p.Experiment != ManifestExperiment || p.Board != ManifestBoard || p.ArtifactFilename != ManifestArtifact {
		return errors.New("static resource policy identity is not the pinned experiment")
	}
	if p.BuildLane != BuildLaneOSS && p.BuildLane != BuildLaneOracle {
		return errors.New("static resource policy build lane is invalid")
	}
	if p.BuildLane != manifest.BuildLane {
		return errors.New("static resource policy build lane does not match manifest")
	}
	if p.ArtifactSHA256 == "" || p.ArtifactSHA256 != manifest.ArtifactSHA256 || p.ArtifactSHA256 != metadata.SHA256 {
		return errors.New("static resource policy artifact hash is not bound to the artifact")
	}
	if p.SourceCommit == "" || p.SourceCommit != manifest.SourceCommit {
		return errors.New("static resource policy source commit is not bound to the manifest")
	}
	if p.Evidence == (ResourceEvidenceV2{}) {
		return errors.New("static resource policy has no resource evidence")
	}
	if err := p.Evidence.MatchesManifest(manifest); err != nil {
		return fmt.Errorf("static resource evidence is invalid: %w", err)
	}
	if p.Evidence.ArtifactSHA256 != metadata.SHA256 {
		return errors.New("static resource evidence artifact hash is not bound to the artifact")
	}
	if p.Experiment != p.Evidence.Experiment || p.Board != p.Evidence.Board || p.BuildLane != p.Evidence.BuildLane || p.SourceCommit != p.Evidence.SourceCommit {
		return errors.New("static resource policy identity differs from resource evidence")
	}
	if p.ExactlyOneHPSGeneralPurpose != (p.Evidence.HPSGeneralPurposeInterfaces == 1) {
		return errors.New("static resource policy primitive result differs from resource evidence")
	}
	if p.NoForbiddenResources != resourceEvidenceHasNoForbiddenResources(p.Evidence) {
		return errors.New("static resource policy resource result differs from resource evidence")
	}
	if !p.ExactlyOneHPSGeneralPurpose {
		return errors.New("static resource policy does not prove exactly one HPS general-purpose primitive")
	}
	if !p.NoForbiddenResources || len(p.ForbiddenResources) != 0 || p.ExternalResources {
		return errors.New("static resource policy contains forbidden or external resources")
	}
	return nil
}

func resourceEvidenceHasNoForbiddenResources(evidence ResourceEvidenceV2) bool {
	return evidence.ExternalInputPorts == 0 && evidence.ExternalOutputPorts == 0 && evidence.BidirectionalPorts == 0 &&
		evidence.PLLBlocks == 0 && evidence.DSPBlocks == 0 && evidence.BlockMemoryBits == 0 && evidence.LUTRAMBits == 0 && evidence.SDRAMInterfaces == 0
}

// PolicySubsystemProof records the process, descriptor, input, and resource
// absence checks made after the compatibility owner exits.  The booleans are
// intentionally positive evidence and all are required.
type PolicySubsystemProof struct {
	MainAbsent                bool
	InputWorkersAbsent        bool
	OffloadWorkersAbsent      bool
	PresentationWorkersAbsent bool
	DeviceDescriptorsAbsent   bool
	PressedInputCleared       bool

	NoInput          bool
	NoOffload        bool
	NoPresentation   bool
	NoSave           bool
	NoStorage        bool
	NoVideo          bool
	NoAudio          bool
	NoPLL            bool
	NoSDRAM          bool
	NoExternalOutput bool
	NoSharedMemory   bool
}

func (p PolicySubsystemProof) Validate() error {
	checks := []struct {
		name string
		ok   bool
	}{
		{"Main process set", p.MainAbsent},
		{"input workers", p.InputWorkersAbsent},
		{"offload workers", p.OffloadWorkersAbsent},
		{"presentation workers", p.PresentationWorkersAbsent},
		{"device descriptors", p.DeviceDescriptorsAbsent},
		{"pressed input", p.PressedInputCleared},
		{"input resource", p.NoInput},
		{"offload resource", p.NoOffload},
		{"presentation resource", p.NoPresentation},
		{"save resource", p.NoSave},
		{"storage resource", p.NoStorage},
		{"video resource", p.NoVideo},
		{"audio resource", p.NoAudio},
		{"PLL resource", p.NoPLL},
		{"SDRAM resource", p.NoSDRAM},
		{"external output", p.NoExternalOutput},
		{"shared memory", p.NoSharedMemory},
	}
	for _, check := range checks {
		if !check.ok {
			return fmt.Errorf("subsystem proof does not prove %s absent", check.name)
		}
	}
	return nil
}

// recoveryRegisters extends the mailbox register surface with the read/write
// operations needed only while the compatibility owner is quiescing. It is
// never returned to a development caller after qualification.
type recoveryRegisters interface {
	Registers
	DisableBridges(BridgeTuple) error
	ReadBridgeTuple() (BridgeTuple, error)
	ReadProgramming() (ProgrammingProof, error)
}

// recoveryMapper opens a temporary recovery mapping. OpenMailbox is kept on
// the concrete Mapper as the distinct post-transfer development mapping.
type recoveryMapper interface {
	openRecovery(context.Context) (recoveryRegisters, error)
}

// contextRecoveryRegisters is the production-only context-aware extension.
// The legacy methods remain on recoveryRegisters for a small compatibility
// surface, but Qualifier always prefers this extension and checks the same
// qualification context on both sides of every call.
type contextRecoveryRegisters interface {
	DisableBridgesContext(context.Context, BridgeTuple) error
	ReadBridgeTupleContext(context.Context) (BridgeTuple, error)
	ReadProgrammingContext(context.Context) (ProgrammingProof, error)
	ReadGPIContext(context.Context) (uint32, error)
	WriteGPOContext(context.Context, uint32) error
	ReadGPOContext(context.Context) (uint32, error)
	CloseContext(context.Context) error
}

// Mapper owns one descriptor-backed physical mapping operation.  The
// function-valued fields are intentionally injectable seams: production
// callers leave them nil and use the Linux syscalls, while tests provide
// anonymous byte pages and never touch /dev/mem.  They are typed as any so a
// fixture can use either the syscall-shaped signature or a small address/byte
// callback without introducing a second production interface.
type Mapper struct {
	// These configuration and syscall fields are intentionally private.  The
	// production adapter has one fixed FPGA-manager address and /dev/mem path;
	// tests in this package inject anonymous-memory seams without exposing
	// Linux device paths or physical addresses through the package API.
	devicePath      string
	path            string
	physicalAddress uint64
	address         uint64
	pageSize        int
	mappingLength   int

	open    any
	mapFn   any
	unmap   any
	close   any
	fstat   any
	read32  any
	write32 any

	// Counters are test-visible only within this package.  They make the
	// exactly-once cleanup contract observable without wrapping host syscalls.
	mu         sync.Mutex
	closeCalls int
	unmapCalls int
}

// mmioDeviceStat is the tiny cross-platform shape used by the injectable
// /dev/mem validator.  The Linux adapter fills it from unix.Stat_t; keeping
// this shape here lets package tests compile against the stable stub too.
type mmioDeviceStat struct {
	mode  uint32
	uid   uint32
	major uint32
	minor uint32
}

const (
	unixSIFMT     uint32 = 0o170000
	unixSIFCHR    uint32 = 0o020000
	mmioONOFOLLOW        = 0x20000
)

// NewMapper constructs an adapter for the fixed FPGA-manager page.  Its
// device path and physical address are intentionally not caller-configurable;
// software tests inject seams from this package instead.
func NewMapper() *Mapper {
	return &Mapper{}
}

// MainAbsenceVerifier is deliberately compatible with Observer's complete
// process-set proof.  Callers supply the snapshot captured before dispatch.
type MainAbsenceVerifier interface {
	WaitStableAbsent(context.Context, []ProcessIdentity, time.Duration) error
}

type policyVerifier interface {
	Verify(context.Context, ArtifactBinding) (PolicyProof, error)
}

type SubsystemVerifier interface {
	Verify(context.Context) (PolicySubsystemProof, error)
}

// bindingSubsystemVerifier is the production-only extension used when the
// dynamic subsystem observer must also bind its static resource result to the
// already validated artifact. Package fixtures may continue to implement the
// smaller SubsystemVerifier interface.
type bindingSubsystemVerifier interface {
	VerifyWithPolicy(context.Context, ArtifactBinding, PolicyProof) (PolicySubsystemProof, error)
}

type PressedInputNeutralizer interface {
	NeutralizePressedInput(context.Context) error
}

type BridgeVerifier interface {
	VerifyBridgeViews(context.Context) (BridgeViews, error)
}

type ProgrammingAbsenceProof struct {
	NoProgrammingProcess bool
	NoProgrammingMapping bool
}

type ProgrammingVerifier interface {
	VerifyProgramming(context.Context) (ProgrammingAbsenceProof, error)
}

// QualificationDependencies is deliberately package-private.  The test
// fixture constructor below can compose anonymous fakes, but external callers
// must use NewQualifier, which owns a concrete Mapper and StaticPolicy rather
// than accepting arbitrary mapped or static proof providers.
type qualificationDependencies struct {
	Mapper       recoveryMapper
	MainAbsent   MainAbsenceVerifier
	Baseline     []ProcessIdentity
	Policy       policyVerifier
	Subsystems   SubsystemVerifier
	PressedInput PressedInputNeutralizer
	Bridge       BridgeVerifier
	Programming  ProgrammingVerifier
	Clock        Clock
	Timeout      time.Duration
}

// QualificationObservers contains the dynamic, independently observed
// evidence that production qualification needs in addition to the concrete
// recovery mapper and static policy source.  It intentionally contains no
// mapped-register or static-policy injection seam.
type QualificationObservers struct {
	MainAbsent   MainAbsenceVerifier
	Baseline     []ProcessIdentity
	Subsystems   SubsystemVerifier
	PressedInput PressedInputNeutralizer
	Programming  ProgrammingVerifier
}

// Qualifier performs the bounded no-owner qualification.  Its implementation
// is opaque so an external caller cannot populate arbitrary evidence fields.
type Qualifier struct {
	implementation *qualifier
}

var (
	ErrQualificationUnsupported = errors.New("FPGA qualification unsupported")
	ErrQualificationFailed      = errors.New("FPGA no-owner qualification failed")
	ErrInvalidReceipt           = errors.New("invalid FPGA qualification receipt")
)

type qualifier struct {
	dependencies qualificationDependencies
}

// newFixtureQualifier is intentionally unexported.  It is used only by
// package-local tests to inject anonymous memory and hostile evidence seams.
func newFixtureQualifier(dependencies qualificationDependencies) Qualifier {
	dependencies.Baseline = append([]ProcessIdentity(nil), dependencies.Baseline...)
	return Qualifier{implementation: &qualifier{dependencies: dependencies}}
}

// StaticPolicy is an opaque marker for the one compiled-in static resource
// policy used by this experiment. It carries no caller-authored assertions.
type StaticPolicy struct{}

// NewStaticPolicy returns the fixed production policy source. Its identity is
// derived from the already validated ArtifactBinding on each qualification;
// callers cannot provide policy booleans or substitute a proof.
func NewStaticPolicy() StaticPolicy { return StaticPolicy{} }

type staticPolicyVerifier struct{}

func (staticPolicyVerifier) Verify(ctx context.Context, binding ArtifactBinding) (PolicyProof, error) {
	if err := contextError(ctx); err != nil {
		return PolicyProof{}, err
	}
	manifest, metadata, err := validateQualificationBinding(&binding)
	if err != nil {
		return PolicyProof{}, err
	}
	if err := contextError(ctx); err != nil {
		return PolicyProof{}, err
	}
	evidence, err := binding.ResourceEvidence()
	if err != nil {
		return PolicyProof{}, fmt.Errorf("load resource evidence: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return PolicyProof{}, err
	}
	if err := evidence.MatchesManifest(manifest); err != nil {
		return PolicyProof{}, err
	}
	if evidence.ArtifactSHA256 != metadata.SHA256 {
		return PolicyProof{}, errors.New("resource evidence artifact hash does not match opened artifact")
	}
	return PolicyProof{
		Experiment:                  manifest.Experiment,
		Board:                       manifest.Board,
		BuildLane:                   manifest.BuildLane,
		ArtifactFilename:            manifest.ArtifactFilename,
		ArtifactSHA256:              metadata.SHA256,
		SourceCommit:                manifest.SourceCommit,
		Evidence:                    evidence,
		ExactlyOneHPSGeneralPurpose: evidence.HPSGeneralPurposeInterfaces == 1,
		NoForbiddenResources:        resourceEvidenceHasNoForbiddenResources(evidence),
	}, nil
}

// NewQualifier composes production qualification from a concrete Mapper and
// opaque StaticPolicy.  Dynamic observers are supplied as observation seams;
// mapped register and static proof evidence cannot be substituted by them.
func NewQualifier(mapper *Mapper, _ StaticPolicy, observerArgs ...QualificationObservers) *Qualifier {
	if mapper == nil {
		mapper = NewMapper()
	}
	var observers QualificationObservers
	if len(observerArgs) != 0 {
		observers = observerArgs[0]
	}
	return &Qualifier{implementation: &qualifier{dependencies: qualificationDependencies{
		Mapper:       mapper,
		MainAbsent:   observers.MainAbsent,
		Baseline:     append([]ProcessIdentity(nil), observers.Baseline...),
		Policy:       staticPolicyVerifier{},
		Subsystems:   observers.Subsystems,
		PressedInput: observers.PressedInput,
		// The production bridge observer is fixed and cannot be replaced by an
		// exported caller. Package tests use newFixtureQualifier for anonymous
		// state fixtures instead.
		Bridge:      newProductionBridgeStateObserver(),
		Programming: observers.Programming,
		Clock:       mailboxRealClock{},
		Timeout:     qualificationTimeout,
	}}}
}

// receiptSeal is a package-private capability.  Receipt values cannot be
// manufactured by another package and validation additionally checks every
// proof field, preventing a zero value or partial proof from being accepted.
type receiptSeal struct{}

var qualificationReceiptSeal = &receiptSeal{}

type receiptProof struct {
	seal           *receiptSeal
	binding        ArtifactMetadata
	manifest       Manifest
	policy         PolicyProof
	bridge         BridgeProof
	programming    ProgrammingProof
	subsystems     PolicySubsystemProof
	gpo            uint32
	hello          uint32
	helloSamples   [2]uint32
	mappingsClosed bool
}

// Receipt is an immutable qualification result.  Its proof is intentionally
// private; consumers can inspect only validated summaries through accessors.
type Receipt struct {
	proof receiptProof
}

func (r Receipt) Validate() error {
	if r.proof.seal != qualificationReceiptSeal {
		return ErrInvalidReceipt
	}
	if r.proof.binding.Device == 0 || r.proof.binding.Inode == 0 || r.proof.binding.Size <= 0 || r.proof.binding.Path == "" {
		return fmt.Errorf("%w: artifact binding is incomplete", ErrInvalidReceipt)
	}
	if !filepath.IsAbs(r.proof.binding.Path) || filepath.Clean(r.proof.binding.Path) != r.proof.binding.Path || filepath.Base(r.proof.binding.Path) != ManifestArtifact {
		return fmt.Errorf("%w: artifact filename is not the pinned top.rbf", ErrInvalidReceipt)
	}
	if !manifestHashPattern.MatchString(r.proof.binding.SHA256) {
		return fmt.Errorf("%w: artifact hash is invalid", ErrInvalidReceipt)
	}
	if err := r.proof.manifest.Validate(); err != nil {
		return fmt.Errorf("%w: manifest proof is invalid: %w", ErrInvalidReceipt, err)
	}
	if r.proof.binding.Size != int64(r.proof.manifest.ArtifactSize) {
		return fmt.Errorf("%w: artifact size is not bound to the manifest", ErrInvalidReceipt)
	}
	if err := r.proof.policy.Validate(r.proof.manifest, r.proof.binding); err != nil {
		return fmt.Errorf("%w: policy proof is invalid: %w", ErrInvalidReceipt, err)
	}
	if err := r.proof.bridge.Validate(); err != nil {
		return fmt.Errorf("%w: bridge proof is invalid: %w", ErrInvalidReceipt, err)
	}
	if err := r.proof.programming.Validate(); err != nil {
		return fmt.Errorf("%w: programming proof is invalid: %w", ErrInvalidReceipt, err)
	}
	if err := r.proof.subsystems.Validate(); err != nil {
		return fmt.Errorf("%w: subsystem proof is invalid: %w", ErrInvalidReceipt, err)
	}
	if r.proof.gpo != 0 {
		return fmt.Errorf("%w: GPO proof is not zero", ErrInvalidReceipt)
	}
	if r.proof.hello != qualificationHelloWord || r.proof.helloSamples[0] != qualificationHelloWord || r.proof.helloSamples[1] != qualificationHelloWord {
		return fmt.Errorf("%w: stable HELLO proof is invalid", ErrInvalidReceipt)
	}
	if !r.proof.mappingsClosed {
		return fmt.Errorf("%w: recovery mapping is still open", ErrInvalidReceipt)
	}
	return nil
}

func (r Receipt) Valid() bool { return r.Validate() == nil }

func (r Receipt) IsValid() bool { return r.Valid() }

func (r Receipt) StableHello() uint32 { return r.proof.hello }

func (r Receipt) RecoveryMappingsClosed() bool { return r.proof.mappingsClosed }

func (r Receipt) GPO() uint32 { return r.proof.gpo }

func (r Receipt) Artifact() ArtifactMetadata { return r.proof.binding }

// Manifest returns the exact manifest identity accepted by qualification.
func (r Receipt) Manifest() Manifest { return r.proof.manifest }

// Policy returns a copy of the exact static policy proof accepted by
// qualification.  Its slice is copied so a caller cannot mutate receipt
// evidence through the accessor.
func (r Receipt) Policy() PolicyProof { return clonePolicyProof(r.proof.policy) }

func (r Receipt) Bridge() BridgeProof { return cloneBridgeProof(r.proof.bridge) }

func (r Receipt) Programming() ProgrammingProof { return r.proof.programming }

func (r Receipt) Subsystems() PolicySubsystemProof { return r.proof.subsystems }

func (r Receipt) HelloSamples() [2]uint32 { return r.proof.helloSamples }

func (q Qualifier) Qualify(ctx context.Context, binding ArtifactBinding) (receipt Receipt, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if q.implementation == nil {
		return Receipt{}, ErrQualificationUnsupported
	}
	d := q.implementation.dependencies
	if d.Clock == nil {
		d.Clock = mailboxRealClock{}
	}
	timeout := d.Timeout
	if timeout <= 0 || timeout > qualificationTimeout {
		timeout = qualificationTimeout
	}
	qualificationCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline := d.Clock.Now().Add(timeout)
	check := func() error { return qualificationDeadlineError(qualificationCtx, d.Clock, deadline) }
	callFailure := func(label string, callErr error) error {
		wrapped := fmt.Errorf("%s: %w", label, callErr)
		if deadlineErr := check(); deadlineErr != nil {
			return errors.Join(deadlineErr, wrapped)
		}
		return wrapped
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}

	manifest, metadata, err := validateQualificationBinding(&binding)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, err)
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	if d.Policy == nil {
		return Receipt{}, fmt.Errorf("%w: static resource policy verifier is unavailable", ErrQualificationUnsupported)
	}
	policy, err := d.Policy.Verify(qualificationCtx, binding)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("static resource policy", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	if err := policy.Validate(manifest, metadata); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, err)
	}

	if d.MainAbsent == nil {
		return Receipt{}, fmt.Errorf("%w: complete Main process observer is unavailable", ErrQualificationUnsupported)
	}
	if err := validateQualificationBaseline(d.Baseline); err != nil {
		return Receipt{}, fmt.Errorf("%w: Main process baseline is invalid: %w", ErrQualificationFailed, err)
	}
	if err := d.MainAbsent.WaitStableAbsent(qualificationCtx, d.Baseline, qualificationStableAbsence); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("Main process set did not remain absent", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}

	if d.Subsystems == nil {
		return Receipt{}, fmt.Errorf("%w: subsystem observer is unavailable", ErrQualificationUnsupported)
	}
	var subsystems PolicySubsystemProof
	if contextual, ok := d.Subsystems.(bindingSubsystemVerifier); ok {
		subsystems, err = contextual.VerifyWithPolicy(qualificationCtx, binding, policy)
	} else {
		subsystems, err = d.Subsystems.Verify(qualificationCtx)
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("subsystem absence", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	if d.PressedInput == nil {
		return Receipt{}, fmt.Errorf("%w: pressed-input neutralizer is unavailable", ErrQualificationUnsupported)
	}
	if err := d.PressedInput.NeutralizePressedInput(qualificationCtx); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("pressed input neutralization", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	subsystems.PressedInputCleared = true
	if err := subsystems.Validate(); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, err)
	}
	if d.Mapper == nil {
		return Receipt{}, fmt.Errorf("%w: recovery mapper is unavailable", ErrQualificationUnsupported)
	}
	if d.Bridge == nil {
		return Receipt{}, fmt.Errorf("%w: independent Linux bridge-state observer is unavailable", ErrQualificationUnsupported)
	}
	if d.Programming == nil {
		return Receipt{}, fmt.Errorf("%w: independent programming process/mapping observer is unavailable", ErrQualificationUnsupported)
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	independent, err := d.Programming.VerifyProgramming(qualificationCtx)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("programming process/mapping absence", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	if !independent.NoProgrammingProcess {
		return Receipt{}, fmt.Errorf("%w: programming process remains present", ErrQualificationFailed)
	}
	if !independent.NoProgrammingMapping {
		return Receipt{}, fmt.Errorf("%w: programming mapping remains present", ErrQualificationFailed)
	}

	recovery, openErr := d.Mapper.openRecovery(qualificationCtx)
	closed := false
	if recovery != nil {
		// Install cleanup before inspecting the open result or checking the
		// deadline: a defensive mapper may return both a mapping and an error.
		defer func() {
			if closed {
				return
			}
			cleanupErr := closeRecovery(qualificationCtx, recovery)
			closed = true
			if cleanupErr == nil {
				return
			}
			if err == nil {
				err = fmt.Errorf("%w: close recovery mapping: %w", ErrQualificationFailed, cleanupErr)
				receipt = Receipt{}
				return
			}
			err = errors.Join(err, fmt.Errorf("close recovery mapping: %w", cleanupErr))
			receipt = Receipt{}
		}()
	}
	if openErr != nil {
		if deadlineErr := check(); deadlineErr != nil {
			return Receipt{}, errors.Join(deadlineErr, fmt.Errorf("open recovery mapping: %w", openErr))
		}
		if errors.Is(openErr, ErrUnsupported) || errors.Is(openErr, ErrQualificationUnsupported) {
			return Receipt{}, fmt.Errorf("%w: open recovery mapping: %w", ErrQualificationUnsupported, openErr)
		}
		return Receipt{}, fmt.Errorf("%w: open recovery mapping: %w", ErrQualificationFailed, openErr)
	}
	if recovery == nil {
		if deadlineErr := check(); deadlineErr != nil {
			return Receipt{}, deadlineErr
		}
		return Receipt{}, fmt.Errorf("%w: recovery mapper returned nil mapping", ErrQualificationFailed)
	}

	if err := check(); err != nil {
		return Receipt{}, err
	}
	if err := disableRecoveryBridges(qualificationCtx, recovery); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("bridge disable", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	readback, err := readRecoveryBridgeTuple(qualificationCtx, recovery)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("bridge readback", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	views, err := d.Bridge.VerifyBridgeViews(qualificationCtx)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("Linux bridge views", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	bridge := BridgeProof{Requested: pinnedBridgeTuple, Readback: readback, L3RemapIssued: pinnedBridgeTuple.NIC301Remap, Views: cloneBridgeViews(views)}
	if err := bridge.Validate(); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, err)
	}

	programming, err := readRecoveryProgramming(qualificationCtx, recovery)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("mapped programming status", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	// Only the independent verifier owns process/mapping absence.  Mapped USERMODE
	// and every CTRL ownership/pull bit came from the recovery page above.
	programming.NoProgrammingProcess = independent.NoProgrammingProcess
	programming.NoProgrammingMapping = independent.NoProgrammingMapping
	if err := programming.Validate(); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, err)
	}

	if err := check(); err != nil {
		return Receipt{}, err
	}
	if err := writeRecoveryGPO(qualificationCtx, recovery, 0); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("clear GPO", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	zero, err := readRecoveryGPO(qualificationCtx, recovery)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("read GPO zero", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	if zero != 0 {
		return Receipt{}, fmt.Errorf("%w: GPO zero readback is %#08x", ErrQualificationFailed, zero)
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	helloSamples, err := readStableQualificationHelloUntil(qualificationCtx, recovery, d.Clock, deadline)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("stable HELLO", err))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}

	closeErr := closeRecovery(qualificationCtx, recovery)
	closed = true
	if closeErr != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrQualificationFailed, callFailure("close recovery mapping", closeErr))
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	receipt = Receipt{proof: receiptProof{
		seal:           qualificationReceiptSeal,
		binding:        metadata,
		manifest:       manifest,
		policy:         clonePolicyProof(policy),
		bridge:         bridge,
		programming:    programming,
		subsystems:     subsystems,
		gpo:            0,
		hello:          qualificationHelloWord,
		helloSamples:   helloSamples,
		mappingsClosed: true,
	}}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	validationErr := receipt.Validate()
	if validationErr != nil {
		if deadlineErr := check(); deadlineErr != nil {
			return Receipt{}, errors.Join(validationErr, deadlineErr)
		}
		return Receipt{}, validationErr
	}
	if err := check(); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func validateQualificationBinding(binding *ArtifactBinding) (Manifest, ArtifactMetadata, error) {
	if binding == nil || binding.state == nil {
		return Manifest{}, ArtifactMetadata{}, errors.New("artifact binding is nil")
	}
	binding.state.mu.Lock()
	if binding.state.closed {
		binding.state.mu.Unlock()
		return Manifest{}, ArtifactMetadata{}, errors.New("artifact binding is closed")
	}
	manifest := binding.state.manifest
	metadata := binding.state.metadata
	hasDirectory := binding.state.dir != nil
	hasArtifact := binding.state.artifact != nil
	binding.state.mu.Unlock()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, ArtifactMetadata{}, err
	}
	if metadata.Device == 0 || metadata.Inode == 0 || metadata.Size != int64(manifest.ArtifactSize) || metadata.SHA256 != manifest.ArtifactSHA256 {
		return Manifest{}, ArtifactMetadata{}, errors.New("artifact identity is not bound to the validated manifest")
	}
	if metadata.Path == "" || !filepath.IsAbs(metadata.Path) || filepath.Clean(metadata.Path) != metadata.Path || filepath.Base(metadata.Path) != manifest.ArtifactFilename {
		return Manifest{}, ArtifactMetadata{}, errors.New("artifact identity does not name the pinned top.rbf")
	}
	if hasDirectory != hasArtifact {
		return Manifest{}, ArtifactMetadata{}, errors.New("artifact binding has incomplete descriptor ownership")
	}
	// The retained descriptor identity was validated by the artifact staging
	// handoff. Qualify deliberately does not rehash or reopen it; Task 7
	// performs the final binding revalidation immediately before dispatch.
	return manifest, metadata, nil
}

func validateQualificationBaseline(baseline []ProcessIdentity) error {
	if len(baseline) == 0 {
		return errors.New("Main process baseline must be nonempty")
	}
	normalized := normalizeProcessIdentities(baseline)
	if len(normalized) != len(baseline) {
		return errors.New("Main process baseline is not a canonical duplicate-free snapshot")
	}
	seenPIDs := make(map[int]struct{}, len(baseline))
	for index, identity := range baseline {
		if identity.PID <= 0 || identity.StartTime == 0 || !identity.executable().valid() {
			return errors.New("Main process baseline contains an invalid process identity")
		}
		if _, exists := seenPIDs[identity.PID]; exists {
			return ErrProcessIdentityChanged
		}
		seenPIDs[identity.PID] = struct{}{}
		if !identity.equal(normalized[index]) {
			return errors.New("Main process baseline is not canonically ordered")
		}
	}
	return nil
}

func readStableQualificationHello(ctx context.Context, registers recoveryRegisters, clock Clock) ([2]uint32, error) {
	return readStableQualificationHelloUntil(ctx, registers, clock, clock.Now().Add(qualificationTimeout))
}

func readStableQualificationHelloUntil(ctx context.Context, registers recoveryRegisters, clock Clock, deadline time.Time) ([2]uint32, error) {
	var samples [2]uint32
	if err := contextError(ctx); err != nil {
		return samples, err
	}
	if !clock.Now().Before(deadline) {
		return samples, context.DeadlineExceeded
	}
	first, err := readRecoveryGPI(ctx, registers)
	if err != nil {
		return samples, err
	}
	if err := qualificationDeadlineError(ctx, clock, deadline); err != nil {
		return samples, err
	}
	samples[0] = first
	if first != qualificationHelloWord {
		return samples, fmt.Errorf("first HELLO sample is %#08x", first)
	}
	if err := waitQualificationPoll(ctx, clock, deadline); err != nil {
		return samples, err
	}
	second, err := readRecoveryGPI(ctx, registers)
	if err != nil {
		return samples, err
	}
	samples[1] = second
	if err := qualificationDeadlineError(ctx, clock, deadline); err != nil {
		return samples, err
	}
	if second != qualificationHelloWord || second != first {
		return samples, fmt.Errorf("HELLO samples are unstable or invalid: %#08x, %#08x", first, second)
	}
	return samples, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func qualificationDeadlineError(ctx context.Context, clock Clock, deadline time.Time) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if clock == nil || !clock.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func disableRecoveryBridges(ctx context.Context, registers recoveryRegisters) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if contextRegisters, ok := registers.(contextRecoveryRegisters); ok {
		if err := contextRegisters.DisableBridgesContext(ctx, pinnedBridgeTuple); err != nil {
			return err
		}
		return contextError(ctx)
	}
	if err := registers.DisableBridges(pinnedBridgeTuple); err != nil {
		return err
	}
	return contextError(ctx)
}

func readRecoveryBridgeTuple(ctx context.Context, registers recoveryRegisters) (BridgeTuple, error) {
	if err := contextError(ctx); err != nil {
		return BridgeTuple{}, err
	}
	if contextRegisters, ok := registers.(contextRecoveryRegisters); ok {
		value, err := contextRegisters.ReadBridgeTupleContext(ctx)
		if err != nil {
			return BridgeTuple{}, err
		}
		return value, contextError(ctx)
	}
	value, err := registers.ReadBridgeTuple()
	if err != nil {
		return BridgeTuple{}, err
	}
	return value, contextError(ctx)
}

func readRecoveryProgramming(ctx context.Context, registers recoveryRegisters) (ProgrammingProof, error) {
	if err := contextError(ctx); err != nil {
		return ProgrammingProof{}, err
	}
	if contextRegisters, ok := registers.(contextRecoveryRegisters); ok {
		value, err := contextRegisters.ReadProgrammingContext(ctx)
		if err != nil {
			return ProgrammingProof{}, err
		}
		return value, contextError(ctx)
	}
	value, err := registers.ReadProgramming()
	if err != nil {
		return ProgrammingProof{}, err
	}
	return value, contextError(ctx)
}

func readRecoveryGPI(ctx context.Context, registers recoveryRegisters) (uint32, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	if contextRegisters, ok := registers.(contextRecoveryRegisters); ok {
		value, err := contextRegisters.ReadGPIContext(ctx)
		if err != nil {
			return 0, err
		}
		return value, contextError(ctx)
	}
	value, err := registers.ReadGPI()
	if err != nil {
		return 0, err
	}
	return value, contextError(ctx)
}

func writeRecoveryGPO(ctx context.Context, registers recoveryRegisters, value uint32) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if contextRegisters, ok := registers.(contextRecoveryRegisters); ok {
		if err := contextRegisters.WriteGPOContext(ctx, value); err != nil {
			return err
		}
		return contextError(ctx)
	}
	if err := registers.WriteGPO(value); err != nil {
		return err
	}
	return contextError(ctx)
}

func readRecoveryGPO(ctx context.Context, registers recoveryRegisters) (uint32, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	if contextRegisters, ok := registers.(contextRecoveryRegisters); ok {
		value, err := contextRegisters.ReadGPOContext(ctx)
		if err != nil {
			return 0, err
		}
		return value, contextError(ctx)
	}
	value, err := registers.ReadGPO()
	if err != nil {
		return 0, err
	}
	return value, contextError(ctx)
}

func closeRecovery(ctx context.Context, registers recoveryRegisters) error {
	if contextRegisters, ok := registers.(contextRecoveryRegisters); ok {
		return contextRegisters.CloseContext(ctx)
	}
	// Cleanup must run even when the qualification context has expired. The
	// context check after this call prevents a stale success from escaping.
	return registers.Close()
}

func waitQualificationPoll(ctx context.Context, clock Clock, deadline time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !clock.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-clock.After(qualificationPollInterval):
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !clock.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
