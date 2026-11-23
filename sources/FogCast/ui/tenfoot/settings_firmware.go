package tenfoot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// settingsFirmwareFamilies are the v1 families in Home's order, with the
// family IDs and labels the workbenches give the firmware card
// (stdlib/systems.lua).
var settingsFirmwareFamilies = []struct {
	ID, Label string
	Cores     []string
}{
	{"zx81", "Sinclair ZX81", []string{"fes.zx81", "fes.zx80", "fes.ts1000", "fes.ts1500"}},
	{"spectrum", "ZX Spectrum", []string{"fes.spectrum", "fes.spectrum128", "fes.spectrum-plus3", "fes.pentagon"}},
	{"fes.atari-st", "Atari ST", []string{"fes.atari-st"}},
	{"fes.c64", "Commodore 64", []string{"fes.c64"}},
}

// settingsFirmware is the host's bundled firmware list, fetched as Settings
// opens.
type settingsFirmware struct {
	Known   bool
	Err     bool
	Bundled []protocol.BundledFirmware
}

func (a *App) fetchSettingsFirmwareLocked() {
	a.settingsFirmware = settingsFirmware{}
	if a.client == nil {
		return
	}
	gen := a.settingsGen
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	client := a.client
	go func() {
		ctx, cancel := context.WithTimeout(parent, 20*time.Second)
		defer cancel()
		bundled, err := client.BundledFirmware(ctx)
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.settingsGen != gen {
			return
		}
		a.settingsFirmware = settingsFirmware{Known: true, Err: err != nil, Bundled: bundled}
	}()
}

func (a *App) settingsFirmwareRowsLocked() []SettingsRow {
	rows := make([]SettingsRow, 0, len(settingsFirmwareFamilies))
	for i, family := range settingsFirmwareFamilies {
		rows = append(rows, SettingsRow{ID: fmt.Sprintf("firmware-%d", i), Label: family.Label, Value: a.settingsFirmwareValueLocked(family.Cores)})
	}
	return rows
}

// settingsFirmwareValueLocked says whether a family's models start with
// firmware that comes with FES or need the user's files.
func (a *App) settingsFirmwareValueLocked(cores []string) string {
	switch {
	case !a.settingsFirmware.Known:
		return "Checking…"
	case a.settingsFirmware.Err:
		return "Unavailable"
	}
	var names []string
	seen := map[string]bool{}
	builtIn := 0
	for _, core := range cores {
		fw, ok := bundledForModel(a.settingsFirmware.Bundled, firmwareCardModelForCore(core, ""))
		if !ok {
			continue
		}
		builtIn++
		if !seen[fw.Name] {
			seen[fw.Name] = true
			names = append(names, fw.Name)
		}
	}
	switch {
	case builtIn == 0:
		return "Needs your files"
	case builtIn < len(cores):
		return fmt.Sprintf("Built in for %d of %d models", builtIn, len(cores))
	case len(names) == 1:
		return "Built in  ·  " + names[0]
	}
	return "Built in"
}

// settingsFirmwareSummaryLocked names the families that need the user's
// files, for the section list.
func (a *App) settingsFirmwareSummaryLocked() string {
	if !a.settingsFirmware.Known || a.settingsFirmware.Err {
		return a.settingsFirmwareValueLocked(nil)
	}
	var needs []string
	partial := 0
	for _, family := range settingsFirmwareFamilies {
		switch value := a.settingsFirmwareValueLocked(family.Cores); {
		case value == "Needs your files":
			needs = append(needs, family.Label)
		case strings.HasPrefix(value, "Built in for "):
			partial++
		}
	}
	switch {
	case len(needs) == 0 && partial == 0:
		return "Built in"
	case len(needs) == 1 && partial == 0:
		return needs[0] + " needs your files"
	}
	return "Some models need your files"
}

// openSettingsFirmwareCardLocked shows a family's firmware card over
// Settings, as the workbench's Firmware button does. Closing it returns to
// Settings.
func (a *App) openSettingsFirmwareCardLocked(index int) {
	if index < 0 || index >= len(settingsFirmwareFamilies) {
		return
	}
	family := settingsFirmwareFamilies[index]
	a.openFirmwareCardLocked(rooms.Action{CoreID: family.Cores[0], FamilyID: family.ID, FamilyLabel: family.Label, CoreIDs: family.Cores})
}
