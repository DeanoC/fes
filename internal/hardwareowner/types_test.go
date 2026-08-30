package hardwareowner

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const (
	validBootID       = "01234567-89ab-cdef-0123-456789abcdef"
	validBootIDNext   = "fedcba98-7654-3210-fedc-ba9876543210"
	validSessionMain  = "11111111111111111111111111111111"
	validSessionDev   = "22222222222222222222222222222222"
	validSessionNext  = "33333333333333333333333333333333"
	validRunID        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	validRunIDNext    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	validFirstFailure = "mmio_failed"
)

var (
	validNormalLeases = []string{
		"command_fifo",
		"core_input_saves",
		"core_protocol",
		"fpga_bridges",
		"fpga_generation",
		"fpga_programming",
		"main_process_set",
		"native_video_audio",
	}
	validDevLeases = []string{"fpga_generation", "fpga_manager_gpi_gpo"}
)

func normalMainRecord() Record {
	return Record{
		Schema:              1,
		State:               "normal_main",
		Phase:               "",
		BootID:              validBootID,
		RunID:               "",
		GenerationHighWater: 42,
		ActiveSession:       validSessionMain,
		ActiveGeneration:    42,
		ActiveMode:          "fpga_native",
		CandidateSession:    "",
		CandidateGeneration: 0,
		CandidateMode:       "none",
		QuiescingOwner:      "none",
		CandidateOwner:      "none",
		ActiveOwner:         "compat_main",
		ActiveLeases:        append([]string(nil), validNormalLeases...),
		RequestedResources:  []string{},
		FirstFailure:        "",
	}
}

func recoveringIntentRecord() Record {
	return Record{
		Schema:              1,
		State:               "recovering_intent",
		Phase:               "intent_committed",
		BootID:              validBootID,
		RunID:               validRunID,
		GenerationHighWater: 42,
		ActiveSession:       validSessionMain,
		ActiveGeneration:    41,
		ActiveMode:          "fpga_native",
		CandidateSession:    validSessionDev,
		CandidateGeneration: 42,
		CandidateMode:       "updating",
		QuiescingOwner:      "compat_main",
		CandidateOwner:      "fpgadev",
		ActiveOwner:         "compat_main",
		ActiveLeases:        append([]string(nil), validNormalLeases...),
		RequestedResources:  append([]string(nil), validDevLeases...),
		FirstFailure:        "",
	}
}

func noOwnerRecord() Record {
	return Record{
		Schema:              1,
		State:               "no_owner",
		Phase:               "main_absent",
		BootID:              validBootID,
		RunID:               validRunID,
		GenerationHighWater: 42,
		ActiveSession:       "",
		ActiveGeneration:    0,
		ActiveMode:          "none",
		CandidateSession:    validSessionDev,
		CandidateGeneration: 42,
		CandidateMode:       "updating",
		QuiescingOwner:      "none",
		CandidateOwner:      "fpgadev",
		ActiveOwner:         "none",
		ActiveLeases:        []string{},
		RequestedResources:  append([]string(nil), validDevLeases...),
		FirstFailure:        "",
	}
}

func fpgadevActiveRecord() Record {
	return Record{
		Schema:              1,
		State:               "fpgadev_active",
		Phase:               "lease_active",
		BootID:              validBootID,
		RunID:               validRunID,
		GenerationHighWater: 42,
		ActiveSession:       validSessionDev,
		ActiveGeneration:    42,
		ActiveMode:          "updating",
		CandidateSession:    "",
		CandidateGeneration: 0,
		CandidateMode:       "none",
		QuiescingOwner:      "none",
		CandidateOwner:      "none",
		ActiveOwner:         "fpgadev",
		ActiveLeases:        append([]string(nil), validDevLeases...),
		RequestedResources:  []string{},
		FirstFailure:        "",
	}
}

func recoveryRequiredRecord() Record {
	return Record{
		Schema:              1,
		State:               "recovery_required",
		Phase:               "message_partial",
		BootID:              validBootID,
		RunID:               validRunID,
		GenerationHighWater: 42,
		ActiveSession:       validSessionDev,
		ActiveGeneration:    42,
		ActiveMode:          "updating",
		CandidateSession:    validSessionDev,
		CandidateGeneration: 42,
		CandidateMode:       "updating",
		QuiescingOwner:      "fpgadev",
		CandidateOwner:      "fpgadev",
		ActiveOwner:         "fpgadev",
		ActiveLeases:        append([]string(nil), validDevLeases...),
		RequestedResources:  append([]string(nil), validDevLeases...),
		FirstFailure:        validFirstFailure,
	}
}

func normalMainStartingRecord() Record {
	return Record{
		Schema:              1,
		State:               "normal_main_starting",
		Phase:               "",
		BootID:              validBootID,
		RunID:               "",
		GenerationHighWater: 43,
		ActiveSession:       validSessionNext,
		ActiveGeneration:    43,
		ActiveMode:          "fpga_native",
		CandidateSession:    "",
		CandidateGeneration: 0,
		CandidateMode:       "none",
		QuiescingOwner:      "none",
		CandidateOwner:      "none",
		ActiveOwner:         "compat_main",
		ActiveLeases:        append([]string(nil), validNormalLeases...),
		RequestedResources:  []string{},
		FirstFailure:        "",
	}
}

func cloneRecord(in Record) Record {
	out := in
	out.ActiveLeases = append([]string{}, in.ActiveLeases...)
	out.RequestedResources = append([]string{}, in.RequestedResources...)
	return out
}

func TestValidateAcceptsEveryCanonicalOwnerStateRow(t *testing.T) {
	tests := map[string]Record{
		"normal_main":          normalMainRecord(),
		"recovering_intent":    recoveringIntentRecord(),
		"no_owner":             noOwnerRecord(),
		"fpgadev_active":       fpgadevActiveRecord(),
		"recovery_required":    recoveryRequiredRecord(),
		"normal_main_starting": normalMainStartingRecord(),
	}
	for name, record := range tests {
		name, record := name, record
		t.Run(name, func(t *testing.T) {
			if err := record.Validate(); err != nil {
				t.Fatalf("canonical %s row rejected: %v", name, err)
			}
		})
	}
}

