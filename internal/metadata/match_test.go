package metadata

import (
	"testing"
	"time"
)

func TestMatchCandidatesReturnsExactWithoutMutatingCandidates(t *testing.T) {
	candidates := []Candidate{{
		ProviderID: "2", Name: "Sonic the Hedgehog", AlternativeNames: []string{"Sonic"},
		PlatformIDs: []string{"megadrive"}, Summary: "safe", UpdatedAt: time.Unix(2, 0),
	}}
	before := append([]Candidate(nil), candidates...)
	decision, err := MatchCandidates(LookupInput{Title: " sonic the hedgehog ", System: "megadrive"}, "megadrive", candidates)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != OutcomeExact || decision.Score != 100 || decision.Candidate.ProviderID != "2" {
		t.Fatalf("decision = %#v", decision)
	}
	if candidates[0].Name != before[0].Name || candidates[0].PlatformIDs[0] != before[0].PlatformIDs[0] {
		t.Fatal("matcher mutated provider candidate")
	}
}

func TestMatchCandidatesAcceptsOneApprovedDecoratedCandidate(t *testing.T) {
	decision, err := MatchCandidates(LookupInput{Title: "Sonic (USA)", System: "megadrive"}, "megadrive", []Candidate{{
		ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"megadrive"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != OutcomeConfident || decision.Score != 95 {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestMatchCandidatesAcceptsDecoratedEquivalenceInEitherDirection(t *testing.T) {
	tests := []struct {
		name      string
		lookup    string
		candidate Candidate
	}{
		{name: "primary reverse", lookup: "Sonic", candidate: Candidate{ProviderID: "1", Name: "Sonic (USA)", PlatformIDs: []string{"megadrive"}}},
		{name: "alternate reverse", lookup: "Sonic (USA)", candidate: Candidate{ProviderID: "2", Name: "Different", AlternativeNames: []string{"Sonic"}, PlatformIDs: []string{"megadrive"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := MatchCandidates(LookupInput{Title: test.lookup, System: "megadrive"}, "megadrive", []Candidate{test.candidate})
			if err != nil || decision.Outcome != OutcomeConfident || decision.Score != 95 {
				t.Fatalf("decision=%#v err=%v", decision, err)
			}
		})
	}
}

func TestMatchCandidatesRejectsWeakPlatformMismatchAndAmbiguousTie(t *testing.T) {
	weak, err := MatchCandidates(LookupInput{Title: "Sonic 2", System: "megadrive"}, "megadrive", []Candidate{{
		ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"megadrive"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if weak.Outcome != OutcomeNoMatch {
		t.Fatalf("weak decision = %#v", weak)
	}
	mismatch, err := MatchCandidates(LookupInput{Title: "Sonic", System: "megadrive"}, "megadrive", []Candidate{{
		ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"snes"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if mismatch.Outcome != OutcomeNoMatch {
		t.Fatalf("mismatch decision = %#v", mismatch)
	}
	ambiguous, err := MatchCandidates(LookupInput{Title: "Sonic", System: "megadrive"}, "megadrive", []Candidate{{
		ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"megadrive"},
	}, {
		ProviderID: "2", Name: "Sonic", PlatformIDs: []string{"megadrive"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous.Outcome != OutcomeAmbiguous || ambiguous.Candidate.ProviderID != "" {
		t.Fatalf("ambiguous decision = %#v", ambiguous)
	}
}

func TestMatchCandidatesRejectsConflictingDuplicateProviderID(t *testing.T) {
	decision, err := MatchCandidates(LookupInput{Title: "Sonic", System: "megadrive"}, "megadrive", []Candidate{{
		ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"megadrive"},
	}, {
		ProviderID: "1", Name: "Sonic 2", PlatformIDs: []string{"megadrive"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != OutcomeAmbiguous {
		t.Fatalf("decision = %#v", decision)
	}
}
