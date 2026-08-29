//go:build fpgadev

package fpgadev

// This file contains the boot-local proof protocol and the platform-neutral
// supervisor state machine.  Hardware and process operations are deliberately
// represented by small callbacks in Dependencies: host tests can exercise
// ordering and failure fences without opening target devices, while the ARM
// composition supplies the real adapters.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

const (
	SupervisorSchemaVersion       uint64 = 1
	SupervisorReadinessTimeout           = 30 * time.Second
	SupervisorAgentTimeout               = 5 * time.Second
	SupervisorChildCleanupTimeout        = 2 * time.Second
)

var developmentCapabilities = [...]string{
	"cast_unavailable",
	"input_unavailable",
	"controller_routes_unavailable",
	"presentation_unavailable",
	"audio_unavailable",
}

// DevelopmentCapabilities returns the exact ordered capability declaration
// emitted by the development agent and copied into its boot proof.
func DevelopmentCapabilities() []string { return append([]string(nil), developmentCapabilities[:]...) }

// ReadinessReceipt is the one-shot child-to-supervisor attestation.  PIDs and
// start times are represented as unsigned values so their canonical decimal
// JSON spelling is unambiguous on both 32-bit ARM and host tests.
type ReadinessReceipt struct {
	Schema           uint64   `json:"schema"`
	PID              uint64   `json:"pid"`
	StartTime        uint64   `json:"start_time"`
	ExecutableDevice uint64   `json:"executable_device"`
	ExecutableInode  uint64   `json:"executable_inode"`
	ExecutableSHA256 string   `json:"executable_sha256"`
	ProfileSHA256    string   `json:"profile_sha256"`
	Capabilities     []string `json:"capabilities"`
}

type AgentReadinessReceipt = ReadinessReceipt
type ReadinessReceiptV1 = ReadinessReceipt
type AgentReceiptV1 = ReadinessReceipt

var receiptFields = [...]string{
	"schema", "pid", "start_time", "executable_device", "executable_inode",
	"executable_sha256", "profile_sha256", "capabilities",
}

func (r ReadinessReceipt) Validate() error {
	if r.Schema != SupervisorSchemaVersion || r.PID == 0 || r.StartTime == 0 || r.ExecutableDevice == 0 || r.ExecutableInode == 0 {
		return errors.New("readiness receipt identity is incomplete")
	}
	if !manifestHashPattern.MatchString(r.ExecutableSHA256) || !manifestHashPattern.MatchString(r.ProfileSHA256) {
		return errors.New("readiness receipt hash is not canonical")
	}
	if len(r.Capabilities) != len(developmentCapabilities) {
		return errors.New("readiness receipt capability set is incomplete")
	}
	for i, value := range developmentCapabilities {
		if r.Capabilities[i] != value {
			return errors.New("readiness receipt capability set is not canonical")
		}
	}
	return nil
}

func (r ReadinessReceipt) MarshalCanonical() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(raw) > ReadinessReceiptMaxBytes {
		return nil, errors.New("readiness receipt exceeds size bound")
	}
	return raw, nil
}

func ParseReadinessReceipt(raw []byte) (ReadinessReceipt, error) {
	var receipt ReadinessReceipt
	if len(raw) == 0 || len(raw) > ReadinessReceiptMaxBytes {
		return receipt, errors.New("readiness receipt exceeds size bound")
	}
	if err := decodeStrictObject(raw, receiptFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &receipt.Schema)
		case "pid":
			return decodeJSONUint(value, &receipt.PID)
		case "start_time":
			return decodeJSONUint(value, &receipt.StartTime)
		case "executable_device":
			return decodeJSONUint(value, &receipt.ExecutableDevice)
		case "executable_inode":
			return decodeJSONUint(value, &receipt.ExecutableInode)
		case "executable_sha256":
			return decodeJSONString(value, &receipt.ExecutableSHA256)
		case "profile_sha256":
			return decodeJSONString(value, &receipt.ProfileSHA256)
		case "capabilities":
			return decodeStringArray(value, &receipt.Capabilities)
		default:
			return errors.New("unknown readiness receipt field")
		}
	}); err != nil {
		return ReadinessReceipt{}, err
	}
	if err := receipt.Validate(); err != nil {
		return ReadinessReceipt{}, err
	}
	canonical, err := receipt.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return ReadinessReceipt{}, errors.New("readiness receipt JSON is not canonical")
	}
	return receipt, nil
}

// ReadReadinessReceipt consumes exactly one bounded inherited-pipe payload.
// The caller owns child termination/pipe closure on timeout; this helper never
// launches a detached reader that could outlive the failed supervisor phase.
func ReadReadinessReceipt(ctx context.Context, reader io.Reader) (ReadinessReceipt, error) {
	if reader == nil {
		return ReadinessReceipt{}, errors.New("readiness receipt pipe is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ReadinessReceipt{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(reader, ReadinessReceiptMaxBytes+1))
	if err != nil {
		return ReadinessReceipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return ReadinessReceipt{}, err
	}
	return ParseReadinessReceipt(raw)
}

// ReadAgentReadinessReceipt is a descriptive compatibility alias.
func ReadAgentReadinessReceipt(ctx context.Context, reader io.Reader) (ReadinessReceipt, error) {
	return ReadReadinessReceipt(ctx, reader)
}

const (
	// ReadyRecordV3Path is the boot-local admission record. It deliberately
	// lives outside the durable install journal: a reboot invalidates the
	// record, while the journal remains installation history.
	ReadyRecordV3Path     = "/run/fogcast/fpgadev-ready-v3.json"
	ReadyRecordV3MaxBytes = 4096
	ReadyRecordPathV3     = ReadyRecordV3Path
	ReadyMaxBytesV3       = ReadyRecordV3MaxBytes
)

// ReadyRecordV3 is the compact, boot-local proof consumed by normal
// development admission.  PID plus Linux kernel start time is sufficient for
// the private trusted-development threat model; executable paths, inode/device
// identities, executable hashes, and resolved inventories are intentionally
// not part of this record.
type ReadyRecordV3 struct {
	Schema              uint64   `json:"schema"`
	BootID              string   `json:"boot_id"`
	JournalSHA256       string   `json:"journal_sha256"`
	OwnerSession        string   `json:"owner_session"`
	OwnerGeneration     uint64   `json:"owner_generation"`
	ProfileSHA256       string   `json:"profile_sha256"`
	Capabilities        []string `json:"capabilities"`
	SupervisorPID       uint64   `json:"supervisor_pid"`
	SupervisorStartTime uint64   `json:"supervisor_start_time"`
	MainPID             uint64   `json:"main_pid"`
	MainStartTime       uint64   `json:"main_start_time"`
	AgentPID            uint64   `json:"agent_pid"`
	AgentStartTime      uint64   `json:"agent_start_time"`
}

var readyRecordV3Fields = [...]string{
	"schema", "boot_id", "journal_sha256", "owner_session", "owner_generation",
	"profile_sha256", "capabilities", "supervisor_pid", "supervisor_start_time",
	"main_pid", "main_start_time", "agent_pid", "agent_start_time",
}

func (r ReadyRecordV3) Validate() error {
	if r.Schema != 3 {
		return errors.New("ready record schema must be 3")
	}
	if !installBootIDPattern.MatchString(r.BootID) {
		return errors.New("ready record boot ID is not canonical")
	}
	if !manifestHashPattern.MatchString(r.JournalSHA256) || !manifestHashPattern.MatchString(r.ProfileSHA256) {
		return errors.New("ready record hash is not canonical")
	}
	if !manifestRunIDPattern.MatchString(r.OwnerSession) || r.OwnerGeneration == 0 {
		return errors.New("ready record owner tuple is incomplete")
	}
	if r.SupervisorPID == 0 || r.SupervisorStartTime == 0 || r.MainPID == 0 || r.MainStartTime == 0 || r.AgentPID == 0 || r.AgentStartTime == 0 {
		return errors.New("ready record process tuple is incomplete")
	}
	if !sameCapabilities(r.Capabilities) {
		return errors.New("ready record capability set is not canonical")
	}
	return nil
}

// MarshalCanonical returns exactly one compact JSON object followed by one
// newline. Struct declaration order is the wire order; ParseReadyRecordV3
// separately rejects every other order.
func (r ReadyRecordV3) MarshalCanonical() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(raw) > ReadyRecordV3MaxBytes {
		return nil, errors.New("ready record exceeds size bound")
	}
	return raw, nil
}

// ParseReadyRecordV3 validates the closed ready-record grammar and requires
// that the input bytes are the canonical representation emitted above.
func ParseReadyRecordV3(raw []byte) (ReadyRecordV3, error) {
	var record ReadyRecordV3
	if len(raw) == 0 || len(raw) > ReadyRecordV3MaxBytes {
		return record, errors.New("ready record exceeds size bound")
	}
	if err := decodeStrictObject(raw, readyRecordV3Fields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &record.Schema)
		case "boot_id":
			return decodeJSONString(value, &record.BootID)
		case "journal_sha256":
			return decodeJSONString(value, &record.JournalSHA256)
		case "owner_session":
			return decodeJSONString(value, &record.OwnerSession)
		case "owner_generation":
			return decodeJSONUint(value, &record.OwnerGeneration)
		case "profile_sha256":
			return decodeJSONString(value, &record.ProfileSHA256)
		case "capabilities":
			return decodeStringArray(value, &record.Capabilities)
		case "supervisor_pid":
			return decodeJSONUint(value, &record.SupervisorPID)
		case "supervisor_start_time":
			return decodeJSONUint(value, &record.SupervisorStartTime)
		case "main_pid":
			return decodeJSONUint(value, &record.MainPID)
		case "main_start_time":
			return decodeJSONUint(value, &record.MainStartTime)
		case "agent_pid":
			return decodeJSONUint(value, &record.AgentPID)
		case "agent_start_time":
			return decodeJSONUint(value, &record.AgentStartTime)
		default:
			return errors.New("unknown ready record field")
		}
	}); err != nil {
		return ReadyRecordV3{}, err
	}
	if err := record.Validate(); err != nil {
		return ReadyRecordV3{}, err
	}
	canonical, err := record.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return ReadyRecordV3{}, errors.New("ready record JSON is not canonical")
	}
	return record, nil
}

// ReadyStoreV3 is the protected-file boundary for the boot-local record.
// Implementations must atomically publish and remove records and make those
// operations durable before returning.
type ReadyStoreV3 interface {
	Load() (ReadyRecordV3, bool, error)
	Replace(ReadyRecordV3) error
	Remove() error
}

// ReadyRecordStoreV3 is the filesystem implementation used by the target.
// ExpectedUID is explicit so tests can use a private /dev/shm directory while
// production constructors retain the root-owned protected-file contract.
type ReadyRecordStoreV3 struct {
	Path        string
	ExpectedUID uint32
	syncFile    func(*os.File) error
	syncParent  func(*os.File) error
	rename      func(string, string) error
}

// ReadyRecordV3Store is a descriptive spelling alias matching the public
// ReadyStoreV3 interface name.
type ReadyRecordV3Store = ReadyRecordStoreV3

func NewReadyStoreV3(path string, expectedUID ...uint32) *ReadyRecordStoreV3 {
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	return &ReadyRecordStoreV3{Path: path, ExpectedUID: uid}
}

func NewReadyRecordStoreV3(path string, expectedUID ...uint32) *ReadyRecordStoreV3 {
	return NewReadyStoreV3(path, expectedUID...)
}

func NewReadyRecordV3Store(path string, expectedUID ...uint32) *ReadyRecordStoreV3 {
	return NewReadyStoreV3(path, expectedUID...)
}

func NewProductionReadyStoreV3() *ReadyRecordStoreV3 {
	return NewReadyStoreV3(ReadyRecordV3Path)
}

func NewProductionReadyRecordV3Store() *ReadyRecordStoreV3 {
	return NewProductionReadyStoreV3()
}

func (s *ReadyRecordStoreV3) Load() (ReadyRecordV3, bool, error) {
	if err := validateReadyStorePath(s); err != nil {
		return ReadyRecordV3{}, false, err
	}
	parent := filepath.Dir(s.Path)
	if err := ensureNoSymlinkComponents(parent); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReadyRecordV3{}, false, nil
		}
		return ReadyRecordV3{}, false, err
	}
	if err := validateSecureJournalDir(parent, s.ExpectedUID); err != nil {
		return ReadyRecordV3{}, false, err
	}
	info, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return ReadyRecordV3{}, false, nil
	}
	if err != nil {
		return ReadyRecordV3{}, false, err
	}
	if err := validateReadyRecordFileInfo(info, s.ExpectedUID); err != nil {
		return ReadyRecordV3{}, true, err
	}
	file, err := os.Open(s.Path)
	if err != nil {
		return ReadyRecordV3{}, true, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, ReadyRecordV3MaxBytes+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return ReadyRecordV3{}, true, err
	}
	if len(raw) > ReadyRecordV3MaxBytes {
		return ReadyRecordV3{}, true, errors.New("ready record exceeds size bound")
	}
	record, err := ParseReadyRecordV3(raw)
	return record, true, err
}

func (s *ReadyRecordStoreV3) Replace(record ReadyRecordV3) error {
	if err := validateReadyStorePath(s); err != nil {
		return err
	}
	raw, err := record.MarshalCanonical()
	if err != nil {
		return err
	}
	parent := filepath.Dir(s.Path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return err
	}
	if err := validateSecureJournalDir(parent, s.ExpectedUID); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".fpgadev-ready-v3.tmp-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := s.syncReadyFile(temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := s.renameReadyPath(temporaryPath, s.Path); err != nil {
		return err
	}
	keep = true
	parentFile, err := os.Open(parent)
	if err != nil {
		return err
	}
	syncErr := s.syncReadyParent(parentFile)
	closeErr := parentFile.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return err
	}
	info, err := os.Lstat(s.Path)
	if err != nil {
		return err
	}
	return validateReadyRecordFileInfo(info, s.ExpectedUID)
}

