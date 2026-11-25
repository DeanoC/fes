package tenfoot

import (
	"testing"

	"github.com/DeanoC/FogCast/internal/release"
)

// Settings shows these families, and the firmware card names every model
// they list, without release filtering. A held-back model listed here would
// appear on the default Settings card while Home and the workbench hide it.
func TestSettingsFirmwareFamiliesListOnlyV1Models(t *testing.T) {
	for _, family := range settingsFirmwareFamilies {
		for _, core := range family.Cores {
			if scope := release.Of(core); scope != release.V1 {
				t.Errorf("%s lists %s, which is %s, not v1", family.Label, core, scope)
			}
		}
	}
}