func TestValidateRejectsIndependentMutationOfEveryField(t *testing.T) {
	mutations := map[string]func(*Record){
		"schema":                func(r *Record) { r.Schema = 2 },
		"state":                 func(r *Record) { r.State = "unknown" },
		"phase":                 func(r *Record) { r.Phase = "done_observed" },
		"boot_id":               func(r *Record) { r.BootID = "BOOT" },
		"run_id":                func(r *Record) { r.RunID = "not-a-run-id" },
		"generation_high_water": func(r *Record) { r.GenerationHighWater = 0 },
		"active_session":        func(r *Record) { r.ActiveSession = "not-a-session" },
		"active_generation":     func(r *Record) { r.ActiveGeneration = 0 },
		"active_mode":           func(r *Record) { r.ActiveMode = "updating" },
		"candidate_session":     func(r *Record) { r.CandidateSession = "not-a-session" },
		"candidate_generation":  func(r *Record) { r.CandidateGeneration = 1 },
		"candidate_mode":        func(r *Record) { r.CandidateMode = "fpga_native" },
		"quiescing_owner":       func(r *Record) { r.QuiescingOwner = "fpgadev" },
		"candidate_owner":       func(r *Record) { r.CandidateOwner = "compat_main" },
		"active_owner":          func(r *Record) { r.ActiveOwner = "fpgadev" },
		"active_leases":         func(r *Record) { r.ActiveLeases = []string{"fpga_generation"} },
		"requested_resources":   func(r *Record) { r.RequestedResources = []string{"fpga_generation", "fpga_generation"} },
		"first_failure":         func(r *Record) { r.FirstFailure = "not-a-result-code" },
	}
	for field, mutate := range mutations {
		field, mutate := field, mutate
		t.Run(field, func(t *testing.T) {
			record := normalMainRecord()
			mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatalf("mutation of %s was accepted", field)
			}
		})
	}
}

func TestValidateRejectsNonCanonicalSessionsModesAndArrays(t *testing.T) {
	tests := map[string]func(*Record){
		"uppercase session":         func(r *Record) { r.ActiveSession = "1111111111111111111111111111111A" },
		"short session":             func(r *Record) { r.ActiveSession = "1111" },
		"none mode on active owner": func(r *Record) { r.ActiveMode = "none" },
		"unknown mode":              func(r *Record) { r.ActiveMode = "host_cast" },
		"unsorted active leases": func(r *Record) {
			r.ActiveLeases[0], r.ActiveLeases[1] = r.ActiveLeases[1], r.ActiveLeases[0]
		},
		"duplicate active leases": func(r *Record) { r.ActiveLeases[1] = r.ActiveLeases[0] },
		"nil requested resources": func(r *Record) { r.RequestedResources = nil },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			record := normalMainRecord()
			mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatalf("non-canonical %s accepted", name)
			}
		})
	}
}

func TestValidateDerivesEveryActiveLeaseFromTheOwnerTuple(t *testing.T) {
	tests := []struct {
		name       string
		record     Record
		session    string
		generation uint64
		mode       string
		resources  []string
	}{
		{"main", normalMainRecord(), validSessionMain, 42, "fpga_native", validNormalLeases},
		{"development", fpgadevActiveRecord(), validSessionDev, 42, "updating", validDevLeases},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.record.Validate(); err != nil {
				t.Fatal(err)
			}
			if test.record.ActiveSession != test.session || test.record.ActiveGeneration != test.generation || test.record.ActiveMode != test.mode {
				t.Fatalf("active tuple = (%q,%d,%q), want (%q,%d,%q)", test.record.ActiveSession, test.record.ActiveGeneration, test.record.ActiveMode, test.session, test.generation, test.mode)
			}
			if !reflect.DeepEqual(test.record.ActiveLeases, test.resources) {
				t.Fatalf("active resources = %#v, want %#v", test.record.ActiveLeases, test.resources)
			}
		})
	}
}

func TestParseRejectsUnknownDuplicateAndTrailingJSON(t *testing.T) {
	canonical := canonicalJSONFixture()
	tests := map[string]string{
		"unknown field":   strings.Replace(canonical, `"first_failure":""`, `"first_failure":"","unexpected":true`, 1),
		"duplicate field": strings.Replace(canonical, `"schema":1`, `"schema":1,"schema":1`, 1),
		"trailing object": canonical + `{}`,
		"trailing token":  canonical + ` true`,
		"invalid utf8":    string([]byte{'{', '"', 's', 'c', 'h', 'e', 'm', 'a', '"', ':', '1', 0xff, '}'}),
	}
	for name, input := range tests {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatalf("%s input accepted", name)
			}
		})
	}
}

func TestParseAcceptsCanonicalRecordAndRetainsValues(t *testing.T) {
	record, err := Parse([]byte(canonicalJSONFixture()))
	if err != nil {
		t.Fatal(err)
	}
	want := recoveringIntentRecord()
	if !reflect.DeepEqual(record, want) {
		t.Fatalf("parsed record = %#v, want %#v", record, want)
	}
}

func TestMarshalCanonicalUsesSchemaFieldOrderAndDoesNotSortInput(t *testing.T) {
	record := recoveringIntentRecord()
	got, err := MarshalCanonical(record)
	if err != nil {
		t.Fatal(err)
	}
	want := canonicalJSONFixture() + "\n"
	if string(got) != want {
		t.Fatalf("canonical JSON = %s, want %s", got, want)
	}
	if !reflect.DeepEqual(record.ActiveLeases, validNormalLeases) || !reflect.DeepEqual(record.RequestedResources, validDevLeases) {
		t.Fatal("canonical marshaling mutated resource order")
	}
}

