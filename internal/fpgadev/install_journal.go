//go:build fpgadev

package fpgadev

// This file owns the persistent maintenance journal and the immutable
// installation inventory.  The journal is deliberately independent from the
// hardware-owner record: a terminal install record describes what was
// changed on disk, while the owner record describes the current boot's
// volatile owner.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

const (
	InstallJournalPath = "/var/lib/fogcast/fpgadev-install-v1.json"
	InstallBackupDir   = "/var/lib/fogcast/fpgadev-install-v1-backups"
	InstallLockPath    = "/var/lock/fogcast/fpgadev-install.lock"
	BootProofPath      = "/run/fogcast/fpgadev-boot-v1.json"
	// InstallJournalMaxBytes bounds durable JSON and related control metadata.
	InstallJournalMaxBytes = 1 << 20
	// ProtectedRegularMaxBytes bounds descriptor-held protected artifact reads
	// and hashes. Eight MiB covers the 7,471,266-byte staged mister-agent while
	// the independent durable journal cap remains one MiB; larger files fail
	// closed.
	ProtectedRegularMaxBytes = 8 << 20
	BootProofMaxBytes        = 16 << 10
	ReadinessReceiptMaxBytes = 1024
)

var installBootIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// InstallState is the coarse durable source-replacement state machine. State
// names are part of the on-disk protocol. The four historical fine-grained
// names remain source-compatible aliases for old in-process callers, but they
// all serialize as the one coarse `installed` checkpoint and are never
// accepted from disk under their old spellings.
type InstallState string

const (
	InstallStatePrepared     InstallState = "prepared"
	InstallStateInstalled    InstallState = "installed"
	InstallStateTerminal     InstallState = "terminal"
	InstallStateUninstalling InstallState = "uninstalling"
	InstallStateRestored     InstallState = "restored"

	// Compatibility aliases for callers from the pre-amendment implementation.
	// They intentionally have the coarse value so no old state can be emitted.
	InstallStateTrampolineInstalled InstallState = InstallStateInstalled
	InstallStateOwnerInitialized    InstallState = InstallStateInstalled
	InstallStateSourcesDisabled     InstallState = InstallStateInstalled
	InstallStateSupervisorInstalled InstallState = InstallStateInstalled
)

// JournalState is kept as a spelling alias for callers that describe the
// state machine rather than the file it is persisted in.
type JournalState = InstallState

const (
	JournalPrepared     = InstallStatePrepared
	JournalInstalled    = InstallStateInstalled
	JournalTerminal     = InstallStateTerminal
	JournalUninstalling = InstallStateUninstalling
	JournalRestored     = InstallStateRestored

	// Deprecated compatibility aliases; these do not create additional
	// durable states.
	JournalTrampolineInstalled = InstallStateInstalled
	JournalOwnerInitialized    = InstallStateInstalled
	JournalSourcesDisabled     = InstallStateInstalled
	JournalSupervisorInstalled = InstallStateInstalled
)

var installStates = map[InstallState]int{
	InstallStatePrepared:     1,
	InstallStateInstalled:    2,
	InstallStateTerminal:     3,
	InstallStateUninstalling: 4,
	InstallStateRestored:     5,
}

// PathExpectation is the stable, reboot-independent identity of a configured
// path. Device/inode are intentionally absent: those values are volatile and
// are recorded only in ResolvedInventoryV1 in the current-boot proof.
type PathExpectation struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Rdev   uint64 `json:"rdev"`
	SHA256 string `json:"sha256"`
}

// PathBinding and InventoryPath are descriptive compatibility aliases.
type PathBinding = PathExpectation
type InventoryPath = PathExpectation

type NetworkExpectation struct {
	Network string `json:"network"`
	Address string `json:"address"`
}

type NetworkBinding = NetworkExpectation

// SourceRecord is one launch source plus its protected byte-identical backup.
// Mode is the complete permission mode (including special bits, which are
// rejected by validation unless a caller explicitly records a normal mode).
type SourceRecord struct {
	Path           string `json:"path"`
	Kind           string `json:"kind"`
	Mode           uint32 `json:"mode"`
	SHA256         string `json:"sha256"`
	BackupPath     string `json:"backup_path"`
	BackupSHA256   string `json:"backup_sha256"`
	DisabledState  string `json:"disabled_state"`
	DisabledSHA256 string `json:"disabled_sha256"`
}

type StartSource = SourceRecord
type LaunchSource = SourceRecord

// InventoryV1 is the closed v1 inventory derived from the protected previous
// agent configuration. Identity is a compatibility-only in-memory attestation
// used by the Task 7 fixture seam; it is never serialized. Concrete Task 8
// records use the canonical fields below.
type InventoryV1 struct {
	Identity          string              `json:"-"`
	Schema            uint64              `json:"schema"`
	MainExecutable    PathExpectation     `json:"main_executable"`
	CastExecutable    *PathExpectation    `json:"cast_executable"`
	InputUInput       *PathExpectation    `json:"input_uinput"`
	CastFramebuffer   *PathExpectation    `json:"cast_framebuffer"`
	CastNativeCommand *PathExpectation    `json:"cast_native_command"`
	CastTokenFile     *PathExpectation    `json:"cast_token_file"`
	InputListen       *NetworkExpectation `json:"input_listen"`
	CastRTP           *NetworkExpectation `json:"cast_rtp"`
	CastControl       *NetworkExpectation `json:"cast_control"`
	MainFIFO          PathExpectation     `json:"main_fifo"`
	StartSources      []SourceRecord      `json:"start_sources"`
}

