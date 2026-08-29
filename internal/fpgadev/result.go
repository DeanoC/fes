package fpgadev

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf8"
)

const (
	ResultSchemaVersion uint64 = 1
	ResultDirectory            = "/var/lib/fogcast/fpga-dev/results"
	ResultMaxPayload           = 256
	ResultModeUpdating         = "updating"
	ResultExperiment           = ManifestExperiment
	emptySHA256                = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// EmptyPayloadSHA256 is the canonical digest for a failed run that accepted
// no DATA bytes.
const EmptyPayloadSHA256 = emptySHA256

type Code string

const (
	CodeOK                         Code = "ok"
	CodeDesignationFailed          Code = "designation_failed"
	CodeProfileDisabled            Code = "profile_disabled"
	CodePrivilegeRequired          Code = "privilege_required"
	CodeManifestRejected           Code = "manifest_rejected"
	CodeResultConflict             Code = "result_conflict"
	CodeOwnershipConflict          Code = "ownership_conflict"
	CodeGenerationFailed           Code = "generation_failed"
	CodeStateStoreFailed           Code = "state_store_failed"
	CodeLoadDispatchFailed         Code = "load_dispatch_failed"
	CodeMainHandoffTimeout         Code = "main_handoff_timeout"
	CodeNoOwnerQualificationFailed Code = "no_owner_qualification_failed"
	CodeMMIOFailed                 Code = "mmio_failed"
	CodeProtocolViolation          Code = "protocol_violation"
	CodeMessageTimeout             Code = "message_timeout"
	CodePayloadMismatch            Code = "payload_mismatch"
	CodeRebootRequestFailed        Code = "reboot_request_failed"
	CodeReconnectFailed            Code = "reconnect_failed"
	CodeReadinessFailed            Code = "readiness_failed"
)

const (
	ResultPhaseIntentCommitted = "intent_committed"
	ResultPhaseLoadAttempted   = "load_attempted"
	ResultPhaseMainAbsent      = "main_absent"
	ResultPhaseLeaseActive     = "lease_active"
	ResultPhaseHelloObserved   = "hello_observed"
	ResultPhaseMessagePartial  = "message_partial"
	ResultPhaseEndAckWritten   = "end_ack_written"
	ResultPhaseDoneObserved    = "done_observed"
)

// Short aliases make the schema enums convenient to use without duplicating
// string literals in lifecycle code.
const (
	PhaseIntentCommitted = ResultPhaseIntentCommitted
	PhaseLoadAttempted   = ResultPhaseLoadAttempted
	PhaseMainAbsent      = ResultPhaseMainAbsent
	PhaseLeaseActive     = ResultPhaseLeaseActive
	PhaseHelloObserved   = ResultPhaseHelloObserved
	PhaseMessagePartial  = ResultPhaseMessagePartial
	PhaseEndAckWritten   = ResultPhaseEndAckWritten
	PhaseDoneObserved    = ResultPhaseDoneObserved
)

var (
	resultRunIDPattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	resultHashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	resultCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	resultHex8Pattern   = regexp.MustCompile(`^[0-9a-f]{8}$`)
)

var resultFailureCodes = map[Code]struct{}{
	CodeOK:                 {},
	CodeLoadDispatchFailed: {}, CodeMainHandoffTimeout: {},
	CodeNoOwnerQualificationFailed: {}, CodeStateStoreFailed: {},
	CodeMMIOFailed: {}, CodeProtocolViolation: {}, CodeMessageTimeout: {},
	CodePayloadMismatch: {},
}

var preflightCodes = map[Code]struct{}{
	CodeOK: {}, CodeDesignationFailed: {}, CodeProfileDisabled: {},
	CodePrivilegeRequired: {}, CodeManifestRejected: {}, CodeResultConflict: {},
	CodeOwnershipConflict: {},
}

var runPreIntentCodes = map[Code]struct{}{
	CodeDesignationFailed: {}, CodeProfileDisabled: {},
	CodePrivilegeRequired: {}, CodeManifestRejected: {}, CodeResultConflict: {},
	CodeOwnershipConflict: {}, CodeGenerationFailed: {}, CodeStateStoreFailed: {},
}

var resultPhases = map[string]struct{}{
	ResultPhaseIntentCommitted: {}, ResultPhaseLoadAttempted: {},
	ResultPhaseMainAbsent: {}, ResultPhaseLeaseActive: {},
	ResultPhaseHelloObserved: {}, ResultPhaseMessagePartial: {},
	ResultPhaseEndAckWritten: {}, ResultPhaseDoneObserved: {},
}

var resultFields = [...]string{
	"schema",
	"run_id",
	"generation",
	"session",
	"mode",
	"experiment",
	"build_lane",
	"artifact_sha256",
	"source_commit",
	"phase",
	"primary_code",
	"primary_detail",
	"payload_hex",
	"payload_length",
	"payload_sha256",
	"terminal_word",
	"recovery_request",
	"elapsed_ms",
}

// Result is the exact schema-v1 durable development outcome. Field order is
// part of its canonical persistence format and follows this declaration.
type Result struct {
	Schema          uint64 `json:"schema"`
	RunID           string `json:"run_id"`
	Generation      uint64 `json:"generation"`
	Session         string `json:"session"`
	Mode            string `json:"mode"`
	Experiment      string `json:"experiment"`
	BuildLane       string `json:"build_lane"`
	ArtifactSHA256  string `json:"artifact_sha256"`
	SourceCommit    string `json:"source_commit"`
	Phase           string `json:"phase"`
	PrimaryCode     string `json:"primary_code"`
	PrimaryDetail   string `json:"primary_detail"`
	PayloadHex      string `json:"payload_hex"`
	PayloadLength   uint64 `json:"payload_length"`
	PayloadSHA256   string `json:"payload_sha256"`
	TerminalWord    string `json:"terminal_word"`
	RecoveryRequest string `json:"recovery_request"`
	ElapsedMS       uint64 `json:"elapsed_ms"`
}

// Validate checks enums, immutable bindings, payload accounting, and the
// success terminal contract. It deliberately does not normalize any value.
func (r Result) Validate() error {
	if r.Schema != ResultSchemaVersion {
		return fmt.Errorf("result schema must be %d", ResultSchemaVersion)
	}
	if !resultRunIDPattern.MatchString(r.RunID) {
		return errors.New("result run_id must be 32 lowercase hexadecimal characters")
	}
	if r.Generation == 0 {
		return errors.New("result generation must be nonzero")
	}
	if !resultRunIDPattern.MatchString(r.Session) {
		return errors.New("result session must be 32 lowercase hexadecimal characters")
	}
	if r.Mode != ResultModeUpdating {
		return errors.New("result mode must be updating")
	}
	if r.Experiment != ResultExperiment {
		return fmt.Errorf("result experiment must be %q", ResultExperiment)
	}
	if r.BuildLane != BuildLaneOSS && r.BuildLane != BuildLaneOracle {
		return errors.New("result build_lane must be oss or oracle")
	}
	if !resultHashPattern.MatchString(r.ArtifactSHA256) {
		return errors.New("result artifact_sha256 must be 64 lowercase hexadecimal characters")
	}
	if !resultCommitPattern.MatchString(r.SourceCommit) {
		return errors.New("result source_commit must be 40 lowercase hexadecimal characters")
	}
	if _, ok := resultPhases[r.Phase]; !ok {
		return fmt.Errorf("unknown result phase %q", r.Phase)
	}
	code := Code(r.PrimaryCode)
	if _, ok := resultFailureCodes[code]; !ok {
		return fmt.Errorf("unknown result primary_code %q", r.PrimaryCode)
	}
	if !safeResultDetail(r.PrimaryDetail) {
		return errors.New("result primary_detail is unsafe or too long")
	}
	if code == CodeOK {
		if r.PrimaryDetail != "" {
			return errors.New("successful result must have empty primary_detail")
		}
	} else if r.PrimaryDetail == "" {
		return errors.New("failed result must have a primary_detail")
	}
	if r.RecoveryRequest != "pending" && r.RecoveryRequest != "failed" {
		return errors.New("result recovery_request must be pending or failed")
	}
	if r.PayloadHex == "" {
		if r.PayloadLength != 0 || r.PayloadSHA256 != emptySHA256 {
			return errors.New("empty result payload accounting is inconsistent")
		}
	} else {
		if len(r.PayloadHex)%2 != 0 || len(r.PayloadHex) > ResultMaxPayload*2 || !resultHashPattern.MatchString(r.PayloadSHA256) {
			return errors.New("result payload encoding is invalid")
		}
		for _, character := range r.PayloadHex {
			if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
				return errors.New("result payload_hex must be lowercase hexadecimal")
			}
		}
		payload, err := hex.DecodeString(r.PayloadHex)
		if err != nil || uint64(len(payload)) != r.PayloadLength {
			return errors.New("result payload length is inconsistent")
		}
		digest := sha256.Sum256(payload)
		if hex.EncodeToString(digest[:]) != r.PayloadSHA256 {
			return errors.New("result payload hash is inconsistent")
		}
	}
	if r.PayloadLength > ResultMaxPayload {
		return errors.New("result payload exceeds 256 bytes")
	}
	if !resultHex8Pattern.MatchString(r.TerminalWord) {
		return errors.New("result terminal_word must be eight lowercase hexadecimal characters")
	}
	if r.Phase == ResultPhaseDoneObserved && r.TerminalWord != "d3130c00" {
		return errors.New("done_observed result must retain the protocol-v1 DONE word")
	}
	if r.Phase != ResultPhaseDoneObserved && r.TerminalWord != "00000000" {
		return errors.New("terminal_word is present before stable DONE")
	}
	if r.TerminalWord != "00000000" && r.TerminalWord != "d3130c00" {
		return errors.New("result terminal_word is not the protocol-v1 DONE word")
	}
	if code == CodeOK {
		payload, _ := hex.DecodeString(r.PayloadHex)
		if r.Phase != ResultPhaseDoneObserved || string(payload) != "OSS FPGA OK\n" || r.PayloadLength != 12 || r.TerminalWord != "d3130c00" {
			return errors.New("successful result does not describe the exact mailbox payload")
		}
	}
	return nil
}

