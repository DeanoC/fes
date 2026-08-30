package hardwareowner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"unicode/utf8"
)

const (
	SchemaVersion uint64 = 1

	StateNormalMain        State = "normal_main"
	StateRecoveringIntent  State = "recovering_intent"
	StateNoOwner           State = "no_owner"
	StateFPGADefaultActive State = "fpgadev_active"
	// StateFPGADActive is a short alias retained for callers that refer to the
	// development owner by its fpgadev name.
	StateFPGADActive        State = StateFPGADefaultActive
	StateRecoveryRequired   State = "recovery_required"
	StateNormalMainStarting State = "normal_main_starting"
)

const (
	PhaseIntentCommitted Phase = "intent_committed"
	PhaseLoadAttempted   Phase = "load_attempted"
	PhaseMainAbsent      Phase = "main_absent"
	PhaseLeaseActive     Phase = "lease_active"
	PhaseHelloObserved   Phase = "hello_observed"
	PhaseMessagePartial  Phase = "message_partial"
	PhaseEndAckWritten   Phase = "end_ack_written"
	PhaseDoneObserved    Phase = "done_observed"
)

const (
	OwnerNone       Owner = "none"
	OwnerCompatMain Owner = "compat_main"
	OwnerFPGADev    Owner = "fpgadev"
)

const (
	ModeNone       = "none"
	ModeFPGANative = "fpga_native"
	ModeUpdating   = "updating"
)

var (
	bootIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	hex32Pattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	phaseRanks    = map[Phase]int{
		"":                   0,
		PhaseIntentCommitted: 1,
		PhaseLoadAttempted:   2,
		PhaseMainAbsent:      3,
		PhaseLeaseActive:     4,
		PhaseHelloObserved:   5,
		PhaseMessagePartial:  6,
		PhaseEndAckWritten:   7,
		PhaseDoneObserved:    8,
	}
	knownStates = map[State]struct{}{
		StateNormalMain:         {},
		StateRecoveringIntent:   {},
		StateNoOwner:            {},
		StateFPGADefaultActive:  {},
		StateRecoveryRequired:   {},
		StateNormalMainStarting: {},
	}
	knownPhases = map[Phase]struct{}{
		"":                   {},
		PhaseIntentCommitted: {},
		PhaseLoadAttempted:   {},
		PhaseMainAbsent:      {},
		PhaseLeaseActive:     {},
		PhaseHelloObserved:   {},
		PhaseMessagePartial:  {},
		PhaseEndAckWritten:   {},
		PhaseDoneObserved:    {},
	}
	knownOwners = map[Owner]struct{}{
		OwnerNone:       {},
		OwnerCompatMain: {},
		OwnerFPGADev:    {},
	}
	knownModes = map[string]struct{}{
		ModeNone:       {},
		ModeFPGANative: {},
		ModeUpdating:   {},
	}
	knownResources = map[string]struct{}{
		"command_fifo":         {},
		"core_input_saves":     {},
		"core_protocol":        {},
		"fpga_bridges":         {},
		"fpga_generation":      {},
		"fpga_manager_gpi_gpo": {},
		"fpga_programming":     {},
		"main_process_set":     {},
		"native_video_audio":   {},
	}
	knownFailureCodes = map[string]struct{}{
		"designation_failed":            {},
		"profile_disabled":              {},
		"privilege_required":            {},
		"manifest_rejected":             {},
		"result_conflict":               {},
		"ownership_conflict":            {},
		"generation_failed":             {},
		"state_store_failed":            {},
		"load_dispatch_failed":          {},
		"main_handoff_timeout":          {},
		"no_owner_qualification_failed": {},
		"mmio_failed":                   {},
		"protocol_violation":            {},
		"message_timeout":               {},
		"payload_mismatch":              {},
		"reboot_request_failed":         {},
		"reconnect_failed":              {},
		"readiness_failed":              {},
	}
)

var normalLeases = []string{
	"command_fifo",
	"core_input_saves",
	"core_protocol",
	"fpga_bridges",
	"fpga_generation",
	"fpga_programming",
	"main_process_set",
	"native_video_audio",
}

var devLeases = []string{"fpga_generation", "fpga_manager_gpi_gpo"}

// Unlock releases a process-local inter-process lock. It is safe to call an
// Unlock more than once; subsequent calls return the result of the first call.
type Unlock func() error

type Owner string
type State string
type Phase string

type Record struct {
	Schema              uint64   `json:"schema"`
	State               State    `json:"state"`
	Phase               Phase    `json:"phase"`
	BootID              string   `json:"boot_id"`
	RunID               string   `json:"run_id"`
	GenerationHighWater uint64   `json:"generation_high_water"`
	ActiveSession       string   `json:"active_session"`
	ActiveGeneration    uint64   `json:"active_generation"`
	ActiveMode          string   `json:"active_mode"`
	CandidateSession    string   `json:"candidate_session"`
	CandidateGeneration uint64   `json:"candidate_generation"`
	CandidateMode       string   `json:"candidate_mode"`
	QuiescingOwner      Owner    `json:"quiescing_owner"`
	CandidateOwner      Owner    `json:"candidate_owner"`
	ActiveOwner         Owner    `json:"active_owner"`
	ActiveLeases        []string `json:"active_leases"`
	RequestedResources  []string `json:"requested_resources"`
	FirstFailure        string   `json:"first_failure"`
}

