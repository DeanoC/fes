package fpgadev

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newAnonymousMapperForTest(t *testing.T) *Mapper {
	t.Helper()
	page := make([]byte, 4096)
	return &Mapper{
		devicePath: "/anonymous/fake-mem",
		pageSize:   4096,
		open: func(string, int, uint32) (int, error) {
			return 41, nil
		},
		mapFn: func(int, int64, int, int, int) ([]byte, error) {
			copyPage := append([]byte(nil), page...)
			binary.LittleEndian.PutUint32(copyPage[fpgaManagerGPOOffset:], 0x01020304)
			return copyPage, nil
		},
		unmap: func([]byte) error { return nil },
		close: func(int) error { return nil },
		fstat: func(int) (mmioDeviceStat, error) {
			return mmioDeviceStat{mode: unixSIFCHR | 0o600, uid: 0, major: 1, minor: 1}, nil
		},
	}
}

type qualificationFakeClock struct {
	mu       sync.Mutex
	now      time.Time
	after    []time.Duration
	advances []time.Duration
}

func newQualificationFakeClock() *qualificationFakeClock {
	return &qualificationFakeClock{now: time.Unix(100, 0)}
}

func (c *qualificationFakeClock) expire() {
	c.mu.Lock()
	c.now = c.now.Add(qualificationTimeout)
	c.mu.Unlock()
}

func (c *qualificationFakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *qualificationFakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.after = append(c.after, d)
	advance := d
	if len(c.advances) != 0 {
		advance = c.advances[0]
		c.advances = c.advances[1:]
	}
	if advance < d {
		panic("qualification fake clock advanced before requested duration")
	}
	c.now = c.now.Add(advance)
	now := c.now
	c.mu.Unlock()
	out := make(chan time.Time, 1)
	out <- now
	return out
}

type qualificationFakeRegisters struct {
	mu                  sync.Mutex
	gpo                 uint32
	gpi                 []uint32
	gpoWrites           []uint32
	gpoReads            int
	gpiReads            int
	closeCalls          int
	closeErr            error
	gpoReadErr          error
	gpiReadErr          error
	gpoWriteErr         error
	gpoReadback         []uint32
	bridge              BridgeTuple
	bridgeReads         []BridgeTuple
	bridgeViews         BridgeViews
	programming         ProgrammingProof
	bridgeErr           error
	bridgeReadErr       error
	bridgeViewErr       error
	programmingErr      error
	policyErr           error
	subsystemErr        error
	expireOnClose       *qualificationFakeClock
	expireOnFirstGPI    *qualificationFakeClock
	expireOnSecondGPI   *qualificationFakeClock
	expireOnGPOWrite    *qualificationFakeClock
	expireOnGPORead     *qualificationFakeClock
	expireOnBridgeWrite *qualificationFakeClock
	expireOnBridgeRead  *qualificationFakeClock
	expireOnProgramming *qualificationFakeClock
}

func (r *qualificationFakeRegisters) ReadGPI() (uint32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gpiReads++
	if r.gpiReads == 1 && r.expireOnFirstGPI != nil {
		r.expireOnFirstGPI.expire()
	}
	if r.gpiReads == 2 && r.expireOnSecondGPI != nil {
		r.expireOnSecondGPI.expire()
	}
	if r.gpiReadErr != nil {
		return 0, r.gpiReadErr
	}
	if len(r.gpi) == 0 {
		return 0xd3100000, nil
	}
	value := r.gpi[0]
	r.gpi = r.gpi[1:]
	return value, nil
}

func (r *qualificationFakeRegisters) ReadGPIContext(ctx context.Context) (uint32, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	value, err := r.ReadGPI()
	if err != nil {
		return 0, err
	}
	return value, contextError(ctx)
}

func (r *qualificationFakeRegisters) WriteGPO(value uint32) error {
	if r.expireOnGPOWrite != nil {
		r.expireOnGPOWrite.expire()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gpoWriteErr != nil {
		return r.gpoWriteErr
	}
	r.gpo = value
	r.gpoWrites = append(r.gpoWrites, value)
	return nil
}

func (r *qualificationFakeRegisters) WriteGPOContext(ctx context.Context, value uint32) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := r.WriteGPO(value); err != nil {
		return err
	}
	return contextError(ctx)
}

