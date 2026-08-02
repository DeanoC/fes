package romsource

import (
	"fmt"

	"github.com/DeanoC/FogCast-POC/protocol"
)

// Error reports a preparation failure without exposing a NAS or staging path.
type Error struct {
	GameID string
	Code   protocol.ErrorCode
	cause  error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: prepare game %q", e.Code, e.GameID)
}

func (e *Error) Unwrap() error {
	return e.cause
}

func preparationError(gameID string, code protocol.ErrorCode, cause error) error {
	return &Error{GameID: gameID, Code: code, cause: cause}
}
