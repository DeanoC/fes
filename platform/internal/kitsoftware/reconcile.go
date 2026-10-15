package kitsoftware

import (
	"context"
	"errors"
)

// HandoffTrial installs the persisted software queue before the caller ends the
// boot maintenance reservation. install runs while that reservation is still
// held. release runs only after install succeeds, so a failed handoff keeps the
// boot credential and its exclusion. An unreleased completion has no successor
// queue; its release is the end of that reservation.
func HandoffTrial(ctx context.Context, repo Repository, restore Request, install func(context.Context, Request) error, release func(context.Context) error) error {
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
	if err := install(ctx, state.Pending.Request); err != nil {
		return err
	}
	return release(ctx)
}
