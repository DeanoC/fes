// SPDX-License-Identifier: GPL-3.0-or-later
package scanout

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

var (
	ErrReaderArgument = errors.New("window reader: invalid argument")
	ErrReaderIdentity = errors.New("window reader: identity, generation or geometry differs")
	ErrReaderProtocol = errors.New("window reader: aborted, faulted or inconsistent result")
	ErrReaderData     = errors.New("window reader: framebuffer CRC differs")
	ErrReaderRetired  = errors.New("window reader: retired; platform containment required")
	ErrReaderBusy     = errors.New("window reader: operation already in progress")
	ErrReaderLimit    = errors.New("window reader: time, poll or cookie limit")
)

const (
	WindowControlABI   uint32 = 0x10203042
	windowCommand      uint32 = 0x80
	windowExpectLo     uint32 = 0x84
	windowCookie       uint32 = 0x8c
	windowStatus       uint32 = 0x90
	windowResultLo     uint32 = 0x94
	windowResultCookie uint32 = 0x9c
	windowRequested    uint32 = 0xa0
	windowDelivered    uint32 = 0xa4
	windowCRC          uint32 = 0xa8
	windowBase         uint32 = 0xb0
	windowReady        uint32 = 1
	windowBusy         uint32 = 2
	windowDone         uint32 = 4
	windowAborted      uint32 = 8
	windowFault        uint32 = 16
	windowDisabled     uint32 = 32
)

// WindowGeometry is a compiled bus window, not a CPU mapping or memory grant.
type WindowGeometry struct{ Base, Bytes, RowBytes, Stride, Rows uint32 }

// ReaderLimits bound each operation. IO and Yield must themselves return within
// platform bounds; cancellation cannot interrupt a nonreturning platform hook.
type ReaderLimits struct {
	Timeout  time.Duration
	MaxPolls uint32
	Now      func() time.Time
	Yield    func()
}

// FrameRead observes delivery into the FPGA CRC sink. It grants no drain,
// buffer reuse, vblank, HDMI or runtime admission authority.
type FrameRead struct {
	Generation                                    uint64
	Cookie, RequestedBytes, DeliveredBytes, CRC32 uint32
}

// ReaderDrain identifies a completed stop of this reader generation. It covers
// the local endpoint only; the platform still owns other masters, bridge closure
// and the pinned allocation. It supplies no allocation release or HDMI receipt.
type ReaderDrain struct{ Generation, Cookie uint64 }

// ReaderFailure preserves only observations already made by a failed operation.
// Sample is a diagnostic snapshot, never a successful FrameRead or a drain
// receipt. Inspecting or formatting the error performs no clock or device I/O.
type ReaderFailure struct {
	Cause                            error
	Operation, Stage, Limit          string
	Polls, Reads, Writes, SamplePoll uint32
	StartAttempted, SnapshotComplete bool
	Flags                            uint32
	Sample                           FrameRead
	Elapsed                          time.Duration
}

func (f *ReaderFailure) Error() string { return f.Cause.Error() }
func (f *ReaderFailure) Unwrap() error { return f.Cause }

// WindowReader has one exclusive owner for an already enabled reader generation.
// The platform supplies ordered, full-width, synchronous IO protected by its
// bridge/ownership checks. It keeps the entire memory window pinned on every
// outcome and performs the separate endpoint/all-master stop and drain.
type WindowReader struct {
	io         embeddedcontrol.IO
	want       embeddedcontrol.Identity
	generation uint64
	frameBytes uint32
	limits     ReaderLimits
	active     atomic.Bool
	retired    bool
	nextCookie uint32
}

type readerOperation struct {
	r           *WindowReader
	ctx         context.Context
	start, last time.Time
	failure     ReaderFailure
}

type operationIO struct{ operation *readerOperation }

func (p operationIO) Read32(offset uint32) (uint32, error) { return p.operation.read(offset) }
func (p operationIO) Write32(offset, value uint32) error   { return p.operation.write(offset, value) }

