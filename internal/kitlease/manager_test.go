package kitlease

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func req(s string) ClaimRequest {
	return ClaimRequest{RequestID: strings.Repeat(s, 32), Owner: s, Purpose: "test"}
}
func wait(t *testing.T, m *Manager, state string) Status {
	t.Helper()
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		s := m.Status()
		if s.State == state {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("want %s got %+v", state, m.Status())
	return Status{}
}
func ready(t *testing.T, ttl time.Duration, clean func(context.Context) error) *Manager {
	t.Helper()
	m := New(ttl, clean)
	t.Cleanup(m.Close)
	wait(t, m, "free")
	return m
}
func TestCompetingClaims(t *testing.T) {
	m := ready(t, time.Minute, func(context.Context) error { return nil })
	var wg sync.WaitGroup
	var n atomic.Int32
	for _, s := range []string{"a", "b"} {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			if _, e := m.Claim(req(s)); e == nil {
				n.Add(1)
			} else if !errors.Is(e, ErrBusy) {
				t.Error(e)
			}
		}(s)
	}
	wg.Wait()
	if n.Load() != 1 {
		t.Fatal(n.Load())
	}
	if _, _, e := m.Begin("foreign"); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
}
func TestRetryRenewStaleOwner(t *testing.T) {
	m := ready(t, time.Minute, func(context.Context) error { return nil })
	a, e := m.Claim(req("a"))
	if e != nil {
		t.Fatal(e)
	}
	again, e := m.Claim(req("a"))
	if e != nil || again.Token != a.Token {
		t.Fatal("retry", e)
	}
	changed := req("a")
	changed.Purpose = "other"
	if _, e = m.Claim(changed); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e = m.Renew(a.Token); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Release("foreign"); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
	_, e = m.Release(a.Token)
	if e != nil {
		t.Fatal(e)
	}
	wait(t, m, "free")
	b, e := m.Claim(req("b"))
	if e != nil || b.Status.Generation == a.Status.Generation {
		t.Fatal(e)
	}
	if _, e = m.Release(a.Token); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
	if _, e = m.Claim(req("a")); !errors.Is(e, ErrLease) {
		t.Fatal("retired replay", e)
	}
}
func TestExpiryWaitsForAdmittedOperation(t *testing.T) {
	var n atomic.Int32
	m := ready(t, 40*time.Millisecond, func(context.Context) error { n.Add(1); return nil })
	g, _ := m.Claim(req("a"))
	ctx, done, e := m.Begin(g.Token)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("not revoked")
	}
	if m.Status().State != "revoking" || n.Load() != 1 {
		t.Fatal("premature cleanup")
	}
	if _, _, e = m.Begin(g.Token); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
	if _, e = m.Claim(req("b")); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	done()
	done()
	wait(t, m, "free")
	if n.Load() != 2 {
		t.Fatal(n.Load())
	}
}
func TestRenewTimer(t *testing.T) {
	m := ready(t, 150*time.Millisecond, func(context.Context) error { return nil })
	g, _ := m.Claim(req("a"))
	time.Sleep(100 * time.Millisecond)
	if _, e := m.Renew(g.Token); e != nil {
		t.Fatal(e)
	}
	time.Sleep(75 * time.Millisecond)
	if m.Status().State != "held" {
		t.Fatal("old timer fired")
	}
	wait(t, m, "free")
}
func TestTakeoverFencesAndWaits(t *testing.T) {
	m := ready(t, time.Minute, func(context.Context) error { return nil })
	g, _ := m.Claim(req("a"))
	_, done, _ := m.Begin(g.Token)
	r := TakeoverRequest{ClaimRequest: req("b"), ExpectedGeneration: g.Status.Generation, Reason: "recover"}
	if _, e := m.Takeover(r); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	if _, e := m.Renew(g.Token); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
	done()
	wait(t, m, "free")
	b, e := m.Takeover(r)
	if e != nil {
		t.Fatal(e)
	}
	again, e := m.Takeover(r)
	if e != nil || b.Token != again.Token {
		t.Fatal("retry", e)
	}
	if _, _, e = m.Begin(g.Token); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
	r.ClaimRequest = req("c")
	if _, e = m.Takeover(r); !errors.Is(e, ErrLease) {
		t.Fatal("stale takeover", e)
	}
}
func TestFailedCleanupAndOperatorRetry(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	m := New(time.Minute, func(context.Context) error {
		if fail.Load() {
			return errors.New("private details")
		}
		return nil
	})
	defer m.Close()
	s := wait(t, m, "blocked")
	if strings.Contains(s.Reason, "private") {
		t.Fatal("error leaked")
	}
	if _, e := m.Claim(req("a")); !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
	fail.Store(false)
	r := TakeoverRequest{ClaimRequest: req("b"), ExpectedGeneration: s.Generation, Reason: "repaired"}
	if _, e := m.Takeover(r); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	wait(t, m, "free")
	if _, e := m.Takeover(r); e != nil {
		t.Fatal(e)
	}
}
func TestAbandonedTakeoverDoesNotReserve(t *testing.T) {
	m := ready(t, time.Minute, func(context.Context) error { return nil })
	g, _ := m.Claim(req("a"))
	r := TakeoverRequest{ClaimRequest: req("b"), ExpectedGeneration: g.Status.Generation, Reason: "recover"}
	m.Takeover(r)
	wait(t, m, "free")
	if _, e := m.Claim(req("c")); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Takeover(r); !errors.Is(e, ErrLease) {
		t.Fatal(e)
	}
}
func TestValidationAndClose(t *testing.T) {
	m := ready(t, time.Minute, func(context.Context) error { return nil })
	for _, r := range []ClaimRequest{{}, {RequestID: "short", Owner: "a", Purpose: "b"}, {RequestID: strings.Repeat("a", 32), Owner: "a\n", Purpose: "b"}} {
		if _, e := m.Claim(r); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
	m.Close()
	if _, e := m.Claim(req("a")); !errors.Is(e, ErrBlocked) {
		t.Fatal(e)
	}
}

func TestCloseWaitsForCleanup(t *testing.T) {
	entered := make(chan struct{})
	finish := make(chan struct{})
	m := New(time.Minute, func(context.Context) error { close(entered); <-finish; return nil })
	<-entered
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned during cleanup")
	case <-time.After(20 * time.Millisecond):
	}
	close(finish)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish")
	}
}

func TestGrantReportsRemainingDurationOnRetry(t *testing.T) {
	m := ready(t, time.Second, func(context.Context) error { return nil })
	g, e := m.Claim(req("a"))
	if e != nil {
		t.Fatal(e)
	}
	if g.Status.ExpiresInMS < 900 || g.Status.ExpiresInMS > 1000 {
		t.Fatalf("remaining=%d", g.Status.ExpiresInMS)
	}
	time.Sleep(30 * time.Millisecond)
	again, e := m.Claim(req("a"))
	if e != nil {
		t.Fatal(e)
	}
	if again.Status.ExpiresInMS >= g.Status.ExpiresInMS {
		t.Fatal("retry resets remaining duration")
	}
}