// MarshalCanonical emits the exact field order and one trailing newline used
// for durable results.
func (r Result) MarshalCanonical() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return append(raw, '\n'), nil
}

// ParseResult accepts only a canonical durable result. Unlike a generic JSON
// map decoder it identifies duplicate keys and rejects field-order drift.
func ParseResult(raw []byte) (Result, error) {
	var result Result
	if err := decodeStrictObject(raw, resultFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &result.Schema)
		case "run_id":
			return decodeJSONString(value, &result.RunID)
		case "generation":
			return decodeJSONUint(value, &result.Generation)
		case "session":
			return decodeJSONString(value, &result.Session)
		case "mode":
			return decodeJSONString(value, &result.Mode)
		case "experiment":
			return decodeJSONString(value, &result.Experiment)
		case "build_lane":
			return decodeJSONString(value, &result.BuildLane)
		case "artifact_sha256":
			return decodeJSONString(value, &result.ArtifactSHA256)
		case "source_commit":
			return decodeJSONString(value, &result.SourceCommit)
		case "phase":
			return decodeJSONString(value, &result.Phase)
		case "primary_code":
			return decodeJSONString(value, &result.PrimaryCode)
		case "primary_detail":
			return decodeJSONString(value, &result.PrimaryDetail)
		case "payload_hex":
			return decodeJSONString(value, &result.PayloadHex)
		case "payload_length":
			return decodeJSONUint(value, &result.PayloadLength)
		case "payload_sha256":
			return decodeJSONString(value, &result.PayloadSHA256)
		case "terminal_word":
			return decodeJSONString(value, &result.TerminalWord)
		case "recovery_request":
			return decodeJSONString(value, &result.RecoveryRequest)
		case "elapsed_ms":
			return decodeJSONUint(value, &result.ElapsedMS)
		default:
			return fmt.Errorf("unknown result field %q", key)
		}
	}); err != nil {
		return Result{}, err
	}
	if err := result.Validate(); err != nil {
		return Result{}, err
	}
	canonical, err := result.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return Result{}, errors.New("result JSON is not canonical")
	}
	return result, nil
}

