package gfx

import (
	"bytes"
	"context"
	"errors"
	"image"
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
	// unchanged marks a frame whose pixels the runtime already has. Only the
	// recovery present after a backoff sends one as a whole frame instead of
	// a probe, and it is the one present that must stop when the runtime has
	// given up recovering this display.
	unchanged bool
	binding   menuSessionBinding
	epoch     uint64
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
	// RetryFromError and RetryFromTiming retain the refused first attempt.
	// Err, Timing and Presented still describe the final worker outcome.
	RetryFromError  string
	RetryFromTiming menudisplay.Timing
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

// PresentedSince confirms a frame at or after a submission fence on the
// current binding. The caller establishes the fence only after rendering its
// desired state, so a newer frame may replace a dropped queued submission.
func (d *MenuDisplay) PresentedSince(seq uint64) bool {
	if d == nil || seq == 0 {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	status := d.lastStatus
	return !d.paused && !d.closed && d.hasPresented && d.presentedSeq >= seq && d.lastErr == nil && status.Available &&
		status.Generation == d.presentedGen && status.Underflows <= menudisplay.TransientUnderflowCap
}

// ForceNextFrame requests a fresh submission for a snapshot-bound fence.
func (d *MenuDisplay) ForceNextFrame() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.forceNext = true
	d.mu.Unlock()
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
	traceSink                   func(MenuPresentTrace)
	client                      MenuPresenter
	frames                      chan queuedMenuFrame
	ctx                         context.Context
	cancel                      context.CancelFunc
	done                        chan struct{}
	mu                          sync.Mutex
	closed                      bool
	paused                      bool
	keepReadyWhileLaunchPending bool
	flight                      chan struct{}
	pending                     int
	lastErr                     error
	lastStatus                  menudisplay.Status
	statusKnown                 bool
	generation                  uint64
	known                       bool
	changeDriven                bool
	forceNext                   bool
	nextSeq                     uint64
	submitted                   *submittedMenuFrame
	freeBuffers                 []*menuPixels
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
	// presentedDisplayed is the runtime's displayed sequence that the last
	// present returned, and presentedDisplayedKnown whether one has. A probe
	// that sees the runtime showing anything else is not looking at the
	// frame this client put up, however healthy the rest of the status is.
	presentedDisplayed      uint64
	presentedDisplayedKnown bool
	binding                 menuSessionBinding
	bindingEpoch            uint64
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

// MenuReadinessDiagnostic is a snapshot of the facts used by managed software
// readiness, plus the one display fact that stops this client presenting
// without stopping readiness. It is intended for startup diagnostics; it does
// not relax the readiness predicate.
type MenuReadinessDiagnostic struct {
	HasPresented bool
	Paused       bool
	Closed       bool
	StatusKnown  bool
	Available    bool
	Generation   uint64
	PresentedGen uint64
	Underflows   uint64
	LastErr      string
	// RecoveryAbandoned is the runtime's verdict that it has stopped
	// recovering this display. Nothing is presented to it while it holds, so
	// a diagnostic that showed only healthy facts would leave an operator
	// with no sign of why the screen is black and nothing is retrying.
	RecoveryAbandoned bool
	Ready             bool
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
	d.presentedDisplayed, d.presentedDisplayedKnown = 0, false
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
	probe, unchanged := false, false
	if d.changeDriven {
		now := d.clock()
		if !d.backoffUntil.IsZero() && now.Before(d.backoffUntil) {
			return
		}
		recover := !d.backoffUntil.IsZero()
		if recover {
			d.backoffUntil = time.Time{}
		}
		if previous := d.submitted; previous != nil && d.generation == previous.generation {
			if revisionKnown && previous.revisionKnown {
				unchanged = revision == previous.revision
			} else if isList || previous.isList {
				unchanged = isList && previous.isList && slices.Equal(list, previous.list)
			} else {
				unchanged = bytes.Equal(pix, previous.pixels)
			}
		}
		if d.forceNext {
			unchanged = false
			d.forceNext = false
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
	frame := queuedMenuFrame{buffer: buffer, list: list, isList: isList, generation: d.generation, known: d.known, seq: seq, probe: probe, unchanged: unchanged, binding: d.binding, epoch: d.bindingEpoch, queuedAt: time.Now(), copyUS: copyUS}
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
// runtime transaction to finish before releasing the client's grant mapping.
// A retained mapping can pin a kernel-owned allocation even after DMA drains.
func (d *MenuDisplay) Pause(ctx context.Context) error {
	return d.pause(ctx, false)
}

// PauseForLaunch holds the last acknowledged launcher readiness marker while
// the local agent decides whether it accepts the requested package. The
// caller must invalidate readiness after acceptance or Resume after rejection.
func (d *MenuDisplay) PauseForLaunch(ctx context.Context) error {
	return d.pause(ctx, true)
}

func (d *MenuDisplay) pause(ctx context.Context, keepReadyWhileLaunchPending bool) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	d.paused = true
	d.keepReadyWhileLaunchPending = keepReadyWhileLaunchPending
	if !keepReadyWhileLaunchPending {
		reportSoftwareReady(false)
	}
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
		return d.releasePausedWindow()
	}
	select {
	case <-flight:
		return d.releasePausedWindow()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *MenuDisplay) releasePausedWindow() error {
	release, ok := d.client.(interface{ ReleaseWindow() error })
	if !ok {
		return nil
	}
	// Keep Resume from starting a new frame between checking the paused state
	// and releasing its mapping. The worker clears flight before signalling.
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.paused || d.flight != nil || d.closed {
		return errors.New("menu display resumed or closed before pause completed")
	}
	return release.ReleaseWindow()
}

// InvalidateSoftwareReady clears idle-launcher readiness after an accepted
// launch or an uncertain launch result. A definite rejection should Resume.
func (d *MenuDisplay) InvalidateSoftwareReady() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.keepReadyWhileLaunchPending = false
	reportSoftwareReady(false)
	d.mu.Unlock()
}

// Resume allows fresh idle frames after the mutation has completed. The next
// Present is submitted even when its pixels match the pre-pause frame.
func (d *MenuDisplay) Resume() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.paused = false
	d.keepReadyWhileLaunchPending = false
	d.forgetSubmissionLocked()
	d.dropShortcutsLocked()
	d.clearPaceLocked()
	d.mu.Unlock()
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
