//go:build linux

package tenfoot

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/controller"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
	"golang.org/x/sys/unix"
)

type nativeInput struct {
	fd   int
	path string
	kind InputKind
	// player is this pad's controller port, or noLocalPlayer. Keyboards and
	// mice never hold one.
	player int8
	// yields marks a pad that gives its port to any other pad (yieldsPort).
	yields   bool
	pending  []byte
	shifts   map[uint16]bool
	commands map[uint16]Command
	axes     map[uint16]Command
	controls map[uint16]Command
	hatHeld  map[uint16]Button
	padHeld  map[padKey]remoteinput.Event
	// padSent is what each held button pressed on the port. Several buttons
	// can send one control: it goes down with the first of them and up with
	// the last.
	padSent  map[Button]padKey
	keysHeld map[uint16]string
	// localKeyboardUI is used only by the opt-in delegated native keyboard
	// boundary. Ordinary input keeps its existing handling.
	localKeyboardUI *nativeKeyboardUIState
	localKeyboardID localhid.DeviceID
	mapper          *controller.Mapper
	// remap is the profile saved for this pad, applied after mapper.
	remap *inputmap.Remapper
	// device is the pad's evdev identity, and mapping the step-by-step
	// mapping its mapper reads, if any.
	device  controller.Device
	mapping string
	// learner reads the pad's raw controls while Settings › Controllers
	// maps it step by step. Nothing it reads reaches menus or a core.
	learner *controller.Learner
	// info is what Settings › Controllers shows for a real pad; its GUID is
	// empty for anything else.
	info PadInfo
	// profileGen is the App profile generation remap was read at.
	profileGen uint64
	// ledSynced and ledPlayer track the last player-port LED state pushed to sysfs.
	ledSynced bool
	ledPlayer int
	// settingsCombo enables Select+Start in menus to toggle Settings, and its
	// menu* fields track chord/unwind state while the pad isn't owned by a core.
	settingsCombo  bool
	menuSelectDown bool
	menuStartDown  bool
	menuChordFired bool
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
	// padProfile reads the profile saved for a pad's GUID. Nil reads every
	// pad as its mapping does.
	padProfile padProfileFunc
	// profileGen is the App profile generation every pad's remap is at.
	profileGen uint64
	// kindOf and openFile are nil in production. Tests substitute them
	// because a fixture node cannot answer EVIOCGBIT.
	kindOf   func(fd int) InputKind
	openFile func(path string) (int, error)
	// padID stands in for EVIOCGID where kindOf does: it gives a test pad's
	// vendor and product. Nil reads every test pad as 0000:0000.
	padID func(fd int) (vendor, product uint16)
	// sysClassInput overrides /sys/class/input in tests for deterministic LED checks.
	sysClassInput string
	// Nil in production until a reviewed authority/snapshot integration
	// explicitly installs the gate. A keyboard never falls back if installed.
	localKeyboardGate  *nativeKeyboardGate
	localKeyboardNewID func() (localhid.DeviceID, error)
}

