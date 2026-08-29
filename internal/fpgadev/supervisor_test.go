//go:build fpgadev

package fpgadev

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
	"golang.org/x/sys/unix"
)

func validReadyRecordV3Task1() ReadyRecordV3 {
	return ReadyRecordV3{
		Schema:              3,
		BootID:              testBootID,
		JournalSHA256:       strings.Repeat("a", 64),
		OwnerSession:        strings.Repeat("1", 32),
		OwnerGeneration:     2,
		ProfileSHA256:       strings.Repeat("b", 64),
		Capabilities:        DevelopmentCapabilities(),
		SupervisorPID:       11,
		SupervisorStartTime: 12,
		MainPID:             21,
		MainStartTime:       22,
		AgentPID:            31,
		AgentStartTime:      32,
	}
}

func TestReadyRecordV3CanonicalRoundTripAndFieldOrder(t *testing.T) {
	record := validReadyRecordV3Task1()
	raw, err := record.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":3,"boot_id":"00000000-0000-0000-0000-000000000001","journal_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","owner_session":"11111111111111111111111111111111","owner_generation":2,"profile_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","capabilities":["cast_unavailable","input_unavailable","controller_routes_unavailable","presentation_unavailable","audio_unavailable"],"supervisor_pid":11,"supervisor_start_time":12,"main_pid":21,"main_start_time":22,"agent_pid":31,"agent_start_time":32}` + "\n"
	if string(raw) != want {
		t.Fatalf("canonical ready record = %q, want %q", raw, want)
	}
	parsed, err := ParseReadyRecordV3(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, record) {
		t.Fatalf("parsed ready record = %#v, want %#v", parsed, record)
	}
	if len(raw) > 4096 || raw[len(raw)-1] != '\n' {
		t.Fatalf("ready record length/newline = %d/%q", len(raw), raw[len(raw)-1])
	}
}

func TestReadyRecordV3RejectsSupersededMalformedAndNonCanonicalInputs(t *testing.T) {
	record := validReadyRecordV3Task1()
	canonical, err := record.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "schema zero", raw: bytes.Replace(canonical, []byte(`"schema":3`), []byte(`"schema":0`), 1)},
		{name: "schema one", raw: bytes.Replace(canonical, []byte(`"schema":3`), []byte(`"schema":1`), 1)},
		{name: "schema two", raw: bytes.Replace(canonical, []byte(`"schema":3`), []byte(`"schema":2`), 1)},
		{name: "schema four", raw: bytes.Replace(canonical, []byte(`"schema":3`), []byte(`"schema":4`), 1)},
		{name: "reordered", raw: []byte(`{"boot_id":"00000000-0000-0000-0000-000000000001","schema":3,"journal_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","owner_session":"11111111111111111111111111111111","owner_generation":2,"profile_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","capabilities":["cast_unavailable","input_unavailable","controller_routes_unavailable","presentation_unavailable","audio_unavailable"],"supervisor_pid":11,"supervisor_start_time":12,"main_pid":21,"main_start_time":22,"agent_pid":31,"agent_start_time":32}` + "\n")},
		{name: "duplicate", raw: bytes.Replace(canonical, []byte(`"schema":3,`), []byte(`"schema":3,"schema":3,`), 1)},
		{name: "unknown", raw: bytes.Replace(canonical, []byte(`"schema":3,`), []byte(`"schema":3,"extra":1,`), 1)},
		{name: "missing", raw: bytes.Replace(canonical, []byte(`,"agent_start_time":32`), nil, 1)},
		{name: "float", raw: bytes.Replace(canonical, []byte(`"schema":3`), []byte(`"schema":3.0`), 1)},
		{name: "string number", raw: bytes.Replace(canonical, []byte(`"schema":3`), []byte(`"schema":"3"`), 1)},
		{name: "negative", raw: bytes.Replace(canonical, []byte(`"owner_generation":2`), []byte(`"owner_generation":-2`), 1)},
		{name: "zero PID", raw: bytes.Replace(canonical, []byte(`"agent_pid":31`), []byte(`"agent_pid":0`), 1)},
		{name: "bad boot", raw: bytes.Replace(canonical, []byte(`00000000-0000-0000-0000-000000000001`), []byte(`not-a-boot`), 1)},
		{name: "bad journal hash", raw: bytes.Replace(canonical, []byte(`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`), []byte(`AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA`), 1)},
		{name: "bad profile hash", raw: bytes.Replace(canonical, []byte(`bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`), []byte(`short`), 1)},
		{name: "bad session", raw: bytes.Replace(canonical, []byte(`11111111111111111111111111111111`), []byte(`not-a-session`), 1)},
		{name: "bad capabilities", raw: bytes.Replace(canonical, []byte(`audio_unavailable"]`), []byte(`wrong"]`), 1)},
		{name: "trailing object", raw: append(append([]byte(nil), canonical...), []byte(`{}`)...)},
		{name: "missing newline", raw: bytes.TrimSuffix(canonical, []byte("\n"))},
		{name: "oversized", raw: bytes.Repeat([]byte("x"), 4097)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseReadyRecordV3(test.raw); err == nil {
				t.Fatal("malformed ready record was accepted")
			}
		})
	}
}

func TestReadyRecordV3StoreUsesApprovedPathAndAtomicDurableReplacement(t *testing.T) {
	if got := NewProductionReadyStoreV3().Path; got != ReadyRecordV3Path {
		t.Fatalf("production ready path = %q, want %q", got, ReadyRecordV3Path)
	}
	if err := os.MkdirAll("/dev/shm/fogcast-task1", 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("/dev/shm/fogcast-task1", "ready-store-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewReadyStoreV3(filepath.Join(root, "ready-v3.json"), uint32(os.Getuid()))
	var fileSyncs, parentSyncs int
	store.syncFile = func(*os.File) error { fileSyncs++; return nil }
	store.syncParent = func(*os.File) error { parentSyncs++; return nil }
	first := validReadyRecordV3Task1()
	if err := store.Replace(first); err != nil {
		t.Fatal(err)
	}
	firstInfo, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.OwnerGeneration++
	if err := store.Replace(second); err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(firstInfo, secondInfo) {
		t.Fatal("ready replacement reused the previous inode")
	}
	if fileSyncs != 2 || parentSyncs != 2 {
		t.Fatalf("sync calls = file %d parent %d, want two each", fileSyncs, parentSyncs)
	}
	got, exists, err := store.Load()
	if err != nil || !exists || !reflect.DeepEqual(got, second) {
		t.Fatalf("loaded ready = (%#v,%v,%v), want (%#v,true,nil)", got, exists, err, second)
	}
	raw, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := second.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("stored ready bytes = %q, want canonical %q", raw, want)
	}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ready path after remove = %v, want not-exist", err)
	}
}

// The following fixtures exercise the fail-stop lifecycle without opening
// target devices.  They deliberately use the same typed child boundary as
// the production runtime, so the ordering tests cannot accidentally pass by
// reducing a child to a PID or by adopting a process discovered on /proc.
type failStopEvents struct {
	mu     sync.Mutex
	values []string
}

func (e *failStopEvents) add(value string) {
	e.mu.Lock()
	e.values = append(e.values, value)
	e.mu.Unlock()
}

func (e *failStopEvents) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.values...)
}

type failStopLocker struct {
	name   string
	events *failStopEvents
}

type failStopScriptedLocker struct {
	name       string
	events     *failStopEvents
	mu         sync.Mutex
	locks      int
	lockErrors []error
	unlockErr  error
}