// Drain sends DISABLE once and waits for the exact generation/cookie receipt.
// A completed stop retires this instance; a platform that retains exclusive
// ownership may enable a newer generation after its protected CPU publication.
// Failure retires access without claiming that pending traffic has stopped.
func (r *WindowReader) Drain(ctx context.Context, cookie uint64) (result ReaderDrain, err error) {
	if r == nil || cookie == 0 {
		return result, ErrReaderArgument
	}
	o, err := r.begin(ctx, "drain")
	if err != nil {
		return result, err
	}
	defer func() {
		r.retired = true
		o.finish(err)
		if err != nil {
			result = ReaderDrain{}
			err = o.failed(err)
		}
	}()
	o.failure.Stage = "owner"
	if err = o.owner(); err != nil {
		return result, err
	}
	for _, entry := range []struct{ offset, value uint32 }{
		{g.FesEmbeddedRegRequestGenerationLo, uint32(r.generation)},
		{g.FesEmbeddedRegRequestGenerationHi, uint32(r.generation >> 32)},
		{g.FesEmbeddedRegRequestCookieLo, uint32(cookie)},
		{g.FesEmbeddedRegRequestCookieHi, uint32(cookie >> 32)},
		{g.FesEmbeddedRegCommand, g.FesEmbeddedCommandDisable},
	} {
		o.failure.Stage = "prepare"
		if entry.offset == g.FesEmbeddedRegCommand {
			o.failure.Stage = "disable"
		}
		if err = o.write(entry.offset, entry.value); err != nil {
			return result, err
		}
	}
	control := embeddedcontrol.Control{IO: operationIO{o}}
	for poll := uint32(0); poll < r.limits.MaxPolls; poll++ {
		o.failure.Polls = poll + 1
		o.failure.Stage = "poll-drain"
		var sample embeddedcontrol.Snapshot
		sample, err = control.Snapshot()
		if err == nil {
			if sample.Identity != r.want || sample.Generation != r.generation {
				return result, ErrReaderIdentity
			}
			if sample.Status == g.FesEmbeddedStatusDrained && sample.Pending == 0 &&
				sample.AckGeneration == r.generation && sample.Cookie == cookie {
				return ReaderDrain{r.generation, cookie}, nil
			}
			if sample.Status != g.FesEmbeddedStatusDraining || sample.AckGeneration != 0 || sample.Cookie != 0 {
				return result, ErrReaderProtocol
			}
		} else if !errors.Is(err, embeddedcontrol.ErrBusy) {
			return result, err
		}
		r.limits.Yield()
		if err = o.check(); err != nil {
			return result, err
		}
	}
	o.failure.Limit = "polls"
	return result, ErrReaderLimit
}

