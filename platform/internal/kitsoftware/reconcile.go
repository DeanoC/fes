package kitsoftware

import (
	"context"
	"errors"
)

// SuccessorHold is a successor the running agent has already accepted. Release
// retries for that same request do not require the boot reservation to still
// be active. A different durable request discards it.
type SuccessorHold struct {
	Request Request
	Ready   bool
}

// ReservationStillHeld reports whether this boot reservation can still accept
// a held successor. A completed record is published as none. Another
// operation is not this reservation.
func ReservationStillHeld(status AdmissionStatus, operationID string) bool {
	if status.Maintenance.OperationID != "" && status.Maintenance.OperationID != operationID {
		return false
	}
	switch status.Maintenance.State {
	case "", "none", "completed":
		return false
	default:
		return true
	}
}

// HandoffTrial installs the persisted software queue before the caller ends the
// boot maintenance reservation. install runs while that reservation is still
// held. release runs only after the successor is installed, so a failed handoff
// keeps the boot credential and its exclusion.
//
// When reservationActive reports that the boot reservation has finished,
// confirm restores or verifies the successor without requiring that reservation.
// A changed queue or agent makes confirm fail, and release is not called.
// An unreleased completion has no successor queue; its release is the end of
// that reservation.
func HandoffTrial(ctx context.Context, repo Repository, restore Request, hold *SuccessorHold, install func(context.Context, Request) error, confirm func(context.Context, Request) error, reservationActive func(context.Context) (bool, error), release func(context.Context) error) error {
	state, err := repo.Read()
	if err != nil {
		return err
	}
	if state.Unreleased != nil {
		return release(ctx)
	}
	if state.Pending == nil {
		restore.Candidate = state.Selected
		restore.Generation = state.Generation
		if err := Queue(repo, restore); err != nil {
			return err
		}
		state, err = repo.Read()
		if err != nil {
			return err
		}
	}
	if state.Pending == nil {
		return errors.New("persisted software request was not queued")
	}
	pending := state.Pending.Request
	// A different durable request is not the successor already installed.
	if hold != nil && hold.Ready && hold.Request != pending {
		hold.Ready = false
	}
	active := true
	if reservationActive != nil {
		active, err = reservationActive(ctx)
		if err != nil {
			return err
		}
	}
	if !active {
		// The boot reservation can no longer take a held successor. Confirm the
		// remembered request, or the replacement if the durable operation changed.
		// A conflicting queue or a changed agent fails this call, so release waits.
		if confirm == nil {
			return errors.New("boot reservation finished before successor release")
		}
		if err := confirm(ctx, pending); err != nil {
			return err
		}
	} else if err := install(ctx, pending); err != nil {
		return err
	}
	if hold != nil {
		hold.Request = pending
		hold.Ready = true
	}
	return release(ctx)
}
