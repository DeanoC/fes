//go:build !linux

package tenfoot

import (
	"context"
	"fmt"
)

func runFramebuffer(context.Context, Options) error { return fmt.Errorf("linuxfb requires Linux") }
