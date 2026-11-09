// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gnet "github.com/usbarmory/go-net"
)

type controlStack struct{ gnet.Stack }

func (*controlStack) WriteOutboundPacket(b []byte) (int, error) { clear(b[:14]); return 14, nil }

type controlDevice struct {
	rx, tx  atomic.Int64
	entered chan struct{}
	release chan struct{}
	blockTX bool
}

func (d *controlDevice) Receive([]byte) (int, error) {
	d.rx.Add(1)
	if d.entered != nil && !d.blockTX {
		close(d.entered)
		<-d.release
	}
	return 0, nil
}
func (d *controlDevice) Transmit([]byte) error {
	d.tx.Add(1)
	if d.entered != nil && d.blockTX {
		close(d.entered)
		<-d.release
	}
	return nil
}
func controlLink(d *controlDevice) *Link {
	return &Link{stack: &controlStack{}, device: d, state: &linkState{}}
}
func waitControl(t *testing.T, ready func() bool) {
	t.Helper()
	end := time.Now().Add(time.Second)
	for !ready() {
		if time.Now().After(end) {
			t.Fatal("control did not reach boundary")
		}
		time.Sleep(time.Millisecond)
	}
}
func controlResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("control did not finish")
		return nil
	}
}

func TestControllerWaitsForRXAndTXBeforePlatformDrain(t *testing.T) {
	for _, tx := range []bool{false, true} {
		t.Run(map[bool]string{false: "RX", true: "TX"}[tx], func(t *testing.T) {
			d := &controlDevice{entered: make(chan struct{}), release: make(chan struct{}), blockTX: tx}
			l := controlLink(d)
			var stops, starts atomic.Int64
			c, err := NewController(l, DMAControl{Stop: func(ctx context.Context) error {
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
					t.Error("unbounded DMA control")
				}
				stops.Add(1)
				return nil
			}, Start: func(context.Context) error { starts.Add(1); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			ioDone := make(chan error, 1)
			if tx {
				go func() { l.transmit(nil); ioDone <- nil }()
			} else {
				go func() {
					if !l.enter() {
						ioDone <- ErrPaused
						return
					}
					_, err := d.Receive(nil)
					l.state.io.RUnlock()
					ioDone <- err
				}()
			}
			<-d.entered
			paused := make(chan error, 1)
			go func() { paused <- c.PauseNetwork(context.Background()) }()
			waitControl(t, l.state.paused.Load)
			if stops.Load() != 0 || !errors.Is(l.Available(), ErrPaused) {
				t.Fatal("drain ran before software IO stopped")
			}
			l.transmit(nil)
			if l.enter() {
				l.state.io.RUnlock()
				t.Fatal("RX entered closed gate")
			}
			if !errors.Is(c.ResumeNetwork(context.Background()), ErrBusy) {
				t.Fatal("concurrent control admitted")
			}
			if !errors.Is(c.Claim(), ErrBusy) {
				t.Fatal("ownership transferred during drain")
			}
			close(d.release)
			if err = controlResult(t, ioDone); err != nil {
				t.Fatal(err)
			}
			if err = controlResult(t, paused); err != nil {
				t.Fatal(err)
			}
			if err = c.PauseNetwork(context.Background()); err != nil || stops.Load() != 1 {
				t.Fatal("already-closed pause repeated hardware", err)
			}
			if err = c.ResumeNetwork(context.Background()); err != nil || starts.Load() != 1 || l.Available() != nil {
				t.Fatal("resume", err)
			}
			if !errors.Is(c.ResumeNetwork(context.Background()), ErrControl) || starts.Load() != 1 {
				t.Fatal("unpaired restart")
			}
		})
	}
}

func TestControllerFailuresRemainGatedAndCannotReplay(t *testing.T) {
	for _, phase := range []string{"stop", "start", "cancel-stop", "cancel-start", "link"} {
		t.Run(phase, func(t *testing.T) {
			d := &controlDevice{}
			l := controlLink(d)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lost := errors.New("platform completion unknown")
			stops, starts := 0, 0
			c, err := NewController(l, DMAControl{Stop: func(context.Context) error {
				stops++
				if phase == "stop" {
					return lost
				}
				if phase == "cancel-stop" {
					cancel()
				}
				return nil
			}, Start: func(context.Context) error {
				starts++
				if phase == "start" {
					return lost
				}
				if phase == "cancel-start" {
					cancel()
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			err = c.PauseNetwork(ctx)
			if phase == "link" {
				l.fail(lost)
			}
			if err == nil {
				err = c.ResumeNetwork(ctx)
			}
			if err == nil || l.Err() == nil || !l.state.paused.Load() {
				t.Fatal("uncertain control reopened traffic", err)
			}
			previousStops, previousStarts := stops, starts
			if c.PauseNetwork(context.Background()) == nil || c.ResumeNetwork(context.Background()) == nil {
				t.Fatal("failed control retried")
			}
			l.transmit(nil)
			if stops != previousStops || starts != previousStarts || d.tx.Load() != 0 || l.enter() {
				t.Fatal("retired IO/control repeated")
			}
			if _, err = NewController(l, DMAControl{Stop: func(context.Context) error { return nil }, Start: func(context.Context) error { return nil }}); err == nil {
				t.Fatal("failed claim replaced")
			}
		})
	}
}

func TestControllerCancellationWhileIOPendingRetiresWithoutHardwareCall(t *testing.T) {
	l := controlLink(&controlDevice{})
	stops := 0
	c, err := NewController(l, DMAControl{Stop: func(context.Context) error { stops++; return nil }, Start: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	l.state.io.RLock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.PauseNetwork(ctx) }()
	waitControl(t, l.state.paused.Load)
	cancel()
	err = controlResult(t, done)
	l.state.io.RUnlock()
	if !errors.Is(err, context.Canceled) || stops != 0 || l.Err() == nil || !l.state.paused.Load() {
		t.Fatal("uncertain pending IO escaped", err)
	}
}

func TestControllerRequiresCompleteExclusiveSupplier(t *testing.T) {
	l := controlLink(&controlDevice{})
	valid := DMAControl{Stop: func(context.Context) error { return nil }, Start: func(context.Context) error { return nil }}
	for _, dma := range []DMAControl{{}, {Stop: valid.Stop}, {Start: valid.Start}} {
		if _, err := NewController(l, dma); err == nil || l.state.claimed.Load() {
			t.Fatal("incomplete supplier claimed link")
		}
	}
	c, err := NewController(l, valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewController(l, valid); err == nil {
		t.Fatal("duplicate supplier")
	}
	copyLink := *l
	if _, err = NewController(&copyLink, valid); err == nil {
		t.Fatal("copied link replaced exclusive supplier")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(c.PauseNetwork(ctx), context.Canceled) || l.state.paused.Load() || l.Err() != nil {
		t.Fatal("pre-cancellation changed traffic")
	}
	if c.PauseNetwork(nil) == nil || c.ResumeNetwork(nil) == nil {
		t.Fatal("nil context")
	}
	copyControl := *c
	if err = copyControl.PauseNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = c.ResumeNetwork(context.Background()); err != nil {
		t.Fatal("copied control lost shared state", err)
	}
	if err = copyControl.ResumeNetwork(context.Background()); !errors.Is(err, ErrControl) {
		t.Fatal("copied handle replayed restart", err)
	}
	if (&Controller{}).PauseNetwork(context.Background()) == nil || (&Controller{}).ResumeNetwork(context.Background()) == nil {
		t.Fatal("fabricated control accepted")
	}
}
