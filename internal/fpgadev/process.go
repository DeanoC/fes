package fpgadev

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExecutableIdentity is the immutable identity of an executable file.  A
// process is considered part of the expected Main process set only when all
// three values match.
type ExecutableIdentity struct {
	Device uint64
	Inode  uint64
	SHA256 string
}

func (i ExecutableIdentity) valid() bool {
	if i.Device == 0 || i.Inode == 0 || len(i.SHA256) != 64 {
		return false
	}
	for _, value := range i.SHA256 {
		if !((value >= '0' && value <= '9') || (value >= 'a' && value <= 'f')) {
			return false
		}
	}
	return true
}

func (i ExecutableIdentity) equal(other ExecutableIdentity) bool {
	return i.Device == other.Device && i.Inode == other.Inode && i.SHA256 == other.SHA256
}

// ProcessIdentity identifies one process instance by its PID and kernel
// start-time in addition to the expected executable identity.  StartTime is
// the field 22 value from /proc/<pid>/stat, not a wall-clock timestamp.
type ProcessIdentity struct {
	PID       int
	StartTime uint64
	Device    uint64
	Inode     uint64
	SHA256    string
}

func (p ProcessIdentity) executable() ExecutableIdentity {
	return ExecutableIdentity{Device: p.Device, Inode: p.Inode, SHA256: p.SHA256}
}

func (p ProcessIdentity) equal(other ProcessIdentity) bool {
	return p.PID == other.PID && p.StartTime == other.StartTime && p.executable().equal(other.executable())
}

// ProcessRecord is one complete population entry returned by a scanner. A
// scanner returns entries for every readable process, not only expected
// matches, so WaitStableAbsent can fail closed on PID reuse or executable
// replacement instead of mistaking either for a clean exit. For an unrelated
// executable, SHA256 may be empty because the Linux scanner deliberately
// avoids hashing it; its device/inode pair is still retained for identity
// mismatch detection.
type ProcessRecord struct {
	Identity ProcessIdentity
}

// ProcessScanner is the platform seam for complete process-population scans.
// The expected identity is supplied on every call so a fake cannot
// accidentally hide the production filtering policy.
type ProcessScanner interface {
	Scan(context.Context, ExecutableIdentity) ([]ProcessRecord, error)
}

// ProcessClock is the deterministic time seam used by WaitStableAbsent.
// Implementations must return monotonic-compatible times from Now and a
// channel that becomes ready no earlier than the requested duration.
type ProcessClock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

var (
	// ErrProcessIdentityChanged means that a baseline PID was reused or its
	// executable identity changed while waiting for the original set to exit.
	ErrProcessIdentityChanged    = errors.New("process identity changed")
	ErrProcessScannerUnsupported = errors.New("process scanner unsupported")
)

// Observer watches the complete process population for one immutable
// executable identity.  ProcRoot defaults to /proc for the Linux scanner;
// Scanner and Clock are injectable for deterministic software tests.
type Observer struct {
	// ProcRoot and Root are equivalent aliases.  Root is retained as a
	// convenient spelling for callers configuring a fake proc tree.
	ProcRoot string
	Root     string

	Expected           ExecutableIdentity
	ExpectedExecutable ExecutableIdentity
	ExpectedIdentity   ExecutableIdentity
	Executable         ExecutableIdentity

	Scanner      ProcessScanner
	Clock        ProcessClock
	PollInterval time.Duration
}

// NewObserver returns an observer for an already-attested executable
// identity.  The optional proc root is for the Linux implementation and
// defaults to /proc.
func NewObserver(expected ExecutableIdentity, procRoot ...string) *Observer {
	observer := &Observer{Expected: expected, ExpectedExecutable: expected, ExpectedIdentity: expected, Executable: expected}
	if len(procRoot) != 0 {
		observer.ProcRoot = procRoot[0]
	}
	return observer
}

// NewProcessObserver is a descriptive alias for NewObserver.
func NewProcessObserver(expected ExecutableIdentity, procRoot ...string) *Observer {
	return NewObserver(expected, procRoot...)
}

// NewObserverForExecutable obtains an immutable executable identity once and
// constructs an observer.  The identity is deliberately not recomputed on
// every poll: replacement of the expected binary must be observable as a
// mismatch rather than silently becoming the new expectation.
func NewObserverForExecutable(path string, procRoot ...string) (*Observer, error) {
	if executableIdentityForPath == nil {
		return nil, ErrUnsupported
	}
	identity, err := executableIdentityForPath(path)
	if err != nil {
		return nil, err
	}
	return NewObserver(identity, procRoot...), nil
}

// executableIdentityForPath is installed by the Linux implementation.  A
// nil value is the fail-closed non-Linux behavior.
var executableIdentityForPath func(string) (ExecutableIdentity, error)

type realProcessClock struct{}

func (realProcessClock) Now() time.Time                                { return time.Now() }
func (realProcessClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }

