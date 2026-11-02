// Package kitsoftware owns durable userspace selection independently of the
// replaceable services. A repository must reside on verified persistent storage.
package kitsoftware

import (
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrConflict = errors.New("software selection conflict")
	ErrBusy     = errors.New("software activation waiting for idle maintenance")
)

type Request = softwarecontrol.Request
type Operation = softwarecontrol.Operation
type Receipt = softwarecontrol.Receipt
type State = softwarecontrol.State

// Repository serializes short transactions separately from the single activation
// worker. Update commits and syncs the result before returning success.
type Repository interface {
	Read() (State, error)
	Update(func(*State) error) error
	WorkerLock() (func(), error)
}

func validOwner(s string) bool {
	return len(s) <= 160 && utf8.ValidString(s) && strings.TrimSpace(s) == s && s != "" && strings.IndexFunc(s, unicode.IsControl) < 0
}

func validateState(s State) error {
	return softwarecontrol.ValidateState(s)
}

// Queue is idempotent by the complete request, including its original expected
// generation. A completed or cancelled identifier is never reused.
func Queue(r Repository, req Request) error {
	if !software.ValidID(req.ID) || !software.ValidID(req.Candidate) || !validOwner(req.Requester) {
		return errors.New("invalid software activation request")
	}
	return r.Update(func(s *State) error {
		if old, ok := s.Receipts[req.ID]; ok {
			if old.Request == req {
				return nil
			}
			return ErrConflict
		}
		if s.Unreleased != nil {
			return ErrConflict
		}
		if s.Pending != nil {
			if s.Pending.Request == req {
				return nil
			}
			return ErrConflict
		}
		if req.Generation != s.Generation {
			return ErrConflict
		}
		s.Pending = &Operation{Request: req, Previous: s.Selected, Phase: "queued", Reason: "waiting for idle maintenance"}
		return nil
	})
}

func Cancel(r Repository, id string, generation uint64) error {
	return r.Update(func(s *State) error {
		if old, ok := s.Receipts[id]; ok && old.Result == "cancelled" && old.Generation == generation {
			return nil
		}
		p := s.Pending
		if p == nil || p.ID != id || p.Generation != generation || p.Phase != "queued" {
			return ErrConflict
		}
		s.Receipts[id] = Receipt{Request: p.Request, Result: "cancelled"}
		s.Pending = nil
		return nil
	})
}

func transition(r Repository, op Operation, from, to, reason string) error {
	return r.Update(func(s *State) error {
		p := s.Pending
		if p == nil || p.Request != op.Request || p.Previous != op.Previous || s.Generation != op.Generation || p.Phase != from {
			return ErrConflict
		}
		p.Phase, p.Reason = to, reason
		return nil
	})
}

func complete(r Repository, op Operation, phase, result, reason string) error {
	return r.Update(func(s *State) error {
		p := s.Pending
		if p == nil || p.Request != op.Request || p.Previous != op.Previous || p.Phase != phase || s.Generation != op.Generation {
			return ErrConflict
		}
		if result == "accepted" {
			if s.Generation == ^uint64(0) {
				return fmt.Errorf("selection generation exhausted")
			}
			s.Previous, s.Selected = op.Previous, op.Candidate
			s.Generation++
		}
		s.Unreleased = &op.Request
		s.Receipts[op.ID] = Receipt{Request: op.Request, Result: result, Reason: reason}
		s.Pending = nil
		return nil
	})
}