// installNativeKeyboardGate is intentionally not called by production yet.
// Its future caller must first latch App v2 quarantine and prove any previous
// guest source retired. It fences event dispatch, then adopts every already-
// open keyboard; partial failure leaves the gate installed so no descriptor
// can fall back to legacy input.
func (ins *nativeInputs) installNativeKeyboardGate(g *nativeKeyboardGate) error {
	if g == nil || ins.localKeyboardGate != nil {
		return errors.New("native keyboard gate already installed or invalid")
	}
	ins.localKeyboardGate = g
	for _, in := range ins.devices {
		if in != nil && in.kind == InputKeyboard {
			if err := ins.adoptNativeKeyboard(in); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ins *nativeInputs) adoptNativeKeyboard(in *nativeInput) error {
	if ins.localKeyboardGate == nil || in == nil || in.kind != InputKeyboard {
		return errors.New("invalid native keyboard gate adoption")
	}
	// Legacy repeat ownership has no per-device identity. Do not discard a
	// previously held keyboard command and leave its aggregate repeat alive;
	// activation must occur before dispatch or after proven legacy retirement.
	heldShift := false
	for _, down := range in.shifts {
		heldShift = heldShift || down
	}
	if len(in.keysHeld) != 0 || len(in.commands) != 0 || heldShift {
		ins.localKeyboardGate.fault(nativeFaultAuthority)
		return errors.New("legacy keyboard holds require retirement")
	}
	newID := ins.localKeyboardNewID
	if newID == nil {
		newID = func() (localhid.DeviceID, error) {
			var id localhid.DeviceID
			_, err := rand.Read(id[:])
			return id, err
		}
	}
	id, err := newID()
	if err != nil || id == (localhid.DeviceID{}) {
		ins.localKeyboardGate.fault(nativeFaultAuthority)
		return errors.New("native keyboard incarnation unavailable")
	}
	if err = ins.localKeyboardGate.add(id, in); err != nil {
		return err
	}
	in.localKeyboardID = id
	return nil
}

// gatedKeyboardRecord owns all keyboard records while a gate is installed.
// The bool means handled; errors must never fall back to the legacy path.
func (ins *nativeInputs) gatedKeyboardRecord(in *nativeInput, typ, code uint16, value int32, now time.Time) (bool, bool, error) {
	if ins.localKeyboardGate == nil || in.kind != InputKeyboard {
		return false, false, nil
	}
	if in.localKeyboardID == (localhid.DeviceID{}) {
		ins.localKeyboardGate.fault(nativeFaultAuthority)
		return true, false, errors.New("native keyboard is not registered")
	}
	_, quit, err := ins.localKeyboardGate.dispatch(in.localKeyboardID, typ, code, value, now)
	return true, quit, err
}

// padProfileFunc reads the profile saved for a pad's GUID: the zero
// Profile when there is none.
type padProfileFunc func(guid string) (inputmap.Profile, error)

func openNativeInputs(spec, label string, padProfile padProfileFunc) (*nativeInputs, error) {
	return openNativeInputsDir(spec, label, "/dev/input", padProfile)
}

func openNativeInputsDir(spec, label, dir string, padProfile padProfileFunc) (*nativeInputs, error) {
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
		automatic:  automatic,
		label:      label,
		dir:        dir,
		scanEvery:  time.Second,
		lastScan:   time.Now(),
		padProfile: padProfile,
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
	in := &nativeInput{fd: fd, path: path, kind: kind, player: noLocalPlayer}
	if ins.localKeyboardGate != nil && kind == InputKeyboard {
		if err := ins.adoptNativeKeyboard(in); err != nil {
			unix.Close(fd)
			return fmt.Errorf("%s input: %w", ins.label, err)
		}
	}
	if kind == InputMouse {
		in.mapper = controller.NewMouseMapper()
	}
	if kind == InputGamepad {
		var vendor, product uint16
		if ins.kindOf == nil {
			var device controller.Device
			in.mapper, in.remap, in.mapping, device = nativeGamepadMapper(fd, ins.padProfile)
			vendor, product = device.Vendor, device.Product
			if in.mapper != nil {
				in.device = device
				in.info = PadInfo{Name: strings.TrimSpace(device.Name), GUID: device.GUID(), Vendor: vendor, Product: product,
					Family: promptFamily(device.Family()), Recognised: device.Recognised(), Bluetooth: device.Bluetooth(), Tested: device.Tested(),
					NoGuide: !in.mapper.HasGuide()}
				in.profileGen = app.PadProfileGeneration()
				if ins.padProfile != nil {
					if profile, err := ins.padProfile(in.info.GUID); err == nil {
						in.settingsCombo = padSettingsComboEnabled(in.info, profile)
					} else {
						in.settingsCombo = in.info.NoGuide
					}
				} else {
					in.settingsCombo = in.info.NoGuide
				}
			}
		} else if ins.padID != nil {
			vendor, product = ins.padID(fd)
		}
		in.yields = yieldsPort(vendor, product)
	}
	// A pad takes a controller port. So does a node named with -input that
	// the classifier could not place; automatic mode never keeps one.
	if kind == InputGamepad || kind == InputNone {
		what := "gamepad"
		if kind == InputNone {
			what = "input"
		}
		in.player = ins.freePlayer()
		yielder := ins.yieldingPad(in)
		var directions []yieldedDirection
		if yielder != nil {
			directions = yielder.yieldedDirections(app)
			ins.releasePort(app, yielder)
			in.player = yielder.player
			yielder.player = noLocalPlayer
		}
		if in.player == noLocalPlayer {
			fmt.Fprintf(os.Stderr, "%s: %s %s: both controller ports are taken; menu only\n", ins.label, what, path)
		} else {
			fmt.Fprintf(os.Stderr, "%s: %s %s: controller port %d\n", ins.label, what, path, in.player+1)
		}
		ins.devices = append(ins.devices, in)
		if yielder != nil {
			yielder.player = ins.freePlayer()
			yielder.restoreYieldedDirections(app, directions)
			if yielder.player == noLocalPlayer {
				fmt.Fprintf(os.Stderr, "%s: gamepad %s: gave its controller port to %s; menu only\n", ins.label, yielder.path, path)
			} else {
				fmt.Fprintf(os.Stderr, "%s: gamepad %s: gave its controller port to %s; controller port %d\n", ins.label, yielder.path, path, yielder.player+1)
			}
		}
	} else {
		ins.devices = append(ins.devices, in)
	}
	if app != nil {
		ins.publishPads(app)
		app.AttachInput(kind, fd)
	}
	return nil
}

// publishPads tells the app each real pad's identity and controller port
// and keeps hardware player-port LEDs synchronized with assigned ports.
func (ins *nativeInputs) publishPads(app *App) {
	for _, in := range ins.devices {
		player := 0
		if in.player != noLocalPlayer {
			player = int(in.player) + 1
		}
		if in.kind == InputGamepad && (!in.ledSynced || in.ledPlayer != player) {
			ins.syncPadLED(in, player)
			in.ledSynced = true
			in.ledPlayer = player
		}
		if in.info.GUID == "" {
			continue
		}
		in.info.Player = player
		app.SetPadInfo(in.fd, in.info)
	}
}

func (ins *nativeInputs) syncPadLED(in *nativeInput, player int) {
	syncEvdevPlayerLEDs(ins.sysClassInput, in.path, player)
}

// syncProfiles reloads the pads' saved profiles after Settings › Controllers
// changes one. A pad with a button down waits until it is released, so the
// button comes up as the control it went down as.
func (ins *nativeInputs) syncProfiles(app *App) {
	gen := app.PadProfileGeneration()
	if gen == ins.profileGen || ins.padProfile == nil {
		return
	}
	waiting := false
	for _, in := range ins.devices {
		if in.info.GUID == "" || in.profileGen == gen {
			continue
		}
		if len(in.padHeld) > 0 || len(in.controls) > 0 {
			waiting = true
			continue
		}
		profile, err := ins.padProfile(in.info.GUID)
		if err != nil {
			profile = inputmap.Profile{}
		}
		noGuideChanged := false
		if profile.Mapping != in.mapping {
			in.mapper, in.mapping, _ = compilePadMapping(in.device, profile.Mapping)
			noGuide := !in.mapper.HasGuide()
			if noGuide != in.info.NoGuide {
				in.info.NoGuide = noGuide
				noGuideChanged = true
			}
		}
		in.remap, _ = profileRemap(profile)
		in.settingsCombo = padSettingsComboEnabled(in.info, profile)
		if !in.settingsCombo {
			in.menuSelectDown = false
			in.menuStartDown = false
			in.menuChordFired = false
		}
		if noGuideChanged && app != nil {
			app.SetPadInfo(in.fd, in.info)
		}
		in.profileGen = gen
	}
	if !waiting {
		ins.profileGen = gen
	}
}

func (ins *nativeInputs) deviceKind(fd int) InputKind {
	if ins != nil && ins.kindOf != nil {
		return ins.kindOf(fd)
	}
	return nativeDeviceKind(fd)
}

// freePlayer is the lowest controller port no connected pad holds. Ports
// follow the order pads are opened (path order at startup, then hotplug). A
// pad retains its port until unplugged unless it is a yielding fixture.
func (ins *nativeInputs) freePlayer() int8 {
	var taken [localControllerPorts]bool
	for _, in := range ins.devices {
		if in.player != noLocalPlayer && int(in.player) < len(taken) {
			taken[in.player] = true
		}
	}
	for player, held := range taken {
		if !held {
			return int8(player)
		}
	}
	return noLocalPlayer
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

// retireNativeKeyboards fences every source before the App/worker is stopped.
// It does not infer that a guest release or neutral operation has completed.
func (ins *nativeInputs) retireNativeKeyboards() error {
	if ins.localKeyboardGate == nil {
		return nil
	}
	var result error
	for _, in := range ins.devices {
		if in.kind != InputKeyboard {
			continue
		}
		if in.localKeyboardID == (localhid.DeviceID{}) {
			result = errors.Join(result, errors.New("unregistered native keyboard at shutdown"))
			continue
		}
		if err := ins.localKeyboardGate.retire(in.localKeyboardID); err != nil {
			result = errors.Join(result, err)
		}
		in.localKeyboardID = localhid.DeviceID{}
	}
	return result
}

// await may return nil only after the exact source has confirmed neutral or
// retired; it must honor ctx. The sequence closes/stops even on failure, but
// never interprets timeout, missing worker or a close as proof of neutral.
func shutdownNativeInputSequence(ctx context.Context, gated bool, retire func() error, await func(context.Context) error, stop, closeInputs func()) error {
	var result error
	if gated {
		if retire == nil {
			result = errors.New("native retirement unavailable")
		} else {
			result = retire()
		}
		if await == nil {
			result = errors.Join(result, errors.New("native neutral confirmation unavailable"))
		} else {
			result = errors.Join(result, await(ctx))
			if err := ctx.Err(); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	stop()
	closeInputs()
	return result
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
	var relative [8]byte
	const relativeRequest = uintptr(0x80000000 | (8 << 16) | ('E' << 8) | 0x22)
	_, _, relErr := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), relativeRequest, uintptr(unsafe.Pointer(&relative[0])))
	bus := binary.LittleEndian.Uint16(id[:2])
	name := nativeDeviceName(fd)
	if relErr == 0 && controller.EligibleMouse(bus, name, relative[0]&3 == 3, nativeHasKey(keys[:], 272) || nativeHasKey(keys[:], 273)) {
		return InputMouse
	}
	return nativeKindWithIdentity(keys[:], bus, name)
}

func nativeKindWithIdentity(keys []byte, bus uint16, name string) InputKind {
	buttons := nativeHasKey(keys, 304) || nativeHasKey(keys, 305)
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
	// lack BTN_SOUTH/EAST. Their readable, non-virtual identity is enough to
	// admit those game keys; the controller mapper identifies the layout.
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

// nativeGamepadMapper gives the controller mapper the pad's identity, its
// EVIOCGBIT key/axis capabilities and the range of every reported axis. It
// also returns the pad's identity.
func nativeGamepadMapper(fd int, padProfile padProfileFunc) (*controller.Mapper, *inputmap.Remapper, string, controller.Device) {
	var id [8]byte
	const idRequest = uintptr(0x80000000 | (8 << 16) | ('E' << 8) | 0x02)
	_, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), idRequest, uintptr(unsafe.Pointer(&id[0])))
	if err != 0 {
		return nil, nil, "", controller.Device{}
	}
	device := controller.Device{
		Name:    nativeDeviceName(fd),
		Bus:     binary.LittleEndian.Uint16(id[0:2]),
		Vendor:  binary.LittleEndian.Uint16(id[2:4]),
		Product: binary.LittleEndian.Uint16(id[4:6]),
		Version: binary.LittleEndian.Uint16(id[6:8]),
		Keys:    make([]byte, 96),
		Abs:     make([]byte, 8),
		Axes:    map[uint16]controller.Range{},
	}
	const keysRequest = uintptr(0x80000000 | (96 << 16) | ('E' << 8) | 0x21)
	const absRequest = uintptr(0x80000000 | (8 << 16) | ('E' << 8) | 0x23)
	unix.Syscall(unix.SYS_IOCTL, uintptr(fd), keysRequest, uintptr(unsafe.Pointer(&device.Keys[0])))
	unix.Syscall(unix.SYS_IOCTL, uintptr(fd), absRequest, uintptr(unsafe.Pointer(&device.Abs[0])))
	for code := uint16(0); code < 64; code++ {
		if !nativeHasKey(device.Abs, int(code)) {
			continue
		}
		var info [24]byte
		request := uintptr(0x80000000|(24<<16)|('E'<<8)) | uintptr(0x40+code)
		if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&info[0]))); e == 0 {
			device.Axes[code] = controller.Range{Min: int32(binary.LittleEndian.Uint32(info[4:])), Max: int32(binary.LittleEndian.Uint32(info[8:]))}
		}
	}
	mapper, remap, mapping := padMapper(device, padProfile)
	return mapper, remap, mapping, device
}

