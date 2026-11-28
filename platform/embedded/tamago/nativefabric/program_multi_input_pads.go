// SPDX-License-Identifier: GPL-3.0-or-later

package nativefabric

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/usbhub"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

const (
	tamagoBusUSB       uint16 = 3
	tamagoBusBluetooth uint16 = 5
	tamagoStickZone           = int16(16384)
)

var menuPadEventNames = [10]string{"up", "down", "left", "right", "return", "escape", "x", "i", "[", "]"}

func appendPadMenuEdges(events []input.Event, prev, next [10]bool) ([]input.Event, [10]bool) {
	for i, down := range next {
		if down && !prev[i] {
			events = append(events, input.Event{Key: menuPadEventNames[i]})
		}
	}
	return events, next
}

var canonicalButtonMap = []struct {
	bit  controllers.Button
	code remoteinput.Code
	raw  string
}{
	{controllers.ButtonSouth, remoteinput.ButtonA, "b0"},
	{controllers.ButtonEast, remoteinput.ButtonB, "b1"},
	{controllers.ButtonWest, remoteinput.ButtonX, "b2"},
	{controllers.ButtonNorth, remoteinput.ButtonY, "b3"},
	{controllers.ButtonL1, remoteinput.ButtonL, "b4"},
	{controllers.ButtonR1, remoteinput.ButtonR, "b5"},
	{controllers.ButtonSelect, remoteinput.ButtonSelect, "b8"},
	{controllers.ButtonStart, remoteinput.ButtonStart, "b9"},
	{controllers.ButtonDPadUp, remoteinput.ButtonDPadUp, "h0.1"},
	{controllers.ButtonDPadDown, remoteinput.ButtonDPadDown, "h0.4"},
	{controllers.ButtonDPadLeft, remoteinput.ButtonDPadLeft, "h0.8"},
	{controllers.ButtonDPadRight, remoteinput.ButtonDPadRight, "h0.2"},
}

var rawButtonNames = [32]string{
	"b0", "b1", "b2", "b3", "b4", "b5", "b6", "b7",
	"b8", "b9", "b10", "b11", "b12", "b13", "b14", "b15",
	"b16", "b17", "b18", "b19", "b20", "b21", "b22", "b23",
	"b24", "b25", "b26", "b27", "b28", "b29", "b30", "b31",
}

var rawHatSources = [4]struct {
	bit controllers.Button
	raw string
}{
	{controllers.ButtonDPadUp, "h0.1"},
	{controllers.ButtonDPadDown, "h0.4"},
	{controllers.ButtonDPadLeft, "h0.8"},
	{controllers.ButtonDPadRight, "h0.2"},
}

var rawAxisNames = [6][2]string{
	{"-a0", "+a0"},
	{"-a1", "+a1"},
	{"-a2", "+a2"},
	{"-a3", "+a3"},
	{"-a4", "+a4"},
	{"-a5", "+a5"},
}

var stickDirSources = [4]struct {
	raw  string
	code remoteinput.Code
}{
	{raw: "-a1", code: remoteinput.ButtonDPadUp},
	{raw: "+a1", code: remoteinput.ButtonDPadDown},
	{raw: "-a0", code: remoteinput.ButtonDPadLeft},
	{raw: "+a0", code: remoteinput.ButtonDPadRight},
}

var learnedTargetCode = map[string]remoteinput.Code{
	"a":             remoteinput.ButtonA,
	"b":             remoteinput.ButtonB,
	"x":             remoteinput.ButtonX,
	"y":             remoteinput.ButtonY,
	"leftshoulder":  remoteinput.ButtonL,
	"rightshoulder": remoteinput.ButtonR,
	"back":          remoteinput.ButtonSelect,
	"start":         remoteinput.ButtonStart,
	"dpup":          remoteinput.ButtonDPadUp,
	"dpdown":        remoteinput.ButtonDPadDown,
	"dpleft":        remoteinput.ButtonDPadLeft,
	"dpright":       remoteinput.ButtonDPadRight,
}

