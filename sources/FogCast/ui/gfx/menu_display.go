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

type queuedMenuFrame struct {
	pixels     []byte
	generation uint64
	known      bool
	seq        uint64
	probe      bool
}

// submittedMenuFrame is the last frame queued for the runtime while
// change-driven presents are on. pixels is immutable and may alias the
// queued frame's buffer.
type submittedMenuFrame struct {
	pixels     []byte
	generation uint64
	seq        uint64
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
	lastErr      error
	generation   uint64
	known        bool
	changeDriven bool
	nextSeq      uint64
	submitted    *submittedMenuFrame
	now          func() time.Time
	probeQueued  bool
	lastProbe    time.Time
	backoff      time.Duration
	backoffUntil time.Time
	hasPresented bool
	presentedGen uint64
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

// SetChangeDriven opts into skipping Present when the pixels match the last
// queued frame, that frame has not since failed or been dropped, and the
// display's known generation is still the generation at submit. The default
// is off: every Present is submitted. fogcast-kit depends on that default.
//
// While it is on, an unchanged frame still queues a status probe once a
// second. A new menu generation, or a frame that was never presented, is
// submitted; the same generation presents nothing. Status errors, an
// unavailable menu, underflow rejection, and present failures back off from
// 250ms, doubling to 5s. Pause and Resume clear that backoff. Present calls
// do not reprogram the menu.
func (d *MenuDisplay) SetChangeDriven(on bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.changeDriven = on
	if !on {
		d.submitted = nil
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
func (d *MenuDisplay) Present() {
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
		unchanged := d.submitted != nil && d.generation == d.submitted.generation && bytes.Equal(pix, d.submitted.pixels)
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
	frame := queuedMenuFrame{pixels: append([]byte(nil), pix...), generation: d.generation, known: d.known, seq: seq, probe: probe}
	if d.changeDriven && !probe {
		d.submitted = &submittedMenuFrame{pixels: frame.pixels, generation: d.generation, seq: seq}
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
	return d.flight != nil || len(d.frames) > 0 || d.probeQueued
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
	d.submitted = nil
	d.clearPaceLocked()
	select {
	case <-d.frames:
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
	d.submitted = nil
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
			if d.paused {
				if frame.probe {
					d.probeQueued = false
				} else {
					d.forgetLocked(frame.seq)
				}
				d.mu.Unlock()
				continue
			}
			flight := make(chan struct{})
			d.flight = flight
			d.mu.Unlock()
			if frame.probe {
				d.runProbe(frame)
				d.mu.Lock()
				d.flight = nil
				close(flight)
				d.mu.Unlock()
				continue
			}
			status, err := d.client.Status(d.ctx)
			if err == nil {
				d.mu.Lock()
				d.generation = status.Generation
				d.known = true
				d.noteGenerationLocked(frame.seq, status.Generation)
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
	d.generation = status.Generation
	d.known = true
	d.mu.Unlock()
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
	d.armBackoffLocked(d.clock())
}

// forgetLocked drops the remembered frame when seq is still that frame.
// A newer Present replaces the record and must survive an older result.
// A probe reuses the seq it observed and must not call this on success.
func (d *MenuDisplay) forgetLocked(seq uint64) {
	if d.changeDriven && d.submitted != nil && d.submitted.seq == seq {
		d.submitted = nil
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
		return
	}
	if err != nil || !status.Available {
		d.armBackoffLocked(d.clock())
	}
}

func (d *MenuDisplay) armBackoffLocked(now time.Time) {
	if d.backoff <= 0 {
		d.backoff = menuBackoffInitial
	} else {
		d.backoff *= 2
	}
	if d.backoff > menuBackoffCap {
		d.backoff = menuBackoffCap
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
		default:
			d.probeQueued = false
		}
		return
	}
	select {
	case d.frames <- frame:
		return
	default:
	}
	select {
	case <-d.frames:
		d.probeQueued = false
	default:
	}
	select {
	case d.frames <- frame:
	case <-d.ctx.Done():
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
	d.Software.Close()
}

var _ Device = (*MenuDisplay)(nil)