func (r *qualificationFakeRegisters) ReadGPO() (uint32, error) {
	if r.expireOnGPORead != nil {
		r.expireOnGPORead.expire()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gpoReads++
	if r.gpoReadErr != nil {
		return 0, r.gpoReadErr
	}
	if len(r.gpoReadback) != 0 {
		value := r.gpoReadback[0]
		r.gpoReadback = r.gpoReadback[1:]
		return value, nil
	}
	return r.gpo, nil
}

func (r *qualificationFakeRegisters) ReadGPOContext(ctx context.Context) (uint32, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	value, err := r.ReadGPO()
	if err != nil {
		return 0, err
	}
	return value, contextError(ctx)
}

func (r *qualificationFakeRegisters) Close() error {
	if r.expireOnClose != nil {
		r.expireOnClose.expire()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCalls++
	return r.closeErr
}

func (r *qualificationFakeRegisters) CloseContext(ctx context.Context) error {
	cleanupErr := r.Close()
	if ctxErr := contextError(ctx); ctxErr != nil {
		return errors.Join(cleanupErr, ctxErr)
	}
	return cleanupErr
}

func (r *qualificationFakeRegisters) DisableBridges(_ BridgeTuple) error {
	if r.expireOnBridgeWrite != nil {
		r.expireOnBridgeWrite.expire()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bridgeErr != nil {
		return r.bridgeErr
	}
	return nil
}

func (r *qualificationFakeRegisters) DisableBridgesContext(ctx context.Context, tuple BridgeTuple) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := r.DisableBridges(tuple); err != nil {
		return err
	}
	return contextError(ctx)
}

func (r *qualificationFakeRegisters) ReadBridgeTuple() (BridgeTuple, error) {
	if r.expireOnBridgeRead != nil {
		r.expireOnBridgeRead.expire()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bridgeReadErr != nil {
		return BridgeTuple{}, r.bridgeReadErr
	}
	r.bridgeReads = append(r.bridgeReads, r.bridge)
	readback := r.bridge
	// The source-bound L3 register is write-only and is intentionally absent
	// from the readable tuple returned by this fixture.
	readback.NIC301Remap = 0
	return readback, nil
}

func (r *qualificationFakeRegisters) ReadBridgeTupleContext(ctx context.Context) (BridgeTuple, error) {
	if err := contextError(ctx); err != nil {
		return BridgeTuple{}, err
	}
	value, err := r.ReadBridgeTuple()
	if err != nil {
		return BridgeTuple{}, err
	}
	return value, contextError(ctx)
}

func (r *qualificationFakeRegisters) ReadProgramming() (ProgrammingProof, error) {
	if r.expireOnProgramming != nil {
		r.expireOnProgramming.expire()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.programmingErr != nil {
		return ProgrammingProof{}, r.programmingErr
	}
	return r.programming, nil
}

func (r *qualificationFakeRegisters) ReadProgrammingContext(ctx context.Context) (ProgrammingProof, error) {
	if err := contextError(ctx); err != nil {
		return ProgrammingProof{}, err
	}
	value, err := r.ReadProgramming()
	if err != nil {
		return ProgrammingProof{}, err
	}
	return value, contextError(ctx)
}

type qualificationFakeMapper struct {
	mu           sync.Mutex
	opened       []*qualificationFakeRegisters
	recovery     *qualificationFakeRegisters
	openErr      error
	openCalls    int
	mailboxOpen  int
	expireOnOpen *qualificationFakeClock
}

func (m *qualificationFakeMapper) openRecovery(ctx context.Context) (recoveryRegisters, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openCalls++
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m.openErr != nil {
		return nil, m.openErr
	}
	registers := m.recovery
	if registers == nil {
		registers = &qualificationFakeRegisters{
			gpi: []uint32{0xd3100000, 0xd3100000},
			bridge: BridgeTuple{
				FPGAInterfaceModule: 0,
				SDRPort:             0,
				BridgeModuleReset:   7,
				NIC301Remap:         1,
			},
			bridgeViews: BridgeViews{
				FPGA2HPSDisabled:   true,
				HPS2FPGADisabled:   true,
				LWHPS2FPGADisabled: true,
				FPGASDRAMDisabled:  true,
			},
			programming: ProgrammingProof{
				UserMode:                true,
				DriveReleased:           true,
				EnableClear:             true,
				AXICFGENClear:           true,
				ConfigurationPullsClear: true,
				NoProgrammingProcess:    true,
				NoProgrammingMapping:    true,
				Status:                  programmingUserModeValue,
			},
		}
	}
	m.opened = append(m.opened, registers)
	if m.expireOnOpen != nil {
		m.expireOnOpen.expire()
	}
	return registers, nil
}

func (m *qualificationFakeMapper) OpenMailbox() (Registers, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mailboxOpen++
	return &qualificationFakeRegisters{}, nil
}

type qualificationFakeObserver struct {
	err          error
	baseline     []ProcessIdentity
	stable       time.Duration
	calls        int
	expireOnWait *qualificationFakeClock
	onWait       func([]ProcessIdentity) error
}

func (o *qualificationFakeObserver) WaitStableAbsent(_ context.Context, baseline []ProcessIdentity, stable time.Duration) error {
	if o.expireOnWait != nil {
		o.expireOnWait.expire()
	}
	o.calls++
	o.baseline = append([]ProcessIdentity(nil), baseline...)
	o.stable = stable
	if o.onWait != nil {
		if err := o.onWait(o.baseline); err != nil {
			return err
		}
	}
	return o.err
}

type qualificationFakePolicy struct {
	proof          PolicyProof
	err            error
	calls          int
	expireOnVerify *qualificationFakeClock
}

func (p *qualificationFakePolicy) Verify(ctx context.Context, _ ArtifactBinding) (PolicyProof, error) {
	if err := contextError(ctx); err != nil {
		return PolicyProof{}, err
	}
	p.calls++
	if p.expireOnVerify != nil {
		p.expireOnVerify.expire()
	}
	if p.err != nil {
		return PolicyProof{}, p.err
	}
	return p.proof, nil
}

type qualificationFakeSubsystems struct {
	proof              PolicySubsystemProof
	err                error
	clear              int
	check              int
	expireOnVerify     *qualificationFakeClock
	expireOnNeutralize *qualificationFakeClock
}

func (s *qualificationFakeSubsystems) NeutralizePressedInput(context.Context) error {
	if s.expireOnNeutralize != nil {
		s.expireOnNeutralize.expire()
	}
	s.clear++
	return s.err
}

func (s *qualificationFakeSubsystems) Verify(context.Context) (PolicySubsystemProof, error) {
	if s.expireOnVerify != nil {
		s.expireOnVerify.expire()
	}
	s.check++
	if s.err != nil {
		return PolicySubsystemProof{}, s.err
	}
	return s.proof, nil
}

func qualificationArtifactBinding() ArtifactBinding {
	evidence := &ResourceEvidenceV2{
		Schema:                      ResourceEvidenceSchemaVersion,
		Experiment:                  ManifestExperiment,
		Board:                       ManifestBoard,
		BuildLane:                   BuildLaneOSS,
		SourceCommit:                strings.Repeat("b", 40),
		ArtifactSHA256:              strings.Repeat("a", 64),
		SynthesisReportSHA256:       strings.Repeat("c", 64),
		ClockInputs:                 1,
		HPSGeneralPurposeInterfaces: 1,
	}
	return ArtifactBinding{state: &artifactBindingState{
		metadata: ArtifactMetadata{
			Device: 11,
			Inode:  22,
			Size:   4,
			SHA256: strings.Repeat("a", 64),
			Path:   "/anonymous/top.rbf",
		},
		manifest: Manifest{
			Schema:           ManifestSchemaVersion,
			RunID:            strings.Repeat("1", 32),
			Experiment:       ManifestExperiment,
			Board:            ManifestBoard,
			BuildLane:        BuildLaneOSS,
			ArtifactFilename: ManifestArtifact,
			ArtifactSize:     4,
			ArtifactSHA256:   strings.Repeat("a", 64),
			SourceCommit:     strings.Repeat("b", 40),
		},
		resourceEvidence: evidence,
	}}
}

func validQualificationDeps() (qualificationDependencies, *qualificationFakeMapper, *qualificationFakeRegisters) {
	binding := qualificationArtifactBinding()
	registers := &qualificationFakeRegisters{
		gpi:         []uint32{0xd3100000, 0xd3100000},
		bridge:      validBridgeTuple(),
		bridgeViews: validBridgeViews(),
		programming: validProgrammingProof(),
	}
	mapper := &qualificationFakeMapper{recovery: registers}
	policy := &qualificationFakePolicy{proof: PolicyProof{
		Experiment:                  ManifestExperiment,
		Board:                       ManifestBoard,
		BuildLane:                   BuildLaneOSS,
		ArtifactFilename:            ManifestArtifact,
		ArtifactSHA256:              strings.Repeat("a", 64),
		SourceCommit:                strings.Repeat("b", 40),
		Evidence:                    *binding.state.resourceEvidence,
		ExactlyOneHPSGeneralPurpose: true,
		NoForbiddenResources:        true,
	}}
	subsystems := &qualificationFakeSubsystems{proof: PolicySubsystemProof{
		MainAbsent:                true,
		InputWorkersAbsent:        true,
		OffloadWorkersAbsent:      true,
		PresentationWorkersAbsent: true,
		DeviceDescriptorsAbsent:   true,
		NoInput:                   true,
		NoOffload:                 true,
		NoPresentation:            true,
		NoSave:                    true,
		NoStorage:                 true,
		NoVideo:                   true,
		NoAudio:                   true,
		NoPLL:                     true,
		NoSDRAM:                   true,
		NoExternalOutput:          true,
		NoSharedMemory:            true,
	}}
	deps := qualificationDependencies{
		Mapper:       mapper,
		Policy:       policy,
		Subsystems:   subsystems,
		PressedInput: subsystems,
		Clock:        newQualificationFakeClock(),
		MainAbsent:   &qualificationFakeObserver{},
		Baseline:     []ProcessIdentity{validMainBaseline()},
		Bridge:       qualificationFakeBridgeVerifier{registers: registers},
		Programming:  qualificationFakeProgrammingVerifier{registers: registers},
	}
	return deps, mapper, registers
}

func validMainBaseline() ProcessIdentity {
	return ProcessIdentity{
		PID:       101,
		StartTime: 20260827,
		Device:    31,
		Inode:     41,
		SHA256:    strings.Repeat("c", 64),
	}
}

func TestQualificationAcceptsOnlyCompleteProofAndClosesRecoveryMapping(t *testing.T) {
	deps, mapper, _ := validQualificationDeps()
	q := newFixtureQualifier(deps)
	receipt, err := q.Qualify(context.Background(), qualificationArtifactBinding())
	if err != nil {
		t.Fatalf("Qualify: %v", err)
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("Receipt.Validate: %v", err)
	}
	if mapper.openCalls != 1 || len(mapper.opened) != 1 || mapper.opened[0].closeCalls != 1 {
		t.Fatalf("recovery mapping lifecycle = opens:%d mappings:%d closes:%d", mapper.openCalls, len(mapper.opened), mapper.opened[0].closeCalls)
	}
	mapper.opened[0].mu.Lock()
	writes := append([]uint32(nil), mapper.opened[0].gpoWrites...)
	mapper.opened[0].mu.Unlock()
	if !reflect.DeepEqual(writes, []uint32{0}) {
		t.Fatalf("qualification GPO writes = %#v, want only zero", writes)
	}
	if receipt.StableHello() != 0xd3100000 || !receipt.RecoveryMappingsClosed() || receipt.GPO() != 0 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if receipt.Manifest().ArtifactSHA256 != strings.Repeat("a", 64) || receipt.Policy().SourceCommit != strings.Repeat("b", 40) {
		t.Fatalf("receipt identity = manifest:%#v policy:%#v", receipt.Manifest(), receipt.Policy())
	}
	bridge := receipt.Bridge()
	if !bridge.Views.allDisabled() || bridge.L3RemapIssued != 1 || bridge.Readback.NIC301Remap != 0 {
		t.Fatalf("receipt bridge evidence = %#v", bridge)
	}
	bridge.Views.Entries[0].State = "enabled\n"
	if !receipt.Valid() {
		t.Fatal("bridge accessor mutation changed receipt evidence")
	}
}

func TestQualificationRejectsEachIndependentLeaseProof(t *testing.T) {
	baseDeps, _, _ := validQualificationDeps()
	failures := map[string]func(*qualificationDependencies){
		"main absent": func(d *qualificationDependencies) {
			d.MainAbsent = &qualificationFakeObserver{err: errors.New("main still present")}
		},
		"policy": func(d *qualificationDependencies) {
			d.Policy = &qualificationFakePolicy{proof: PolicyProof{}}
		},
		"subsystems": func(d *qualificationDependencies) {
			d.Subsystems = &qualificationFakeSubsystems{proof: PolicySubsystemProof{}}
		},
		"mapper": func(d *qualificationDependencies) {
			d.Mapper = &qualificationFakeMapper{openErr: errors.New("map")}
		},
	}
	for name, configure := range failures {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			deps := baseDeps
			configure(&deps)
			if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
				t.Fatal("incomplete lease proof was accepted")
			}
		})
	}
}

func TestQualificationRejectsWrongArtifactBindingAndUnstableHello(t *testing.T) {
	deps, _, _ := validQualificationDeps()
	wrong := qualificationArtifactBinding()
	wrong.state.manifest.ArtifactSHA256 = strings.Repeat("c", 64)
	if _, err := newFixtureQualifier(deps).Qualify(context.Background(), wrong); err == nil {
		t.Fatal("wrong artifact hash was accepted")
	}

	deps, mapper, _ := validQualificationDeps()
	mapper.opened = nil
	mapperFactory := mapper
	mapperFactory.openErr = nil
	// The mapper supplies a fresh fake for each open; override its default
	// stable transcript after construction through a dedicated mapper seam.
	unstable := &qualificationFakeMapperWithRegisters{
		registers: &qualificationFakeRegisters{
			gpi:         []uint32{0xd3100000, 0xd3100001},
			bridge:      validBridgeTuple(),
			bridgeViews: validBridgeViews(),
			programming: validProgrammingProof(),
		},
	}
	deps.Mapper = unstable
	if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
		t.Fatal("unstable HELLO was accepted")
	}
}

