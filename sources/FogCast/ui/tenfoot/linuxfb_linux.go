//go:build linux

package tenfoot

import (
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/kitlauncher/controller"
	"golang.org/x/sys/unix"
)

// The kernel input_event timeval has two native longs. Do not use the
// 16-byte ARM proof-command format on 64-bit development hosts.
const nativeEventSize = 2*(strconv.IntSize/8) + 8

func decodeNativeEvent(b []byte) (uint16, uint16, int32) {
	tail := b[len(b)-8:]
	return binary.NativeEndian.Uint16(tail), binary.NativeEndian.Uint16(tail[2:]), int32(binary.NativeEndian.Uint32(tail[4:]))
}

func runFramebuffer(ctx context.Context, opts Options) error {
	dev, err := gfx.OpenLinuxFB(opts.Framebuffer)
	if err != nil {
		return err
	}
	defer dev.Close()
	original := append([]byte(nil), dev.Destination()...)
	defer copy(dev.Destination(), original)
	return runDirectDisplay(ctx, opts, dev, "linuxfb")
}

// runDirectDisplay is the app, evdev, smoke, and present loop shared by
// linuxfb and menu-display. linuxfb still restores the mapped bytes itself.
func runDirectDisplay(ctx context.Context, opts Options, dev directDisplay, label string) error {
	opts = sizedOptions(opts, dev)
	app, err := configuredApp(opts)
	if err != nil {
		return err
	}
	if display, ok := dev.(*gfx.MenuDisplay); ok {
		app.SetMenuDisplay(display)
	}
	inputSpec := opts.Input
	if opts.Smoke {
		inputSpec = "none"
	}
	inputs, err := openNativeInputs(inputSpec, label)
	if err != nil {
		app.Stop()
		return err
	}
	defer inputs.close()
	inputs.seed(app)
	if opts.Smoke {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.SmokeTimeout)
		defer cancel()
	}
	app.Start(ctx)
	defer app.Stop()
	return framebufferLoop(ctx, opts, app, dev, inputs.poll, label)
}

type directDisplay interface {
	gfx.Device
	Config() gfx.FBConfig
}

// framebufferLoop renders the shared App; it never programs an FPGA or
// launches a title during smoke checks. Injected input/device keep it testable.
func framebufferLoop(ctx context.Context, opts Options, app *App, dev gfx.Device, poll func(*App, time.Time) (bool, error), label string) error {
	painter := gfx.NewFrameCache(dev)
	textures, labels := map[string]gpuTexture{}, map[string]gpuTexture{}
	defer destroyTextures(dev, textures)
	defer destroyTextures(dev, labels)
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	parked := false
	frames := 0
	var hold presentHold
	for {
		select {
		case <-ctx.Done():
			if opts.Smoke {
				return fmt.Errorf("%s smoke: %w", label, ctx.Err())
			}
			return nil
		case now := <-ticker.C:
			quit, err := poll(app, now)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}
			app.Tick(now)
			snap := app.Snapshot()
			if !hold.skip(ctx, dev, snap) {
				parked = presentFrame(painter, snap, textures, labels, parked)
			}
			frames++
			if opts.Smoke {
				if snap.LoadErr != "" {
					return fmt.Errorf("%s smoke: %s", label, snap.LoadErr)
				}
				if !snap.Loading && len(snap.Games) > 0 && frames >= 3 {
					return nil
				}
			}
		}
	}
}

type nativeInput struct {
	fd       int
	path     string
	kind     InputKind
	pending  []byte
	shifts   map[uint16]bool
	commands map[uint16]Command
	axes     map[uint16]Command
	controls map[uint16]Command
	hatHeld  map[uint16]Button
	padHeld  map[remoteinput.Code]remoteinput.Event
	keysHeld map[uint16]string
	mapper   *controller.Mapper
}
type nativeInputs struct {
	devices   []*nativeInput
	held      map[Command]bool
	automatic bool
	label     string
	dir       string
	lastScan  time.Time
	scanEvery time.Duration
	// menuSuppressed drops held menu edges once while a local core owns
	// the pad, and once more when that core returns the menu.
	menuSuppressed bool
	// kindOf and openFile are nil in production. Tests substitute them
	// because a fixture node cannot answer EVIOCGBIT.
	kindOf   func(fd int) InputKind
	openFile func(path string) (int, error)
}