// DecodeResult is a descriptive alias for ParseResult.
func DecodeResult(raw []byte) (Result, error) { return ParseResult(raw) }

// PreflightLine formats the only preflight stdout/stderr grammar. Invalid
// codes are rendered as a safe sentinel rather than allowing control bytes.
func PreflightLine(code Code) string {
	if _, ok := preflightCodes[code]; !ok {
		return ""
	}
	return "FOGCAST_FPGA_DEV_PREFLIGHT code=" + string(code) + "\n"
}

// RunLine formats the separate pre-intent run grammar. Post-intent result
// codes are deliberately excluded because they require a durable result.
func RunLine(code Code) string {
	if _, ok := runPreIntentCodes[code]; !ok {
		return ""
	}
	return "FOGCAST_FPGA_DEV_RUN code=" + string(code) + "\n"
}

// ResultLine formats the post-intent machine-readable result line. It does
// not include the human payload line, which is emitted only for success.
func ResultLine(result Result) string {
	if err := result.Validate(); err != nil {
		return ""
	}
	return "FOGCAST_FPGA_DEV_RESULT run_id=" + result.RunID + " primary=" + result.PrimaryCode + "\n"
}

// SuccessOutput is the exact two-line stdout produced after a successful
// mailbox transaction.
func SuccessOutput(result Result) string {
	if err := result.Validate(); err != nil || result.PrimaryCode != string(CodeOK) {
		return ""
	}
	return "FPGA> OSS FPGA OK\n" + ResultLine(result)
}

func safeResultDetail(value string) bool {
	if len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character == '/' || character == '\\' || unicode.IsControl(character) {
			return false
		}
	}
	return true
}

// ResultStore persists canonical result files below one root-owned directory.
// The function seams are exported so fault-injection tests can prove failure
// preservation without replacing the real filesystem.
type ResultStore struct {
	// Dir is the result directory. Path is accepted as a compatibility alias;
	// when it names a .json file, its parent is used and that exact file is used
	// only when its basename matches the validated run ID.
	Dir         string
	Path        string
	Root        string
	ExpectedUID uint32

	SyncFile                 func(*os.File) error
	SyncParent               func(*os.File) error
	Link                     func(string, string) error
	Rename                   func(string, string) error
	BeforeConditionalReplace func()
	BeforeFinalReplace       func()
	BeforeAtomicExchange     func()
	BeforeFinalCreate        func()
	AfterCreateLink          func()
	BeforeDirectoryOpen      func()
	BeforeMarkLock           func()
	Exchange                 func(string, string) error
	Unlink                   func(string) error

	// Unexported aliases match the repository's existing fault-injection style
	// for in-package tests while the exported seams remain usable externally.
	syncFile                 func(*os.File) error
	syncParent               func(*os.File) error
	link                     func(string, string) error
	rename                   func(string, string) error
	beforeConditionalReplace func()
	beforeFinalReplace       func()
	beforeAtomicExchange     func()
	beforeFinalCreate        func()
	afterCreateLink          func()
	beforeDirectoryOpen      func()
	beforeMarkLock           func()
	exchange                 func(string, string) error
	unlink                   func(string) error

	mu sync.Mutex
}

type resultDirectoryHandle struct {
	file *os.File
}

type resultIdentity struct {
	device uint64
	inode  uint64
	size   int64
}

// resultEntrySnapshot preserves a displaced directory entry even when it is
// not a canonical readable result. Type, mode, ownership, link count, device,
// inode, and symlink target cover special entries; regular files additionally
// retain their exact bytes.
type resultEntrySnapshot struct {
	device, inode uint64
	size          int64
	mode, uid     uint32
	gid           uint32
	nlink         uint64
	rdev          uint64
	kind          uint32
	target        string
	targetKnown   bool
	raw           []byte
	rawKnown      bool
}

const (
	resultEntryRegularKind uint32 = 0o100000
	resultEntrySymlinkKind uint32 = 0o120000
)

func (d *resultDirectoryHandle) close() error {
	if d == nil || d.file == nil {
		return nil
	}
	return d.file.Close()
}

// NewResultStore constructs a store rooted at dir. Omitting expectedUID
// enforces root ownership, as required by the target profile.
func NewResultStore(dir string, expectedUID ...uint32) *ResultStore {
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	return &ResultStore{Dir: dir, ExpectedUID: uid}
}

// NewProductionResultStore returns the target-profile result store.
func NewProductionResultStore() *ResultStore {
	return NewResultStore(ResultDirectory)
}

func (s *ResultStore) resultDirectory() string {
	if s == nil {
		return ""
	}
	if s.Dir != "" {
		return s.Dir
	}
	if s.Root != "" {
		return s.Root
	}
	if s.Path != "" && strings.HasSuffix(s.Path, ".json") {
		return filepath.Dir(s.Path)
	}
	if s.Path != "" {
		return s.Path
	}
	return ResultDirectory
}

func (s *ResultStore) openResultDirectory() (*resultDirectoryHandle, error) {
	hook := s.beforeDirectoryOpen
	if hook == nil {
		hook = s.BeforeDirectoryOpen
	}
	return openResultDirectory(s.resultDirectory(), s.ExpectedUID, hook)
}

// PathFor derives the only accepted result filename. Invalid IDs return an
// empty string so callers cannot accidentally construct an escaping path.
func (s *ResultStore) PathFor(runID string) string {
	if s == nil || !resultRunIDPattern.MatchString(runID) {
		return ""
	}
	if s.Dir == "" && s.Path != "" && strings.HasSuffix(s.Path, ".json") && filepath.Base(s.Path) == "result-"+runID+".json" {
		return s.Path
	}
	return filepath.Join(s.resultDirectory(), "result-"+runID+".json")
}

// ResultPath is an alias for PathFor.
func (s *ResultStore) ResultPath(runID string) string { return s.PathFor(runID) }

// Create exclusively publishes one canonical result and never overwrites a
// collision. A failed first publication removes its unpublished candidate.
const resultMaxBytes = 64 << 10

