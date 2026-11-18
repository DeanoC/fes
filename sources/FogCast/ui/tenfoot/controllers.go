package tenfoot

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

// PadInfo is what the kit's input layer knows about an attached pad.
type PadInfo struct {
	Name            string
	GUID            string
	Vendor, Product uint16
	Family          gfx.PromptFamily
	// Recognised is a pad GameControllerDB has a mapping for.
	Recognised bool
	// Player is the pad's controller port from 1, or 0 for a menu-only pad.
	Player int
}

// padSource is a pad button Settings › Controllers can rebind.
type padSource struct {
	code    remoteinput.Code
	control gfx.PromptControl
	label   string
}

// padSources are the Controllers rows, in focus order: the left column,
// then the right column from the top.
var padSources = []padSource{
	{remoteinput.ButtonL, gfx.PromptL, "Left shoulder"},
	{remoteinput.ButtonSelect, gfx.PromptSelect, "Select"},
	{remoteinput.ButtonR, gfx.PromptR, "Right shoulder"},
	{remoteinput.ButtonY, gfx.PromptNorth, "Top button"},
	{remoteinput.ButtonB, gfx.PromptEast, "Right button"},
	{remoteinput.ButtonA, gfx.PromptSouth, "Bottom button"},
	{remoteinput.ButtonX, gfx.PromptWest, "Left button"},
	{remoteinput.ButtonStart, gfx.PromptStart, "Start"},
}

// Focus slots after the rows.
const (
	controllersFocusSwap = iota + 8
	controllersFocusReset
	controllersFocusCount
)

// padTarget is a control a button can send.
type padTarget struct {
	code  remoteinput.Code
	label string
	// note says what FES menus use the control for.
	note string
	// group is the picker section: 0 pad buttons, 1 directions, 2 keypad.
	group int
}

var padTargets = func() []padTarget {
	targets := []padTarget{
		{remoteinput.ButtonA, "A", "Confirm", 0},
		{remoteinput.ButtonB, "B", "Back", 0},
		{remoteinput.ButtonC, "C", "", 0},
		{remoteinput.ButtonX, "X", "", 0},
		{remoteinput.ButtonY, "Y", "", 0},
		{remoteinput.ButtonL, "L", "", 0},
		{remoteinput.ButtonR, "R", "", 0},
		{remoteinput.ButtonSelect, "Select", "", 0},
		{remoteinput.ButtonStart, "Start", "", 0},
		{remoteinput.ButtonDPadUp, "Up", "", 1},
		{remoteinput.ButtonDPadDown, "Down", "", 1},
		{remoteinput.ButtonDPadLeft, "Left", "", 1},
		{remoteinput.ButtonDPadRight, "Right", "", 1},
	}
	for i := 1; i <= 9; i++ {
		targets = append(targets, padTarget{remoteinput.Keypad0 + remoteinput.Code(i), fmt.Sprint(i), "", 2})
	}
	return append(targets,
		padTarget{remoteinput.KeypadStar, "*", "", 2},
		padTarget{remoteinput.Keypad0, "0", "", 2},
		padTarget{remoteinput.KeypadHash, "#", "", 2})
}()

// padTargetLabel names a control as a Controllers row shows it.
func padTargetLabel(code remoteinput.Code) string {
	for _, t := range padTargets {
		if t.code == code {
			if t.group == 2 {
				return "Keypad " + t.label
			}
			return t.label
		}
	}
	return "?"
}

// controllersState is the Settings › Controllers page.
type controllersState struct {
	open bool
	// pad is the shown pad's input ID.
	pad    int
	focus  int
	picker struct {
		open  bool
		index int
	}
	status string
}

// ControllerTab is a pad in the Controllers header.
type ControllerTab struct {
	Label string
	Name  string
	// Pressed is a pad with a button down, so a player can find theirs.
	Pressed bool
}

// ControllerRow is one button of the shown pad.
type ControllerRow struct {
	Control gfx.PromptControl
	Glyph   string
	Label   string
	Target  string
	// Changed is a button that no longer sends its own control.
	Changed bool
	Pressed bool
}

// ControllerTarget is a picker cell.
type ControllerTarget struct {
	Label, Note string
	Group       int
	// Now is what the button sends today.
	Now bool
}

