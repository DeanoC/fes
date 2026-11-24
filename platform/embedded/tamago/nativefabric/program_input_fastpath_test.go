// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
)

type fastPathKeyboardSource struct {
	mu      sync.Mutex
	ready   chan struct{}
	reports [][8]byte
	polls   int
}

func (s *fastPathKeyboardSource) push(r [8]byte) {
	s.mu.Lock()
	s.reports = append(s.reports, r)
	s.mu.Unlock()
	select {
	case s.ready <- struct{}{}:
	default:
	}
}

func (s *fastPathKeyboardSource) PollKeyboard() ([]byte, error) {
	var buf [8]byte
	n, err := s.PollKeyboardInto(buf[:])
	if err != nil || n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

func (s *fastPathKeyboardSource) PollKeyboardInto(dst []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	if len(s.reports) == 0 {
		return 0, nil
	}
	r := s.reports[0]
	s.reports = s.reports[1:]
	return copy(dst, r[:]), nil
}

func (s *fastPathKeyboardSource) Ready() <-chan struct{} { return s.ready }

type countingPlatform struct {
	mu        sync.Mutex
	nextCalls int
	uartReady chan struct{}
}

func (c *countingPlatform) Next(context.Context) (tenfoot.PlatformInput, error) {
	c.mu.Lock()
	c.nextCalls++
	c.mu.Unlock()
	return tenfoot.PlatformInput{At: time.Now()}, nil
}
func (c *countingPlatform) Present(context.Context, tenfoot.Snapshot) error { return nil }
func (c *countingPlatform) Ready() <-chan struct{}                          { return c.uartReady }

func TestGameplayFastPathBypassesTenfootUntilReturnChord(t *testing.T) {
	f, b, c := backendFixture(t)
	ctx := context.Background()
	src := &fastPathKeyboardSource{ready: make(chan struct{}, 1)}
	base := &countingPlatform{uartReady: make(chan struct{}, 1)}
	p, err := NewProgramInputPlatform(base, b, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Launch(ctx, "native-0"); err != nil {
		t.Fatal(err)
	}

	// Drive neutral arming + 4 alternating press/release keystrokes + return
	// chord asynchronously via USB IRQ wakeups, all within a single p.Next call
	// (no calls to base.Next / Tenfoot UI loop!).
	done := make(chan tenfoot.PlatformInput, 1)
	errCh := make(chan error, 1)
	go func() {
		frame, err := p.Next(ctx)
		if err != nil {
			errCh <- err
			return
		}
		done <- frame
	}()

	// Arm neutral report, then 4 rapid key edges, then Ctrl+Alt+Escape.
	seq := [][8]byte{
		{},
		{0, 0, 4, 0, 0, 0, 0, 0},
		{},
		{0, 0, 5, 0, 0, 0, 0, 0},
		{},
		{0x05, 0, 0x29, 0, 0, 0, 0, 0},
	}
	for _, rep := range seq {
		src.push(rep)
		time.Sleep(time.Millisecond)
	}

	select {
	case err := <-errCh:
		t.Fatalf("Next returned error: %v", err)
	case frame := <-done:
		if len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop {
			t.Fatalf("expected CmdStop from return chord, got %+v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gameplay fast path did not return on Ctrl+Alt+Escape")
	}

	base.mu.Lock()
	nextCalls := base.nextCalls
	base.mu.Unlock()
	if nextCalls != 0 {
		t.Fatalf("gameplay fast path woke Tenfoot base.Next %d times, want 0", nextCalls)
	}
	// Initial neutralization (9) + 4 single-row dirty changes (4) = 13 writes!
	if f.cpu.keyboardWrites != 13 {
		t.Fatalf("keyboardWrites=%d, want 13 (9 init + 4 dirty rows)", f.cpu.keyboardWrites)
	}
}
