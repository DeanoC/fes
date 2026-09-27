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

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
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
	opts.Width, opts.Height = dev.Config().Width, dev.Config().Height
	app, err := configuredApp(opts)
	if err != nil {
		return err
	}
	inputSpec := opts.Input
	if opts.Smoke {
		inputSpec = "none"
	}
	inputs, err := openNativeInputs(inputSpec)
	if err != nil {
		return err
	}
	defer inputs.close()
	if opts.Smoke {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.SmokeTimeout)
		defer cancel()
	}
	app.Start(ctx)
	defer app.Stop()
	return framebufferLoop(ctx, opts, app, dev, inputs.poll)
}

// framebufferLoop renders the shared App; it never programs an FPGA or
// launches a title during smoke checks. Injected input/device keep it testable.
func framebufferLoop(ctx context.Context, opts Options, app *App, dev gfx.Device, poll func(*App, time.Time) (bool, error)) error {
	textures, labels := map[string]gpuTexture{}, map[string]gpuTexture{}
	defer destroyTextures(dev, textures)
	defer destroyTextures(dev, labels)
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	parked := false
	frames := 0
	for {
		select {
		case <-ctx.Done():
			if opts.Smoke {
				return fmt.Errorf("linuxfb smoke: %w", ctx.Err())
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
			parked = presentFrame(dev, snap, textures, labels, parked)
			frames++
			if opts.Smoke {
				if snap.LoadErr != "" {
					return fmt.Errorf("linuxfb smoke: %s", snap.LoadErr)
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
	pending  []byte
	shifts   map[uint16]bool
	commands map[uint16]Command
	axes     map[uint16]Command
	controls map[uint16]Command
}
type nativeInputs struct {
	devices []*nativeInput
	held    map[Command]bool
}

func openNativeInputs(spec string) (*nativeInputs, error) {
	var paths []string
	spec = strings.TrimSpace(spec)
	switch spec {
	case "none":
		return &nativeInputs{}, nil
	case "", "auto":
		var err error
		paths, err = filepath.Glob("/dev/input/event*")
		if err != nil {
			return nil, err
		}
	default:
		paths = strings.Split(spec, ",")
	}
	result := &nativeInputs{}
	for _, path := range paths {
		fd, err := unix.Open(strings.TrimSpace(path), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			if spec != "" && spec != "auto" {
				result.close()
				return nil, fmt.Errorf("linuxfb input %s: %w", path, err)
			}
			continue
		}
		result.devices = append(result.devices, &nativeInput{fd: fd})
	}
	if len(result.devices) == 0 {
		return nil, fmt.Errorf("linuxfb: no readable evdev inputs (use -input none for a display-only check)")
	}
	return result, nil
}
func (ins *nativeInputs) close() {
	for _, in := range ins.devices {
		unix.Close(in.fd)
	}
}
func (ins *nativeInputs) poll(app *App, now time.Time) (bool, error) {
	for _, in := range ins.devices {
		// Bound draining so a noisy device cannot starve rendering or cancellation.
		for batch := 0; batch < 8; batch++ {
			var b [nativeEventSize * 32]byte
			n, err := unix.Read(in.fd, b[:])
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
				break
			}
			if err != nil {
				return false, fmt.Errorf("linuxfb input: %w", err)
			}
			if n == 0 {
				return false, fmt.Errorf("linuxfb input: device closed")
			}
			in.pending = append(in.pending, b[:n]...)
			for len(in.pending) >= nativeEventSize {
				typ, code, value := decodeNativeEvent(in.pending[:nativeEventSize])
				in.pending = in.pending[nativeEventSize:]
				if typ == 0 && code == 3 {
					return false, fmt.Errorf("linuxfb input: evdev dropped events; restart to resynchronise")
				}
				if typ == 1 {
					if code == 316 {
						if value == 1 {
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
				} else if typ == 3 && (code == 16 || code == 17) {
					in.hat(app, code, value, now)
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
	return ins.applyButtons(app, now), nil
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
func nativeButtonCommand(app *App, button Button) Command {
	e := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: LogicalFromButton(button), Action: remoteinput.ActionPress}
	if remap := app.remapper(); remap != nil {
		e = remap.Apply(e)
	}
	return CommandFromLogical(e)
}
func (in *nativeInput) button(app *App, button Button, value int32, now time.Time) {
	if value == 2 {
		return
	}
	in.setButton(uint16(button), nativeButtonCommand(app, button), value != 0)
	if value != 0 {
		app.NoteInput(InputGamepad, in.fd)
	}
	return
}
func (in *nativeInput) hat(app *App, code uint16, value int32, now time.Time) {
	if in.axes == nil {
		in.axes = map[uint16]Command{}
	}
	button := ButtonNone
	if code == 16 {
		if value < 0 {
			button = ButtonDPadLeft
		} else if value > 0 {
			button = ButtonDPadRight
		}
	} else {
		if value < 0 {
			button = ButtonDPadUp
		} else if value > 0 {
			button = ButtonDPadDown
		}
	}
	cmd := nativeButtonCommand(app, button)
	if old := in.axes[code]; old != cmd {
		in.setButton(1000+code, old, false)
		in.axes[code] = cmd
		in.setButton(1000+code, cmd, cmd != CmdNone)
		if cmd != CmdNone {
			app.NoteInput(InputGamepad, in.fd)
		}
	}
}
func (in *nativeInput) key(app *App, code uint16, value int32, now time.Time) bool {
	name := nativeKeyName(code)
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