func (s *ResultStore) Create(result Result) (returnErr error) {
	if s == nil {
		return errors.New("nil result store")
	}
	if !resultPersistenceSupported() {
		return ErrUnsupported
	}
	if err := result.Validate(); err != nil {
		returnErr = fmt.Errorf("validate result: %w", err)
		return returnErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDirectory(); err != nil {
		returnErr = err
		return returnErr
	}
	directory, err := s.openResultDirectory()
	if err != nil {
		returnErr = err
		return returnErr
	}
	defer func() {
		if closeErr := directory.close(); returnErr == nil && closeErr != nil {
			returnErr = fmt.Errorf("close result directory: %w", closeErr)
		}
	}()
	if err := lockResultDirectory(directory); err != nil {
		returnErr = fmt.Errorf("lock result directory for create: %w", err)
		return returnErr
	}
	defer func() {
		if err := unlockResultDirectory(directory); err != nil {
			returnErr = combineResultErrors(returnErr, err)
		}
	}()
	returnErr = s.createAt(directory, result)
	return returnErr
}

func (s *ResultStore) createAt(directory *resultDirectoryHandle, result Result) error {
	name := resultFileName(result.RunID)
	existing, err := openResultFile(directory, name)
	if err == nil {
		closeErr := existing.Close()
		if closeErr != nil {
			return fmt.Errorf("close result collision descriptor: %w", closeErr)
		}
		return errors.New("result already exists")
	}
	if !isResultNotExist(err) {
		return fmt.Errorf("inspect result collision: %w", err)
	}
	canonical, err := result.MarshalCanonical()
	if err != nil {
		return err
	}
	temporary, temporaryName, err := s.createTemporary(directory, canonical, "create")
	if err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		return combineResultFailure(fmt.Errorf("close result temporary: %w", err), cleanupErr)
	}
	if hook := s.beforeFinalCreate; hook != nil {
		hook()
	} else if s.BeforeFinalCreate != nil {
		s.BeforeFinalCreate()
	}
	if err := s.linkAt(directory, temporaryName, name); err != nil {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		return combineResultFailure(fmt.Errorf("publish result exclusively: %w", err), cleanupErr)
	}
	publishedEntry, err := snapshotResultEntry(directory, temporaryName)
	if err != nil || publishedEntry.kind != resultEntryRegularKind || !publishedEntry.rawKnown || !bytes.Equal(publishedEntry.raw, canonical) {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		if err == nil {
			err = errors.New("published candidate identity or bytes are not canonical")
		}
		return combineResultFailure(fmt.Errorf("capture published result identity: %w", err), cleanupErr)
	}
	publishedIdentity := resultIdentity{device: publishedEntry.device, inode: publishedEntry.inode, size: publishedEntry.size}
	publishedBytes := append([]byte(nil), publishedEntry.raw...)
	if hook := s.afterCreateLink; hook != nil {
		hook()
	} else if s.AfterCreateLink != nil {
		s.AfterCreateLink()
	}
	if err := s.unlinkAt(directory, temporaryName); err != nil {
		removeErr := s.removePublishedEntryAndSync(directory, name, publishedIdentity, publishedBytes)
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		cleanupErr = combineResultErrors(removeErr, cleanupErr)
		return combineResultFailure(fmt.Errorf("remove result temporary: %w", err), cleanupErr)
	}
	if err := s.syncDirectory(directory); err != nil {
		rollbackErr := s.removePublishedEntryAndSync(directory, name, publishedIdentity, publishedBytes)
		return combineResultFailure(fmt.Errorf("fsync result directory: %w", err), rollbackErr)
	}
	stored, _, storedBytes, err := s.readResultEntryAt(directory, name)
	if err != nil {
		rollbackErr := s.removePublishedEntryAndSync(directory, name, publishedIdentity, publishedBytes)
		return combineResultFailure(fmt.Errorf("validate published result: %w", err), rollbackErr)
	}
	if stored != result || !bytes.Equal(storedBytes, canonical) {
		rollbackErr := s.removePublishedEntryAndSync(directory, name, publishedIdentity, publishedBytes)
		return combineResultFailure(errors.New("published result differs from canonical candidate"), rollbackErr)
	}
	return nil
}

func (s *ResultStore) Load(runID string) (result Result, returnErr error) {
	if s == nil {
		return Result{}, errors.New("nil result store")
	}
	if !resultPersistenceSupported() {
		return Result{}, ErrUnsupported
	}
	if !resultRunIDPattern.MatchString(runID) {
		return Result{}, errors.New("invalid result run_id")
	}
	directory, err := s.openResultDirectory()
	if err != nil {
		return Result{}, err
	}
	defer func() {
		if closeErr := directory.close(); returnErr == nil && closeErr != nil {
			returnErr = fmt.Errorf("close result directory: %w", closeErr)
		}
	}()
	result, _, _, returnErr = s.readCurrentAt(directory, runID)
	return result, returnErr
}

func (s *ResultStore) Read(runID string) (Result, error) { return s.Load(runID) }