func openNativeInputs(spec, label string) (*nativeInputs, error) {
	return openNativeInputsDir(spec, label, "/dev/input")
}

func openNativeInputsDir(spec, label, dir string) (*nativeInputs, error) {
	var paths []string
	spec = strings.TrimSpace(spec)
	automatic := spec == "" || spec == "auto"
	switch spec {
	case "none":
		return &nativeInputs{}, nil
	case "", "auto":
		var err error
		paths, err = filepath.Glob(filepath.Join(dir, "event*"))
		if err != nil {
			return nil, err
		}
	default:
		paths = strings.Split(spec, ",")
	}
	result := &nativeInputs{
		automatic: automatic,
		label:     label,
		dir:       dir,
		scanEvery: time.Second,
		lastScan:  time.Now(),
	}
	for _, path := range paths {
		if err := result.openPath(nil, strings.TrimSpace(path)); err != nil {
			result.close()
			return nil, err
		}
	}
	// Automatic mode paints with whatever is plugged in, including nothing.
	// Explicit nodes are still required to open.
	if !automatic && len(result.devices) == 0 {
		return nil, fmt.Errorf("%s: no readable supported keyboard/gamepad evdev inputs (use -input none for a display-only check)", label)
	}
	return result, nil
}

func (ins *nativeInputs) openPath(app *App, path string) error {
	if ins.hasPath(path) {
		return nil
	}
	var (
		fd  int
		err error
	)
	if ins.openFile != nil {
		fd, err = ins.openFile(path)
	} else {
		fd, err = unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		if ins.automatic {
			return nil
		}
		return fmt.Errorf("%s input %s: %w", ins.label, path, err)
	}
	kind := ins.deviceKind(fd)
	if ins.automatic && kind == InputNone {
		unix.Close(fd)
		return nil
	}
	in := &nativeInput{fd: fd, path: path, kind: kind}
	if ins.kindOf == nil && kind == InputGamepad {
		in.mapper = nativeGamepadMapper(fd)
	}
	ins.devices = append(ins.devices, in)
	if app != nil {
		app.AttachInput(kind, fd)
	}
	return nil
}

func (ins *nativeInputs) deviceKind(fd int) InputKind {
	if ins != nil && ins.kindOf != nil {
		return ins.kindOf(fd)
	}
	return nativeDeviceKind(fd)
}

func (ins *nativeInputs) hasPath(path string) bool {
	for _, in := range ins.devices {
		if in.path == path {
			return true
		}
	}
	return false
}