// ControllersSnapshot is the Controllers page as the renderer draws it.
type ControllersSnapshot struct {
	Open     bool
	Tabs     []ControllerTab
	Tab      int
	Family   gfx.PromptFamily
	Identity string
	Rows     []ControllerRow
	SwapAB   bool
	Focus    int
	Status   string
	Picker   struct {
		Open    bool
		Index   int
		Targets []ControllerTarget
	}
}

// SetPadInfo records an attached pad and draws its hints with the glyphs of
// its family and saved profile.
func (a *App) SetPadInfo(id int, info PadInfo) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pads == nil {
		a.pads = map[int]PadInfo{}
	}
	a.pads[id] = info
	a.refreshPadPromptsLocked(id)
}

// NotePadButton records a pad's physical button going down or up, before
// its profile remaps it.
func (a *App) NotePadButton(id int, code remoteinput.Code, down bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.pads[id]; !ok {
		return
	}
	if a.padDown == nil {
		a.padDown = map[int]map[remoteinput.Code]bool{}
	}
	if down {
		if a.padDown[id] == nil {
			a.padDown[id] = map[remoteinput.Code]bool{}
		}
		a.padDown[id][code] = true
	} else {
		delete(a.padDown[id], code)
	}
}

// PadProfileGeneration changes whenever a pad's saved profile does. The
// input layer reloads its pads' profiles when it sees a new value.
func (a *App) PadProfileGeneration() uint64 {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.padProfileGen
}

func (a *App) dropPadInfoLocked(id int) {
	delete(a.pads, id)
	delete(a.padDown, id)
	delete(a.padPrompts, id)
	if a.controllers.open && a.controllers.pad == id {
		ids := a.padIDsLocked()
		if len(ids) == 0 {
			a.controllers.open = false
			return
		}
		a.showControllerPadLocked(ids[0])
	}
}

// padIDsLocked orders pads by player, then by the order they attached.
func (a *App) padIDsLocked() []int {
	ids := make([]int, 0, len(a.pads))
	for id := range a.pads {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		pi, pj := a.pads[ids[i]].Player, a.pads[ids[j]].Player
		if (pi == 0) != (pj == 0) {
			return pj == 0
		}
		if pi != pj {
			return pi < pj
		}
		return ids[i] < ids[j]
	})
	return ids
}

// padProfilesLocked is the saved profiles, read once from the preferences.
func (a *App) padProfilesLocked() map[string]inputmap.Profile {
	if a.padProfiles != nil {
		return a.padProfiles
	}
	a.padProfiles = map[string]inputmap.Profile{}
	if a.preferenceStore == nil && a.prefsPath == "" {
		return a.padProfiles
	}
	prefs, err := a.readPreferencesLocked()
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			a.padProfilesUnread = true
		}
		return a.padProfiles
	}
	for guid, profile := range prefs.Controllers {
		a.padProfiles[guid] = profile
	}
	return a.padProfiles
}

// padRemapLocked compiles a pad's saved profile. A profile that does not
// compile reads as none, as it does when the pad attaches.
func (a *App) padRemapLocked(guid string) *inputmap.Remapper {
	profile, ok := a.padProfilesLocked()[guid]
	if !ok {
		return nil
	}
	remap, err := newPadRemapper(profile)
	if err != nil {
		return nil
	}
	return remap
}

// newPadRemapper rejects profiles that make two rebindable buttons send the
// same control. Session input tracks holds by remapped control, so applying
// such a profile would let one physical release release the other button's
// hold too.
func newPadRemapper(profile inputmap.Profile) (*inputmap.Remapper, error) {
	remap, err := inputmap.NewRemapper(profile)
	if err != nil {
		return nil, err
	}
	targets := make(map[remoteinput.Code]remoteinput.Code, len(padSources))
	for _, source := range padSources {
		target := remap.Target(source.code)
		if previous, ok := targets[target]; ok {
			return nil, fmt.Errorf("inputmap: buttons %q and %q both send %q",
				inputmap.Name(previous), inputmap.Name(source.code), inputmap.Name(target))
		}
		targets[target] = source.code
	}
	return remap, nil
}

func (a *App) refreshPadPromptsLocked(id int) {
	info, ok := a.pads[id]
	if !ok {
		return
	}
	if a.padPrompts == nil {
		a.padPrompts = map[int]PadPrompts{}
	}
	a.padPrompts[id] = NewPadPrompts(info.Family, a.padRemapLocked(info.GUID))
}