func (s *ReadyRecordStoreV3) Remove() error {
	if err := validateReadyStorePath(s); err != nil {
		return err
	}
	parent := filepath.Dir(s.Path)
	if err := ensureNoSymlinkComponents(parent); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := validateSecureJournalDir(parent, s.ExpectedUID); err != nil {
		return err
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parentFile, err := os.Open(parent)
	if err != nil {
		return err
	}
	syncErr := s.syncReadyParent(parentFile)
	closeErr := parentFile.Close()
	return errors.Join(syncErr, closeErr)
}

func validateReadyStorePath(s *ReadyRecordStoreV3) error {
	if s == nil || s.Path == "" || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path || filepath.Base(s.Path) == "." || filepath.Base(s.Path) == ".." {
		return errSupervisorDeps
	}
	return nil
}

func validateReadyRecordFileInfo(info os.FileInfo, uid uint32) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() > ReadyRecordV3MaxBytes {
		return errors.New("ready record is not a protected regular 0600 file")
	}
	if got, ok := journalUID(info); !ok || got != uid {
		return errors.New("ready record owner is not authorized")
	}
	if n, ok := journalNlink(info); ok && n != 1 {
		return errors.New("ready record link count is not one")
	}
	return nil
}

func (s *ReadyRecordStoreV3) syncReadyFile(file *os.File) error {
	if s.syncFile != nil {
		return s.syncFile(file)
	}
	return file.Sync()
}

func (s *ReadyRecordStoreV3) syncReadyParent(file *os.File) error {
	if s.syncParent != nil {
		return s.syncParent(file)
	}
	return file.Sync()
}

func (s *ReadyRecordStoreV3) renameReadyPath(old, next string) error {
	if s.rename != nil {
		return s.rename(old, next)
	}
	return os.Rename(old, next)
}

var _ ReadyStoreV3 = (*ReadyRecordStoreV3)(nil)

// DevelopmentAdmissionVerifier checks the boot-local ready record against
// the current terminal journal, protected profile, owner tuple, and live
// process start times. It intentionally does not hash executables or scan the
// complete process table.
//
// The callback-typed fields are kept as any so host tests can inject readers
// with either context-aware or context-free signatures without exposing Linux
// process details through the stable agent API. New code should prefer the
// context-aware forms accepted by NewDevelopmentAdmissionVerifier.
type DevelopmentAdmissionVerifier struct {
	ReadyStore ReadyStoreV3
	// Ready is a descriptive alias accepted for fixture and adapter callers.
	Ready   ReadyStoreV3
	Journal any
	// JournalStore is a descriptive alias for Journal.
	JournalStore  any
	BootID        any
	CurrentBootID string

	// ProfilePath reads the protected profile through LoadProtectedAgentConfig
	// when ProfileHash/ReadProfileHash are not injected.
	ProfilePath     string
	ProfileSHA256   string
	ProfileHash     any
	ReadProfileHash any
	Profile         any
	ReadProfile     any

	ProcRoot             string
	ProcessStartTime     any
	ReadProcessStartTime any
	ReadStartTime        any
}

// DevelopmentAdmissionVerifierOptions is the explicit form of the flexible
// constructor. It is useful to production adapters and keeps fixture setup
// self-documenting while preserving the small Verify interface.
type DevelopmentAdmissionVerifierOptions struct {
	ReadyStore           ReadyStoreV3
	Journal              any
	BootID               any
	ProfilePath          string
	ProfileSHA256        string
	ProfileHash          any
	Profile              any
	ProcessStartTime     any
	ReadProcessStartTime any
	ReadStartTime        any
	ProcRoot             string
}

func NewDevelopmentAdmissionVerifier(args ...any) *DevelopmentAdmissionVerifier {
	v := &DevelopmentAdmissionVerifier{}
	bootSet, profileSet, processSet := false, false, false
	for _, arg := range args {
		switch value := arg.(type) {
		case DevelopmentAdmissionVerifierOptions:
			v.ReadyStore = value.ReadyStore
			v.Journal = value.Journal
			v.BootID = value.BootID
			v.ProfilePath = value.ProfilePath
			v.ProfileSHA256 = value.ProfileSHA256
			v.ProfileHash = value.ProfileHash
			v.Profile = value.Profile
			v.ProcessStartTime = value.ProcessStartTime
			v.ReadProcessStartTime = value.ReadProcessStartTime
			v.ReadStartTime = value.ReadStartTime
			v.ProcRoot = value.ProcRoot
			bootSet = value.BootID != nil
			profileSet = value.ProfileHash != nil || value.Profile != nil || value.ProfileSHA256 != ""
			processSet = value.ProcessStartTime != nil || value.ReadProcessStartTime != nil || value.ReadStartTime != nil
		case *DevelopmentAdmissionVerifierOptions:
			if value == nil {
				continue
			}
			v.ReadyStore = value.ReadyStore
			v.Journal = value.Journal
			v.BootID = value.BootID
			v.ProfilePath = value.ProfilePath
			v.ProfileSHA256 = value.ProfileSHA256
			v.ProfileHash = value.ProfileHash
			v.Profile = value.Profile
			v.ProcessStartTime = value.ProcessStartTime
			v.ReadProcessStartTime = value.ReadProcessStartTime
			v.ReadStartTime = value.ReadStartTime
			v.ProcRoot = value.ProcRoot
			bootSet = value.BootID != nil
			profileSet = value.ProfileHash != nil || value.Profile != nil || value.ProfileSHA256 != ""
			processSet = value.ProcessStartTime != nil || value.ReadProcessStartTime != nil || value.ReadStartTime != nil
		case ReadyStoreV3:
			v.ReadyStore = value
		case *ReadyRecordStoreV3:
			v.ReadyStore = value
		case installJournalReader:
			v.Journal = value
		case string:
			if v.ProfilePath == "" {
				v.ProfilePath = value
			} else if v.CurrentBootID == "" {
				v.CurrentBootID = value
			}
		case func() (string, error):
			if !bootSet {
				v.BootID, bootSet = value, true
			} else if !profileSet {
				v.ProfileHash, profileSet = value, true
			}
		case func(context.Context) (string, error):
			if !bootSet {
				v.BootID, bootSet = value, true
			} else if !profileSet {
				v.ProfileHash, profileSet = value, true
			}
		case func() string:
			if !bootSet {
				v.BootID, bootSet = value, true
			} else if !profileSet {
				v.ProfileHash, profileSet = value, true
			}
		case func(context.Context) string:
			if !bootSet {
				v.BootID, bootSet = value, true
			} else if !profileSet {
				v.ProfileHash, profileSet = value, true
			}
		case func(context.Context, string, int) (uint64, error):
			if !processSet {
				v.ProcessStartTime, processSet = value, true
			}
		case func(context.Context, string, uint64) (uint64, error):
			if !processSet {
				v.ProcessStartTime, processSet = value, true
			}
		case func(context.Context, int) (uint64, error):
			if !processSet {
				v.ProcessStartTime, processSet = value, true
			}
		case func(context.Context, uint64) (uint64, error):
			if !processSet {
				v.ProcessStartTime, processSet = value, true
			}
		case func(string, int) (uint64, error):
			if !processSet {
				v.ProcessStartTime, processSet = value, true
			}
		case func(string, uint64) (uint64, error):
			if !processSet {
				v.ProcessStartTime, processSet = value, true
			}
		case func(int) (uint64, error):
			if !processSet {
				v.ProcessStartTime, processSet = value, true
			}
		case func(uint64) (uint64, error):
			if !processSet {
				v.ProcessStartTime, processSet = value, true
			}
		}
	}
	return v
}

func NewProductionDevelopmentAdmissionVerifier(profilePath ...string) *DevelopmentAdmissionVerifier {
	path := ""
	if len(profilePath) != 0 {
		path = profilePath[0]
	}
	return &DevelopmentAdmissionVerifier{
		ReadyStore:  NewProductionReadyStoreV3(),
		Journal:     NewProductionInstallJournal(),
		BootID:      readKernelBootID,
		ProfilePath: path,
		ProcRoot:    "/proc",
	}
}

func newDevelopmentAdmissionVerifier(args ...any) *DevelopmentAdmissionVerifier {
	return NewDevelopmentAdmissionVerifier(args...)
}

func newProductionAdmissionVerifier(profilePath ...string) *DevelopmentAdmissionVerifier {
	return NewProductionDevelopmentAdmissionVerifier(profilePath...)
}

// NewAdmissionVerifier is a concise compatibility alias for callers that
// describe the verifier without the development-profile qualifier.
func NewAdmissionVerifier(args ...any) *DevelopmentAdmissionVerifier {
	return NewDevelopmentAdmissionVerifier(args...)
}

func (v *DevelopmentAdmissionVerifier) Verify(ctx context.Context, owner hardwareowner.Record) error {
	if v == nil {
		return errSupervisorDeps
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
		return fmt.Errorf("owner record is invalid: %w", err)
	}
	if owner.State != hardwareowner.StateNormalMain {
		return errors.New("owner is not normal_main")
	}
	readyStore := v.ReadyStore
	if readyStore == nil {
		readyStore = v.Ready
	}
	if admissionNil(readyStore) {
		return errors.New("ready record store is unavailable")
	}
	ready, exists, err := readyStore.Load()
	if err != nil {
		return fmt.Errorf("load ready record: %w", err)
	}
	if !exists {
		return errors.New("ready record is absent")
	}
	if err := ready.Validate(); err != nil {
		return fmt.Errorf("ready record is invalid: %w", err)
	}
	bootID, err := invokeAdmissionString(ctx, v.BootID, v.CurrentBootID, readKernelBootID)
	if err != nil {
		return fmt.Errorf("read current boot ID: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ready.BootID != bootID || owner.BootID != bootID {
		return errors.New("ready record or owner belongs to another boot")
	}
	journalValue := v.Journal
	if journalValue == nil {
		journalValue = v.JournalStore
	}
	journal, ok := journalValue.(installJournalReader)
	if !ok || admissionNil(journal) {
		return errors.New("terminal journal is unavailable")
	}
	record, exists, err := journal.Load()
	if err != nil {
		return fmt.Errorf("load terminal journal: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !exists || record.State != InstallStateTerminal {
		return errors.New("terminal journal is not terminal")
	}
	journalBytes, err := record.MarshalCanonical()
	if err != nil {
		return fmt.Errorf("marshal terminal journal: %w", err)
	}
	digest := sha256.Sum256(journalBytes)
	if ready.JournalSHA256 != hex.EncodeToString(digest[:]) {
		return errors.New("ready record journal digest does not match terminal journal")
	}
	profileHash, err := v.profileHash(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ready.ProfileSHA256 != profileHash {
		return errors.New("ready record profile digest does not match protected profile")
	}
	if ready.OwnerSession != owner.ActiveSession || ready.OwnerGeneration != owner.ActiveGeneration {
		return errors.New("ready record owner tuple does not match owner record")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	procRoot := v.ProcRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	processes := []struct {
		name      string
		pid       uint64
		startTime uint64
	}{
		{name: "supervisor", pid: ready.SupervisorPID, startTime: ready.SupervisorStartTime},
		{name: "Main", pid: ready.MainPID, startTime: ready.MainStartTime},
		{name: "agent", pid: ready.AgentPID, startTime: ready.AgentStartTime},
	}
	for _, process := range processes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if process.pid > uint64(^uint(0)>>1) {
			return fmt.Errorf("%s process PID is out of range", process.name)
		}
		actual, err := invokeAdmissionStartTime(ctx, v.processReader(), procRoot, int(process.pid), process.pid)
		if err != nil {
			return fmt.Errorf("read %s process start time: %w", process.name, err)
		}
		if actual == 0 || actual != process.startTime {
			return fmt.Errorf("%s process start time does not match ready record", process.name)
		}
	}
	return ctx.Err()
}

func (v *DevelopmentAdmissionVerifier) profileHash(ctx context.Context) (string, error) {
	if v == nil {
		return "", errSupervisorDeps
	}
	value := v.ProfileHash
	if value == nil {
		value = v.ReadProfileHash
	}
	if value == nil {
		value = v.Profile
	}
	if value == nil {
		value = v.ReadProfile
	}
	if value != nil {
		hash, err := invokeAdmissionString(ctx, value, "", nil)
		if err != nil {
			return "", fmt.Errorf("read protected profile hash: %w", err)
		}
		if !manifestHashPattern.MatchString(hash) {
			return "", errors.New("protected profile hash is not canonical")
		}
		return hash, nil
	}
	if v.ProfileSHA256 != "" {
		if !manifestHashPattern.MatchString(v.ProfileSHA256) {
			return "", errors.New("protected profile hash is not canonical")
		}
		return v.ProfileSHA256, nil
	}
	if v.ProfilePath == "" {
		return "", errors.New("protected profile reader is unavailable")
	}
	_, hash, err := LoadProtectedAgentConfig(v.ProfilePath)
	if err != nil {
		return "", fmt.Errorf("read protected profile: %w", err)
	}
	return hash, nil
}

func (v *DevelopmentAdmissionVerifier) processReader() any {
	if v == nil {
		return nil
	}
	if v.ProcessStartTime != nil {
		return v.ProcessStartTime
	}
	if v.ReadProcessStartTime != nil {
		return v.ReadProcessStartTime
	}
	return v.ReadStartTime
}

func invokeAdmissionString(ctx context.Context, value any, fixed string, fallback any) (string, error) {
	if fixed != "" {
		return fixed, nil
	}
	if value == nil {
		value = fallback
	}
	if admissionNil(value) {
		return "", errSupervisorDeps
	}
	switch fn := value.(type) {
	case func() (string, error):
		return fn()
	case func(context.Context) (string, error):
		return fn(ctx)
	case func() string:
		return fn(), nil
	case func(context.Context) string:
		return fn(ctx), nil
	default:
		return "", errSupervisorDeps
	}
}

func invokeAdmissionStartTime(ctx context.Context, value any, procRoot string, pid int, pid64 uint64) (uint64, error) {
	if value == nil {
		return readAdmissionProcessStartTime(ctx, procRoot, pid)
	}
	if admissionNil(value) {
		return 0, errSupervisorDeps
	}
	switch fn := value.(type) {
	case func(context.Context, string, int) (uint64, error):
		return fn(ctx, procRoot, pid)
	case func(context.Context, string, uint64) (uint64, error):
		return fn(ctx, procRoot, pid64)
	case func(context.Context, int) (uint64, error):
		return fn(ctx, pid)
	case func(context.Context, uint64) (uint64, error):
		return fn(ctx, pid64)
	case func(string, int) (uint64, error):
		return fn(procRoot, pid)
	case func(string, uint64) (uint64, error):
		return fn(procRoot, pid64)
	case func(int) (uint64, error):
		return fn(pid)
	case func(uint64) (uint64, error):
		return fn(pid64)
	default:
		return 0, errSupervisorDeps
	}
}

func admissionNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func readAdmissionProcessStartTime(ctx context.Context, procRoot string, pid int) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if pid <= 0 || procRoot == "" {
		return 0, errors.New("process identity is invalid")
	}
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	start, parsedPID, state, err := parseProcStat(raw)
	if err != nil {
		return 0, err
	}
	if parsedPID != pid || start == 0 {
		return 0, errors.New("process start-time identity is invalid")
	}
	switch state {
	case 'Z', 'X', 'x':
		return 0, fmt.Errorf("process state %q is dead or zombie", state)
	}
	return start, ctx.Err()
}

func decodeStringArray(raw []byte, target *[]string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var values []string
	if err := decoder.Decode(&values); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("array has trailing data")
	}
	for _, value := range values {
		if err := validateString(value, "array value", true); err != nil {
			return err
		}
	}
	*target = values
	return nil
}

// ResolvedPathV1 is a current-boot metadata observation.  Device and inode
// are intentionally kept here, rather than in the immutable journal, because
// FIFOs/devtmpfs objects legitimately receive new identities after reboot.
type ResolvedPathV1 struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Rdev   uint64 `json:"rdev"`
	SHA256 string `json:"sha256"`
}