type multiPadProfile struct {
	hasMapping bool
	sources    map[string]remoteinput.Code
	remap      *inputmap.Remapper
}

func padStickDirs(leftX, leftY int16) [4]bool {
	return [4]bool{
		leftY < -tamagoStickZone,
		leftY > tamagoStickZone,
		leftX < -tamagoStickZone,
		leftX > tamagoStickZone,
	}
}

func effectiveRawButtons(st usbhub.GamepadPadState) uint32 {
	if st.RawButtons != 0 {
		return st.RawButtons
	}
	return uint32(st.Buttons&0x7fff) &^ uint32(controllers.ButtonHome)
}

func effectiveRawAxes(st usbhub.GamepadPadState) [6]int16 {
	axes := st.RawAxes
	if axes[0] == 0 && st.LeftX != 0 {
		axes[0] = st.LeftX
	}
	if axes[1] == 0 && st.LeftY != 0 {
		axes[1] = st.LeftY
	}
	return axes
}

func padAxisDirs(st usbhub.GamepadPadState) [6][2]bool {
	axes := effectiveRawAxes(st)
	var dirs [6][2]bool
	for i := range axes {
		dirs[i][0] = axes[i] < -tamagoStickZone
		dirs[i][1] = axes[i] > tamagoStickZone
	}
	return dirs
}

func padStateNeutral(st usbhub.GamepadPadState) bool {
	return st.Buttons == 0 && st.RawButtons == 0 && padAxisDirs(st) == ([6][2]bool{})
}

func (p *ProgramMultiInputPlatform) livePadStatesInto(dst []usbhub.GamepadPadState) []usbhub.GamepadPadState {
	n := p.padCount
	if n > len(dst) {
		n = len(dst)
	}
	for i := 0; i < n; i++ {
		dev := p.padCache[i]
		dst[i] = usbhub.GamepadPadState{
			ID:         dev.ID,
			Player:     uint8(dev.Player),
			Buttons:    dev.Buttons,
			LeftX:      dev.LeftX,
			LeftY:      dev.LeftY,
			RawButtons: dev.RawButtons,
			RawAxes:    dev.RawAxes,
		}
	}
	return dst[:n]
}

func (p *ProgramMultiInputPlatform) refreshPadProfiles() {
	if p.cfg.App == nil || p.cfg.Preferences == nil {
		return
	}
	gen := p.cfg.App.PadProfileGeneration() + 1
	if gen == p.padProfilesGen {
		return
	}
	raw, err := p.cfg.Preferences.ReadPreferences()
	profiles := map[string]inputmap.Profile{}
	if err == nil {
		var parsed struct {
			Controllers map[string]inputmap.Profile `json:"controllers"`
		}
		if jsonErr := json.Unmarshal(raw, &parsed); jsonErr == nil && parsed.Controllers != nil {
			profiles = parsed.Controllers
		}
	}
	p.padProfiles = profiles
	p.padProfilesGen = gen
}

func releaseTrackedPadButtons(app *tenfoot.App, id int, buttons controllers.Button) {
	if buttons == 0 {
		return
	}
	for _, m := range canonicalButtonMap {
		if buttons&m.bit != 0 {
			app.NotePadButton(id, m.code, false)
		}
	}
}

