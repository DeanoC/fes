package gfx

import (
	"bytes"
	"context"
	"errors"
	"image"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

// MenuPresenter is the runtime menu client a MenuDisplay submits to.
// Production uses the menudisplay socket client. Tests pass a fake.
type MenuPresenter interface {
	Status(context.Context) (menudisplay.Status, error)
	Present(context.Context, uint64, []byte) (menudisplay.Result, error)
}

// MenuDamagePresenter is a MenuPresenter that can write only the rectangles
// that changed since the frame the runtime is showing.
type MenuDamagePresenter interface {
	PresentDamage(context.Context, uint64, []byte, menudisplay.Damage) (menudisplay.Result, error)
}

type menuPixels struct {
	pixels []byte
	refs   int // queue/worker and remembered submission; guarded by MenuDisplay.mu
}

type queuedMenuFrame struct {
	buffer *menuPixels
	pixels []byte
	// list is the frame as blitter commands when isList; pixels and buffer
	// are then nil.
	list       []menudisplay.BlitOp
	isList     bool
	generation uint64
	known      bool
	seq        uint64
	probe      bool
	binding    menuSessionBinding
	epoch      uint64
	// queuedAt and copyUS feed the frame trace.
	queuedAt time.Time
	copyUS   int64
}

// MenuPresentTrace is where one submitted menu frame's time went, for
// frame-time diagnostics. Durations are microseconds. Status probes of an
// unchanged frame are not traced.
type MenuPresentTrace struct {
	Seq       uint64
	Queued    time.Time
	Presented bool
	Err       string
	// FrameCopyUS is copying the rendered frame for the worker, on the
	// render thread. The rest happens on the worker.
	FrameCopyUS int64
	QueueWaitUS int64
	StatusUS    int64
	PresentUS   int64
	Timing      menudisplay.Timing
	// StatusSkipped means an attempt trusted cached status instead of
	// asking the runtime. A retried frame still counts that refused skip.
	StatusSkipped bool
	// DamageRects is how many rectangles a damaged present staged; zero
	// for a complete frame.
	DamageRects int
	// Retried means a shortcut was refused and the frame went again with a
	// fresh status and complete pixels.
	Retried bool
	// Blitted means the blitter drew the frame from a command list.
	Blitted bool
}

// QueuedSeq is the sequence of the last frame queued for presentation, so a
// caller can tell whether a render produced a new frame.
func (d *MenuDisplay) QueuedSeq() uint64 {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.nextSeq
}

// SetTraceSink receives one MenuPresentTrace per frame the worker handles.
// It is called on the worker goroutine and must not block. nil disables it.
func (d *MenuDisplay) SetTraceSink(sink func(MenuPresentTrace)) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.traceSink = sink
	d.mu.Unlock()
}

type menuSessionBinding struct {
	packageID      string
	coreGeneration uint64
}

// ErrSessionDisplayChanged means a queued frame no longer belongs to the
// runtime's display. Session presenters never follow a replacement machine.
var ErrSessionDisplayChanged = errors.New("session display changed before presentation")

// ErrMenuFrameNotPresented means the newest submission finished without a
// successful Present and the runtime did not report a more specific error.
// WaitIdle returns it so a launch handoff cannot treat that frame as shown.
var ErrMenuFrameNotPresented = errors.New("menu frame was not presented")

// submittedMenuFrame is the last frame queued for the runtime while
// change-driven presents are on. pixels is immutable and may alias the
// queued frame's buffer.
type submittedMenuFrame struct {
	buffer        *menuPixels
	revision      uint64
	revisionKnown bool
	pixels        []byte
	list          []menudisplay.BlitOp
	isList        bool
	generation    uint64
	seq           uint64
}

const (
	// Unchanged change-driven frames read menu status at this interval.
	// The check rides the caller's Present; there is no extra timer.
	menuProbeInterval = time.Second
	// Retries after the menu cannot be shown start here and double until
	// the cap. This only paces the client. It does not reprogram the menu.
	menuBackoffInitial = 250 * time.Millisecond
	menuBackoffCap     = 5 * time.Second
)