func TestRecordMarshalCanonicalMethodMatchesPackageFunction(t *testing.T) {
	record := normalMainRecord()
	got, err := record.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	want, err := MarshalCanonical(record)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("method bytes differ: %q vs %q", got, want)
	}
}

func TestValidateTransitionRejectsGenerationRegressionAndCandidateDiscard(t *testing.T) {
	previous := recoveringIntentRecord()
	tests := map[string]func(*Record){
		"high-water decreases":        func(next *Record) { next.GenerationHighWater = previous.GenerationHighWater - 1 },
		"active generation decreases": func(next *Record) { next.ActiveGeneration = previous.ActiveGeneration - 1 },
		"candidate identity is discarded during recovery": func(next *Record) {
			next.State = "recovery_required"
			next.Phase = "message_partial"
			next.ActiveSession = previous.ActiveSession
			next.ActiveGeneration = previous.ActiveGeneration
			next.ActiveMode = previous.ActiveMode
			next.ActiveOwner = previous.ActiveOwner
			next.ActiveLeases = append([]string(nil), previous.ActiveLeases...)
			next.CandidateSession = ""
			next.CandidateGeneration = 0
			next.CandidateMode = "none"
			next.QuiescingOwner = "compat_main"
			next.CandidateOwner = "none"
			next.RequestedResources = []string{}
			next.FirstFailure = validFirstFailure
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			next := recoveryRequiredRecord()
			mutate(&next)
			if err := next.ValidateTransition(previous); err == nil {
				t.Fatalf("invalid %s transition accepted", name)
			}
		})
	}
}

func TestValidateTransitionPreservesCandidateAcrossRecovery(t *testing.T) {
	previous := recoveringIntentRecord()
	next := recoveryRequiredRecord()
	next.Phase = previous.Phase
	next.ActiveSession = previous.ActiveSession
	next.ActiveGeneration = previous.ActiveGeneration
	next.ActiveMode = previous.ActiveMode
	next.ActiveOwner = previous.ActiveOwner
	next.ActiveLeases = append([]string(nil), previous.ActiveLeases...)
	next.QuiescingOwner = previous.QuiescingOwner
	if err := next.ValidateTransition(previous); err != nil {
		t.Fatalf("recovery transition rejected: %v", err)
	}
	if next.CandidateSession != validSessionDev || next.CandidateGeneration != 42 || next.RunID != validRunID {
		t.Fatalf("recovery candidate identity not preserved: %#v", next)
	}
}

func TestFixRound3ReconstructsRecoveryCandidateFromFPGADActive(t *testing.T) {
	previous := fpgadevActiveRecord()
	next := recoveryRequiredRecord()
	next.Phase = previous.Phase
	next.CandidateSession = previous.ActiveSession
	next.CandidateGeneration = previous.ActiveGeneration
	next.CandidateMode = previous.ActiveMode
	next.CandidateOwner = previous.ActiveOwner
	next.RequestedResources = append([]string(nil), devLeases...)
	if err := next.ValidateTransition(previous); err != nil {
		t.Fatalf("active-to-recovery candidate reconstruction rejected: %v", err)
	}
	if next.CandidateSession != previous.ActiveSession || next.CandidateGeneration != previous.ActiveGeneration || next.CandidateMode != previous.ActiveMode || next.CandidateOwner != previous.ActiveOwner || !sameStrings(next.RequestedResources, devLeases) {
		t.Fatalf("recovery candidate was not reconstructed from active tuple: %#v", next)
	}

	for _, test := range []struct {
		name   string
		mutate func(*Record)
	}{
		{
			name: "candidate absent",
			mutate: func(record *Record) {
				record.CandidateSession = ""
				record.CandidateGeneration = 0
				record.CandidateMode = ModeNone
				record.CandidateOwner = OwnerNone
				record.RequestedResources = []string{}
			},
		},
		{
			name: "candidate identity mismatch",
			mutate: func(record *Record) {
				record.CandidateSession = validSessionNext
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := next
			test.mutate(&candidate)
			if err := candidate.ValidateTransition(previous); err == nil {
				t.Fatal("fpgadev_active recovery candidate mismatch was accepted")
			}
		})
	}
}

func canonicalJSONFixture() string {
	return `{"schema":1,"state":"recovering_intent","phase":"intent_committed","boot_id":"01234567-89ab-cdef-0123-456789abcdef","run_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation_high_water":42,"active_session":"11111111111111111111111111111111","active_generation":41,"active_mode":"fpga_native","candidate_session":"22222222222222222222222222222222","candidate_generation":42,"candidate_mode":"updating","quiescing_owner":"compat_main","candidate_owner":"fpgadev","active_owner":"compat_main","active_leases":["command_fifo","core_input_saves","core_protocol","fpga_bridges","fpga_generation","fpga_programming","main_process_set","native_video_audio"],"requested_resources":["fpga_generation","fpga_manager_gpi_gpo"],"first_failure":""}`
}

func TestRecordJSONTagsRemainExact(t *testing.T) {
	type field struct {
		name string
		want string
	}
	// MarshalCanonical exercises the tags as well as the required order. This
	// second check catches accidental omission of a zero-valued field.
	got, err := json.Marshal(recoveringIntentRecord())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []field{
		{"schema", `"schema":1`},
		{"generation_high_water", `"generation_high_water":42`},
		{"active_leases", `"active_leases":[`},
		{"requested_resources", `"requested_resources":[`},
	} {
		if !strings.Contains(string(got), test.want) {
			t.Fatalf("field %s missing from JSON %s", test.name, got)
		}
	}
}