// maybeRescan opens event nodes that appeared after startup. Automatic mode
// only: the supervisor is not the retry loop. Already-open paths are not
// opened again. The cadence is one second, same classifier as startup.
func (ins *nativeInputs) maybeRescan(app *App, now time.Time) {
	if ins == nil || !ins.automatic || ins.dir == "" {
		return
	}
	every := ins.scanEvery
	if every <= 0 {
		every = time.Second
	}
	if !ins.lastScan.IsZero() && now.Sub(ins.lastScan) < every {
		return
	}
	ins.lastScan = now
	paths, err := filepath.Glob(filepath.Join(ins.dir, "event*"))
	if err != nil {
		return
	}
	for _, path := range paths {
		_ = ins.openPath(app, path)
	}
}
func (ins *nativeInputs) close() {
	for _, in := range ins.devices {
		unix.Close(in.fd)
	}
}
func (ins *nativeInputs) poll(app *App, now time.Time) (bool, error) {
	ins.maybeRescan(app, now)
devices:
	for _, in := range append([]*nativeInput(nil), ins.devices...) {
		// Bound draining so a noisy device cannot starve rendering or cancellation.
		for batch := 0; batch < 8; batch++ {
			var b [nativeEventSize * 32]byte
			n, err := unix.Read(in.fd, b[:])
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				break
			}
			if err != nil {
				if ins.automatic {
					ins.drop(app, in)
					continue devices
				}
				return false, fmt.Errorf("%s input: %w", ins.label, err)
			}
			if n == 0 {
				if ins.automatic {
					ins.drop(app, in)
					continue devices
				}
				return false, fmt.Errorf("%s input: device closed", ins.label)
			}
			in.pending = append(in.pending, b[:n]...)
			for len(in.pending) >= nativeEventSize {
				typ, code, value := decodeNativeEvent(in.pending[:nativeEventSize])
				in.pending = in.pending[nativeEventSize:]
				if typ == 0 && code == 3 {
					if ins.automatic {
						ins.drop(app, in)
						continue devices
					}
					return false, fmt.Errorf("%s input: evdev dropped events; restart to resynchronise", ins.label)
				}
				if typ == 1 {
					if in.mapper != nil && code != 316 {
						if e, ok := in.mapper.Map(typ, code, value); ok {
							if e.Kind == remoteinput.KindButton {
								in.button(app, ButtonFromLogical(e.Code), value, now)
								if ins.applyButtons(app, now) {
									return true, nil
								}
							}
						}
						continue
					}
					if code == 316 {
						if value == 1 && app.Snapshot().Session.HardwareRoom {
							app.Press(CmdHome, now)
						} else if app.coreOwnsPadInput() {
							if value != 0 {
								app.NoteInput(InputGamepad, in.fd)
							}
						} else if value == 1 {
							app.NoteInput(InputGamepad, in.fd)
							in.setButton(316, CmdSettings, true)
						} else if value == 0 {
							in.setButton(316, CmdSettings, false)
						}
					} else if button := nativeButton(code); button != ButtonNone {
						in.button(app, button, value, now)
					} else if code < 256 {
						if in.key(app, code, value, now) {
							return true, nil
						}
					}
				} else if typ == 3 && (code == 0 || code == 1 || code == 16 || code == 17) {
					if in.mapper != nil {
						if e, ok := in.mapper.Map(typ, code, value); ok && e.Kind == remoteinput.KindAxis {
							in.axis(app, e.Code, e.Value, now)
							if code == 0 || code == 1 {
								if ins.applyButtons(app, now) {
									return true, nil
								}
							}
						}
					} else if code == 16 || code == 17 {
						in.hat(app, code, value, now)
					}
				}
				// Preserve quick down/up pairs even when drained in one render frame.
				if (typ == 1 && code >= 256) || (typ == 3 && (code == 16 || code == 17)) {
					if ins.applyButtons(app, now) {
						return true, nil
					}
				}
			}
		}
	}
	if app.coreOwnsPadInput() {
		if !ins.menuSuppressed {
			ins.dropMenuEdges(app)
			ins.menuSuppressed = true
		}
		return false, nil
	}
	if ins.menuSuppressed {
		ins.dropMenuEdges(app)
		ins.menuSuppressed = false
	}
	return ins.applyButtons(app, now), nil
}

