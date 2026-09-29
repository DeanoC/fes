//go:build linux

package tenfoot

import (
	"context"

	"github.com/DeanoC/FogCast/ui/gfx"
)

func runMenuDisplay(ctx context.Context, opts Options) error {
	dev, err := gfx.NewMenuDisplay(opts.MenuSocket)
	if err != nil {
		return err
	}
	dev.SetChangeDriven(true)
	defer dev.Close()
	return runDirectDisplay(ctx, opts, dev)
}