func (p *ProgramMultiInputPlatform) syncTenfootPadTopology(force bool) {
	app := p.cfg.App
	if app == nil {
		return
	}
	p.refreshPadProfiles()
	gen := p.source.GamepadGeneration()
	btGen := p.source.BluetoothGeneration()
	if !force && gen == p.lastPadGen && btGen == p.lastPadBTGen {
		return
	}
	n := p.source.GamepadDevicesInto(p.padCache[:])
	p.padCount = n
	p.lastPadGen = gen
	p.lastPadBTGen = btGen

	if p.pads == nil {
		p.pads = map[int]*trackedMultiPad{}
	}
	for _, tracked := range p.pads {
		tracked.seen = false
	}
	for i := 0; i < n; i++ {
		dev := p.padCache[i]
		info := tamagoPadInfo(dev)
		tracked, exists := p.pads[dev.ID]
		if !exists {
			tracked = &trackedMultiPad{info: info}
			p.pads[dev.ID] = tracked
			app.SetPadInfo(dev.ID, info)
			app.AttachInput(tenfoot.InputGamepad, dev.ID)
		} else if tracked.info.GUID != info.GUID {
			releaseTrackedPadButtons(app, dev.ID, tracked.buttons)
			if tracked.learning {
				app.DetachInput(tenfoot.InputGamepad, dev.ID)
			}
			tracked = &trackedMultiPad{info: info}
			p.pads[dev.ID] = tracked
			app.SetPadInfo(dev.ID, info)
			app.AttachInput(tenfoot.InputGamepad, dev.ID)
		} else if tracked.info != info {
			tracked.info = info
			app.SetPadInfo(dev.ID, info)
		}
		tracked.seen = true

		if tracked.profileGen == 0 {
			prof, err := compileMultiProfile(p.padProfiles[info.GUID])
			if err == nil {
				tracked.profile = prof
			} else {
				tracked.profile = nil
			}
			tracked.profileGen = p.padProfilesGen
		}
	}
	for id, tracked := range p.pads {
		if tracked.seen {
			continue
		}
		releaseTrackedPadButtons(app, id, tracked.buttons)
		app.DetachInput(tenfoot.InputGamepad, id)
		delete(p.pads, id)
	}
}

func (p *ProgramMultiInputPlatform) processPadStates(states []usbhub.GamepadPadState, now time.Time) {
	app := p.cfg.App
	if app == nil {
		return
	}
	for _, st := range states {
		tracked := p.pads[st.ID]
		if tracked == nil {
			continue
		}
		if (tracked.profileGen == 0 || tracked.profileGen != p.padProfilesGen) && padStateNeutral(st) {
			prof, err := compileMultiProfile(p.padProfiles[tracked.info.GUID])
			if err == nil {
				tracked.profile = prof
			} else {
				tracked.profile = nil
			}
			tracked.profileGen = p.padProfilesGen
		}

		rawBtns := effectiveRawButtons(st)
		axisHeld := padAxisDirs(st)
		learning := app.PadLearning(st.ID)
		if learning {
			if !tracked.learning {
				releaseTrackedPadButtons(app, st.ID, tracked.buttons)
			}
			for bit := 0; bit < 32; bit++ {
				mask := uint32(1) << bit
				if rawBtns&mask != 0 && tracked.rawButtons&mask == 0 {
					app.NoteInput(tenfoot.InputGamepad, st.ID)
					app.LearnPadSource(st.ID, rawButtonNames[bit], now)
				}
			}
			for _, h := range rawHatSources {
				if st.Buttons&h.bit != 0 && tracked.buttons&h.bit == 0 {
					app.NoteInput(tenfoot.InputGamepad, st.ID)
					app.LearnPadSource(st.ID, h.raw, now)
				}
			}
			for axis := range axisHeld {
				for dir := range axisHeld[axis] {
					if axisHeld[axis][dir] && !tracked.axes[axis][dir] {
						app.NoteInput(tenfoot.InputGamepad, st.ID)
						app.LearnPadSource(st.ID, rawAxisNames[axis][dir], now)
					}
				}
			}
			tracked.learning = true
			tracked.buttons = st.Buttons
			tracked.rawButtons = rawBtns
			tracked.axes = axisHeld
			continue
		}
		if tracked.learning && (tracked.buttons != 0 || tracked.rawButtons != 0 || tracked.axes != ([6][2]bool{})) && !padStateNeutral(st) {
			tracked.buttons = st.Buttons
			tracked.rawButtons = rawBtns
			tracked.axes = axisHeld
			continue
		}
		tracked.learning = false

		pressedAny := false
		changed := st.Buttons ^ tracked.buttons
		if changed != 0 {
			for _, m := range canonicalButtonMap {
				if changed&m.bit != 0 {
					down := st.Buttons&m.bit != 0
					app.NotePadButton(st.ID, m.code, down)
					if down {
						pressedAny = true
					}
				}
			}
			if !pressedAny && st.Buttons&^tracked.buttons != 0 {
				pressedAny = true
			}
		}
		if rawBtns&^tracked.rawButtons != 0 {
			pressedAny = true
		}
		if pressedAny || axisHeld != ([6][2]bool{}) {
			app.NoteInput(tenfoot.InputGamepad, st.ID)
		}
		tracked.buttons = st.Buttons
		tracked.rawButtons = rawBtns
		tracked.axes = axisHeld
	}
}