func (ins *nativeInputs) dropMenuEdges(app *App) {
	if ins == nil {
		return
	}
	for _, in := range ins.devices {
		if in == nil {
			continue
		}
		in.controls = nil
		in.axes = nil
	}
	for cmd := range ins.held {
		app.Release(cmd)
		delete(ins.held, cmd)
	}
}
func nativeButton(code uint16) Button {
	switch code {
	case 304:
		return ButtonSouth
	case 305:
		return ButtonEast
	case 307:
		return ButtonNorth
	case 308:
		return ButtonWest
	case 310:
		return ButtonLeftShoulder
	case 311:
		return ButtonRightShoulder
	case 314:
		return ButtonBack
	case 315:
		return ButtonStart
	case 544:
		return ButtonDPadUp
	case 545:
		return ButtonDPadDown
	case 546:
		return ButtonDPadLeft
	case 547:
		return ButtonDPadRight
	}
	return ButtonNone
}
func (in *nativeInput) setButton(control uint16, cmd Command, down bool) {
	if in.controls == nil {
		in.controls = map[uint16]Command{}
	}
	if down {
		in.controls[control] = cmd
	} else {
		delete(in.controls, control)
	}
}
func (ins *nativeInputs) applyButtons(app *App, now time.Time) bool {
	if ins.held == nil {
		ins.held = map[Command]bool{}
	}
	pressed := map[Command]bool{}
	for _, in := range ins.devices {
		for _, cmd := range in.controls {
			pressed[cmd] = true
		}
	}
	return applyPressed(app, pressed, ins.held, now)
}
func (in *nativeInput) button(app *App, button Button, value int32, now time.Time) {
	if value == 2 || button == ButtonNone {
		return
	}
	e := remoteinput.Event{
		Device: remoteinput.DeviceGamepad,
		Kind:   remoteinput.KindButton,
		Code:   LogicalFromButton(button),
		Action: remoteinput.ActionRelease,
	}
	if value != 0 {
		e.Action = remoteinput.ActionPress
	}
	if remap := app.remapper(); remap != nil {
		e = remap.Apply(e)
	}
	if in.padHeld == nil {
		in.padHeld = make(map[remoteinput.Code]remoteinput.Event)
	}
	if value != 0 {
		in.padHeld[e.Code] = e
	} else {
		delete(in.padHeld, e.Code)
	}
	if app.HandleLocalPad(e, now) {
		if value != 0 {
			app.NoteInput(InputGamepad, in.fd)
		}
		return
	}
	in.setButton(uint16(button), CommandFromLogical(e), value != 0)
	if value != 0 {
		app.NoteInput(InputGamepad, in.fd)
	}
}
func (in *nativeInput) localHat(app *App, code uint16, value int32, now time.Time) {
	button := ButtonNone
	if code == 16 {
		if value < 0 {
			button = ButtonDPadLeft
		} else if value > 0 {
			button = ButtonDPadRight
		}
	} else if value < 0 {
		button = ButtonDPadUp
	} else if value > 0 {
		button = ButtonDPadDown
	}
	if in.hatHeld == nil {
		in.hatHeld = map[uint16]Button{}
	}
	prev := in.hatHeld[code]
	if prev == button {
		return
	}
	if prev != ButtonNone {
		in.button(app, prev, 0, now)
	}
	if button == ButtonNone {
		delete(in.hatHeld, code)
		return
	}
	in.hatHeld[code] = button
	in.button(app, button, 1, now)
}

func (in *nativeInput) localAxis(app *App, code remoteinput.Code, value int32, now time.Time) {
	axisCode := uint16(16)
	if code == remoteinput.AxisLeftY {
		axisCode = 17
	}
	direction := int32(0)
	if value < 0 {
		direction = -1
	} else if value > 0 {
		direction = 1
	}
	in.localHat(app, axisCode, direction, now)
}

func (in *nativeInput) hat(app *App, code uint16, value int32, now time.Time) {
	in.localHat(app, code, value, now)
}
func (in *nativeInput) axis(app *App, code remoteinput.Code, value int32, now time.Time) {
	if app.coreOwnsPadInput() {
		in.localAxis(app, code, value, now)
		return
	}
	if in.axes == nil {
		in.axes = map[uint16]Command{}
	}
	axisX, axisY := int(value), 0
	if code == remoteinput.AxisLeftY {
		axisX, axisY = 0, int(value)
	}
	old := in.axes[uint16(code)]
	cmd := CommandFromStickHeld(axisX, axisY, old)
	if cmd != old {
		in.button(app, nativeDirectionButton(old), 0, now)
		in.axes[uint16(code)] = cmd
		in.button(app, nativeDirectionButton(cmd), 1, now)
	}
	_ = now
}

