//go:build !sdl3

package tenfoot

import (
	"context"
	"errors"
)

// ErrSDLRequired is returned when the binary was built without SDL3.
var ErrSDLRequired = errors.New("fogcast-tenfoot requires SDL3; build with go build -tags sdl3 ./cmd/fogcast-tenfoot")

func runWindow(context.Context, Options) error {
	return ErrSDLRequired
}