// nativeAxisRest reads where each of a pad's axes is now, from EVIOCGABS.
func nativeAxisRest(fd int, device controller.Device) map[uint16]int32 {
	rest := map[uint16]int32{}
	for code := range device.Axes {
		var info [24]byte
		request := uintptr(0x80000000|(24<<16)|('E'<<8)) | uintptr(0x40+code)
		if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&info[0]))); e == 0 {
			rest[code] = int32(binary.LittleEndian.Uint32(info[0:]))
		}
	}
	return rest
}

// padMapper reads a pad through its mapping and the profile saved for its
// GUID, and logs the GUID a profile is saved under. It returns the
// step-by-step mapping the mapper reads, if any.
func padMapper(device controller.Device, padProfile padProfileFunc) (*controller.Mapper, *inputmap.Remapper, string) {
	guid := device.GUID()
	var (
		profile inputmap.Profile
		notes   []string
	)
	if padProfile != nil {
		var err error
		if profile, err = padProfile(guid); err != nil {
			notes = append(notes, "saved profile ignored: "+err.Error())
			profile = inputmap.Profile{}
		}
	}
	mapper, mapping, err := compilePadMapping(device, profile.Mapping)
	if err != nil {
		notes = append(notes, "saved mapping ignored: "+err.Error())
	}
	remap, err := profileRemap(profile)
	if err != nil {
		notes = append(notes, "saved profile ignored: "+err.Error())
	} else if remap != nil {
		notes = append(notes, "saved profile")
	}
	name := mapper.Mapping()
	if name == "" {
		name = "fallback layout"
	}
	if device.Tested() {
		notes = append(notes, "tested")
	}
	for _, note := range notes {
		name += "; " + note
	}
	fmt.Fprintf(os.Stderr, "tenfoot: gamepad %q %04x:%04x v%04x bus %d guid %s: %s; %s glyphs\n",
		device.Name, device.Vendor, device.Product, device.Version, device.Bus, guid, name, device.Family())
	return mapper, remap, mapping
}

