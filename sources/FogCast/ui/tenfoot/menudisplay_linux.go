//go:build linux

package tenfoot

import "context"

func runMenuDisplay(ctx context.Context, opts Options) error {
	dev, err := openChangeDrivenMenu(opts.MenuSocket)
	if err != nil {
		return err
	}
	defer dev.Close()
	return runDirectDisplay(ctx, opts, dev, "menu-display")
}