func (r *WindowReader) begin(ctx context.Context, operation string) (*readerOperation, error) {
	if ctx == nil {
		return nil, ErrReaderArgument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !r.active.CompareAndSwap(false, true) {
		return nil, ErrReaderBusy
	}
	if r.retired {
		r.active.Store(false)
		return nil, ErrReaderRetired
	}
	now := r.limits.Now()
	return &readerOperation{r: r, ctx: ctx, start: now, last: now, failure: ReaderFailure{Operation: operation, Limit: "none"}}, nil
}

func (o *readerOperation) failed(cause error) error {
	f := o.failure
	f.Cause = cause
	return &f
}
func (o *readerOperation) finish(err error) {
	if err != nil {
		o.r.retired = true
	}
	o.r.active.Store(false)
}
func (o *readerOperation) check() error {
	if err := o.ctx.Err(); err != nil {
		return err
	}
	now := o.r.limits.Now()
	o.failure.Elapsed = now.Sub(o.start)
	if now.Before(o.last) {
		o.failure.Limit = "clock"
		return ErrReaderLimit
	}
	if o.failure.Elapsed >= o.r.limits.Timeout {
		o.failure.Limit = "timeout"
		return ErrReaderLimit
	}
	o.last = now
	return nil
}
func (o *readerOperation) read(offset uint32) (uint32, error) {
	if err := o.check(); err != nil {
		return 0, err
	}
	o.failure.Reads++
	v, err := o.r.io.Read32(offset)
	if err != nil {
		return 0, err
	}
	return v, o.check()
}
func (o *readerOperation) write(offset, value uint32) error {
	if err := o.check(); err != nil {
		return err
	}
	o.failure.Writes++
	if err := o.r.io.Write32(offset, value); err != nil {
		return err
	}
	return o.check()
}
func (o *readerOperation) read64(offset uint32) (uint64, error) {
	lo, err := o.read(offset)
	if err != nil {
		return 0, err
	}
	hi, err := o.read(offset + 4)
	return uint64(hi)<<32 | uint64(lo), err
}
func (o *readerOperation) owner() error {
	checks := []struct{ offset, value uint32 }{
		{g.FesEmbeddedRegMagic, g.FesEmbeddedMagic}, {g.FesEmbeddedRegVersion, g.FesEmbeddedVersion},
		{g.FesEmbeddedRegAbi, o.r.want.ABI},
	}
	for i, v := range o.r.want.Build {
		checks = append(checks, struct{ offset, value uint32 }{g.FesEmbeddedRegBuild0 + uint32(i)*4, v})
	}
	for _, check := range checks {
		v, err := o.read(check.offset)
		if err != nil {
			return err
		}
		if v != check.value {
			return ErrReaderIdentity
		}
	}
	// Pending changes normally during a frame. Do not use a whole-endpoint
	// double snapshot that mistakes progressing memory traffic for lost ownership.
	for i := 0; i < 2; i++ {
		generation, err := o.read64(g.FesEmbeddedRegGenerationLo)
		if err != nil {
			return err
		}
		if generation != o.r.generation {
			return ErrReaderIdentity
		}
		status, err := o.read(g.FesEmbeddedRegStatus)
		if err != nil {
			return err
		}
		if status != g.FesEmbeddedStatusEnabled {
			return ErrReaderProtocol
		}
	}
	return nil
}
func (o *readerOperation) snapshot() (FrameRead, uint32, error) {
	flags, err := o.read(windowStatus)
	if err != nil {
		return FrameRead{}, 0, err
	}
	var s FrameRead
	if s.Generation, err = o.read64(windowResultLo); err != nil {
		return s, flags, err
	}
	for _, entry := range []struct {
		offset uint32
		value  *uint32
	}{
		{windowResultCookie, &s.Cookie}, {windowRequested, &s.RequestedBytes},
		{windowDelivered, &s.DeliveredBytes}, {windowCRC, &s.CRC32},
	} {
		if *entry.value, err = o.read(entry.offset); err != nil {
			return s, flags, err
		}
	}
	o.failure.SnapshotComplete = true
	o.failure.SamplePoll = o.failure.Polls
	o.failure.Sample = s
	o.failure.Flags = flags
	if flags & ^uint32(63) != 0 {
		return s, flags, ErrReaderProtocol
	}
	return s, flags, nil
}

// NewWindowReader checks the explicit diagnostic personality and compiled
// geometry after platform admission. It never enables an endpoint or bridge.
func NewWindowReader(ctx context.Context, registers embeddedcontrol.IO, want embeddedcontrol.Identity,
	generation uint64, geometry WindowGeometry, limits ReaderLimits) (_ *WindowReader, err error) {
	span := uint64(geometry.Stride)*(uint64(geometry.Rows)-1) + uint64(geometry.RowBytes)
	if registers == nil || want.ABI != WindowControlABI || want.Build == [4]uint32{} || generation == 0 ||
		geometry.Base&15 != 0 || geometry.Bytes == 0 || geometry.Bytes > 16<<20 || geometry.Bytes&15 != 0 ||
		geometry.RowBytes < 16 || geometry.RowBytes > 16384 || geometry.RowBytes&15 != 0 ||
		geometry.Stride < geometry.RowBytes || geometry.Stride > 16384 || geometry.Stride&15 != 0 ||
		geometry.Rows == 0 || geometry.Rows > 2160 || span > uint64(geometry.Bytes) ||
		uint64(geometry.Base)+uint64(geometry.Bytes) > 1<<32 || limits.Timeout <= 0 || limits.Timeout > time.Second ||
		limits.MaxPolls == 0 || limits.MaxPolls > 64 || limits.Now == nil || limits.Yield == nil {
		return nil, ErrReaderArgument
	}
	r := &WindowReader{io: registers, want: want, generation: generation, frameBytes: geometry.RowBytes * geometry.Rows, limits: limits, nextCookie: 1}
	o, err := r.begin(ctx, "open")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = o.failed(err)
		}
		o.finish(err)
	}()
	o.failure.Stage = "owner"
	if err = o.owner(); err != nil {
		return nil, err
	}
	values := []uint32{geometry.Base, geometry.Bytes, geometry.RowBytes, geometry.Stride, geometry.Rows, 0x31435746, 1}
	o.failure.Stage = "geometry"
	for i, want := range values {
		var got uint32
		got, err = o.read(windowBase + uint32(i)*4)
		if err != nil {
			return nil, err
		}
		if got != want {
			return nil, ErrReaderIdentity
		}
	}
	var s FrameRead
	var flags uint32
	o.failure.Stage = "snapshot"
	if s, flags, err = o.snapshot(); err != nil {
		return nil, err
	}
	if flags&windowReady == 0 || flags&(windowBusy|windowFault|windowDisabled) != 0 || s.Generation >= generation && s.Cookie != 0 {
		return nil, ErrReaderProtocol
	}
	o.failure.Stage = "verify"
	if err = o.owner(); err != nil {
		return nil, err
	}
	return r, nil
}