var inventoryFields = [...]string{
	"schema", "main_executable", "cast_executable", "input_uinput",
	"cast_framebuffer", "cast_native_command", "cast_token_file",
	"input_listen", "cast_rtp", "cast_control", "main_fifo", "start_sources",
}

var pathExpectationFields = [...]string{"path", "kind", "rdev", "sha256"}
var networkExpectationFields = [...]string{"network", "address"}
var sourceFields = [...]string{"path", "kind", "mode", "sha256", "backup_path", "backup_sha256", "disabled_state", "disabled_sha256"}

func (i InventoryV1) Validate() error {
	// Task 7's anonymous semantic fixtures intentionally carry only Identity.
	// Preserve that private seam while requiring every concrete Task 8 record to
	// satisfy the closed schema.
	if i.Identity != "" && i.Schema == 0 && i.MainExecutable.Path == "" && i.MainFIFO.Path == "" && i.StartSources == nil {
		return nil
	}
	if i.Schema != 1 {
		return errors.New("inventory schema must be 1")
	}
	if err := i.MainExecutable.Validate(true); err != nil {
		return fmt.Errorf("main_executable: %w", err)
	}
	if i.MainExecutable.Kind != "regular" {
		return errors.New("main_executable must be a regular file")
	}
	if err := validateOptionalPath(i.CastExecutable, true); err != nil {
		return fmt.Errorf("cast_executable: %w", err)
	}
	if i.CastExecutable != nil && (i.CastExecutable.Kind != "regular" || i.CastExecutable.SHA256 == "") {
		return errors.New("cast_executable must be a hashed regular file")
	}
	if err := validateOptionalPath(i.InputUInput, true); err != nil {
		return fmt.Errorf("input_uinput: %w", err)
	}
	if err := validateOptionalPath(i.CastFramebuffer, true); err != nil {
		return fmt.Errorf("cast_framebuffer: %w", err)
	}
	if err := validateOptionalPath(i.CastNativeCommand, true); err != nil {
		return fmt.Errorf("cast_native_command: %w", err)
	}
	if err := validateOptionalPath(i.CastTokenFile, true); err != nil {
		return fmt.Errorf("cast_token_file: %w", err)
	}
	if err := validateOptionalNetwork(i.InputListen); err != nil {
		return fmt.Errorf("input_listen: %w", err)
	}
	if err := validateOptionalNetwork(i.CastRTP); err != nil {
		return fmt.Errorf("cast_rtp: %w", err)
	}
	if err := validateOptionalNetwork(i.CastControl); err != nil {
		return fmt.Errorf("cast_control: %w", err)
	}
	if err := i.MainFIFO.Validate(true); err != nil {
		return fmt.Errorf("main_fifo: %w", err)
	}
	if i.MainFIFO.Kind != "fifo" {
		return errors.New("main_fifo must be a fifo")
	}
	if i.StartSources == nil || len(i.StartSources) > 16 {
		return errors.New("inventory start_sources must be present and contain at most 16 entries")
	}
	for n, source := range i.StartSources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("start_sources[%d]: %w", n, err)
		}
		if n > 0 && i.StartSources[n-1].Path >= source.Path {
			return errors.New("inventory start_sources must be path sorted and duplicate-free")
		}
	}
	return nil
}

func (i InventoryV1) Equal(other InventoryV1) bool {
	if i.Identity != "" || other.Identity != "" {
		return i.Identity != "" && i.Identity == other.Identity
	}
	left, lerr := i.MarshalCanonical()
	right, rerr := other.MarshalCanonical()
	return lerr == nil && rerr == nil && bytes.Equal(left, right)
}

func (i InventoryV1) MarshalCanonical() ([]byte, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(i)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func ParseInventory(raw []byte) (InventoryV1, error) {
	var inventory InventoryV1
	err := decodeStrictObject(raw, inventoryFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &inventory.Schema)
		case "main_executable":
			return decodePathExpectation(value, &inventory.MainExecutable, false)
		case "cast_executable":
			return decodeOptionalPath(value, &inventory.CastExecutable)
		case "input_uinput":
			return decodeOptionalPath(value, &inventory.InputUInput)
		case "cast_framebuffer":
			return decodeOptionalPath(value, &inventory.CastFramebuffer)
		case "cast_native_command":
			return decodeOptionalPath(value, &inventory.CastNativeCommand)
		case "cast_token_file":
			return decodeOptionalPath(value, &inventory.CastTokenFile)
		case "input_listen":
			return decodeOptionalNetwork(value, &inventory.InputListen)
		case "cast_rtp":
			return decodeOptionalNetwork(value, &inventory.CastRTP)
		case "cast_control":
			return decodeOptionalNetwork(value, &inventory.CastControl)
		case "main_fifo":
			return decodePathExpectation(value, &inventory.MainFIFO, false)
		case "start_sources":
			return decodeSources(value, &inventory.StartSources)
		default:
			return errors.New("unknown inventory field")
		}
	})
	if err != nil {
		return InventoryV1{}, err
	}
	if err := inventory.Validate(); err != nil {
		return InventoryV1{}, err
	}
	canonical, err := inventory.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return InventoryV1{}, errors.New("inventory JSON is not canonical")
	}
	return inventory, nil
}

