package kitsoftware

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	ServiceRestartRetention  = 30 * 24 * time.Hour
	ServiceRestartMaxResults = 256
	serviceRestartLedgerName = "service-restarts.v1.json"
)

var serviceRestartIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type DiagnosticSessionExpectation struct {
	TargetID   string `json:"target_id,omitempty"`
	PackageID  string `json:"package_id,omitempty"`
	Generation uint64 `json:"generation,omitempty"`
}

// ServiceRestartRequest is the complete operator intent. It contains no PID,
// executable path, lease credential, or caller-selected process arguments.
type ServiceRestartRequest struct {
	RequestID string                        `json:"request_id"`
	Service   string                        `json:"service"`
	Session   *DiagnosticSessionExpectation `json:"session,omitempty"`
}

type ServiceProcessIdentity struct {
	Role             string `json:"role"`
	PID              int    `json:"pid"`
	StartTime        string `json:"start_time"`
	ProcessGroupID   int    `json:"process_group_id"`
	Executable       string `json:"executable"`
	ExecutableDevice uint64 `json:"executable_device"`
	ExecutableInode  uint64 `json:"executable_inode"`
	LifetimeMS       int64  `json:"lifetime_ms"`
}

// ServiceRestartSession is a non-secret snapshot of runtime and execution
// ownership. Runtime generation and lease generation are deliberately separate.
type ServiceRestartSession struct {
	TargetID          string `json:"target_id"`
	State             string `json:"state"`
	Execution         string `json:"execution"`
	Core              string `json:"core,omitempty"`
	PackageID         string `json:"package_id,omitempty"`
	RuntimeGeneration uint64 `json:"runtime_generation,omitempty"`
	LeaseState        string `json:"lease_state"`
	LeaseGeneration   string `json:"lease_generation,omitempty"`
	LeaseOwner        string `json:"lease_owner,omitempty"`
	LeasePurpose      string `json:"lease_purpose,omitempty"`
	MaintenanceState  string `json:"maintenance_state"`
	RawIdleReady      bool   `json:"raw_idle_ready"`
	UpdateBlocked     bool   `json:"update_blocked"`
	SoftwareBundleID  string `json:"software_bundle_id"`
}

type ServiceRestartRecord struct {
	Request             ServiceRestartRequest   `json:"request"`
	Phase               string                  `json:"phase"`
	Outcome             string                  `json:"outcome,omitempty"`
	Reason              string                  `json:"reason,omitempty"`
	BundleID            string                  `json:"bundle_id,omitempty"`
	Old                 *ServiceProcessIdentity `json:"old_identity,omitempty"`
	Replacement         *ServiceProcessIdentity `json:"replacement_identity,omitempty"`
	SessionBefore       *ServiceRestartSession  `json:"session_before,omitempty"`
	SessionAfter        *ServiceRestartSession  `json:"session_after,omitempty"`
	StopResult          string                  `json:"stop_result,omitempty"`
	ReplacementStart    string                  `json:"replacement_start,omitempty"`
	ReadinessProbe      string                  `json:"readiness_probe,omitempty"`
	ReadinessResult     string                  `json:"readiness_result,omitempty"`
	SiblingIdentitiesOK bool                    `json:"sibling_identities_unchanged,omitempty"`
	ElapsedMS           int64                   `json:"elapsed_ms,omitempty"`
	CreatedAt           time.Time               `json:"created_at"`
	UpdatedAt           time.Time               `json:"updated_at"`
	FinishedAt          *time.Time              `json:"finished_at,omitempty"`
}

type serviceRestartLedgerFile struct {
	Version int                    `json:"version"`
	Records []ServiceRestartRecord `json:"records"`
}

// ServiceRestartLedger keeps a bounded, durable idempotency/result window on
// the verified persistent control filesystem. An accepted but unfinished
// record is never replayed: OpenServiceRestartLedger reconciles it to failure.
type ServiceRestartLedger struct {
	path string
	mu   sync.Mutex
	Now  func() time.Time
}

func OpenServiceRestartLedger(root string) (*ServiceRestartLedger, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("service restart ledger directory must be a private directory")
	}
	if os.Geteuid() == 0 {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return nil, errors.New("service restart ledger directory is not root-owned")
		}
	}
	ledger := &ServiceRestartLedger{path: filepath.Join(root, serviceRestartLedgerName), Now: time.Now}
	if _, err := ledger.read(); errors.Is(err, os.ErrNotExist) {
		if err := ledger.write(serviceRestartLedgerFile{Version: 1, Records: []ServiceRestartRecord{}}); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := ledger.reconcileInterrupted(); err != nil {
		return nil, err
	}
	return ledger, nil
}