func (s *ResultStore) MarkRecoveryFailed(expected Result) (returnErr error) {
	if s == nil {
		return errors.New("nil result store")
	}
	if !resultPersistenceSupported() {
		return ErrUnsupported
	}
	if expected.RecoveryRequest != "pending" {
		return errors.New("expected result must be pending")
	}
	if err := expected.Validate(); err != nil {
		returnErr = fmt.Errorf("validate expected result: %w", err)
		return returnErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDirectory(); err != nil {
		returnErr = err
		return returnErr
	}
	directory, err := s.openResultDirectory()
	if err != nil {
		returnErr = err
		return returnErr
	}
	defer func() {
		if closeErr := directory.close(); returnErr == nil && closeErr != nil {
			returnErr = fmt.Errorf("close result directory: %w", closeErr)
		}
	}()
	if hook := s.beforeMarkLock; hook != nil {
		hook()
	} else if s.BeforeMarkLock != nil {
		s.BeforeMarkLock()
	}
	if err := lockResultDirectory(directory); err != nil {
		returnErr = fmt.Errorf("lock result directory for recovery update: %w", err)
		return returnErr
	}
	defer func() {
		if err := unlockResultDirectory(directory); err != nil {
			returnErr = combineResultErrors(returnErr, err)
		}
	}()
	returnErr = s.markRecoveryFailedAt(directory, expected)
	return returnErr
}

func (s *ResultStore) markRecoveryFailedAt(directory *resultDirectoryHandle, expected Result) (returnErr error) {
	name := resultFileName(expected.RunID)
	current, identity, currentBytes, err := s.readCurrentAt(directory, expected.RunID)
	if err != nil {
		return err
	}
	if current != expected {
		return errors.New("result identity or evidence differs")
	}
	if hook := s.beforeConditionalReplace; hook != nil {
		hook()
	} else if s.BeforeConditionalReplace != nil {
		s.BeforeConditionalReplace()
	}
	current, identityAfter, currentBytesAfter, err := s.readCurrentAt(directory, expected.RunID)
	if err != nil {
		return err
	}
	if current != expected || identityAfter != identity || !bytes.Equal(currentBytesAfter, currentBytes) {
		return errors.New("result was replaced during conditional update")
	}
	updated := expected
	updated.RecoveryRequest = "failed"
	canonical, err := updated.MarshalCanonical()
	if err != nil {
		return err
	}
	temporary, temporaryName, err := s.createTemporary(directory, canonical, "update")
	if err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		return combineResultFailure(fmt.Errorf("close result update temporary: %w", err), cleanupErr)
	}
	_, identityFinal, bytesFinal, err := s.readCurrentAt(directory, expected.RunID)
	if err != nil {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		return combineResultFailure(err, cleanupErr)
	}
	if identityFinal != identity || !bytes.Equal(bytesFinal, currentBytes) {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		return combineResultFailure(errors.New("result was replaced before conditional update"), cleanupErr)
	}
	if hook := s.beforeFinalReplace; hook != nil {
		hook()
	} else if s.BeforeFinalReplace != nil {
		s.BeforeFinalReplace()
	}
	lockedResult, lockedIdentity, lockedBytes, err := s.readCurrentAt(directory, expected.RunID)
	if err != nil {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		return combineResultFailure(fmt.Errorf("recheck result under exchange lock: %w", err), cleanupErr)
	}
	if lockedResult != expected || lockedIdentity != identity || !bytes.Equal(lockedBytes, currentBytes) {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		return combineResultFailure(errors.New("result was replaced under conditional exchange lock"), cleanupErr)
	}
	if hook := s.beforeAtomicExchange; hook != nil {
		hook()
	} else if s.BeforeAtomicExchange != nil {
		s.BeforeAtomicExchange()
	}
	if err := s.exchangeAt(directory, temporaryName, name, true); err != nil {
		cleanupErr := s.cleanupTemporary(directory, temporaryName)
		return combineResultFailure(fmt.Errorf("exchange result: %w", err), cleanupErr)
	}
	swappedResult, swappedIdentity, swappedBytes, swappedErr := s.readResultEntryAt(directory, temporaryName)
	if swappedErr != nil || swappedIdentity != identity || !bytes.Equal(swappedBytes, currentBytes) || swappedResult != expected {
		cause := errors.New("result was replaced during conditional exchange")
		if swappedErr != nil {
			cause = fmt.Errorf("%w: swapped entry: %v", cause, swappedErr)
		}
		restoreErr := s.restoreExchange(directory, temporaryName, name, swappedIdentity, swappedBytes, canonical)
		return combineResultFailure(cause, restoreErr)
	}
	publishedResult, publishedIdentity, publishedBytes, publishedErr := s.readResultEntryAt(directory, name)
	if publishedErr != nil || publishedResult != updated || !bytes.Equal(publishedBytes, canonical) {
		cause := errors.New("published result differs from conditional update")
		if publishedErr != nil {
			cause = fmt.Errorf("%w: %v", cause, publishedErr)
		}
		restoreErr := s.restoreExchange(directory, temporaryName, name, swappedIdentity, swappedBytes, canonical)
		return combineResultFailure(cause, restoreErr)
	}
	if err := s.syncResultEntry(directory, name); err != nil {
		restoreErr := s.restoreExchange(directory, temporaryName, name, swappedIdentity, swappedBytes, canonical)
		return combineResultFailure(fmt.Errorf("fsync published result: %w", err), restoreErr)
	}
	if err := s.unlinkAt(directory, temporaryName); err != nil {
		restoreErr := s.restoreExchange(directory, temporaryName, name, swappedIdentity, swappedBytes, canonical)
		return combineResultFailure(fmt.Errorf("remove old result after exchange: %w", err), restoreErr)
	}
	if err := s.syncDirectory(directory); err != nil {
		restoreErr := s.restorePublished(directory, name, expected, currentBytes, publishedIdentity, publishedBytes)
		return combineResultFailure(fmt.Errorf("fsync result directory: %w", err), restoreErr)
	}
	finalResult, finalIdentity, finalBytes, finalErr := s.readResultEntryAt(directory, name)
	if finalErr != nil || finalResult != updated || !bytes.Equal(finalBytes, canonical) || finalIdentity != publishedIdentity {
		cause := errors.New("conditional result changed after publication")
		if finalErr != nil {
			cause = fmt.Errorf("%w: %v", cause, finalErr)
		}
		restoreErr := s.restorePublished(directory, name, expected, currentBytes, publishedIdentity, publishedBytes)
		return combineResultFailure(cause, restoreErr)
	}
	return nil
}

func (s *ResultStore) readCurrentAt(directory *resultDirectoryHandle, runID string) (Result, resultIdentity, []byte, error) {
	result, identity, raw, err := s.readResultEntryAt(directory, resultFileName(runID))
	if err != nil {
		return Result{}, resultIdentity{}, nil, err
	}
	if result.RunID != runID {
		return Result{}, resultIdentity{}, nil, errors.New("result filename and run_id differ")
	}
	return result, identity, raw, nil
}