func (a *App) openControllersLocked() {
	ids := a.padIDsLocked()
	if len(ids) == 0 {
		a.settingsStatus = "Plug in a controller first."
		return
	}
	a.controllers = controllersState{open: true, focus: 5}
	pad := ids[0]
	// Open on the pad that asked, when it is one of them.
	if a.affinity.current.Kind == InputGamepad {
		if _, ok := a.pads[a.affinity.current.ID]; ok {
			pad = a.affinity.current.ID
		}
	}
	a.controllers.pad = pad
}

func (a *App) closeControllersLocked() {
	a.controllers = controllersState{}
}

func (a *App) showControllerPadLocked(id int) {
	a.controllers.pad = id
	a.controllers.picker.open = false
	a.controllers.status = ""
}

func (a *App) stepControllerPadLocked(step int) {
	ids := a.padIDsLocked()
	for i, id := range ids {
		if id == a.controllers.pad {
			a.showControllerPadLocked(ids[(i+step+len(ids))%len(ids)])
			return
		}
	}
}

// controllersNav is where each direction moves focus on the page: up,
// down, left, right. A slot that maps to itself stays put.
var controllersNav = [controllersFocusCount][4]int{
	0:                     {0, 1, 0, 2},
	1:                     {0, controllersFocusSwap, 1, 7},
	2:                     {2, 3, 0, 2},
	3:                     {2, 4, 0, 3},
	4:                     {3, 5, 0, 4},
	5:                     {4, 6, 1, 5},
	6:                     {5, 7, 1, 6},
	7:                     {6, controllersFocusReset, 1, 7},
	controllersFocusSwap:  {1, controllersFocusSwap, controllersFocusSwap, controllersFocusReset},
	controllersFocusReset: {7, controllersFocusReset, controllersFocusSwap, controllersFocusReset},
}

func (a *App) handleControllersLocked(cmd Command) {
	c := &a.controllers
	if _, ok := a.pads[c.pad]; !ok {
		a.closeControllersLocked()
		return
	}
	if c.picker.open {
		a.handleControllerPickerLocked(cmd)
		return
	}
	dir := -1
	switch cmd {
	case CmdUp:
		dir = 0
	case CmdDown:
		dir = 1
	case CmdLeft:
		dir = 2
	case CmdRight:
		dir = 3
	case CmdBack:
		a.closeControllersLocked()
		a.focusSettingsRowLocked(settingsSectionRowPrefix + settingsSectionControllers)
		return
	case CmdSettings:
		a.closeSettingsLocked()
		return
	case CmdFilterPrev, CmdTabPrev:
		a.stepControllerPadLocked(-1)
		return
	case CmdFilterNext, CmdTab:
		a.stepControllerPadLocked(1)
		return
	case CmdSortCycle:
		if c.focus < len(padSources) {
			a.setPadTargetLocked(padSources[c.focus].code, padSources[c.focus].code)
		}
		return
	case CmdSelect:
		switch c.focus {
		case controllersFocusSwap:
			a.togglePadSwapLocked()
		case controllersFocusReset:
			a.resetPadLocked()
		default:
			c.picker.open = true
			c.picker.index = 0
			now := a.padEffectiveLocked()[padSources[c.focus].code]
			for i, t := range padTargets {
				if t.code == now {
					c.picker.index = i
				}
			}
			c.status = ""
		}
		return
	}
	if dir >= 0 {
		c.focus = controllersNav[c.focus][dir]
		c.status = ""
	}
}

func (a *App) handleControllerPickerLocked(cmd Command) {
	c := &a.controllers
	source := padSources[c.focus].code
	switch cmd {
	case CmdBack:
		c.picker.open = false
	case CmdSettings:
		a.closeSettingsLocked()
	case CmdSortCycle:
		c.picker.open = false
		a.setPadTargetLocked(source, source)
	case CmdSelect:
		c.picker.open = false
		a.setPadTargetLocked(source, padTargets[c.picker.index].code)
	case CmdUp, CmdDown, CmdLeft, CmdRight:
		c.picker.index = controllerPickerStep(c.picker.index, cmd)
	}
}

// controllerPickerColumns is how many cells a picker group lays in a row.
var controllerPickerColumns = [3]int{5, 5, 6}