func (l *failStopScriptedLocker) Lock(context.Context) (hardwareowner.Unlock, error) {
	l.mu.Lock()
	index := l.locks
	l.locks++
	var lockErr error
	if index < len(l.lockErrors) {
		lockErr = l.lockErrors[index]
	}
	l.mu.Unlock()
	l.events.add(l.name + "-lock")
	if lockErr != nil {
		return nil, lockErr
	}
	var once sync.Once
	return func() error {
		once.Do(func() { l.events.add(l.name + "-unlock") })
		return l.unlockErr
	}, nil
}

func (l failStopLocker) Lock(context.Context) (hardwareowner.Unlock, error) {
	l.events.add(l.name + "-lock")
	var once sync.Once
	return func() error {
		once.Do(func() { l.events.add(l.name + "-unlock") })
		return nil
	}, nil
}

type failStopOwnerStore struct {
	mu       sync.Mutex
	record   hardwareowner.Record
	absent   bool
	events   *failStopEvents
	replace  error
	replaces []hardwareowner.Record
}

func (s *failStopOwnerStore) Load() (hardwareowner.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.absent {
		return hardwareowner.Record{}, false, nil
	}
	return s.record, true, nil
}

// productionOwnerStoreObserver retains the real hardwareowner.Store semantics
// while exposing the durable transition order needed by the production
// successor-boot composition test.  The supervisor must commit no_owner
// before allocating normal_main_starting; observing only the final record
// would not prove that checkpoint survived the real atomic store boundary.
type productionOwnerStoreObserver struct {
	store   *hardwareowner.Store
	events  *failStopEvents
	mu      sync.Mutex
	records []hardwareowner.Record
}

func (s *productionOwnerStoreObserver) Load() (hardwareowner.Record, bool, error) {
	return s.store.Load()
}

func (s *productionOwnerStoreObserver) Replace(next hardwareowner.Record) error {
	if err := s.store.Replace(next); err != nil {
		return err
	}
	s.mu.Lock()
	s.records = append(s.records, next)
	s.mu.Unlock()
	if s.events != nil {
		s.events.add("owner-" + string(next.State))
	}
	return nil
}

func (s *productionOwnerStoreObserver) snapshot() []hardwareowner.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]hardwareowner.Record(nil), s.records...)
}

func (s *failStopOwnerStore) Replace(next hardwareowner.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replace != nil {
		return s.replace
	}
	if s.absent {
		if err := next.Validate(); err != nil {
			return err
		}
		s.absent = false
	} else if err := next.ValidateTransition(s.record); err != nil {
		return err
	}
	s.record = next
	s.replaces = append(s.replaces, next)
	if s.events != nil {
		s.events.add("owner-" + string(next.State))
	}
	return nil
}

type failStopJournal struct{ record InstallJournalRecord }

func (j failStopJournal) Load() (InstallJournalRecord, bool, error) { return j.record, true, nil }

type failStopReadyStore struct {
	mu         sync.Mutex
	events     *failStopEvents
	record     ReadyRecordV3
	exists     bool
	removeErr  error
	replaceErr error
}

func (s *failStopReadyStore) Load() (ReadyRecordV3, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record, s.exists, nil
}

func (s *failStopReadyStore) Replace(record ReadyRecordV3) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replaceErr != nil {
		return s.replaceErr
	}
	s.record, s.exists = record, true
	if s.events != nil {
		s.events.add("ready-publish")
	}
	return nil
}

func (s *failStopReadyStore) Remove() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.removeErr != nil {
		return s.removeErr
	}
	s.exists = false
	if s.events != nil {
		s.events.add("ready-remove")
	}
	return nil
}

type failStopReboot struct {
	events *failStopEvents
	err    error
}

func (r failStopReboot) Request(context.Context) error {
	r.events.add("reboot")
	return r.err
}

type failStopChild struct {
	name     string
	events   *failStopEvents
	wait     <-chan error
	identity ProcessAttestation
}

func (c *failStopChild) TerminateAndReap(context.Context) error {
	c.events.add(c.name + "-terminate")
	return nil
}