// ReadFrame issues START once and waits for its exact generation/cookie result
// and expected IEEE CRC32. The caller keeps its published CPU bytes unchanged
// for this entire operation; this driver cannot lock another memory writer.
// Any I/O, ownership, protocol, cancellation or deadline uncertainty retires this
// instance. Neither an uncertain command nor a failed frame is replayed.
func (r *WindowReader) ReadFrame(ctx context.Context, expectedCRC uint32) (result FrameRead, err error) {
	if r == nil {
		return result, ErrReaderArgument
	}
	o, err := r.begin(ctx, "frame")
	if err != nil {
		return result, err
	}
	defer func() {
		o.finish(err)
		if err != nil {
			result = FrameRead{}
			err = o.failed(err)
		}
	}()
	o.failure.Stage = "cookie"
	if r.nextCookie == 0 {
		o.failure.Limit = "cookie"
		return result, ErrReaderLimit
	}
	cookie := r.nextCookie
	r.nextCookie++
	o.failure.Stage = "owner"
	if err = o.owner(); err != nil {
		return result, err
	}
	for _, entry := range []struct{ offset, value uint32 }{
		{windowExpectLo, uint32(r.generation)}, {windowExpectLo + 4, uint32(r.generation >> 32)},
		{windowCookie, cookie}, {windowCommand, 1},
	} {
		o.failure.Stage = "prepare"
		if entry.offset == windowCommand {
			o.failure.Stage = "start"
			o.failure.StartAttempted = true
		}
		if err = o.write(entry.offset, entry.value); err != nil {
			return result, err
		}
	}
	for poll := uint32(0); poll < r.limits.MaxPolls; poll++ {
		o.failure.Polls = poll + 1
		o.failure.Stage = "poll-owner"
		if err = o.owner(); err != nil {
			return result, err
		}
		var flags uint32
		o.failure.Stage = "poll-snapshot"
		if result, flags, err = o.snapshot(); err != nil {
			return FrameRead{}, err
		}
		if result.Generation != r.generation || result.Cookie != cookie || flags&(windowAborted|windowFault|windowDisabled) != 0 ||
			result.RequestedBytes > r.frameBytes || result.DeliveredBytes > result.RequestedBytes ||
			(result.RequestedBytes|result.DeliveredBytes)&15 != 0 || flags&(windowReady|windowBusy) == windowReady|windowBusy {
			return FrameRead{}, ErrReaderProtocol
		}
		if flags&windowDone != 0 {
			if flags&windowBusy != 0 || result.RequestedBytes != r.frameBytes || result.DeliveredBytes != r.frameBytes {
				return FrameRead{}, ErrReaderProtocol
			}
			if result.CRC32 != expectedCRC {
				return FrameRead{}, ErrReaderData
			}
			o.failure.Stage = "complete-owner"
			if err = o.owner(); err != nil {
				return FrameRead{}, err
			}
			return result, nil
		}
		if flags&windowBusy == 0 {
			return FrameRead{}, ErrReaderProtocol
		}
		o.failure.Stage = "yield"
		r.limits.Yield()
		if err = o.check(); err != nil {
			return FrameRead{}, err
		}
	}
	o.failure.Stage = "poll-limit"
	o.failure.Limit = "polls"
	return FrameRead{}, ErrReaderLimit
}