// compilePadMapping reads a pad through the mapping its player made step
// by step, or through its own mapping when there is none or it does not
// compile. It returns the step-by-step mapping in use.
func compilePadMapping(device controller.Device, mapping string) (*controller.Mapper, string, error) {
	if mapping == "" {
		return controller.NewMapper(device), "", nil
	}
	mapper, err := controller.NewLearnedMapper(device, mapping)
	if err != nil {
		return controller.NewMapper(device), "", err
	}
	return mapper, mapping, nil
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
	ins.publishPads(app)
	for _, in := range ins.devices {
		app.SeedInput(in.kind, in.fd)
	}
	app.FinishInputSeed()
}

func (ins *nativeInputs) drop(app *App, lost *nativeInput) {
	gatedKeyboard := ins.localKeyboardGate != nil && lost.kind == InputKeyboard
	if gatedKeyboard && lost.localKeyboardID != (localhid.DeviceID{}) {
		_ = ins.localKeyboardGate.retire(lost.localKeyboardID)
	}
	if lost.kind == InputMouse {
		app.ReleasePlayMouse(lost.fd)
	}
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
	if !gatedKeyboard {
		for _, cmd := range lost.commands {
			app.Release(cmd)
		}
		ins.releaseLostPlayInput(app, lost)
	}
	app.DetachInput(InputKeyboard, lost.fd)
	app.DetachInput(InputGamepad, lost.fd)
	ins.publishPads(app)
}