func (c *failStopChild) Wait(ctx context.Context) error {
	select {
	case err := <-c.wait:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *failStopChild) Attestation() (ProcessAttestation, error) { return c.identity, nil }

func failStopTerminalJournal() InstallJournalRecord {
	source := SourceRecord{Path: "/etc/fogcast-start", Kind: "regular", Mode: 0o755, SHA256: strings.Repeat("a", 64), BackupPath: "/var/lib/fogcast/backup", BackupSHA256: strings.Repeat("b", 64), DisabledState: "approved_trampoline", DisabledSHA256: strings.Repeat("c", 64)}
	return InstallJournalRecord{
		Schema: 1, State: InstallStateTerminal, InstallBootID: testBootID,
		PackageSHA256: strings.Repeat("d", 64), PreviousConfigSHA256: strings.Repeat("e", 64),
		Inventory: InventoryV1{Schema: 1, MainExecutable: PathExpectation{Path: "/usr/bin/Main", Kind: "regular", SHA256: strings.Repeat("f", 64)}, MainFIFO: PathExpectation{Path: "/dev/MiSTer_cmd", Kind: "fifo"}, StartSources: []SourceRecord{source}},
		Sources:   []SourceRecord{source},
	}
}

func failStopRecoveryOwner() hardwareowner.Record {
	return hardwareowner.Record{
		Schema: 1, State: hardwareowner.StateRecoveryRequired, Phase: hardwareowner.PhaseIntentCommitted,
		BootID: testBootID, GenerationHighWater: 3, ActiveSession: strings.Repeat("1", 32), ActiveGeneration: 3,
		ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain, ActiveLeases: hardwareowner.NormalLeases(),
		CandidateSession: strings.Repeat("2", 32), CandidateGeneration: 3, CandidateMode: hardwareowner.ModeUpdating,
		CandidateOwner: hardwareowner.OwnerFPGADev, QuiescingOwner: hardwareowner.OwnerCompatMain,
		RequestedResources: hardwareowner.DevelopmentLeases(), RunID: strings.Repeat("3", 32), FirstFailure: "load_dispatch_failed",
	}
}

func failStopDependencies(t *testing.T, events *failStopEvents, ready *failStopReadyStore, reboot failStopReboot, wait <-chan error) (Dependencies, *failStopOwnerStore) {
	t.Helper()
	bootID := "00000000-0000-0000-0000-000000000002"
	ownerStore := &failStopOwnerStore{record: failStopRecoveryOwner(), events: events}
	journal := failStopJournal{record: failStopTerminalJournal()}
	profile := strings.Repeat("a", 64)
	main := &failStopChild{name: "main", events: events, wait: wait, identity: ProcessAttestation{PID: 21, StartTime: 22, Device: 23, Inode: 24, SHA256: strings.Repeat("b", 64)}}
	agent := &failStopChild{name: "agent", events: events, wait: wait, identity: ProcessAttestation{PID: 31, StartTime: 32, Device: 33, Inode: 34, SHA256: strings.Repeat("c", 64)}}
	deps := Dependencies{
		Journal: journal, OwnerStore: ownerStore,
		InstallLocker: failStopLocker{name: "install", events: events}, OwnerLocker: failStopLocker{name: "owner", events: events},
		BootID: func() (string, error) { return bootID, nil }, Reset: func(context.Context) error { events.add("reset"); return nil },
		ReadyStore: ready, RebootRequester: reboot,
		StartMain:           func(context.Context) (retainedSupervisorChild, error) { events.add("start-main"); return main, nil },
		MainExecutableReady: func(context.Context) error { events.add("main-process-ready"); return nil },
		CommandFIFOReady:    func(context.Context) error { events.add("fifo-ready"); return nil },
		FPGAManagerReady:    func(context.Context) error { events.add("manager-ready"); return nil },
		MenuReady:           func(context.Context) error { events.add("menu-ready"); return nil },
		StartAgent:          func(context.Context) (retainedSupervisorChild, error) { events.add("start-agent"); return agent, nil },
		ReadinessReceipt: func(context.Context) (ReadinessReceipt, error) {
			events.add("receipt")
			return ReadinessReceipt{Schema: 1, PID: 31, StartTime: 32, ExecutableDevice: 33, ExecutableInode: 34, ExecutableSHA256: strings.Repeat("c", 64), ProfileSHA256: profile, Capabilities: DevelopmentCapabilities()}, nil
		},
		ProfileSHA256: profile, SupervisorIdentity: ProcessAttestation{PID: 41, StartTime: 42, Device: 43, Inode: 44, SHA256: strings.Repeat("d", 64)},
		NewSession: func() (string, error) { return strings.Repeat("4", 32), nil },
		WaitLiveness: func(ctx context.Context) error {
			events.add("wait-children")
			return (&failStopChild{name: "wait", events: events, wait: wait}).Wait(ctx)
		},
	}
	return deps, ownerStore
}

func TestFailStopSupervisorPublishesOnlyAfterReadinessAndUnlocksInOrder(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	wait := make(chan error, 1)
	deps, ownerStore := failStopDependencies(t, events, ready, failStopReboot{events: events}, wait)
	done := make(chan error, 1)
	go func() { done <- NewSupervisor().Run(context.Background(), deps) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got := events.snapshot()
		if containsEvent(got, "ready-publish") {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !containsEvent(events.snapshot(), "ready-publish") {
		select {
		case runErr := <-done:
			t.Fatalf("supervisor did not publish ready record: events=%v err=%v", events.snapshot(), runErr)
		default:
			t.Fatalf("supervisor did not publish ready record: events=%v", events.snapshot())
		}
	}
	wait <- errors.New("child exited")
	err := <-done
	if err == nil || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("Run error=%v, want fail-stop fence", err)
	}
	got := events.snapshot()
	want := []string{"install-lock", "owner-lock", "ready-remove", "reset", "start-main", "main-process-ready", "fifo-ready", "manager-ready", "menu-ready", "owner-normal_main", "start-agent", "receipt", "ready-publish", "owner-unlock", "install-unlock", "wait-children", "ready-remove", "owner-recovery_required", "reboot"}
	assertOrderedEvents(t, got, want)
	owner, _, loadErr := ownerStore.Load()
	if loadErr != nil || owner.State != hardwareowner.StateRecoveryRequired {
		t.Fatalf("owner after child exit=%#v err=%v", owner, loadErr)
	}
}

func TestFailStopSupervisorSameBootReplacementRequestsRebootWithoutCleanupScan(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	deps, ownerStore := failStopDependencies(t, events, ready, failStopReboot{events: events}, make(chan error, 1))
	deps.BootID = func() (string, error) { return testBootID, nil }
	if err := NewSupervisor().Run(context.Background(), deps); err == nil || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("same-boot Run error=%v, want fenced", err)
	}
	got := events.snapshot()
	if containsEvent(got, "reset") || containsEvent(got, "start-main") || containsEvent(got, "start-agent") || containsEvent(got, "wait-children") || containsEvent(got, "terminate") {
		t.Fatalf("same-boot replacement touched lifecycle=%v", got)
	}
	if !containsEvent(got, "reboot") || !containsEvent(got, "ready-remove") {
		t.Fatalf("same-boot replacement events=%v, want ready removal and reboot", got)
	}
	owner, _, loadErr := ownerStore.Load()
	if loadErr != nil || owner.State != hardwareowner.StateRecoveryRequired {
		t.Fatalf("same-boot owner=%#v err=%v", owner, loadErr)
	}
}

func TestFailStopSupervisorSuccessorBootCreatesNoOwnerBeforeMainReadiness(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	wait := make(chan error, 1)
	deps, ownerStore := failStopDependencies(t, events, ready, failStopReboot{events: events}, wait)
	ownerStore.absent = true
	deps.AllowAbsentOwner = true
	// The terminal journal was durably installed on testBootID; this boot is
	// distinct and is therefore eligible for the absent-owner bootstrap.
	deps.BootID = func() (string, error) { return "00000000-0000-0000-0000-000000000003", nil }
	done := make(chan error, 1)
	go func() { done <- NewSupervisor().Run(context.Background(), deps) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !containsEvent(events.snapshot(), "ready-publish") {
		time.Sleep(time.Millisecond)
	}
	if !containsEvent(events.snapshot(), "ready-publish") {
		select {
		case runErr := <-done:
			t.Fatalf("successor supervisor did not reach ready publication: events=%v err=%v", events.snapshot(), runErr)
		default:
			t.Fatalf("successor supervisor did not reach ready publication: %v", events.snapshot())
		}
	}
	wait <- errors.New("child exited")
	if err := <-done; err == nil || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("successor Run error=%v, want fail-stop fence after lifecycle", err)
	}
	got := events.snapshot()
	assertOrderedEvents(t, got, []string{"reset", "owner-no_owner", "owner-normal_main_starting", "start-main", "main-process-ready", "fifo-ready", "manager-ready", "menu-ready", "owner-normal_main", "start-agent", "ready-publish"})
	owner, exists, err := ownerStore.Load()
	if err != nil || !exists || owner.State != hardwareowner.StateRecoveryRequired || owner.BootID != "00000000-0000-0000-0000-000000000003" {
		t.Fatalf("successor owner=%#v exists=%v err=%v", owner, exists, err)
	}
}

func TestFailStopSupervisorProductionStoreBootstrapsAbsentOwner(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	wait := make(chan error, 1)
	deps, _ := failStopDependencies(t, events, ready, failStopReboot{events: events}, wait)
	currentBoot := "00000000-0000-0000-0000-000000000003"
	deps.BootID = func() (string, error) { return currentBoot, nil }
	deps.AllowAbsentOwner = true
	productionOwner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	productionObserver := &productionOwnerStoreObserver{store: productionOwner, events: events}
	deps.OwnerStore = productionObserver
	deps.StartMain = func(context.Context) (retainedSupervisorChild, error) {
		record, exists, err := productionOwner.Load()
		history := productionObserver.snapshot()
		if err != nil || !exists || record.State != hardwareowner.StateNormalMainStarting || record.BootID != currentBoot {
			return nil, fmt.Errorf("Main started without normal_main_starting checkpoint: record=%#v exists=%v err=%v", record, exists, err)
		}
		if len(history) < 2 || history[0].State != hardwareowner.StateNoOwner || history[0].Phase != hardwareowner.PhaseMainAbsent || history[0].BootID != currentBoot || history[1].State != hardwareowner.StateNormalMainStarting {
			return nil, fmt.Errorf("Main started without prior durable no_owner checkpoint: history=%#v", history)
		}
		events.add("start-main")
		return &failStopChild{name: "main", events: events, wait: wait, identity: ProcessAttestation{PID: 21, StartTime: 22, Device: 23, Inode: 24, SHA256: strings.Repeat("b", 64)}}, nil
	}
	done := make(chan error, 1)
	go func() { done <- NewSupervisor().Run(context.Background(), deps) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !containsEvent(events.snapshot(), "ready-publish") {
		time.Sleep(time.Millisecond)
	}
	if !containsEvent(events.snapshot(), "ready-publish") {
		select {
		case runErr := <-done:
			t.Fatalf("production-store supervisor did not reach ready publication: events=%v err=%v", events.snapshot(), runErr)
		default:
			t.Fatalf("production-store supervisor did not reach ready publication: %v", events.snapshot())
		}
	}
	wait <- errors.New("child exited")
	if err := <-done; err == nil || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("production-store supervisor error=%v, want fail-stop fence", err)
	}
	if !containsEvent(events.snapshot(), "owner-no_owner") {
		t.Fatalf("production-store fixture events did not record no_owner checkpoint: %v", events.snapshot())
	}
	history := productionObserver.snapshot()
	if len(history) < 2 || history[0].State != hardwareowner.StateNoOwner || history[1].State != hardwareowner.StateNormalMainStarting {
		t.Fatalf("production-store owner transitions=%#v, want no_owner then normal_main_starting", history)
	}
}

func TestFailStopSupervisorAbsentOwnerSameBootFencesWithoutReset(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	deps, ownerStore := failStopDependencies(t, events, ready, failStopReboot{events: events}, make(chan error, 1))
	ownerStore.absent = true
	deps.AllowAbsentOwner = true
	deps.BootID = func() (string, error) { return testBootID, nil }
	if err := NewSupervisor().Run(context.Background(), deps); err == nil || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("same-boot absent-owner Run error=%v, want fenced", err)
	}
	got := events.snapshot()
	if containsEvent(got, "reset") || containsEvent(got, "start-main") || containsEvent(got, "start-agent") || containsEvent(got, "ready-publish") {
		t.Fatalf("same-boot absent owner touched lifecycle=%v", got)
	}
	if !containsEvent(got, "reboot") || !containsEvent(got, "ready-remove") {
		t.Fatalf("same-boot absent owner events=%v, want ready removal and reboot", got)
	}
	if _, exists, err := ownerStore.Load(); err != nil || exists {
		t.Fatalf("same-boot absent owner was synthesized: exists=%v err=%v", exists, err)
	}
}