// MenuDisplay uses the same software painter as linuxfb and submits complete
// immutable frames to the local runtime. A one-slot queue keeps the most recent
// frame while a previous display acknowledgment is in flight.
//
// When the runtime's last grant for the idle menu included the menu blitter,
// a frame records blitter commands instead of drawing on the CPU, and the
// fabric draws it. Each frame picks its mode at its first call. A grant
// without the blitter, such as a session plane, gets CPU pixels: the worker
// rasterizes a list that reaches one. A frame that needs more than the
// blitter holds is rasterized and copied whole.
type MenuDisplay struct {
	*Software
	traceSink    func(MenuPresentTrace)
	client       MenuPresenter
	frames       chan queuedMenuFrame
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	mu           sync.Mutex
	closed       bool
	paused       bool
	flight       chan struct{}
	pending      int
	lastErr      error
	generation   uint64
	known        bool
	changeDriven bool
	nextSeq      uint64
	submitted    *submittedMenuFrame
	freeBuffers  []*menuPixels
	// drawing is the buffer the Software device draws into; previous is the
	// last frame taken from it, which Software may still copy back.
	drawing, previous *menuPixels
	now               func() time.Time
	probeQueued       bool
	probeEpoch        uint64
	lastProbe         time.Time
	backoff           time.Duration
	backoffUntil      time.Time
	hasPresented      bool
	presentedGen      uint64
	// presentedSeq is the submission sequence last shown on this binding.
	// Zero means none. A status probe may acknowledge only this sequence.
	presentedSeq uint64
	binding      menuSessionBinding
	bindingEpoch uint64
	// picture is the bound session's machine picture from its latest status.
	picture image.Rectangle
	// Change-driven worker shortcuts. statusCache is the runtime status as of
	// the last successful present, trusted until a probe interval after the
	// runtime was last asked. base is the frame the runtime then showed, the
	// reference for the next damaged present.
	statusCache      menudisplay.Status
	statusCacheOK    bool
	statusCacheAt    time.Time
	statusCacheEpoch uint64
	base             *menuPixels
	baseSeq          uint64
	baseGen          uint64
	baseEpoch        uint64
	// settled is the newest submission that finished on this binding.
	// WaitIdle fails when it was not presented. False until the first
	// completion, so an empty queue with no submission is idle. A status
	// probe records a presented outcome only for presentedSeq.
	settled          bool
	settledPresented bool
	settledErr       error
	// blitReady is whether the last idle-menu grant selected the blitter.
	blitReady bool
	// Render thread: the frame's mode, its commands, and the last finished
	// frame. cpuStale means the Software buffer lacks that frame because the
	// blitter drew it; needBase means the list frame has not yet decided
	// whether it starts from the frame before it.
	mode      frameMode
	ops       []menudisplay.BlitOp
	needBase  bool
	lastOps   []menudisplay.BlitOp
	lastList  bool
	cpuStale  bool
	blitCache blitCaches
	// scratch is the worker's CPU raster of a list frame.
	scratch []byte
}

func NewMenuDisplay(socketPath string) (*MenuDisplay, error) {
	return newMenuDisplayWithClient(menudisplay.New(socketPath))
}

// NewMenuDisplayWithPresenter builds a MenuDisplay around client.
// Tests use it to avoid a runtime socket.
func NewMenuDisplayWithPresenter(client MenuPresenter) (*MenuDisplay, error) {
	return newMenuDisplayWithClient(client)
}

func newMenuDisplayWithClient(client MenuPresenter) (*MenuDisplay, error) {
	if client == nil {
		return nil, errors.New("menu client unavailable")
	}
	sw, err := NewSoftware(menudisplay.Width, menudisplay.Height)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &MenuDisplay{Software: sw, client: client, frames: make(chan queuedMenuFrame, 1), ctx: ctx, cancel: cancel, done: make(chan struct{}),
		drawing: &menuPixels{pixels: sw.Framebuffer().Pix}, blitCache: newBlitCaches()}
	go d.run()
	return d, nil
}

func (d *MenuDisplay) BackendName() string { return BackendMenuDisplay }

// BindSession pins subsequent frames to the captured package and core
// generation. Call Pause and wait for it before opening or returning HDMI.
func (d *MenuDisplay) BindSession(packageID string, coreGeneration uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.setBindingLocked(menuSessionBinding{packageID, coreGeneration})
}

// ClearSessionBinding returns to idle-menu presentation. An idle presenter
// rejects a session display, including the interval before the first new frame.
func (d *MenuDisplay) ClearSessionBinding() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.setBindingLocked(menuSessionBinding{})
}

func (d *MenuDisplay) setBindingLocked(binding menuSessionBinding) {
	reportSoftwareReady(false)
	d.binding = binding
	d.bindingEpoch++
	d.picture = image.Rectangle{}
	d.blitReady = false
	d.known = false
	d.generation = 0
	d.hasPresented = false
	d.presentedSeq = 0
	d.lastErr = nil
	d.clearSettledLocked()
	d.forgetSubmissionLocked()
	d.dropShortcutsLocked()
	d.clearPaceLocked()
	select {
	case frame := <-d.frames:
		d.pending--
		d.releaseBufferLocked(frame.buffer)
	default:
	}
}

