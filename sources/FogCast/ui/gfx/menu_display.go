package gfx

import (
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
}

// MenuDisplay uses the same software painter as linuxfb and submits complete
// immutable frames to the local runtime. A one-slot queue keeps the most recent
// frame while a previous display acknowledgment is in flight.
type MenuDisplay struct {
	*Software
	client     menuFrameClient
	frames     chan queuedMenuFrame
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	mu         sync.Mutex
	closed     bool
	paused     bool
	flight     chan struct{}
	lastErr    error
	generation uint64
	known      bool
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

func (d *MenuDisplay) BackendName() string { return "menu-display" }
func (d *MenuDisplay) Config() FBConfig {
	return FBConfig{Width: menudisplay.Width, Height: menudisplay.Height, Stride: menudisplay.Stride, BPP: 32}
}

// Present snapshots the software framebuffer and returns without waiting for
// the physical frame boundary. Superseded queued frames are dropped.
func (d *MenuDisplay) Present() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	if d.paused {
		d.mu.Unlock()
		return
	}
	frame := queuedMenuFrame{pixels: append([]byte(nil), d.Software.Framebuffer().Pix...), generation: d.generation, known: d.known}
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

// Pause prevents new submissions, discards queued frames and waits for the
// current runtime transaction to finish before a game mutation begins.
func (d *MenuDisplay) Pause(ctx context.Context) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	d.paused = true
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

// Resume allows fresh idle frames after the mutation has completed.
func (d *MenuDisplay) Resume() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.paused = false
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
				d.mu.Unlock()
			}
			if err == nil && status.Available {
				if status.Underflows != 0 {
					err = errors.New("menu scanout underflow")
				} else if frame.known && frame.generation != status.Generation {
					err = errors.New("menu generation changed before presentation")
				} else {
					_, err = d.client.Present(d.ctx, status.Generation, frame.pixels)
				}
			}
			d.mu.Lock()
			d.lastErr = err
			d.flight = nil
			close(flight)
			d.mu.Unlock()
		}
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