// controllerPickerCell is a target's group, row and column.
func controllerPickerCell(index int) (group, row, col int) {
	group = padTargets[index].group
	n := 0
	for i := 0; i < index; i++ {
		if padTargets[i].group == group {
			n++
		}
	}
	return group, n / controllerPickerColumns[group], n % controllerPickerColumns[group]
}

// controllerPickerStep moves through the picker's rows as they are laid
// out, keeping the column as near as the next row allows.
func controllerPickerStep(index int, cmd Command) int {
	switch cmd {
	case CmdLeft:
		if index > 0 {
			return index - 1
		}
	case CmdRight:
		if index < len(padTargets)-1 {
			return index + 1
		}
	case CmdUp, CmdDown:
		group, row, col := controllerPickerCell(index)
		frac := (float64(col) + 0.5) / float64(controllerPickerColumns[group])
		best, bestGap := index, 2.0
		step := 1
		if cmd == CmdUp {
			step = -1
		}
		// The next laid-out row is the first one met walking that way.
		wantGroup, wantRow := -1, -1
		for i := index + step; i >= 0 && i < len(padTargets); i += step {
			g, r, c := controllerPickerCell(i)
			if g == group && r == row {
				continue
			}
			if wantGroup < 0 {
				wantGroup, wantRow = g, r
			}
			if g != wantGroup || r != wantRow {
				break
			}
			gap := (float64(c)+0.5)/float64(controllerPickerColumns[g]) - frac
			if gap < 0 {
				gap = -gap
			}
			if gap < bestGap {
				best, bestGap = i, gap
			}
		}
		return best
	}
	return index
}

// padEffectiveLocked is what each rebindable button of the shown pad sends.
func (a *App) padEffectiveLocked() map[remoteinput.Code]remoteinput.Code {
	remap := a.padRemapLocked(a.pads[a.controllers.pad].GUID)
	sends := map[remoteinput.Code]remoteinput.Code{}
	for _, s := range padSources {
		sends[s.code] = remap.Target(s.code)
	}
	return sends
}

// padEssential are the controls FES menus cannot do without.
var padEssential = []remoteinput.Code{remoteinput.ButtonA, remoteinput.ButtonB}

// setPadTargetLocked makes a button of the shown pad send target. When that
// would leave Confirm or Back without a button, the button that sent target
// takes over what this one sent; if there is none, nothing changes.
func (a *App) setPadTargetLocked(source, target remoteinput.Code) {
	c := &a.controllers
	sends := a.padEffectiveLocked()
	old := sends[source]
	if old == target {
		return
	}
	// Keep the mapping one-to-one. Session input is represented by its
	// remapped code, so two physical buttons sending the same code would make
	// either button's release look like the logical control was released.
	var swapped remoteinput.Code
	for _, s := range padSources {
		if s.code != source && sends[s.code] == target {
			if swapped != 0 {
				c.status = fmt.Sprintf("Not changed: more than one button already sends %s. Reset this pad's controls first.", padTargetLabel(target))
				return
			}
			swapped = s.code
		}
	}
	if swapped != 0 {
		sends[swapped] = old
	}
	sends[source] = target
	sent := func(code remoteinput.Code) bool {
		for _, s := range padSources {
			if sends[s.code] == code {
				return true
			}
		}
		return false
	}
	for _, need := range padEssential {
		if sent(need) {
			continue
		}
		c.status = fmt.Sprintf("Not changed: no button would send %s, and FES menus need it. Give %s to another button first.",
			padTargetLabel(need), padTargetLabel(need))
		return
	}
	info := a.pads[c.pad]
	profile := a.padProfilesLocked()[info.GUID]
	preset, err := inputmap.NewRemapper(inputmap.Profile{Preset: profile.Preset})
	if err != nil {
		profile.Preset = ""
		preset = nil
	}
	bindings := map[string]string{}
	for from, to := range profile.Bindings {
		bindings[from] = to
	}
	for _, s := range padSources {
		name := inputmap.Name(s.code)
		delete(bindings, name)
		if sends[s.code] != preset.Target(s.code) {
			bindings[name] = inputmap.Name(sends[s.code])
		}
	}
	profile.Bindings = bindings
	if !a.savePadProfileLocked(info.GUID, profile) {
		return
	}
	c.status = fmt.Sprintf("%s now sends %s.", padSourceLabel(source), padTargetLabel(target))
	if swapped != 0 {
		c.status += fmt.Sprintf(" %s took %s.", padSourceLabel(swapped), padTargetLabel(old))
	}
}