func TestFixRound1EnforcesOrderedStateTransitions(t *testing.T) {
	// A state that is internally valid is still invalid when it skips the
	// durable quiescing and no-owner checkpoints.
	direct := fpgadevActiveRecord()
	direct.ActiveSession = validSessionNext
	direct.ActiveGeneration = 43
	direct.GenerationHighWater = 43
	if err := direct.ValidateTransition(normalMainRecord()); err == nil {
		t.Fatal("normal_main to fpgadev_active direct transition was accepted")
	}

	recovering := recoveringIntentRecord()
	recovering.GenerationHighWater = 43
	recovering.ActiveGeneration = 42
	recovering.CandidateGeneration = 43
	recoveringLoad := recovering
	recoveringLoad.Phase = PhaseLoadAttempted
	noOwner := noOwnerRecord()
	noOwner.GenerationHighWater = 43
	noOwner.CandidateGeneration = 43
	active := fpgadevActiveRecord()
	active.GenerationHighWater = 43
	active.ActiveGeneration = 43
	failure := recoveryRequiredRecord()
	failure.Phase = noOwner.Phase
	failure.ActiveSession = noOwner.ActiveSession
	failure.ActiveGeneration = noOwner.ActiveGeneration
	failure.ActiveMode = noOwner.ActiveMode
	failure.ActiveOwner = noOwner.ActiveOwner
	failure.ActiveLeases = append([]string{}, noOwner.ActiveLeases...)
	failure.QuiescingOwner = noOwner.QuiescingOwner
	failure.GenerationHighWater = 43
	failure.CandidateGeneration = 43
	failure.CandidateSession = noOwner.CandidateSession
	failure.CandidateMode = noOwner.CandidateMode
	failure.CandidateOwner = noOwner.CandidateOwner
	failure.RequestedResources = append([]string(nil), noOwner.RequestedResources...)
	bootNoOwner := rebootRecoveryNoOwnerRecord(failure)
	starting := normalMainStartingRecord()
	starting.BootID = "fedcba98-7654-3210-fedc-ba9876543210"
	starting.GenerationHighWater = 44
	starting.ActiveGeneration = 44
	finished := starting
	finished.State = StateNormalMain

	ordered := []struct {
		name string
		from Record
		to   Record
	}{
		{"normal_main to recovering_intent", normalMainRecord(), recovering},
		{"recovering_intent phase intent_committed to load_attempted", recovering, recoveringLoad},
		{"recovering_intent to no_owner", recoveringLoad, noOwner},
		{"no_owner to fpgadev_active", noOwner, active},
		{"no_owner to recovery_required", noOwner, failure},
		{"recovery_required to no_owner records successor boot", failure, bootNoOwner},
		{"reboot no_owner to normal_main_starting", bootNoOwner, starting},
		{"normal_main_starting to normal_main", starting, finished},
	}
	for _, test := range ordered {
		t.Run(test.name, func(t *testing.T) {
			if err := test.to.ValidateTransition(test.from); err != nil {
				t.Fatalf("legal transition rejected: %v", err)
			}
		})
	}
}

func TestFixRound1RejectsNonCanonicalTopLevelOrder(t *testing.T) {
	canonical := canonicalJSONFixture()
	swapped := strings.Replace(canonical,
		`{"schema":1,"state":"recovering_intent"`,
		`{"state":"recovering_intent","schema":1`, 1)
	if _, err := Parse([]byte(swapped)); err == nil {
		t.Fatal("record with swapped top-level keys was accepted")
	}
}

