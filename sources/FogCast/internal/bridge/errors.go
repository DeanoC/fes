package bridge

import "errors"

var ErrUnsupportedPlatform = errors.New("uinput is unavailable on this platform")

// RejectInput marks a frame rejected before any sink mutation. The authenticated
// stream remains usable; transport or delivery errors still close it.
func RejectInput(message string) error {
	return errors.Join(errRejectedInputFrame, errors.New(message))
}
