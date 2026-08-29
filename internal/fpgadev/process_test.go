//go:build linux

package fpgadev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func testExecutableIdentity() ExecutableIdentity {
	return ExecutableIdentity{Device: 11, Inode: 22, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
}

func processFor(pid int, start uint64, executable ExecutableIdentity) ProcessIdentity {
	return ProcessIdentity{PID: pid, StartTime: start, Device: executable.Device, Inode: executable.Inode, SHA256: executable.SHA256}
}

type fakeProcessScanner struct {
	scans [][]ProcessRecord
	reads int
	err   error
}

func (s *fakeProcessScanner) Scan(ctx context.Context, _ ExecutableIdentity) ([]ProcessRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := s.reads
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	if len(s.scans) == 0 {
		return nil, nil
	}
	if index >= len(s.scans) {
		index = len(s.scans) - 1
	}
	return append([]ProcessRecord(nil), s.scans[index]...), nil
}

type fakeProcessClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeProcessTimer
}

type fakeProcessTimer struct {
	due time.Time
	out chan time.Time
}

func (c *fakeProcessClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeProcessClock) After(d time.Duration) <-chan time.Time {
	out := make(chan time.Time, 1)
	c.mu.Lock()
	c.timers = append(c.timers, fakeProcessTimer{due: c.now.Add(d), out: out})
	c.mu.Unlock()
	return out
}

// Advance is a deterministic, buffered timer drive.  It never starts a
// goroutine and therefore cannot leave timer work behind after a test.
func (c *fakeProcessClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	ready := make([]fakeProcessTimer, 0, len(c.timers))
	pending := c.timers[:0]
	for _, timer := range c.timers {
		if !timer.due.After(c.now) {
			ready = append(ready, timer)
		} else {
			pending = append(pending, timer)
		}
	}
	c.timers = pending
	now := c.now
	c.mu.Unlock()
	for _, timer := range ready {
		timer.out <- now
	}
}

func (c *fakeProcessClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func waitForFakeTimer(t *testing.T, clock *fakeProcessClock) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if clock.Pending() != 0 {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("timed out waiting for fake timer")
}

func awaitProcessResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for process observer")
		return nil
	}
}

func TestFakeProcessClockUsesDeterministicBufferedTimersWithoutLeaks(t *testing.T) {
	clock := &fakeProcessClock{now: time.Unix(1, 0)}
	before := runtime.NumGoroutine()
	first := clock.After(250 * time.Millisecond)
	second := clock.After(500 * time.Millisecond)
	if clock.Pending() != 2 {
		t.Fatalf("pending timers = %d, want 2", clock.Pending())
	}
	clock.Advance(249 * time.Millisecond)
	select {
	case <-first:
		t.Fatal("first timer fired before its due time")
	default:
	}
	clock.Advance(time.Millisecond)
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first timer did not fire at 250 ms")
	}
	if clock.Pending() != 1 {
		t.Fatalf("pending timers after first drive = %d, want 1", clock.Pending())
	}
	clock.Advance(250 * time.Millisecond)
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("second timer did not fire at 500 ms")
	}
	if clock.Pending() != 0 {
		t.Fatalf("pending timers after completion = %d, want 0", clock.Pending())
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("fake timer scheduler leaked goroutines: before=%d after=%d", before, after)
	}
}

func processRecord(identity ProcessIdentity) ProcessRecord {
	return ProcessRecord{Identity: identity}
}