// SetChangeDriven opts into skipping Present when the pixels match the last
// queued frame, that frame has not since failed or been dropped, and the
// display's known generation is still the generation at submit. The default
// is off: every Present is submitted. Both kit launchers explicitly opt in.
//
// While it is on, an unchanged frame still queues a status probe once a
// second after a successful present. A new menu generation is submitted; the
// same generation presents nothing. The first frame is submitted because
// nothing has been queued yet, not because a probe saw that nothing had been
// presented. Status errors, scanout underflow, and present failures back off
// from 250ms, doubling to 5s. An unavailable menu uses that schedule but never
// waits longer than the one-second probe, so a game Stop redraws within about
// a second. A generation mismatch does not start a new wait. Pause and Resume
// clear the backoff. Present calls do not reprogram the menu.
func (d *MenuDisplay) SetChangeDriven(on bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.changeDriven = on
	if !on {
		d.forgetSubmissionLocked()
		d.dropShortcutsLocked()
		d.clearPaceLocked()
	}
	d.mu.Unlock()
}

func (d *MenuDisplay) Config() FBConfig {
	return FBConfig{Width: menudisplay.Width, Height: menudisplay.Height, Stride: menudisplay.Stride, BPP: 32}
}

// Present snapshots the software framebuffer and returns without waiting for
// the physical frame boundary. Superseded queued frames are dropped. With
// change-driven presents on, a byte-identical frame is not queued unless a
// status probe is due or a backoff wait has expired.
func (d *MenuDisplay) Present() { d.present(0, false) }

// PresentRevision presents a complete rendered frame identified by revision.
// The caller must change revision whenever the framebuffer changes, and must
// use one revision namespace for this device. Identical revisions avoid a
// full-frame comparison; retries and generation probes still run.
func (d *MenuDisplay) PresentRevision(revision uint64) { d.present(revision, true) }

func (d *MenuDisplay) present(revision uint64, revisionKnown bool) {
	if d == nil {
		return
	}
	list, isList := d.finishFrame()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.paused {
		return
	}
	var pix []byte
	if !isList {
		pix = d.Software.Framebuffer().Pix
	}
	probe := false
	if d.changeDriven {
		now := d.clock()
		if !d.backoffUntil.IsZero() && now.Before(d.backoffUntil) {
			return
		}
		recover := !d.backoffUntil.IsZero()
		if recover {
			d.backoffUntil = time.Time{}
		}
		unchanged := false
		if previous := d.submitted; previous != nil && d.generation == previous.generation {
			if revisionKnown && previous.revisionKnown {
				unchanged = revision == previous.revision
			} else if isList || previous.isList {
				unchanged = isList && previous.isList && slices.Equal(list, previous.list)
			} else {
				unchanged = bytes.Equal(pix, previous.pixels)
			}
		}
		if unchanged && !recover {
			if d.probeQueued || !d.probeDueLocked(now) {
				return
			}
			probe = true
			d.probeQueued = true
			d.probeEpoch = d.bindingEpoch
		}
	}
	var seq uint64
	if probe {
		if d.submitted != nil {
			seq = d.submitted.seq
		}
	} else if d.changeDriven {
		d.nextSeq++
		seq = d.nextSeq
	}
	var buffer *menuPixels
	var copyUS int64
	switch {
	case probe && d.submitted.isList:
		list, isList = d.submitted.list, true
	case probe:
		buffer = d.submitted.buffer
		if buffer == nil {
			buffer = d.copyFrameLocked(d.submitted.pixels)
		} else {
			buffer.refs++
		}
	case !isList:
		copyStart := time.Now()
		buffer = d.takeFrameLocked()
		copyUS = time.Since(copyStart).Microseconds()
	}
	frame := queuedMenuFrame{buffer: buffer, list: list, isList: isList, generation: d.generation, known: d.known, seq: seq, probe: probe, binding: d.binding, epoch: d.bindingEpoch, queuedAt: time.Now(), copyUS: copyUS}
	if buffer != nil {
		frame.pixels = buffer.pixels
	}
	if d.changeDriven && !probe {
		d.forgetSubmissionLocked()
		if buffer != nil {
			buffer.refs++
		}
		d.submitted = &submittedMenuFrame{pixels: frame.pixels, buffer: buffer, list: list, isList: isList, generation: d.generation, seq: seq, revision: revision, revisionKnown: revisionKnown}
	}
	d.sendFrameLocked(frame)
}

// FramePending reports whether a frame is queued or being submitted.
// Tests use it to tell a skipped Present from one still in flight.
func (d *MenuDisplay) FramePending() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.framePendingLocked()
}

func (d *MenuDisplay) framePendingLocked() bool {
	return d.pending > 0 || d.flight != nil || d.probeQueued
}