func (s *ResultStore) readResultEntryAt(directory *resultDirectoryHandle, name string) (Result, resultIdentity, []byte, error) {
	identity, raw, err := readResultRawAt(directory, name, s.ExpectedUID)
	if err != nil {
		return Result{}, identity, raw, err
	}
	result, err := ParseResult(raw)
	if err != nil {
		return Result{}, identity, raw, fmt.Errorf("parse result: %w", err)
	}
	return result, identity, raw, nil
}

func readResultRawAt(directory *resultDirectoryHandle, name string, expectedUID uint32) (resultIdentity, []byte, error) {
	file, err := openResultFile(directory, name)
	if err != nil {
		return resultIdentity{}, nil, fmt.Errorf("open result: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return resultIdentity{}, nil, fmt.Errorf("stat result: %w", err)
	}
	if err := validateResultFileInfo(info, expectedUID); err != nil {
		_ = file.Close()
		return resultIdentity{}, nil, err
	}
	identity, err := getResultIdentity(info)
	if err != nil {
		_ = file.Close()
		return resultIdentity{}, nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, resultMaxBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return identity, raw, fmt.Errorf("read result: %w", readErr)
	}
	if closeErr != nil {
		return identity, raw, fmt.Errorf("close result: %w", closeErr)
	}
	if len(raw) > resultMaxBytes {
		return identity, raw, errors.New("result exceeds bounded size")
	}
	openedAgain, err := openResultFile(directory, name)
	if err != nil {
		return identity, raw, fmt.Errorf("reopen result for identity check: %w", err)
	}
	afterInfo, err := openedAgain.Stat()
	if err != nil {
		_ = openedAgain.Close()
		return identity, raw, fmt.Errorf("restat result: %w", err)
	}
	afterIdentity, err := getResultIdentity(afterInfo)
	closeErr = openedAgain.Close()
	if err != nil {
		return identity, raw, err
	}
	if closeErr != nil {
		return identity, raw, fmt.Errorf("close result identity descriptor: %w", closeErr)
	}
	if afterIdentity != identity {
		return identity, raw, errors.New("result changed while reading")
	}
	return identity, raw, nil
}

func (s *ResultStore) createTemporary(directory *resultDirectoryHandle, data []byte, operation string) (*os.File, string, error) {
	temporary, name, err := createResultTemp(directory, operation)
	if err != nil {
		return nil, "", err
	}
	cleanup := func(cause error) (*os.File, string, error) {
		closeErr := temporary.Close()
		unlinkErr := unlinkResult(directory, name)
		syncErr := s.syncDirectory(directory)
		cleanupErr := combineResultErrors(closeErr, combineResultErrors(unlinkErr, syncErr))
		return nil, "", combineResultErrors(cause, cleanupErr)
	}
	if err := temporary.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("set result temporary mode: %w", err))
	}
	info, err := temporary.Stat()
	if err != nil {
		return cleanup(fmt.Errorf("stat result temporary: %w", err))
	}
	if err := validateResultFileInfo(info, s.ExpectedUID); err != nil {
		return cleanup(err)
	}
	n, err := temporary.Write(data)
	if err != nil {
		return cleanup(fmt.Errorf("write result temporary: %w", err))
	}
	if n != len(data) {
		return cleanup(io.ErrShortWrite)
	}
	if err := s.syncResultFile(temporary); err != nil {
		return cleanup(fmt.Errorf("fsync result temporary: %w", err))
	}
	return temporary, name, nil
}

func (s *ResultStore) syncResultFile(file *os.File) error {
	if s.SyncFile != nil {
		return s.SyncFile(file)
	}
	if s.syncFile != nil {
		return s.syncFile(file)
	}
	return file.Sync()
}