func TestProcessObserverSnapshotSortsAndDeduplicatesExpectedPopulation(t *testing.T) {
	expected := testExecutableIdentity()
	other := ExecutableIdentity{Device: 33, Inode: 44, SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	scanner := &fakeProcessScanner{scans: [][]ProcessRecord{{
		processRecord(processFor(7, 2, expected)),
		processRecord(processFor(3, 1, expected)),
		processRecord(processFor(7, 2, expected)),
		processRecord(processFor(9, 4, other)),
	}}}
	observer := Observer{Expected: expected, Scanner: scanner}

	got, err := observer.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := []ProcessIdentity{processFor(3, 1, expected), processFor(7, 2, expected)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %#v, want %#v", got, want)
	}
}

func TestProcessObserverWaitStableAbsentHandlesOriginalExit(t *testing.T) {
	expected := testExecutableIdentity()
	baseline := []ProcessIdentity{processFor(17, 100, expected)}
	scanner := &fakeProcessScanner{scans: [][]ProcessRecord{
		{processRecord(baseline[0])},
		{},
	}}
	clock := &fakeProcessClock{now: time.Unix(1, 0)}
	observer := Observer{Expected: expected, Scanner: scanner, Clock: clock, PollInterval: 250 * time.Millisecond}
	result := make(chan error, 1)
	go func() { result <- observer.WaitStableAbsent(context.Background(), baseline, 250*time.Millisecond) }()
	waitForFakeTimer(t, clock)
	clock.Advance(249 * time.Millisecond)
	select {
	case err := <-result:
		t.Fatalf("WaitStableAbsent completed before exact boundary: %v", err)
	default:
	}
	clock.Advance(time.Millisecond)
	waitForFakeTimer(t, clock)
	clock.Advance(250 * time.Millisecond)
	if err := awaitProcessResult(t, result); err != nil {
		t.Fatalf("WaitStableAbsent: %v", err)
	}
}

func TestProcessObserverReplacementAndReparentingNeverAcceptStableAbsence(t *testing.T) {
	expected := testExecutableIdentity()
	baseline := []ProcessIdentity{processFor(17, 100, expected)}
	tests := []struct {
		name  string
		scans [][]ProcessRecord
	}{
		{
			name: "same executable double fork replacement",
			scans: [][]ProcessRecord{
				{},
				{processRecord(processFor(18, 101, expected))},
			},
		},
		{
			name:  "reparented original",
			scans: [][]ProcessRecord{{processRecord(processFor(17, 100, expected))}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &fakeProcessClock{now: time.Unix(1, 0)}
			scanner := &fakeProcessScanner{scans: tt.scans}
			observer := Observer{Expected: expected, Scanner: scanner, Clock: clock, PollInterval: time.Millisecond}
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() { result <- observer.WaitStableAbsent(ctx, baseline, 250*time.Millisecond) }()
			waitForFakeTimer(t, clock)
			clock.Advance(time.Millisecond)
			if tt.name == "same executable double fork replacement" {
				waitForFakeTimer(t, clock)
				clock.Advance(time.Millisecond)
			} else {
				waitForFakeTimer(t, clock)
			}
			cancel()
			if err := awaitProcessResult(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("WaitStableAbsent error = %v, want cancellation", err)
			}
		})
	}
}

func TestProcessObserverRejectsPIDReuseStartTimeAndExecutableReplacement(t *testing.T) {
	expected := testExecutableIdentity()
	baseline := []ProcessIdentity{processFor(17, 100, expected)}
	replaced := ExecutableIdentity{Device: 55, Inode: 66, SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	tests := []struct {
		name  string
		entry ProcessIdentity
	}{
		{name: "start time mismatch", entry: processFor(17, 101, expected)},
		{name: "executable replacement", entry: processFor(17, 100, replaced)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &fakeProcessScanner{scans: [][]ProcessRecord{{processRecord(tt.entry)}}}
			observer := Observer{Expected: expected, Scanner: scanner}
			if err := observer.WaitStableAbsent(context.Background(), baseline, 250*time.Millisecond); err == nil {
				t.Fatal("identity replacement was accepted as absence")
			}
		})
	}
}

func TestProcessObserverTransient249msAbsenceResetsTimerAtAppearance(t *testing.T) {
	expected := testExecutableIdentity()
	baseline := []ProcessIdentity{processFor(17, 100, expected)}
	scanner := &fakeProcessScanner{scans: [][]ProcessRecord{
		{},
		{processRecord(processFor(18, 101, expected))},
		{},
	}}
	clock := &fakeProcessClock{now: time.Unix(1, 0)}
	observer := Observer{Expected: expected, Scanner: scanner, Clock: clock, PollInterval: 250 * time.Millisecond}
	result := make(chan error, 1)
	go func() { result <- observer.WaitStableAbsent(context.Background(), baseline, 250*time.Millisecond) }()
	waitForFakeTimer(t, clock)
	clock.Advance(249 * time.Millisecond)
	select {
	case err := <-result:
		t.Fatalf("WaitStableAbsent completed before exact boundary: %v", err)
	default:
	}
	clock.Advance(time.Millisecond)
	waitForFakeTimer(t, clock)
	clock.Advance(250 * time.Millisecond)
	waitForFakeTimer(t, clock)
	clock.Advance(250 * time.Millisecond)
	if err := awaitProcessResult(t, result); err != nil {
		t.Fatalf("WaitStableAbsent after transient absence: %v", err)
	}
	if scanner.reads < 4 {
		t.Fatalf("scans = %d, want reappearance and fresh absence observation", scanner.reads)
	}
}

func TestProcessObserverFailsClosedForUnreadableOrRacingProcEntries(t *testing.T) {
	expected := testExecutableIdentity()
	scanErr := errors.New("proc entry raced")
	observer := Observer{Expected: expected, Scanner: &fakeProcessScanner{err: scanErr}}
	if _, err := observer.Snapshot(); !errors.Is(err, scanErr) {
		t.Fatalf("Snapshot error = %v, want %v", err, scanErr)
	}
	if err := observer.WaitStableAbsent(context.Background(), nil, 250*time.Millisecond); !errors.Is(err, scanErr) {
		t.Fatalf("WaitStableAbsent error = %v, want %v", err, scanErr)
	}
}

func TestProcessObserverRejectsStaleBaselineConflictingAliasesAndInvalidDigests(t *testing.T) {
	expected := testExecutableIdentity()
	other := ExecutableIdentity{Device: 33, Inode: 44, SHA256: strings.Repeat("b", 64)}
	baseline := []ProcessIdentity{processFor(17, 100, other)}
	observer := Observer{Expected: expected, Scanner: &fakeProcessScanner{scans: [][]ProcessRecord{{}}}}
	if err := observer.WaitStableAbsent(context.Background(), baseline, 0); err == nil {
		t.Fatal("stale baseline executable was accepted")
	}

	conflicting := Observer{Expected: expected, ExpectedExecutable: other, Scanner: &fakeProcessScanner{scans: [][]ProcessRecord{{}}}}
	if _, err := conflicting.Snapshot(); err == nil {
		t.Fatal("conflicting expected-identity aliases were silently accepted")
	}

	invalid := expected
	invalid.SHA256 = strings.Repeat("A", 64)
	if _, err := (Observer{Expected: invalid, Scanner: &fakeProcessScanner{scans: [][]ProcessRecord{{}}}}).Snapshot(); err == nil {
		t.Fatal("uppercase executable digest was accepted")
	}
	invalid = expected
	invalid.SHA256 = strings.Repeat("a", 63)
	if _, err := (Observer{Expected: invalid, Scanner: &fakeProcessScanner{scans: [][]ProcessRecord{{}}}}).Snapshot(); err == nil {
		t.Fatal("short executable digest was accepted")
	}

	badRecord := processFor(17, 100, expected)
	badRecord.SHA256 = strings.Repeat("g", 64)
	if _, err := (Observer{Expected: expected, Scanner: &fakeProcessScanner{scans: [][]ProcessRecord{{processRecord(badRecord)}}}}).Snapshot(); err == nil {
		t.Fatal("invalid process executable digest was accepted")
	}
}

type blockingProcessScanner struct {
	started chan struct{}
}

func (s *blockingProcessScanner) Scan(ctx context.Context, _ ExecutableIdentity) ([]ProcessRecord, error) {
	close(s.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestProcessObserverPassesDeadlineToBlockingScanner(t *testing.T) {
	expected := testExecutableIdentity()
	scanner := &blockingProcessScanner{started: make(chan struct{})}
	observer := Observer{Expected: expected, Scanner: scanner}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := observer.WaitStableAbsent(ctx, nil, 250*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitStableAbsent error = %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("blocking scan cancellation took %v", elapsed)
	}
}

func TestProcessObserverDoesNotSucceedAfterScanCancelsContext(t *testing.T) {
	expected := testExecutableIdentity()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scanner := &cancelingProcessScanner{cancel: cancel}
	observer := Observer{Expected: expected, Scanner: scanner}
	if err := observer.WaitStableAbsent(ctx, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitStableAbsent error = %v, want cancellation", err)
	}
}

type cancelingProcessScanner struct {
	cancel context.CancelFunc
}

func (s *cancelingProcessScanner) Scan(context.Context, ExecutableIdentity) ([]ProcessRecord, error) {
	s.cancel()
	return nil, nil
}

func TestProcessObserverCancellationAndDeadlineAreBounded(t *testing.T) {
	expected := testExecutableIdentity()
	baseline := []ProcessIdentity{processFor(17, 100, expected)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer := Observer{Expected: expected, Scanner: &fakeProcessScanner{scans: [][]ProcessRecord{{}}}}
	if err := observer.WaitStableAbsent(ctx, baseline, 250*time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled WaitStableAbsent = %v", err)
	}

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer deadlineCancel()
	observer = Observer{Expected: expected, Scanner: &fakeProcessScanner{scans: [][]ProcessRecord{{processRecord(baseline[0])}}}, PollInterval: time.Millisecond}
	if err := observer.WaitStableAbsent(deadlineCtx, baseline, 250*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline WaitStableAbsent = %v", err)
	}
}

func TestProcessObserverBaselineOrderingAndDuplicateInputsAreDeterministic(t *testing.T) {
	expected := testExecutableIdentity()
	a := processFor(9, 2, expected)
	b := processFor(3, 1, expected)
	scanner := &fakeProcessScanner{scans: [][]ProcessRecord{{}}}
	observer := Observer{Expected: expected, Scanner: scanner}
	baseline := []ProcessIdentity{a, b, a}
	original := append([]ProcessIdentity(nil), baseline...)
	// Zero duration still performs one complete scan and returns only after the
	// supplied baseline has been normalized for identity checks.
	if err := observer.WaitStableAbsent(context.Background(), baseline, 0); err != nil {
		t.Fatalf("WaitStableAbsent: %v", err)
	}
	if !reflect.DeepEqual(baseline, original) {
		t.Fatalf("baseline mutated from %#v to %#v", original, baseline)
	}
}

func TestParseProcStatStartTimeHandlesSpacesAndParenthesesInComm(t *testing.T) {
	fields := make([]string, 20)
	fields[0] = "S"
	for index := 1; index < len(fields); index++ {
		fields[index] = "0"
	}
	fields[19] = "987654"
	raw := "123 (worker has spaces ) and parentheses) " + strings.Join(fields, " ")
	start, pid, err := parseProcStatStartTime([]byte(raw))
	if err != nil {
		t.Fatalf("parseProcStatStartTime: %v", err)
	}
	if pid != 123 || start != 987654 {
		t.Fatalf("parsed pid/start = %d/%d, want 123/987654", pid, start)
	}
}

func TestParseProcStatFieldsExtractsStateFlagsAndStartTime(t *testing.T) {
	fields := make([]string, 20)
	fields[0] = "I"
	for index := 1; index < len(fields); index++ {
		fields[index] = "0"
	}
	fields[6] = fmt.Sprint(processFlagKThread)
	fields[19] = "987654"
	raw := "123 (worker ) with parentheses) " + strings.Join(fields, " ")
	start, pid, state, flags, err := parseProcStatFields([]byte(raw))
	if err != nil {
		t.Fatalf("parseProcStatFields: %v", err)
	}
	if pid != 123 || start != 987654 || state != 'I' || flags != processFlagKThread {
		t.Fatalf("parsed pid/start/state/flags = %d/%d/%q/%#x", pid, start, state, flags)
	}
}

func TestLinuxProcessObserverScansCompleteProcPopulation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	executablePath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(executablePath, []byte("main-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := testExecutableIdentityFromPath(executablePath)
	if err != nil {
		t.Fatalf("linuxExecutableIdentity: %v", err)
	}
	for _, fixture := range []struct {
		pid   int
		start uint64
		comm  string
		want  bool
	}{
		{pid: 31, start: 77, comm: "Main worker", want: true},
		{pid: 32, start: 78, comm: "other", want: false},
	} {
		processDir := filepath.Join(root, fmt.Sprint(fixture.pid))
		if err := os.Mkdir(processDir, 0o700); err != nil {
			t.Fatal(err)
		}
		fields := make([]string, 20)
		fields[0] = "S"
		for index := 1; index < len(fields); index++ {
			fields[index] = "0"
		}
		fields[19] = fmt.Sprint(fixture.start)
		stat := fmt.Sprintf("%d (%s) %s", fixture.pid, fixture.comm, strings.Join(fields, " "))
		if err := os.WriteFile(filepath.Join(processDir, "stat"), []byte(stat), 0o600); err != nil {
			t.Fatal(err)
		}
		target := executablePath
		if !fixture.want {
			target = filepath.Join(root, "other")
			if err := os.WriteFile(target, []byte("other-binary"), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(target, filepath.Join(processDir, "exe")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := (Observer{ProcRoot: root, Expected: executable}).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := []ProcessIdentity{{PID: 31, StartTime: 77, Device: executable.Device, Inode: executable.Inode, SHA256: executable.SHA256}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %#v, want %#v", got, want)
	}
}

func writeProcStatFixture(t *testing.T, root string, pid int, start uint64, state string) string {
	return writeProcStatFixtureWithFlags(t, root, pid, start, state, 0)
}

func writeProcStatFixtureWithFlags(t *testing.T, root string, pid int, start uint64, state string, flags uint64) string {
	t.Helper()
	processDir := filepath.Join(root, fmt.Sprint(pid))
	if err := os.Mkdir(processDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fields := make([]string, 20)
	fields[0] = state
	for index := 1; index < len(fields); index++ {
		fields[index] = "0"
	}
	fields[6] = fmt.Sprint(flags)
	fields[19] = fmt.Sprint(start)
	stat := fmt.Sprintf("%d (fixture) %s", pid, strings.Join(fields, " "))
	if err := os.WriteFile(filepath.Join(processDir, "stat"), []byte(stat), 0o600); err != nil {
		t.Fatal(err)
	}
	return processDir
}

func TestLinuxProcessObserverSkipsPFKThreadMissingExe(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	executable, mainDir, want := makeMainFixture(t, root)
	writeProcStatFixtureWithFlags(t, root, 32, 78, "S", processFlagKThread)
	got, err := (Observer{ProcRoot: root, Expected: executable}).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot with PF_KTHREAD no-exe entry: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %#v, want %#v", got, want)
	}
	_ = mainDir
}

func TestLinuxProcessObserverSkipsZombieMissingExe(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	executable, _, want := makeMainFixture(t, root)
	writeProcStatFixture(t, root, 33, 79, "Z")
	got, err := (Observer{ProcRoot: root, Expected: executable}).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot with zombie no-exe entry: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %#v, want %#v", got, want)
	}
}

func TestLinuxProcessObserverSkipsDisappearedMissingExe(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	executable, _, want := makeMainFixture(t, root)
	writeProcStatFixture(t, root, 34, 80, "S")
	var statReads atomic.Int32
	scanner := linuxProcessScanner{root: root}
	scanner.readStat = func(ctx context.Context, path string, pid int) (procStatInfo, error) {
		if pid == 34 && statReads.Add(1) == 2 {
			return procStatInfo{}, processError("read process stat failed", unix.ENOENT)
		}
		return readLinuxProcStat(ctx, path, pid)
	}
	got, err := (Observer{Expected: executable, Scanner: scanner}).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot with disappeared no-exe entry: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %#v, want %#v", got, want)
	}
}

func TestLinuxProcessObserverFailsClosedForUnexplainedLiveMissingExe(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	executable, _, _ := makeMainFixture(t, root)
	writeProcStatFixture(t, root, 35, 81, "S")
	if _, err := (Observer{ProcRoot: root, Expected: executable}).Snapshot(); err == nil {
		t.Fatal("persistent live-state missing exe was accepted")
	}
}

func makeMainFixture(t *testing.T, root string) (ExecutableIdentity, string, []ProcessIdentity) {
	t.Helper()
	executablePath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(executablePath, []byte("main-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := testExecutableIdentityFromPath(executablePath)
	if err != nil {
		t.Fatalf("linuxExecutableIdentity: %v", err)
	}
	mainDir := writeProcStatFixture(t, root, 31, 77, "S")
	if err := os.Symlink(executablePath, filepath.Join(mainDir, "exe")); err != nil {
		t.Fatal(err)
	}
	want := []ProcessIdentity{{PID: 31, StartTime: 77, Device: executable.Device, Inode: executable.Inode, SHA256: executable.SHA256}}
	return executable, mainDir, want
}

func TestLinuxProcessObserverFailsClosedForUnreadableExe(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	executable, _, _ := makeMainFixture(t, root)
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	procDir := writeProcStatFixture(t, root, 35, 81, "S")
	if err := os.Symlink(filepath.Join(blocked, "exe"), filepath.Join(procDir, "exe")); err != nil {
		t.Fatal(err)
	}
	_, err := (Observer{ProcRoot: root, Expected: executable}).Snapshot()
	if !errors.Is(err, unix.EACCES) {
		t.Fatalf("Snapshot unreadable exe error = %v, want EACCES", err)
	}
}

func TestLinuxProcessObserverFailsClosedForCorruptStat(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	executablePath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(executablePath, []byte("main-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := testExecutableIdentityFromPath(executablePath)
	if err != nil {
		t.Fatal(err)
	}
	corruptDir := writeProcStatFixture(t, root, 36, 82, "S")
	if err := os.WriteFile(filepath.Join(corruptDir, "stat"), []byte("36 (corrupt) S not-a-start-time"), 0o600); err != nil {
		t.Fatal(err)
	}
	mainDir := writeProcStatFixture(t, root, 37, 83, "S")
	if err := os.Symlink(executablePath, filepath.Join(mainDir, "exe")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Observer{ProcRoot: root, Expected: executable}).Snapshot(); err == nil {
		t.Fatal("corrupt stat entry was silently skipped")
	}
}

func TestLinuxProcessScannerHashesOnlyMatchingDeviceInodeAndHonorsCancellation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	expectedPath := filepath.Join(root, "expected")
	otherPath := filepath.Join(root, "other")
	if err := os.WriteFile(expectedPath, []byte("expected"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("other"), 0o700); err != nil {
		t.Fatal(err)
	}
	expected, err := testExecutableIdentityFromPath(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	mainDir := writeProcStatFixture(t, root, 41, 91, "S")
	if err := os.Symlink(expectedPath, filepath.Join(mainDir, "exe")); err != nil {
		t.Fatal(err)
	}
	otherDir := writeProcStatFixture(t, root, 42, 92, "S")
	if err := os.Symlink(otherPath, filepath.Join(otherDir, "exe")); err != nil {
		t.Fatal(err)
	}

	var hashCalls int
	scanner := linuxProcessScanner{root: root}
	scanner.hashExecutable = func(ctx context.Context, _ string, identity ExecutableIdentity) (uint64, uint64, string, error) {
		hashCalls++
		if err := ctx.Err(); err != nil {
			return 0, 0, "", err
		}
		return identity.Device, identity.Inode, expected.SHA256, nil
	}
	if _, err := scanner.Scan(context.Background(), expected); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if hashCalls != 1 {
		t.Fatalf("hash calls = %d, want one matching candidate", hashCalls)
	}

	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	scanner.hashExecutable = func(ctx context.Context, _ string, _ ExecutableIdentity) (uint64, uint64, string, error) {
		close(started)
		<-ctx.Done()
		return 0, 0, "", ctx.Err()
	}
	result := make(chan error, 1)
	go func() {
		_, err := scanner.Scan(ctx, expected)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("candidate hash did not start")
	}
	cancel()
	if err := awaitProcessResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("slow candidate hash error = %v, want cancellation", err)
	}
}

func TestLinuxProcessScannerRetriesUnrelatedToExpectedExecTransition(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	expectedPath := filepath.Join(root, "expected")
	otherPath := filepath.Join(root, "other")
	if err := os.WriteFile(expectedPath, []byte("expected"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("other"), 0o700); err != nil {
		t.Fatal(err)
	}
	expected, err := testExecutableIdentityFromPath(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	procDir := writeProcStatFixture(t, root, 51, 101, "S")
	exePath := filepath.Join(procDir, "exe")
	if err := os.Symlink(otherPath, exePath); err != nil {
		t.Fatal(err)
	}
	var opens atomic.Int32
	scanner := linuxProcessScanner{root: root}
	scanner.openExecutable = func(ctx context.Context, path string) (*os.File, error) {
		if opens.Add(1) == 2 {
			if err := os.Remove(exePath); err != nil {
				return nil, err
			}
			if err := os.Symlink(expectedPath, exePath); err != nil {
				return nil, err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return os.Open(path)
	}
	got, err := (Observer{ProcRoot: root, Expected: expected, Scanner: scanner}).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot during unrelated-to-expected exec transition: %v", err)
	}
	want := []ProcessIdentity{{PID: 51, StartTime: 101, Device: expected.Device, Inode: expected.Inode, SHA256: expected.SHA256}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %#v, want %#v", got, want)
	}
	if opens.Load() < 4 {
		t.Fatalf("executable samples = %d, want retry and stable resample", opens.Load())
	}
}

func TestLinuxProcessObserverRejectsExpectedToUnrelatedExecRace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	expectedPath := filepath.Join(root, "expected")
	otherPath := filepath.Join(root, "other")
	if err := os.WriteFile(expectedPath, []byte("expected"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("other"), 0o700); err != nil {
		t.Fatal(err)
	}
	expected, err := testExecutableIdentityFromPath(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	procDir := writeProcStatFixture(t, root, 52, 102, "S")
	exePath := filepath.Join(procDir, "exe")
	if err := os.Symlink(expectedPath, exePath); err != nil {
		t.Fatal(err)
	}
	var opens atomic.Int32
	scanner := linuxProcessScanner{root: root}
	scanner.openExecutable = func(ctx context.Context, path string) (*os.File, error) {
		if opens.Add(1) == 2 {
			if err := os.Remove(exePath); err != nil {
				return nil, err
			}
			if err := os.Symlink(otherPath, exePath); err != nil {
				return nil, err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return os.Open(path)
	}
	baseline := []ProcessIdentity{{PID: 52, StartTime: 102, Device: expected.Device, Inode: expected.Inode, SHA256: expected.SHA256}}
	observer := Observer{ProcRoot: root, Expected: expected, Scanner: scanner}
	if err := observer.WaitStableAbsent(context.Background(), baseline, 0); err == nil {
		t.Fatal("expected-to-unrelated executable race was accepted as absence")
	}
	if opens.Load() < 4 {
		t.Fatalf("executable samples = %d, want retry and stable resample", opens.Load())
	}
}

func TestLinuxProcessScannerCancellationDuringHeldHashClosesDescriptor(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc adapter")
	}
	root := t.TempDir()
	expectedPath := filepath.Join(root, "expected")
	if err := os.WriteFile(expectedPath, []byte("expected"), 0o700); err != nil {
		t.Fatal(err)
	}
	expected, err := testExecutableIdentityFromPath(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	procDir := writeProcStatFixture(t, root, 53, 103, "S")
	if err := os.Symlink(expectedPath, filepath.Join(procDir, "exe")); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	scanner := linuxProcessScanner{root: root}
	scanner.hashHeldExecutable = func(ctx context.Context, file *os.File, _ ExecutableIdentity) (uint64, uint64, string, error) {
		close(started)
		<-ctx.Done()
		return 0, 0, "", ctx.Err()
	}
	result := make(chan error, 1)
	go func() {
		_, err := scanner.Scan(ctx, expected)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("held executable hash did not start")
	}
	cancel()
	if err := awaitProcessResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("held hash cancellation error = %v, want cancellation", err)
	}
}

func testExecutableIdentityFromPath(path string) (ExecutableIdentity, error) {
	file, err := os.Open(path)
	if err != nil {
		return ExecutableIdentity{}, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		_ = file.Close()
		return ExecutableIdentity{}, err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		_ = file.Close()
		return ExecutableIdentity{}, err
	}
	if err := file.Close(); err != nil {
		return ExecutableIdentity{}, err
	}
	return ExecutableIdentity{Device: uint64(stat.Dev), Inode: uint64(stat.Ino), SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
}