func TestFailStopSupervisorPostResetFailuresFenceAndRequestReboot(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Dependencies)
		forbid []string
	}{
		{name: "reset", mutate: func(deps *Dependencies) {
			deps.Reset = func(context.Context) error { return errors.New("reset failed") }
		}, forbid: []string{"start-main", "start-agent"}},
		{name: "main start", mutate: func(deps *Dependencies) {
			deps.StartMain = func(context.Context) (retainedSupervisorChild, error) { return nil, errors.New("main start failed") }
		}, forbid: []string{"start-agent"}},
		{name: "main process readiness", mutate: func(deps *Dependencies) {
			deps.MainExecutableReady = func(context.Context) error { return errors.New("main process not ready") }
		}, forbid: []string{"start-agent"}},
		{name: "fifo readiness", mutate: func(deps *Dependencies) {
			deps.CommandFIFOReady = func(context.Context) error { return errors.New("fifo not ready") }
		}, forbid: []string{"start-agent"}},
		{name: "manager readiness", mutate: func(deps *Dependencies) {
			deps.FPGAManagerReady = func(context.Context) error { return errors.New("manager not ready") }
		}, forbid: []string{"start-agent"}},
		{name: "core name readiness", mutate: func(deps *Dependencies) {
			deps.MenuReady = func(context.Context) error { return errors.New("CORENAME not stable") }
		}, forbid: []string{"start-agent"}},
		{name: "agent start", mutate: func(deps *Dependencies) {
			deps.StartAgent = func(context.Context) (retainedSupervisorChild, error) { return nil, errors.New("agent start failed") }
		}, forbid: nil},
		{name: "receipt", mutate: func(deps *Dependencies) {
			deps.ReadinessReceipt = func(context.Context) (ReadinessReceipt, error) {
				return ReadinessReceipt{}, errors.New("receipt failed")
			}
		}, forbid: nil},
		{name: "ready publication", mutate: func(deps *Dependencies) {
			deps.ReadyStore.(*failStopReadyStore).replaceErr = errors.New("ready fsync failed")
		}, forbid: nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			events := &failStopEvents{}
			ready := &failStopReadyStore{events: events}
			deps, ownerStore := failStopDependencies(t, events, ready, failStopReboot{events: events}, make(chan error, 1))
			testCase.mutate(&deps)
			err := NewSupervisor().Run(context.Background(), deps)
			if err == nil || !errors.Is(err, errSupervisorFenced) {
				t.Fatalf("Run error=%v, want fail-stop fence", err)
			}
			got := events.snapshot()
			if !containsEvent(got, "ready-remove") || !containsEvent(got, "reboot") {
				t.Fatalf("post-reset failure events=%v, want ready removal and reboot", got)
			}
			for _, forbidden := range testCase.forbid {
				if containsEvent(got, forbidden) {
					t.Fatalf("post-reset %s failure started forbidden phase %q: %v", testCase.name, forbidden, got)
				}
			}
			owner, _, loadErr := ownerStore.Load()
			if loadErr != nil || owner.State != hardwareowner.StateRecoveryRequired {
				t.Fatalf("post-reset owner=%#v err=%v", owner, loadErr)
			}
		})
	}
}

func TestFailStopSupervisorRebootFailureIsReportedAfterFence(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	deps, ownerStore := failStopDependencies(t, events, ready, failStopReboot{events: events, err: errors.New("reboot unavailable")}, make(chan error, 1))
	deps.StartMain = func(context.Context) (retainedSupervisorChild, error) { return nil, errors.New("Main unavailable") }
	err := NewSupervisor().Run(context.Background(), deps)
	if err == nil || !errors.Is(err, ErrSupervisorRebootFailed) || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("Run error=%v, want fence plus reboot failure", err)
	}
	owner, _, loadErr := ownerStore.Load()
	if loadErr != nil || owner.State != hardwareowner.StateRecoveryRequired {
		t.Fatalf("owner after reboot failure=%#v err=%v", owner, loadErr)
	}
}

func TestFailStopSupervisorRequiresRetainedMainAndAgentHandles(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*Dependencies)
		forbidden string
	}{
		{name: "Main", mutate: func(deps *Dependencies) {
			deps.StartMain = func(context.Context) error { return nil }
		}, forbidden: "start-agent"},
		{name: "agent", mutate: func(deps *Dependencies) {
			deps.StartAgent = func(context.Context) error { return nil }
		}, forbidden: "receipt"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			events := &failStopEvents{}
			ready := &failStopReadyStore{events: events}
			deps, ownerStore := failStopDependencies(t, events, ready, failStopReboot{events: events}, make(chan error, 1))
			testCase.mutate(&deps)
			err := NewSupervisor().Run(context.Background(), deps)
			if err == nil {
				t.Fatal("Run unexpectedly accepted callback-only starter")
			}
			for _, forbidden := range []string{"reset", "start-main", "start-agent", "receipt", "reboot"} {
				if containsEvent(events.snapshot(), forbidden) {
					t.Fatalf("%s-less lifecycle reached %q: %v", testCase.name, forbidden, events.snapshot())
				}
			}
			owner, _, loadErr := ownerStore.Load()
			if loadErr != nil || owner.State != hardwareowner.StateRecoveryRequired {
				t.Fatalf("owner after retained-child validation=%#v err=%v", owner, loadErr)
			}
		})
	}
}