func (ins *nativeInputs) releaseLostPlayInput(app *App, lost *nativeInput) {
	app.mu.Lock()
	// Only an admitted contribution can keep a shared core key down. Use
	// its original mapping rather than reinterpreting it after a mode change.
	remainingCore := map[playHIDKey]bool{}
	for _, in := range ins.devices {
		for code, name := range in.keysHeld {
			key := physicalPlayKey{source: in.fd, key: playHeldKey(name, nativeHIDUsage(code))}
			if coreKey, admitted := app.playKeyAdmitted[key]; admitted && !app.playKeySuppressed[key] {
				remainingCore[coreKey] = true
			}
		}
	}
	padEvents := releasePortLocked(app, lost)
	forward := localPadForwardLocked(app)
	_, delegatedKeyboard := app.localKeyboardBindingLocked()
	keyForward := (forward || delegatedKeyboard) && app.playHIDRouteReadyLocked()
	var keyEvents []remoteinput.Event
	for code, name := range lost.keysHeld {
		key := physicalPlayKey{source: lost.fd, key: playHeldKey(name, nativeHIDUsage(code))}
		coreKey, admitted := app.playKeyAdmitted[key]
		delete(app.playKeyHeld, key)
		delete(app.playKeySuppressed, key)
		delete(app.playKeyAdmitted, key)
		if !admitted {
			continue
		}
		if _, held := app.playHIDHeld[coreKey]; held && !remainingCore[coreKey] {
			if !keyForward && app.session.State == "active" {
				if app.playKeyPendingRelease == nil {
					app.playKeyPendingRelease = make(map[physicalPlayKey]playHIDKey)
				}
				app.playKeyPendingRelease[key] = coreKey
				continue
			}
			delete(app.playHIDHeld, coreKey)
			keyEvents = append(keyEvents, remoteinput.Event{Player: coreKey.player, Device: coreKey.device, Kind: coreKey.kind, Code: coreKey.code, Action: remoteinput.ActionRelease})
		}
	}
	feed := app.localFeed
	if keyForward {
		if len(keyEvents) != 0 {
			app.traceKeyRouteLocked("disconnect-neutral", lost.fd)
		}
		for _, event := range keyEvents {
			app.queuePhysicalKeyboardLocked(event)
		}
	}
	app.mu.Unlock()
	if forward && feed != nil {
		for _, event := range padEvents {
			_ = feed.Send(event, time.Now())
		}
	}
}