func (s *ResultStore) syncResultEntry(directory *resultDirectoryHandle, name string) error {
	file, err := openResultFile(directory, name)
	if err != nil {
		return fmt.Errorf("open result for fsync: %w", err)
	}
	if info, statErr := file.Stat(); statErr != nil {
		_ = file.Close()
		return fmt.Errorf("stat result for fsync: %w", statErr)
	} else if validateErr := validateResultFileInfo(info, s.ExpectedUID); validateErr != nil {
		_ = file.Close()
		return validateErr
	}
	syncErr := s.syncResultFile(file)
	closeErr := file.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func (s *ResultStore) syncAnyResultEntry(directory *resultDirectoryHandle, name string) error {
	return syncResultEntryFile(directory, name, s.syncResultFile)
}

func (s *ResultStore) syncDirectory(directory *resultDirectoryHandle) error {
	if directory == nil || directory.file == nil {
		return errors.New("result directory descriptor is unavailable")
	}
	if s.SyncParent != nil {
		return s.SyncParent(directory.file)
	}
	if s.syncParent != nil {
		return s.syncParent(directory.file)
	}
	return directory.file.Sync()
}

func (s *ResultStore) linkAt(directory *resultDirectoryHandle, source, target string) error {
	if s.Link != nil {
		if err := s.Link(s.logicalResultPath(source), s.logicalResultPath(target)); err != nil {
			return err
		}
	} else if s.link != nil {
		if err := s.link(s.logicalResultPath(source), s.logicalResultPath(target)); err != nil {
			return err
		}
	}
	return linkResult(directory, source, target)
}

func (s *ResultStore) exchangeAt(directory *resultDirectoryHandle, source, target string, injectFault bool) error {
	if injectFault {
		if s.Exchange != nil {
			if err := s.Exchange(s.logicalResultPath(source), s.logicalResultPath(target)); err != nil {
				return err
			}
		} else if s.exchange != nil {
			if err := s.exchange(s.logicalResultPath(source), s.logicalResultPath(target)); err != nil {
				return err
			}
		} else if s.Rename != nil {
			if err := s.Rename(s.logicalResultPath(source), s.logicalResultPath(target)); err != nil {
				return err
			}
		} else if s.rename != nil {
			if err := s.rename(s.logicalResultPath(source), s.logicalResultPath(target)); err != nil {
				return err
			}
		}
	}
	return exchangeResult(directory, source, target)
}

func (s *ResultStore) unlinkAt(directory *resultDirectoryHandle, name string) error {
	if s.Unlink != nil {
		if err := s.Unlink(s.logicalResultPath(name)); err != nil {
			return err
		}
	} else if s.unlink != nil {
		if err := s.unlink(s.logicalResultPath(name)); err != nil {
			return err
		}
	}
	return unlinkResult(directory, name)
}

func (s *ResultStore) cleanupTemporary(directory *resultDirectoryHandle, name string) error {
	unlinkErr := s.unlinkAt(directory, name)
	if isResultNotExist(unlinkErr) {
		unlinkErr = nil
	}
	syncErr := s.syncDirectory(directory)
	return combineResultErrors(unlinkErr, syncErr)
}

func (s *ResultStore) removeEntryAndSync(directory *resultDirectoryHandle, name string) error {
	unlinkErr := s.unlinkAt(directory, name)
	if isResultNotExist(unlinkErr) {
		unlinkErr = nil
	}
	syncErr := s.syncDirectory(directory)
	return combineResultErrors(unlinkErr, syncErr)
}

func (s *ResultStore) removePublishedEntryAndSync(directory *resultDirectoryHandle, name string, expectedIdentity resultIdentity, expectedBytes []byte) error {
	if expectedIdentity == (resultIdentity{}) || len(expectedBytes) == 0 {
		return errors.New("published result identity is unavailable; preserving evidence")
	}
	identity, raw, err := readResultRawAt(directory, name, s.ExpectedUID)
	if err != nil {
		return fmt.Errorf("published result changed; preserving evidence: %w", err)
	}
	if identity != expectedIdentity || !bytes.Equal(raw, expectedBytes) {
		return errors.New("published result changed; preserving evidence")
	}
	return s.removeEntryAndSync(directory, name)
}

func (s *ResultStore) restoreExchange(directory *resultDirectoryHandle, temporaryName, finalName string, wantedIdentity resultIdentity, wantedBytes, candidateBytes []byte) error {
	// Capture the displaced entry before exchanging back, but never let an
	// unreadable type prevent the unconditional atomic exchange-back. The
	// snapshot supports regular files, zero-byte files, symlinks, FIFOs, and
	// other special entries without following or blocking on them.
	displaced, displacedErr := snapshotResultEntry(directory, temporaryName)
	if err := s.exchangeAt(directory, temporaryName, finalName, true); err != nil {
		return fmt.Errorf("restoration failed during exchange-back: %w", err)
	}
	restored, restoredErr := snapshotResultEntry(directory, finalName)
	var verificationErr error
	if displacedErr != nil {
		verificationErr = fmt.Errorf("restoration failed while capturing displaced entry: %w", displacedErr)
	} else if restoredErr != nil {
		verificationErr = fmt.Errorf("restoration failed while verifying restored entry: %w", restoredErr)
	} else if !sameResultEntrySnapshot(displaced, restored) {
		verificationErr = errors.New("restoration failed: displaced directory entry changed")
	} else if wantedIdentity != (resultIdentity{}) &&
		(resultIdentity{device: displaced.device, inode: displaced.inode, size: displaced.size} != wantedIdentity ||
			!displaced.rawKnown || !bytes.Equal(displaced.raw, wantedBytes)) {
		verificationErr = errors.New("restoration failed: original result identity or bytes changed")
	}

	// Once the exchange-back has restored the displaced entry at the result
	// name, fsync only regular files; symlinks and special entries are covered
	// by the parent-directory fsync below.
	if verificationErr == nil && restored.kind == resultEntryRegularKind {
		if err := s.syncAnyResultEntry(directory, finalName); err != nil {
			verificationErr = fmt.Errorf("restoration failed to fsync restored result: %w", err)
		}
	}

	// The entry now at temporaryName should be our failed candidate. If a
	// non-cooperating writer replaced the final name around exchange-back, the
	// temporary entry contains that writer's bytes instead. Exchange it back a
	// second time so the writer's entry is retained at the result name before
	// cleaning the displaced pending entry.
	candidate, candidateErr := snapshotResultEntry(directory, temporaryName)
	if candidateErr != nil {
		if verificationErr == nil {
			verificationErr = fmt.Errorf("restoration failed while checking candidate: %w", candidateErr)
		}
	} else if candidate.kind != resultEntryRegularKind || !candidate.rawKnown || !bytes.Equal(candidate.raw, candidateBytes) {
		if err := s.exchangeAt(directory, temporaryName, finalName, true); err != nil {
			if verificationErr == nil {
				verificationErr = fmt.Errorf("restoration failed preserving concurrent replacement: %w", err)
			}
		} else {
			preserved, preservedErr := snapshotResultEntry(directory, finalName)
			if preservedErr != nil || !sameResultEntrySnapshot(candidate, preserved) {
				if verificationErr == nil {
					verificationErr = errors.New("restoration failed: concurrent replacement bytes changed")
				}
			} else if preserved.kind == resultEntryRegularKind {
				if err := s.syncAnyResultEntry(directory, finalName); err != nil && verificationErr == nil {
					verificationErr = fmt.Errorf("restoration failed to fsync concurrent replacement: %w", err)
				}
			}
		}
	}
	cleanupErr := s.cleanupTemporary(directory, temporaryName)
	if verificationErr != nil {
		return combineResultFailure(verificationErr, cleanupErr)
	}
	if cleanupErr != nil {
		return fmt.Errorf("restoration failed while cleaning candidate: %w", cleanupErr)
	}
	return nil
}

func (s *ResultStore) restorePublished(directory *resultDirectoryHandle, finalName string, expected Result, oldBytes []byte, publishedIdentity resultIdentity, publishedBytes []byte) error {
	if publishedIdentity == (resultIdentity{}) || len(oldBytes) == 0 {
		return errors.New("restoration failed: rollback identity or bytes are unavailable")
	}
	currentIdentity, currentBytes, err := readResultRawAt(directory, finalName, s.ExpectedUID)
	if err != nil {
		return fmt.Errorf("restoration failed while checking published result: %w", err)
	}
	if currentIdentity != publishedIdentity || !bytes.Equal(currentBytes, publishedBytes) {
		return errors.New("restoration failed: published result was replaced")
	}
	rollback, rollbackName, err := s.createTemporary(directory, oldBytes, "rollback")
	if err != nil {
		return fmt.Errorf("restoration failed preparing rollback: %w", err)
	}
	if err := rollback.Close(); err != nil {
		cleanupErr := s.cleanupTemporary(directory, rollbackName)
		return combineResultFailure(fmt.Errorf("restoration failed closing rollback: %w", err), cleanupErr)
	}
	if err := s.exchangeAt(directory, rollbackName, finalName, true); err != nil {
		cleanupErr := s.cleanupTemporary(directory, rollbackName)
		return combineResultFailure(fmt.Errorf("restoration failed during exchange: %w", err), cleanupErr)
	}
	swappedIdentity, swappedBytes, err := readResultRawAt(directory, rollbackName, s.ExpectedUID)
	if err != nil {
		return fmt.Errorf("restoration failed verifying swapped result: %w", err)
	}
	if swappedIdentity != publishedIdentity || !bytes.Equal(swappedBytes, publishedBytes) {
		return errors.New("restoration failed: published bytes changed during rollback")
	}
	restoredIdentity, restoredBytes, err := readResultRawAt(directory, finalName, s.ExpectedUID)
	if err != nil {
		return fmt.Errorf("restoration failed reading restored result: %w", err)
	}
	if !bytes.Equal(restoredBytes, oldBytes) {
		return errors.New("restoration failed: restored bytes differ")
	}
	restoredFile, err := openResultFile(directory, finalName)
	if err != nil {
		return fmt.Errorf("restoration failed opening restored result: %w", err)
	}
	syncErr := s.syncResultFile(restoredFile)
	closeErr := restoredFile.Close()
	if syncErr != nil {
		return fmt.Errorf("restoration failed to fsync restored result: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("restoration failed closing restored result: %w", closeErr)
	}
	if err := s.unlinkAt(directory, rollbackName); err != nil {
		syncErr := s.syncDirectory(directory)
		return combineResultFailure(errors.New("restoration failed cleaning rollback candidate"), combineResultErrors(err, syncErr))
	}
	if err := s.syncDirectory(directory); err != nil {
		return fmt.Errorf("restoration failed to fsync result directory: %w", err)
	}
	finalIdentity, finalBytes, err := readResultRawAt(directory, finalName, s.ExpectedUID)
	if err != nil {
		return fmt.Errorf("restoration failed final verification: %w", err)
	}
	if finalIdentity == (resultIdentity{}) || !bytes.Equal(finalBytes, oldBytes) {
		return errors.New("restoration failed: final bytes are not the original result")
	}
	_ = expected
	_ = restoredIdentity
	return nil
}

func (s *ResultStore) logicalResultPath(name string) string {
	return filepath.Join(s.resultDirectory(), name)
}

func (s *ResultStore) ensureDirectory() error {
	return s.validateExistingDirectory()
}

func (s *ResultStore) validateExistingDirectory() error {
	directory := s.resultDirectory()
	if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || directory == string(filepath.Separator) {
		return errors.New("result directory must be an absolute canonical non-root path")
	}
	// The protected-directory existence, type, owner, mode, and link-count
	// checks are performed on the descriptor returned by openResultDirectory.
	// Do not resolve this pathname here: the openat2 trusted-root walk must be
	// the first filesystem lookup so an ancestor swap cannot redirect policy.
	return nil
}

func resultFileName(runID string) string {
	return "result-" + runID + ".json"
}

func isResultNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT)
}