// NormalLeases returns the complete compatibility-Main lease set in its
// canonical order. The returned slice is independent of package state.
func NormalLeases() []string { return append([]string(nil), normalLeases...) }

// DevelopmentLeases returns the non-owning development resource intent set in
// its canonical order. The returned slice is independent of package state.
func DevelopmentLeases() []string { return append([]string(nil), devLeases...) }

// Validate checks the immutable, fail-closed owner-record contract. It never
// sorts, trims, fills, or otherwise changes any field of the receiver.
func (r Record) Validate() error {
	if r.Schema != SchemaVersion {
		return fmt.Errorf("schema must be %d", SchemaVersion)
	}
	if _, ok := knownStates[r.State]; !ok {
		return fmt.Errorf("unknown state %q", r.State)
	}
	if _, ok := knownPhases[r.Phase]; !ok {
		return fmt.Errorf("unknown phase %q", r.Phase)
	}
	if !bootIDPattern.MatchString(r.BootID) {
		return fmt.Errorf("boot_id must be a lowercase Linux boot UUID")
	}
	if r.GenerationHighWater == 0 {
		return fmt.Errorf("generation_high_water must be nonzero")
	}
	if r.ActiveGeneration > r.GenerationHighWater || r.CandidateGeneration > r.GenerationHighWater {
		return fmt.Errorf("owner generation exceeds generation_high_water")
	}
	if _, ok := knownOwners[r.QuiescingOwner]; !ok {
		return fmt.Errorf("unknown quiescing_owner %q", r.QuiescingOwner)
	}
	if _, ok := knownOwners[r.CandidateOwner]; !ok {
		return fmt.Errorf("unknown candidate_owner %q", r.CandidateOwner)
	}
	if _, ok := knownOwners[r.ActiveOwner]; !ok {
		return fmt.Errorf("unknown active_owner %q", r.ActiveOwner)
	}
	if _, ok := knownModes[r.ActiveMode]; !ok {
		return fmt.Errorf("unknown active_mode %q", r.ActiveMode)
	}
	if _, ok := knownModes[r.CandidateMode]; !ok {
		return fmt.Errorf("unknown candidate_mode %q", r.CandidateMode)
	}
	if r.ActiveLeases == nil || r.RequestedResources == nil {
		return fmt.Errorf("resource arrays must be present")
	}
	if err := validateResources(r.ActiveLeases, "active_leases"); err != nil {
		return err
	}
	if err := validateResources(r.RequestedResources, "requested_resources"); err != nil {
		return err
	}
	if r.FirstFailure != "" {
		if _, ok := knownFailureCodes[r.FirstFailure]; !ok {
			return fmt.Errorf("unknown first_failure %q", r.FirstFailure)
		}
	}

	switch r.State {
	case StateNormalMain:
		return r.validateNormalMain()
	case StateRecoveringIntent:
		return r.validateRecoveringIntent()
	case StateNoOwner:
		return r.validateNoOwner()
	case StateFPGADefaultActive:
		return r.validateFPGADefaultActive()
	case StateRecoveryRequired:
		return r.validateRecoveryRequired()
	case StateNormalMainStarting:
		return r.validateNormalMainStarting()
	default:
		return fmt.Errorf("unsupported state %q", r.State)
	}
}

func (r Record) validateNormalMain() error {
	if r.Phase != "" || r.RunID != "" || r.FirstFailure != "" {
		return fmt.Errorf("normal_main must have empty phase, run_id, and first_failure")
	}
	if err := validateTuple(r.ActiveSession, r.ActiveGeneration, r.ActiveMode, r.ActiveOwner, OwnerCompatMain, ModeFPGANative, "active"); err != nil {
		return err
	}
	if err := validateCurrentGeneration(r.ActiveGeneration, r.GenerationHighWater, "active"); err != nil {
		return err
	}
	if !sameStrings(r.ActiveLeases, normalLeases) {
		return fmt.Errorf("normal_main active_leases must be the complete compatibility set")
	}
	if err := validateAbsentTuple(r.CandidateSession, r.CandidateGeneration, r.CandidateMode, r.CandidateOwner, "candidate"); err != nil {
		return err
	}
	if r.QuiescingOwner != OwnerNone || len(r.RequestedResources) != 0 {
		return fmt.Errorf("normal_main has unexpected candidate intent")
	}
	return nil
}