type ResolvedNetworkV1 = NetworkExpectation

type ResolvedSourceV1 struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	SHA256 string `json:"sha256"`
}

// ResolvedInventoryV1 mirrors InventoryV1 while adding only current-boot
// object identities and source state.  The pointer fields preserve null for
// absent optional routes.
type ResolvedInventoryV1 struct {
	Schema            uint64             `json:"schema"`
	MainExecutable    ResolvedPathV1     `json:"main_executable"`
	CastExecutable    *ResolvedPathV1    `json:"cast_executable"`
	InputUInput       *ResolvedPathV1    `json:"input_uinput"`
	CastFramebuffer   *ResolvedPathV1    `json:"cast_framebuffer"`
	CastNativeCommand *ResolvedPathV1    `json:"cast_native_command"`
	CastTokenFile     *ResolvedPathV1    `json:"cast_token_file"`
	InputListen       *ResolvedNetworkV1 `json:"input_listen"`
	CastRTP           *ResolvedNetworkV1 `json:"cast_rtp"`
	CastControl       *ResolvedNetworkV1 `json:"cast_control"`
	MainFIFO          ResolvedPathV1     `json:"main_fifo"`
	StartSources      []ResolvedSourceV1 `json:"start_sources"`
}

// InventoryResolutionPhase determines which process/descriptor holders are
// admissible while the boot-local object is being attested.  The
// pre-dispatch phase is the only phase in which the exact Main process may
// retain the command FIFO.  Once Main is stably absent every inventoried
// holder is a fence condition.
type InventoryResolutionPhase string

const (
	InventoryPhasePreDispatch InventoryResolutionPhase = "pre_dispatch"
	InventoryPhasePostMain    InventoryResolutionPhase = "post_main"
	InventoryPhaseMainAbsent  InventoryResolutionPhase = "main_absent"
	// Descriptive aliases keep call sites readable without introducing a
	// second wire spelling.
	InventoryPhaseBeforeDispatch = InventoryPhasePreDispatch
	InventoryPhaseAfterMain      = InventoryPhaseMainAbsent
)

// InventoryResolutionOptions contains only boot-local observation controls;
// none of these values is serialized into the immutable install journal.
// ProcRoot is injectable for hostile software fixtures and defaults to /proc.
type InventoryResolutionOptions struct {
	ProcRoot               string
	Phase                  InventoryResolutionPhase
	Prior                  *ResolvedInventoryV1
	MainProcess            *ProcessIdentity
	RequireProcessEvidence bool
	ScanProcessDescriptors bool
	RequireNetworkEvidence bool
	RequireNetworkAbsence  bool
	RequireMainFIFOOwner   bool
}

var resolvedInventoryFields = inventoryFields
var resolvedPathFields = [...]string{"path", "kind", "state", "device", "inode", "rdev", "sha256"}
var resolvedSourceFields = [...]string{"path", "kind", "state", "device", "inode", "sha256"}

func (p ResolvedPathV1) Validate(required bool) error {
	if err := validateString(p.Path, "resolved path", required); err != nil {
		return err
	}
	if err := validateString(p.Kind, "resolved kind", required); err != nil {
		return err
	}
	if p.Kind != "" && p.Kind != "regular" && p.Kind != "fifo" && p.Kind != "character" && p.Kind != "block" && p.Kind != "socket" && p.Kind != "directory" {
		return errors.New("unknown resolved path kind")
	}
	if p.State != "present" && p.State != "expected_absent" {
		return errors.New("unknown resolved path state")
	}
	if p.State == "expected_absent" && (p.Device != 0 || p.Inode != 0 || p.Rdev != 0 || p.SHA256 != "") {
		return errors.New("expected-absent path carries identity")
	}
	if p.State == "present" && (p.Device == 0 || p.Inode == 0) {
		return errors.New("present path identity is incomplete")
	}
	if required && p.State != "present" {
		return errors.New("required path cannot be expected absent")
	}
	if p.Kind != "character" && p.Kind != "block" && p.Rdev != 0 {
		return errors.New("resolved rdev is only valid for character or block paths")
	}
	if p.SHA256 != "" && !manifestHashPattern.MatchString(p.SHA256) {
		return errors.New("resolved path hash is not canonical")
	}
	return nil
}

func (i ResolvedInventoryV1) Validate() error {
	if i.Schema != 1 {
		return errors.New("resolved inventory schema must be 1")
	}
	if err := i.MainExecutable.Validate(true); err != nil {
		return err
	}
	if err := validateResolvedOptional(i.CastExecutable); err != nil {
		return err
	}
	if err := validateResolvedOptional(i.InputUInput); err != nil {
		return err
	}
	if err := validateResolvedOptional(i.CastFramebuffer); err != nil {
		return err
	}
	if err := validateResolvedOptional(i.CastNativeCommand); err != nil {
		return err
	}
	if err := validateResolvedOptional(i.CastTokenFile); err != nil {
		return err
	}
	if err := validateOptionalNetwork(i.InputListen); err != nil {
		return err
	}
	if err := validateOptionalNetwork(i.CastRTP); err != nil {
		return err
	}
	if err := validateOptionalNetwork(i.CastControl); err != nil {
		return err
	}
	if err := i.MainFIFO.Validate(true); err != nil {
		return err
	}
	if i.StartSources == nil || len(i.StartSources) > 16 {
		return errors.New("resolved start_sources is invalid")
	}
	for n, source := range i.StartSources {
		if err := validateString(source.Path, "resolved source path", true); err != nil {
			return errors.New("resolved source path is invalid")
		}
		if !filepath.IsAbs(source.Path) || filepath.Clean(source.Path) != source.Path {
			return errors.New("resolved source path is not canonical")
		}
		switch source.Kind {
		case "regular", "fifo", "character", "block", "socket":
		default:
			return errors.New("unknown resolved source kind")
		}
		if source.State != "disabled_absent" && source.State != "disabled_inert" && source.State != "approved_trampoline" {
			return errors.New("unknown resolved source state")
		}
		if source.State == "disabled_absent" {
			if source.Device != 0 || source.Inode != 0 || source.SHA256 != "" {
				return errors.New("absent resolved source carries identity")
			}
		} else if source.Device == 0 || source.Inode == 0 {
			return errors.New("resolved source identity is incomplete")
		}
		if source.State != "disabled_absent" && !manifestHashPattern.MatchString(source.SHA256) {
			return errors.New("resolved enabled-source hash is missing")
		}
		if source.SHA256 != "" && !manifestHashPattern.MatchString(source.SHA256) {
			return errors.New("resolved source hash is invalid")
		}
		if n > 0 && i.StartSources[n-1].Path >= source.Path {
			return errors.New("resolved sources are not sorted")
		}
	}
	return nil
}

func validateResolvedOptional(p *ResolvedPathV1) error {
	if p == nil {
		return nil
	}
	return p.Validate(false)
}

func (i ResolvedInventoryV1) MarshalCanonical() ([]byte, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(i)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeResolvedPath(raw []byte, target *ResolvedPathV1, required bool) error {
	return decodeStrictObject(raw, resolvedPathFields[:], func(key string, value []byte) error {
		switch key {
		case "path":
			return decodeJSONString(value, &target.Path)
		case "kind":
			return decodeJSONString(value, &target.Kind)
		case "state":
			return decodeJSONString(value, &target.State)
		case "device":
			return decodeJSONUint(value, &target.Device)
		case "inode":
			return decodeJSONUint(value, &target.Inode)
		case "rdev":
			return decodeJSONUint(value, &target.Rdev)
		case "sha256":
			return decodeJSONString(value, &target.SHA256)
		default:
			return errors.New("unknown resolved path field")
		}
	})
}

func parseResolvedInventory(raw []byte) (ResolvedInventoryV1, error) {
	var i ResolvedInventoryV1
	err := decodeStrictObject(raw, resolvedInventoryFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &i.Schema)
		case "main_executable":
			return decodeResolvedPath(value, &i.MainExecutable, true)
		case "cast_executable":
			return decodeOptionalResolvedPath(value, &i.CastExecutable)
		case "input_uinput":
			return decodeOptionalResolvedPath(value, &i.InputUInput)
		case "cast_framebuffer":
			return decodeOptionalResolvedPath(value, &i.CastFramebuffer)
		case "cast_native_command":
			return decodeOptionalResolvedPath(value, &i.CastNativeCommand)
		case "cast_token_file":
			return decodeOptionalResolvedPath(value, &i.CastTokenFile)
		case "input_listen":
			return decodeOptionalNetwork(value, &i.InputListen)
		case "cast_rtp":
			return decodeOptionalNetwork(value, &i.CastRTP)
		case "cast_control":
			return decodeOptionalNetwork(value, &i.CastControl)
		case "main_fifo":
			return decodeResolvedPath(value, &i.MainFIFO, true)
		case "start_sources":
			return decodeResolvedSources(value, &i.StartSources)
		default:
			return errors.New("unknown resolved inventory field")
		}
	})
	if err != nil {
		return ResolvedInventoryV1{}, err
	}
	if err := i.Validate(); err != nil {
		return ResolvedInventoryV1{}, err
	}
	canonical, err := i.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return ResolvedInventoryV1{}, errors.New("resolved inventory JSON is not canonical")
	}
	return i, nil
}

// ParseResolvedInventory validates the current-boot inventory independently
// for callers that consume a proof without parsing the enclosing proof.
func ParseResolvedInventory(raw []byte) (ResolvedInventoryV1, error) {
	return parseResolvedInventory(raw)
}

func decodeOptionalResolvedPath(raw []byte, target **ResolvedPathV1) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*target = nil
		return nil
	}
	var p ResolvedPathV1
	if err := decodeResolvedPath(raw, &p, false); err != nil {
		return err
	}
	*target = &p
	return nil
}
func decodeResolvedSources(raw []byte, target *[]ResolvedSourceV1) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var values []json.RawMessage
	if err := decoder.Decode(&values); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("resolved source array has trailing data")
	}
	result := make([]ResolvedSourceV1, 0, len(values))
	for _, value := range values {
		var source ResolvedSourceV1
		if err := decodeStrictObject(value, resolvedSourceFields[:], func(key string, item []byte) error {
			switch key {
			case "path":
				return decodeJSONString(item, &source.Path)
			case "kind":
				return decodeJSONString(item, &source.Kind)
			case "state":
				return decodeJSONString(item, &source.State)
			case "device":
				return decodeJSONUint(item, &source.Device)
			case "inode":
				return decodeJSONUint(item, &source.Inode)
			case "sha256":
				return decodeJSONString(item, &source.SHA256)
			default:
				return errors.New("unknown resolved source field")
			}
		}); err != nil {
			return err
		}
		result = append(result, source)
	}
	*target = result
	return nil
}

