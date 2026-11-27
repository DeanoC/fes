package gfx

import (
	"errors"
	"image"
	"time"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

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
			var retryFromError string
			var retryFromTiming menudisplay.Timing
			var statusUS, presentUS int64
			presented, genChanged, cached, retried, blitted := false, false, false, false, false
			abandoned := false
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
				if err == nil && frame.unchanged && recoveryAbandoned(status) {
					// The recovery present after a backoff is the only
					// unchanged frame that gets this far, and the runtime has
					// deliberately stopped recovering this loss: sending it
					// spends from the present path the reactivation the idle
					// check withheld, or clears the menu package and drops
					// the kit to splash. The verdict is read from this
					// attempt's own status, not from an older one, because
					// the failure that armed the backoff is also what can
					// have hidden the verdict until now. An unchanged frame
					// never reaches here on a cached status: the cache is
					// dropped by the failure that arms a backoff.
					abandoned = true
					break
				}
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
					retryFromError, retryFromTiming = err.Error(), result.Timing
					result = menudisplay.Result{}
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
			if abandoned {
				// Nothing was sent and nothing failed, so the acknowledged
				// frame and the settled outcome both stand. The submission
				// stays too, which keeps the next Present an unchanged frame
				// that goes no further than a probe, and pacing comes from
				// the probe interval rather than a backoff that would only
				// count down to the same refusal.
				d.lastProbe = d.clock()
				d.dropShortcutsLocked()
				d.reportSoftwareReadyLocked(status)
			} else {
				d.lastErr = err
				d.noteSettledLocked(presented, err)
				if !presented {
					d.forgetLocked(frame.seq)
					d.dropShortcutsLocked()
				} else {
					d.rememberShownLocked(frame, status, result, cached, blitted)
				}
				if d.changeDriven {
					d.notePresentResultLocked(presented, genChanged, err, status, frame.seq, result.DisplayedSequence)
				}
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
					StatusSkipped: statusSkipped, DamageRects: damageRects, Retried: retried, Blitted: presented && blitted,
					RetryFromError: retryFromError, RetryFromTiming: retryFromTiming}
				if err != nil {
					trace.Err = err.Error()
				}
				sink(trace)
			}
		}
	}
}

// scanoutLostLocked reports whether the runtime's own live reads say this
// client's frame is no longer on the wire. An available status is not
// evidence of scanout: the runtime confirmed one present and, before the
// idle health check, never looked again. Status without live fields is an
// older runtime, where only the sequence is known.
func (d *MenuDisplay) scanoutLostLocked(status menudisplay.Status) bool {
	if live := status.Live; live != nil {
		if !live.Enabled || live.Faulted || live.Quiesced {
			return true
		}
		// Whether scanout is still showing the committed frame is the
		// runtime's verdict, not this client's subtraction. The sample's own
		// DisplayedSequence was read up to a probe interval ago and a present
		// has moved the committed frame since, so comparing it with what that
		// present returned straddles two instants: it reads as a loss after
		// an ordinary present, sends a complete frame to "recover", and that
		// present moves the sequence again. How often that repeats is timing
		// nothing here controls -- the sample is refreshed only by the check
		// that follows a status reply, paced to once a second -- so the cost
		// is one unnecessary 3.7 MB upload per UI change where the phases
		// miss, and one a second for as long as they line up. The verdict is
		// made inside the health check, where both numbers are read together,
		// and a present drops it rather than leaving a stale one to speak for
		// a frame it never saw.
		if live.ScanoutFrame != "" {
			return live.ScanoutFrame == menudisplay.ScanoutFrameMismatched
		}
		// A runtime that measures the display but publishes no verdict is
		// older than this comparison, and the sample is all it has to offer.
		return d.presentedDisplayedKnown && live.DisplayedSequence != d.presentedDisplayed
	}
	// An older runtime measures nothing, leaving only the cached sequence.
	return d.presentedDisplayedKnown && status.DisplayedSequence != d.presentedDisplayed
}

// recoveryAbandoned reports that the runtime has stopped trying to recover
// the loss its live sample describes. An older runtime sends no live reads
// at all, and a runtime that sends them without this flag is still willing
// to reactivate, so both read as false and behave exactly as before.
func recoveryAbandoned(status menudisplay.Status) bool {
	return status.Live != nil && status.Live.RecoveryAbandoned
}