func (r Record) validateRecoveringIntent() error {
	if r.Phase != PhaseIntentCommitted && r.Phase != PhaseLoadAttempted {
		return fmt.Errorf("recovering_intent phase must be intent_committed or load_attempted")
	}
	if r.RunID == "" || !hex32Pattern.MatchString(r.RunID) {
		return fmt.Errorf("recovering_intent run_id must be lowercase 32-character hex")
	}
	if r.FirstFailure != "" {
		return fmt.Errorf("recovering_intent cannot have first_failure")
	}
	if err := validateTuple(r.ActiveSession, r.ActiveGeneration, r.ActiveMode, r.ActiveOwner, OwnerCompatMain, ModeFPGANative, "active"); err != nil {
		return err
	}
	if !sameStrings(r.ActiveLeases, normalLeases) {
		return fmt.Errorf("recovering_intent active_leases must be the complete compatibility set")
	}
	if r.QuiescingOwner != OwnerCompatMain {
		return fmt.Errorf("recovering_intent quiescing_owner must be compat_main")
	}
	if err := validateTuple(r.CandidateSession, r.CandidateGeneration, r.CandidateMode, r.CandidateOwner, OwnerFPGADev, ModeUpdating, "candidate"); err != nil {
		return err
	}
	if r.CandidateSession == r.ActiveSession || r.CandidateGeneration <= r.ActiveGeneration {
		return fmt.Errorf("candidate identity must be fresh and newer than active")
	}
	if r.CandidateGeneration > r.GenerationHighWater {
		return fmt.Errorf("candidate generation exceeds high-water")
	}
	if err := validateCurrentGeneration(r.CandidateGeneration, r.GenerationHighWater, "candidate"); err != nil {
		return err
	}
	if !sameStrings(r.RequestedResources, devLeases) {
		return fmt.Errorf("recovering_intent requested_resources must be development intent")
	}
	return nil
}

func (r Record) validateNoOwner() error {
	if r.Phase != PhaseMainAbsent {
		return fmt.Errorf("no_owner phase must be main_absent")
	}
	if r.FirstFailure != "" {
		return fmt.Errorf("no_owner cannot have first_failure")
	}
	if err := validateAbsentTuple(r.ActiveSession, r.ActiveGeneration, r.ActiveMode, r.ActiveOwner, "active"); err != nil {
		return err
	}
	if len(r.ActiveLeases) != 0 || r.QuiescingOwner != OwnerNone {
		return fmt.Errorf("no_owner must have no active owner or quiescing owner")
	}
	if r.RunID == "" {
		if err := validateAbsentTuple(r.CandidateSession, r.CandidateGeneration, r.CandidateMode, r.CandidateOwner, "candidate"); err != nil {
			return fmt.Errorf("reboot-recovery no_owner: %w", err)
		}
		if len(r.RequestedResources) != 0 {
			return fmt.Errorf("reboot-recovery no_owner cannot retain requested resources")
		}
		return nil
	}
	if !hex32Pattern.MatchString(r.RunID) {
		return fmt.Errorf("no_owner run_id must be lowercase 32-character hex")
	}
	if err := validateTuple(r.CandidateSession, r.CandidateGeneration, r.CandidateMode, r.CandidateOwner, OwnerFPGADev, ModeUpdating, "candidate"); err != nil {
		return err
	}
	if err := validateCurrentGeneration(r.CandidateGeneration, r.GenerationHighWater, "candidate"); err != nil {
		return err
	}
	if !sameStrings(r.RequestedResources, devLeases) {
		return fmt.Errorf("no_owner candidate intent is inconsistent")
	}
	return nil
}

func (r Record) validateFPGADefaultActive() error {
	if r.Phase != PhaseLeaseActive && r.Phase != PhaseHelloObserved && r.Phase != PhaseMessagePartial && r.Phase != PhaseEndAckWritten && r.Phase != PhaseDoneObserved {
		return fmt.Errorf("fpgadev_active has invalid phase %q", r.Phase)
	}
	if r.RunID == "" || !hex32Pattern.MatchString(r.RunID) {
		return fmt.Errorf("fpgadev_active run_id must be lowercase 32-character hex")
	}
	if r.FirstFailure != "" {
		return fmt.Errorf("fpgadev_active cannot have first_failure")
	}
	if err := validateTuple(r.ActiveSession, r.ActiveGeneration, r.ActiveMode, r.ActiveOwner, OwnerFPGADev, ModeUpdating, "active"); err != nil {
		return err
	}
	if err := validateCurrentGeneration(r.ActiveGeneration, r.GenerationHighWater, "active"); err != nil {
		return err
	}
	if !sameStrings(r.ActiveLeases, devLeases) {
		return fmt.Errorf("fpgadev_active lease set is inconsistent")
	}
	if err := validateAbsentTuple(r.CandidateSession, r.CandidateGeneration, r.CandidateMode, r.CandidateOwner, "candidate"); err != nil {
		return err
	}
	if r.QuiescingOwner != OwnerNone || len(r.RequestedResources) != 0 {
		return fmt.Errorf("fpgadev_active has unexpected candidate intent")
	}
	return nil
}