func compileMultiProfile(profile inputmap.Profile) (*multiPadProfile, error) {
	if profile.Mapping == "" && profile.Preset == "" && len(profile.Bindings) == 0 {
		return nil, nil
	}
	var out multiPadProfile
	if profile.Mapping != "" {
		out.hasMapping = true
		out.sources = parseMultiMapping(profile.Mapping)
	}
	if profile.Preset != "" || len(profile.Bindings) > 0 {
		remap, err := inputmap.NewRemapper(inputmap.Profile{
			Preset:   profile.Preset,
			Bindings: profile.Bindings,
		})
		if err != nil {
			return nil, err
		}
		out.remap = remap
	}
	return &out, nil
}

func parseMultiMapping(mapping string) map[string]remoteinput.Code {
	sources := make(map[string]remoteinput.Code)
	for _, field := range strings.Split(mapping, ",") {
		target, src, ok := strings.Cut(strings.TrimSpace(field), ":")
		target, src = strings.TrimSpace(target), strings.TrimSpace(src)
		if !ok || src == "" || target == "" || target == "platform" {
			continue
		}
		if target == "leftx" || target == "lefty" {
			inv := strings.HasSuffix(src, "~")
			axis := strings.TrimLeft(strings.TrimSuffix(src, "~"), "+-")
			if axis == "" {
				continue
			}
			neg, pos := remoteinput.ButtonDPadLeft, remoteinput.ButtonDPadRight
			if target == "lefty" {
				neg, pos = remoteinput.ButtonDPadUp, remoteinput.ButtonDPadDown
			}
			if inv {
				neg, pos = pos, neg
			}
			sources["-"+axis] = neg
			sources["+"+axis] = pos
			continue
		}
		if code, ok := learnedTargetCode[target]; ok {
			sources[src] = code
			if strings.HasPrefix(src, "a") {
				if axis, inv := strings.CutSuffix(src, "~"); inv && axis != "" {
					sources["-"+axis] = code
				} else {
					sources["+"+src] = code
				}
			}
		}
	}
	return sources
}

func (p *ProgramMultiInputPlatform) hasActivePadProfile(states []usbhub.GamepadPadState) bool {
	for _, st := range states {
		if tracked := p.pads[st.ID]; tracked != nil && tracked.profile != nil {
			return true
		}
	}
	return false
}

func (p *ProgramMultiInputPlatform) effectiveCoreRowsFromStates(base usb.GamepadState, states []usbhub.GamepadPadState) [9]uint16 {
	if len(states) == 0 || !p.hasActivePadProfile(states) {
		return p.cfg.CoreKeys.rows(base)
	}
	var rows [9]uint16
	for _, st := range states {
		var padState usb.GamepadState
		if tracked := p.pads[st.ID]; tracked != nil && tracked.profile != nil {
			padState = usbGamepadStateFromControls(remappedControls(st, tracked.profile))
		} else {
			padState = (controllers.State{Buttons: st.Buttons, LeftX: st.LeftX, LeftY: st.LeftY}).ToUSBGamepadState()
			if st.RawButtons != 0 {
				padState.Buttons = st.RawButtons
			}
			if st.RawAxes != ([6]int16{}) {
				padState.Axes = st.RawAxes
			}
		}
		rows = combineRows(rows, p.cfg.CoreKeys.rows(padState))
	}
	return rows
}