// BootProof is the current-boot, identity-bound admission proof.  It is
// immutable for the boot and only the supervisor may publish it.
type BootProof struct {
	Schema                     uint64              `json:"schema"`
	BootID                     string              `json:"boot_id"`
	OwnerSession               string              `json:"owner_session"`
	OwnerGeneration            uint64              `json:"owner_generation"`
	SupervisorPID              uint64              `json:"supervisor_pid"`
	SupervisorStartTime        uint64              `json:"supervisor_start_time"`
	SupervisorExecutableDevice uint64              `json:"supervisor_executable_device"`
	SupervisorExecutableInode  uint64              `json:"supervisor_executable_inode"`
	SupervisorExecutableSHA256 string              `json:"supervisor_executable_sha256"`
	MainPID                    uint64              `json:"main_pid"`
	MainStartTime              uint64              `json:"main_start_time"`
	MainExecutableDevice       uint64              `json:"main_executable_device"`
	MainExecutableInode        uint64              `json:"main_executable_inode"`
	MainExecutableSHA256       string              `json:"main_executable_sha256"`
	AgentPID                   uint64              `json:"agent_pid"`
	AgentStartTime             uint64              `json:"agent_start_time"`
	AgentExecutableDevice      uint64              `json:"agent_executable_device"`
	AgentExecutableInode       uint64              `json:"agent_executable_inode"`
	AgentExecutableSHA256      string              `json:"agent_executable_sha256"`
	ProfileSHA256              string              `json:"profile_sha256"`
	JournalSHA256              string              `json:"journal_sha256"`
	Capabilities               []string            `json:"capabilities"`
	ResolvedInventory          ResolvedInventoryV1 `json:"resolved_inventory"`
}

// ProcessAttestation is the small identity shape used by Supervisor.Run for
// live child/supervisor bindings. It mirrors ProcessIdentity without exposing
// a PID-reuse-prone pointer or handle.
type ProcessAttestation struct {
	PID       uint64
	StartTime uint64
	Device    uint64
	Inode     uint64
	SHA256    string
}

// CurrentProcessAttestation returns the supervisor's live executable tuple.
// It hashes the kernel-owned /proc/self/exe handle on Linux, avoiding a
// pathname that can be replaced after startup.
func CurrentProcessAttestation() (ProcessAttestation, error) {
	pid := os.Getpid()
	return currentProcessAttestationWithReaders(pid, func() (*os.File, error) {
		return os.Open("/proc/self/exe")
	}, func(pid int) ([]byte, error) {
		return os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	})
}

func currentProcessAttestationWithReaders(pid int, openSelf func() (*os.File, error), readStat func(int) ([]byte, error)) (result ProcessAttestation, resultErr error) {
	if pid <= 0 || openSelf == nil || readStat == nil {
		return ProcessAttestation{}, errors.New("supervisor /proc attestation readers are unavailable")
	}
	file, err := openSelf()
	if err != nil {
		return ProcessAttestation{}, err
	}
	if file == nil {
		return ProcessAttestation{}, errors.New("supervisor /proc self executable is unavailable")
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
	}()
	info, err := file.Stat()
	if err != nil {
		return ProcessAttestation{}, err
	}
	device, okDevice := journalStatUint64(info, "Dev", "Device")
	inode, okInode := journalStatUint64(info, "Ino", "Inode")
	if !okDevice || !okInode || device == 0 || inode == 0 {
		return ProcessAttestation{}, errors.New("supervisor executable identity is unavailable")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return ProcessAttestation{}, err
	}
	raw, err := readStat(pid)
	if err != nil {
		return ProcessAttestation{}, err
	}
	closeParen := strings.LastIndexByte(string(raw), ')')
	if closeParen < 0 {
		return ProcessAttestation{}, errors.New("supervisor /proc stat is malformed")
	}
	fields := strings.Fields(string(raw)[closeParen+1:])
	if len(fields) <= 19 {
		return ProcessAttestation{}, errors.New("supervisor /proc stat lacks start time")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return ProcessAttestation{}, errors.New("supervisor /proc start time is invalid")
	}
	return ProcessAttestation{PID: uint64(pid), StartTime: start, Device: device, Inode: inode, SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

var bootProofFields = [...]string{
	"schema", "boot_id", "owner_session", "owner_generation", "supervisor_pid",
	"supervisor_start_time", "supervisor_executable_device", "supervisor_executable_inode",
	"supervisor_executable_sha256", "main_pid", "main_start_time", "main_executable_device",
	"main_executable_inode", "main_executable_sha256", "agent_pid", "agent_start_time", "agent_executable_device",
	"agent_executable_inode", "agent_executable_sha256", "profile_sha256", "journal_sha256",
	"capabilities", "resolved_inventory",
}

func (p BootProof) Validate() error {
	if p.Schema != 1 || !installBootIDPattern.MatchString(p.BootID) || p.OwnerSession == "" || p.OwnerGeneration == 0 || p.SupervisorPID == 0 || p.MainPID == 0 || p.AgentPID == 0 || p.SupervisorStartTime == 0 || p.MainStartTime == 0 || p.AgentStartTime == 0 || p.SupervisorExecutableDevice == 0 || p.SupervisorExecutableInode == 0 || p.MainExecutableDevice == 0 || p.MainExecutableInode == 0 || p.AgentExecutableDevice == 0 || p.AgentExecutableInode == 0 {
		return errors.New("boot proof identity is incomplete")
	}
	if !manifestRunIDPattern.MatchString(p.OwnerSession) || !manifestHashPattern.MatchString(p.SupervisorExecutableSHA256) || !manifestHashPattern.MatchString(p.MainExecutableSHA256) || !manifestHashPattern.MatchString(p.AgentExecutableSHA256) || !manifestHashPattern.MatchString(p.ProfileSHA256) || !manifestHashPattern.MatchString(p.JournalSHA256) {
		return errors.New("boot proof identity or hash is not canonical")
	}
	if !sameCapabilities(p.Capabilities) {
		return errors.New("boot proof capability set is not canonical")
	}
	return p.ResolvedInventory.Validate()
}

func sameCapabilities(values []string) bool {
	if len(values) != len(developmentCapabilities) {
		return false
	}
	for i, value := range developmentCapabilities {
		if values[i] != value {
			return false
		}
	}
	return true
}

func (p BootProof) MarshalCanonical() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(raw) > BootProofMaxBytes {
		return nil, errors.New("boot proof exceeds size bound")
	}
	return raw, nil
}

func ParseBootProof(raw []byte) (BootProof, error) {
	var proof BootProof
	if len(raw) == 0 || len(raw) > BootProofMaxBytes {
		return proof, errors.New("boot proof exceeds size bound")
	}
	if err := decodeStrictObject(raw, bootProofFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &proof.Schema)
		case "boot_id":
			return decodeJSONString(value, &proof.BootID)
		case "owner_session":
			return decodeJSONString(value, &proof.OwnerSession)
		case "owner_generation":
			return decodeJSONUint(value, &proof.OwnerGeneration)
		case "supervisor_pid":
			return decodeJSONUint(value, &proof.SupervisorPID)
		case "supervisor_start_time":
			return decodeJSONUint(value, &proof.SupervisorStartTime)
		case "supervisor_executable_device":
			return decodeJSONUint(value, &proof.SupervisorExecutableDevice)
		case "supervisor_executable_inode":
			return decodeJSONUint(value, &proof.SupervisorExecutableInode)
		case "supervisor_executable_sha256":
			return decodeJSONString(value, &proof.SupervisorExecutableSHA256)
		case "main_pid":
			return decodeJSONUint(value, &proof.MainPID)
		case "main_start_time":
			return decodeJSONUint(value, &proof.MainStartTime)
		case "main_executable_device":
			return decodeJSONUint(value, &proof.MainExecutableDevice)
		case "main_executable_inode":
			return decodeJSONUint(value, &proof.MainExecutableInode)
		case "main_executable_sha256":
			return decodeJSONString(value, &proof.MainExecutableSHA256)
		case "agent_pid":
			return decodeJSONUint(value, &proof.AgentPID)
		case "agent_start_time":
			return decodeJSONUint(value, &proof.AgentStartTime)
		case "agent_executable_device":
			return decodeJSONUint(value, &proof.AgentExecutableDevice)
		case "agent_executable_inode":
			return decodeJSONUint(value, &proof.AgentExecutableInode)
		case "agent_executable_sha256":
			return decodeJSONString(value, &proof.AgentExecutableSHA256)
		case "profile_sha256":
			return decodeJSONString(value, &proof.ProfileSHA256)
		case "journal_sha256":
			return decodeJSONString(value, &proof.JournalSHA256)
		case "capabilities":
			return decodeStringArray(value, &proof.Capabilities)
		case "resolved_inventory":
			var err error
			proof.ResolvedInventory, err = parseResolvedInventory(value)
			return err
		default:
			return errors.New("unknown boot proof field")
		}
	}); err != nil {
		return BootProof{}, err
	}
	if err := proof.Validate(); err != nil {
		return BootProof{}, err
	}
	canonical, err := proof.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return BootProof{}, errors.New("boot proof JSON is not canonical")
	}
	return proof, nil
}

// Dependencies is intentionally callback-oriented.  Each field accepts the
// concrete function shape used by the production adapter or by host fixtures;
// Supervisor.Run invokes it through the small reflection bridge below and
// rejects unsupported shapes before hardware/reset/child creation.
type Dependencies struct {
	Journal       any
	OwnerStore    any
	InstallLocker any
	OwnerLocker   any
	// ReadyStore is the boot-local schema-3 record used by normal admission.
	// Ready and ReadyStoreV3 are descriptive aliases accepted by fixture and
	// adapter callers; production composition supplies ReadyStore.
	ReadyStore          ReadyStoreV3
	Ready               ReadyStoreV3
	ReadyStoreV3        ReadyStoreV3
	ReadyRecordStore    ReadyStoreV3
	BootID              any
	Reset               any
	StartMain           any
	MainReadiness       any
	MainExecutableReady any
	CommandFIFOReady    any
	FPGAManagerReady    any
	MenuReady           any
	StartAgent          any
	ReadinessReceipt    any
	// PublishReady and RemoveReady are callback aliases for the ready-store
	// operations.  A concrete ReadyStore is preferred whenever it is present.
	PublishReady any
	RemoveReady  any
	// RebootRequester is the only recovery escape hatch after reset begins.
	// Reboot is a descriptive callback alias retained for small adapters.
	RebootRequester any
	Reboot          any
	// Children is an optional concrete runtime controller. It supplies exact
	// pipe closure and child cleanup when a start callback cannot expose a typed
	// handle directly; production uses SupervisorRuntime.
	Children           any
	AgentAlive         any
	WaitLiveness       any
	Now                any
	NewSession         any
	SupervisorIdentity any
	ProfileSHA256      string
	InheritedLockFD    int
	InheritedLockFDSet bool
	// AllowAbsentOwner is enabled only by the protected production composition.
	// On a successor boot it permits a terminal journal to bootstrap a fresh,
	// truthful no_owner checkpoint before Main readiness; same-boot absence
	// remains fenced.
	AllowAbsentOwner bool
}
type SupervisorDependencies = Dependencies

// Supervisor coordinates successor-boot recovery and the single Main/agent
// starter. A zero Supervisor is valid; all required behavior comes from deps.
type Supervisor struct{ defaults *Dependencies }

func NewSupervisor(defaults ...Dependencies) *Supervisor {
	s := &Supervisor{}
	if len(defaults) != 0 {
		value := defaults[0]
		s.defaults = &value
	}
	return s
}

var (
	errSupervisorFenced = errors.New("development supervisor is fenced")
	errSupervisorDeps   = errors.New("development supervisor dependencies are incomplete")
	// ErrSupervisorRebootFailed identifies a fail-stop path where the target
	// could not be asked to cross the reboot recovery boundary.  The original
	// lifecycle failure is joined with this sentinel and the adapter error.
	ErrSupervisorRebootFailed = errors.New("supervisor reboot request failed")
	// ErrRebootRequestFailed is a concise compatibility spelling for adapters
	// that expose the recovery boundary without the supervisor qualifier.
	ErrRebootRequestFailed = ErrSupervisorRebootFailed
	// ErrSupervisorChildExited is returned by the concrete runtime liveness
	// primitive when either retained child exits. It is deliberately exported
	// so a production supervisor adapter can classify the event without
	// matching process-specific diagnostics.
	ErrSupervisorChildExited = errors.New("supervisor child exited")
	// ErrQuiescenceEvidenceUnavailable prevents a missing production observer
	// from being mistaken for positive absence evidence.
	ErrQuiescenceEvidenceUnavailable = errors.New("post-Main quiescence evidence is unavailable")
)

// RebootRequester is intentionally tiny: after reset or another post-reset
// failure the supervisor requests a new kernel boot and never attempts
// same-boot repair or child replacement.
type RebootRequester interface {
	Request(context.Context) error
}

type installJournalReader interface {
	Load() (InstallJournalRecord, bool, error)
}

// retainedSupervisorChild is the private lifecycle boundary implemented by
// ChildProcess. Keeping the interface narrow lets host fixtures prove
// cleanup ordering without exposing process handles in the stable protocol.
type retainedSupervisorChild interface {
	TerminateAndReap(context.Context) error
	Wait(context.Context) error
}

type supervisorChildController interface {
	WaitChildren(context.Context) error
	TerminateChildren(context.Context) error
}

type supervisorReceiptCloser interface {
	CloseReceipt() error
}

// supervisorLifecycle retains the exact handles returned by start callbacks.
// The controller path is preferred because it also closes the inherited
// receipt reader; the direct path keeps callback-only fixtures useful.
type supervisorLifecycle struct {
	controller any
	main       retainedSupervisorChild
	agent      retainedSupervisorChild
}

func (l *supervisorLifecycle) setMain(value any) {
	if l == nil {
		return
	}
	if child, ok := retainedSupervisorChildFrom(value); ok {
		l.main = child
	}
}

func (l *supervisorLifecycle) setAgent(value any) {
	if l == nil {
		return
	}
	if child, ok := retainedSupervisorChildFrom(value); ok {
		l.agent = child
	}
}

func (l *supervisorLifecycle) hasChildren() bool {
	return l != nil && (l.main != nil || l.agent != nil || l.controller != nil)
}

func retainedSupervisorChildFrom(value any) (retainedSupervisorChild, bool) {
	if value == nil {
		return nil, false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if reflected.IsNil() {
			return nil, false
		}
	}
	child, ok := value.(retainedSupervisorChild)
	return child, ok && child != nil
}