func (r Record) validateRecoveryRequired() error {
	normalOwnerFailure := r.ActiveSession != "" && r.ActiveGeneration != 0 && r.ActiveMode == ModeFPGANative && r.ActiveOwner == OwnerCompatMain && r.CandidateSession == "" && r.CandidateGeneration == 0 && r.CandidateMode == ModeNone && r.CandidateOwner == OwnerNone && len(r.RequestedResources) == 0
	if r.Phase == "" && !normalOwnerFailure {
		return fmt.Errorf("recovery_required must preserve a result phase")
	}
	if r.RunID == "" && !normalOwnerFailure {
		return fmt.Errorf("recovery_required run_id must be lowercase 32-character hex")
	}
	if r.RunID != "" && !hex32Pattern.MatchString(r.RunID) {
		return fmt.Errorf("recovery_required run_id must be lowercase 32-character hex")
	}
	if err := validateOwnedTupleForRecovery(r.ActiveSession, r.ActiveGeneration, r.ActiveMode, r.ActiveOwner, r.ActiveLeases, "active"); err != nil {
		return err
	}
	if err := validateRecoveryCandidate(r.CandidateSession, r.CandidateGeneration, r.CandidateMode, r.CandidateOwner, r.RequestedResources, "candidate"); err != nil {
		return err
	}
	if r.CandidateSession == "" {
		// A candidate may be absent after its tuple has already transferred to
		// the active owner, or when normal-main startup is fenced. The active
		// tuple must be the current allocation; transition validation proves
		// which predecessor made that tuple canonical.
		if r.ActiveGeneration == 0 || r.ActiveGeneration != r.GenerationHighWater || len(r.RequestedResources) != 0 {
			return fmt.Errorf("recovery_required absent candidate requires a current active generation")
		}
	} else if err := validateCurrentGeneration(r.CandidateGeneration, r.GenerationHighWater, "candidate"); err != nil {
		return err
	}
	if r.ActiveGeneration > r.GenerationHighWater || r.CandidateGeneration > r.GenerationHighWater {
		return fmt.Errorf("recovery_required generation exceeds high-water")
	}
	if r.ActiveGeneration != 0 && r.CandidateGeneration != 0 && r.CandidateGeneration < r.ActiveGeneration {
		return fmt.Errorf("recovery candidate generation is older than active")
	}
	if r.ActiveGeneration == 0 && r.QuiescingOwner != OwnerNone {
		return fmt.Errorf("recovery_required has a quiescing owner without an active tuple")
	}
	if r.ActiveGeneration != 0 && r.QuiescingOwner != r.ActiveOwner {
		return fmt.Errorf("recovery_required quiescing owner does not match active owner")
	}
	return nil
}

func (r Record) validateNormalMainStarting() error {
	if r.Phase != "" || r.RunID != "" || r.FirstFailure != "" {
		return fmt.Errorf("normal_main_starting must have empty phase, run_id, and first_failure")
	}
	if err := validateTuple(r.ActiveSession, r.ActiveGeneration, r.ActiveMode, r.ActiveOwner, OwnerCompatMain, ModeFPGANative, "active"); err != nil {
		return err
	}
	if err := validateCurrentGeneration(r.ActiveGeneration, r.GenerationHighWater, "active"); err != nil {
		return err
	}
	if !sameStrings(r.ActiveLeases, normalLeases) {
		return fmt.Errorf("normal_main_starting lease set is inconsistent")
	}
	if err := validateAbsentTuple(r.CandidateSession, r.CandidateGeneration, r.CandidateMode, r.CandidateOwner, "candidate"); err != nil {
		return err
	}
	if r.QuiescingOwner != OwnerNone || len(r.RequestedResources) != 0 {
		return fmt.Errorf("normal_main_starting has unexpected candidate intent")
	}
	return nil
}

func validateTuple(session string, generation uint64, mode string, owner Owner, wantOwner Owner, wantMode string, label string) error {
	if !hex32Pattern.MatchString(session) {
		return fmt.Errorf("%s session must be lowercase 32-character hex", label)
	}
	if generation == 0 {
		return fmt.Errorf("%s generation must be nonzero", label)
	}
	if mode != wantMode || owner != wantOwner {
		return fmt.Errorf("%s owner tuple is inconsistent", label)
	}
	return nil
}

func validateCurrentGeneration(generation, highWater uint64, label string) error {
	if generation == 0 || generation != highWater {
		return fmt.Errorf("%s generation must equal current high-water", label)
	}
	return nil
}

func validateAbsentTuple(session string, generation uint64, mode string, owner Owner, label string) error {
	if session != "" || generation != 0 || mode != ModeNone || owner != OwnerNone {
		return fmt.Errorf("%s tuple must be canonically absent", label)
	}
	return nil
}

func validateOwnedTupleForRecovery(session string, generation uint64, mode string, owner Owner, leases []string, label string) error {
	if session == "" || generation == 0 {
		if session != "" || generation != 0 || mode != ModeNone || owner != OwnerNone || len(leases) != 0 {
			return fmt.Errorf("%s absent tuple is inconsistent", label)
		}
		return nil
	}
	if mode == ModeFPGANative && owner == OwnerCompatMain {
		if err := validateTuple(session, generation, mode, owner, OwnerCompatMain, ModeFPGANative, label); err != nil {
			return err
		}
		if !sameStrings(leases, normalLeases) {
			return fmt.Errorf("%s compatibility lease set is inconsistent", label)
		}
		return nil
	}
	if mode == ModeUpdating && owner == OwnerFPGADev {
		if err := validateTuple(session, generation, mode, owner, OwnerFPGADev, ModeUpdating, label); err != nil {
			return err
		}
		if !sameStrings(leases, devLeases) {
			return fmt.Errorf("%s development lease set is inconsistent", label)
		}
		return nil
	}
	return fmt.Errorf("%s owner tuple is inconsistent", label)
}

