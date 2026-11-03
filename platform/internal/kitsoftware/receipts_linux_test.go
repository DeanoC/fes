package kitsoftware

import (
	"errors"
	"fmt"
	"testing"
)

func receiptID(n int) string { return fmt.Sprintf("%064x", n) }

// ledgerState is a kit after many activations: one accepted receipt per past
// generation, cancelled ones at the current generation, and an unreleased
// completion whose receipt is old.
func ledgerState(generations, cancelled int, unreleased uint64) State {
	s := State{Version: 1, Generation: uint64(generations), Selected: testCandidateID, Receipts: map[string]Receipt{}}
	for g := 0; g < generations; g++ {
		r := Request{ID: receiptID(1000000 + g), Candidate: testCandidateID, Requester: "host1", Generation: uint64(g)}
		s.Receipts[r.ID] = Receipt{Request: r, Result: "accepted"}
	}
	for i := 0; i < cancelled; i++ {
		r := Request{ID: receiptID(i), Candidate: testCandidateID, Requester: "host1", Generation: s.Generation}
		s.Receipts[r.ID] = Receipt{Request: r, Result: "cancelled"}
	}
	u := s.Receipts[receiptID(1000000+int(unreleased))].Request
	s.Unreleased = &u
	return s
}

func TestPruneReceiptsKeepsWhatRequestsCanStillNeed(t *testing.T) {
	s := ledgerState(700, 3, 5)
	pruneReceipts(&s)
	if err := validateState(s); err != nil {
		t.Fatal(err)
	}
	if len(s.Receipts) != retainedGenerations+3+1 {
		t.Fatalf("%d receipts kept", len(s.Receipts))
	}
	for g := 700 - retainedGenerations; g < 700; g++ {
		if _, ok := s.Receipts[receiptID(1000000+g)]; !ok {
			t.Fatalf("recent generation %d pruned", g)
		}
	}
	for i := 0; i < 3; i++ {
		if _, ok := s.Receipts[receiptID(i)]; !ok {
			t.Fatal("current-generation receipt pruned")
		}
	}
	if _, ok := s.Receipts[receiptID(1000000+5)]; !ok {
		t.Fatal("unreleased completion's receipt pruned")
	}
	if _, ok := s.Receipts[receiptID(1000000+6)]; ok {
		t.Fatal("old receipt kept")
	}
	small := ledgerState(retainedGenerations, 0, 0)
	pruneReceipts(&small)
	if len(small.Receipts) != retainedGenerations {
		t.Fatalf("ledger within the bound pruned to %d", len(small.Receipts))
	}
}

// Many cancellations beside the activation that ended a generation must not
// crowd out its receipt, whatever the IDs sort as.
func TestPruneReceiptsKeepsWholeGenerations(t *testing.T) {
	s := ledgerState(100, 0, 99)
	s.Unreleased = nil
	for i := 0; i < 2*retainedGenerations; i++ {
		r := Request{ID: receiptID(i), Candidate: testCandidateID, Requester: "host1", Generation: 99}
		s.Receipts[r.ID] = Receipt{Request: r, Result: "cancelled"}
	}
	pruneReceipts(&s)
	if _, ok := s.Receipts[receiptID(1000000+99)]; !ok {
		t.Fatal("receipt of the generation's activation pruned")
	}
	if len(s.Receipts) != retainedGenerations+2*retainedGenerations {
		t.Fatalf("%d receipts kept", len(s.Receipts))
	}
}

func TestRepositoryBoundsReceiptsAndRefusesPrunedReplays(t *testing.T) {
	r, _, _, _ := setup(t)
	if err := r.Update(func(s *State) error { *s = ledgerState(700, 1, 699); return nil }); err != nil {
		t.Fatal(err)
	}
	s, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Receipts) != retainedGenerations+1 {
		t.Fatalf("%d receipts persisted", len(s.Receipts))
	}
	recent := Request{ID: receiptID(1000000 + 699), Candidate: testCandidateID, Requester: "host1", Generation: 699}
	if err := Queue(r, recent); err != nil {
		t.Fatalf("replay of a retained completion: %v", err)
	}
	if err := Cancel(r, receiptID(0), 700); err != nil {
		t.Fatalf("replay of a current-generation cancellation: %v", err)
	}
	pruned := Request{ID: receiptID(1000000 + 3), Candidate: testCandidateID, Requester: "host1", Generation: 3}
	if err := Queue(r, pruned); !errors.Is(err, ErrConflict) {
		t.Fatalf("replay of a pruned completion: %v", err)
	}
	if s, err := r.Read(); err != nil || s.Pending != nil || s.Generation != 700 {
		t.Fatalf("pruned replay changed state: %+v %v", s.Pending, err)
	}
}

func TestCompactReceiptsShrinksAnIdleLedger(t *testing.T) {
	r, _, _, _ := setup(t)
	if err := r.write(ledgerState(700, 0, 699)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := r.CompactReceipts(); err != nil {
			t.Fatal(err)
		}
		if s, err := r.Read(); err != nil || len(s.Receipts) != retainedGenerations {
			t.Fatalf("compacted ledger: %d receipts, %v", len(s.Receipts), err)
		}
	}
}