func (l *supervisorLifecycle) terminate(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if !admissionNil(l.controller) {
		if controller, ok := l.controller.(supervisorChildController); ok {
			return controller.TerminateChildren(ctx)
		}
	}
	var errs []error
	if l.agent != nil {
		errs = append(errs, terminateSupervisorChild(l.agent))
	}
	if l.main != nil {
		errs = append(errs, terminateSupervisorChild(l.main))
	}
	return errors.Join(errs...)
}

func (l *supervisorLifecycle) closeReceipt() error {
	if l == nil || admissionNil(l.controller) {
		return nil
	}
	if closer, ok := l.controller.(supervisorReceiptCloser); ok {
		return closer.CloseReceipt()
	}
	return nil
}

func (l *supervisorLifecycle) wait(ctx context.Context) error {
	if l == nil {
		return errSupervisorDeps
	}
	if !admissionNil(l.controller) {
		if controller, ok := l.controller.(supervisorChildController); ok {
			return controller.WaitChildren(ctx)
		}
	}
	children := []struct {
		name  string
		child retainedSupervisorChild
	}{{"Main", l.main}, {"agent", l.agent}}
	results := make(chan error, len(children))
	count := 0
	for _, entry := range children {
		if entry.child == nil {
			continue
		}
		count++
		go func(entry struct {
			name  string
			child retainedSupervisorChild
		}) {
			err := entry.child.Wait(ctx)
			if err == nil {
				err = ErrSupervisorChildExited
			} else {
				err = errors.Join(ErrSupervisorChildExited, err)
			}
			results <- fmt.Errorf("%s: %w", entry.name, err)
		}(entry)
	}
	if count == 0 {
		return errSupervisorDeps
	}
	select {
	case err := <-results:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *supervisorLifecycle) mainAttestation() (ProcessAttestation, error) {
	if l == nil || l.main == nil {
		return ProcessAttestation{}, errors.New("retained Main handle is unavailable")
	}
	attested, ok := l.main.(interface {
		Attestation() (ProcessAttestation, error)
	})
	if !ok {
		return ProcessAttestation{}, errors.New("retained Main handle has no identity attestation")
	}
	return attested.Attestation()
}

func terminateSupervisorChild(child retainedSupervisorChild) error {
	if child == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), SupervisorChildCleanupTimeout)
	defer cancel()
	return child.TerminateAndReap(cleanupCtx)
}

func (s *Supervisor) Run(ctx context.Context, deps Dependencies) error {
	if s != nil && deps.Journal == nil && s.defaults != nil {
		deps = *s.defaults
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return runFailStopSupervisor(ctx, deps)
}

// runFailStopSupervisor is the only supervisor lifecycle.  It owns one
// install lock, one owner lock, one Main handle, and one agent handle.  Once
// reset begins every error path removes the boot-local ready record, fences
// an existing durable owner record on a best-effort basis, requests a reboot,
// and returns without scanning, adopting, or replacing a process. A successor
// boot with an absent owner first writes a truthful no_owner checkpoint.
func runFailStopSupervisor(ctx context.Context, deps Dependencies) error {
	journal, ok := deps.Journal.(installJournalReader)
	if !ok || journal == nil {
		return errSupervisorDeps
	}
	readyStore := supervisorReadyStore(deps)
	if readyStore == nil && admissionNil(deps.PublishReady) && admissionNil(deps.RemoveReady) {
		return errSupervisorDeps
	}
	ownerStore, ok := deps.OwnerStore.(interface {
		Load() (hardwareowner.Record, bool, error)
		Replace(hardwareowner.Record) error
	})
	if !ok || admissionNil(ownerStore) {
		return errSupervisorDeps
	}
	_, installUnlock, err := acquireSupervisorInstallLock(ctx, deps, InstallLockPath)
	if err != nil {
		return err
	}
	ownerUnlock, err := lockDependency(ctx, deps.OwnerLocker, hardwareowner.LockPath)
	if err != nil {
		if installUnlock != nil {
			_ = installUnlock()
		}
		return err
	}
	locksHeld := true
	releaseLocks := func() error {
		if !locksHeld {
			return nil
		}
		locksHeld = false
		err := releaseSupervisorAdmission(ownerUnlock, installUnlock)
		ownerUnlock, installUnlock = nil, nil
		return err
	}

	record, exists, err := journal.Load()
	if err != nil || !exists || record.State != InstallStateTerminal {
		_ = releaseLocks()
		return errors.Join(errSupervisorFenced, err)
	}
	bootID, err := stringCallback(ctx, deps.BootID)
	if err != nil || !installBootIDPattern.MatchString(bootID) {
		_ = releaseLocks()
		return errors.Join(errSupervisorFenced, err)
	}
	owner, ownerExists, err := ownerStore.Load()
	if err != nil {
		_ = releaseLocks()
		return errors.Join(errSupervisorFenced, err)
	}
	// A missing owner record is recoverable only on a successor boot and only
	// for the production composition that explicitly opts into this bootstrap.
	// Same-boot absence is ambiguous: it must not reset hardware or start a
	// replacement lifecycle. Defer the fence until after stale-ready removal so
	// the fail-stop cleanup ordering remains identical to other ambiguities.
	absentOwnerIneligible := !ownerExists && (!deps.AllowAbsentOwner || record.InstallBootID == bootID)
	if ownerExists {
		if err := owner.Validate(); err != nil {
			_ = releaseLocks()
			return errors.Join(errSupervisorFenced, err)
		}
	}
	if err := validateSupervisorStarter(deps.StartMain); err != nil {
		_ = releaseLocks()
		return err
	}
	if err := validateSupervisorStarter(deps.StartAgent); err != nil {
		_ = releaseLocks()
		return err
	}

	// Stale readiness is removed before touching hardware.  A same-boot
	// replacement is ambiguous: it never resets, scans, signals, adopts, or
	// starts a successor.  The fail-stop helper still records the fence and
	// requests a reboot.
	if err := removeSupervisorReady(ctx, deps, readyStore); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, owner, nil, err, ownerUnlock, installUnlock)
	}
	if absentOwnerIneligible {
		return failStopSupervisor(ctx, deps, ownerStore, owner, nil, errors.New("owner record is absent on an ineligible boot"), ownerUnlock, installUnlock)
	}
	if ownerExists && owner.BootID == bootID {
		return failStopSupervisor(ctx, deps, ownerStore, owner, nil, errors.New("same-boot supervisor replacement"), ownerUnlock, installUnlock)
	}
	// A prior boot may leave a normal owner record behind.  Fence it before
	// successor-boot reconciliation; no hardware operation is attempted until
	// the durable record permits recovery_required -> no_owner.
	if ownerExists && owner.State != hardwareowner.StateRecoveryRequired {
		fenced := owner
		fenced.State, fenced.FirstFailure = hardwareowner.StateRecoveryRequired, "readiness_failed"
		if fenced.ActiveGeneration != 0 {
			fenced.QuiescingOwner = fenced.ActiveOwner
		}
		if err := ownerStore.Replace(fenced); err != nil {
			_ = releaseLocks()
			return errors.Join(errSupervisorFenced, err)
		}
		owner = fenced
	}

	if err := supervisorReset(ctx, deps.Reset); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, owner, nil, err, ownerUnlock, installUnlock)
	}
	var noOwner hardwareowner.Record
	if ownerExists {
		noOwner, err = reconcileNoOwner(owner, bootID)
	} else {
		noOwner, err = makeAbsentOwnerBootstrap(bootID)
	}
	if err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, owner, nil, err, ownerUnlock, installUnlock)
	}
	if err := ownerStore.Replace(noOwner); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, owner, nil, err, ownerUnlock, installUnlock)
	}
	starting, err := allocateNormalMainStartingWithSession(noOwner, bootID, deps.NewSession)
	if err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, noOwner, nil, err, ownerUnlock, installUnlock)
	}
	if err := ownerStore.Replace(starting); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, noOwner, nil, err, ownerUnlock, installUnlock)
	}

	lifecycle := &supervisorLifecycle{controller: deps.Children}
	mainHandle, err := invokeStartMainHandle(ctx, deps.StartMain)
	lifecycle.setMain(mainHandle)
	if err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, starting, lifecycle, err, ownerUnlock, installUnlock)
	}
	if lifecycle.main == nil {
		return failStopSupervisor(ctx, deps, ownerStore, starting, lifecycle, errors.New("StartMain did not return a retained child"), ownerUnlock, installUnlock)
	}
	readyCtx, cancelReady := context.WithTimeout(ctx, SupervisorReadinessTimeout)
	err = invokeMainReadiness(readyCtx, deps)
	cancelReady()
	if err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, starting, lifecycle, err, ownerUnlock, installUnlock)
	}
	normal := starting
	normal.State, normal.Phase = hardwareowner.StateNormalMain, ""
	normal.RunID = ""
	normal.CandidateSession, normal.CandidateGeneration, normal.CandidateMode, normal.CandidateOwner = "", 0, hardwareowner.ModeNone, hardwareowner.OwnerNone
	normal.QuiescingOwner, normal.RequestedResources, normal.FirstFailure = hardwareowner.OwnerNone, []string{}, ""
	if err := ownerStore.Replace(normal); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, starting, lifecycle, err, ownerUnlock, installUnlock)
	}

	agentCtx, cancelAgent := context.WithTimeout(ctx, SupervisorAgentTimeout)
	defer cancelAgent()
	receipt, agentHandle, err := invokeAgentAndReceiptWithHandle(agentCtx, deps.StartAgent, deps.ReadinessReceipt)
	lifecycle.setAgent(agentHandle)
	if err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, err, ownerUnlock, installUnlock)
	}
	if lifecycle.agent == nil {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, errors.New("StartAgent did not return a retained child"), ownerUnlock, installUnlock)
	}
	if err := receipt.Validate(); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, err, ownerUnlock, installUnlock)
	}
	if receipt.ProfileSHA256 != deps.ProfileSHA256 {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, errors.New("agent profile identity does not match supervisor profile"), ownerUnlock, installUnlock)
	}
	if err := invokeAgentAlive(agentCtx, deps, lifecycle); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, err, ownerUnlock, installUnlock)
	}
	mainIdentity, err := invokeMainAttestation(agentCtx, deps, lifecycle)
	if err != nil || !validSupervisorProcessTuple(mainIdentity) {
		if err == nil {
			err = errors.New("Main identity is incomplete")
		}
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, err, ownerUnlock, installUnlock)
	}
	supervisorIdentity, ok := processAttestation(deps.SupervisorIdentity)
	if !ok || !validSupervisorProcessTuple(supervisorIdentity) {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, errors.New("supervisor identity is incomplete"), ownerUnlock, installUnlock)
	}
	readyRecord, err := makeReadyRecordV3(bootID, record, normal, receipt, mainIdentity, supervisorIdentity, deps.ProfileSHA256)
	if err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, err, ownerUnlock, installUnlock)
	}
	if err := agentCtx.Err(); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, err, ownerUnlock, installUnlock)
	}
	if err := publishSupervisorReady(agentCtx, deps, readyStore, readyRecord); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, err, ownerUnlock, installUnlock)
	}
	if err := releaseLocks(); err != nil {
		return failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, err, nil, nil)
	}

	// Waiting is the final lifecycle phase.  A normal child exit is still a
	// failure because the supervisor requires exactly one live Main and agent.
	livenessErr := invokeSupervisorLiveness(ctx, deps, lifecycle)
	if livenessErr == nil {
		livenessErr = ErrSupervisorChildExited
	}
	installReacquired, ownerReacquired, lockErr := reacquireSupervisorAdmission(ctx, deps)
	cleanupErr := failStopSupervisor(ctx, deps, ownerStore, normal, lifecycle, livenessErr, ownerReacquired, installReacquired)
	return errors.Join(cleanupErr, lockErr)
}

func validSupervisorProcessTuple(identity ProcessAttestation) bool {
	return identity.PID != 0 && identity.StartTime != 0
}

func supervisorReadyStore(deps Dependencies) ReadyStoreV3 {
	if !admissionNil(deps.ReadyStore) {
		return deps.ReadyStore
	}
	if !admissionNil(deps.Ready) {
		return deps.Ready
	}
	if !admissionNil(deps.ReadyRecordStore) {
		return deps.ReadyRecordStore
	}
	if !admissionNil(deps.ReadyStoreV3) {
		return deps.ReadyStoreV3
	}
	return nil
}

func removeSupervisorReady(ctx context.Context, deps Dependencies, store ReadyStoreV3) error {
	if !admissionNil(store) {
		return store.Remove()
	}
	if !admissionNil(deps.RemoveReady) {
		return invokeMutation(ctx, deps.RemoveReady)
	}
	return errSupervisorDeps
}

func publishSupervisorReady(ctx context.Context, deps Dependencies, store ReadyStoreV3, record ReadyRecordV3) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !admissionNil(store) {
		err := store.Replace(record)
		if err == nil {
			err = ctx.Err()
		}
		return err
	}
	if admissionNil(deps.PublishReady) {
		return errSupervisorDeps
	}
	if fn, ok := deps.PublishReady.(func(context.Context, ReadyRecordV3) error); ok {
		err := fn(ctx, record)
		if err == nil {
			err = ctx.Err()
		}
		return err
	}
	if fn, ok := deps.PublishReady.(func(ReadyRecordV3) error); ok {
		err := fn(record)
		if err == nil {
			err = ctx.Err()
		}
		return err
	}
	err := invokeReflect(ctx, deps.PublishReady, record)
	if err == nil {
		err = ctx.Err()
	}
	return err
}

