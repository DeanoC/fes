package protocol

type System string

const (
	SystemMegaDrive System = "megadrive"
	SystemSNES      System = "snes"
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
	CodeBadRequest        ErrorCode = "BAD_REQUEST"
	CodeUnauthorized      ErrorCode = "UNAUTHORIZED"
	CodeROMNotFound       ErrorCode = "ROM_NOT_FOUND"
	CodeBusy              ErrorCode = "BUSY"
	CodeUnsupportedSystem ErrorCode = "UNSUPPORTED_SYSTEM"
	CodeInvalidROMPath    ErrorCode = "INVALID_ROM_PATH"
	CodeMiSTerUnavailable ErrorCode = "MISTER_UNAVAILABLE"
	CodeCoreTimeout       ErrorCode = "CORE_TIMEOUT"
	CodeInternal          ErrorCode = "INTERNAL"
	CodeUnrecognizedCore  ErrorCode = "UNRECOGNIZED_CORE"
)

type APIError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *APIError) Error() string {
	return string(e.Code) + ": " + e.Message
}

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

type Health struct {
	APIVersion    string `json:"api_version"`
	AgentVersion  string `json:"agent_version"`
	Ready         bool   `json:"ready"`
	MiSTerProcess bool   `json:"mister_process"`
	CommandPipe   bool   `json:"command_pipe"`
}

type Status struct {
	State        State     `json:"state"`
	GameID       *string   `json:"game_id"`
	System       *System   `json:"system"`
	ExpectedCore *string   `json:"expected_core"`
	ObservedCore *string   `json:"observed_core"`
	LastError    *APIError `json:"last_error"`
}

type LaunchRequest struct {
	GameID  string `json:"game_id"`
	System  System `json:"system"`
	ROMPath string `json:"rom_path"`
}
