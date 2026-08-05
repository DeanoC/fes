package bridge

import "errors"

var ErrUnsupportedPlatform = errors.New("uinput is unavailable on this platform")
