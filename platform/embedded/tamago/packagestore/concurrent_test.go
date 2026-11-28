// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

func sharedLease() *returnLease {
	base := lease()
	base.r = sdcontrol.Receipt{
		Owner:      17,
		Generation: 2,
		Active:     true,
		Prepared:   true,
		Shared:     true,
	}
	return &returnLease{testLease: base}
}

func TestSharedStoreRetainActiveAndConcurrencyLifecycle(t *testing.T) {
	body, request, _ := basic(t)
	files := fstest.MapFS{request.Path: &fstest.MapFile{Data: body}}
	l := sharedLease()
	f := &trackedFS{base: files, lease: l.testLease}
	s, err := New(f, l)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Shared() {
		t.Fatal("expected Shared()=true")
	}
	if (&Store{}).Shared() {
		t.Fatal("non-shared store reported Shared()=true")
	}

	// Legacy store refuses RetainActive.
	legacyStore, err := New(&trackedFS{base: files, lease: lease()}, lease())
	if err != nil {
		t.Fatal(err)
	}
	if legacyStore.Shared() {
		t.Fatal("legacy store reported Shared()=true")
	}
	legSel, err := legacyStore.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyStore.RetainActive(legSel); !errors.Is(err, ErrState) {
		t.Fatalf("legacy RetainActive should fail with ErrState, got %v", err)
	}

	// Shared store allows RetainActive without pausing SD.
	sel, err := s.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.RetainActive(sel)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Concurrent() || !h.SameStore(h) {
		t.Fatalf("unexpected concurrent handoff state: concurrent=%v same=%v", h.Concurrent(), h.SameStore(h))
	}
	if h.SameStore(nil) || (&Handoff{}).SameStore(h) {
		t.Fatal("invalid SameStore match")
	}
	if _, err := s.ActiveReceipt(); err != nil {
		t.Fatalf("SD should remain active after RetainActive: %v", err)
	}
	gotSel, err := h.Selection()
	if err != nil || gotSel != sel {
		t.Fatalf("RetainActive Selection failed: %v", err)
	}

	// Shared store refuses ResumeAfterReturn cleanly.
	if err := containForTest(s, sel); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Selection(); !errors.Is(err, ErrState) {
		t.Fatalf("expected old concurrent handoff invalidated after Contain, got %v", err)
	}
	if err := s.ResumeAfterReturn(17, 10); !errors.Is(err, ErrState) {
		t.Fatalf("expected ErrState on shared ResumeAfterReturn, got %v", err)
	}
	// Shared store uses Resume() to reopen cleanly at next generation.
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	r, err := s.ActiveReceipt()
	if err != nil || !r.Shared || r.Generation != 5 {
		t.Fatalf("unexpected resumed receipt: %+v err=%v", r, err)
	}
}

func containForTest(s *Store, sel *Selection) error {
	h, err := s.Contain(sel)
	if err != nil {
		return err
	}
	if h.Concurrent() {
		return errors.New("Contain marked handoff concurrent")
	}
	_, err = h.Selection()
	return err
}