func validateRecoveryCandidate(session string, generation uint64, mode string, owner Owner, resources []string, label string) error {
	if session == "" && generation == 0 {
		return validateAbsentTuple(session, generation, mode, owner, label)
	}
	if err := validateTuple(session, generation, mode, owner, OwnerFPGADev, ModeUpdating, label); err != nil {
		return err
	}
	if !sameStrings(resources, devLeases) {
		return fmt.Errorf("%s development intent is inconsistent", label)
	}
	return nil
}

func validateResources(resources []string, field string) error {
	for i, resource := range resources {
		if _, ok := knownResources[resource]; !ok {
			return fmt.Errorf("%s contains unknown resource %q", field, resource)
		}
		if i > 0 && resources[i-1] >= resource {
			return fmt.Errorf("%s must be strictly lexically sorted", field)
		}
	}
	return nil
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// ValidateTransition validates a proposed successor without mutating either
// record. It enforces the durable owner-state lifecycle, monotonic high-water
// and phase progress, and exact generation allocation rules.
func (r Record) ValidateTransition(previous Record) error {
	if err := previous.Validate(); err != nil {
		return fmt.Errorf("previous record: %w", err)
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("next record: %w", err)
	}
	bootChanged := previous.BootID != r.BootID
	priorBootCompatMainReclaim := isPriorBootCompatMainReclaim(previous, r)
	if err := validateStateTransition(previous, r, bootChanged); err != nil {
		return err
	}
	if err := validatePhaseTransition(previous, r); err != nil {
		return err
	}
	if err := validateRecoveryRequiredEntry(previous, r); err != nil {
		return err
	}
	if err := validateRecoveryRequiredSameState(previous, r); err != nil {
		return err
	}
	if r.GenerationHighWater < previous.GenerationHighWater {
		return fmt.Errorf("generation_high_water moved backward")
	}
	if r.GenerationHighWater > previous.GenerationHighWater {
		if previous.GenerationHighWater == ^uint64(0) || r.GenerationHighWater != previous.GenerationHighWater+1 {
			return fmt.Errorf("generation_high_water allocation must increment exactly once")
		}
	}
	if previous.ActiveGeneration != 0 && r.ActiveGeneration != 0 && r.ActiveGeneration < previous.ActiveGeneration {
		return fmt.Errorf("active generation moved backward")
	}
	// Reboot reconciliation resets the operation phase while recording the
	// successor boot in its candidate-free no-owner checkpoint.
	rebootCheckpoint := previous.State == StateRecoveryRequired && r.State == StateNoOwner
	if !bootChanged && !rebootCheckpoint && r.Phase != "" && phaseRanks[r.Phase] < phaseRanks[previous.Phase] {
		return fmt.Errorf("phase moved backward")
	}
	transferFromCandidate := r.State == StateFPGADefaultActive && previous.CandidateSession != "" && previous.CandidateSession == r.ActiveSession && previous.CandidateGeneration == r.ActiveGeneration
	reconstructedRecoveryCandidate := previous.State == StateFPGADefaultActive && r.State == StateRecoveryRequired && recoveryCandidateMatchesActive(previous, r)
	rebootClearsCandidate := previous.State == StateRecoveryRequired && isRebootRecoveryNoOwner(r)
	if previous.CandidateSession != "" {
		if r.CandidateSession == "" && !rebootClearsCandidate && !(transferFromCandidate && r.State == StateFPGADefaultActive) {
			return fmt.Errorf("recovery candidate identity was discarded without an allowed transfer")
		}
		if r.CandidateSession != "" && r.CandidateGeneration < previous.CandidateGeneration {
			return fmt.Errorf("candidate generation moved backward")
		}
	}

	candidateChanged := candidateIdentityChanged(previous, r)
	activeChanged := activeIdentityChanged(previous, r)
	candidateAllocation := candidateChanged && r.CandidateSession != "" && !reconstructedRecoveryCandidate
	activeAllocation := activeChanged && r.ActiveSession != "" && !transferFromCandidate
	highWaterAdvanced := r.GenerationHighWater > previous.GenerationHighWater
	if candidateAllocation {
		if !highWaterAdvanced || r.CandidateGeneration != r.GenerationHighWater {
			return fmt.Errorf("new candidate identity requires the next high-water generation")
		}
		if r.CandidateSession == previous.ActiveSession || r.CandidateSession == previous.CandidateSession {
			return fmt.Errorf("new candidate allocation requires a fresh session")
		}
	}
	if activeAllocation {
		if !highWaterAdvanced || r.ActiveGeneration != r.GenerationHighWater {
			return fmt.Errorf("new owner identity requires the next high-water generation")
		}
		if r.ActiveSession == previous.ActiveSession || r.ActiveSession == previous.CandidateSession {
			return fmt.Errorf("new owner allocation requires a fresh session")
		}
	}
	if candidateAllocation && activeAllocation {
		return fmt.Errorf("one transition cannot allocate both candidate and active generations")
	}
	if highWaterAdvanced && !candidateAllocation && !activeAllocation {
		return fmt.Errorf("high-water advanced without a new owner allocation")
	}
	if previous.State == r.State && (highWaterAdvanced || candidateAllocation || activeAllocation) {
		return fmt.Errorf("same owner state cannot allocate a new generation")
	}
	if !highWaterAdvanced && activeChanged && r.ActiveSession != "" && !transferFromCandidate {
		return fmt.Errorf("new owner identity did not advance high-water")
	}
	if previous.RunID != "" && r.RunID != "" && previous.RunID != r.RunID && !priorBootCompatMainReclaim {
		return fmt.Errorf("run_id changed before recovery completed")
	}
	return nil
}

func validateStateTransition(previous, next Record, bootChanged bool) error {
	allowed := map[State]map[State]struct{}{
		StateNormalMain: {
			StateNormalMain:       {},
			StateRecoveringIntent: {},
			StateRecoveryRequired: {},
		},
		StateRecoveringIntent: {
			StateRecoveringIntent: {},
			StateNoOwner:          {},
			StateRecoveryRequired: {},
		},
		StateNoOwner: {
			StateNoOwner:            {},
			StateFPGADefaultActive:  {},
			StateRecoveryRequired:   {},
			StateNormalMainStarting: {},
		},
		StateFPGADefaultActive: {
			StateFPGADefaultActive: {},
			StateRecoveryRequired:  {},
		},
		StateRecoveryRequired: {
			StateRecoveryRequired: {},
			StateNoOwner:          {},
			StateRecoveringIntent: {},
		},
		StateNormalMainStarting: {
			StateNormalMainStarting: {},
			StateNormalMain:         {},
			StateRecoveryRequired:   {},
		},
	}
	if _, ok := allowed[previous.State][next.State]; !ok {
		return fmt.Errorf("invalid owner-state transition %s -> %s", previous.State, next.State)
	}
	if bootChanged {
		if !isPriorBootCompatMainReclaim(previous, next) && (previous.State != StateRecoveryRequired || !isRebootRecoveryNoOwner(next)) {
			return fmt.Errorf("boot_id changed outside recovery_required -> reboot-recovery no_owner")
		}
	} else if previous.State == StateRecoveryRequired && next.State == StateRecoveringIntent {
		return fmt.Errorf("recovery_required -> recovering_intent requires a proven successor boot")
	} else if previous.State == StateRecoveryRequired && next.State == StateNoOwner {
		return fmt.Errorf("recovery_required -> no_owner requires a successor boot")
	}
	if previous.State == StateRecoveringIntent && next.State == StateNoOwner && !isDevelopmentNoOwner(next) {
		return fmt.Errorf("recovering_intent requires a development no_owner checkpoint")
	}
	if previous.State == StateNoOwner && next.State == StateFPGADefaultActive && !isDevelopmentNoOwner(previous) {
		return fmt.Errorf("reboot-recovery no_owner cannot transfer to fpgadev")
	}
	if previous.State == StateNoOwner && next.State == StateNormalMainStarting && !isRebootRecoveryNoOwner(previous) {
		return fmt.Errorf("development no_owner cannot start Main")
	}
	return nil
}

// isPriorBootCompatMainReclaim recognizes the migration transition that binds
// an already-attested current-boot compatibility Main to a fresh development
// intent. The previous record may be canonical normal_main or the exact
// candidate-bearing recovery_required residue of a failed development
// handoff. The caller supplies the current-boot proof; transition validation
// keeps the durable shape narrow and all neighboring boot changes fenced.
// Ordinary Gate admission never uses this exception.
func isPriorBootCompatMainReclaim(previous, next Record) bool {
	previousReclaimable := previous.State == StateNormalMain ||
		(previous.State == StateRecoveryRequired &&
			(previous.Phase == PhaseIntentCommitted || previous.Phase == PhaseLoadAttempted) &&
			previous.FirstFailure != "" &&
			previous.ActiveOwner == OwnerCompatMain &&
			previous.ActiveMode == ModeFPGANative &&
			previous.QuiescingOwner == OwnerCompatMain &&
			previous.CandidateSession != "" &&
			previous.CandidateSession != previous.ActiveSession &&
			previous.CandidateGeneration > previous.ActiveGeneration &&
			previous.CandidateOwner == OwnerFPGADev &&
			previous.CandidateMode == ModeUpdating &&
			sameStrings(previous.RequestedResources, devLeases))
	return previousReclaimable &&
		next.State == StateRecoveringIntent &&
		next.Phase == PhaseIntentCommitted &&
		(previous.RunID == "" || next.RunID != previous.RunID) &&
		next.ActiveSession == previous.ActiveSession &&
		next.ActiveGeneration == previous.ActiveGeneration &&
		next.ActiveMode == previous.ActiveMode &&
		next.ActiveOwner == previous.ActiveOwner &&
		sameStrings(next.ActiveLeases, previous.ActiveLeases) &&
		next.QuiescingOwner == OwnerCompatMain &&
		next.CandidateOwner == OwnerFPGADev &&
		next.CandidateMode == ModeUpdating &&
		sameStrings(next.RequestedResources, devLeases)
}

func validatePhaseTransition(previous, next Record) error {
	if previous.State == next.State {
		switch previous.State {
		case StateRecoveringIntent, StateFPGADefaultActive:
			if phaseRanks[next.Phase] < phaseRanks[previous.Phase] || phaseRanks[next.Phase] > phaseRanks[previous.Phase]+1 {
				return fmt.Errorf("phase skipped or moved backward in %s", previous.State)
			}
		case StateRecoveryRequired:
			if next.Phase != previous.Phase {
				return fmt.Errorf("recovery_required phase changed")
			}
		default:
			if next.Phase != previous.Phase {
				return fmt.Errorf("phase changed in %s", previous.State)
			}
		}
		return nil
	}

	switch {
	case previous.State == StateNormalMain && next.State == StateRecoveringIntent:
		if next.Phase != PhaseIntentCommitted {
			return fmt.Errorf("normal_main -> recovering_intent must begin at intent_committed")
		}
	case previous.State == StateRecoveryRequired && next.State == StateRecoveringIntent:
		if next.Phase != PhaseIntentCommitted {
			return fmt.Errorf("recovery_required reclaim must begin at intent_committed")
		}
	case previous.State == StateRecoveringIntent && next.State == StateNoOwner:
		if previous.Phase != PhaseLoadAttempted || next.Phase != PhaseMainAbsent {
			return fmt.Errorf("recovering_intent -> no_owner requires load_attempted then main_absent")
		}
	case previous.State == StateNoOwner && next.State == StateFPGADefaultActive:
		if next.Phase != PhaseLeaseActive {
			return fmt.Errorf("no_owner -> fpgadev_active must begin at lease_active")
		}
	case previous.State == StateRecoveryRequired && next.State == StateNoOwner:
		if next.Phase != PhaseMainAbsent {
			return fmt.Errorf("recovery reconciliation must commit main_absent")
		}
	case previous.State == StateNoOwner && next.State == StateNormalMainStarting:
		if next.Phase != "" {
			return fmt.Errorf("no_owner -> normal_main_starting must clear the operation phase")
		}
	case previous.State == StateNormalMainStarting && next.State == StateNormalMain:
		if next.Phase != "" {
			return fmt.Errorf("normal_main_starting -> normal_main must keep an empty phase")
		}
	case previous.State == StateNormalMain && next.State == StateRecoveryRequired:
		if next.Phase != previous.Phase {
			return fmt.Errorf("normal_main failure must preserve the empty phase")
		}
	case next.State == StateRecoveryRequired && (previous.State == StateRecoveringIntent || previous.State == StateNoOwner || previous.State == StateFPGADefaultActive):
		if next.Phase != previous.Phase {
			return fmt.Errorf("failure transition to recovery_required must preserve the current phase")
		}
	case previous.State == StateNormalMainStarting && next.State == StateRecoveryRequired:
		if next.Phase != previous.Phase {
			return fmt.Errorf("normal_main_starting failure must preserve the empty phase")
		}
	}
	return nil
}

func validateRecoveryRequiredEntry(previous, next Record) error {
	if next.State != StateRecoveryRequired || previous.State == StateRecoveryRequired {
		return nil
	}
	if next.GenerationHighWater != previous.GenerationHighWater {
		return fmt.Errorf("recovery_required entry cannot allocate a generation")
	}
	if next.RunID != previous.RunID {
		return fmt.Errorf("recovery_required entry must preserve run_id")
	}
	if !sameActiveTuple(previous, next) {
		return fmt.Errorf("recovery_required entry must preserve the active tuple")
	}
	switch previous.State {
	case StateFPGADefaultActive:
		if !recoveryCandidateMatchesActive(previous, next) {
			return fmt.Errorf("fpgadev_active recovery_required entry must reconstruct the candidate intent")
		}
	case StateNormalMain, StateNormalMainStarting:
		if !sameCandidateIntent(previous, next) {
			return fmt.Errorf("normal-main recovery_required entry must preserve candidate absence")
		}
	default:
		if !sameCandidateIntent(previous, next) {
			return fmt.Errorf("recovery_required entry must preserve the candidate intent")
		}
	}
	return nil
}

func validateRecoveryRequiredSameState(previous, next Record) error {
	if previous.State != StateRecoveryRequired || next.State != StateRecoveryRequired {
		return nil
	}
	if previous.Schema != next.Schema || previous.Phase != next.Phase || previous.BootID != next.BootID || previous.RunID != next.RunID || previous.GenerationHighWater != next.GenerationHighWater || previous.FirstFailure != next.FirstFailure || previous.QuiescingOwner != next.QuiescingOwner || !sameActiveTuple(previous, next) || !sameCandidateIntent(previous, next) {
		return fmt.Errorf("same-state recovery_required replacement must preserve the complete record")
	}
	return nil
}

func sameActiveTuple(left, right Record) bool {
	return left.ActiveSession == right.ActiveSession && left.ActiveGeneration == right.ActiveGeneration && left.ActiveMode == right.ActiveMode && left.ActiveOwner == right.ActiveOwner && sameStrings(left.ActiveLeases, right.ActiveLeases)
}

func sameCandidateIntent(left, right Record) bool {
	return left.CandidateSession == right.CandidateSession && left.CandidateGeneration == right.CandidateGeneration && left.CandidateMode == right.CandidateMode && left.CandidateOwner == right.CandidateOwner && sameStrings(left.RequestedResources, right.RequestedResources)
}

func recoveryCandidateMatchesActive(previous, next Record) bool {
	return next.CandidateSession == previous.ActiveSession && next.CandidateGeneration == previous.ActiveGeneration && next.CandidateMode == previous.ActiveMode && next.CandidateOwner == previous.ActiveOwner && sameStrings(next.RequestedResources, devLeases)
}

func candidateIdentityChanged(previous, next Record) bool {
	return previous.CandidateSession != next.CandidateSession || previous.CandidateGeneration != next.CandidateGeneration
}

func activeIdentityChanged(previous, next Record) bool {
	return previous.ActiveSession != next.ActiveSession || previous.ActiveGeneration != next.ActiveGeneration
}

func isDevelopmentNoOwner(record Record) bool {
	return record.State == StateNoOwner && record.RunID != ""
}

func isRebootRecoveryNoOwner(record Record) bool {
	return record.State == StateNoOwner && record.RunID == ""
}

// MarshalCanonical emits the canonical field order and a single terminating
// newline. Validation deliberately happens before encoding so hostile values
// are rejected rather than normalized into a different record.
func MarshalCanonical(record Record) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal owner record: %w", err)
	}
	return append(encoded, '\n'), nil
}