func TestQualificationClosesRecoveryMappingOnEveryInjectedRegisterFailure(t *testing.T) {
	failures := map[string]func(*qualificationFakeRegisters){
		"bridge write":    func(r *qualificationFakeRegisters) { r.bridgeErr = errors.New("bridge write") },
		"bridge readback": func(r *qualificationFakeRegisters) { r.bridgeReadErr = errors.New("bridge readback") },
		"bridge view":     func(r *qualificationFakeRegisters) { r.bridgeViewErr = errors.New("bridge view") },
		"programming":     func(r *qualificationFakeRegisters) { r.programmingErr = errors.New("programming") },
		"GPO write":       func(r *qualificationFakeRegisters) { r.gpoWriteErr = errors.New("GPO write") },
		"GPO read":        func(r *qualificationFakeRegisters) { r.gpoReadErr = errors.New("GPO read") },
		"GPO readback":    func(r *qualificationFakeRegisters) { r.gpoReadback = []uint32{1} },
		"HELLO read":      func(r *qualificationFakeRegisters) { r.gpiReadErr = errors.New("HELLO read") },
		"HELLO stability": func(r *qualificationFakeRegisters) {
			r.gpi = []uint32{qualificationHelloWord, qualificationHelloWord + 1}
		},
	}
	for name, configure := range failures {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			deps, mapper, registers := validQualificationDeps()
			configure(registers)
			if name == "programming" {
				deps.Programming = qualificationFakeProgrammingVerifier{proof: ProgrammingAbsenceProof{NoProgrammingProcess: true, NoProgrammingMapping: true}}
			}
			receipt, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding())
			if err == nil {
				t.Fatal("injected register failure was accepted")
			}
			if receipt.Valid() {
				t.Fatal("failed qualification returned valid receipt")
			}
			if mapper.openCalls != 1 || registers.closeCalls != 1 {
				t.Fatalf("recovery lifecycle = opens:%d closes:%d", mapper.openCalls, registers.closeCalls)
			}
		})
	}
}