func usbGamepadStateFromControls(c map[remoteinput.Code]bool) usb.GamepadState {
	var btns controllers.Button
	if c[remoteinput.ButtonDPadUp] {
		btns |= controllers.ButtonDPadUp
	}
	if c[remoteinput.ButtonDPadDown] {
		btns |= controllers.ButtonDPadDown
	}
	if c[remoteinput.ButtonDPadLeft] {
		btns |= controllers.ButtonDPadLeft
	}
	if c[remoteinput.ButtonDPadRight] {
		btns |= controllers.ButtonDPadRight
	}
	if c[remoteinput.ButtonA] {
		btns |= controllers.ButtonSouth
	}
	if c[remoteinput.ButtonB] {
		btns |= controllers.ButtonEast
	}
	if c[remoteinput.ButtonX] {
		btns |= controllers.ButtonWest
	}
	if c[remoteinput.ButtonY] {
		btns |= controllers.ButtonNorth
	}
	if c[remoteinput.ButtonL] {
		btns |= controllers.ButtonL1
	}
	if c[remoteinput.ButtonR] {
		btns |= controllers.ButtonR1
	}
	if c[remoteinput.ButtonSelect] {
		btns |= controllers.ButtonSelect
	}
	if c[remoteinput.ButtonStart] {
		btns |= controllers.ButtonStart
	}
	return (controllers.State{Buttons: btns}).ToUSBGamepadState()
}

func (p *ProgramMultiInputPlatform) effectiveControllerPortsFromStates(base [usbhub.MaxPlayerPorts]uint8, states []usbhub.GamepadPadState) [usbhub.MaxPlayerPorts]uint8 {
	ports := base
	for _, st := range states {
		if st.Player < 1 || st.Player > usbhub.MaxPlayerPorts {
			continue
		}
		tracked := p.pads[st.ID]
		if tracked == nil || tracked.profile == nil {
			continue
		}
		ports[st.Player-1] = computerMaskFromControls(remappedControls(st, tracked.profile))
	}
	return ports
}

func (p *ProgramMultiInputPlatform) menuControlsFromPadStates(states []usbhub.GamepadPadState) [10]bool {
	var held [10]bool
	for _, st := range states {
		var profile *multiPadProfile
		if tracked := p.pads[st.ID]; tracked != nil {
			if tracked.learning {
				continue
			}
			profile = tracked.profile
		}
		c := remappedControls(st, profile)
		if c[remoteinput.ButtonDPadUp] {
			held[0] = true
		}
		if c[remoteinput.ButtonDPadDown] {
			held[1] = true
		}
		if c[remoteinput.ButtonDPadLeft] {
			held[2] = true
		}
		if c[remoteinput.ButtonDPadRight] {
			held[3] = true
		}
		if c[remoteinput.ButtonA] {
			held[4] = true
		}
		if c[remoteinput.ButtonB] {
			held[5] = true
		}
		if c[remoteinput.ButtonX] {
			held[6] = true
		}
		if c[remoteinput.ButtonY] {
			held[7] = true
		}
		if c[remoteinput.ButtonL] {
			held[8] = true
		}
		if c[remoteinput.ButtonR] {
			held[9] = true
		}
	}
	return held
}

