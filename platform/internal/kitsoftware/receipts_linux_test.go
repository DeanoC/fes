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
		r := Request{ID: receiptID(g), Candidate: testCandidateID, Requester: "host1", Generation: uint64(g)}
		s.Receipts[r.ID] = Receipt{Request: r, Result: "accepted"}
	}
	for i := 0; i < cancelled; i++ {
		r := Request{ID: receiptID(100000 + i), Candidate: testCandidateID, Requester: "host1", Generation: s.Generation}
		s.Receipts[r.ID] = Receipt{Request: r, Result: "cancelled"}
	}
	u := s.Receipts[receiptID(int(unreleased))].Request
	s.Unreleased = &u
	return s
}

func TestPruneReceiptsKeepsWhatRequestsCanStillNeed(t *testing.T) {
	s := ledgerState(700, 3, 5)
	pruneReceipts(&s)
	if err := validateState(s); err != nil {
		t.Fatal(err)
	}
	if len(s.Receipts) != retainedReceipts+3+1 {
		t.Fatalf("%d receipts kept", len(s.Receipts))
	}
	for g := 700 - retainedReceipts; g < 700; g++ {
		if _, ok := s.Receipts[receiptID(g)]; !ok {
			t.Fatalf("recent generation %d pruned", g)
		}
	}
	for i := 0; i < 3; i++ {
		if _, ok := s.Receipts[receiptID(100000+i)]; !ok {
			t.Fatal("current-generation receipt pruned")
		}
	}
	if _, ok := s.Receipts[receiptID(5)]; !ok {
		t.Fatal("unreleased completion's receipt pruned")
	}
	if _, ok := s.Receipts[receiptID(6)]; ok {
		t.Fatal("old receipt kept")
	}
	small := ledgerState(retainedReceipts, 0, 0)
	pruneReceipts(&small)
	if len(small.Receipts) != retainedReceipts {
		t.Fatalf("ledger within the bound pruned to %d", len(small.Receipts))
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
	// The newest older receipts, the unreleased completion's and the cancellation.
	if len(s.Receipts) != retainedReceipts+2 {
		t.Fatalf("%d receipts persisted", len(s.Receipts))
	}
	recent := Request{ID: receiptID(699), Candidate: testCandidateID, Requester: "host1", Generation: 699}
	if err := Queue(r, recent); err != nil {
		t.Fatalf("replay of a retained completion: %v", err)
	}
	if err := Cancel(r, receiptID(100000), 700); err != nil {
		t.Fatalf("replay of a current-generation cancellation: %v", err)
	}
	pruned := Request{ID: receiptID(3), Candidate: testCandidateID, Requester: "host1", Generation: 3}
	if err := Queue(r, pruned); !errors.Is(err, ErrConflict) {
		t.Fatalf("replay of a pruned completion: %v", err)
	}
	if s, err := r.Read(); err != nil || s.Pending != nil || s.Generation != 700 {
		t.Fatalf("pruned replay changed state: %+v %v", s.Pending, err)
	}
}