func TestQualificationPreservesPrimaryAndCleanupErrors(t *testing.T) {
	deps, mapper, registers := validQualificationDeps()
	primary := errors.New("bridge primary")
	cleanup := errors.New("recovery cleanup")
	registers.bridgeErr = primary
	registers.closeErr = cleanup
	_, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding())
	if !errors.Is(err, primary) || !errors.Is(err, cleanup) {
		t.Fatalf("qualification error = %v, want primary and cleanup causes", err)
	}
	if mapper.openCalls != 1 || registers.closeCalls != 1 {
		t.Fatalf("recovery lifecycle = opens:%d closes:%d", mapper.openCalls, registers.closeCalls)
	}
}

func TestQualificationRequiresPressedInputNeutralizerAndHonorsCancellation(t *testing.T) {
	deps, mapper, _ := validQualificationDeps()
	deps.PressedInput = nil
	if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); !errors.Is(err, ErrQualificationUnsupported) {
		t.Fatalf("missing pressed-input neutralizer error = %v", err)
	}
	if mapper.openCalls != 0 {
		t.Fatalf("mapper opened without pressed-input proof: %d", mapper.openCalls)
	}

	deps, mapper, _ = validQualificationDeps()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newFixtureQualifier(deps).Qualify(ctx, qualificationArtifactBinding()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled qualification error = %v", err)
	}
	if mapper.openCalls != 0 {
		t.Fatalf("mapper opened after cancellation: %d", mapper.openCalls)
	}
}

func TestQualificationRejectsEmptyOrNonCanonicalMainBaselineBeforeMapping(t *testing.T) {
	tests := []struct {
		name     string
		baseline []ProcessIdentity
	}{
		{name: "empty", baseline: nil},
		{name: "duplicate", baseline: []ProcessIdentity{validMainBaseline(), validMainBaseline()}},
		{name: "unsorted", baseline: []ProcessIdentity{{PID: 102, StartTime: 1, Device: 31, Inode: 41, SHA256: strings.Repeat("c", 64)}, validMainBaseline()}},
		{name: "zero start time", baseline: []ProcessIdentity{{PID: 101, StartTime: 0, Device: 31, Inode: 41, SHA256: strings.Repeat("c", 64)}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps, mapper, _ := validQualificationDeps()
			deps.Baseline = test.baseline
			if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
				t.Fatal("invalid Main baseline was accepted")
			}
			if mapper.openCalls != 0 {
				t.Fatalf("recovery mapping opened for invalid baseline: %d", mapper.openCalls)
			}
		})
	}
}

func TestQualificationPropagatesMainPIDStartTimeAndExecutableReplacement(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*ProcessIdentity)
	}{
		{name: "PID replacement", mutate: func(identity *ProcessIdentity) { identity.PID++ }},
		{name: "start-time replacement", mutate: func(identity *ProcessIdentity) { identity.StartTime++ }},
		{name: "executable device replacement", mutate: func(identity *ProcessIdentity) { identity.Device++ }},
		{name: "executable inode replacement", mutate: func(identity *ProcessIdentity) { identity.Inode++ }},
		{name: "executable digest replacement", mutate: func(identity *ProcessIdentity) { identity.SHA256 = strings.Repeat("d", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps, mapper, _ := validQualificationDeps()
			candidate := validMainBaseline()
			test.mutate(&candidate)
			deps.Baseline = []ProcessIdentity{candidate}
			deps.MainAbsent = &qualificationFakeObserver{
				onWait: func(got []ProcessIdentity) error {
					if len(got) != 1 || !got[0].equal(candidate) {
						t.Fatalf("replacement baseline = %#v, want %#v", got, candidate)
					}
					return ErrProcessIdentityChanged
				},
			}
			if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); !errors.Is(err, ErrProcessIdentityChanged) {
				t.Fatalf("replacement error = %v, want ErrProcessIdentityChanged", err)
			}
			if mapper.openCalls != 0 {
				t.Fatalf("recovery mapping opened after Main identity replacement: %d", mapper.openCalls)
			}
		})
	}
}

func TestQualificationRejectsStableHelloAtDeadline(t *testing.T) {
	deps, mapper, registers := validQualificationDeps()
	clock := newQualificationFakeClock()
	clock.advances = []time.Duration{qualificationTimeout}
	deps.Clock = clock
	_, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	if mapper.openCalls != 1 || registers.closeCalls != 1 {
		t.Fatalf("recovery lifecycle = opens:%d closes:%d", mapper.openCalls, registers.closeCalls)
	}
}