func padSourceLabel(code remoteinput.Code) string {
	for _, s := range padSources {
		if s.code == code {
			return s.label
		}
	}
	return "?"
}

func (a *App) togglePadSwapLocked() {
	info := a.pads[a.controllers.pad]
	profile := a.padProfilesLocked()[info.GUID]
	bindings := map[string]string{}
	for from, to := range profile.Bindings {
		bindings[from] = to
	}
	delete(bindings, inputmap.Name(remoteinput.ButtonA))
	delete(bindings, inputmap.Name(remoteinput.ButtonB))
	profile.Bindings = bindings
	if profile.Preset == inputmap.PresetSwapAB {
		profile.Preset = ""
	} else {
		profile.Preset = inputmap.PresetSwapAB
	}
	if !a.savePadProfileLocked(info.GUID, profile) {
		return
	}
	a.controllers.status = "Swap A / B is off."
	if profile.Preset != "" {
		a.controllers.status = "Swap A / B is on: the right-hand button confirms."
	}
}

func (a *App) resetPadLocked() {
	info := a.pads[a.controllers.pad]
	if _, ok := a.padProfilesLocked()[info.GUID]; !ok {
		a.controllers.status = "This pad already uses its own layout."
		return
	}
	if a.savePadProfileLocked(info.GUID, inputmap.Profile{}) {
		a.controllers.status = "This pad is back to its own layout."
	}
}

// savePadProfileLocked saves a pad's profile, dropping an empty one, and
// applies it to every attached pad with that GUID.
func (a *App) savePadProfileLocked(guid string, profile inputmap.Profile) bool {
	profiles := a.padProfilesLocked()
	if a.padProfilesUnread {
		a.controllers.status = "Not saved: FES could not read its settings file."
		return false
	}
	if len(profile.Bindings) == 0 {
		profile.Bindings = nil
	}
	if profile.Preset == "" && profile.Bindings == nil {
		delete(profiles, guid)
	} else {
		profiles[guid] = profile
	}
	a.persistPrefsLocked("controllers")
	a.padProfileGen++
	for id, info := range a.pads {
		if info.GUID == guid {
			a.refreshPadPromptsLocked(id)
		}
	}
	return true
}

func (a *App) controllersSnapshotLocked() ControllersSnapshot {
	c := a.controllers
	info, ok := a.pads[c.pad]
	if !c.open || !ok {
		return ControllersSnapshot{}
	}
	snap := ControllersSnapshot{Open: true, Family: info.Family, Focus: c.focus, Status: c.status}
	for i, id := range a.padIDsLocked() {
		pad := a.pads[id]
		label := "Menu"
		if pad.Player > 0 {
			label = fmt.Sprintf("P%d", pad.Player)
		}
		snap.Tabs = append(snap.Tabs, ControllerTab{Label: label, Name: pad.Name, Pressed: len(a.padDown[id]) > 0})
		if id == c.pad {
			snap.Tab = i
		}
	}
	snap.Identity = fmt.Sprintf("%04x:%04x", info.Vendor, info.Product)
	if info.Recognised {
		snap.Identity = "Recognised  ·  " + snap.Identity
	} else {
		snap.Identity = "Not in the pad list, using a standard layout  ·  " + snap.Identity
	}
	if info.Player > 0 {
		snap.Identity += fmt.Sprintf("  ·  Player %d", info.Player)
	} else {
		snap.Identity += "  ·  Menus only"
	}
	sends := a.padEffectiveLocked()
	for _, s := range padSources {
		snap.Rows = append(snap.Rows, ControllerRow{
			Control: s.control,
			Glyph:   string(gfx.PromptRune(info.Family, s.control)),
			Label:   s.label,
			Target:  padTargetLabel(sends[s.code]),
			Changed: sends[s.code] != s.code,
			Pressed: a.padDown[c.pad][s.code],
		})
	}
	snap.SwapAB = a.padProfilesLocked()[info.GUID].Preset == inputmap.PresetSwapAB
	if c.picker.open {
		snap.Picker.Open = true
		snap.Picker.Index = c.picker.index
		now := sends[padSources[c.focus].code]
		for _, t := range padTargets {
			snap.Picker.Targets = append(snap.Picker.Targets, ControllerTarget{Label: t.label, Note: t.note, Group: t.group, Now: t.code == now})
		}
	}
	return snap
}