// WaitIdle waits until every submitted menu frame has finished its runtime
// transaction. The newest finished submission must have been presented.
// A failed Status or Present, or a frame that never reached the display, is
// returned so a launch handoff cannot pause and dispatch on it. Call before
// Pause when the final visible frame must survive. The caller retries or
// abandons; WaitIdle does not. Late adoption submits no frame and uses
// WaitPending so an older failure does not block that pause.
func (d *MenuDisplay) WaitIdle(ctx context.Context) error {
	return d.waitIdle(ctx, true)
}

// WaitPending waits until queued and in-flight menu work has finished.
// It does not report that work's outcome. Late adoption uses it when this
// handoff submitted no loading frame, so an older failure cannot block the
// pause while a core already owns HDMI.
func (d *MenuDisplay) WaitPending(ctx context.Context) error {
	return d.waitIdle(ctx, false)
}

func (d *MenuDisplay) waitIdle(ctx context.Context, acknowledge bool) error {
	if d == nil {
		return nil
	}
	t := time.NewTicker(time.Millisecond)
	defer t.Stop()
	for {
		d.mu.Lock()
		idle := !d.framePendingLocked()
		var err error
		if idle && acknowledge {
			err = idleHandoffError(d.settled, d.settledPresented, d.settledErr)
		}
		d.mu.Unlock()
		if idle {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.done:
			return errors.New("menu display closed")
		case <-t.C:
		}
	}
}

// idleHandoffError is the result of an idle WaitIdle. Nothing finished yet is
// success. A finished submission that was presented is success. Otherwise the
// runtime error is returned, or ErrMenuFrameNotPresented when there is none.
func idleHandoffError(settled, presented bool, err error) error {
	if !settled || (presented && err == nil) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrMenuFrameNotPresented
}

// Pause prevents new submissions, discards queued frames, forgets the last
// submitted frame, clears change-driven backoff, and waits for the current
// runtime transaction to finish before a game mutation begins.
func (d *MenuDisplay) Pause(ctx context.Context) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	d.paused = true
	reportSoftwareReady(false)
	d.forgetSubmissionLocked()
	d.dropShortcutsLocked()
	d.clearPaceLocked()
	select {
	case frame := <-d.frames:
		d.pending--
		d.releaseBufferLocked(frame.buffer)
	default:
	}
	flight := d.flight
	d.mu.Unlock()
	if flight == nil {
		return nil
	}
	select {
	case <-flight:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Resume allows fresh idle frames after the mutation has completed. The next
// Present is submitted even when its pixels match the pre-pause frame.
func (d *MenuDisplay) Resume() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.paused = false
	d.forgetSubmissionLocked()
	d.dropShortcutsLocked()
	d.clearPaceLocked()
	d.mu.Unlock()
}

func (d *MenuDisplay) run() {
	defer close(d.done)
	for {
		select {
		case <-d.ctx.Done():
			return
		case frame := <-d.frames:
			d.mu.Lock()
			if d.paused || frame.epoch != d.bindingEpoch {
				d.pending--
				if frame.probe {
					d.clearProbeQueuedLocked(frame.epoch)
				} else {
					d.forgetLocked(frame.seq)
				}
				d.releaseBufferLocked(frame.buffer)
				d.mu.Unlock()
				continue
			}
			flight := make(chan struct{})
			d.flight = flight
			d.mu.Unlock()
			if frame.probe {
				d.runProbe(frame)
				d.mu.Lock()
				d.releaseBufferLocked(frame.buffer)
				d.pending--
				d.flight = nil
				close(flight)
				d.mu.Unlock()
				continue
			}
			started := time.Now()
			var status menudisplay.Status
			var err error
			var result menudisplay.Result
			var statusUS, presentUS int64
			presented, genChanged, cached, retried, blitted := false, false, false, false, false
			statusSkipped := false
			damageRects := 0
			for attempt := 0; ; attempt++ {
				d.mu.Lock()
				var attemptCached bool
				status, attemptCached = d.cachedStatusLocked(frame)
				d.mu.Unlock()
				cached = attemptCached
				// A refused shortcut still skipped status on that attempt.
				statusSkipped = statusSkipped || cached
				err = nil
				if !cached {
					statusStart := time.Now()
					status, err = d.client.Status(d.ctx)
					statusUS += time.Since(statusStart).Microseconds()
				}
				if err == nil {
					d.mu.Lock()
					err = d.acceptStatusLocked(frame, status)
					d.mu.Unlock()
				}
				damaged := 0
				if err == nil && status.Available {
					if status.Underflows > menudisplay.TransientUnderflowCap {
						err = errors.New("menu scanout underflow")
					} else if frame.known && frame.generation != status.Generation {
						err = errors.New("menu generation changed before presentation")
						genChanged = true
					} else {
						presentStart := time.Now()
						result, damaged, blitted, err = d.presentFrame(frame, status, true)
						presentUS += time.Since(presentStart).Microseconds()
						presented = err == nil
					}
				}
				if err != nil && attempt == 0 && (cached || damaged > 0) && d.ctx.Err() == nil {
					// A shortcut's assumption no longer holds, for example a
					// new generation or another frame on screen. Ask the
					// runtime and send the complete frame once.
					d.mu.Lock()
					d.dropShortcutsLocked()
					d.mu.Unlock()
					retried = true
					continue
				}
				if presented {
					damageRects = damaged
				}
				break
			}
			d.mu.Lock()
			if frame.epoch != d.bindingEpoch {
				// BindSession reset presentation state while this runtime call
				// was in flight. Its result belongs to the old binding; only
				// release its resources and flight marker.
				d.releaseBufferLocked(frame.buffer)
				d.pending--
				d.flight = nil
				close(flight)
				d.mu.Unlock()
				continue
			}
			d.lastErr = err
			d.noteSettledLocked(presented, err)
			if !presented {
				d.forgetLocked(frame.seq)
				d.dropShortcutsLocked()
			} else {
				d.rememberShownLocked(frame, status, result, cached, blitted)
			}
			if d.changeDriven {
				d.notePresentResultLocked(presented, genChanged, err, status, frame.seq)
			}
			d.releaseBufferLocked(frame.buffer)
			d.pending--
			d.flight = nil
			close(flight)
			sink := d.traceSink
			d.mu.Unlock()
			if sink != nil {
				trace := MenuPresentTrace{Seq: frame.seq, Queued: frame.queuedAt, Presented: presented,
					FrameCopyUS: frame.copyUS, QueueWaitUS: started.Sub(frame.queuedAt).Microseconds(),
					StatusUS: statusUS, PresentUS: presentUS, Timing: result.Timing,
					StatusSkipped: statusSkipped, DamageRects: damageRects, Retried: retried, Blitted: presented && blitted}
				if err != nil {
					trace.Err = err.Error()
				}
				sink(trace)
			}
		}
	}
}