func TestFailStopSupervisorAgentPhasesShareOneDeadline(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	deps, _ := failStopDependencies(t, events, ready, failStopReboot{events: events}, make(chan error, 1))
	var firstDeadline time.Time
	checkDeadline := func(ctx context.Context, phase string) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return fmt.Errorf("%s context has no deadline", phase)
		}
		if firstDeadline.IsZero() {
			firstDeadline = deadline
		} else if deadline != firstDeadline {
			return fmt.Errorf("%s received a different deadline", phase)
		}
		return nil
	}
	deps.StartAgent = func(ctx context.Context) (retainedSupervisorChild, error) {
		if err := checkDeadline(ctx, "start-agent"); err != nil {
			return nil, err
		}
		return &failStopChild{name: "agent", events: events, wait: make(chan error, 1), identity: ProcessAttestation{PID: 31, StartTime: 32, Device: 33, Inode: 34, SHA256: strings.Repeat("c", 64)}}, nil
	}
	deps.ReadinessReceipt = func(ctx context.Context) (ReadinessReceipt, error) {
		if err := checkDeadline(ctx, "receipt"); err != nil {
			return ReadinessReceipt{}, err
		}
		return ReadinessReceipt{Schema: 1, PID: 31, StartTime: 32, ExecutableDevice: 33, ExecutableInode: 34, ExecutableSHA256: strings.Repeat("c", 64), ProfileSHA256: deps.ProfileSHA256, Capabilities: DevelopmentCapabilities()}, nil
	}
	deps.ReadyStore = &failStopReadyStore{events: events, replaceErr: errors.New("stop after publication context check")}
	if err := NewSupervisor().Run(context.Background(), deps); err == nil || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("Run error=%v, want publication fence", err)
	}
	if firstDeadline.IsZero() {
		t.Fatal("agent deadline was not observed")
	}
}

func TestFailStopSupervisorReacquireFailureDoesNotTouchReadyOrOwner(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	wait := make(chan error, 1)
	deps, ownerStore := failStopDependencies(t, events, ready, failStopReboot{events: events}, wait)
	ownerLocker := &failStopScriptedLocker{name: "owner", events: events, lockErrors: []error{nil, errors.New("owner contention")}}
	deps.OwnerLocker = ownerLocker
	installLocker := &failStopScriptedLocker{name: "install", events: events}
	deps.InstallLocker = installLocker
	done := make(chan error, 1)
	go func() { done <- NewSupervisor().Run(context.Background(), deps) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !containsEvent(events.snapshot(), "ready-publish") {
		time.Sleep(time.Millisecond)
	}
	if !containsEvent(events.snapshot(), "ready-publish") {
		t.Fatal("supervisor did not publish ready record")
	}
	ownerStore.mu.Lock()
	replacesBefore := len(ownerStore.replaces)
	ownerStore.mu.Unlock()
	wait <- errors.New("child exited")
	err := <-done
	if err == nil || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("Run error=%v, want fail-stop fence", err)
	}
	got := events.snapshot()
	removeCount, replaceCount := 0, 0
	for _, event := range got {
		if event == "ready-remove" {
			removeCount++
		}
		if event == "owner-recovery_required" {
			replaceCount++
		}
	}
	if removeCount != 1 || replaceCount != 0 {
		t.Fatalf("unsafe reacquire cleanup events=%v, ready removes=%d recovery writes=%d", got, removeCount, replaceCount)
	}
	ownerStore.mu.Lock()
	replacesAfter := len(ownerStore.replaces)
	ownerStore.mu.Unlock()
	if replacesAfter != replacesBefore {
		t.Fatalf("owner changed after unproven reacquire: before=%d after=%d", replacesBefore, replacesAfter)
	}
	if !containsEvent(got, "main-terminate") || !containsEvent(got, "agent-terminate") || !containsEvent(got, "reboot") {
		t.Fatalf("reacquire failure did not terminate retained children/reboot: %v", got)
	}
}

func TestFailStopSupervisorUnlockAmbiguityDoesNotTouchReadyOrOwner(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	wait := make(chan error, 1)
	deps, _ := failStopDependencies(t, events, ready, failStopReboot{events: events}, wait)
	deps.InstallLocker = &failStopScriptedLocker{name: "install", events: events}
	deps.OwnerLocker = &failStopScriptedLocker{name: "owner", events: events, unlockErr: errors.New("owner unlock ambiguous")}
	done := make(chan error, 1)
	go func() { done <- NewSupervisor().Run(context.Background(), deps) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !containsEvent(events.snapshot(), "ready-publish") {
		time.Sleep(time.Millisecond)
	}
	if !containsEvent(events.snapshot(), "ready-publish") {
		t.Fatal("supervisor did not publish ready record")
	}
	wait <- errors.New("child exited")
	err := <-done
	if err == nil || !errors.Is(err, errSupervisorFenced) {
		t.Fatalf("Run error=%v, want fail-stop fence", err)
	}
	got := events.snapshot()
	removeCount, replaceCount := 0, 0
	for _, event := range got {
		if event == "ready-remove" {
			removeCount++
		}
		if event == "owner-recovery_required" {
			replaceCount++
		}
	}
	if removeCount != 1 || replaceCount != 0 {
		t.Fatalf("unsafe unlock cleanup events=%v, ready removes=%d recovery writes=%d", got, removeCount, replaceCount)
	}
	if !containsEvent(got, "main-terminate") || !containsEvent(got, "agent-terminate") || !containsEvent(got, "reboot") {
		t.Fatalf("unlock ambiguity did not terminate retained children/reboot: %v", got)
	}
}

func TestFailStopSupervisorRejectsInvalidStarterBeforeReset(t *testing.T) {
	events := &failStopEvents{}
	ready := &failStopReadyStore{events: events}
	deps, _ := failStopDependencies(t, events, ready, failStopReboot{events: events}, make(chan error, 1))
	deps.StartMain = func(context.Context) error { events.add("invalid-start"); return nil }
	deps.Reset = func(context.Context) error { events.add("reset"); return nil }
	if err := NewSupervisor().Run(context.Background(), deps); err == nil {
		t.Fatal("invalid starter unexpectedly accepted")
	}
	got := events.snapshot()
	for _, forbidden := range []string{"reset", "invalid-start", "start-agent", "reboot"} {
		if containsEvent(got, forbidden) {
			t.Fatalf("invalid starter reached %q: %v", forbidden, got)
		}
	}
}

func containsEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}

func assertOrderedEvents(t *testing.T, events, want []string) {
	t.Helper()
	position := 0
	for _, expected := range want {
		for position < len(events) && events[position] != expected {
			position++
		}
		if position == len(events) {
			t.Fatalf("events=%v missing ordered event %q", events, expected)
		}
		position++
	}
}

func TestSupervisorReceiptRejectsNonCanonicalCapabilities(t *testing.T) {
	receipt := ReadinessReceipt{Schema: 1, PID: 1, StartTime: 1, ExecutableDevice: 1, ExecutableInode: 1, ExecutableSHA256: strings.Repeat("a", 64), ProfileSHA256: strings.Repeat("b", 64), Capabilities: []string{"input_unavailable"}}
	if err := receipt.Validate(); err == nil {
		t.Fatal("partial capability receipt accepted")
	}
}

func TestResolveInventoryAtUsesProtectedMetadataAndBoundedContext(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resolved, err := ResolveInventoryAt(ctx, fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		RequireProcessEvidence: false,
		ScanProcessDescriptors: false,
		RequireNetworkEvidence: false,
		RequireMainFIFOOwner:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MainExecutable.Device == 0 || resolved.MainExecutable.Inode == 0 {
		t.Fatalf("main executable lacks descriptor identity: %#v", resolved.MainExecutable)
	}
	if resolved.MainFIFO.Device == 0 || resolved.MainFIFO.Inode == 0 {
		t.Fatalf("main FIFO lacks descriptor identity: %#v", resolved.MainFIFO)
	}
}

func TestResolveInventoryAtHashesPlayKitMainAndRejectsOversizedRegular(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	mainBytes := bytes.Repeat([]byte{0xa5}, 1059560)
	if err := os.WriteFile(fixture.inventory.MainExecutable.Path, mainBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	mainHash := sha256.Sum256(mainBytes)
	fixture.inventory.MainExecutable.SHA256 = hex.EncodeToString(mainHash[:])
	options := InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		RequireProcessEvidence: false,
		ScanProcessDescriptors: false,
		RequireNetworkEvidence: false,
		RequireMainFIFOOwner:   false,
	}
	if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, options); err != nil {
		t.Fatalf("resolve %d-byte Main: %v", len(mainBytes), err)
	}

	if err := os.WriteFile(fixture.inventory.MainExecutable.Path, bytes.Repeat([]byte{0x5a}, ProtectedRegularMaxBytes+1), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, options); err == nil {
		t.Fatal("oversized inventory regular file accepted")
	}
}