func makeReadyRecordV3(bootID string, journal InstallJournalRecord, owner hardwareowner.Record, receipt ReadinessReceipt, main, supervisor ProcessAttestation, profile string) (ReadyRecordV3, error) {
	if !installBootIDPattern.MatchString(bootID) {
		return ReadyRecordV3{}, errors.New("ready record boot ID is not canonical")
	}
	if !manifestHashPattern.MatchString(profile) {
		return ReadyRecordV3{}, errors.New("ready record profile hash is not canonical")
	}
	journalBytes, err := journal.MarshalCanonical()
	if err != nil {
		return ReadyRecordV3{}, fmt.Errorf("marshal terminal journal: %w", err)
	}
	digest := sha256.Sum256(journalBytes)
	record := ReadyRecordV3{
		Schema: 3, BootID: bootID, JournalSHA256: hex.EncodeToString(digest[:]), OwnerSession: owner.ActiveSession,
		OwnerGeneration: owner.ActiveGeneration, ProfileSHA256: profile, Capabilities: DevelopmentCapabilities(),
		SupervisorPID: supervisor.PID, SupervisorStartTime: supervisor.StartTime, MainPID: main.PID, MainStartTime: main.StartTime,
		AgentPID: receipt.PID, AgentStartTime: receipt.StartTime,
	}
	if err := record.Validate(); err != nil {
		return ReadyRecordV3{}, err
	}
	return record, nil
}

func failStopSupervisor(ctx context.Context, deps Dependencies, ownerStore interface {
	Replace(hardwareowner.Record) error
}, owner hardwareowner.Record, lifecycle *supervisorLifecycle, cause error, ownerUnlock, installUnlock hardwareowner.Unlock) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if lifecycle != nil {
		_ = lifecycle.closeReceipt()
	}
	// Ready/owner mutations are safe only while both admission locks are
	// proven held. A failed reacquisition or ambiguous unlock deliberately
	// leaves both records untouched, but still terminates retained children and
	// requests a reboot.
	locksProven := ownerUnlock != nil && installUnlock != nil
	var readyErr, fenceErr error
	if locksProven {
		readyErr = removeSupervisorReady(ctx, deps, supervisorReadyStore(deps))
		// An absent owner has no durable state that can be fenced. Likewise,
		// no_owner is the truthful checkpoint written after reset and cannot be
		// transitioned to recovery_required as a same-boot cleanup operation.
		if owner.Schema != 0 && owner.State != hardwareowner.StateNoOwner {
			fenceErr = fenceRecovery(ownerStore, owner, "readiness_failed")
		}
	}
	cleanupErr := lifecycle.terminate(ctx)
	rebootErr := requestSupervisorReboot(deps)
	releaseErr := releaseSupervisorAdmission(ownerUnlock, installUnlock)
	return errors.Join(errSupervisorFenced, cause, readyErr, fenceErr, cleanupErr, rebootErr, releaseErr)
}

func requestSupervisorReboot(deps Dependencies) error {
	value := deps.RebootRequester
	if admissionNil(value) {
		value = deps.Reboot
	}
	if admissionNil(value) {
		return errors.Join(ErrSupervisorRebootFailed, errSupervisorDeps)
	}
	ctx, cancel := context.WithTimeout(context.Background(), SupervisorAgentTimeout)
	defer cancel()
	var err error
	switch requester := value.(type) {
	case RebootRequester:
		err = requester.Request(ctx)
	case func(context.Context) error:
		err = requester(ctx)
	case func() error:
		err = requester()
	default:
		return errors.Join(ErrSupervisorRebootFailed, errSupervisorDeps)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return errors.Join(ErrSupervisorRebootFailed, err)
	}
	return nil
}

func acquireSupervisorInstallLock(ctx context.Context, deps Dependencies, path string) (*os.File, hardwareowner.Unlock, error) {
	inherited := deps.InheritedLockFDSet || deps.InheritedLockFD != 0
	if inherited {
		file, err := retainInheritedInstallLockFD(deps.InheritedLockFD, path)
		if err != nil {
			return nil, nil, errors.Join(errSupervisorFenced, err)
		}
		var once sync.Once
		var closeErr error
		return file, func() error {
			once.Do(func() { closeErr = file.Close() })
			return closeErr
		}, nil
	}
	unlock, err := lockDependency(ctx, deps.InstallLocker, path)
	return nil, unlock, err
}

func reacquireSupervisorAdmission(ctx context.Context, deps Dependencies) (hardwareowner.Unlock, hardwareowner.Unlock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lockCtx, cancel := context.WithTimeout(ctx, SupervisorChildCleanupTimeout)
	defer cancel()
	installUnlock, err := lockDependency(lockCtx, deps.InstallLocker, InstallLockPath)
	if err != nil {
		return nil, nil, err
	}
	ownerUnlock, err := lockDependency(lockCtx, deps.OwnerLocker, hardwareowner.LockPath)
	if err != nil {
		if installUnlock != nil {
			_ = installUnlock()
		}
		return nil, nil, err
	}
	return installUnlock, ownerUnlock, nil
}

func releaseSupervisorAdmission(ownerUnlock, installUnlock hardwareowner.Unlock) error {
	var ownerErr, installErr error
	if ownerUnlock != nil {
		ownerErr = ownerUnlock()
	}
	if installUnlock != nil {
		installErr = installUnlock()
	}
	return errors.Join(ownerErr, installErr)
}

func lockDependency(ctx context.Context, value any, defaultPath string) (hardwareowner.Unlock, error) {
	var locker interface {
		Lock(context.Context) (hardwareowner.Unlock, error)
	}
	if value == nil {
		locker = hardwareowner.NewLocker(defaultPath)
	} else if candidate, ok := value.(interface {
		Lock(context.Context) (hardwareowner.Unlock, error)
	}); ok {
		locker = candidate
	} else {
		return nil, errSupervisorDeps
	}
	unlocked, err := locker.Lock(ctx)
	if err != nil {
		if unlocked != nil {
			return nil, errors.Join(err, unlocked())
		}
		return nil, err
	}
	if unlocked == nil {
		return nil, errSupervisorDeps
	}
	return unlocked, nil
}

func stringCallback(ctx context.Context, value any) (string, error) {
	if admissionNil(value) {
		return "", errSupervisorDeps
	}
	if fn, ok := value.(func() (string, error)); ok {
		return fn()
	}
	if fn, ok := value.(func(context.Context) (string, error)); ok {
		return fn(ctx)
	}
	return "", errSupervisorDeps
}

func supervisorReset(ctx context.Context, value any) error {
	if admissionNil(value) {
		return errSupervisorDeps
	}
	if resetter, ok := value.(interface{ Reset(context.Context) error }); ok {
		return resetter.Reset(ctx)
	}
	if fn, ok := value.(func(context.Context) error); ok {
		return fn(ctx)
	}
	if fn, ok := value.(func() error); ok {
		return fn()
	}
	return errSupervisorDeps
}

// invokeStartMainHandle accepts both the historical callback-only starter and
// the retained-handle production boundary. The returned value is deliberately
// opaque here; supervisorLifecycle performs the private interface assertion so
// a malformed or callback-only fixture cannot smuggle a PID into cleanup.
func invokeStartMainHandle(ctx context.Context, value any) (any, error) {
	if admissionNil(value) {
		return nil, errSupervisorDeps
	}
	if fn, ok := value.(func(context.Context) (*ChildProcess, error)); ok {
		return fn(ctx)
	}
	if fn, ok := value.(func(context.Context) (retainedSupervisorChild, error)); ok {
		return fn(ctx)
	}
	if fn, ok := value.(func() (*ChildProcess, error)); ok {
		return fn()
	}
	if fn, ok := value.(func() (retainedSupervisorChild, error)); ok {
		return fn()
	}
	return nil, errSupervisorDeps
}

func validateSupervisorStarter(value any) error {
	if admissionNil(value) {
		return errSupervisorDeps
	}
	switch value.(type) {
	case func(context.Context) (*ChildProcess, error), func(context.Context) (retainedSupervisorChild, error):
		return nil
	case func() (*ChildProcess, error), func() (retainedSupervisorChild, error):
		return nil
	default:
		return errors.New("supervisor starter must return a retained child")
	}
}

func invokeStartMain(ctx context.Context, value any) error {
	_, err := invokeStartMainHandle(ctx, value)
	return err
}

func invokeReadiness(ctx context.Context, value any) error {
	if admissionNil(value) {
		return errSupervisorDeps
	}
	if fn, ok := value.(func(context.Context) error); ok {
		return fn(ctx)
	}
	if fn, ok := value.(func(context.Context) (bool, error)); ok {
		okReady, err := fn(ctx)
		if err != nil {
			return err
		}
		if !okReady {
			return errors.New("Main readiness is false")
		}
		return nil
	}
	return invokeReflect(ctx, value)
}

func invokeMainReadiness(ctx context.Context, deps Dependencies) error {
	checks := []any{deps.MainExecutableReady, deps.CommandFIFOReady, deps.FPGAManagerReady, deps.MenuReady}
	configured := false
	for _, check := range checks {
		if !admissionNil(check) {
			configured = true
			break
		}
	}
	if configured {
		for _, check := range checks {
			if admissionNil(check) {
				return errSupervisorDeps
			}
			if err := invokeReadiness(ctx, check); err != nil {
				return err
			}
		}
		return nil
	}
	return invokeReadiness(ctx, deps.MainReadiness)
}

func invokeAgentAndReceipt(ctx context.Context, start any, read any) (ReadinessReceipt, error) {
	receipt, _, err := invokeAgentAndReceiptWithHandle(ctx, start, read)
	return receipt, err
}

func invokeAgentAndReceiptWithHandle(ctx context.Context, start any, read any) (ReadinessReceipt, any, error) {
	if admissionNil(start) {
		return ReadinessReceipt{}, nil, errSupervisorDeps
	}
	agentHandle, err := invokeStartMainHandle(ctx, start)
	if err != nil {
		return ReadinessReceipt{}, agentHandle, err
	}
	if _, ok := retainedSupervisorChildFrom(agentHandle); !ok {
		return ReadinessReceipt{}, agentHandle, errors.New("StartAgent did not return a retained child")
	}
	if admissionNil(read) {
		return ReadinessReceipt{}, agentHandle, errSupervisorDeps
	}
	if fn, ok := read.(func(context.Context) (ReadinessReceipt, error)); ok {
		receipt, err := fn(ctx)
		return receipt, agentHandle, err
	}
	if fn, ok := read.(func(context.Context) ([]byte, error)); ok {
		raw, err := fn(ctx)
		if err != nil {
			return ReadinessReceipt{}, agentHandle, err
		}
		receipt, err := ParseReadinessReceipt(raw)
		return receipt, agentHandle, err
	}
	if fn, ok := read.(func(context.Context) (string, error)); ok {
		raw, err := fn(ctx)
		if err != nil {
			return ReadinessReceipt{}, agentHandle, err
		}
		receipt, err := ParseReadinessReceipt([]byte(raw))
		return receipt, agentHandle, err
	}
	receipt, err := invokeReflectReceipt(ctx, read)
	return receipt, agentHandle, err
}

func invokeAgentAlive(ctx context.Context, deps Dependencies, lifecycle *supervisorLifecycle) error {
	if !admissionNil(deps.AgentAlive) {
		return invokeAlive(ctx, deps.AgentAlive)
	}
	if lifecycle != nil && lifecycle.agent != nil {
		if alive, ok := lifecycle.agent.(interface{ Alive() error }); ok {
			return alive.Alive()
		}
	}
	return nil
}

func invokeMainAttestation(ctx context.Context, deps Dependencies, lifecycle *supervisorLifecycle) (ProcessAttestation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProcessAttestation{}, err
	}
	if lifecycle != nil && lifecycle.main != nil {
		identity, err := lifecycle.mainAttestation()
		if err != nil {
			return ProcessAttestation{}, err
		}
		return identity, nil
	}
	return ProcessAttestation{}, errors.New("Main identity is unavailable")
}

func invokeAlive(ctx context.Context, value any) error {
	if value == nil {
		return nil
	}
	if alive, ok := value.(interface{ Alive() error }); ok {
		return alive.Alive()
	}
	if fn, ok := value.(func(context.Context) error); ok {
		return fn(ctx)
	}
	if fn, ok := value.(func() error); ok {
		return fn()
	}
	if fn, ok := value.(func(context.Context) (bool, error)); ok {
		ready, err := fn(ctx)
		if err == nil && !ready {
			return errors.New("agent is not alive")
		}
		return err
	}
	if fn, ok := value.(func() (bool, error)); ok {
		ready, err := fn()
		if err == nil && !ready {
			return errors.New("agent is not alive")
		}
		return err
	}
	return invokeReflect(ctx, value)
}

func invokeSupervisorLiveness(ctx context.Context, deps Dependencies, lifecycle *supervisorLifecycle) error {
	if !admissionNil(deps.WaitLiveness) {
		if fn, ok := deps.WaitLiveness.(func(context.Context) error); ok {
			return fn(ctx)
		}
		if fn, ok := deps.WaitLiveness.(func() error); ok {
			return fn()
		}
		return invokeReflect(ctx, deps.WaitLiveness)
	}
	return lifecycle.wait(ctx)
}