func TestQualificationClosesMappingWhenOpenReturnsMappingAndError(t *testing.T) {
	deps, _, registers := validQualificationDeps()
	openErr := errors.New("open returned a mapping and an error")
	cleanupErr := errors.New("cleanup after open error")
	registers.closeErr = cleanupErr
	deps.Mapper = &qualificationFakeMapperWithRegisters{registers: registers, openErr: openErr}
	_, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding())
	if !errors.Is(err, openErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("open/cleanup error = %v, want %v and %v", err, openErr, cleanupErr)
	}
	if registers.closeCalls != 1 {
		t.Fatalf("mapping returned with open error was closed %d times, want once", registers.closeCalls)
	}
}

func TestQualificationCancellationAfterRecoveryMapClosesMapping(t *testing.T) {
	deps, _, registers := validQualificationDeps()
	ctx, cancel := context.WithCancel(context.Background())
	deps.Mapper = &qualificationFakeMapperWithRegisters{registers: registers, cancelOnOpen: cancel}
	_, err := newFixtureQualifier(deps).Qualify(ctx, qualificationArtifactBinding())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("post-map cancellation error = %v, want context.Canceled", err)
	}
	if registers.closeCalls != 1 {
		t.Fatalf("post-map cancellation closed mapping %d times, want once", registers.closeCalls)
	}
}

func TestQualificationDeadlineCoversEveryObserverAndRegisterSeam(t *testing.T) {
	tests := []struct {
		name        string
		configure   func(*qualificationDependencies, *qualificationFakeMapper, *qualificationFakeRegisters, *qualificationFakeClock)
		expectsOpen bool
	}{
		{name: "policy", configure: func(d *qualificationDependencies, _ *qualificationFakeMapper, _ *qualificationFakeRegisters, clock *qualificationFakeClock) {
			d.Policy.(*qualificationFakePolicy).expireOnVerify = clock
		}},
		{name: "main observer", configure: func(d *qualificationDependencies, _ *qualificationFakeMapper, _ *qualificationFakeRegisters, clock *qualificationFakeClock) {
			d.MainAbsent.(*qualificationFakeObserver).expireOnWait = clock
		}},
		{name: "subsystem observer", configure: func(d *qualificationDependencies, _ *qualificationFakeMapper, _ *qualificationFakeRegisters, clock *qualificationFakeClock) {
			d.Subsystems.(*qualificationFakeSubsystems).expireOnVerify = clock
		}},
		{name: "pressed input", configure: func(d *qualificationDependencies, _ *qualificationFakeMapper, _ *qualificationFakeRegisters, clock *qualificationFakeClock) {
			d.PressedInput.(*qualificationFakeSubsystems).expireOnNeutralize = clock
		}},
		{name: "open", expectsOpen: true, configure: func(_ *qualificationDependencies, mapper *qualificationFakeMapper, _ *qualificationFakeRegisters, clock *qualificationFakeClock) {
			mapper.expireOnOpen = clock
		}},
		{name: "bridge write", expectsOpen: true, configure: func(_ *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			registers.expireOnBridgeWrite = clock
		}},
		{name: "bridge read", expectsOpen: true, configure: func(_ *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			registers.expireOnBridgeRead = clock
		}},
		{name: "bridge view", expectsOpen: true, configure: func(d *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			d.Bridge = qualificationFakeBridgeVerifier{registers: registers, expireOnVerify: clock}
		}},
		{name: "mapped programming", expectsOpen: true, configure: func(_ *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			registers.expireOnProgramming = clock
		}},
		{name: "programming observer", expectsOpen: false, configure: func(d *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			d.Programming = qualificationFakeProgrammingVerifier{registers: registers, expireOnVerify: clock}
		}},
		{name: "GPO write", expectsOpen: true, configure: func(_ *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			registers.expireOnGPOWrite = clock
		}},
		{name: "GPO read", expectsOpen: true, configure: func(_ *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			registers.expireOnGPORead = clock
		}},
		{name: "first GPI", expectsOpen: true, configure: func(_ *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			registers.expireOnFirstGPI = clock
		}},
		{name: "second GPI", expectsOpen: true, configure: func(_ *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			registers.expireOnSecondGPI = clock
		}},
		{name: "cleanup", expectsOpen: true, configure: func(_ *qualificationDependencies, _ *qualificationFakeMapper, registers *qualificationFakeRegisters, clock *qualificationFakeClock) {
			registers.expireOnClose = clock
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps, mapper, registers := validQualificationDeps()
			clock := newQualificationFakeClock()
			deps.Clock = clock
			test.configure(&deps, mapper, registers, clock)
			_, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding())
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline error = %v", err)
			}
			if test.expectsOpen {
				if mapper.openCalls != 1 || registers.closeCalls != 1 {
					t.Fatalf("recovery lifecycle = opens:%d closes:%d, want one each", mapper.openCalls, registers.closeCalls)
				}
			} else if mapper.openCalls != 0 || registers.closeCalls != 0 {
				t.Fatalf("pre-map lifecycle = opens:%d closes:%d, want no mapping", mapper.openCalls, registers.closeCalls)
			}
		})
	}
}

type qualificationFakeBridgeVerifier struct {
	proof          BridgeProof
	registers      *qualificationFakeRegisters
	err            error
	expireOnVerify *qualificationFakeClock
}

func (v qualificationFakeBridgeVerifier) VerifyBridgeViews(ctx context.Context) (BridgeViews, error) {
	if err := contextError(ctx); err != nil {
		return BridgeViews{}, err
	}
	if v.expireOnVerify != nil {
		v.expireOnVerify.expire()
	}
	if v.err != nil {
		return BridgeViews{}, v.err
	}
	if v.registers != nil {
		v.registers.mu.Lock()
		defer v.registers.mu.Unlock()
		if v.registers.bridgeViewErr != nil {
			return BridgeViews{}, v.registers.bridgeViewErr
		}
		return v.registers.bridgeViews, nil
	}
	return v.proof.Views, nil
}

type qualificationFakeProgrammingVerifier struct {
	proof          ProgrammingAbsenceProof
	registers      *qualificationFakeRegisters
	expireOnVerify *qualificationFakeClock
}