func (o Observer) expected() ExecutableIdentity {
	identity, _ := o.expectedIdentity()
	return identity
}

func (o Observer) expectedIdentity() (ExecutableIdentity, error) {
	var selected ExecutableIdentity
	found := false
	for _, candidate := range []ExecutableIdentity{o.Expected, o.ExpectedExecutable, o.ExpectedIdentity, o.Executable} {
		if candidate == (ExecutableIdentity{}) {
			continue
		}
		if !candidate.valid() {
			return ExecutableIdentity{}, errors.New("expected executable identity is invalid")
		}
		if !found {
			selected = candidate
			found = true
			continue
		}
		if !selected.equal(candidate) {
			return ExecutableIdentity{}, errors.New("expected executable identity aliases conflict")
		}
	}
	if !found {
		return ExecutableIdentity{}, errors.New("expected executable identity is invalid")
	}
	return selected, nil
}

func (o Observer) procRoot() string {
	if o.ProcRoot != "" {
		return o.ProcRoot
	}
	if o.Root != "" {
		return o.Root
	}
	return "/proc"
}

func (o Observer) scanner() ProcessScanner {
	if o.Scanner != nil {
		return o.Scanner
	}
	if makeProcessScanner == nil {
		return nil
	}
	return makeProcessScanner(o.procRoot())
}

// makeProcessScanner is installed by process_linux.go.  Leaving it nil is
// intentional: non-Linux production builds compile but fail closed.
var makeProcessScanner func(string) ProcessScanner

func (o Observer) clock() ProcessClock {
	if o.Clock != nil {
		return o.Clock
	}
	return realProcessClock{}
}

func (o Observer) pollInterval() time.Duration {
	if o.PollInterval <= 0 {
		return 10 * time.Millisecond
	}
	return o.PollInterval
}

func (o Observer) scanRecords(ctx context.Context) ([]ProcessRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	expected, err := o.expectedIdentity()
	if err != nil {
		return nil, err
	}
	scanner := o.scanner()
	if scanner == nil {
		return nil, ErrProcessScannerUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := scanner.Scan(ctx, expected)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateProcessRecords(records, expected); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func processMatchesExpected(identity ProcessIdentity, expected ExecutableIdentity) bool {
	return identity.executable().equal(expected)
}

func normalizeProcessIdentities(identities []ProcessIdentity) []ProcessIdentity {
	result := append([]ProcessIdentity(nil), identities...)
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.PID != right.PID {
			return left.PID < right.PID
		}
		if left.StartTime != right.StartTime {
			return left.StartTime < right.StartTime
		}
		if left.Device != right.Device {
			return left.Device < right.Device
		}
		if left.Inode != right.Inode {
			return left.Inode < right.Inode
		}
		return left.SHA256 < right.SHA256
	})
	if len(result) < 2 {
		return result
	}
	deduplicated := result[:1]
	for _, identity := range result[1:] {
		if identity.equal(deduplicated[len(deduplicated)-1]) {
			continue
		}
		deduplicated = append(deduplicated, identity)
	}
	return deduplicated
}

func validateProcessRecords(records []ProcessRecord, expected ExecutableIdentity) error {
	byPID := make(map[int]ProcessIdentity, len(records))
	for _, record := range records {
		identity := record.Identity
		if identity.PID <= 0 || identity.StartTime == 0 || identity.Device == 0 || identity.Inode == 0 {
			return errors.New("process scan returned an invalid identity")
		}
		if identity.SHA256 != "" && !identity.executable().valid() {
			return errors.New("process scan returned an invalid identity")
		}
		if identity.Device == expected.Device && identity.Inode == expected.Inode && identity.SHA256 == "" {
			return errors.New("process scan omitted candidate executable digest")
		}
		if previous, exists := byPID[identity.PID]; exists && !previous.equal(identity) {
			return ErrProcessIdentityChanged
		}
		byPID[identity.PID] = identity
	}
	return nil
}

// Snapshot returns a sorted, duplicate-free snapshot of every process whose
// executable identity matches the observer's expected immutable identity.
func (o Observer) Snapshot() ([]ProcessIdentity, error) {
	return o.SnapshotContext(context.Background())
}

// SnapshotContext is the bounded form used by production lifecycle code. It
// carries the caller's cumulative deadline into the complete process scan so
// a slow or disappearing /proc population cannot outlive the operation.
func (o Observer) SnapshotContext(ctx context.Context) ([]ProcessIdentity, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	records, err := o.scanRecords(ctx)
	if err != nil {
		return nil, err
	}
	expected, err := o.expectedIdentity()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identities := make([]ProcessIdentity, 0, len(records))
	for _, record := range records {
		if processMatchesExpected(record.Identity, expected) {
			identities = append(identities, record.Identity)
		}
	}
	return normalizeProcessIdentities(identities), nil
}