func TestResolveInventoryAtRejectsExpectedAbsentAppearanceAfterResolution(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	first, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		RequireProcessEvidence: false,
		RequireMainFIFOOwner:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fixture.inventory.CastTokenFile == nil {
		t.Fatal("fixture did not include an optional expected-absent route")
	}
	if err := os.WriteFile(fixture.inventory.CastTokenFile.Path, []byte("late route"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		Prior:                  &first,
		RequireProcessEvidence: false,
		RequireMainFIFOOwner:   false,
	})
	if err == nil {
		t.Fatal("expected-absent route appearance was accepted")
	}
}

func TestResolveInventoryAtRejectsMainFIFOHolderAfterStableMainAbsence(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	proc := filepath.Join(fixture.root, "proc", "41", "fd")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.inventory.MainFIFO.Path, filepath.Join(proc, "3")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhaseMainAbsent,
		RequireProcessEvidence: false,
		ScanProcessDescriptors: true,
		RequireMainFIFOOwner:   false,
	})
	if err == nil {
		t.Fatal("FIFO holder survived stable Main absence")
	}
}

func TestResolveInventoryAtEnforcesMainFIFOModeAndOwner(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	if err := os.Chmod(fixture.inventory.MainFIFO.Path, 0o620); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		RequireMainFIFOOwner:   true,
		RequireProcessEvidence: false,
	})
	if err == nil {
		t.Fatal("insecure Main FIFO mode was accepted")
	}
}

func TestResolveInventoryAtRejectsDeletedExpectedAbsentDescriptor(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	proc := filepath.Join(fixture.root, "proc", "42", "fd")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.inventory.CastTokenFile.Path+" (deleted)", filepath.Join(proc, "4")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		ScanProcessDescriptors: true,
		RequireMainFIFOOwner:   false,
	})
	if err == nil {
		t.Fatal("deleted descriptor for expected-absent route was accepted")
	}
}

func TestResolveInventoryAtAllowsExactMainFIFOHolderBeforeDispatch(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	proc := filepath.Join(fixture.root, "proc", "41", "fd")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.inventory.MainFIFO.Path, filepath.Join(proc, "3")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		MainProcess:            &ProcessIdentity{PID: 41, StartTime: 1, Device: 1, Inode: 1},
		ScanProcessDescriptors: true,
		RequireMainFIFOOwner:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveInventoryAtAppliesStrictestRuleToAliasedMainFIFO(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	fixture.inventory.CastNativeCommand = &PathExpectation{Path: fixture.inventory.MainFIFO.Path, Kind: "fifo"}
	proc := filepath.Join(fixture.root, "proc", "41", "fd")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.inventory.MainFIFO.Path, filepath.Join(proc, "3")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		MainProcess:            &ProcessIdentity{PID: 41, StartTime: 1, Device: 1, Inode: 1},
		ScanProcessDescriptors: true,
		RequireMainFIFOOwner:   false,
	})
	if err == nil {
		t.Fatal("aliased Main FIFO holder bypassed strictest route rule")
	}
}

func TestResolveInventoryAtAppliesStrictestRuleToHardLinkedMainFIFO(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	aliasPath := filepath.Join(fixture.root, "native-command")
	if err := os.Link(fixture.inventory.MainFIFO.Path, aliasPath); err != nil {
		t.Fatal(err)
	}
	fixture.inventory.CastNativeCommand = &PathExpectation{Path: aliasPath, Kind: "fifo"}
	proc := filepath.Join(fixture.root, "proc", "41", "fd")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.inventory.MainFIFO.Path, filepath.Join(proc, "3")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               filepath.Join(fixture.root, "proc"),
		Phase:                  InventoryPhasePreDispatch,
		MainProcess:            &ProcessIdentity{PID: 41, StartTime: 1, Device: 1, Inode: 1},
		ScanProcessDescriptors: true,
		RequireMainFIFOOwner:   false,
	})
	if err == nil {
		t.Fatal("hard-linked Main FIFO holder bypassed strictest route rule")
	}
}

func TestResolveInventoryAtRejectsSameBootInodeChurnButAcceptsNewBoot(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	first, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:             filepath.Join(fixture.root, "proc"),
		Phase:                InventoryPhasePreDispatch,
		RequireMainFIFOOwner: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the first FIFO inode pinned while the pathname is unlinked. Linux
	// may otherwise recycle an inode immediately after the final reference is
	// closed, turning this hostile same-boot recreation into an identity-
	// preserving fixture and making the continuity assertion nondeterministic.
	hold, heldInfo, err := openProtectedMetadata(fixture.inventory.MainFIFO.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	originalDevice, originalDeviceOK := journalStatField(heldInfo, "Dev")
	originalInode, originalInodeOK := journalStatField(heldInfo, "Ino")
	if !originalDeviceOK || !originalInodeOK {
		t.Fatal("fixture could not read original FIFO device/inode")
	}
	if err := os.Remove(fixture.inventory.MainFIFO.Path); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(fixture.inventory.MainFIFO.Path, 0o600); err != nil {
		t.Fatal(err)
	}
	newInfo, err := os.Lstat(fixture.inventory.MainFIFO.Path)
	if err != nil {
		t.Fatal(err)
	}
	newDevice, newDeviceOK := journalStatField(newInfo, "Dev")
	newInode, newInodeOK := journalStatField(newInfo, "Ino")
	if !newDeviceOK || !newInodeOK {
		t.Fatal("fixture could not read recreated FIFO device/inode")
	}
	if originalDevice == newDevice && originalInode == newInode {
		t.Fatalf("fixture recreated FIFO with original identity dev=%d ino=%d", newDevice, newInode)
	}
	if first.MainFIFO.Device == newDevice && first.MainFIFO.Inode == newInode {
		t.Fatalf("fixture recreated FIFO with first resolved identity dev=%d ino=%d", newDevice, newInode)
	}
	_, err = ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:             filepath.Join(fixture.root, "proc"),
		Phase:                InventoryPhasePreDispatch,
		Prior:                &first,
		RequireMainFIFOOwner: false,
	})
	if err == nil {
		t.Fatal("same-boot FIFO inode churn was accepted")
	}
	if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:             filepath.Join(fixture.root, "proc"),
		Phase:                InventoryPhasePreDispatch,
		RequireMainFIFOOwner: false,
	}); err != nil {
		t.Fatal("new-boot FIFO recreation was rejected", err)
	}
}

