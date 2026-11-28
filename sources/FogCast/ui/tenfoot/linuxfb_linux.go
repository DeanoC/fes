//go:build linux

package tenfoot

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
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
func runDirectDisplay(ctx context.Context, opts Options, dev directDisplay, label string) (resultErr error) {
	opts = sizedOptions(opts, dev)
	app, err := configuredApp(opts)
	if err != nil {
		return err
	}
	if bt := startLinuxBluetooth(ctx, opts); bt != nil {
		if err := app.SetBluetooth(bt); err != nil {
			bt.Close()
			return err
		}
		defer bt.Close()
	}
	if display, ok := dev.(*gfx.MenuDisplay); ok {
		app.SetMenuDisplay(display)
		if !opts.Smoke {
			stopDisplayIPC, err := app.startKitDisplayIPC(ctx)
			if err != nil {
				return fmt.Errorf("launcher display control: %w", err)
			}
			defer stopDisplayIPC()
			err = app.reconcileDisplayStartupRetry(ctx, menudisplay.New(opts.MenuSocket))
			if err != nil {
				return fmt.Errorf("launcher display startup: %w", err)
			}
		}
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
	service, err := configureNativeKeyboardService(app, inputs, nativeKeyboardInputEnabled(opts))
	if err != nil {
		inputs.close()
		app.Stop()
		return err
	}
	inputs.seed(app)
	if opts.Smoke {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.SmokeTimeout)
		defer cancel()
	}
	app.Start(ctx)
	if opts.KitHost && !opts.Smoke {
		stopReload, err := startLibraryReloadControl(ctx, app, libraryReloadSocket)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tenfoot: library reload control unavailable: %v\n", err)
		} else {
			defer stopReload()
		}
	}
	if service != nil {
		service.start()
	}
	defer func() {
		// The display context is commonly already canceled here. A future
		// v2 worker must own a distinct lifetime that survives until Stop.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if service != nil {
			app.nativeKeyboardBeginShutdown()
		}
		var workerErr error
		stop := func() {
			if service != nil {
				workerCtx, workerCancel := context.WithTimeout(context.Background(), 2*time.Second)
				workerErr = service.stop(workerCtx)
				workerCancel()
			}
			app.Stop()
		}
		err := shutdownNativeInputSequence(shutdownCtx, inputs.localKeyboardGate != nil, inputs.retireNativeKeyboards, app.nativeKeyboardAwaitRetirement, stop, inputs.close)
		err = errors.Join(err, workerErr)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}()
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
	defer app.abortStartupFrame()
	startupDeadline := time.Now().Add(5 * time.Second)
	trace, stopTrace := startFrameTrace(ctx, app, dev, painter, label)
	defer stopTrace()
	for {
		select {
		case <-ctx.Done():
			if opts.Smoke {
				return fmt.Errorf("%s smoke: %w", label, ctx.Err())
			}
			return nil
		case scheduled := <-ticker.C:
			app.mu.Lock()
			ipcFailure := app.displayIPCFailure
			app.mu.Unlock()
			select {
			case err := <-ipcFailure:
				return fmt.Errorf("launcher display control stopped: %w", err)
			default:
			}
			// The ticker delivers its scheduled time, which is already in
			// the past when this iteration starts late. Trace from the
			// moment work begins; App timing still uses the schedule.
			started := time.Now()
			fenced, startupErr := app.checkStartupFrame(dev, time.Now().After(startupDeadline))
			if startupErr != nil {
				return startupErr
			}
			var quit bool
			var err error
			if !fenced {
				quit, err = poll(app, scheduled)
			}
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
				if display, ok := dev.(*gfx.MenuDisplay); ok && snap.displayFrameRequest != 0 {
					display.ForceNextFrame()
				}
				parked = presentFrame(painter, snap, textures, labels, parked)
				after := menuQueuedSeq(dev)
				if snap.displayFrameRequest != 0 && after > before {
					app.submittedDisplayFrame(snap.displayFrameRequest, after)
				}
				trace.tick(started, polled, ticked, snapped, time.Now(), before, after, snap)
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
func startFrameTrace(ctx context.Context, app *App, dev gfx.Device, painter *gfx.FrameCache, label string) (*kitFrameTrace, func()) {
	trace := newFrameTrace()
	app.mu.Lock()
	app.frameTrace = trace
	kitHost := app.kitHost
	app.mu.Unlock()
	painter.SetListCompareTraceSink(trace.listCompare)
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
			painter.SetListCompareTraceSink(nil)
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
		GridWidth: snap.Grid.Width, GridHeight: snap.Grid.Height, View: traceView(snap),
	}
	if seqAfter != seqBefore {
		r.Queued, r.Seq = true, seqAfter
	}
	k.trace.add(r)
}
