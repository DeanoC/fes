//go:build linux

package tenfoot

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/controller"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
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
		app.logKitMenuReadiness()
	}
	inputSpec := opts.Input
	if opts.Smoke {
		inputSpec = "none"
	}
	inputs, err := openNativeInputs(inputSpec, label, app.padProfile)
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
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	parked := false
	frames := 0
	defer app.reportSessionReady(false, 0)
	var hold presentHold
	trace, stopTrace := startFrameTrace(ctx, app, dev, label)
	defer stopTrace()
	for {
		select {
		case <-ctx.Done():
			if opts.Smoke {
				return fmt.Errorf("%s smoke: %w", label, ctx.Err())
			}
			return nil
		case scheduled := <-ticker.C:
			// The ticker delivers its scheduled time, which is already in
			// the past when this iteration starts late. Trace from the
			// moment work begins; App timing still uses the schedule.
			started := time.Now()
			quit, err := poll(app, scheduled)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}
			polled := time.Now()
			app.Tick(scheduled)
			ticked := time.Now()
			snap := app.Snapshot()
			if display, ok := dev.(*gfx.MenuDisplay); ok && snap.Surround.Active {
				snap.Surround.Picture = display.SessionPicture()
			}
			snapped := time.Now()
			skip, pauseErr := hold.skip(ctx, dev, snap)
			if pauseErr != nil {
				app.completeHostLaunchHandoff(pauseErr)
				if display, ok := dev.(*gfx.MenuDisplay); ok {
					display.Resume()
				}
				hold.armed = false
				hold.paused = false
			} else if hold.paused {
				app.completeHostLaunchHandoff(nil)
			}
			_, menuDevice := dev.(*gfx.MenuDisplay)
			app.reportSessionReady(menuDevice && skip && pauseErr == nil && hold.paused, snap.sessionObservation)
			if !skip {
				before := menuQueuedSeq(dev)
				parked = presentFrame(painter, snap, textures, labels, parked)
				trace.tick(started, polled, ticked, snapped, time.Now(), before, menuQueuedSeq(dev), snap)
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
	mapper   *controller.Mapper
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
					Family: promptFamily(device.Family()), Recognised: device.Recognised()}
				in.profileGen = app.PadProfileGeneration()
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

// publishPads tells the app each real pad's identity and controller port.
// A port can change hands when a pad attaches or leaves.
func (ins *nativeInputs) publishPads(app *App) {
	for _, in := range ins.devices {
		if in.info.GUID == "" {
			continue
		}
		in.info.Player = 0
		if in.player != noLocalPlayer {
			in.info.Player = int(in.player) + 1
		}
		app.SetPadInfo(in.fd, in.info)
	}
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
		if profile.Mapping != in.mapping {
			in.mapper, in.mapping, _ = compilePadMapping(in.device, profile.Mapping)
		}
		in.remap, _ = profileRemap(profile)
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

// yieldedDirection is a still-held hat/stick direction at the time the
// fixture is moved. Button is before user remapping, while code is the
// remapped core key used for suppression and per-player tracking.
type yieldedDirection struct {
	button     Button
	code       remoteinput.Code
	suppressed bool
	forwarded  bool
}

// Snapshot while the old player's held and suppression records still exist.
// Menu-only analog axes use in.axes, not in.hatHeld; their existing menu hold
// is unaffected by a port reassignment and must not be pressed a second time.
func (in *nativeInput) yieldedDirections(app *App) []yieldedDirection {
	if app == nil || in.player == noLocalPlayer || len(in.hatHeld) == 0 {
		return nil
	}
	var result []yieldedDirection
	seen := map[Button]bool{}
	app.mu.Lock()
	defer app.mu.Unlock()
	for _, button := range in.hatHeld {
		if button == ButtonNone || seen[button] {
			continue
		}
		seen[button] = true
		e := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: LogicalFromButton(button)}
		if in.remap != nil {
			e = in.remap.Apply(e)
		}
		key := padKey{uint8(in.player), e.Code}
		_, sent := in.padHeld[key]
		_, tracked := app.playPadHeld[key]
		// Never synthesize a Select/Start press or a swallowed transition
		// event on the new port. Swallow its eventual release there too.
		suppressed := app.playPadSuppressed[key] || app.sessionDisplayBusy || app.sessionDisplayUncertain ||
			app.stopPhase == "stopping" || app.retryStopLock || app.playHIDFailClosedLocked() ||
			e.Code == remoteinput.ButtonSelect || e.Code == remoteinput.ButtonStart
		coreOwns := app.needsSessionDisplayLocked() && app.session.State == "active" && !app.roomDuringPlay
		result = append(result, yieldedDirection{button: button, code: e.Code, suppressed: suppressed, forwarded: sent && tracked && coreOwns})
	}
	return result
}

// Restore only directions the active machine was already seeing. The
// release/departure of the old player is sent before this is called. A
// portless fixture cannot send to a machine, and ports do not auto-compact.
func (in *nativeInput) restoreYieldedDirections(app *App, directions []yieldedDirection) {
	if app == nil || in.player == noLocalPlayer {
		return
	}
	for _, direction := range directions {
		if direction.suppressed {
			app.mu.Lock()
			if app.needsSessionDisplayLocked() && app.session.State == "active" {
				if app.playPadSuppressed == nil {
					app.playPadSuppressed = make(map[padKey]bool)
				}
				app.playPadSuppressed[padKey{uint8(in.player), direction.code}] = true
			}
			app.mu.Unlock()
			continue
		}
		if direction.forwarded {
			in.button(app, direction.button, 1, time.Now())
		}
	}
}

// yieldingPad is the pad that gives up its controller port to arriving, or
// nil. A pad that yields gives way to one that does not, when it holds a
// lower port than arriving would take or arriving would take none. The
// lowest such port goes first.
func (ins *nativeInputs) yieldingPad(arriving *nativeInput) *nativeInput {
	if arriving.kind != InputGamepad || arriving.yields {
		return nil
	}
	var yielder *nativeInput
	for _, in := range ins.devices {
		if !in.yields || in.player == noLocalPlayer {
			continue
		}
		if arriving.player != noLocalPlayer && in.player > arriving.player {
			continue
		}
		if yielder == nil || in.player < yielder.player {
			yielder = in
		}
	}
	return yielder
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
	ins.syncProfiles(app)
devices:
	for _, in := range append([]*nativeInput(nil), ins.devices...) {
		if in.info.GUID != "" {
			ins.syncLearner(app, in)
		}
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
				if in.learner != nil {
					if source, ok := in.learner.Feed(typ, code, value); ok {
						app.NoteInput(InputGamepad, in.fd)
						app.LearnPadSource(in.fd, source, now)
					}
					continue
				}
				if in.kind == InputMouse && in.mapper != nil {
					if event, ok := in.mapper.Map(typ, code, value); ok {
						x, y, buttons, _ := remoteinput.MouseVector(event)
						app.HandlePlayMouseReport(in.fd, x, y, buttons)
					}
					if in.mapper.MouseFault() {
						if ins.automatic {
							ins.drop(app, in)
							continue devices
						}
						return false, fmt.Errorf("%s input: mouse report exceeds signed16 range", ins.label)
					}
					continue
				}
				if typ == 1 {
					guide := code == 316
					if in.mapper != nil {
						guide = in.mapper.Guide(code)
					}
					if in.mapper != nil && !guide {
						if ins.padEvents(app, in, in.mapper.Events(typ, code, value), now) {
							return true, nil
						}
						continue
					}
					if guide {
						session := app.Snapshot().Session
						if value == 1 {
							if command := sessionGuideCommand(session); command != CmdNone {
								app.Press(command, now)
							} else if app.coreOwnsPadInput() {
								app.NoteInput(InputGamepad, in.fd)
								app.RequestRoomlessStop(now)
							} else {
								app.NoteInput(InputGamepad, in.fd)
								in.setButton(code, CmdSettings, true)
							}
						} else if app.coreOwnsPadInput() {
							if value != 0 {
								app.NoteInput(InputGamepad, in.fd)
							}
						} else if value == 0 {
							in.setButton(code, CmdSettings, false)
						}
					} else if button := nativeButton(code); button != ButtonNone {
						in.button(app, button, value, now)
					} else if code < 256 {
						if ins.key(app, in, code, value, now) {
							return true, nil
						}
					}
				} else if typ == 3 {
					if in.mapper != nil {
						if ins.padEvents(app, in, in.mapper.Events(typ, code, value), now) {
							return true, nil
						}
					} else if code == 16 || code == 17 {
						in.hat(app, code, value, now)
					}
				}
				// Preserve quick down/up pairs even when drained in one render frame.
				if (typ == 1 && code >= 256) || (typ == 3 && in.mapper == nil && (code == 16 || code == 17)) {
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

// syncLearner starts reading a pad's raw controls once Settings ›
// Controllers maps it step by step and the pad has nothing held, so the
// press that started mapping comes up through the menus. When mapping ends
// it stops, and keys still down then come up unseen.
func (ins *nativeInputs) syncLearner(app *App, in *nativeInput) {
	learning := app.PadLearning(in.fd)
	switch {
	case learning && in.learner == nil:
		if len(in.padHeld) > 0 || len(in.controls) > 0 || len(in.hatHeld) > 0 || len(in.axes) > 0 {
			return
		}
		var rest map[uint16]int32
		if ins.kindOf == nil {
			rest = nativeAxisRest(in.fd, in.device)
		}
		in.learner = controller.NewLearner(in.device, rest)
	case !learning && in.learner != nil:
		if in.mapper != nil {
			for _, code := range in.learner.HeldKeys() {
				in.mapper.Suppress(code)
			}
		}
		in.learner = nil
	}
}

func sessionGuideCommand(session SessionSnapshot) Command {
	if !session.HardwareRoom {
		return CmdNone
	}
	if session.Coleco && !session.ControlsOpen {
		return CmdSearch
	}
	return CmdHome
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

// padEvents takes what a pad's mapping reads from one evdev event: the
// stick or hat it moves and the buttons it presses or releases, including
// buttons a mapping puts on an axis or a hat direction.
func (ins *nativeInputs) padEvents(app *App, in *nativeInput, events []remoteinput.Event, now time.Time) bool {
	for _, e := range events {
		switch e.Kind {
		case remoteinput.KindButton:
			value := int32(0)
			if e.Action == remoteinput.ActionPress {
				value = 1
			}
			in.button(app, ButtonFromLogical(e.Code), value, now)
		case remoteinput.KindAxis:
			in.axis(app, e.Code, e.Value, now)
		}
	}
	return len(events) > 0 && ins.applyButtons(app, now)
}

func (in *nativeInput) button(app *App, button Button, value int32, now time.Time) {
	if value == 2 || button == ButtonNone {
		return
	}
	app.NotePadButton(in.fd, LogicalFromButton(button), value != 0)
	e := remoteinput.Event{
		Device: remoteinput.DeviceGamepad,
		Kind:   remoteinput.KindButton,
		Code:   LogicalFromButton(button),
		Action: remoteinput.ActionRelease,
	}
	if value != 0 {
		e.Action = remoteinput.ActionPress
	}
	if in.remap != nil {
		e = in.remap.Apply(e)
	}
	if in.player == noLocalPlayer {
		// Both controller ports belong to other pads. This pad never reaches
		// a machine. It presses menu controls only where a pad with a port
		// would, and its releases always clear them. Keypad Select follows the
		// focused launcher's confirmation mapping too.
		if value == 0 || app.portlessPadReachesMenu(e.Code) {
			in.setButton(uint16(button), app.localPadCommand(e), value != 0)
		}
		if value != 0 {
			app.NoteInput(InputGamepad, in.fd)
		}
		return
	}
	e.Player = uint8(in.player)
	key := padKey{e.Player, e.Code}
	if value != 0 {
		shared := in.holding(key)
		if in.padSent == nil {
			in.padSent = make(map[Button]padKey)
		}
		in.padSent[button] = key
		if shared {
			return
		}
	} else if sent, ok := in.padSent[button]; ok {
		// Release what this button pressed, even if the profile changed.
		key = sent
		e.Code = sent.code
		delete(in.padSent, button)
		if in.holding(key) {
			return
		}
	}
	if in.padHeld == nil {
		in.padHeld = make(map[padKey]remoteinput.Event)
	}
	if value != 0 {
		in.padHeld[key] = e
	} else {
		delete(in.padHeld, key)
	}
	if app.HandleLocalPad(e, now) {
		if value != 0 {
			app.NoteInput(InputGamepad, in.fd)
		}
		return
	}
	in.setButton(uint16(button), app.localPadCommand(e), value != 0)
	if value != 0 {
		app.NoteInput(InputGamepad, in.fd)
	}
}

// holding reports whether a held button already holds key on the port.
func (in *nativeInput) holding(key padKey) bool {
	for _, sent := range in.padSent {
		if sent == key {
			return true
		}
	}
	return false
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
		if cmd == CmdNone {
			// A stick at rest holds nothing.
			delete(in.axes, uint16(code))
		} else {
			in.axes[uint16(code)] = cmd
		}
		in.button(app, nativeDirectionButton(cmd), 1, now)
	}
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

// key keeps a shared core key held until every other unsuppressed evdev key
// contributing it releases. Contributions stay distinct by physical key,
// including left and right modifiers on one keyboard.
func (ins *nativeInputs) key(app *App, in *nativeInput, code uint16, value int32, now time.Time) bool {
	if value == 0 && in.kind == InputKeyboard && app != nil {
		app.mu.Lock()
		hid := app.session.CoreKeyboardHID
		keyboard := app.session.CoreKeyboard
		app.mu.Unlock()
		coreKey := func(code uint16, name string) (playHIDKey, bool) {
			var event remoteinput.Event
			var ok bool
			if hid {
				event, ok = hidkeys.Event(nativeHIDUsage(code), false)
			} else {
				event, ok = playhid.Event(name, false, keyboard)
			}
			return playHIDKey{event.Player, event.Device, event.Kind, event.Code}, ok
		}
		physical := physicalPlayKey{source: in.fd, key: playHeldKey(nativeKeyName(code), nativeHIDUsage(code))}
		app.mu.Lock()
		key, valid := app.playKeyAdmitted[physical]
		app.mu.Unlock()
		if !valid {
			key, valid = coreKey(code, nativeKeyName(code))
		}
		if valid {
			for _, other := range ins.devices {
				if other == nil || other.kind != InputKeyboard {
					continue
				}
				for heldCode, heldName := range other.keysHeld {
					otherPhysical := physicalPlayKey{source: other.fd, key: playHeldKey(heldName, nativeHIDUsage(heldCode))}
					if otherPhysical == physical {
						continue
					}
					app.mu.Lock()
					otherKey, otherAdmitted := app.playKeyAdmitted[otherPhysical]
					otherSuppressed := app.playKeySuppressed[otherPhysical]
					app.mu.Unlock()
					if otherAdmitted && otherKey == key && !otherSuppressed {
						// Retire this physical hold and its menu command without
						// releasing the core key that the other keyboard holds.
						app.consumeSuppressedPlayKeyFrom(in.fd, nativeKeyName(code), nativeHIDUsage(code), false)
						app.mu.Lock()
						delete(app.playKeyAdmitted, physicalPlayKey{source: in.fd, key: playHeldKey(nativeKeyName(code), nativeHIDUsage(code))})
						app.mu.Unlock()
						delete(in.keysHeld, code)
						if (code == 42 || code == 54) && in.shifts != nil {
							in.shifts[code] = false
						}
						if in.commands != nil {
							app.Release(in.commands[code])
							delete(in.commands, code)
						}
						return false
					}
				}
			}
		}
	}
	return in.key(app, code, value, now)
}

// nativeKeyF12 is MiSTer's menu key. A host session that can change
// removable media opens that selector,
// including when the library has no .p files yet; otherwise it opens the
// playing room, and with no room it stops the session. The key is never
// forwarded.
const nativeKeyF12 = 88

func (in *nativeInput) key(app *App, code uint16, value int32, now time.Time) bool {
	if code == nativeKeyF12 {
		if value == 2 {
			return false
		}
		if in.keysHeld == nil {
			in.keysHeld = make(map[uint16]string)
		}
		if value == 1 {
			in.keysHeld[code] = "f12"
			app.NoteInput(InputKeyboard, in.fd)
		} else {
			delete(in.keysHeld, code)
		}
		app.handlePlayHIDScancode(in.fd, "f12", hidKeyboardF12, value != 0, now)
		return false
	}
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
	if app.handlePlayHIDScancode(in.fd, name, nativeHIDUsage(code), value != 0, now) {
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

// nativeHIDUsage is the USB HID usage of a Linux key, or 0 for a key that
// fes.keyboard.hid sessions do not carry.
func nativeHIDUsage(code uint16) uint8 {
	usage, _ := hidkeys.FromLinuxKey(code)
	return usage
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

// promptFamily is the glyph set for a pad's family.
func promptFamily(family controller.Family) gfx.PromptFamily {
	switch family {
	case controller.FamilyXbox:
		return gfx.PromptXbox
	case controller.FamilyPlayStation:
		return gfx.PromptPlayStation
	case controller.FamilyNintendo:
		return gfx.PromptNintendo
	default:
		return gfx.PromptGeneric
	}
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
	for _, cmd := range lost.commands {
		app.Release(cmd)
	}
	ins.releaseLostPlayInput(app, lost)
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
	keyForward := forward && app.playHIDRouteReadyLocked()
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
		app.queuePlayHIDLocked(keyEvents)
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

// kitFrameTrace times each display-loop iteration into the app's frame
// trace and dumps the trace to /tmp on SIGUSR1.
type kitFrameTrace struct {
	app   *App
	trace *frameTrace
}

// startFrameTrace begins recording and returns a stop function. stop
// detaches the sink, drops the signal handler, and waits for an in-progress
// dump to finish. The caller must call it when the display loop returns,
// including when the loop ends without canceling ctx.
func startFrameTrace(ctx context.Context, app *App, dev gfx.Device, label string) (*kitFrameTrace, func()) {
	trace := newFrameTrace()
	app.mu.Lock()
	app.frameTrace = trace
	kitHost := app.kitHost
	app.mu.Unlock()
	processStart, _ := keyTraceProcessStart()
	if display, ok := dev.(*gfx.MenuDisplay); ok {
		display.SetTraceSink(trace.present)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	traceCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		defer signal.Stop(signals)
		requests := time.NewTicker(250 * time.Millisecond)
		defer requests.Stop()
		for {
			select {
			case <-traceCtx.Done():
				return
			case now := <-requests.C:
				if kitHost && os.Geteuid() == 0 {
					if consumeKeyTraceRequest(keyTraceArmRequest, os.Getpid(), processStart, 0, now) {
						trace.armKeyRouting(now)
						fmt.Fprintf(os.Stderr, "%s: key routing trace armed for one minute\n", label)
					}
					if consumeKeyTraceRequest(keyTraceDumpRequest, os.Getpid(), processStart, 0, now) {
						path, err := trace.dump(os.Getenv("FOGCAST_FRAME_TRACE_DIR"))
						if err != nil {
							fmt.Fprintf(os.Stderr, "%s: frame trace dump failed: %v\n", label, err)
						} else {
							fmt.Fprintf(os.Stderr, "%s: frame trace written to %s\n", label, path)
						}
					}
					app.handleDiagnosticSurroundRequest(os.Getpid(), processStart, 0, now, keyTraceSurroundHoldRequest, keyTraceSurroundReleaseRequest)
				}
			case <-signals:
				path, err := trace.dump(os.Getenv("FOGCAST_FRAME_TRACE_DIR"))
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s: frame trace dump failed: %v\n", label, err)
					continue
				}
				fmt.Fprintf(os.Stderr, "%s: frame trace written to %s\n", label, path)
			}
		}
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			<-stopped
			if display, ok := dev.(*gfx.MenuDisplay); ok {
				display.SetTraceSink(nil)
			}
			app.mu.Lock()
			app.diagnosticSurroundUntil = time.Time{}
			app.diagnosticSurroundSession = hostclient.SessionResult{}
			if app.frameTrace == trace {
				app.frameTrace = nil
			}
			app.mu.Unlock()
		})
	}
	return &kitFrameTrace{app: app, trace: trace}, stop
}

// tick records one rendered loop iteration. started is when this iteration
// began work, which can be later than the ticker's scheduled time. The
// other times bound the poll, Tick, Snapshot and render phases. snap is the
// frame that was drawn.
func (k *kitFrameTrace) tick(started, polled, ticked, snapped, rendered time.Time, seqBefore, seqAfter uint64, snap Snapshot) {
	if k == nil {
		return
	}
	async, update, draw, step := k.app.frameLuaTiming()
	r := frameTraceRecord{
		T: started.UnixMicro(), Kind: "tick",
		PollUS: polled.Sub(started).Microseconds(), TickUS: ticked.Sub(polled).Microseconds(),
		LuaAsyncUS: async.Microseconds(), LuaUpdateUS: update.Microseconds(), LuaDrawUS: draw.Microseconds(), LuaStepUS: step.Microseconds(),
		SnapshotUS: snapped.Sub(ticked).Microseconds(), RenderUS: rendered.Sub(snapped).Microseconds(),
		View: traceView(snap),
	}
	if seqAfter != seqBefore {
		r.Queued, r.Seq = true, seqAfter
	}
	k.trace.add(r)
}