func validateString(value, field string, required bool) error {
	if !utf8.ValidString(value) || len([]byte(value)) > 255 {
		return fmt.Errorf("%s must be valid UTF-8 of at most 255 bytes", field)
	}
	if required && value == "" {
		return fmt.Errorf("%s is required", field)
	}
	for _, r := range value {
		if r == 0 || r == '\n' || r == '\r' {
			return fmt.Errorf("%s contains a forbidden control character", field)
		}
	}
	return nil
}

func (p PathExpectation) Validate(required bool) error {
	if err := validateString(p.Path, "path", required); err != nil {
		return err
	}
	if err := validateString(p.Kind, "kind", required); err != nil {
		return err
	}
	switch p.Kind {
	case "", "regular", "fifo", "character", "block", "socket", "directory":
	default:
		return fmt.Errorf("unknown path kind %q", p.Kind)
	}
	if p.Path != "" && (!filepath.IsAbs(p.Path) || filepath.Clean(p.Path) != p.Path) {
		return errors.New("path must be absolute and canonical")
	}
	if p.Kind != "character" && p.Kind != "block" && p.Rdev != 0 {
		return errors.New("rdev is only valid for character or block paths")
	}
	if p.Kind != "regular" && p.SHA256 != "" {
		return errors.New("sha256 is only valid for regular paths")
	}
	if p.SHA256 != "" && !manifestHashPattern.MatchString(p.SHA256) {
		return errors.New("path sha256 is not canonical")
	}
	if p.Kind == "regular" && required && p.SHA256 == "" {
		return errors.New("regular executable path requires sha256")
	}
	return nil
}

func validateOptionalPath(p *PathExpectation, required bool) error {
	if p == nil {
		return nil
	}
	return p.Validate(required)
}
func validateOptionalNetwork(n *NetworkExpectation) error {
	if n == nil {
		return nil
	}
	if err := validateString(n.Network, "network", true); err != nil {
		return err
	}
	return validateString(n.Address, "address", true)
}

func (s SourceRecord) Validate() error {
	if err := validateString(s.Path, "source path", true); err != nil {
		return err
	}
	if err := validateString(s.Kind, "source kind", true); err != nil {
		return err
	}
	if s.Kind != "regular" && s.Kind != "fifo" && s.Kind != "character" && s.Kind != "block" && s.Kind != "socket" {
		return fmt.Errorf("unknown source kind %q", s.Kind)
	}
	if s.Mode > 0o777 {
		return errors.New("source mode is invalid")
	}
	if !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path || !filepath.IsAbs(s.BackupPath) || filepath.Clean(s.BackupPath) != s.BackupPath {
		return errors.New("source paths must be absolute and canonical")
	}
	if !manifestHashPattern.MatchString(s.SHA256) {
		return errors.New("source sha256 is not canonical")
	}
	if err := validateString(s.BackupPath, "backup path", true); err != nil {
		return err
	}
	if !manifestHashPattern.MatchString(s.BackupSHA256) {
		return errors.New("backup sha256 is not canonical")
	}
	switch s.DisabledState {
	case "absent":
		if s.DisabledSHA256 != "" {
			return errors.New("absent source has disabled hash")
		}
	case "inert_replacement", "approved_trampoline":
		if !manifestHashPattern.MatchString(s.DisabledSHA256) {
			return errors.New("disabled source hash is not canonical")
		}
	default:
		return fmt.Errorf("unknown disabled_state %q", s.DisabledState)
	}
	return nil
}