func nativeDirectionButton(cmd Command) Button {
	switch cmd {
	case CmdUp:
		return ButtonDPadUp
	case CmdDown:
		return ButtonDPadDown
	case CmdLeft:
		return ButtonDPadLeft
	case CmdRight:
		return ButtonDPadRight
	default:
		return ButtonNone
	}
}
func (in *nativeInput) key(app *App, code uint16, value int32, now time.Time) bool {
	name := nativeKeyName(code)
	if in.keysHeld == nil {
		in.keysHeld = make(map[uint16]string)
	}
	if value != 0 {
		in.keysHeld[code] = name
	} else {
		delete(in.keysHeld, code)
	}
	modifier := code == 42 || code == 54
	if modifier {
		if in.shifts == nil {
			in.shifts = map[uint16]bool{}
		}
		in.shifts[code] = value != 0
	}
	if app.HandlePlayHIDScancode(name, nativeHIDUsage(code), value != 0, now) {
		return false
	}
	if modifier {
		return false
	}
	shift := in.shifts[42] || in.shifts[54]
	cmd := CommandFromKey(name)
	if shift && cmd == CmdTab {
		cmd = CmdTabPrev
	}
	if value == 0 {
		if in.commands != nil {
			app.Release(in.commands[code])
			delete(in.commands, code)
		}
		return false
	}
	app.NoteInput(InputKeyboard, in.fd)
	if app.OSKOpen() {
		switch name {
		case "backspace":
			app.SearchBackspace(now)
		case "return":
			app.ConfirmSearch(now)
		case "escape", "tab", "up", "down", "left", "right":
			app.Press(cmd, now)
		default:
			app.TypeText(nativeKeyText(code, shift), now)
		}
		if in.commands == nil {
			in.commands = map[uint16]Command{}
		}
		in.commands[code] = cmd
		return false
	}
	if value == 2 {
		return false
	} // App owns navigation repeat.
	if in.commands == nil {
		in.commands = map[uint16]Command{}
	}
	in.commands[code] = cmd
	return app.Press(cmd, now) == CmdQuit
}

var nativeKeys = map[uint16]string{
	29: "ctrl", 42: "shift", 54: "shift", 56: "alt", 97: "ctrl", 100: "alt", 125: "super", 126: "super",
	1: "escape", 2: "1", 3: "2", 4: "3", 5: "4", 6: "5", 7: "6", 8: "7", 9: "8", 10: "9", 11: "0", 12: "minus", 13: "equals", 14: "backspace", 15: "tab",
	16: "q", 17: "w", 18: "e", 19: "r", 20: "t", 21: "y", 22: "u", 23: "i", 24: "o", 25: "p", 26: "leftbracket", 27: "rightbracket", 28: "return",
	30: "a", 31: "s", 32: "d", 33: "f", 34: "g", 35: "h", 36: "j", 37: "k", 38: "l", 39: ";", 40: "'", 41: "`", 43: "\\",
	44: "z", 45: "x", 46: "c", 47: "v", 48: "b", 49: "n", 50: "m", 51: ",", 52: ".", 53: "slash", 57: "space", 102: "home", 103: "up", 105: "left", 106: "right", 108: "down",
}

func nativeKeyName(code uint16) string { return nativeKeys[code] }
func nativeKeyText(code uint16, shift bool) string {
	name := nativeKeyName(code)
	switch name {
	case "space":
		name = " "
	case "minus":
		name = "-"
	case "equals":
		name = "="
	case "leftbracket":
		name = "["
	case "rightbracket":
		name = "]"
	case "slash":
		name = "/"
	}
	if len(name) != 1 {
		return ""
	}
	if !shift {
		return name
	}
	const plain = "1234567890-=[];'`\\,./"
	const shifted = "!@#$%^&*()_+{}:\"~|<>?"
	if i := strings.Index(plain, name); i >= 0 {
		return string(shifted[i])
	}
	return strings.ToUpper(name)
}

