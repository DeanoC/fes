package protocol

import (
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

const MaxDevelopmentRBFBytes int64 = 32 << 20

const RecoveryRebootRequired = "reboot_required"

type System string

const (
	SystemPong            System = "pong"
	SystemMegaDrive       System = "megadrive"
	SystemSNES            System = "snes"
	SystemNES             System = "nes"
	SystemSMS             System = "sms"
	SystemGameBoy         System = "gb"
	SystemGameBoyColor    System = "gbc"
	SystemGBA             System = "gba"
	SystemPCE             System = "pce"
	SystemGameGear        System = "gg"
	SystemAtari2600       System = "a2600"
	SystemAtari7800       System = "a7800"
	SystemColecoVision    System = "coleco"
	SystemAtariLynx       System = "lynx"
	SystemWonderSwan      System = "ws"
	SystemWonderSwanColor System = "wsc"
	SystemIntellivision   System = "intv"
)

type State string

const (
	StateIdle      State = "idle"
	StateLaunching State = "launching"
	StateActive    State = "active"
	StateStopping  State = "stopping"
	StateFailed    State = "failed"
)

type ErrorCode string

const (
	CodeBadRequest           ErrorCode = "BAD_REQUEST"
	CodeUnauthorized         ErrorCode = "UNAUTHORIZED"
	CodeROMNotFound          ErrorCode = "ROM_NOT_FOUND"
	CodeBusy                 ErrorCode = "BUSY"
	CodeUnsupportedSystem    ErrorCode = "UNSUPPORTED_SYSTEM"
	CodeUnsupportedOperation ErrorCode = "UNSUPPORTED_OPERATION"
	CodeInvalidROMPath       ErrorCode = "INVALID_ROM_PATH"
	CodeMiSTerUnavailable    ErrorCode = "MISTER_UNAVAILABLE"
	CodeCoreTimeout          ErrorCode = "CORE_TIMEOUT"
	CodeInternal             ErrorCode = "INTERNAL"
	CodeUnrecognizedCore     ErrorCode = "UNRECOGNIZED_CORE"
	CodeSourceUnavailable    ErrorCode = "SOURCE_UNAVAILABLE"
	CodeInvalidArchive       ErrorCode = "INVALID_ARCHIVE"
	CodeTransferFailed       ErrorCode = "TRANSFER_FAILED"
	CodeDigestMismatch       ErrorCode = "DIGEST_MISMATCH"
	CodeContentNotCached     ErrorCode = "CONTENT_NOT_CACHED"
	CodeCacheFull            ErrorCode = "CACHE_FULL"
	CodeKitLeaseDenied       ErrorCode = "KIT_LEASE_DENIED"
	CodeVersionMismatch      ErrorCode = "VERSION_MISMATCH"
)

type APIError struct {
	Cause    error     `json:"-"`
	Code     ErrorCode `json:"code"`
	Message  string    `json:"message"`
	Phase    string    `json:"phase,omitempty"`
	Expected string    `json:"expected,omitempty"`
	Observed string    `json:"observed,omitempty"`
}

func (e *APIError) Error() string {
	return string(e.Code) + ": " + e.Message
}

func (e *APIError) Unwrap() error { return e.Cause }

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

// CoreInspection is the target runtime's read-only compatibility observation
// for one exact core package.
type CoreInspection struct {
	PersistenceLayout  *RuntimeContract       `json:"persistence_layout,omitempty"`
	PackageID          string                 `json:"package_id"`
	Descriptor         corepackage.Descriptor `json:"descriptor"`
	Compatible         bool                   `json:"compatible"`
	CompatibilityError *APIError              `json:"compatibility_error"`
}

type Health struct {
	TargetID     string     `json:"target_id,omitempty"`
	APIVersion   string     `json:"api_version"`
	AgentVersion string     `json:"agent_version"`
	Ready        bool       `json:"ready"`
	BootID       string     `json:"boot_id,omitempty"`
	Artifacts    *Artifacts `json:"artifacts,omitempty"`
}

// Artifacts identifies the sealed build inputs; absent fields are not live hashes.
type Artifacts struct {
	RecordSHA256  string            `json:"record_sha256,omitempty"`
	RuntimeCommit string            `json:"runtime_commit,omitempty"`
	AgentSHA256   string            `json:"agent_sha256,omitempty"`
	AgentRevision string            `json:"agent_revision,omitempty"`
	KitSHA256     string            `json:"kit_sha256,omitempty"`
	ImageSHA256   string            `json:"image_sha256,omitempty"`
	IdleSHA256    string            `json:"idle_sha256,omitempty"`
	ABI           string            `json:"abi,omitempty"`
	PackageID     string            `json:"package_id,omitempty"`
	Cores         map[string]string `json:"cores,omitempty"`
}

type RuntimeContract struct {
	ID    string `json:"id"`
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

type RuntimeInterface struct {
	ID    string `json:"id"`
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

type CorePackageStatus struct {
	Composition      *expansion.Composition `json:"composition,omitempty"`
	MediaStream      *MediaStreamCapability `json:"media_stream,omitempty"`
	PersistenceMode  string                 `json:"persistence_mode,omitempty"`
	PackageID        string                 `json:"package_id"`
	Generation       uint64                 `json:"generation"`
	ABI              RuntimeContract        `json:"abi"`
	BuildID          string                 `json:"build_id"`
	ActiveInterfaces []RuntimeInterface     `json:"active_interfaces"`
	Gamepad          bool                   `json:"gamepad"`
}

type Status struct {
	State        State              `json:"state"`
	GameID       *string            `json:"game_id"`
	System       *System            `json:"system"`
	ExpectedCore *string            `json:"expected_core"`
	ObservedCore *string            `json:"observed_core"`
	LastError    *APIError          `json:"last_error"`
	Development  bool               `json:"development,omitempty"`
	Recovery     string             `json:"recovery,omitempty"`
	CorePackage  *CorePackageStatus `json:"core_package,omitempty"`
}