func decodeOptionalPath(raw []byte, target **PathExpectation) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*target = nil
		return nil
	}
	var value PathExpectation
	if err := decodePathExpectation(raw, &value, false); err != nil {
		return err
	}
	*target = &value
	return nil
}
func decodePathExpectation(raw []byte, target *PathExpectation, required bool) error {
	if err := decodeStrictObject(raw, pathExpectationFields[:], func(key string, value []byte) error {
		switch key {
		case "path":
			return decodeJSONString(value, &target.Path)
		case "kind":
			return decodeJSONString(value, &target.Kind)
		case "rdev":
			return decodeJSONUint(value, &target.Rdev)
		case "sha256":
			return decodeJSONString(value, &target.SHA256)
		default:
			return errors.New("unknown path field")
		}
	}); err != nil {
		return err
	}
	return target.Validate(required)
}
func decodeOptionalNetwork(raw []byte, target **NetworkExpectation) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*target = nil
		return nil
	}
	var value NetworkExpectation
	if err := decodeStrictObject(raw, networkExpectationFields[:], func(key string, valueRaw []byte) error {
		switch key {
		case "network":
			return decodeJSONString(valueRaw, &value.Network)
		case "address":
			return decodeJSONString(valueRaw, &value.Address)
		default:
			return errors.New("unknown network field")
		}
	}); err != nil {
		return err
	}
	if err := validateOptionalNetwork(&value); err != nil {
		return err
	}
	*target = &value
	return nil
}
func decodeSources(raw []byte, target *[]SourceRecord) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var values []json.RawMessage
	if err := decoder.Decode(&values); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("source array has trailing data")
	}
	result := make([]SourceRecord, 0, len(values))
	for _, value := range values {
		var source SourceRecord
		if err := decodeStrictObject(value, sourceFields[:], func(key string, item []byte) error {
			switch key {
			case "path":
				return decodeJSONString(item, &source.Path)
			case "kind":
				return decodeJSONString(item, &source.Kind)
			case "mode":
				var n uint64
				if err := decodeJSONUint(item, &n); err != nil {
					return err
				}
				source.Mode = uint32(n)
				return nil
			case "sha256":
				return decodeJSONString(item, &source.SHA256)
			case "backup_path":
				return decodeJSONString(item, &source.BackupPath)
			case "backup_sha256":
				return decodeJSONString(item, &source.BackupSHA256)
			case "disabled_state":
				return decodeJSONString(item, &source.DisabledState)
			case "disabled_sha256":
				return decodeJSONString(item, &source.DisabledSHA256)
			default:
				return errors.New("unknown source field")
			}
		}); err != nil {
			return err
		}
		if err := source.Validate(); err != nil {
			return err
		}
		result = append(result, source)
	}
	*target = result
	return nil
}

// InstallJournalRecord is the exact persisted top-level schema.
type InstallJournalRecord struct {
	Schema               uint64         `json:"schema"`
	State                InstallState   `json:"state"`
	InstallBootID        string         `json:"install_boot_id"`
	PackageSHA256        string         `json:"package_sha256"`
	PreviousConfigSHA256 string         `json:"previous_config_sha256"`
	Inventory            InventoryV1    `json:"inventory"`
	Sources              []SourceRecord `json:"sources"`
}

type InstallJournal = InstallJournalRecord
type Journal = InstallJournalRecord

// PreJournalRecoveryAuthority is the protected, pre-journal recovery
// binding. It is deliberately separate from InstallJournalRecord: before the
// first coarse checkpoint exists, recovery still needs a durable authority for
// the staged helper and the exact original dispatcher backup. It carries no
// additional install state and is removed once the coarse prepared checkpoint
// is durable (or after a successful pre-journal recovery).
type PreJournalRecoveryAuthority struct {
	Schema                 uint64 `json:"schema"`
	StagePath              string `json:"stage_path"`
	StageManifestSHA256    string `json:"stage_manifest_sha256"`
	DispatcherPath         string `json:"dispatcher_path"`
	DispatcherBackupPath   string `json:"dispatcher_backup_path"`
	DispatcherBackupSHA256 string `json:"dispatcher_backup_sha256"`
	DispatcherMode         uint32 `json:"dispatcher_mode"`
	RecoveryHelperSHA256   string `json:"recovery_helper_sha256"`
}

var preJournalAuthorityFields = [...]string{
	"schema", "stage_path", "stage_manifest_sha256", "dispatcher_path",
	"dispatcher_backup_path", "dispatcher_backup_sha256", "dispatcher_mode",
	"recovery_helper_sha256",
}

// PreJournalRecoveryAuthorityPath derives the only authority pathname from a
// fixture or production journal pathname. Returning an empty path for an
// invalid input keeps callers from accidentally creating a relative or
// non-canonical authority file.
func PreJournalRecoveryAuthorityPath(journalPath string) string {
	if journalPath == "" || !filepath.IsAbs(journalPath) || filepath.Clean(journalPath) != journalPath {
		return ""
	}
	return filepath.Join(filepath.Dir(journalPath), "fpgadev-prejournal-v1.json")
}

func (a PreJournalRecoveryAuthority) Validate() error {
	if a.Schema != 1 {
		return errors.New("pre-journal recovery authority schema must be 1")
	}
	for _, item := range []struct {
		value string
		field string
	}{
		{a.StagePath, "stage path"},
		{a.DispatcherPath, "dispatcher path"},
		{a.DispatcherBackupPath, "dispatcher backup path"},
	} {
		value, field := item.value, item.field
		if err := validateString(value, field, true); err != nil {
			return err
		}
		if !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return fmt.Errorf("%s is not absolute and canonical", field)
		}
	}
	if !manifestHashPattern.MatchString(a.StageManifestSHA256) {
		return errors.New("pre-journal stage manifest hash is not canonical")
	}
	if filepath.Base(a.StagePath) != a.StageManifestSHA256 {
		return errors.New("pre-journal stage path is not bound to its manifest")
	}
	if !manifestHashPattern.MatchString(a.DispatcherBackupSHA256) {
		return errors.New("pre-journal dispatcher backup hash is not canonical")
	}
	if a.DispatcherMode == 0 || a.DispatcherMode > 0o777 {
		return errors.New("pre-journal dispatcher mode is invalid")
	}
	if !manifestHashPattern.MatchString(a.RecoveryHelperSHA256) {
		return errors.New("pre-journal recovery helper hash is not canonical")
	}
	return nil
}

