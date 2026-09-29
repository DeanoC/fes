package tenfoot

import "github.com/DeanoC/FogCast/ui/gfx"

// openMenuDisplay is replaced by tests that supply a fake runtime client.
var openMenuDisplay = gfx.NewMenuDisplay

func openChangeDrivenMenu(socket string) (*gfx.MenuDisplay, error) {
	dev, err := openMenuDisplay(socket)
	if err != nil {
		return nil, err
	}
	dev.SetChangeDriven(true)
	return dev, nil
}