func combineResultErrors(primary, secondary error) error {
	if primary == nil {
		return secondary
	}
	if secondary == nil {
		return primary
	}
	return fmt.Errorf("%w (also: %v)", primary, secondary)
}

func combineResultFailure(primary, restoration error) error {
	if primary == nil {
		return restoration
	}
	if restoration == nil {
		return primary
	}
	return fmt.Errorf("%w (restoration failure: %v)", primary, restoration)
}

func sameResultEntrySnapshot(left, right resultEntrySnapshot) bool {
	if !sameResultEntryMetadata(left, right) {
		return false
	}
	if left.kind == resultEntrySymlinkKind {
		return left.targetKnown && right.targetKnown && left.target == right.target
	}
	if left.kind == resultEntryRegularKind {
		return left.rawKnown && right.rawKnown && bytes.Equal(left.raw, right.raw)
	}
	return true
}

func sameResultEntryMetadata(left, right resultEntrySnapshot) bool {
	return left.device == right.device && left.inode == right.inode && left.size == right.size &&
		left.mode == right.mode && left.uid == right.uid && left.gid == right.gid &&
		left.nlink == right.nlink && left.rdev == right.rdev && left.kind == right.kind
}

func validateResultFileInfo(info os.FileInfo, expectedUID uint32) error {
	if info.Mode()&os.ModeType != 0 || !info.Mode().IsRegular() {
		return errors.New("result path is not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return errors.New("result file mode must be 0600")
	}
	uid, _, _, nlink, err := resultStatValues(info)
	if err != nil {
		return err
	}
	if uid != uint64(expectedUID) {
		return errors.New("result file owner is not authorized")
	}
	if nlink != 1 {
		return errors.New("result file link count must be one")
	}
	return nil
}

func resultStatValues(info os.FileInfo) (uid, device, inode, nlink uint64, err error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return 0, 0, 0, 0, errors.New("result metadata has no platform stat")
	}
	return uint64(stat.Uid), uint64(stat.Dev), uint64(stat.Ino), uint64(stat.Nlink), nil
}

func getResultIdentity(info os.FileInfo) (resultIdentity, error) {
	_, device, inode, _, err := resultStatValues(info)
	if err != nil {
		return resultIdentity{}, err
	}
	return resultIdentity{device: device, inode: inode, size: info.Size()}, nil
}

func sameFileIdentity(left, right os.FileInfo) bool {
	_, leftDevice, leftInode, _, leftErr := resultStatValues(left)
	_, rightDevice, rightInode, _, rightErr := resultStatValues(right)
	return leftErr == nil && rightErr == nil && leftDevice == rightDevice && leftInode == rightInode && left.Size() == right.Size()
}