func (a PreJournalRecoveryAuthority) MarshalCanonical() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func ParsePreJournalRecoveryAuthority(raw []byte) (PreJournalRecoveryAuthority, error) {
	var authority PreJournalRecoveryAuthority
	if len(raw) == 0 || len(raw) > InstallJournalMaxBytes {
		return PreJournalRecoveryAuthority{}, errors.New("pre-journal recovery authority exceeds size bound")
	}
	if err := decodeStrictObject(raw, preJournalAuthorityFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &authority.Schema)
		case "stage_path":
			return decodeJSONString(value, &authority.StagePath)
		case "stage_manifest_sha256":
			return decodeJSONString(value, &authority.StageManifestSHA256)
		case "dispatcher_path":
			return decodeJSONString(value, &authority.DispatcherPath)
		case "dispatcher_backup_path":
			return decodeJSONString(value, &authority.DispatcherBackupPath)
		case "dispatcher_backup_sha256":
			return decodeJSONString(value, &authority.DispatcherBackupSHA256)
		case "dispatcher_mode":
			valueUint := uint64(0)
			if err := decodeJSONUint(value, &valueUint); err != nil {
				return err
			}
			if valueUint > 0o777 {
				return errors.New("pre-journal dispatcher mode is invalid")
			}
			authority.DispatcherMode = uint32(valueUint)
			return nil
		case "recovery_helper_sha256":
			return decodeJSONString(value, &authority.RecoveryHelperSHA256)
		default:
			return errors.New("unknown pre-journal recovery authority field")
		}
	}); err != nil {
		return PreJournalRecoveryAuthority{}, err
	}
	if err := authority.Validate(); err != nil {
		return PreJournalRecoveryAuthority{}, err
	}
	canonical, err := authority.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return PreJournalRecoveryAuthority{}, errors.New("pre-journal recovery authority JSON is not canonical")
	}
	return authority, nil
}

var journalFields = [...]string{"schema", "state", "install_boot_id", "package_sha256", "previous_config_sha256", "inventory", "sources"}

func (j InstallJournalRecord) Validate() error {
	if j.Schema != 1 {
		return errors.New("install journal schema must be 1")
	}
	if _, ok := installStates[j.State]; !ok {
		return fmt.Errorf("unknown install journal state %q", j.State)
	}
	if !installBootIDPattern.MatchString(j.InstallBootID) {
		return errors.New("install_boot_id must be a lowercase Linux boot UUID")
	}
	if !manifestHashPattern.MatchString(j.PackageSHA256) {
		return errors.New("package_sha256 is not canonical")
	}
	if !manifestHashPattern.MatchString(j.PreviousConfigSHA256) {
		return errors.New("previous_config_sha256 is not canonical")
	}
	if err := j.Inventory.Validate(); err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	if j.Sources == nil || len(j.Sources) > 16 {
		return errors.New("sources must be present and contain at most 16 entries")
	}
	seen := make(map[string]struct{}, len(j.Sources))
	approved := 0
	for n, source := range j.Sources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("sources[%d]: %w", n, err)
		}
		if _, ok := seen[source.Path]; ok {
			return errors.New("sources contains duplicate paths")
		}
		seen[source.Path] = struct{}{}
		if n > 0 && j.Sources[n-1].Path >= source.Path {
			return errors.New("sources must be path sorted")
		}
		if source.DisabledState == "approved_trampoline" {
			approved++
		}
	}
	if approved > 1 {
		return errors.New("sources contain more than one approved trampoline")
	}
	if !reflect.DeepEqual(j.Sources, j.Inventory.StartSources) {
		return errors.New("journal sources do not match inventory start_sources")
	}
	if approved != 1 {
		return errors.New("install journal requires one approved trampoline")
	}
	return nil
}

