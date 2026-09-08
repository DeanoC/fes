package protocol

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
)

type APIError struct {
	Code     ErrorCode `json:"code"`
	Message  string    `json:"message"`
	Phase    string    `json:"phase,omitempty"`
	Expected string    `json:"expected,omitempty"`
	Observed string    `json:"observed,omitempty"`
}

func (e *APIError) Error() string {
	return string(e.Code) + ": " + e.Message
}

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

type Health struct {
	TargetID      string `json:"target_id,omitempty"`
	APIVersion    string `json:"api_version"`
	AgentVersion  string `json:"agent_version"`
	Ready         bool   `json:"ready"`
	MiSTerProcess bool   `json:"mister_process"`
	CommandPipe   bool   `json:"command_pipe"`
	BootID        string `json:"boot_id,omitempty"`
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
	PackageID        string             `json:"package_id"`
	Generation       uint64             `json:"generation"`
	ABI              RuntimeContract    `json:"abi"`
	BuildID          string             `json:"build_id"`
	ActiveInterfaces []RuntimeInterface `json:"active_interfaces"`
	Gamepad          bool               `json:"gamepad"`
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

type LaunchRequest struct {
	GameID  string `json:"game_id"`
	System  System `json:"system"`
	ROMPath string `json:"rom_path"`
}