// releasePort takes a still-connected pad off its controller port as an
// unplug would: what it held comes up and its player departs. The caller
// gives the port to another pad.
func (ins *nativeInputs) releasePort(app *App, in *nativeInput) {
	if app == nil {
		in.padHeld = nil
		in.padSent = nil
		return
	}
	app.mu.Lock()
	padEvents := releasePortLocked(app, in)
	feed := app.localFeed
	forward := localPadForwardLocked(app)
	app.mu.Unlock()
	if forward && feed != nil {
		for _, event := range padEvents {
			_ = feed.Send(event, time.Now())
		}
	}
}

// releasePortLocked forgets what in held on its port and returns the
// releases and departure to send for it. in keeps its player; the caller
// reassigns it.
func releasePortLocked(app *App, in *nativeInput) []remoteinput.Event {
	var padEvents []remoteinput.Event
	if in.player != noLocalPlayer {
		// The port was this pad's alone, so everything held on it comes up.
		player := uint8(in.player)
		for _, event := range in.padHeld {
			event.Action = remoteinput.ActionRelease
			event.Value = 0
			padEvents = append(padEvents, event)
		}
		in.padHeld = nil
		in.padSent = nil
		app.forgetPadLocked(player, time.Now())
		// After its releases, the pad gives its port back. mister-agent
		// drops that player's claim and leaves the other pad alone.
		padEvents = append(padEvents, remoteinput.LocalPlayerDeparture(player))
	}
	if !app.playPadPair.anyHeld() {
		app.playPadChordSince = time.Time{}
		app.playPadChordFired = false
	}
	return padEvents
}

func localPadForwardLocked(app *App) bool {
	return (app.session.State == "active") && !app.playHIDFailClosedLocked()
}

func menuQueuedSeq(dev gfx.Device) uint64 {
	if display, ok := dev.(*gfx.MenuDisplay); ok {
		return display.QueuedSeq()
	}
	return 0
}