func (j InstallJournalRecord) MarshalCanonical() ([]byte, error) {
	if err := j.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func ParseInstallJournal(raw []byte) (InstallJournalRecord, error) {
	var j InstallJournalRecord
	if len(raw) == 0 || len(raw) > InstallJournalMaxBytes {
		return InstallJournalRecord{}, errors.New("install journal exceeds size bound")
	}
	if err := decodeStrictObject(raw, journalFields[:], func(key string, value []byte) error {
		switch key {
		case "schema":
			return decodeJSONUint(value, &j.Schema)
		case "state":
			var s string
			if err := decodeJSONString(value, &s); err != nil {
				return err
			}
			j.State = InstallState(s)
			return nil
		case "install_boot_id":
			return decodeJSONString(value, &j.InstallBootID)
		case "package_sha256":
			return decodeJSONString(value, &j.PackageSHA256)
		case "previous_config_sha256":
			return decodeJSONString(value, &j.PreviousConfigSHA256)
		case "inventory":
			var err error
			j.Inventory, err = ParseInventory(value)
			return err
		case "sources":
			return decodeSources(value, &j.Sources)
		default:
			return errors.New("unknown install journal field")
		}
	}); err != nil {
		return InstallJournalRecord{}, err
	}
	if err := j.Validate(); err != nil {
		return InstallJournalRecord{}, err
	}
	canonical, err := j.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		return InstallJournalRecord{}, errors.New("install journal JSON is not canonical")
	}
	return j, nil
}

type InstallJournalStore struct {
	Path        string
	BackupDir   string
	ExpectedUID uint32
	MaxBytes    int
	syncFile    func(*os.File) error
	syncParent  func(*os.File) error
	rename      func(string, string) error
}

func NewInstallJournal(path string, expectedUID ...uint32) *InstallJournalStore {
	uid := uint32(0)
	if len(expectedUID) > 0 {
		uid = expectedUID[0]
	}
	return &InstallJournalStore{Path: path, BackupDir: filepath.Join(filepath.Dir(path), "fpgadev-install-v1-backups"), ExpectedUID: uid, MaxBytes: InstallJournalMaxBytes}
}
func NewProductionInstallJournal() *InstallJournalStore { return NewInstallJournal(InstallJournalPath) }
func NewJournal(path string, expectedUID ...uint32) *InstallJournalStore {
	return NewInstallJournal(path, expectedUID...)
}

func (s *InstallJournalStore) validateParent(create bool) error {
	if s == nil || s.Path == "" || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path {
		return errors.New("install journal path is invalid")
	}
	parent := filepath.Dir(s.Path)
	if create {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return err
		}
		_ = os.Chmod(parent, 0o700)
	}
	return validateSecureJournalDir(parent, s.ExpectedUID)
}

func (s *InstallJournalStore) Load() (InstallJournalRecord, bool, error) {
	if err := s.validateParent(false); err != nil {
		return InstallJournalRecord{}, false, err
	}
	info, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return InstallJournalRecord{}, false, nil
	}
	if err != nil {
		return InstallJournalRecord{}, false, err
	}
	if err := s.validateBackupParent(false); err != nil {
		return InstallJournalRecord{}, true, err
	}
	if err := validateJournalFileInfo(info, s.ExpectedUID); err != nil {
		return InstallJournalRecord{}, true, err
	}
	file, err := os.Open(s.Path)
	if err != nil {
		return InstallJournalRecord{}, true, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(s.maxBytes()+1)))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return InstallJournalRecord{}, true, err
	}
	if len(data) > s.maxBytes() {
		return InstallJournalRecord{}, true, errors.New("install journal exceeds size bound")
	}
	journal, err := ParseInstallJournal(data)
	return journal, true, err
}

func (s *InstallJournalStore) Replace(next InstallJournalRecord) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if err := s.validateParent(true); err != nil {
		return err
	}
	if err := s.validateBackupParent(true); err != nil {
		return err
	}
	old, hadOld, err := s.Load()
	if err != nil && hadOld {
		return err
	}
	if !hadOld && next.State != InstallStatePrepared {
		return errors.New("initial install journal state must be prepared")
	}
	if hadOld {
		if err := validateJournalTransition(old, next); err != nil {
			return err
		}
	}
	data, err := next.MarshalCanonical()
	if err != nil {
		return err
	}
	if len(data) > s.maxBytes() {
		return errors.New("install journal exceeds size bound")
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".fpgadev-install-v1.tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := s.syncOpenFile(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := s.renamePath(tmpPath, s.Path); err != nil {
		return err
	}
	keep = true
	parent, err := os.Open(filepath.Dir(s.Path))
	if err != nil {
		return err
	}
	syncErr := s.syncOpenParent(parent)
	closeErr := parent.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return err
	}
	info, err := os.Lstat(s.Path)
	if err != nil {
		return err
	}
	return validateJournalFileInfo(info, s.ExpectedUID)
}