func (l *ServiceRestartLedger) Lookup(requestID string) (ServiceRestartRecord, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := l.read()
	if err != nil {
		return ServiceRestartRecord{}, false, err
	}
	oldLength := len(file.Records)
	pruneServiceRestartRecords(&file, l.now())
	if len(file.Records) != oldLength {
		if err := l.write(file); err != nil {
			return ServiceRestartRecord{}, false, err
		}
	}
	for _, record := range file.Records {
		if record.Request.RequestID == requestID {
			return record, true, nil
		}
	}
	return ServiceRestartRecord{}, false, nil
}

func (l *ServiceRestartLedger) Save(record ServiceRestartRecord) error {
	if !serviceRestartIDPattern.MatchString(record.Request.RequestID) {
		return errors.New("invalid service restart request ID")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := l.read()
	if err != nil {
		return err
	}
	replaced := false
	for index := range file.Records {
		if file.Records[index].Request.RequestID == record.Request.RequestID {
			if !sameServiceRestartRequest(file.Records[index].Request, record.Request) {
				return errors.New("service restart request ID conflicts with a stored request")
			}
			if file.Records[index].Outcome != "" {
				return errors.New("completed service restart result is immutable")
			}
			file.Records[index] = record
			replaced = true
			break
		}
	}
	if !replaced {
		file.Records = append(file.Records, record)
	}
	pruneServiceRestartRecords(&file, l.now())
	return l.write(file)
}

func sameServiceRestartRequest(left, right ServiceRestartRequest) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftBytes) == string(rightBytes)
}

func (l *ServiceRestartLedger) reconcileInterrupted() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := l.read()
	if err != nil {
		return err
	}
	now := l.now()
	oldLength := len(file.Records)
	changed := false
	for index := range file.Records {
		record := &file.Records[index]
		if record.Outcome != "" {
			continue
		}
		record.Phase = "interrupted"
		record.Outcome = "failed"
		record.Reason = "supervisor restarted during named-service restart; request was not replayed"
		record.ReadinessResult = "not-established"
		record.UpdatedAt = now
		record.FinishedAt = &now
		logServiceRestartRecord(*record)
		changed = true
	}
	pruneServiceRestartRecords(&file, now)
	if changed || len(file.Records) != oldLength || len(file.Records) == 0 {
		return l.write(file)
	}
	return nil
}

func (l *ServiceRestartLedger) read() (serviceRestartLedgerFile, error) {
	var file serviceRestartLedgerFile
	fd, err := unix.Open(l.path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return file, err
	}
	f := os.NewFile(uintptr(fd), l.path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return file, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4<<20 {
		return file, errors.New("invalid service restart ledger file")
	}
	if os.Geteuid() == 0 {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return file, errors.New("service restart ledger file is not root-owned")
		}
	}
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return file, fmt.Errorf("read service restart ledger: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return file, errors.New("service restart ledger has trailing data")
	}
	if file.Version != 1 || len(file.Records) > ServiceRestartMaxResults {
		return file, errors.New("unsupported or oversized service restart ledger")
	}
	for _, record := range file.Records {
		if !serviceRestartIDPattern.MatchString(record.Request.RequestID) || record.Phase == "" || record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
			return file, errors.New("invalid service restart ledger record")
		}
	}
	return file, nil
}

func (l *ServiceRestartLedger) write(file serviceRestartLedgerFile) error {
	data, err := json.Marshal(file)
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("service restart ledger exceeds size limit")
	}
	openTemp := func() (*os.File, error) {
		fd, openErr := unix.Open(l.path+".tmp", unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if openErr != nil {
			return nil, openErr
		}
		return os.NewFile(uintptr(fd), l.path+".tmp"), nil
	}
	tmp, err := openTemp()
	if errors.Is(err, os.ErrExist) {
		if removeErr := os.Remove(l.path + ".tmp"); removeErr != nil {
			return removeErr
		}
		tmp, err = openTemp()
	}
	if err != nil {
		return err
	}
	_, writeErr := tmp.Write(data)
	err = errors.Join(writeErr, tmp.Sync(), tmp.Close())
	if err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), l.path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	dir, err := os.Open(filepath.Dir(l.path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (l *ServiceRestartLedger) now() time.Time {
	if l.Now == nil {
		return time.Now().UTC()
	}
	return l.Now().UTC()
}

func pruneServiceRestartRecords(file *serviceRestartLedgerFile, now time.Time) {
	kept := file.Records[:0]
	for _, record := range file.Records {
		if record.Outcome == "" || now.Sub(record.UpdatedAt) <= ServiceRestartRetention {
			kept = append(kept, record)
		}
	}
	file.Records = kept
	if len(file.Records) <= ServiceRestartMaxResults {
		return
	}
	sort.SliceStable(file.Records, func(i, j int) bool {
		return file.Records[i].UpdatedAt.Before(file.Records[j].UpdatedAt)
	})
	for len(file.Records) > ServiceRestartMaxResults {
		index := -1
		for i, record := range file.Records {
			if record.Outcome != "" {
				index = i
				break
			}
		}
		if index < 0 {
			break
		}
		file.Records = append(file.Records[:index], file.Records[index+1:]...)
	}
}