func (d *MenuDisplay) runProbe(frame queuedMenuFrame) {
	status, err := d.client.Status(d.ctx)
	if err != nil {
		d.finishProbeLocked(frame, status, 0, err, false)
		return
	}
	d.mu.Lock()
	err = d.acceptStatusLocked(frame, status)
	if err == nil && recoveryAbandoned(status) {
		// Presenting is the only recovery a probe has, and the runtime has
		// deliberately stopped recovering this loss: a present here spends a
		// reactivation it withheld, or clears the menu package and drops the
		// kit to splash. So this probe ends with no frame, and it is also
		// where the recovery present after a backoff ends up, since an
		// unchanged frame only ever reaches the runtime as a probe. The
		// interval keeps running, which is how the launcher sees the loss
		// clear and resumes ordinary presenting. It does not touch the
		// acknowledged frame or the settled outcome: the frame that was
		// accepted stays accepted, and reportSoftwareReadyLocked keeps the
		// supervisor's marker where it was rather than restarting the kit's
		// software over a display nothing can fix.
		d.clearProbeQueuedLocked(frame.epoch)
		d.lastProbe = d.clock()
		d.reportSoftwareReadyLocked(status)
		d.mu.Unlock()
		return
	}
	if err == nil && status.Available && status.Underflows <= menudisplay.TransientUnderflowCap &&
		d.hasPresented && status.Generation == d.presentedGen && !d.scanoutLostLocked(status) {
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
		d.finishProbeLocked(frame, status, 0, err, false)
		return
	}
	if !status.Available || status.Underflows > menudisplay.TransientUnderflowCap {
		if status.Underflows > menudisplay.TransientUnderflowCap {
			err = errors.New("menu scanout underflow")
		}
		d.finishProbeLocked(frame, status, 0, err, false)
		return
	}
	// Either the frame changed or the live state says scanout is gone. Both
	// go up as a complete present, never as an acknowledgment.
	result, _, _, err := d.presentFrame(frame, status, false)
	d.finishProbeLocked(frame, status, result.DisplayedSequence, err, err == nil)
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

func (d *MenuDisplay) finishProbeLocked(frame queuedMenuFrame, status menudisplay.Status, displayed uint64, err error, presented bool) {
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
		d.presentedDisplayed, d.presentedDisplayedKnown = displayed, true
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
	d.lastStatus = status
	d.statusKnown = true
	if d.paused && d.keepReadyWhileLaunchPending {
		return
	}
	// A display the runtime has given up recovering is deliberately not
	// counted here, however black the screen is. This marker is what the kit
	// software supervisor reads: withdrawing it leaves the group unready,
	// which stops and restarts agent, runtime and launcher every few minutes
	// and reprograms the same dead display on every restart, and it blocks
	// the bundle activation that could ship a fix. That is the reprogram loop
	// the runtime's policy gives the loss up to avoid. Readiness follows the
	// acknowledged frame, as its own contract says; the runtime's kit log
	// names the loss, and ReadinessDiagnostic carries it for the launcher's.
	reportSoftwareReady(!d.paused && !d.closed && d.hasPresented && d.lastErr == nil && status.Available && status.Generation == d.presentedGen && status.Underflows <= menudisplay.TransientUnderflowCap)
}

// ReadinessDiagnostic returns the last runtime status and exact display facts
// that currently prevent managed software from reporting readiness.
func (d *MenuDisplay) ReadinessDiagnostic() MenuReadinessDiagnostic {
	if d == nil {
		return MenuReadinessDiagnostic{LastErr: "menu display is unavailable"}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	lastErr := ""
	if d.lastErr != nil {
		lastErr = d.lastErr.Error()
	}
	status := d.lastStatus
	return MenuReadinessDiagnostic{
		HasPresented: d.hasPresented, Paused: d.paused, Closed: d.closed,
		StatusKnown: d.statusKnown,
		Available:   status.Available, Generation: status.Generation,
		PresentedGen: d.presentedGen, Underflows: status.Underflows,
		LastErr:           lastErr,
		RecoveryAbandoned: recoveryAbandoned(status),
		Ready:             !d.paused && !d.closed && d.hasPresented && d.lastErr == nil && status.Available && status.Generation == d.presentedGen && status.Underflows <= menudisplay.TransientUnderflowCap,
	}
}

func (d *MenuDisplay) notePresentResultLocked(presented, genChanged bool, err error, status menudisplay.Status, seq, displayed uint64) {
	defer func() { d.reportSoftwareReadyLocked(status) }()
	if presented {
		d.hasPresented = true
		d.presentedGen = status.Generation
		d.presentedSeq = seq
		d.presentedDisplayed, d.presentedDisplayedKnown = displayed, true
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