// Linux keycodes are not USB HID usages. Preserve the shared play-keyboard path.
func nativeHIDUsage(code uint16) uint8 {
	name := nativeKeyName(code)
	if len(name) == 1 && name[0] >= 'a' && name[0] <= 'z' {
		return name[0] - 'a' + 4
	}
	if code >= 2 && code <= 11 {
		return uint8(code + 28)
	}
	return map[uint16]uint8{1: 41, 12: 45, 13: 46, 14: 42, 15: 43, 26: 47, 27: 48, 28: 40, 29: 224, 39: 51, 40: 52, 41: 53, 42: 225, 43: 49, 51: 54, 52: 55, 53: 56, 54: 229, 56: 226, 57: 44, 58: 57, 97: 228, 100: 230, 102: 74, 103: 82, 104: 75, 105: 80, 106: 79, 107: 77, 108: 81, 109: 78, 110: 73, 111: 76, 125: 227, 126: 231}[code]
}

// EVIOCGBIT(EV_KEY, 96) reads the Linux key capability bitmap. Only actual
// keyboards or supported gamepad button devices belong in automatic selection.
func nativeDeviceKind(fd int) InputKind {
	var keys [96]byte
	const request = uintptr(0x80000000 | (96 << 16) | ('E' << 8) | 0x21)
	_, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&keys[0])))
	if err != 0 {
		return InputNone
	}
	var id [8]byte
	const idRequest = uintptr(0x80000000 | (8 << 16) | ('E' << 8) | 0x02)
	_, _, idErr := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), idRequest, uintptr(unsafe.Pointer(&id[0])))
	if idErr != 0 {
		return InputNone
	}
	return nativeKindWithIdentity(keys[:], binary.LittleEndian.Uint16(id[:2]), nativeDeviceName(fd), binary.LittleEndian.Uint16(id[2:4]), binary.LittleEndian.Uint16(id[4:6]))
}
func nativeKindWithIdentity(keys []byte, bus uint16, name string, vendor, product uint16) InputKind {
	fixture := vendor == 0x081f && product == 0xe401
	buttons := nativeHasKey(keys, 304) || nativeHasKey(keys, 305) || (fixture && nativeHasKey(keys, 288))
	if controller.Eligible(bus, name, buttons) {
		return InputGamepad
	}
	if buttons {
		return InputNone
	}
	if bus == 6 || name == "FogCast Virtual Gamepad" {
		return InputNone
	}
	// Some physical pads expose the older BTN_TRIGGER/BTN_THUMB family but
	// lack the BTN_SOUTH/EAST capabilities used by the known-device matcher.
	// Their readable, non-virtual identity is enough to admit those game keys.
	if bus != 0 && name != "" && nativeHasJoystickKeys(keys) {
		return InputGamepad
	}
	// Key capabilities can identify a keyboard without device metadata, but
	// they cannot establish a physical gamepad identity.
	if kind := nativeKindFromKeys(keys); kind == InputGamepad {
		return InputNone
	} else {
		return kind
	}
}
func nativeHasJoystickKeys(keys []byte) bool {
	for code := 288; code <= 318; code++ {
		if nativeHasKey(keys, code) {
			return true
		}
	}
	return false
}
func nativeHasKey(keys []byte, code int) bool {
	return code/8 < len(keys) && keys[code/8]&(1<<uint(code%8)) != 0
}
func nativeDeviceName(fd int) string {
	var name [256]byte
	const request = uintptr(0x80000000 | (256 << 16) | ('E' << 8) | 0x06)
	_, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&name[0])))
	if err != 0 {
		return ""
	}
	return strings.TrimRight(string(name[:]), "\x00")
}
func nativeGamepadMapper(fd int) *controller.Mapper {
	var id [8]byte
	const idRequest = uintptr(0x80000000 | (8 << 16) | ('E' << 8) | 0x02)
	_, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), idRequest, uintptr(unsafe.Pointer(&id[0])))
	if err != 0 {
		return nil
	}
	axes := map[uint16]controller.Range{}
	for _, code := range []uint16{0, 1, 16, 17} {
		var info [24]byte
		request := uintptr(0x80000000|(24<<16)|('E'<<8)) | uintptr(0x40+code)
		if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&info[0]))); e == 0 {
			axes[code] = controller.Range{Min: int32(binary.LittleEndian.Uint32(info[4:])), Max: int32(binary.LittleEndian.Uint32(info[8:]))}
		}
	}
	return controller.NewMapper(binary.LittleEndian.Uint16(id[2:4]), binary.LittleEndian.Uint16(id[4:6]), axes)
}
func nativeKindFromKeys(keys []byte) InputKind {
	has := func(code int) bool { return code/8 < len(keys) && keys[code/8]&(1<<uint(code%8)) != 0 }
	if has(304) || has(305) || (has(288) && has(289)) {
		return InputGamepad
	}
	if has(30) && has(44) && has(28) {
		return InputKeyboard
	}
	return InputNone
}
func (ins *nativeInputs) seed(app *App) {
	for _, in := range ins.devices {
		app.SeedInput(in.kind, in.fd)
	}
	app.FinishInputSeed()
}
func (ins *nativeInputs) drop(app *App, lost *nativeInput) {
	unix.Close(lost.fd)
	for i, in := range ins.devices {
		if in == lost {
			ins.devices = append(ins.devices[:i], ins.devices[i+1:]...)
			break
		}
	}
	// Cancel lost holds without synthesizing a short press on disconnect.
	remaining := map[Command]bool{}
	for _, in := range ins.devices {
		for _, cmd := range in.controls {
			remaining[cmd] = true
		}
	}
	for cmd := range ins.held {
		if !remaining[cmd] {
			app.hold.Cancel(cmd)
			app.Release(cmd)
			delete(ins.held, cmd)
		}
	}
	for _, cmd := range lost.commands {
		app.Release(cmd)
	}
	ins.releaseLostPlayInput(app, lost)
	app.DetachInput(InputKeyboard, lost.fd)
	app.DetachInput(InputGamepad, lost.fd)
}