func (d *MenuDisplay) runProbe(frame queuedMenuFrame) {
	status, err := d.client.Status(d.ctx)
	if err != nil {
		d.finishProbeLocked(frame, status, err, false)
		return
	}
	d.mu.Lock()
	err = d.acceptStatusLocked(frame, status)
	if err == nil && status.Available && status.Underflows <= menudisplay.TransientUnderflowCap &&
		d.hasPresented && status.Generation == d.presentedGen {
		// Validate the binding and clear recovered pacing under one lock: an
		// old probe must not clear an error after a new binding is installed.
		// A healthy status at this generation only acknowledges the handoff
		// when this probe's submission is the sequence that was presented.
		// An older frame left on screen must not turn a newer failed frame
		// into a successful launch.
		d.clearProbeQueuedLocked(frame.epoch)
		d.lastProbe = d.clock()
		d.lastErr = nil
		if frame.seq != 0 && frame.seq == d.presentedSeq {
			d.noteSettledLocked(true, nil)
		}
		d.resetBackoffLocked()
		d.reportSoftwareReadyLocked(status)
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	if err != nil {
		d.finishProbeLocked(frame, status, err, false)
		return
	}
	if !status.Available || status.Underflows > menudisplay.TransientUnderflowCap {
		if status.Underflows > menudisplay.TransientUnderflowCap {
			err = errors.New("menu scanout underflow")
		}
		d.finishProbeLocked(frame, status, err, false)
		return
	}
	_, _, _, err = d.presentFrame(frame, status, false)
	d.finishProbeLocked(frame, status, err, err == nil)
}

func (d *MenuDisplay) acceptStatusLocked(frame queuedMenuFrame, status menudisplay.Status) error {
	if frame.epoch != d.bindingEpoch {
		return ErrSessionDisplayChanged
	}
	if frame.binding.packageID != "" {
		if !status.Available || !status.Session || status.PackageID != frame.binding.packageID || status.CoreGeneration != frame.binding.coreGeneration ||
			(d.known && status.Generation != d.generation) {
			return ErrSessionDisplayChanged
		}
		d.picture = sessionPicture(status)
	} else if status.Session {
		return ErrSessionDisplayChanged
	}
	d.generation = status.Generation
	d.known = true
	d.noteGenerationLocked(frame.seq, status.Generation)
	return nil
}

// SessionPicture is where the bound session's shell places the machine
// picture in the plane, from the runtime's latest status. It is empty before
// the first status of a binding and for an idle presenter.
func (d *MenuDisplay) SessionPicture() image.Rectangle {
	if d == nil {
		return image.Rectangle{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.picture
}

func sessionPicture(status menudisplay.Status) image.Rectangle {
	if len(status.Picture) != 4 || status.Picture[2] <= 0 || status.Picture[3] <= 0 {
		return image.Rectangle{}
	}
	p := status.Picture
	picture := image.Rect(p[0], p[1], p[0]+p[2], p[1]+p[3])
	if !picture.In(image.Rect(0, 0, menudisplay.Width, menudisplay.Height)) {
		return image.Rectangle{}
	}
	return picture
}

func (d *MenuDisplay) finishProbeLocked(frame queuedMenuFrame, status menudisplay.Status, err error, presented bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if frame.epoch != d.bindingEpoch {
		d.clearProbeQueuedLocked(frame.epoch)
		return
	}
	defer func() { d.reportSoftwareReadyLocked(status) }()
	d.clearProbeQueuedLocked(frame.epoch)
	// A probe present changes what the runtime shows, and a failure leaves it
	// unknown; either way the next frame asks for status and goes whole.
	d.dropShortcutsLocked()
	if err == nil {
		d.generation = status.Generation
		d.known = true
	}
	d.lastErr = err
	d.noteSettledLocked(presented, err)
	if presented {
		d.hasPresented = true
		d.presentedGen = status.Generation
		d.presentedSeq = frame.seq
		d.lastProbe = d.clock()
		d.resetBackoffLocked()
		d.noteGenerationLocked(frame.seq, status.Generation)
		return
	}
	d.armBackoffLocked(d.clock(), menuWaitCap(err, status))
}

func (d *MenuDisplay) noteSettledLocked(presented bool, err error) {
	d.settled = true
	d.settledPresented = presented
	d.settledErr = err
}

func (d *MenuDisplay) clearSettledLocked() {
	d.settled = false
	d.settledPresented = false
	d.settledErr = nil
}

// forgetLocked drops the remembered frame when seq is still that frame.
// A newer Present replaces the record and must survive an older result.
// A probe reuses the seq it observed and must not call this on success.
func (d *MenuDisplay) forgetLocked(seq uint64) {
	if d.changeDriven && d.submitted != nil && d.submitted.seq == seq {
		d.forgetSubmissionLocked()
	}
}

func (d *MenuDisplay) noteGenerationLocked(seq, generation uint64) {
	if d.changeDriven && d.submitted != nil && d.submitted.seq == seq {
		d.submitted.generation = generation
	}
}

// Readiness describes the current acknowledged idle display, not a historical
// successful frame. Call only after validating the response's binding epoch.
func (d *MenuDisplay) reportSoftwareReadyLocked(status menudisplay.Status) {
	reportSoftwareReady(!d.paused && !d.closed && d.hasPresented && d.lastErr == nil && status.Available && status.Generation == d.presentedGen && status.Underflows <= menudisplay.TransientUnderflowCap)
}

func (d *MenuDisplay) notePresentResultLocked(presented, genChanged bool, err error, status menudisplay.Status, seq uint64) {
	defer func() { d.reportSoftwareReadyLocked(status) }()
	if presented {
		d.hasPresented = true
		d.presentedGen = status.Generation
		d.presentedSeq = seq
		d.lastProbe = d.clock()
		d.resetBackoffLocked()
		return
	}
	if genChanged {
		// The next Present submits at the new generation. Do not arm a wait.
		return
	}
	if err != nil || !status.Available {
		d.armBackoffLocked(d.clock(), menuWaitCap(err, status))
	}
}

// menuWaitCap is the longest pause for this failure. An unavailable menu is
// the generation-wait while a game owns the display: Stop publishes a new
// generation, and the redraw has to land within about one probe interval.
// Socket loss, underflow, and present failures keep the longer cap.
func menuWaitCap(err error, status menudisplay.Status) time.Duration {
	if err == nil && !status.Available {
		return menuProbeInterval
	}
	return menuBackoffCap
}

func (d *MenuDisplay) armBackoffLocked(now time.Time, cap time.Duration) {
	if d.backoff <= 0 {
		d.backoff = menuBackoffInitial
	} else {
		d.backoff *= 2
	}
	if d.backoff > cap {
		d.backoff = cap
	}
	d.backoffUntil = now.Add(d.backoff)
}

func (d *MenuDisplay) resetBackoffLocked() {
	d.backoff = 0
	d.backoffUntil = time.Time{}
}

func (d *MenuDisplay) clearPaceLocked() {
	d.resetBackoffLocked()
	d.probeQueued = false
}

func (d *MenuDisplay) clearProbeQueuedLocked(epoch uint64) {
	if d.probeQueued && d.probeEpoch == epoch {
		d.probeQueued = false
	}
}

// probeDueLocked is true one probe interval after the last successful present.
// lastProbe stays zero until that present, which also sets hasPresented, so a
// zero clock is not due. Production submits the first frame because nothing
// has been queued yet; Present does not consult this function in that case.
// runProbe can still submit when hasPresented is false, but only a caller
// that plants lastProbe reaches that branch.
func (d *MenuDisplay) probeDueLocked(now time.Time) bool {
	if d.lastProbe.IsZero() {
		return false
	}
	return !now.Before(d.lastProbe.Add(menuProbeInterval))
}

func (d *MenuDisplay) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

func (d *MenuDisplay) sendFrameLocked(frame queuedMenuFrame) {
	if frame.probe {
		select {
		case d.frames <- frame:
			d.pending++
		default:
			d.clearProbeQueuedLocked(frame.epoch)
			d.releaseBufferLocked(frame.buffer)
		}
		return
	}
	select {
	case d.frames <- frame:
		d.pending++
		return
	default:
	}
	select {
	case dropped := <-d.frames:
		d.pending--
		d.releaseBufferLocked(dropped.buffer)
		if dropped.probe {
			d.clearProbeQueuedLocked(dropped.epoch)
		}
	default:
	}
	select {
	case d.frames <- frame:
		d.pending++
	case <-d.ctx.Done():
		d.releaseBufferLocked(frame.buffer)
	}
}

func (d *MenuDisplay) LastError() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastErr
}

func (d *MenuDisplay) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	reportSoftwareReady(false)
	d.cancel()
	d.mu.Unlock()
	<-d.done
	d.mu.Lock()
	d.forgetSubmissionLocked()
	d.dropShortcutsLocked()
	select {
	case frame := <-d.frames:
		d.pending--
		d.releaseBufferLocked(frame.buffer)
	default:
	}
	d.releaseBufferLocked(d.previous)
	d.previous = nil
	d.freeBuffers = nil
	d.mu.Unlock()
	if closer, ok := d.client.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	d.Software.Close()
}

var _ Device = (*MenuDisplay)(nil)

// Buffers return to the bounded pool only after both the worker/queue and
// remembered submission release them. A probe shares immutable submitted bytes.
func (d *MenuDisplay) copyFrameLocked(pix []byte) *menuPixels {
	buffer := d.freeBufferLocked()
	copy(buffer.pixels, pix)
	buffer.refs = 1
	return buffer
}

func (d *MenuDisplay) freeBufferLocked() *menuPixels {
	if n := len(d.freeBuffers); n > 0 {
		buffer := d.freeBuffers[n-1]
		d.freeBuffers = d.freeBuffers[:n-1]
		return buffer
	}
	return &menuPixels{pixels: make([]byte, len(d.drawing.pixels))}
}

// takeFrameLocked hands the finished frame on without copying it: Software
// draws the next frame into a free buffer. The frame holds one reference for
// the caller and one while Software may still copy it back for a frame that
// does not start with Clear; the swap after that releases it.
func (d *MenuDisplay) takeFrameLocked() *menuPixels {
	next := d.freeBufferLocked()
	done := d.drawing
	d.Software.swapFramebuffer(next.pixels)
	d.releaseBufferLocked(d.previous)
	d.drawing, d.previous = next, done
	done.refs = 2
	return done
}

func (d *MenuDisplay) releaseBufferLocked(buffer *menuPixels) {
	if buffer == nil {
		return
	}
	buffer.refs--
	if buffer.refs == 0 && len(d.freeBuffers) < 3 {
		d.freeBuffers = append(d.freeBuffers, buffer)
	}
}

// cachedStatusLocked returns the status remembered from the last successful
// present while change-driven presents are on, the binding is unchanged and
// the runtime was asked within the probe interval. Only a client that
// presents damage relies on it; another asks the runtime per frame.
func (d *MenuDisplay) cachedStatusLocked(frame queuedMenuFrame) (menudisplay.Status, bool) {
	if _, ok := d.client.(MenuDamagePresenter); !ok {
		return menudisplay.Status{}, false
	}
	if !d.changeDriven || !d.statusCacheOK || d.statusCacheEpoch != frame.epoch || d.paused ||
		!d.clock().Before(d.statusCacheAt.Add(menuProbeInterval)) {
		return menudisplay.Status{}, false
	}
	return d.statusCache, true
}

// rememberShownLocked records a successful present as the next frame's
// status and damage base. A cached status keeps the time the runtime was
// last asked, so status is still read at least once per probe interval.
func (d *MenuDisplay) rememberShownLocked(frame queuedMenuFrame, status menudisplay.Status, result menudisplay.Result, cached, blitted bool) {
	if !d.changeDriven {
		return
	}
	if !cached {
		d.statusCacheAt = d.clock()
	}
	status.DisplayedSequence, status.Underflows = result.DisplayedSequence, result.Underflows
	d.statusCache, d.statusCacheOK, d.statusCacheEpoch = status, true, frame.epoch
	d.releaseBufferLocked(d.base)
	d.base = nil
	if blitted || frame.buffer == nil {
		// CPU damage cannot build on a frame the blitter drew.
		return
	}
	frame.buffer.refs++
	d.base, d.baseSeq, d.baseGen, d.baseEpoch = frame.buffer, result.DisplayedSequence, status.Generation, frame.epoch
}

// presentFrame shows frame at status's generation. On a blitter grant every
// frame is a list: a CPU frame becomes one whole copy, because the grant
// refuses CPU commits after its first list. A list too large for the
// blitter is rasterized and copied whole. A grant without the blitter takes
// pixels, rasterizing a list; damaged presents may write only what changed.
// It reports the damage rectangles sent and whether the blitter drew.
func (d *MenuDisplay) presentFrame(frame queuedMenuFrame, status menudisplay.Status, damaged bool) (menudisplay.Result, int, bool, error) {
	if blitter, ok := d.client.(MenuBlitPresenter); ok {
		grant, err := blitter.Grant(d.ctx, status.Generation)
		if err != nil {
			return menudisplay.Result{}, 0, false, err
		}
		if grant.Blitter && grant.BlitterMinor < menudisplay.MinBlitterMinor {
			d.noteGrant(frame, menudisplay.Grant{})
			return menudisplay.Result{}, 0, false, errors.New("menu blitter grant is below the minimum minor")
		}
		d.noteGrant(frame, grant)
		if grant.Blitter {
			ops := frame.list
			if !frame.isList {
				ops = copyFrameOps(frame.pixels)
			}
			result, err := blitter.PresentBlit(d.ctx, status.Generation, menudisplay.BlitFrame{Ops: ops})
			if errors.Is(err, menudisplay.ErrBlitTooLarge) && frame.isList {
				result, err = blitter.PresentBlit(d.ctx, status.Generation, menudisplay.BlitFrame{Ops: copyFrameOps(d.rasterScratch(frame.list))})
			}
			return result, 0, true, err
		}
	}
	if frame.isList {
		result, err := d.client.Present(d.ctx, status.Generation, d.rasterScratch(frame.list))
		return result, 0, false, err
	}
	if damaged {
		if damage := d.damageFor(frame, status); damage != nil {
			result, err := d.client.(MenuDamagePresenter).PresentDamage(d.ctx, status.Generation, frame.pixels, *damage)
			return result, len(damage.Rects), false, err
		}
	}
	result, err := d.client.Present(d.ctx, status.Generation, frame.pixels)
	return result, 0, false, err
}

// noteGrant records whether the idle menu's grant has the blitter, which
// picks the mode of the next frame drawn.
func (d *MenuDisplay) noteGrant(frame queuedMenuFrame, grant menudisplay.Grant) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if frame.epoch == d.bindingEpoch && frame.binding.packageID == "" {
		d.blitReady = grant.Blitter
	}
}

