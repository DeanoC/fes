package gfx

import (
	"bytes"
	"context"
	"errors"
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

type menuPixels struct {
	pixels []byte
	refs   int // queue/worker and remembered submission; guarded by MenuDisplay.mu
}

type queuedMenuFrame struct {
	buffer     *menuPixels
	pixels     []byte
	generation uint64
	known      bool
	seq        uint64
	probe      bool
	binding    menuSessionBinding
	epoch      uint64
}

type menuSessionBinding struct {
	packageID      string
	coreGeneration uint64
}

// ErrSessionDisplayChanged means a queued frame no longer belongs to the
// runtime's display. Session presenters never follow a replacement machine.
var ErrSessionDisplayChanged = errors.New("session display changed before presentation")

// submittedMenuFrame is the last frame queued for the runtime while
// change-driven presents are on. pixels is immutable and may alias the
// queued frame's buffer.
type submittedMenuFrame struct {
	buffer        *menuPixels
	revision      uint64
	revisionKnown bool
	pixels        []byte
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
type MenuDisplay struct {
	*Software
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
	now          func() time.Time
	probeQueued  bool
	lastProbe    time.Time
	backoff      time.Duration
	backoffUntil time.Time
	hasPresented bool
	presentedGen uint64
	binding      menuSessionBinding
	bindingEpoch uint64
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
	d := &MenuDisplay{Software: sw, client: client, frames: make(chan queuedMenuFrame, 1), ctx: ctx, cancel: cancel, done: make(chan struct{})}
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
	d.binding = binding
	d.bindingEpoch++
	d.known = false
	d.generation = 0
	d.hasPresented = false
	d.lastErr = nil
	d.forgetSubmissionLocked()
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
// is off: every Present is submitted. fogcast-kit depends on that default.
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
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.paused {
		return
	}
	pix := d.Software.Framebuffer().Pix
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
	if probe {
		buffer = d.submitted.buffer
		if buffer == nil {
			buffer = d.copyFrameLocked(d.submitted.pixels)
		} else {
			buffer.refs++
		}
	} else {
		buffer = d.copyFrameLocked(pix)
	}
	frame := queuedMenuFrame{pixels: buffer.pixels, buffer: buffer, generation: d.generation, known: d.known, seq: seq, probe: probe, binding: d.binding, epoch: d.bindingEpoch}
	if d.changeDriven && !probe {
		d.forgetSubmissionLocked()
		buffer.refs++
		d.submitted = &submittedMenuFrame{pixels: frame.pixels, buffer: buffer, generation: d.generation, seq: seq, revision: revision, revisionKnown: revisionKnown}
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
	return d.pending > 0 || d.flight != nil || d.probeQueued
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
	d.forgetSubmissionLocked()
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
					d.probeQueued = false
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
			status, err := d.client.Status(d.ctx)
			if err == nil {
				d.mu.Lock()
				err = d.acceptStatusLocked(frame, status)
				d.mu.Unlock()
			}
			presented := false
			genChanged := false
			if err == nil && status.Available {
				if status.Underflows > menudisplay.TransientUnderflowCap {
					err = errors.New("menu scanout underflow")
				} else if frame.known && frame.generation != status.Generation {
					err = errors.New("menu generation changed before presentation")
					genChanged = true
				} else {
					_, err = d.client.Present(d.ctx, status.Generation, frame.pixels)
					presented = err == nil
				}
			}
			d.mu.Lock()
			d.lastErr = err
			if !presented {
				d.forgetLocked(frame.seq)
			}
			if d.changeDriven {
				d.notePresentResultLocked(presented, genChanged, err, status)
			}
			d.releaseBufferLocked(frame.buffer)
			d.pending--
			d.flight = nil
			close(flight)
			d.mu.Unlock()
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
	d.mu.Lock()
	need := !d.hasPresented || status.Generation != d.presentedGen
	d.mu.Unlock()
	if !need {
		d.mu.Lock()
		d.probeQueued = false
		d.lastProbe = d.clock()
		d.mu.Unlock()
		return
	}
	_, err = d.client.Present(d.ctx, status.Generation, frame.pixels)
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
	} else if status.Session {
		return ErrSessionDisplayChanged
	}
	d.generation = status.Generation
	d.known = true
	d.noteGenerationLocked(frame.seq, status.Generation)
	return nil
}

func (d *MenuDisplay) finishProbeLocked(frame queuedMenuFrame, status menudisplay.Status, err error, presented bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.probeQueued = false
	if err == nil {
		d.generation = status.Generation
		d.known = true
	}
	d.lastErr = err
	if presented {
		d.hasPresented = true
		d.presentedGen = status.Generation
		d.lastProbe = d.clock()
		d.resetBackoffLocked()
		d.noteGenerationLocked(frame.seq, status.Generation)
		return
	}
	d.armBackoffLocked(d.clock(), menuWaitCap(err, status))
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

func (d *MenuDisplay) notePresentResultLocked(presented, genChanged bool, err error, status menudisplay.Status) {
	if presented {
		d.hasPresented = true
		d.presentedGen = status.Generation
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
			d.probeQueued = false
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
			d.probeQueued = false
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
	d.cancel()
	d.mu.Unlock()
	<-d.done
	d.mu.Lock()
	d.forgetSubmissionLocked()
	select {
	case frame := <-d.frames:
		d.pending--
		d.releaseBufferLocked(frame.buffer)
	default:
	}
	d.freeBuffers = nil
	d.mu.Unlock()
	d.Software.Close()
}

var _ Device = (*MenuDisplay)(nil)

// Buffers return to the bounded pool only after both the worker/queue and
// remembered submission release them. A probe shares immutable submitted bytes.
func (d *MenuDisplay) copyFrameLocked(pix []byte) *menuPixels {
	var buffer *menuPixels
	if n := len(d.freeBuffers); n > 0 {
		buffer = d.freeBuffers[n-1]
		d.freeBuffers = d.freeBuffers[:n-1]
	} else {
		buffer = &menuPixels{pixels: make([]byte, len(pix))}
	}
	copy(buffer.pixels, pix)
	buffer.refs = 1
	return buffer
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

func (d *MenuDisplay) forgetSubmissionLocked() {
	if d.submitted != nil {
		d.releaseBufferLocked(d.submitted.buffer)
		d.submitted = nil
	}
}