func invokeReflect(ctx context.Context, value any, extra ...any) error {
	v := reflect.ValueOf(value)
	if !v.IsValid() || v.Kind() != reflect.Func {
		return errSupervisorDeps
	}
	args := []reflect.Value{}
	t := v.Type()
	if t.NumIn() == 1 {
		if t.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
			args = append(args, reflect.ValueOf(ctx))
		} else if len(extra) == 1 && reflect.TypeOf(extra[0]).AssignableTo(t.In(0)) {
			args = append(args, reflect.ValueOf(extra[0]))
		} else {
			return errSupervisorDeps
		}
	} else if t.NumIn() == 2 && t.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) && len(extra) == 1 && reflect.TypeOf(extra[0]).AssignableTo(t.In(1)) {
		args = append(args, reflect.ValueOf(ctx), reflect.ValueOf(extra[0]))
	} else {
		return errSupervisorDeps
	}
	out := v.Call(args)
	if len(out) == 0 {
		return nil
	}
	last := out[len(out)-1]
	if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		if (last.Kind() == reflect.Interface || last.Kind() == reflect.Pointer || last.Kind() == reflect.Func || last.Kind() == reflect.Map || last.Kind() == reflect.Slice) && last.IsNil() {
			return nil
		}
		if err, ok := last.Interface().(error); ok {
			return err
		}
	}
	if len(out) == 1 {
		return nil
	}
	return errSupervisorDeps
}
func invokeReflectReceipt(ctx context.Context, value any) (ReadinessReceipt, error) {
	v := reflect.ValueOf(value)
	if !v.IsValid() || v.Kind() != reflect.Func {
		return ReadinessReceipt{}, errSupervisorDeps
	}
	if v.Type().NumIn() != 1 || !v.Type().In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
		return ReadinessReceipt{}, errSupervisorDeps
	}
	out := v.Call([]reflect.Value{reflect.ValueOf(ctx)})
	if len(out) == 0 {
		return ReadinessReceipt{}, errSupervisorDeps
	}
	var err error
	if len(out) > 1 {
		last := out[len(out)-1]
		if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			switch last.Kind() {
			case reflect.Interface, reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice:
				if !last.IsNil() {
					err, _ = last.Interface().(error)
				}
			default:
				err, _ = last.Interface().(error)
			}
		} else {
			return ReadinessReceipt{}, errSupervisorDeps
		}
	}
	if err != nil {
		return ReadinessReceipt{}, err
	}
	valueOut := out[0]
	if valueOut.Type() == reflect.TypeOf(ReadinessReceipt{}) {
		return valueOut.Interface().(ReadinessReceipt), nil
	}
	if valueOut.Type() == reflect.TypeOf([]byte{}) {
		return ParseReadinessReceipt(valueOut.Bytes())
	}
	return ReadinessReceipt{}, errSupervisorDeps
}

func reconcileNoOwner(record hardwareowner.Record, bootID string) (hardwareowner.Record, error) {
	next := record
	next.BootID, next.State, next.Phase = bootID, hardwareowner.StateNoOwner, hardwareowner.PhaseMainAbsent
	// A successor boot proves that the previous operation's volatile candidate
	// cannot be adopted. Clear its session/run/resource intent before allocating
	// the fresh compatibility-Main tuple.
	next.RunID = ""
	next.ActiveSession, next.ActiveGeneration, next.ActiveMode, next.ActiveOwner = "", 0, hardwareowner.ModeNone, hardwareowner.OwnerNone
	next.ActiveLeases = []string{}
	next.QuiescingOwner = hardwareowner.OwnerNone
	next.CandidateSession, next.CandidateGeneration, next.CandidateMode, next.CandidateOwner = "", 0, hardwareowner.ModeNone, hardwareowner.OwnerNone
	next.RequestedResources = []string{}
	next.FirstFailure = ""
	return next, next.Validate()
}

func makeAbsentOwnerBootstrap(bootID string) (hardwareowner.Record, error) {
	next := hardwareowner.Record{
		Schema:              hardwareowner.SchemaVersion,
		State:               hardwareowner.StateNoOwner,
		Phase:               hardwareowner.PhaseMainAbsent,
		BootID:              bootID,
		GenerationHighWater: 1,
		ActiveMode:          hardwareowner.ModeNone,
		ActiveOwner:         hardwareowner.OwnerNone,
		ActiveLeases:        []string{},
		CandidateMode:       hardwareowner.ModeNone,
		CandidateOwner:      hardwareowner.OwnerNone,
		QuiescingOwner:      hardwareowner.OwnerNone,
		RequestedResources:  []string{},
		CandidateSession:    "",
		CandidateGeneration: 0,
		RunID:               "",
		FirstFailure:        "",
	}
	return next, next.Validate()
}

func allocateNormalMainStarting(record hardwareowner.Record, bootID string) (hardwareowner.Record, error) {
	return allocateNormalMainStartingWithSession(record, bootID, nil)
}

func allocateNormalMainStartingWithSession(record hardwareowner.Record, bootID string, newSession any) (hardwareowner.Record, error) {
	if record.GenerationHighWater == ^uint64(0) {
		return hardwareowner.Record{}, errors.New("generation high-water overflow")
	}
	session, err := invokeNewSession(newSession)
	if err != nil {
		return hardwareowner.Record{}, err
	}
	next := record
	next.BootID, next.State, next.Phase = bootID, hardwareowner.StateNormalMainStarting, ""
	next.GenerationHighWater++
	next.ActiveSession, next.ActiveGeneration, next.ActiveMode, next.ActiveOwner = session, next.GenerationHighWater, hardwareowner.ModeFPGANative, hardwareowner.OwnerCompatMain
	next.ActiveLeases = hardwareowner.NormalLeases()
	next.RunID, next.FirstFailure = "", ""
	next.CandidateSession, next.CandidateGeneration, next.CandidateMode, next.CandidateOwner = "", 0, hardwareowner.ModeNone, hardwareowner.OwnerNone
	next.QuiescingOwner, next.RequestedResources = hardwareowner.OwnerNone, []string{}
	return next, next.Validate()
}

func invokeNewSession(value any) (string, error) {
	if value == nil {
		return randomSession()
	}
	if fn, ok := value.(func() (string, error)); ok {
		return fn()
	}
	if fn, ok := value.(func(context.Context) (string, error)); ok {
		return fn(context.Background())
	}
	return "", errSupervisorDeps
}

func fenceRecovery(store interface {
	Replace(hardwareowner.Record) error
}, record hardwareowner.Record, code string) error {
	next := record
	next.State, next.FirstFailure = hardwareowner.StateRecoveryRequired, code
	// A failed Main/agent startup still has the compatibility active tuple in
	// the durable record. Recovery-required must name that tuple as the
	// quiescing owner or the owner schema correctly rejects an ambiguous fence.
	if next.ActiveGeneration != 0 {
		next.QuiescingOwner = next.ActiveOwner
	}
	return store.Replace(next)
}

func processAttestation(value any) (ProcessAttestation, bool) {
	switch identity := value.(type) {
	case ProcessAttestation:
		return identity, true
	case ProcessIdentity:
		return ProcessAttestation{PID: uint64(identity.PID), StartTime: identity.StartTime, Device: identity.Device, Inode: identity.Inode, SHA256: identity.SHA256}, true
	case func() (ProcessAttestation, error):
		resolved, err := identity()
		return resolved, err == nil
	case func(context.Context) (ProcessAttestation, error):
		resolved, err := identity(context.Background())
		return resolved, err == nil
	case func() (ProcessIdentity, error):
		resolved, err := identity()
		if err != nil {
			return ProcessAttestation{}, false
		}
		return ProcessAttestation{PID: uint64(resolved.PID), StartTime: resolved.StartTime, Device: resolved.Device, Inode: resolved.Inode, SHA256: resolved.SHA256}, true
	case func(context.Context) (ProcessIdentity, error):
		resolved, err := identity(context.Background())
		if err != nil {
			return ProcessAttestation{}, false
		}
		return ProcessAttestation{PID: uint64(resolved.PID), StartTime: resolved.StartTime, Device: resolved.Device, Inode: resolved.Inode, SHA256: resolved.SHA256}, true
	case map[string]uint64:
		return ProcessAttestation{PID: identity["pid"], StartTime: identity["start_time"], Device: identity["device"], Inode: identity["inode"], SHA256: strings.Repeat("0", 64)}, true
	case map[string]string:
		return ProcessAttestation{SHA256: identity["sha256"]}, true
	default:
		return ProcessAttestation{}, false
	}
}

func validProcessAttestation(identity ProcessAttestation) bool {
	return identity.PID != 0 && identity.StartTime != 0 && identity.Device != 0 && identity.Inode != 0 && manifestHashPattern.MatchString(identity.SHA256)
}

func resolvedFromInventory(inv InventoryV1) ResolvedInventoryV1 {
	convert := func(p PathExpectation, required bool) ResolvedPathV1 {
		state := "present"
		if p.Path == "" && !required {
			state = "expected_absent"
		}
		return ResolvedPathV1{Path: p.Path, Kind: p.Kind, State: state, Device: 1, Inode: 1, Rdev: p.Rdev, SHA256: p.SHA256}
	}
	r := ResolvedInventoryV1{Schema: 1, MainExecutable: convert(inv.MainExecutable, true), MainFIFO: convert(inv.MainFIFO, true), StartSources: []ResolvedSourceV1{}}
	if inv.CastExecutable != nil {
		p := convert(*inv.CastExecutable, false)
		r.CastExecutable = &p
	}
	if inv.InputUInput != nil {
		p := convert(*inv.InputUInput, false)
		r.InputUInput = &p
	}
	if inv.CastFramebuffer != nil {
		p := convert(*inv.CastFramebuffer, false)
		r.CastFramebuffer = &p
	}
	if inv.CastNativeCommand != nil {
		p := convert(*inv.CastNativeCommand, false)
		r.CastNativeCommand = &p
	}
	if inv.CastTokenFile != nil {
		p := convert(*inv.CastTokenFile, false)
		r.CastTokenFile = &p
	}
	r.InputListen, r.CastRTP, r.CastControl = inv.InputListen, inv.CastRTP, inv.CastControl
	for _, source := range inv.StartSources {
		state := source.DisabledState
		device, inode, digest := uint64(1), uint64(1), source.DisabledSHA256
		switch source.DisabledState {
		case "absent":
			state, device, inode, digest = "disabled_absent", 0, 0, ""
		case "inert_replacement":
			state = "disabled_inert"
		case "approved_trampoline":
			state = "approved_trampoline"
		}
		r.StartSources = append(r.StartSources, ResolvedSourceV1{Path: source.Path, Kind: source.Kind, State: state, Device: device, Inode: inode, SHA256: digest})
	}
	return r
}

// ResolveInventory performs the strict post-readiness, pre-dispatch
// observation used by the production supervisor. The platform implementation
// binds every path through a held metadata-only descriptor and scans the
// configured process/descriptor population before returning.
func ResolveInventory(inv InventoryV1) (ResolvedInventoryV1, error) {
	return ResolveInventoryAt(context.Background(), inv, InventoryResolutionOptions{
		Phase:                  InventoryPhasePreDispatch,
		RequireProcessEvidence: true,
		ScanProcessDescriptors: true,
		RequireNetworkEvidence: true,
		RequireNetworkAbsence:  true,
		RequireMainFIFOOwner:   true,
	})
}

// ResolveInventoryContext is the bounded-context production entry point. The
// caller's deadline covers every filesystem, process, and endpoint read.
func ResolveInventoryContext(ctx context.Context, inv InventoryV1) (ResolvedInventoryV1, error) {
	return ResolveInventoryAt(ctx, inv, InventoryResolutionOptions{
		Phase:                  InventoryPhasePreDispatch,
		RequireProcessEvidence: true,
		ScanProcessDescriptors: true,
		RequireNetworkEvidence: true,
		RequireNetworkAbsence:  true,
		RequireMainFIFOOwner:   true,
	})
}

// ResolveInventoryAt resolves an inventory with explicit boot-local
// observation controls. ProcRoot and the relaxed evidence switches are only
// for deterministic hostile software fixtures; production uses the strict
// defaults above.
func ResolveInventoryAt(ctx context.Context, inv InventoryV1, options InventoryResolutionOptions) (ResolvedInventoryV1, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ResolvedInventoryV1{}, err
	}
	if err := inv.Validate(); err != nil {
		return ResolvedInventoryV1{}, err
	}
	if options.Phase == "" {
		options.Phase = InventoryPhasePreDispatch
	}
	if options.ProcRoot == "" {
		options.ProcRoot = "/proc"
	}
	result, err := resolveInventoryPlatform(ctx, inv, options)
	if err != nil {
		return ResolvedInventoryV1{}, err
	}
	if err := result.Validate(); err != nil {
		return ResolvedInventoryV1{}, err
	}
	if !(BootProof{ResolvedInventory: result}).ResolvedInventoryMatches(inv) {
		return ResolvedInventoryV1{}, errors.New("resolved inventory does not match immutable inventory")
	}
	if options.Prior != nil {
		if err := validateResolvedInventoryContinuity(*options.Prior, result); err != nil {
			return ResolvedInventoryV1{}, err
		}
	}
	return result, nil
}

// RevalidateResolvedInventory performs a same-boot re-observation. It rejects
// appearance of an expected-absent route and any boot-local identity change.
func RevalidateResolvedInventory(ctx context.Context, inv InventoryV1, prior ResolvedInventoryV1, options InventoryResolutionOptions) error {
	options.Prior = &prior
	current, err := ResolveInventoryAt(ctx, inv, options)
	if err != nil {
		return err
	}
	left, err := prior.MarshalCanonical()
	if err != nil {
		return err
	}
	right, err := current.MarshalCanonical()
	if err != nil {
		return err
	}
	if !bytes.Equal(left, right) {
		return errors.New("resolved inventory changed during same boot")
	}
	return nil
}

// ResolveInventoryV1 is a descriptive alias retained for adapters that name
// the schema explicitly.
func ResolveInventoryV1(inv InventoryV1) (ResolvedInventoryV1, error) {
	return ResolveInventory(inv)
}

// These wrappers retain the package-private fixture seam while routing all
// concrete observations through the platform descriptor implementation.
func resolveInventoryPath(want PathExpectation, required bool) (ResolvedPathV1, error) {
	return resolveInventoryPathPlatform(context.Background(), want, required, InventoryResolutionOptions{Phase: InventoryPhasePreDispatch})
}