func TestFixRound1EnforcesCurrentAndIncrementalGenerations(t *testing.T) {
	tests := []struct {
		name  string
		check func(*testing.T)
	}{
		{
			name: "standalone normal_main active generation must equal high-water",
			check: func(t *testing.T) {
				record := normalMainRecord()
				record.GenerationHighWater = 43
				if err := record.Validate(); err == nil {
					t.Fatal("stale active generation was accepted")
				}
			},
		},
		{
			name: "standalone recovering_intent candidate generation must equal high-water",
			check: func(t *testing.T) {
				record := recoveringIntentRecord()
				record.GenerationHighWater = 43
				if err := record.Validate(); err == nil {
					t.Fatal("stale candidate generation was accepted")
				}
			},
		},
		{
			name: "new allocation must increment high-water exactly once",
			check: func(t *testing.T) {
				next := normalMainStartingRecord()
				next.GenerationHighWater = 44
				next.ActiveGeneration = 44
				if err := next.ValidateTransition(normalMainRecord()); err == nil {
					t.Fatal("jumped active allocation was accepted")
				}
			},
		},
		{
			name: "new candidate allocation must increment high-water exactly once",
			check: func(t *testing.T) {
				next := recoveringIntentRecord()
				next.GenerationHighWater = 44
				next.CandidateGeneration = 44
				if err := next.ValidateTransition(normalMainRecord()); err == nil {
					t.Fatal("jumped candidate allocation was accepted")
				}
			},
		},
		{
			name: "exactly one new candidate generation is legal",
			check: func(t *testing.T) {
				next := recoveringIntentRecord()
				next.ActiveGeneration = 42
				next.GenerationHighWater = 43
				next.CandidateGeneration = 43
				if err := next.ValidateTransition(normalMainRecord()); err != nil {
					t.Fatalf("exact +1 candidate allocation rejected: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, test.check)
	}
}

func TestFixRound3RejectsRecoveryRequiredWithoutCandidateFromFPGADActive(t *testing.T) {
	previous := fpgadevActiveRecord()
	next := recoveryRequiredRecord()
	next.Phase = previous.Phase
	next.CandidateSession = ""
	next.CandidateGeneration = 0
	next.CandidateMode = ModeNone
	next.CandidateOwner = OwnerNone
	next.RequestedResources = []string{}

	if err := next.ValidateTransition(previous); err == nil {
		t.Fatal("fpgadev_active recovery_required transition accepted without reconstructed candidate")
	}
}

func TestFixRound2AllowsRecoveryRequiredWithoutCandidateFromNormalMainStarting(t *testing.T) {
	previous := normalMainStartingRecord()
	next := recoveryRequiredRecord()
	next.ActiveSession = previous.ActiveSession
	next.ActiveGeneration = previous.ActiveGeneration
	next.ActiveMode = previous.ActiveMode
	next.ActiveOwner = previous.ActiveOwner
	next.ActiveLeases = append([]string(nil), previous.ActiveLeases...)
	next.GenerationHighWater = previous.GenerationHighWater
	next.QuiescingOwner = previous.ActiveOwner
	next.RunID = previous.RunID
	next.Phase = previous.Phase
	next.CandidateSession = ""
	next.CandidateGeneration = 0
	next.CandidateMode = ModeNone
	next.CandidateOwner = OwnerNone
	next.RequestedResources = []string{}

	if err := next.Validate(); err != nil {
		t.Fatalf("canonical recovery_required without candidate rejected: %v", err)
	}
	if err := next.ValidateTransition(previous); err != nil {
		t.Fatalf("normal_main_starting recovery_required transition rejected: %v", err)
	}
}

func TestFixRound2PreservesCandidateOnRecoveryFailureEntries(t *testing.T) {
	recovering := recoveringIntentRecord()
	recovering.Phase = PhaseLoadAttempted
	recovering.GenerationHighWater = 43
	recovering.ActiveGeneration = 42
	recovering.CandidateGeneration = 43
	recoveringFailure := recoveryRequiredRecord()
	recoveringFailure.Phase = recovering.Phase
	recoveringFailure.ActiveSession = recovering.ActiveSession
	recoveringFailure.ActiveGeneration = recovering.ActiveGeneration
	recoveringFailure.ActiveMode = recovering.ActiveMode
	recoveringFailure.ActiveOwner = recovering.ActiveOwner
	recoveringFailure.ActiveLeases = append([]string(nil), recovering.ActiveLeases...)
	recoveringFailure.QuiescingOwner = recovering.QuiescingOwner
	recoveringFailure.GenerationHighWater = recovering.GenerationHighWater
	recoveringFailure.CandidateSession = recovering.CandidateSession
	recoveringFailure.CandidateGeneration = recovering.CandidateGeneration
	recoveringFailure.CandidateMode = recovering.CandidateMode
	recoveringFailure.CandidateOwner = recovering.CandidateOwner
	recoveringFailure.RequestedResources = append([]string(nil), recovering.RequestedResources...)
	if err := recoveringFailure.ValidateTransition(recovering); err != nil {
		t.Fatalf("recovering_intent failure transition rejected: %v", err)
	}
	if recoveringFailure.CandidateSession != recovering.CandidateSession || recoveringFailure.CandidateGeneration != recovering.CandidateGeneration {
		t.Fatal("recovering_intent candidate was not preserved")
	}

	noOwner := noOwnerRecord()
	noOwner.GenerationHighWater = 43
	noOwner.CandidateGeneration = 43
	noOwnerFailure := recoveryRequiredRecord()
	noOwnerFailure.Phase = noOwner.Phase
	noOwnerFailure.ActiveSession = noOwner.ActiveSession
	noOwnerFailure.ActiveGeneration = noOwner.ActiveGeneration
	noOwnerFailure.ActiveMode = noOwner.ActiveMode
	noOwnerFailure.ActiveOwner = noOwner.ActiveOwner
	noOwnerFailure.ActiveLeases = append([]string{}, noOwner.ActiveLeases...)
	noOwnerFailure.QuiescingOwner = noOwner.QuiescingOwner
	noOwnerFailure.GenerationHighWater = noOwner.GenerationHighWater
	noOwnerFailure.CandidateSession = noOwner.CandidateSession
	noOwnerFailure.CandidateGeneration = noOwner.CandidateGeneration
	noOwnerFailure.CandidateMode = noOwner.CandidateMode
	noOwnerFailure.CandidateOwner = noOwner.CandidateOwner
	noOwnerFailure.RequestedResources = append([]string(nil), noOwner.RequestedResources...)
	if err := noOwnerFailure.ValidateTransition(noOwner); err != nil {
		t.Fatalf("no_owner failure transition rejected: %v", err)
	}
	if noOwnerFailure.CandidateSession != noOwner.CandidateSession || noOwnerFailure.CandidateGeneration != noOwner.CandidateGeneration {
		t.Fatal("no_owner candidate was not preserved")
	}
}

func TestFixRound4PhaseEdgeMatrix(t *testing.T) {
	normal := normalMainRecord()
	intent := recoveringIntentRecord()
	intent.ActiveGeneration = 42
	intent.GenerationHighWater = 43
	intent.CandidateGeneration = 43
	load := cloneRecord(intent)
	load.Phase = PhaseLoadAttempted
	noOwner := noOwnerRecord()
	noOwner.GenerationHighWater = 43
	noOwner.CandidateGeneration = 43
	active := fpgadevActiveRecord()
	active.GenerationHighWater = 43
	active.ActiveGeneration = 43
	hello := cloneRecord(active)
	hello.Phase = PhaseHelloObserved
	message := cloneRecord(active)
	message.Phase = PhaseMessagePartial
	end := cloneRecord(active)
	end.Phase = PhaseEndAckWritten
	done := cloneRecord(active)
	done.Phase = PhaseDoneObserved
	rebootFailure := recoveryRequiredAfter(done)
	rebootCheckpoint := rebootRecoveryNoOwnerRecord(rebootFailure)
	starting := normalMainStartingRecord()
	starting.GenerationHighWater = 44
	starting.ActiveGeneration = 44
	starting.BootID = validBootIDNext
	finished := cloneRecord(starting)
	finished.State = StateNormalMain
	fabricatedStartingFailurePhase := recoveryRequiredAfter(starting)
	fabricatedStartingFailurePhase.Phase = PhaseHelloObserved

	legal := []struct {
		name string
		from Record
		to   Record
	}{
		{"normal empty -> intent_committed", normal, intent},
		{"intent_committed -> load_attempted", intent, load},
		{"load_attempted -> main_absent", load, noOwner},
		{"main_absent -> lease_active", noOwner, active},
		{"lease_active -> hello_observed", active, hello},
		{"hello_observed -> message_partial", hello, message},
		{"message_partial -> end_ack_written", message, end},
		{"end_ack_written -> done_observed", end, done},
		{"normal failure preserves empty phase", normal, recoveryRequiredAfter(normal)},
		{"intent failure preserves intent_committed", intent, recoveryRequiredAfter(intent)},
		{"load failure preserves load_attempted", load, recoveryRequiredAfter(load)},
		{"no-owner failure preserves main_absent", noOwner, recoveryRequiredAfter(noOwner)},
		{"lease failure preserves lease_active", active, recoveryRequiredAfter(active)},
		{"hello failure preserves hello_observed", hello, recoveryRequiredAfter(hello)},
		{"message failure preserves message_partial", message, recoveryRequiredAfter(message)},
		{"end failure preserves end_ack_written", end, recoveryRequiredAfter(end)},
		{"done failure preserves done_observed", done, recoveryRequiredAfter(done)},
		{"reboot recovery resets to main_absent", rebootFailure, rebootCheckpoint},
		{"recovery checkpoint clears phase", rebootCheckpoint, starting},
		{"normal startup keeps empty phase", starting, finished},
		{"normal startup failure keeps empty phase", starting, recoveryRequiredAfter(starting)},
	}
	for _, test := range legal {
		t.Run("legal/"+test.name, func(t *testing.T) {
			assertValidPhaseFixtures(t, test.from, test.to)
			if err := test.to.ValidateTransition(test.from); err != nil {
				t.Fatalf("legal phase edge rejected: %v", err)
			}
		})
	}

	rejected := []struct {
		name    string
		from    Record
		to      Record
		wantErr string
	}{
		{"backward load_attempted -> intent_committed", load, intent, "phase skipped or moved backward"},
		{"backward hello_observed -> lease_active", hello, active, "phase skipped or moved backward"},
		{"backward message_partial -> hello_observed", message, hello, "phase skipped or moved backward"},
		{"backward end_ack_written -> message_partial", end, message, "phase skipped or moved backward"},
		{"backward done_observed -> end_ack_written", done, end, "phase skipped or moved backward"},
		{"skip normal empty -> load_attempted", normal, load, "must begin at intent_committed"},
		{"skip intent_committed -> main_absent", intent, noOwner, "requires load_attempted then main_absent"},
		{"skip main_absent -> hello_observed", noOwner, hello, "must begin at lease_active"},
		{"skip lease_active -> message_partial", active, message, "phase skipped or moved backward"},
		{"skip hello_observed -> end_ack_written", hello, end, "phase skipped or moved backward"},
		{"skip message_partial -> done_observed", message, done, "phase skipped or moved backward"},
		{"fabricated normal_main_starting failure phase", starting, fabricatedStartingFailurePhase, "normal_main_starting failure must preserve the empty phase"},
	}
	for _, test := range rejected {
		t.Run("rejected/"+test.name, func(t *testing.T) {
			assertValidPhaseFixtures(t, test.from, test.to)
			err := test.to.ValidateTransition(test.from)
			if err == nil {
				t.Fatal("invalid phase edge was accepted")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("transition failed for the wrong reason: got %v, want %q", err, test.wantErr)
			}
		})
	}
}

func recoveryRequiredAfter(previous Record) Record {
	next := cloneRecord(previous)
	next.State = StateRecoveryRequired
	next.FirstFailure = validFirstFailure
	next.QuiescingOwner = previous.ActiveOwner
	if previous.State == StateFPGADefaultActive {
		next.CandidateSession = previous.ActiveSession
		next.CandidateGeneration = previous.ActiveGeneration
		next.CandidateMode = previous.ActiveMode
		next.CandidateOwner = previous.ActiveOwner
		next.RequestedResources = append([]string{}, validDevLeases...)
	}
	if previous.RunID == "" {
		next.FirstFailure = ""
	}
	return next
}

func assertValidPhaseFixtures(t *testing.T, from, to Record) {
	t.Helper()
	if err := from.Validate(); err != nil {
		t.Fatalf("invalid predecessor fixture: %v", err)
	}
	if err := to.Validate(); err != nil {
		t.Fatalf("invalid successor fixture: %v", err)
	}
}

func TestFixRound2RejectsRecoveryRequiredSameStateMutation(t *testing.T) {
	previous := recoveryRequiredRecord()
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{
			name: "active identity",
			mutate: func(next *Record) {
				next.ActiveSession = validSessionNext
			},
		},
		{
			name: "candidate identity",
			mutate: func(next *Record) {
				next.CandidateSession = validSessionNext
			},
		},
		{
			name: "higher active generation and high-water",
			mutate: func(next *Record) {
				next.ActiveSession = validSessionNext
				next.ActiveGeneration = 43
				next.GenerationHighWater = 43
				next.CandidateSession = ""
				next.CandidateGeneration = 0
				next.CandidateMode = ModeNone
				next.CandidateOwner = OwnerNone
				next.RequestedResources = []string{}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := previous
			test.mutate(&next)
			if err := next.ValidateTransition(previous); err == nil {
				t.Fatal("same-state recovery_required mutation was accepted")
			}
		})
	}
}

func TestFixRound2RejectsReusingCandidateAsNormalMainStartingActive(t *testing.T) {
	previous := noOwnerRecord()
	next := normalMainStartingRecord()
	next.ActiveSession = previous.CandidateSession
	next.ActiveGeneration = previous.CandidateGeneration
	next.GenerationHighWater = previous.GenerationHighWater

	if err := next.ValidateTransition(previous); err == nil {
		t.Fatal("normal_main_starting reused the candidate without a fresh allocation")
	}
}

func TestFixRound3RejectsRecoveryRequiredSameStatePreservationMutations(t *testing.T) {
	previous := recoveryRequiredRecord()
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{
			name: "clear active tuple",
			mutate: func(next *Record) {
				next.ActiveSession = ""
				next.ActiveGeneration = 0
				next.ActiveMode = ModeNone
				next.ActiveOwner = OwnerNone
				next.ActiveLeases = []string{}
				next.QuiescingOwner = OwnerNone
			},
		},
		{
			name: "replace active identity",
			mutate: func(next *Record) {
				next.ActiveSession = validSessionNext
			},
		},
		{
			name: "replace active leases",
			mutate: func(next *Record) {
				next.ActiveLeases = []string{"fpga_generation"}
			},
		},
		{
			name: "replace candidate identity",
			mutate: func(next *Record) {
				next.CandidateSession = validSessionNext
			},
		},
		{
			name: "replace candidate resources",
			mutate: func(next *Record) {
				next.RequestedResources = []string{"fpga_generation"}
			},
		},
		{
			name: "replace quiescing owner",
			mutate: func(next *Record) {
				next.QuiescingOwner = OwnerCompatMain
			},
		},
		{
			name: "advance high-water",
			mutate: func(next *Record) {
				next.GenerationHighWater = 43
			},
		},
		{
			name: "replace run id",
			mutate: func(next *Record) {
				next.RunID = validRunIDNext
			},
		},
		{
			name: "replace boot id",
			mutate: func(next *Record) {
				next.BootID = "fedcba98-7654-3210-fedc-ba9876543210"
			},
		},
		{
			name: "replace first failure",
			mutate: func(next *Record) {
				next.FirstFailure = "message_timeout"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := previous
			test.mutate(&next)
			if err := next.ValidateTransition(previous); err == nil {
				t.Fatal("same-state recovery_required mutation was accepted")
			}
		})
	}
}

func TestFixRound4AllowsNormalOwnerFailureWithoutDevelopmentRun(t *testing.T) {
	for _, previous := range []Record{normalMainRecord(), normalMainStartingRecord()} {
		t.Run(string(previous.State), func(t *testing.T) {
			next := cloneRecord(previous)
			next.State = StateRecoveryRequired
			next.QuiescingOwner = OwnerCompatMain

			if err := next.ValidateTransition(previous); err != nil {
				t.Fatalf("normal-owner failure with no development run rejected: %v", err)
			}
		})
	}

	t.Run("normal failure cannot fabricate development run id", func(t *testing.T) {
		previous := normalMainRecord()
		next := cloneRecord(previous)
		next.State = StateRecoveryRequired
		next.RunID = validRunID
		next.QuiescingOwner = OwnerCompatMain
		if err := next.ValidateTransition(previous); err == nil {
			t.Fatal("normal-owner failure fabricated a development run ID")
		}
	})

	t.Run("development recovery still requires run id", func(t *testing.T) {
		previous := fpgadevActiveRecord()
		next := recoveryRequiredRecord()
		next.Phase = previous.Phase
		next.RunID = ""
		next.CandidateSession = previous.ActiveSession
		next.CandidateGeneration = previous.ActiveGeneration
		next.CandidateMode = previous.ActiveMode
		next.CandidateOwner = previous.ActiveOwner
		next.RequestedResources = append([]string(nil), validDevLeases...)
		if err := next.ValidateTransition(previous); err == nil {
			t.Fatal("development recovery without its run ID was accepted")
		}
	})
}

func TestFixRound4EnforcesRebootBoundaryAcrossNoOwner(t *testing.T) {
	recovery := recoveryRequiredRecord()
	checkpoint := rebootRecoveryNoOwnerRecord(recovery)

	t.Run("reboot checkpoint records successor boot and clears development identity", func(t *testing.T) {
		if err := checkpoint.ValidateTransition(recovery); err != nil {
			t.Fatalf("candidate-free successor-boot checkpoint rejected: %v", err)
		}
	})

	t.Run("fresh normal owner preserves the successor boot id", func(t *testing.T) {
		starting := normalMainStartingRecord()
		starting.BootID = checkpoint.BootID
		if err := starting.ValidateTransition(checkpoint); err != nil {
			t.Fatalf("same-successor-boot normal_main_starting rejected: %v", err)
		}
	})

	t.Run("development checkpoint cannot start main", func(t *testing.T) {
		developmentCheckpoint := noOwnerRecord()
		starting := normalMainStartingRecord()
		if err := starting.ValidateTransition(developmentCheckpoint); err == nil {
			t.Fatal("development no_owner restarted Main")
		}
	})

	t.Run("same-boot development checkpoint transfers to fpgadev", func(t *testing.T) {
		developmentCheckpoint := noOwnerRecord()
		if err := fpgadevActiveRecord().ValidateTransition(developmentCheckpoint); err != nil {
			t.Fatalf("same-boot development transfer rejected: %v", err)
		}
	})
}

func TestPreviousBootNormalMainMayCommitOnlyExactCurrentBootDevelopmentIntent(t *testing.T) {
	previous := normalMainRecord()
	intent := cloneRecord(previous)
	intent.State = StateRecoveringIntent
	intent.Phase = PhaseIntentCommitted
	intent.BootID = validBootIDNext
	intent.RunID = validRunID
	intent.GenerationHighWater++
	intent.CandidateSession = validSessionNext
	intent.CandidateGeneration = intent.GenerationHighWater
	intent.CandidateMode = ModeUpdating
	intent.QuiescingOwner = OwnerCompatMain
	intent.CandidateOwner = OwnerFPGADev
	intent.RequestedResources = append([]string(nil), validDevLeases...)

	if err := intent.ValidateTransition(previous); err != nil {
		t.Fatalf("exact prior-boot normal_main reclaim rejected: %v", err)
	}
	mutations := []struct {
		name string
		edit func(*Record)
	}{
		{name: "same state", edit: func(next *Record) { next.State, next.Phase = StateNormalMain, "" }},
		{name: "wrong phase", edit: func(next *Record) { next.Phase = PhaseLoadAttempted }},
		{name: "changed active session", edit: func(next *Record) { next.ActiveSession = validSessionDev }},
		{name: "changed active generation", edit: func(next *Record) { next.ActiveGeneration++ }},
		{name: "changed active owner", edit: func(next *Record) { next.ActiveOwner = OwnerFPGADev }},
		{name: "missing compat quiescer", edit: func(next *Record) { next.QuiescingOwner = OwnerNone }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			next := cloneRecord(intent)
			mutation.edit(&next)
			if err := next.ValidateTransition(previous); err == nil {
				t.Fatal("neighboring cross-boot transition was accepted")
			}
		})
	}
}

func TestFixRound4FreshCandidateSessionDiffersFromPriorActive(t *testing.T) {
	previous := normalMainRecord()
	candidate := recoveringIntentRecord()
	candidate.ActiveGeneration = previous.ActiveGeneration
	candidate.GenerationHighWater = previous.GenerationHighWater + 1
	candidate.CandidateGeneration = candidate.GenerationHighWater
	if err := candidate.ValidateTransition(previous); err != nil {
		t.Fatalf("fresh development candidate control rejected: %v", err)
	}

	reused := candidate
	reused.CandidateSession = previous.ActiveSession
	if err := reused.ValidateTransition(previous); err == nil {
		t.Fatal("fresh development candidate reused the prior active session")
	}
}

func TestFixRound5SeparatesDevelopmentAndRebootNoOwnerCheckpoints(t *testing.T) {
	load := recoveringIntentRecord()
	load.Phase = PhaseLoadAttempted
	developmentCheckpoint := noOwnerRecord()
	if err := developmentCheckpoint.ValidateTransition(load); err != nil {
		t.Fatalf("development no-owner checkpoint rejected: %v", err)
	}
	if err := fpgadevActiveRecord().ValidateTransition(developmentCheckpoint); err != nil {
		t.Fatalf("development no-owner checkpoint could not transfer to fpgadev: %v", err)
	}
	developmentMain := normalMainStartingRecord()
	if err := developmentMain.ValidateTransition(developmentCheckpoint); err == nil {
		t.Fatal("development no-owner checkpoint started Main")
	}

	recovery := recoveryRequiredAfter(fpgadevActiveRecord())
	sameBootDevelopmentCheckpoint := noOwnerRecord()
	if err := sameBootDevelopmentCheckpoint.ValidateTransition(recovery); err == nil {
		t.Fatal("same-boot recovery entered the development no-owner checkpoint")
	}
	if err := fpgadevActiveRecord().ValidateTransition(sameBootDevelopmentCheckpoint); err != nil {
		t.Fatalf("same-boot composition control cannot transfer development checkpoint: %v", err)
	}

	rebootCheckpoint := rebootRecoveryNoOwnerRecord(recovery)
	if err := rebootCheckpoint.ValidateTransition(recovery); err != nil {
		t.Fatalf("reboot-recovery no-owner checkpoint rejected: %v", err)
	}
	fabricatedDevelopment := fpgadevActiveRecord()
	fabricatedDevelopment.BootID = rebootCheckpoint.BootID
	fabricatedDevelopment.ActiveSession = validSessionNext
	fabricatedDevelopment.ActiveGeneration = rebootCheckpoint.GenerationHighWater + 1
	fabricatedDevelopment.GenerationHighWater = rebootCheckpoint.GenerationHighWater + 1
	if err := fabricatedDevelopment.Validate(); err != nil {
		t.Fatalf("invalid fabricated-development fixture: %v", err)
	}
	if err := fabricatedDevelopment.ValidateTransition(rebootCheckpoint); err == nil {
		t.Fatal("reboot-recovery no-owner checkpoint transferred to fpgadev")
	}
}

func rebootRecoveryNoOwnerRecord(previous Record) Record {
	return Record{
		Schema:              SchemaVersion,
		State:               StateNoOwner,
		Phase:               PhaseMainAbsent,
		BootID:              validBootIDNext,
		RunID:               "",
		GenerationHighWater: previous.GenerationHighWater,
		ActiveSession:       "",
		ActiveGeneration:    0,
		ActiveMode:          ModeNone,
		CandidateSession:    "",
		CandidateGeneration: 0,
		CandidateMode:       ModeNone,
		QuiescingOwner:      OwnerNone,
		CandidateOwner:      OwnerNone,
		ActiveOwner:         OwnerNone,
		ActiveLeases:        []string{},
		RequestedResources:  []string{},
		FirstFailure:        "",
	}
}

func TestFixRound5ReconcilesNormalOwnerFailureWithoutDevelopmentIdentity(t *testing.T) {
	normal := normalMainRecord()
	recovery := recoveryRequiredAfter(normal)
	if recovery.RunID != "" || recovery.CandidateSession != "" || recovery.CandidateGeneration != 0 || recovery.CandidateMode != ModeNone || recovery.CandidateOwner != OwnerNone || len(recovery.RequestedResources) != 0 {
		t.Fatalf("normal-owner failure fabricated development identity: %#v", recovery)
	}
	if err := recovery.ValidateTransition(normal); err != nil {
		t.Fatalf("normal-owner failure fence rejected: %v", err)
	}

	checkpoint := rebootRecoveryNoOwnerRecord(recovery)
	if checkpoint.RunID != "" || checkpoint.CandidateSession != "" || checkpoint.CandidateGeneration != 0 || checkpoint.CandidateMode != ModeNone || checkpoint.CandidateOwner != OwnerNone || len(checkpoint.RequestedResources) != 0 {
		t.Fatalf("reboot checkpoint fabricated development identity: %#v", checkpoint)
	}
	if err := checkpoint.ValidateTransition(recovery); err != nil {
		t.Fatalf("changed-boot recovery checkpoint rejected: %v", err)
	}

	starting := normalMainStartingRecord()
	starting.BootID = checkpoint.BootID
	starting.GenerationHighWater = checkpoint.GenerationHighWater + 1
	starting.ActiveGeneration = starting.GenerationHighWater
	if starting.ActiveSession == normal.ActiveSession {
		t.Fatal("normal restart fixture reused the failed owner session")
	}
	if err := starting.ValidateTransition(checkpoint); err != nil {
		t.Fatalf("fresh normal owner transfer rejected: %v", err)
	}

	finished := cloneRecord(starting)
	finished.State = StateNormalMain
	if err := finished.ValidateTransition(starting); err != nil {
		t.Fatalf("normal owner readiness commit rejected: %v", err)
	}
}