func baselineIdentityMap(baseline []ProcessIdentity, expected ExecutableIdentity) (map[int]ProcessIdentity, error) {
	identities := normalizeProcessIdentities(baseline)
	result := make(map[int]ProcessIdentity, len(identities))
	for _, identity := range identities {
		if identity.PID <= 0 || identity.StartTime == 0 || !identity.executable().valid() {
			return nil, errors.New("baseline process identity is invalid")
		}
		if !identity.executable().equal(expected) {
			return nil, errors.New("baseline process identity does not match expected executable")
		}
		if previous, exists := result[identity.PID]; exists {
			if !previous.equal(identity) {
				return nil, ErrProcessIdentityChanged
			}
			continue
		}
		result[identity.PID] = identity
	}
	return result, nil
}

func baselineMismatch(records []ProcessRecord, baseline map[int]ProcessIdentity) error {
	for _, record := range records {
		previous, ok := baseline[record.Identity.PID]
		if !ok {
			continue
		}
		if previous.StartTime != record.Identity.StartTime || !previous.executable().equal(record.Identity.executable()) {
			return ErrProcessIdentityChanged
		}
	}
	return nil
}

// WaitStableAbsent performs a complete process-population rescan on every
// poll.  A matching process resets the absence timer; a reused baseline PID
// or changed executable fails closed.  The successful return is therefore a
// continuous interval with no expected process, not a single empty sample.
func (o Observer) WaitStableAbsent(ctx context.Context, baseline []ProcessIdentity, stable time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if stable < 0 {
		return errors.New("stable absence duration is negative")
	}
	expected, err := o.expectedIdentity()
	if err != nil {
		return err
	}
	baselineByPID, err := baselineIdentityMap(baseline, expected)
	if err != nil {
		return err
	}
	clock := o.clock()
	absenceStarted := false
	var absenceAt time.Time
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		records, err := o.scanRecords(ctx)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := baselineMismatch(records, baselineByPID); err != nil {
			return err
		}
		now := clock.Now()
		if err := ctx.Err(); err != nil {
			return err
		}
		current := make([]ProcessIdentity, 0, len(records))
		expected := o.expected()
		for _, record := range records {
			if processMatchesExpected(record.Identity, expected) {
				current = append(current, record.Identity)
			}
		}
		if len(current) != 0 {
			absenceStarted = false
		} else if !absenceStarted {
			absenceStarted = true
			absenceAt = now
		}
		if absenceStarted && now.Sub(absenceAt) >= stable {
			if err := ctx.Err(); err != nil {
				return err
			}
			return nil
		}

		wait := o.pollInterval()
		if absenceStarted {
			remaining := stable - now.Sub(absenceAt)
			if remaining > 0 && remaining < wait {
				wait = remaining
			}
		}
		if wait <= 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-clock.After(wait):
		}
	}
}

// parseProcStatStartTime parses the Linux proc stat grammar while allowing
// spaces and parentheses in comm.  The parser itself is platform-neutral so
// non-Linux builds retain the same fail-closed testable boundary even though
// the production scanner is unavailable there.
func parseProcStatStartTime(raw []byte) (startTime uint64, pid int, err error) {
	startTime, pid, _, _, err = parseProcStatFields(raw)
	return startTime, pid, err
}

func parseProcStat(raw []byte) (startTime uint64, pid int, state byte, err error) {
	startTime, pid, state, _, err = parseProcStatFields(raw)
	return startTime, pid, state, err
}

func parseProcStatFields(raw []byte) (startTime uint64, pid int, state byte, flags uint64, err error) {
	line := strings.TrimSpace(string(raw))
	open := strings.IndexByte(line, '(')
	if open <= 0 {
		return 0, 0, 0, 0, errors.New("process stat has no comm")
	}
	parsedPID, parseErr := strconv.Atoi(strings.TrimSpace(line[:open]))
	if parseErr != nil || parsedPID <= 0 {
		return 0, 0, 0, 0, errors.New("process stat PID is invalid")
	}
	close := strings.LastIndexByte(line, ')')
	if close <= open || close+1 >= len(line) {
		return 0, 0, 0, 0, errors.New("process stat comm is unterminated")
	}
	rest := strings.TrimSpace(line[close+1:])
	fields := strings.Fields(rest)
	// rest starts at field 3 (state), and starttime is field 22, index 19.
	if len(fields) <= 19 || len(fields[0]) != 1 {
		return 0, 0, 0, 0, errors.New("process stat has insufficient fields")
	}
	start, parseErr := strconv.ParseUint(fields[19], 10, 64)
	if parseErr != nil {
		return 0, 0, 0, 0, errors.New("process stat start time is invalid")
	}
	parsedFlags, parseErr := strconv.ParseUint(fields[6], 10, 64)
	if parseErr != nil {
		return 0, 0, 0, 0, errors.New("process stat flags are invalid")
	}
	return start, parsedPID, fields[0][0], parsedFlags, nil
}