func (v qualificationFakeProgrammingVerifier) VerifyProgramming(context.Context) (ProgrammingAbsenceProof, error) {
	if v.expireOnVerify != nil {
		v.expireOnVerify.expire()
	}
	if v.registers != nil {
		v.registers.mu.Lock()
		defer v.registers.mu.Unlock()
		if v.registers.programmingErr != nil {
			return ProgrammingAbsenceProof{}, v.registers.programmingErr
		}
		return ProgrammingAbsenceProof{
			NoProgrammingProcess: v.registers.programming.NoProgrammingProcess,
			NoProgrammingMapping: v.registers.programming.NoProgrammingMapping,
		}, nil
	}
	return v.proof, nil
}

func TestQualificationNeverLetsIndependentVerifiersReplaceMappedEvidence(t *testing.T) {
	deps, mapper, registers := validQualificationDeps()
	registers.bridgeErr = errors.New("mapped bridge evidence failed")
	registers.programmingErr = errors.New("mapped programming evidence failed")
	deps.Bridge = qualificationFakeBridgeVerifier{proof: BridgeProof{
		Requested: validBridgeTuple(),
		Readback:  validBridgeTuple(),
		Views:     validBridgeViews(),
	}}
	deps.Programming = qualificationFakeProgrammingVerifier{proof: ProgrammingAbsenceProof{NoProgrammingProcess: true, NoProgrammingMapping: true}}
	if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
		t.Fatalf("independent verifier bypassed mapped evidence (open calls:%d)", mapper.openCalls)
	}
}

func TestQualificationRejectsEachIndependentProcessAndMappingAbsenceBit(t *testing.T) {
	for _, field := range []string{"process", "mapping"} {
		t.Run(field, func(t *testing.T) {
			deps, mapper, _ := validQualificationDeps()
			proof := ProgrammingAbsenceProof{NoProgrammingProcess: true, NoProgrammingMapping: true}
			if field == "process" {
				proof.NoProgrammingProcess = false
			} else {
				proof.NoProgrammingMapping = false
			}
			deps.Programming = qualificationFakeProgrammingVerifier{proof: proof}
			if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
				t.Fatalf("missing %s absence bit was accepted", field)
			}
			if mapper.openCalls != 0 {
				t.Fatalf("programming %s conflict opened recovery MMIO %d times", field, mapper.openCalls)
			}
		})
	}
}

func TestQualificationProgrammingObservationFailurePrecedesRecoveryMMIO(t *testing.T) {
	deps, mapper, registers := validQualificationDeps()
	want := errors.New("programming observation unavailable")
	registers.programmingErr = want
	if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); !errors.Is(err, want) {
		t.Fatalf("programming observation error = %v", err)
	}
	if mapper.openCalls != 0 {
		t.Fatalf("failed programming observation opened recovery MMIO %d times", mapper.openCalls)
	}
}

func TestQualificationRejectsContradictoryMappedProgrammingRegisterProof(t *testing.T) {
	deps, _, registers := validQualificationDeps()
	registers.programming.Status = 0
	registers.programming.Control = programmingEnableMask
	if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
		t.Fatal("contradictory raw programming registers were accepted")
	}
}

func TestQualificationRejectsEachReadableBridgeFieldAndIndependentView(t *testing.T) {
	for _, field := range []string{"FPGA interface", "SDR port", "module reset"} {
		t.Run("readback "+field, func(t *testing.T) {
			deps, _, registers := validQualificationDeps()
			switch field {
			case "FPGA interface":
				registers.bridge.FPGAInterfaceModule = 1
			case "SDR port":
				registers.bridge.SDRPort = 1
			case "module reset":
				registers.bridge.BridgeModuleReset = 0
			}
			if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
				t.Fatalf("wrong %s readback was accepted", field)
			}
		})
	}
	for _, field := range []string{"hps2fpga", "lwhps2fpga", "fpga2hps", "fpga2sdram"} {
		t.Run("view "+field, func(t *testing.T) {
			deps, _, _ := validQualificationDeps()
			views := deps.Bridge.(qualificationFakeBridgeVerifier).registers.bridgeViews
			for index := range views.Entries {
				if views.Entries[index].Name == field {
					views.Entries[index].State = "enabled\n"
				}
			}
			deps.Bridge = qualificationFakeBridgeVerifier{proof: BridgeProof{Views: views}}
			if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
				t.Fatalf("enabled %s view was accepted", field)
			}
		})
	}
}

func TestQualificationRejectsEveryPolicyAndSubsystemProofField(t *testing.T) {
	policyFields := map[string]func(*PolicyProof){
		"experiment": func(p *PolicyProof) { p.Experiment = "wrong" },
		"board":      func(p *PolicyProof) { p.Board = "wrong" },
		"lane":       func(p *PolicyProof) { p.BuildLane = "wrong" },
		"filename":   func(p *PolicyProof) { p.ArtifactFilename = "wrong" },
		"hash":       func(p *PolicyProof) { p.ArtifactSHA256 = strings.Repeat("c", 64) },
		"commit":     func(p *PolicyProof) { p.SourceCommit = strings.Repeat("c", 40) },
		"primitive":  func(p *PolicyProof) { p.ExactlyOneHPSGeneralPurpose = false },
		"resources":  func(p *PolicyProof) { p.NoForbiddenResources = false },
		"external":   func(p *PolicyProof) { p.ExternalResources = true },
		"forbidden":  func(p *PolicyProof) { p.ForbiddenResources = []string{"save"} },
	}
	for name, mutate := range policyFields {
		name, mutate := name, mutate
		t.Run("policy "+name, func(t *testing.T) {
			deps, _, _ := validQualificationDeps()
			policy := deps.Policy.(*qualificationFakePolicy)
			mutate(&policy.proof)
			if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
				t.Fatalf("invalid policy field %s was accepted", name)
			}
		})
	}

	subsystemFields := map[string]func(*PolicySubsystemProof){
		"main":             func(p *PolicySubsystemProof) { p.MainAbsent = false },
		"input workers":    func(p *PolicySubsystemProof) { p.InputWorkersAbsent = false },
		"offload":          func(p *PolicySubsystemProof) { p.OffloadWorkersAbsent = false },
		"presentation":     func(p *PolicySubsystemProof) { p.PresentationWorkersAbsent = false },
		"descriptors":      func(p *PolicySubsystemProof) { p.DeviceDescriptorsAbsent = false },
		"input":            func(p *PolicySubsystemProof) { p.NoInput = false },
		"offload res":      func(p *PolicySubsystemProof) { p.NoOffload = false },
		"presentation res": func(p *PolicySubsystemProof) { p.NoPresentation = false },
		"save":             func(p *PolicySubsystemProof) { p.NoSave = false },
		"storage":          func(p *PolicySubsystemProof) { p.NoStorage = false },
		"video":            func(p *PolicySubsystemProof) { p.NoVideo = false },
		"audio":            func(p *PolicySubsystemProof) { p.NoAudio = false },
		"PLL":              func(p *PolicySubsystemProof) { p.NoPLL = false },
		"SDRAM":            func(p *PolicySubsystemProof) { p.NoSDRAM = false },
		"external output":  func(p *PolicySubsystemProof) { p.NoExternalOutput = false },
		"shared memory":    func(p *PolicySubsystemProof) { p.NoSharedMemory = false },
	}
	for name, mutate := range subsystemFields {
		name, mutate := name, mutate
		t.Run("subsystem "+name, func(t *testing.T) {
			deps, _, _ := validQualificationDeps()
			subsystems := deps.Subsystems.(*qualificationFakeSubsystems)
			mutate(&subsystems.proof)
			if _, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding()); err == nil {
				t.Fatalf("invalid subsystem field %s was accepted", name)
			}
		})
	}
}

