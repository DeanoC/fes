//go:build !linux

package tenfoot

import (
	"context"
	"fmt"
)

func runMenuDisplay(context.Context, Options) error {
	return fmt.Errorf("menu-display requires Linux")
}