func remappedControls(st usbhub.GamepadPadState, profile *multiPadProfile) map[remoteinput.Code]bool {
	out := make(map[remoteinput.Code]bool, 12)
	addSource := func(raw string, fallback remoteinput.Code) {
		code := fallback
		if profile != nil && profile.hasMapping {
			mapped, ok := profile.sources[raw]
			if !ok {
				return
			}
			code = mapped
		}
		if profile != nil && profile.remap != nil {
			code = profile.remap.Target(code)
		}
		out[code] = true
	}
	if profile != nil && profile.hasMapping {
		rawBtns := effectiveRawButtons(st)
		for bit := 0; bit < 32; bit++ {
			if rawBtns&(uint32(1)<<bit) != 0 {
				addSource(rawButtonNames[bit], 0)
			}
		}
		for _, h := range rawHatSources {
			if st.Buttons&h.bit != 0 {
				addSource(h.raw, 0)
			}
		}
		axisHeld := padAxisDirs(st)
		for axis := range axisHeld {
			for dir := range axisHeld[axis] {
				if axisHeld[axis][dir] {
					addSource(rawAxisNames[axis][dir], 0)
				}
			}
		}
	} else {
		for _, m := range canonicalButtonMap {
			if st.Buttons&m.bit != 0 {
				addSource(m.raw, m.code)
			}
		}
		axes := effectiveRawAxes(st)
		for k, down := range padStickDirs(axes[0], axes[1]) {
			if down {
				addSource(stickDirSources[k].raw, stickDirSources[k].code)
			}
		}
	}
	return out
}

func computerMaskFromControls(c map[remoteinput.Code]bool) uint8 {
	var mask uint8
	if c[remoteinput.ButtonDPadUp] {
		mask |= 1 << 0
	}
	if c[remoteinput.ButtonDPadDown] {
		mask |= 1 << 1
	}
	if c[remoteinput.ButtonDPadLeft] {
		mask |= 1 << 2
	}
	if c[remoteinput.ButtonDPadRight] {
		mask |= 1 << 3
	}
	if c[remoteinput.ButtonA] {
		mask |= 1 << 4
	}
	if c[remoteinput.ButtonB] {
		mask |= 1 << 5
	}
	if c[remoteinput.ButtonSelect] {
		mask |= 1 << 6
	}
	if c[remoteinput.ButtonStart] {
		mask |= 1 << 7
	}
	return mask
}

func tamagoPadInfo(dev usbhub.GamepadDevice) tenfoot.PadInfo {
	bus := tamagoBusUSB
	if dev.Bluetooth {
		bus = tamagoBusBluetooth
	}
	identityName := dev.IdentityName
	if identityName == "" {
		identityName = dev.Name
	}
	return tenfoot.PadInfo{
		Name:       dev.Name,
		GUID:       tamagoPadGUID(bus, identityName, dev.Vendor, dev.Product, dev.Version),
		Vendor:     dev.Vendor,
		Product:    dev.Product,
		Family:     tamagoPromptFamily(dev.Kind, dev.Vendor),
		Recognised: dev.Kind != controllers.KindGenericHID && dev.Kind != controllers.KindUnknown,
		Bluetooth:  dev.Bluetooth,
		Player:     dev.Player,
	}
}

func tamagoPromptFamily(kind controllers.Kind, vendor uint16) gfx.PromptFamily {
	switch kind {
	case controllers.KindDualSense, controllers.KindDualShock4:
		return gfx.PromptPlayStation
	case controllers.KindXInput, controllers.KindXboxGIP:
		return gfx.PromptXbox
	case controllers.KindSwitchPro:
		return gfx.PromptNintendo
	}
	switch vendor {
	case 0x054c:
		return gfx.PromptPlayStation
	case 0x045e:
		return gfx.PromptXbox
	case 0x057e:
		return gfx.PromptNintendo
	default:
		return gfx.PromptGeneric
	}
}

func tamagoPadGUID(bus uint16, name string, vendor, product, version uint16) string {
	var g [16]byte
	binary.LittleEndian.PutUint16(g[0:], bus)
	binary.LittleEndian.PutUint16(g[2:], sdlCRC16(name))
	if vendor != 0 {
		binary.LittleEndian.PutUint16(g[4:], vendor)
		binary.LittleEndian.PutUint16(g[8:], product)
		binary.LittleEndian.PutUint16(g[12:], version)
	} else {
		copy(g[4:15], name)
	}
	return hex.EncodeToString(g[:])
}

func sdlCRC16(name string) uint16 {
	var crc uint16
	for i := 0; i < len(name); i++ {
		crc ^= uint16(name[i])
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}