func (ins *nativeInputs) releaseLostPlayInput(app *App, lost *nativeInput) {
	remainingPads, remainingKeys := map[remoteinput.Code]bool{}, map[string]bool{}
	for _, in := range ins.devices {
		for code := range in.padHeld {
			remainingPads[code] = true
		}
		for _, name := range in.keysHeld {
			remainingKeys[name] = true
		}
	}
	app.mu.Lock()
	var padEvents, keyEvents []remoteinput.Event
	for code, event := range lost.padHeld {
		if remainingPads[code] {
			continue
		}
		delete(app.playPadHeld, code)
		delete(app.playPadSuppressed, code)
		if code == remoteinput.ButtonSelect {
			app.playPadSelectDown = false
			app.localSelectDown = false
		}
		if code == remoteinput.ButtonStart {
			app.playPadStartDown = false
			app.localStartDown = false
		}
		delete(app.localSent, code)
		event.Action = remoteinput.ActionRelease
		event.Value = 0
		padEvents = append(padEvents, event)
	}
	if !app.playPadSelectDown || !app.playPadStartDown {
		app.playPadChordSince = time.Time{}
		app.playPadChordFired = false
	}
	if !app.localSelectDown || !app.localStartDown {
		app.localChordSince = time.Time{}
		app.localChordFired = false
	}
	for code, name := range lost.keysHeld {
		if remainingKeys[name] {
			continue
		}
		delete(app.playKeyHeld, name)
		delete(app.playKeySuppressed, name)
		event, ok := playhid.Event(name, false, app.session.CoreKeyboard)
		if app.session.CoreKeyboardHID {
			event, ok = hidkeys.Event(nativeHIDUsage(code), false)
		}
		if ok {
			key := playHIDKey{event.Player, event.Device, event.Kind, event.Code}
			if _, held := app.playHIDHeld[key]; held {
				delete(app.playHIDHeld, key)
				keyEvents = append(keyEvents, event)
			}
		}
	}
	feed := app.localFeed
	forward := (app.localPhase == localPhaseRunning || app.session.State == "active") && !app.playHIDFailClosedLocked()
	if forward {
		app.queuePlayHIDLocked(keyEvents)
	}
	app.mu.Unlock()
	if forward && feed != nil {
		for _, event := range padEvents {
			_ = feed.Send(event, time.Now())
		}
	}
}
