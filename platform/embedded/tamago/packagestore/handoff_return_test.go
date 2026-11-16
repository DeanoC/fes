// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

type handoffReturnLease struct{ *testLease }

func (l *handoffReturnLease) ResumeAfterReturn(owner, generation uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resumes++
	if l.resumeErr != nil {
		return l.resumeErr
	}
	if l.r.Active || owner != l.r.Owner || generation <= l.r.Generation {
		return ErrState
	}
	l.r = sdcontrol.Receipt{Owner: owner, Generation: generation + 2, Active: true, Prepared: true}
	if l.resumeChange != nil {
		l.resumeChange(&l.r)
	}
	return nil
}
func returnHandoff(t *testing.T) (*Store, *Handoff, *handoffReturnLease, Request) {
	t.Helper()
	body, request, _ := basic(t)
	l := &handoffReturnLease{lease()}
	s, err := New(fstest.MapFS{request.Path: &fstest.MapFile{Data: body}}, l)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.Contain(p)
	if err != nil {
		t.Fatal(err)
	}
	return s, h, l, request
}
func TestHandoffReturnKeepsPrivateStoreAndSerializesCopies(t *testing.T) {
	store, h, l, request := returnHandoff(t)
	copied := *h
	var group sync.WaitGroup
	answers := make(chan error, 2)
	for _, handle := range []*Handoff{h, &copied} {
		group.Add(1)
		go func(h *Handoff) { defer group.Done(); answers <- h.ResumeAfterReturn(17, 7) }(handle)
	}
	group.Wait()
	close(answers)
	successes := 0
	for err := range answers {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrState) {
			t.Fatal(err)
		}
	}
	if successes != 1 || l.resumes != 1 {
		t.Fatal("copied handoff replayed return", successes, l.resumes)
	}
	if _, err := h.Selection(); err == nil {
		t.Fatal("resumed handoff remained usable")
	}
	selected, err := store.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	next, err := store.Contain(selected)
	if err != nil {
		t.Fatal(err)
	}
	if next.Receipt().Generation != 10 || next.ReturnedFrom() != (sdcontrol.Receipt{Owner: 17, Generation: 7}) {
		t.Fatal("new SD cycle lost return provenance")
	}
	if err := h.ResumeAfterReturn(17, 7); err == nil || l.resumes != 1 {
		t.Fatal("old handoff crossed a newer cycle")
	}
}
func TestHandoffReturnRejectsChangedHandleAndRetainsUncertainGrant(t *testing.T) {
	for _, fault := range []string{"nil", "empty", "selection", "receipt", "provenance", "owner", "generation", "overflow", "grant", "returned-receipt"} {
		t.Run(fault, func(t *testing.T) {
			_, h, l, _ := returnHandoff(t)
			owner, generation := uint64(17), uint64(7)
			switch fault {
			case "nil":
				h = nil
			case "empty":
				h = &Handoff{}
			case "selection":
				h.selection = nil
			case "receipt":
				h.receipt.Generation++
			case "provenance":
				h.returned.Generation++
			case "owner":
				owner++
			case "generation":
				generation = 4
			case "overflow":
				generation = ^uint64(0) - 1
			case "grant":
				l.resumeErr = errors.New("uncertain grant")
			case "returned-receipt":
				l.resumeChange = func(r *sdcontrol.Receipt) { r.Prepared = false }
			}
			if err := h.ResumeAfterReturn(owner, generation); err == nil {
				t.Fatal("invalid return accepted")
			}
			calls := l.resumes
			if err := h.ResumeAfterReturn(owner, generation); err == nil || l.resumes != calls {
				t.Fatal("failed return replayed lease")
			}
			expected := 0
			if fault == "grant" || fault == "returned-receipt" {
				expected = 1
			}
			if calls != expected {
				t.Fatal("invalid handle reached lease", calls)
			}
		})
	}
}

func TestFreshSelectionRequiresOriginalStoreAndExactReturn(t *testing.T) {
	store, previous, _, request := returnHandoff(t)
	if err := previous.ResumeAfterReturn(17, 7); err != nil {
		t.Fatal(err)
	}
	selection, err := store.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	next, err := store.Contain(selection)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := next.SelectionAfterReturn(previous, 17, 7); err != nil || got != selection {
		t.Fatal(got, err)
	}
	_, foreign, _, _ := returnHandoff(t)
	for _, p := range []*Handoff{nil, {}, foreign} {
		if _, err := next.SelectionAfterReturn(p, 17, 7); err == nil {
			t.Fatal("foreign predecessor accepted")
		}
	}
	for _, r := range []sdcontrol.Receipt{{Owner: 18, Generation: 7}, {Owner: 17, Generation: 6}, {Owner: 17, Generation: ^uint64(0)}} {
		if _, err := next.SelectionAfterReturn(previous, r.Owner, r.Generation); err == nil {
			t.Fatal("changed return accepted")
		}
	}
	changed := *next
	changed.receipt.Generation++
	if _, err := changed.SelectionAfterReturn(previous, 17, 7); err == nil {
		t.Fatal("changed fresh containment accepted")
	}
	if err := next.ResumeAfterReturn(17, 13); err != nil {
		t.Fatal(err)
	}
	if _, err := next.SelectionAfterReturn(previous, 17, 7); err == nil {
		t.Fatal("old fresh selection survived resume")
	}
}