// MarshalCanonical is also available as a method for callers that already
// hold a record value.
func (r Record) MarshalCanonical() ([]byte, error) { return MarshalCanonical(r) }

var recordFields = map[string]struct{}{
	"schema": {}, "state": {}, "phase": {}, "boot_id": {}, "run_id": {},
	"generation_high_water": {}, "active_session": {}, "active_generation": {},
	"active_mode": {}, "candidate_session": {}, "candidate_generation": {},
	"candidate_mode": {}, "quiescing_owner": {}, "candidate_owner": {},
	"active_owner": {}, "active_leases": {}, "requested_resources": {},
	"first_failure": {},
}

var recordFieldOrder = [...]string{
	"schema",
	"state",
	"phase",
	"boot_id",
	"run_id",
	"generation_high_water",
	"active_session",
	"active_generation",
	"active_mode",
	"candidate_session",
	"candidate_generation",
	"candidate_mode",
	"quiescing_owner",
	"candidate_owner",
	"active_owner",
	"active_leases",
	"requested_resources",
	"first_failure",
}

// Parse strictly decodes one complete owner record, rejecting duplicate and
// unknown fields, missing fields, trailing JSON values, invalid UTF-8, and all
// state/resource invariant violations.
func Parse(data []byte) (Record, error) {
	if !utf8.Valid(data) {
		return Record{}, fmt.Errorf("owner record is not valid UTF-8")
	}
	fields, err := scanRecordObject(data)
	if err != nil {
		return Record{}, err
	}
	for field := range fields {
		if _, ok := recordFields[field]; !ok {
			return Record{}, fmt.Errorf("unknown owner-record field %q", field)
		}
	}
	for field := range recordFields {
		if _, ok := fields[field]; !ok {
			return Record{}, fmt.Errorf("missing owner-record field %q", field)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("decode owner record: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Record{}, fmt.Errorf("trailing JSON value")
		}
		return Record{}, fmt.Errorf("trailing data: %w", err)
	}
	if err := record.Validate(); err != nil {
		return Record{}, fmt.Errorf("validate owner record: %w", err)
	}
	return record, nil
}

func scanRecordObject(data []byte) (map[string]struct{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("scan owner record: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("owner record must be a JSON object")
	}
	fields := make(map[string]struct{}, len(recordFields))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("scan owner-record key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("owner-record key is not a string")
		}
		if _, known := recordFields[key]; !known {
			return nil, fmt.Errorf("unknown owner-record field %q", key)
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, fmt.Errorf("duplicate owner-record field %q", key)
		}
		if len(fields) >= len(recordFieldOrder) {
			return nil, fmt.Errorf("non-canonical owner-record field order: unexpected field %q after %d fields", key, len(fields))
		}
		if key != recordFieldOrder[len(fields)] {
			return nil, fmt.Errorf("non-canonical owner-record field order: got %q at position %d, want %q", key, len(fields)+1, recordFieldOrder[len(fields)])
		}
		fields[key] = struct{}{}
		if err := scanJSONValue(decoder); err != nil {
			return nil, err
		}
	}
	end, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("scan owner-record end: %w", err)
	}
	if end != json.Delim('}') {
		return nil, fmt.Errorf("owner record object did not close")
	}
	if trailing, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON value %v", trailing)
		}
		return nil, fmt.Errorf("trailing data: %w", err)
	}
	return fields, nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("scan owner-record value: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("scan nested object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("nested object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate nested JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("nested object did not close")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("array did not close")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	return nil
}