func TestProductionQualifierKeepsMapperAndPolicyCompositionOpaque(t *testing.T) {
	typeOfQualifier := reflect.TypeOf(Qualifier{})
	for index := 0; index < typeOfQualifier.NumField(); index++ {
		if typeOfQualifier.Field(index).PkgPath == "" {
			t.Fatalf("Qualifier field %q is exported", typeOfQualifier.Field(index).Name)
		}
	}
	newPolicy := reflect.ValueOf(NewStaticPolicy)
	if newPolicy.Type().NumIn() != 0 {
		t.Fatalf("NewStaticPolicy accepts %d caller inputs; production policy must be fixed", newPolicy.Type().NumIn())
	}
	if fields := reflect.TypeOf(StaticPolicy{}).NumField(); fields != 0 {
		t.Fatalf("StaticPolicy exposes %d caller-authored fields", fields)
	}
	observerType := reflect.TypeOf(QualificationObservers{})
	for _, field := range []string{"Mapper", "Policy", "Bridge", "Clock", "Timeout"} {
		if _, ok := observerType.FieldByName(field); ok {
			t.Fatalf("QualificationObservers exposes production %s injection", field)
		}
	}
	if _, ok := reflect.TypeOf(&Mapper{}).MethodByName("OpenRecovery"); ok {
		t.Fatal("Mapper exposes the recovery-only mapping opener")
	}
	policy := NewStaticPolicy()
	if NewQualifier(NewMapper(), policy, QualificationObservers{}) == nil {
		t.Fatal("NewQualifier returned nil")
	}
}

func TestProductionPolicyDerivesFixedIdentityFromValidatedBinding(t *testing.T) {
	binding := qualificationArtifactBinding()
	proof, err := (staticPolicyVerifier{}).Verify(context.Background(), binding)
	if err != nil {
		t.Fatalf("production policy Verify: %v", err)
	}
	manifest := binding.state.manifest
	metadata := binding.state.metadata
	if proof.Experiment != manifest.Experiment || proof.Board != manifest.Board || proof.BuildLane != manifest.BuildLane || proof.ArtifactFilename != manifest.ArtifactFilename || proof.ArtifactSHA256 != metadata.SHA256 || proof.SourceCommit != manifest.SourceCommit {
		t.Fatalf("policy identity = %#v, want binding-derived identity", proof)
	}
	if !proof.ExactlyOneHPSGeneralPurpose || !proof.NoForbiddenResources || len(proof.ForbiddenResources) != 0 || proof.ExternalResources {
		t.Fatalf("production policy safety semantics = %#v, want fixed inert policy", proof)
	}

	for _, test := range []struct {
		name   string
		mutate func(*ArtifactBinding)
	}{
		{name: "wrong experiment", mutate: func(b *ArtifactBinding) { b.state.manifest.Experiment = "wrong" }},
		{name: "wrong hash", mutate: func(b *ArtifactBinding) { b.state.metadata.SHA256 = strings.Repeat("d", 64) }},
		{name: "noncanonical path", mutate: func(b *ArtifactBinding) { b.state.metadata.Path = "/anonymous/staging/../top.rbf" }},
		{name: "closed binding", mutate: func(b *ArtifactBinding) { b.state.closed = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := qualificationArtifactBinding()
			test.mutate(&candidate)
			if _, err := (staticPolicyVerifier{}).Verify(context.Background(), candidate); err == nil {
				t.Fatal("malformed binding received a production policy proof")
			}
		})
	}
}

func TestProductionQualifierUsesFixedMonotonicQualificationTiming(t *testing.T) {
	observerType := reflect.TypeOf(QualificationObservers{})
	if _, ok := observerType.FieldByName("Clock"); ok {
		t.Fatal("exported QualificationObservers exposes a clock injection")
	}
	if _, ok := observerType.FieldByName("Timeout"); ok {
		t.Fatal("exported QualificationObservers exposes a timeout injection")
	}
	qualifier := NewQualifier(NewMapper(), NewStaticPolicy(), QualificationObservers{})
	if _, ok := qualifier.implementation.dependencies.Clock.(mailboxRealClock); !ok {
		t.Fatalf("production qualification clock = %T, want mailboxRealClock", qualifier.implementation.dependencies.Clock)
	}
	if got := qualifier.implementation.dependencies.Timeout; got != qualificationTimeout {
		t.Fatalf("production qualification timeout = %s, want %s", got, qualificationTimeout)
	}
	if qualificationPollInterval != 10*time.Millisecond {
		t.Fatalf("qualification poll interval = %s, want 10ms", qualificationPollInterval)
	}
}

func TestProductionHelloSamplesUseTheFixedTenMillisecondSeparation(t *testing.T) {
	registers := &qualificationFakeRegisters{gpi: []uint32{qualificationHelloWord, qualificationHelloWord}}
	started := time.Now()
	samples, err := readStableQualificationHello(context.Background(), registers, mailboxRealClock{})
	if err != nil {
		t.Fatalf("readStableQualificationHello: %v", err)
	}
	if samples != [2]uint32{qualificationHelloWord, qualificationHelloWord} {
		t.Fatalf("HELLO samples = %#v", samples)
	}
	if elapsed := time.Since(started); elapsed < qualificationPollInterval {
		t.Fatalf("HELLO samples were separated by %s, want at least %s", elapsed, qualificationPollInterval)
	}
}