func TestResolveInventoryAtSameBootInodeChurnStressRetainsDescriptor(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	for iteration := 0; iteration < 100; iteration++ {
		prior, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
			ProcRoot:             filepath.Join(fixture.root, "proc"),
			Phase:                InventoryPhasePreDispatch,
			RequireMainFIFOOwner: false,
		})
		if err != nil {
			t.Fatalf("iteration %d initial resolve: %v", iteration, err)
		}
		hold, heldInfo, err := openProtectedMetadata(fixture.inventory.MainFIFO.Path)
		if err != nil {
			t.Fatalf("iteration %d hold FIFO: %v", iteration, err)
		}
		oldDevice, oldDeviceOK := journalStatField(heldInfo, "Dev")
		oldInode, oldInodeOK := journalStatField(heldInfo, "Ino")
		if !oldDeviceOK || !oldInodeOK {
			_ = hold.Close()
			t.Fatalf("iteration %d original FIFO identity unavailable", iteration)
		}
		if err := os.Remove(fixture.inventory.MainFIFO.Path); err != nil {
			_ = hold.Close()
			t.Fatalf("iteration %d remove FIFO: %v", iteration, err)
		}
		if err := unix.Mkfifo(fixture.inventory.MainFIFO.Path, 0o600); err != nil {
			_ = hold.Close()
			t.Fatalf("iteration %d recreate FIFO: %v", iteration, err)
		}
		newInfo, err := os.Lstat(fixture.inventory.MainFIFO.Path)
		if err != nil {
			_ = hold.Close()
			t.Fatalf("iteration %d stat recreated FIFO: %v", iteration, err)
		}
		newDevice, newDeviceOK := journalStatField(newInfo, "Dev")
		newInode, newInodeOK := journalStatField(newInfo, "Ino")
		if !newDeviceOK || !newInodeOK || oldDevice == newDevice && oldInode == newInode {
			_ = hold.Close()
			t.Fatalf("iteration %d did not create a distinct FIFO identity: old=(%d,%d) new=(%d,%d)", iteration, oldDevice, oldInode, newDevice, newInode)
		}
		if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
			ProcRoot:             filepath.Join(fixture.root, "proc"),
			Phase:                InventoryPhasePreDispatch,
			Prior:                &prior,
			RequireMainFIFOOwner: false,
		}); err == nil {
			_ = hold.Close()
			t.Fatalf("iteration %d accepted same-boot FIFO identity churn", iteration)
		}
		if err := hold.Close(); err != nil {
			t.Fatalf("iteration %d close held FIFO: %v", iteration, err)
		}
	}
}

func TestResolveInventoryAtVerifiesApprovedTrampolineBytesAndState(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	source := &fixture.inventory.StartSources[0]
	if err := os.WriteFile(source.Path, BuildApprovedRecoveryTrampoline(), 0o755); err != nil {
		t.Fatal(err)
	}
	trampolineHash := sha256.Sum256(BuildApprovedRecoveryTrampoline())
	source.DisabledState = "approved_trampoline"
	source.DisabledSHA256 = hex.EncodeToString(trampolineHash[:])
	resolved, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:             filepath.Join(fixture.root, "proc"),
		Phase:                InventoryPhasePreDispatch,
		RequireMainFIFOOwner: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.StartSources) != 1 || resolved.StartSources[0].State != "approved_trampoline" {
		t.Fatalf("resolved source=%#v", resolved.StartSources)
	}
	if err := os.WriteFile(source.Path, []byte("#!/bin/sh\nexec unexpected\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:             filepath.Join(fixture.root, "proc"),
		Phase:                InventoryPhasePreDispatch,
		RequireMainFIFOOwner: false,
	}); err == nil {
		t.Fatal("non-attested trampoline bytes were accepted")
	}
}

func TestResolveInventoryAtRejectsOriginalDisabledSourceReappearance(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	if err := os.WriteFile(fixture.inventory.StartSources[0].Path, []byte("old dispatcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:             filepath.Join(fixture.root, "proc"),
		Phase:                InventoryPhasePreDispatch,
		RequireMainFIFOOwner: false,
	}); err == nil {
		t.Fatal("original disabled source reappearance was accepted")
	}
}