func validateJournalTransition(old, next InstallJournalRecord) error {
	// The journal payload is immutable across every legal phase edge.  Check
	// that before handling the terminal/uninstall state exceptions so callers
	// cannot smuggle a changed inventory or package binding through an otherwise
	// authorized state transition.
	if next.InstallBootID != old.InstallBootID || next.PackageSHA256 != old.PackageSHA256 || next.PreviousConfigSHA256 != old.PreviousConfigSHA256 || !next.Inventory.Equal(old.Inventory) || !reflect.DeepEqual(next.Sources, old.Sources) {
		return errors.New("install journal immutable fields changed")
	}
	if old.State == InstallStateRestored {
		oldBytes, _ := old.MarshalCanonical()
		nextBytes, _ := next.MarshalCanonical()
		if bytes.Equal(oldBytes, nextBytes) {
			return nil
		}
		return errors.New("immutable install journal cannot be rewritten")
	}
	// Replacing a checkpoint with the same bytes is safe and makes retries
	// idempotent. The only forward edges are the five coarse states; a
	// prepared transition may jump directly to restored for rollback.
	if old.State == next.State {
		oldBytes, _ := old.MarshalCanonical()
		nextBytes, _ := next.MarshalCanonical()
		if bytes.Equal(oldBytes, nextBytes) {
			return nil
		}
		return errors.New("install journal state payload changed")
	}
	if old.State == InstallStateUninstalling && next.State != InstallStateRestored {
		return errors.New("uninstalling journal may only become restored")
	}
	if old.State == InstallStatePrepared && next.State != InstallStateInstalled && next.State != InstallStateRestored {
		return errors.New("prepared journal may only become installed or restored")
	}
	if old.State == InstallStateInstalled && next.State != InstallStateTerminal {
		return errors.New("installed journal may only become terminal")
	}
	if old.State == InstallStateTerminal && next.State != InstallStateUninstalling {
		return errors.New("terminal journal may only become uninstalling")
	}
	if old.State != InstallStatePrepared && next.State == InstallStateRestored && old.State != InstallStateUninstalling {
		return errors.New("install journal state moved out of order")
	}
	return nil
}
func (s *InstallJournalStore) maxBytes() int {
	if s == nil || s.MaxBytes <= 0 {
		return InstallJournalMaxBytes
	}
	return s.MaxBytes
}

func (s *InstallJournalStore) validateBackupParent(create bool) error {
	if s == nil || s.Path == "" {
		return errors.New("install journal backup directory is invalid")
	}
	backupDir := s.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(s.Path), "fpgadev-install-v1-backups")
	}
	if !filepath.IsAbs(backupDir) || filepath.Clean(backupDir) != backupDir {
		return errors.New("install journal backup directory is invalid")
	}
	if create {
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(backupDir, 0o700); err != nil {
			return err
		}
	}
	if err := validateSecureJournalDir(backupDir, s.ExpectedUID); err != nil {
		if !create && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}
func (s *InstallJournalStore) syncOpenFile(f *os.File) error {
	if s.syncFile != nil {
		return s.syncFile(f)
	}
	return f.Sync()
}
func (s *InstallJournalStore) syncOpenParent(f *os.File) error {
	if s.syncParent != nil {
		return s.syncParent(f)
	}
	return f.Sync()
}
func (s *InstallJournalStore) renamePath(old, next string) error {
	if s.rename != nil {
		return s.rename(old, next)
	}
	return os.Rename(old, next)
}

func validateSecureJournalDir(path string, uid uint32) error {
	if err := ensureNoSymlinkComponents(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("install journal parent is not a secure 0700 directory")
	}
	if got, ok := journalUID(info); !ok || got != uid {
		return errors.New("install journal parent owner is not authorized")
	}
	return nil
}

// ensureNoSymlinkComponents closes the lstat/open race at the path boundary
// for the portable store implementation. The Linux production adapter also
// opens the final file with O_NOFOLLOW; checking every parent here prevents a
// writable or swapped-in symlink from redirecting journal/proof state.
func ensureNoSymlinkComponents(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("protected directory path must be absolute")
	}
	clean := filepath.Clean(path)
	for current := clean; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect protected path component %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("protected path component %q is a symlink", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}
func validateJournalFileInfo(info os.FileInfo, uid uint32) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() > InstallJournalMaxBytes {
		return errors.New("install journal file is not a protected regular 0600 file")
	}
	if got, ok := journalUID(info); !ok || got != uid {
		return errors.New("install journal owner is not authorized")
	}
	if n, ok := journalNlink(info); ok && n != 1 {
		return errors.New("install journal link count is not one")
	}
	return nil
}

// The journal also needs to build on non-Linux hosts. Reflection keeps the
// tiny metadata extraction independent from syscall.Stat_t field spelling.
func journalUID(info os.FileInfo) (uint32, bool)   { return journalStatUint(info, "Uid", "UID") }
func journalNlink(info os.FileInfo) (uint64, bool) { return journalStatUint64(info, "Nlink", "Nlink") }
func journalStatUint(info os.FileInfo, names ...string) (uint32, bool) {
	v, ok := journalStatField(info, names...)
	return uint32(v), ok
}
func journalStatUint64(info os.FileInfo, names ...string) (uint64, bool) {
	return journalStatField(info, names...)
}
func journalStatField(info os.FileInfo, names ...string) (uint64, bool) {
	if info == nil || info.Sys() == nil {
		return 0, false
	}
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	for _, name := range names {
		f := v.FieldByName(name)
		if f.IsValid() && f.CanUint() {
			return f.Uint(), true
		}
	}
	return 0, false
}

