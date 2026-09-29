package gfx

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/DeanoC/FogCast/ui/menudisplay"
)

type menuFrameClient interface {
	Status(context.Context) (menudisplay.Status, error)
	Present(context.Context, uint64, []byte) (menudisplay.Result, error)
}

type queuedMenuFrame struct {
	pixels     []byte
	generation uint64
	known      bool
	seq        uint64
}

// submittedMenuFrame is the last frame queued for the runtime while
// change-driven presents are on. pixels is immutable and may alias the
// queued frame's buffer.
type submittedMenuFrame struct {
	pixels     []byte
	generation uint64
	seq        uint64
}

// MenuDisplay uses the same software painter as linuxfb and submits complete
// immutable frames to the local runtime. A one-slot queue keeps the most recent
// frame while a previous display acknowledgment is in flight.
type MenuDisplay struct {
	*Software
	client       menuFrameClient
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
}

func NewMenuDisplay(socketPath string) (*MenuDisplay, error) {
	return newMenuDisplayWithClient(menudisplay.New(socketPath))
}

func newMenuDisplayWithClient(client menuFrameClient) (*MenuDisplay, error) {
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
// An unchanged frame is not resubmitted by itself after a new runtime
// generation. This does not poll status. The UI has to draw a different
// frame; redraw on a new generation is follow-on.
func (d *MenuDisplay) SetChangeDriven(on bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.changeDriven = on
	if !on {
		d.submitted = nil
	}
	d.mu.Unlock()
}
func (d *MenuDisplay) Config() FBConfig {
	return FBConfig{Width: menudisplay.Width, Height: menudisplay.Height, Stride: menudisplay.Stride, BPP: 32}
}

// Present snapshots the software framebuffer and returns without waiting for
// the physical frame boundary. Superseded queued frames are dropped. With
// change-driven presents on, a byte-identical frame is not queued.
func (d *MenuDisplay) Present() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.closed || d.paused {
		d.mu.Unlock()
		return
	}
	pix := d.Software.Framebuffer().Pix
	if d.changeDriven && d.submitted != nil && d.generation == d.submitted.generation && bytes.Equal(pix, d.submitted.pixels) {
		d.mu.Unlock()
		return
	}
	var seq uint64
	if d.changeDriven {
		d.nextSeq++
		seq = d.nextSeq
	}
	frame := queuedMenuFrame{pixels: append([]byte(nil), pix...), generation: d.generation, known: d.known, seq: seq}
	if d.changeDriven {
		d.submitted = &submittedMenuFrame{pixels: frame.pixels, generation: d.generation, seq: seq}
	}
	defer d.mu.Unlock()
	select {
	case d.frames <- frame:
		return
	default:
	}
	select {
	case <-d.frames:
	default:
	}
	select {
	case d.frames <- frame:
	case <-d.ctx.Done():
	}
}

// Pause prevents new submissions, discards queued frames, forgets the last
// submitted frame, and waits for the current runtime transaction to finish
// before a game mutation begins.
func (d *MenuDisplay) Pause(ctx context.Context) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	d.paused = true
	d.submitted = nil
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
				d.forgetLocked(frame.seq)
				d.mu.Unlock()
				continue
			}
			flight := make(chan struct{})
			d.flight = flight
			d.mu.Unlock()
			status, err := d.client.Status(d.ctx)
			if err == nil {
				d.mu.Lock()
				d.generation = status.Generation
				d.known = true
				d.noteGenerationLocked(frame.seq, status.Generation)
				d.mu.Unlock()
			}
			presented := false
			if err == nil && status.Available {
				if status.Underflows > menudisplay.TransientUnderflowCap {
					err = errors.New("menu scanout underflow")
				} else if frame.known && frame.generation != status.Generation {
					err = errors.New("menu generation changed before presentation")
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
			d.flight = nil
			close(flight)
			d.mu.Unlock()
		}
	}
}

// forgetLocked drops the remembered frame when seq is still that frame.
// A newer Present replaces the record and must survive an older result.
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