func resolveInventorySource(source SourceRecord) (ResolvedSourceV1, error) {
	return resolveInventorySourcePlatform(context.Background(), source, InventoryResolutionOptions{Phase: InventoryPhasePreDispatch})
}

func validateResolvedInventoryContinuity(prior, current ResolvedInventoryV1) error {
	if prior.Schema != current.Schema {
		return errors.New("resolved inventory schema changed during same boot")
	}
	comparePath := func(label string, left, right ResolvedPathV1) error {
		if left.Path != right.Path {
			return fmt.Errorf("%s path changed during same boot", label)
		}
		if left.State == "expected_absent" && right.State != "expected_absent" {
			return fmt.Errorf("%s expected-absent route appeared", label)
		}
		if left.State == "present" && (right.State != "present" || left.Device != right.Device || left.Inode != right.Inode) {
			return fmt.Errorf("%s identity changed during same boot", label)
		}
		return nil
	}
	if err := comparePath("main_executable", prior.MainExecutable, current.MainExecutable); err != nil {
		return err
	}
	if err := comparePath("main_fifo", prior.MainFIFO, current.MainFIFO); err != nil {
		return err
	}
	for _, pair := range []struct {
		label string
		left  *ResolvedPathV1
		right *ResolvedPathV1
	}{
		{"cast_executable", prior.CastExecutable, current.CastExecutable},
		{"input_uinput", prior.InputUInput, current.InputUInput},
		{"cast_framebuffer", prior.CastFramebuffer, current.CastFramebuffer},
		{"cast_native_command", prior.CastNativeCommand, current.CastNativeCommand},
		{"cast_token_file", prior.CastTokenFile, current.CastTokenFile},
	} {
		if (pair.left == nil) != (pair.right == nil) {
			return fmt.Errorf("%s binding changed during same boot", pair.label)
		}
		if pair.left != nil {
			if err := comparePath(pair.label, *pair.left, *pair.right); err != nil {
				return err
			}
		}
	}
	if len(prior.StartSources) != len(current.StartSources) {
		return errors.New("start-source population changed during same boot")
	}
	for index := range prior.StartSources {
		left, right := prior.StartSources[index], current.StartSources[index]
		if left.Path != right.Path || left.State != right.State || left.Device != right.Device || left.Inode != right.Inode || left.SHA256 != right.SHA256 {
			return fmt.Errorf("start source %d changed during same boot", index)
		}
	}
	return nil
}

func inventoryFileKind(mode os.FileMode) string {
	switch {
	case mode.IsRegular():
		return "regular"
	case mode&os.ModeNamedPipe != 0:
		return "fifo"
	case mode&os.ModeCharDevice != 0:
		return "character"
	case mode&os.ModeDevice != 0:
		return "block"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode.IsDir():
		return "directory"
	default:
		return ""
	}
}

func hashRegularInventoryFile(path string, info os.FileInfo) (string, error) {
	if !info.Mode().IsRegular() {
		return "", nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, copyErr := io.Copy(digest, file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) {
		if err == nil {
			err = errors.New("resolved inventory file changed while hashed")
		}
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

var _ = reflect.TypeOf
var _ = strings.TrimSpace

// BootProofStore applies the same protected-file contract as the install
// journal, with the tighter proof size bound and an explicit Remove used on
// supervisor/agent death.  It is intentionally tiny so admission code cannot
// accidentally bypass proof parsing.
type BootProofStore struct {
	Path        string
	ExpectedUID uint32
}

func NewBootProofStore(path string, expectedUID ...uint32) *BootProofStore {
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	return &BootProofStore{Path: path, ExpectedUID: uid}
}
func NewProductionBootProofStore() *BootProofStore { return NewBootProofStore(BootProofPath) }

func (s *BootProofStore) Load() (BootProof, bool, error) {
	if err := validateProofPath(s); err != nil {
		return BootProof{}, false, err
	}
	if s == nil {
		return BootProof{}, false, errSupervisorDeps
	}
	parent := filepathDir(s.Path)
	if err := ensureNoSymlinkComponents(parent); err != nil {
		return BootProof{}, false, err
	}
	if err := validateSecureJournalDir(parent, s.ExpectedUID); err != nil {
		return BootProof{}, false, err
	}
	info, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return BootProof{}, false, nil
	}
	if err != nil {
		return BootProof{}, false, err
	}
	if err := validateJournalFileInfo(info, s.ExpectedUID); err != nil {
		return BootProof{}, true, err
	}
	f, err := os.Open(s.Path)
	if err != nil {
		return BootProof{}, true, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, BootProofMaxBytes+1))
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return BootProof{}, true, err
	}
	if len(raw) > BootProofMaxBytes {
		return BootProof{}, true, errors.New("boot proof exceeds size bound")
	}
	proof, err := ParseBootProof(raw)
	return proof, true, err
}

func (s *BootProofStore) Replace(proof BootProof) error {
	if err := validateProofPath(s); err != nil {
		return err
	}
	raw, err := proof.MarshalCanonical()
	if err != nil {
		return err
	}
	parent := filepathDir(s.Path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return err
	}
	if err := validateSecureJournalDir(parent, s.ExpectedUID); err != nil {
		return err
	}
	if previous, exists, err := s.Load(); err != nil {
		return err
	} else if exists && previous.BootID == proof.BootID {
		return errors.New("boot proof already published for this boot")
	}
	tmp, err := os.CreateTemp(parent, ".fpgadev-boot-v1.tmp-")
	if err != nil {
		return err
	}
	path := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(path, s.Path); err != nil {
		return err
	}
	keep = true
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	syncErr, closeErr := dir.Sync(), dir.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return err
	}
	info, err := os.Lstat(s.Path)
	if err != nil {
		return err
	}
	return validateJournalFileInfo(info, s.ExpectedUID)
}

func (s *BootProofStore) Remove() error {
	if err := validateProofPath(s); err != nil {
		return err
	}
	parent := filepathDir(s.Path)
	if err := validateSecureJournalDir(parent, s.ExpectedUID); err != nil {
		// /run is boot-local.  A clean boot may have no proof directory yet;
		// absence means there is no proof to invalidate, while every other
		// metadata failure remains fail-closed.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	syncErr, closeErr := dir.Sync(), dir.Close()
	return errors.Join(syncErr, closeErr)
}

func validateProofPath(s *BootProofStore) error {
	if s == nil || s.Path == "" || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path || filepathDir(s.Path) == "." {
		return errSupervisorDeps
	}
	return nil
}

func filepathDir(path string) string {
	idx := strings.LastIndexByte(path, '/')
	if idx < 0 {
		return "."
	}
	if idx == 0 {
		return "/"
	}
	return path[:idx]
}

type quiescenceDependencies struct {
	Proof     *BootProofStore
	BootID    any
	CheckLive any
	PostMain  any
	Pressed   any
}

type bootQuiescenceVerifier struct{ dependencies quiescenceDependencies }

// NewQuiescenceVerifier composes the concrete boot-proof verifier. Optional
// arguments are accepted as the proof store, boot-ID reader, and observation
// callbacks; absent callbacks fail closed for checks that need them.
func NewQuiescenceVerifier(args ...any) QuiescenceVerifier {
	d := quiescenceDependencies{Proof: NewProductionBootProofStore(), BootID: readKernelBootID}
	for _, arg := range args {
		switch value := arg.(type) {
		case *BootProofStore:
			d.Proof = value
		case func() (string, error):
			d.BootID = value
		case func(context.Context) (string, error):
			d.BootID = value
		case func(context.Context, BootProof) error:
			d.CheckLive = value
		case func(context.Context) (PolicySubsystemProof, error):
			d.PostMain = value
		case func(context.Context) error:
			d.Pressed = value
		}
	}
	return &bootQuiescenceVerifier{dependencies: d}
}
func NewProductionQuiescenceVerifier() QuiescenceVerifier {
	return NewQuiescenceVerifier(productionLiveProofCheck)
}

func (v *bootQuiescenceVerifier) proofFor(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) (BootProof, error) {
	return v.proofForPhase(ctx, status, owner, true)
}

func (v *bootQuiescenceVerifier) proofForPhase(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record, requireLive bool) (BootProof, error) {
	if v == nil || v.dependencies.Proof == nil || v.dependencies.BootID == nil {
		return BootProof{}, errSupervisorDeps
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return BootProof{}, err
	}
	if err := status.Validate(); err != nil {
		return BootProof{}, err
	}
	if err := owner.Validate(); err != nil {
		return BootProof{}, err
	}
	if owner.State != hardwareowner.StateNormalMain {
		return BootProof{}, errors.New("owner is not canonical normal_main")
	}
	bootID, err := quiescenceBootID(ctx, v.dependencies.BootID)
	if err != nil {
		return BootProof{}, err
	}
	proof, exists, err := v.dependencies.Proof.Load()
	if err != nil {
		return BootProof{}, err
	}
	if !exists {
		return BootProof{}, errors.New("boot proof is absent")
	}
	if err := proof.Validate(); err != nil {
		return BootProof{}, err
	}
	if owner.BootID != bootID || proof.BootID != bootID || proof.OwnerSession != owner.ActiveSession || proof.OwnerGeneration != owner.ActiveGeneration || proof.JournalSHA256 != status.TerminalJournalSHA256 {
		return BootProof{}, errors.New("boot proof is stale or mismatched")
	}
	if !proof.ResolvedInventoryMatches(status.Inventory) {
		return BootProof{}, errors.New("boot proof inventory does not match terminal journal")
	}
	if requireLive && v.dependencies.CheckLive != nil {
		if err := invokeReflect(ctx, v.dependencies.CheckLive, proof); err != nil {
			return BootProof{}, err
		}
	}
	return proof, nil
}

func quiescenceBootID(ctx context.Context, value any) (string, error) {
	switch fn := value.(type) {
	case func() (string, error):
		return fn()
	case func(context.Context) (string, error):
		return fn(ctx)
	default:
		return "", errSupervisorDeps
	}
}

func (p BootProof) ResolvedInventoryMatches(inv InventoryV1) bool {
	if inv.Identity != "" {
		// Identity-only inventories are the private Task 7 fixture seam. They
		// intentionally have no persisted resolved representation, so a concrete
		// proof must fail closed instead of treating every opaque value as a
		// match.
		return false
	}
	resolved := p.ResolvedInventory
	if resolved.Schema != inv.Schema || !resolvedPathMatches(resolved.MainExecutable, inv.MainExecutable, true) || !resolvedPathMatches(resolved.MainFIFO, inv.MainFIFO, true) {
		return false
	}
	if !optionalResolvedPathMatches(resolved.CastExecutable, inv.CastExecutable) ||
		!optionalResolvedPathMatches(resolved.InputUInput, inv.InputUInput) ||
		!optionalResolvedPathMatches(resolved.CastFramebuffer, inv.CastFramebuffer) ||
		!optionalResolvedPathMatches(resolved.CastNativeCommand, inv.CastNativeCommand) ||
		!optionalResolvedPathMatches(resolved.CastTokenFile, inv.CastTokenFile) ||
		!networkMatches(resolved.InputListen, inv.InputListen) ||
		!networkMatches(resolved.CastRTP, inv.CastRTP) ||
		!networkMatches(resolved.CastControl, inv.CastControl) ||
		len(resolved.StartSources) != len(inv.StartSources) {
		return false
	}
	for n, source := range inv.StartSources {
		got := resolved.StartSources[n]
		if got.Path != source.Path || got.Kind != source.Kind {
			return false
		}
		wantState := source.DisabledState
		switch source.DisabledState {
		case "absent":
			wantState = "disabled_absent"
		case "inert_replacement":
			wantState = "disabled_inert"
		case "approved_trampoline":
			wantState = "approved_trampoline"
		}
		if got.State != wantState {
			return false
		}
		if got.State == "disabled_absent" {
			if got.SHA256 != "" {
				return false
			}
		} else if got.SHA256 != source.DisabledSHA256 {
			return false
		}
	}
	return true
}

func resolvedPathMatches(got ResolvedPathV1, want PathExpectation, required bool) bool {
	if got.Path != want.Path || got.Kind != want.Kind || got.Rdev != want.Rdev {
		return false
	}
	if required && got.State != "present" {
		return false
	}
	if got.State == "present" {
		return want.SHA256 == "" || got.SHA256 == want.SHA256
	}
	return got.State == "expected_absent" && !required && got.Device == 0 && got.Inode == 0 && got.SHA256 == ""
}

func optionalResolvedPathMatches(got *ResolvedPathV1, want *PathExpectation) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return resolvedPathMatches(*got, *want, false)
}

func networkMatches(got *ResolvedNetworkV1, want *NetworkExpectation) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Network == want.Network && got.Address == want.Address
}

func (v *bootQuiescenceVerifier) VerifyPreDispatch(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) error {
	_, err := v.proofFor(ctx, status, owner)
	return err
}
func (v *bootQuiescenceVerifier) VerifyPostMain(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) (PolicySubsystemProof, error) {
	if _, err := v.proofForPhase(ctx, status, owner, false); err != nil {
		return PolicySubsystemProof{}, err
	}
	if v.dependencies.PostMain != nil {
		if proof, ok := v.dependencies.PostMain.(func(context.Context) (PolicySubsystemProof, error)); ok {
			return proof(ctx)
		}
	}
	return PolicySubsystemProof{}, ErrQuiescenceEvidenceUnavailable
}
func (v *bootQuiescenceVerifier) VerifyPressedInput(ctx context.Context, status MaintenanceStatus, owner hardwareowner.Record) error {
	if _, err := v.proofForPhase(ctx, status, owner, false); err != nil {
		return err
	}
	if v.dependencies.Pressed != nil {
		return invokeMutation(ctx, v.dependencies.Pressed)
	}
	return ErrQuiescenceEvidenceUnavailable
}

var _ QuiescenceVerifier = (*bootQuiescenceVerifier)(nil)