func TestResolveInventoryAtRequiresMainProcessExecutableIdentity(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	procRoot := filepath.Join(fixture.root, "proc")
	if err := os.MkdirAll(procRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	processDir := writeProcStatFixture(t, procRoot, 51, 101, "S")
	if err := os.Symlink(fixture.inventory.MainExecutable.Path, filepath.Join(processDir, "exe")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(processDir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               procRoot,
		Phase:                  InventoryPhasePreDispatch,
		RequireProcessEvidence: true,
		RequireMainFIFOOwner:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MainExecutable.Device == 0 || resolved.MainExecutable.Inode == 0 {
		t.Fatalf("resolved Main identity=%#v", resolved.MainExecutable)
	}
}

func TestResolveInventoryAtRejectsReusedMainPIDIdentity(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	procRoot := filepath.Join(fixture.root, "proc")
	if err := os.MkdirAll(procRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	processDir := writeProcStatFixture(t, procRoot, 51, 101, "S")
	if err := os.Symlink(fixture.inventory.MainExecutable.Path, filepath.Join(processDir, "exe")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               procRoot,
		Phase:                  InventoryPhasePreDispatch,
		RequireProcessEvidence: true,
		MainProcess:            &ProcessIdentity{PID: 51, StartTime: 202, Device: 1, Inode: 1, SHA256: strings.Repeat("a", 64)},
		RequireMainFIFOOwner:   false,
	})
	if err == nil || strings.Contains(err.Error(), "not enabled in this resolver tranche") {
		t.Fatal("reused Main PID/start-time identity was not evaluated")
	}
}

func TestResolveInventoryAtFailsClosedForLiveProcEntryWithoutExecutable(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	procRoot := filepath.Join(fixture.root, "proc")
	if err := os.MkdirAll(procRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = writeProcStatFixture(t, procRoot, 52, 102, "S")
	_, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               procRoot,
		Phase:                  InventoryPhasePreDispatch,
		RequireProcessEvidence: true,
		RequireMainFIFOOwner:   false,
	})
	if err == nil || strings.Contains(err.Error(), "not enabled in this resolver tranche") {
		t.Fatal("live proc entry without executable was not evaluated")
	}
}

func TestResolveInventoryAtJoinsEveryConfiguredSocketRoute(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	fixture.inventory.InputListen = &NetworkExpectation{Network: "tcp", Address: "127.0.0.1:8080"}
	fixture.inventory.CastRTP = &NetworkExpectation{Network: "udp", Address: "127.0.0.1:9000"}
	procRoot := filepath.Join(fixture.root, "proc")
	if err := os.MkdirAll(filepath.Join(procRoot, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "net", "tcp"), []byte("sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n  0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "net", "udp"), []byte("sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n  0: 0100007F:2328 00000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 222\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fdDir := filepath.Join(procRoot, "60", "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[111]", filepath.Join(fdDir, "3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[222]", filepath.Join(fdDir, "4")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               procRoot,
		Phase:                  InventoryPhasePreDispatch,
		ScanProcessDescriptors: true,
		RequireNetworkEvidence: true,
		RequireMainFIFOOwner:   false,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveInventoryAtRejectsIndividuallyMissingSocketRoute(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	fixture.inventory.InputListen = &NetworkExpectation{Network: "tcp", Address: "127.0.0.1:8080"}
	fixture.inventory.CastRTP = &NetworkExpectation{Network: "udp", Address: "127.0.0.1:9000"}
	procRoot := filepath.Join(fixture.root, "proc")
	if err := os.MkdirAll(filepath.Join(procRoot, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "net", "tcp"), []byte("sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n  0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 111\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fdDir := filepath.Join(procRoot, "60", "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[111]", filepath.Join(fdDir, "3")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               procRoot,
		Phase:                  InventoryPhasePreDispatch,
		ScanProcessDescriptors: true,
		RequireNetworkEvidence: true,
		RequireMainFIFOOwner:   false,
	}); err == nil || strings.Contains(err.Error(), "not enabled in this resolver tranche") {
		t.Fatal("missing configured socket route was not evaluated")
	}
}

func TestProductionLiveProofCheck4cBindsMainSupervisorAndAgentIdentity(t *testing.T) {
	identity, err := CurrentProcessAttestation()
	if err != nil {
		t.Fatal(err)
	}
	proof := BootProof{
		SupervisorPID:              identity.PID,
		SupervisorStartTime:        identity.StartTime,
		SupervisorExecutableDevice: identity.Device,
		SupervisorExecutableInode:  identity.Inode,
		SupervisorExecutableSHA256: identity.SHA256,
		MainPID:                    identity.PID,
		MainStartTime:              identity.StartTime,
		MainExecutableDevice:       identity.Device,
		MainExecutableInode:        identity.Inode,
		MainExecutableSHA256:       identity.SHA256,
		AgentPID:                   identity.PID,
		AgentStartTime:             identity.StartTime,
		AgentExecutableDevice:      identity.Device,
		AgentExecutableInode:       identity.Inode,
		AgentExecutableSHA256:      identity.SHA256,
	}
	if err := productionLiveProofCheck(context.Background(), proof); err != nil {
		t.Fatal(err)
	}
	proof.AgentStartTime++
	if err := productionLiveProofCheck(context.Background(), proof); err == nil {
		t.Fatal("reused live process identity was accepted")
	}
	proof.AgentStartTime = identity.StartTime
	proof.MainStartTime++
	if err := productionLiveProofCheck(context.Background(), proof); err == nil {
		t.Fatal("reused Main process identity was accepted")
	}
}

func TestProductionLiveProofCheckHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := productionLiveProofCheck(ctx, BootProof{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("live proof cancellation error=%v", err)
	}
}

func TestProductionQuiescenceVerifierInstallsLiveIdentityCheck(t *testing.T) {
	verifier, ok := NewProductionQuiescenceVerifier().(*bootQuiescenceVerifier)
	if !ok || verifier.dependencies.CheckLive == nil {
		t.Fatal("production quiescence verifier has no live identity check")
	}
}

func validBootProof4c() BootProof {
	return BootProof{
		Schema: 1, BootID: testBootID, OwnerSession: strings.Repeat("1", 32), OwnerGeneration: 1,
		SupervisorPID: 11, SupervisorStartTime: 12, SupervisorExecutableDevice: 13, SupervisorExecutableInode: 14, SupervisorExecutableSHA256: strings.Repeat("a", 64),
		MainPID: 21, MainStartTime: 22, MainExecutableDevice: 23, MainExecutableInode: 24, MainExecutableSHA256: strings.Repeat("b", 64),
		AgentPID: 31, AgentStartTime: 32, AgentExecutableDevice: 33, AgentExecutableInode: 34, AgentExecutableSHA256: strings.Repeat("c", 64),
		ProfileSHA256: strings.Repeat("d", 64), JournalSHA256: strings.Repeat("e", 64), Capabilities: DevelopmentCapabilities(),
		ResolvedInventory: resolvedFromInventory(InventoryV1{
			Schema:         1,
			MainExecutable: PathExpectation{Path: "/usr/bin/Main_MiSTer", Kind: "regular", SHA256: strings.Repeat("f", 64)},
			MainFIFO:       PathExpectation{Path: "/dev/MiSTer_cmd", Kind: "fifo"}, StartSources: []SourceRecord{},
		}),
	}
}

func TestQuiescenceVerifierRejectsMissingPostMainEvidence(t *testing.T) {
	proof := validBootProof4c()
	store := NewBootProofStore(filepath.Join(t.TempDir(), "proof.json"), uint32(os.Getuid()))
	if err := store.Replace(proof); err != nil {
		t.Fatal(err)
	}
	owner := hardwareowner.Record{
		Schema:              1,
		State:               hardwareowner.StateNormalMain,
		BootID:              proof.BootID,
		GenerationHighWater: proof.OwnerGeneration,
		ActiveSession:       proof.OwnerSession,
		ActiveGeneration:    proof.OwnerGeneration,
		ActiveMode:          hardwareowner.ModeFPGANative,
		ActiveOwner:         hardwareowner.OwnerCompatMain,
		ActiveLeases:        hardwareowner.NormalLeases(),
		CandidateMode:       hardwareowner.ModeNone,
		CandidateOwner:      hardwareowner.OwnerNone,
		QuiescingOwner:      hardwareowner.OwnerNone,
		RequestedResources:  []string{},
	}
	status := MaintenanceStatus{TerminalJournalSHA256: proof.JournalSHA256, Inventory: InventoryV1{
		Schema:         1,
		MainExecutable: PathExpectation{Path: "/usr/bin/Main_MiSTer", Kind: "regular", SHA256: strings.Repeat("f", 64)},
		MainFIFO:       PathExpectation{Path: "/dev/MiSTer_cmd", Kind: "fifo"},
		StartSources:   []SourceRecord{},
	}}
	verifier := NewQuiescenceVerifier(store, func() (string, error) { return proof.BootID, nil })
	if _, err := verifier.VerifyPostMain(context.Background(), status, owner); err == nil {
		t.Fatal("post-Main proof succeeded without an evidence callback")
	}
}

func TestResolveInventoryAtJoinsIPv6SocketRoutes(t *testing.T) {
	fixture := newInventoryResolutionFixture(t)
	fixture.inventory.CastControl = &NetworkExpectation{Network: "tcp6", Address: "[::1]:8081"}
	fixture.inventory.CastRTP = &NetworkExpectation{Network: "udp6", Address: "[::]:9001"}
	procRoot := filepath.Join(fixture.root, "proc")
	if err := os.MkdirAll(filepath.Join(procRoot, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	netLine := func(local, inode string) string {
		return "  0: " + local + " 00000000000000000000000000000000:0000 0A 0000000000000000:00000000 00:00000000 00000000 0 0 " + inode + "\n"
	}
	if err := os.WriteFile(filepath.Join(procRoot, "net", "tcp6"), []byte("sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n"+netLine("00000000000000000000000001000000:1F91", "333")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "net", "udp6"), []byte("sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n"+netLine("00000000000000000000000000000000:2329", "444")), 0o600); err != nil {
		t.Fatal(err)
	}
	fdDir := filepath.Join(procRoot, "60", "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[333]", filepath.Join(fdDir, "3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[444]", filepath.Join(fdDir, "4")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInventoryAt(context.Background(), fixture.inventory, InventoryResolutionOptions{
		ProcRoot:               procRoot,
		Phase:                  InventoryPhasePreDispatch,
		ScanProcessDescriptors: true,
		RequireNetworkEvidence: true,
		RequireMainFIFOOwner:   false,
	}); err != nil {
		t.Fatal(err)
	}
}

type inventoryResolutionFixture struct {
	root      string
	inventory InventoryV1
}

func newInventoryResolutionFixture(t *testing.T) inventoryResolutionFixture {
	t.Helper()
	root := t.TempDir()
	mainPath := filepath.Join(root, "Main_MiSTer")
	mainBytes := []byte("main fixture\n")
	if err := os.WriteFile(mainPath, mainBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := unix.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(root, "token")
	hash := sha256.Sum256(mainBytes)
	fileHash := hex.EncodeToString(hash[:])
	source := SourceRecord{
		Path:           filepath.Join(root, "legacy-start"),
		Kind:           "regular",
		Mode:           0o755,
		SHA256:         strings.Repeat("a", 64),
		BackupPath:     filepath.Join(root, "backup"),
		BackupSHA256:   strings.Repeat("b", 64),
		DisabledState:  "absent",
		DisabledSHA256: "",
	}
	return inventoryResolutionFixture{root: root, inventory: InventoryV1{
		Schema:         1,
		MainExecutable: PathExpectation{Path: mainPath, Kind: "regular", SHA256: fileHash},
		CastTokenFile:  &PathExpectation{Path: tokenPath, Kind: "regular", SHA256: strings.Repeat("c", 64)},
		MainFIFO:       PathExpectation{Path: fifoPath, Kind: "fifo"},
		StartSources:   []SourceRecord{source},
	}}
}