// rasterScratch draws a list frame into the worker's frame buffer.
func (d *MenuDisplay) rasterScratch(ops []menudisplay.BlitOp) []byte {
	if d.scratch == nil {
		d.scratch = make([]byte, menudisplay.FrameBytes)
	}
	rasterOps(d.scratch, menudisplay.Stride, ops)
	return d.scratch
}

func (d *MenuDisplay) dropShortcutsLocked() {
	d.statusCacheOK = false
	d.releaseBufferLocked(d.base)
	d.base = nil
}

// damageFor returns the rectangles where frame differs from the frame the
// runtime is showing for this display, or nil when the frame must go whole.
func (d *MenuDisplay) damageFor(frame queuedMenuFrame, status menudisplay.Status) *menudisplay.Damage {
	if _, ok := d.client.(MenuDamagePresenter); !ok {
		return nil
	}
	d.mu.Lock()
	base := d.base
	if !d.changeDriven || base == nil || d.baseEpoch != frame.epoch || d.baseGen != status.Generation ||
		d.baseSeq == 0 || d.baseSeq > math.MaxUint32 || d.baseSeq != status.DisplayedSequence {
		d.mu.Unlock()
		return nil
	}
	seq := uint32(d.baseSeq)
	base.refs++
	d.mu.Unlock()
	rects := menudisplay.Diff(base.pixels, frame.pixels)
	d.mu.Lock()
	d.releaseBufferLocked(base)
	d.mu.Unlock()
	if len(rects) == 0 {
		// The runtime still shows a new frame; stage the least it accepts.
		rects = []menudisplay.Rect{{X: 0, Y: 0, W: 8, H: 1}}
	}
	return &menudisplay.Damage{BaseSequence: seq, Rects: rects}
}

func (d *MenuDisplay) forgetSubmissionLocked() {
	if d.submitted != nil {
		d.releaseBufferLocked(d.submitted.buffer)
		d.submitted = nil
	}
}