// MaintenanceGate is the concrete Task-8 semantic implementation returned by
// NewMaintenanceGate. It holds the install flock for the lifetime of the
// returned unlock and never mutates a terminal journal.
type installMaintenanceGate struct {
	journal        *InstallJournalStore
	locker         hardwareowner.OwnerLocker
	validateStatus func(MaintenanceStatus) error
}

type existingMaintenanceLocker interface {
	LockExisting(context.Context) (hardwareowner.Unlock, error)
}
type maintenanceUnlock struct {
	once   sync.Once
	unlock hardwareowner.Unlock
	err    error
}

func (u *maintenanceUnlock) Unlock() error {
	if u == nil {
		return nil
	}
	u.once.Do(func() {
		if u.unlock != nil {
			u.err = u.unlock()
		}
	})
	return u.err
}

func NewMaintenanceGate(args ...any) *installMaintenanceGate {
	g := &installMaintenanceGate{journal: NewProductionInstallJournal(), locker: hardwareowner.NewLocker(InstallLockPath)}
	for _, arg := range args {
		switch value := arg.(type) {
		case *InstallJournalStore:
			g.journal = value
		case hardwareowner.OwnerLocker:
			g.locker = value
		case *hardwareowner.Locker:
			g.locker = value
		}
	}
	return g
}
func NewProductionMaintenanceGate() *installMaintenanceGate { return NewMaintenanceGate() }

// NewInstallLocker is the named Task-8 constructor for the install lock. It
// deliberately returns the same hardened flock implementation as the owner
// lock; keeping the two paths distinct is what enforces install-before-owner
// admission ordering.
func NewInstallLocker(path string, expectedUID ...uint32) *hardwareowner.Locker {
	return hardwareowner.NewLocker(path, expectedUID...)
}

func (g *installMaintenanceGate) Enter(ctx context.Context) (MaintenanceStatus, MaintenanceUnlock, error) {
	if g == nil || g.journal == nil || g.locker == nil {
		return MaintenanceStatus{}, nil, ErrRunnerConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	existingLocker, ok := g.locker.(existingMaintenanceLocker)
	if !ok {
		return MaintenanceStatus{}, nil, fmt.Errorf("%w: %w", ErrMaintenanceGateLock, ErrRunnerConfiguration)
	}
	unlock, err := existingLocker.LockExisting(ctx)
	if err != nil || unlock == nil {
		if err == nil {
			err = ErrRunnerConfiguration
		}
		return MaintenanceStatus{}, nil, fmt.Errorf("%w: %w", ErrMaintenanceGateLock, err)
	}
	release := func(cause error) (MaintenanceStatus, MaintenanceUnlock, error) {
		return MaintenanceStatus{}, nil, errors.Join(cause, unlock())
	}
	record, exists, err := g.journal.Load()
	if err != nil {
		return release(fmt.Errorf("%w: %w", ErrMaintenanceGateJournalLoad, err))
	}
	if !exists || record.State != InstallStateTerminal {
		return release(ErrMaintenanceGateJournalNotTerminal)
	}
	raw, err := record.MarshalCanonical()
	if err != nil {
		return release(fmt.Errorf("%w: %w", ErrMaintenanceGateStatusValidation, err))
	}
	digest := sha256.Sum256(raw)
	status := MaintenanceStatus{TerminalJournalSHA256: fmt.Sprintf("%x", digest[:]), Inventory: record.Inventory}
	validateStatus := g.validateStatus
	if validateStatus == nil {
		validateStatus = func(candidate MaintenanceStatus) error { return candidate.Validate() }
	}
	if err := validateStatus(status); err != nil {
		return release(fmt.Errorf("%w: %w", ErrMaintenanceGateStatusValidation, err))
	}
	return status, &maintenanceUnlock{unlock: unlock}, nil
}

func (g *installMaintenanceGate) Observe() error {
	if g == nil || g.journal == nil {
		return ErrRunnerConfiguration
	}
	if g.locker == nil {
		return ErrRunnerConfiguration
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	unlock, err := g.locker.Lock(ctx)
	if err != nil {
		return err
	}
	if unlock == nil {
		return ErrRunnerConfiguration
	}
	defer func() { _ = unlock() }()
	return g.observeJournal()
}

// Clear is called by hardwareowner.Gate while that gate already holds the
// install lock. It therefore performs the same read without recursively
// acquiring the non-reentrant flock.
func (g *installMaintenanceGate) Clear() error {
	if g == nil || g.journal == nil {
		return ErrRunnerConfiguration
	}
	return g.observeJournal()
}

func (g *installMaintenanceGate) observeJournal() error {
	if g == nil || g.journal == nil {
		return ErrRunnerConfiguration
	}
	record, exists, err := g.journal.Load()
	if err != nil {
		return err
	}
	if !exists || record.State != InstallStateTerminal {
		return errors.New("install journal is not terminal")
	}
	return nil
}

func mustInventoryBytes(i InventoryV1) []byte { b, _ := i.MarshalCanonical(); return b }

var _ MaintenanceGate = (*installMaintenanceGate)(nil)

// Keep imports used by callers that inspect canonical source order in tests.
var _ = sort.Strings
var _ = strings.TrimSpace