func TestReceiptZeroValueAndForgedProofAreInvalid(t *testing.T) {
	if (Receipt{}).Validate() == nil || (Receipt{}).Valid() {
		t.Fatal("zero-value receipt is evidence of success")
	}
	forged := Receipt{proof: receiptProof{seal: &receiptSeal{}}}
	if forged.Validate() == nil || forged.Valid() {
		t.Fatal("forged receipt was accepted")
	}
}

func TestReceiptRejectsMutatedGPOAndPolicyProof(t *testing.T) {
	deps, _, _ := validQualificationDeps()
	receipt, err := newFixtureQualifier(deps).Qualify(context.Background(), qualificationArtifactBinding())
	if err != nil {
		t.Fatalf("Qualify: %v", err)
	}
	mutated := receipt
	mutated.proof.gpo = 1
	if mutated.Valid() {
		t.Fatal("nonzero GPO proof was accepted")
	}
	mutated = receipt
	mutated.proof.policy.BuildLane = BuildLaneOracle
	if mutated.Valid() {
		t.Fatal("mutated policy proof was accepted")
	}
	policy := receipt.Policy()
	policy.ForbiddenResources = []string{"caller mutation"}
	if !receipt.Valid() {
		t.Fatal("policy accessor mutation changed receipt evidence")
	}
}

func TestMapperOpenMailboxHasDistinctMappingLifetime(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm") {
		t.Skip("Linux MMIO mapping contract")
	}
	mapper := newAnonymousMapperForTest(t)
	first, err := mapper.OpenMailbox()
	if err != nil {
		t.Fatalf("OpenMailbox first: %v", err)
	}
	second, err := mapper.OpenMailbox()
	if err != nil {
		t.Fatalf("OpenMailbox second: %v", err)
	}
	if reflect.ValueOf(first).Pointer() == reflect.ValueOf(second).Pointer() {
		t.Fatal("post-transfer mapping was reused")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first double close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestMapperRejectsInjectedCleanupErrorsWithoutDoubleClose(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm") {
		t.Skip("Linux MMIO mapping contract")
	}
	unmapErr := errors.New("unmap")
	closeErr := errors.New("close")
	mapper := newAnonymousMapperForTest(t)
	mapper.unmap = func([]byte) error { return unmapErr }
	mapper.close = func(int) error { return closeErr }
	registers, err := mapper.OpenMailbox()
	if err != nil {
		t.Fatalf("OpenMailbox: %v", err)
	}
	if err := registers.Close(); !errors.Is(err, unmapErr) || !errors.Is(err, closeErr) {
		t.Fatalf("close error = %v, want both cleanup errors", err)
	}
	if err := registers.Close(); !errors.Is(err, unmapErr) || !errors.Is(err, closeErr) {
		t.Fatalf("double close error = %v, want cached cleanup errors", err)
	}
	if mapper.closeCalls != 1 || mapper.unmapCalls != 1 {
		t.Fatalf("cleanup calls = close:%d unmap:%d", mapper.closeCalls, mapper.unmapCalls)
	}
}

func TestMapperRequiresAnonymousSeamsOnHostAMD64(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("host-amd64 backend contract")
	}
	mapper := NewMapper()
	if _, err := mapper.OpenMailbox(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("production amd64 mapper error = %v, want ErrUnsupported", err)
	}
	if _, err := mapper.openRecovery(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("production amd64 recovery error = %v, want ErrUnsupported", err)
	}
}

func TestMapperRejectsPartialAnonymousSeamsOnHostAMD64(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("host-amd64 backend contract")
	}
	seams := []struct {
		name string
		set  func(*Mapper)
	}{
		{name: "open", set: func(m *Mapper) { m.open = func(string, int, uint32) (int, error) { return 41, nil } }},
		{name: "map", set: func(m *Mapper) { m.mapFn = func(int, int64, int, int, int) ([]byte, error) { return nil, nil } }},
		{name: "unmap", set: func(m *Mapper) { m.unmap = func([]byte) error { return nil } }},
		{name: "close", set: func(m *Mapper) { m.close = func(int) error { return nil } }},
		{name: "fstat", set: func(m *Mapper) { m.fstat = func(int) (mmioDeviceStat, error) { return mmioDeviceStat{}, nil } }},
	}
	for _, test := range seams {
		t.Run(test.name, func(t *testing.T) {
			mapper := &Mapper{}
			test.set(mapper)
			if _, err := mapper.OpenMailbox(); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("partial %s seam error = %v, want ErrUnsupported", test.name, err)
			}
		})
	}
}

func validBridgeTuple() BridgeTuple {
	return BridgeTuple{FPGAInterfaceModule: 0, SDRPort: 0, BridgeModuleReset: 7, NIC301Remap: 1}
}

func validBridgeViews() BridgeViews {
	return BridgeViews{
		FPGA2HPSDisabled:   true,
		HPS2FPGADisabled:   true,
		LWHPS2FPGADisabled: true,
		FPGASDRAMDisabled:  true,
		Entries: []BridgeStateView{
			{Name: "hps2fpga", State: "disabled\n"},
			{Name: "lwhps2fpga", State: "disabled\n"},
			{Name: "fpga2hps", State: "disabled\n"},
			{Name: "fpga2sdram", State: "disabled\n"},
		},
	}
}

func validProgrammingProof() ProgrammingProof {
	return ProgrammingProof{UserMode: true, DriveReleased: true, EnableClear: true, AXICFGENClear: true, ConfigurationPullsClear: true, NoProgrammingProcess: true, NoProgrammingMapping: true, Status: programmingUserModeValue}
}

type qualificationFakeMapperWithRegisters struct {
	registers    *qualificationFakeRegisters
	openErr      error
	cancelOnOpen context.CancelFunc
}

func (m *qualificationFakeMapperWithRegisters) openRecovery(ctx context.Context) (recoveryRegisters, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if m.cancelOnOpen != nil {
		m.cancelOnOpen()
	}
	return m.registers, m.openErr
}

func (m *qualificationFakeMapperWithRegisters) OpenMailbox() (Registers, error) {
	return &qualificationFakeRegisters{}, nil
}
