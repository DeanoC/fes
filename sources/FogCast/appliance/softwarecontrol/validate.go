package softwarecontrol

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/appliance/software"
)

// validOwner matches the supervisor's operation requester contract.
func validOwner(s string) bool {
	return len(s) <= 160 && utf8.ValidString(s) && strings.TrimSpace(s) == s && s != "" && strings.IndexFunc(s, unicode.IsControl) < 0
}

// ValidateState checks the persistent supervisor selection and operation journal.
func ValidateState(s State) error {
	if s.Version != 1 || !software.ValidID(s.Selected) || (s.Previous != "" && !software.ValidID(s.Previous)) || s.Receipts == nil {
		return errors.New("invalid software state")
	}
	if s.Pending != nil {
		p := s.Pending
		if !software.ValidID(p.ID) || !software.ValidID(p.Candidate) || !validOwner(p.Requester) || p.Generation != s.Generation || p.Previous != s.Selected {
			return errors.New("invalid software operation")
		}
		switch p.Phase {
		case "queued", "applying", "recovering", "blocked":
		default:
			return errors.New("invalid software phase")
		}
		if _, ok := s.Receipts[p.ID]; ok {
			return errors.New("pending software request already completed")
		}
	}
	if s.Unreleased != nil {
		receipt, ok := s.Receipts[s.Unreleased.ID]
		if !ok || receipt.Request != *s.Unreleased || receipt.Result == "cancelled" || s.Pending != nil {
			return errors.New("invalid software completion journal")
		}
	}
	for id, r := range s.Receipts {
		if id != r.ID || !software.ValidID(id) || !software.ValidID(r.Candidate) || !validOwner(r.Requester) || r.Generation > s.Generation {
			return errors.New("invalid software receipt")
		}
		switch r.Result {
		case "accepted", "recovered", "cancelled":
		default:
			return errors.New("invalid software receipt result")
		}
	}
	return nil
}
