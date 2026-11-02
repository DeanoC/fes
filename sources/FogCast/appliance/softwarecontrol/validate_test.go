package softwarecontrol

import (
	"strings"
	"testing"
)

func TestValidateStateSelectionAndJournal(t *testing.T) {
	selected, candidate, id := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	request := Request{ID: id, Candidate: candidate, Requester: "kit-owner", Generation: 0}
	base := func() State { return State{Version: 1, Selected: selected, Receipts: map[string]Receipt{}} }
	for name, mutate := range map[string]func(*State){
		"initial":   func(s *State) {},
		"queued":    func(s *State) { s.Pending = &Operation{Request: request, Previous: selected, Phase: "queued"} },
		"completed": func(s *State) { s.Receipts[id] = Receipt{Request: request, Result: "accepted"} },
		"unreleased": func(s *State) {
			s.Receipts[id] = Receipt{Request: request, Result: "recovered"}
			s.Unreleased = &request
		},
	} {
		t.Run(name, func(t *testing.T) {
			state := base()
			mutate(&state)
			if err := ValidateState(state); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, mutate := range map[string]func(*State){
		"version":      func(s *State) { s.Version = 0 },
		"selected":     func(s *State) { s.Selected = "invalid" },
		"previous":     func(s *State) { s.Previous = "invalid" },
		"nil receipts": func(s *State) { s.Receipts = nil },
		"receipt id mismatch": func(s *State) {
			s.Receipts[id] = Receipt{Request: Request{ID: candidate, Candidate: candidate, Requester: "kit-owner"}, Result: "accepted"}
		},
		"receipt invalid candidate": func(s *State) {
			r := request
			r.Candidate = "invalid"
			s.Receipts[id] = Receipt{Request: r, Result: "accepted"}
		},
		"receipt invalid owner": func(s *State) {
			r := request
			r.Requester = " bad"
			s.Receipts[id] = Receipt{Request: r, Result: "accepted"}
		},
		"receipt future generation": func(s *State) {
			r := request
			r.Generation = 1
			s.Receipts[id] = Receipt{Request: r, Result: "accepted"}
		},
		"receipt invalid result":    func(s *State) { s.Receipts[id] = Receipt{Request: request, Result: "other"} },
		"pending previous mismatch": func(s *State) { s.Pending = &Operation{Request: request, Previous: candidate, Phase: "queued"} },
		"pending invalid phase":     func(s *State) { s.Pending = &Operation{Request: request, Previous: selected, Phase: "bad"} },
		"pending already completed": func(s *State) {
			s.Pending = &Operation{Request: request, Previous: selected, Phase: "queued"}
			s.Receipts[id] = Receipt{Request: request, Result: "accepted"}
		},
		"unreleased missing receipt": func(s *State) { s.Unreleased = &request },
		"unreleased cancelled": func(s *State) {
			s.Receipts[id] = Receipt{Request: request, Result: "cancelled"}
			s.Unreleased = &request
		},
	} {
		t.Run(name, func(t *testing.T) {
			state := base()
			mutate(&state)
			if err := ValidateState(state); err == nil {
				t.Fatal("invalid state accepted")
			}
		})
	}
}
