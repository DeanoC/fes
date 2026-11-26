package gfx

import (
	"errors"
	"math"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

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
