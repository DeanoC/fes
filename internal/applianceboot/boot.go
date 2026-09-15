// Package applianceboot runs the one-shot boot choice independently of Linux
// mount/watchdog syscalls, so fallback policy can be exercised without rebooting.
package applianceboot

import (
	"context"
	"errors"
	"fmt"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
)

type Store interface {
	BeginBoot(string) (store.Selection, error)
	Status() (store.Status, error)
	Verify(string) (release.Manifest, error)
	ImagePath(string) (string, error)
	RejectTrialContext(context.Context, string, string) error
	RecordFallbackContext(context.Context, string, string) error
}
type Root interface {
	Close() error
	// Exec must replace PID 1. Any return is fatal: pivot may already have run.
	Exec() error
}
type Platform interface {
	// Prepare checks the mounted image and executable init before any pivot.
	Prepare(string) (Root, error)
	Ticket(store.Selection) error
	Arm(context.Context, store.Selection) error
}

func Run(ctx context.Context, s Store, factory, bootID string, p Platform) error {
	selected, err := s.BeginBoot(bootID)
	var failures []error
	var candidates []store.Selection
	if err == nil {
		candidates = append(candidates, selected)
	} else {
		failures = append(failures, err)
	}
	state, stateErr := s.Status()
	if stateErr != nil {
		failures = append(failures, stateErr)
	}
	// Verification of fallbacks is delayed until it is needed; hashing all retained
	// images on a successful boot needlessly delays readiness on the SD card.
	hashes := []string{state.Good, factory}
	seen := map[string]bool{}
	for len(candidates) > 0 || len(hashes) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		var choice store.Selection
		if len(candidates) > 0 {
			choice = candidates[0]
			candidates = candidates[1:]
		} else {
			hash := hashes[0]
			hashes = hashes[1:]
			if hash == "" || seen[hash] {
				continue
			}
			manifest, err := s.Verify(hash)
			if err != nil {
				seen[hash] = true
				failures = append(failures, err)
				continue
			}
			path, err := s.ImagePath(hash)
			if err != nil {
				return err
			}
			choice = store.Selection{Manifest: manifest, Path: path, BootID: bootID}
		}
		if seen[choice.Manifest.ImageSHA256] {
			continue
		}
		seen[choice.Manifest.ImageSHA256] = true
		root, err := p.Prepare(choice.Path)
		if err != nil {
			if choice.Trial {
				if rejectErr := s.RejectTrialContext(ctx, bootID, choice.Manifest.ImageSHA256); rejectErr != nil {
					return fmt.Errorf("record rejected trial before fallback: %w", rejectErr)
				}
			}
			failures = append(failures, err)
			continue
		}
		defer root.Close()
		if !choice.Trial {
			if err = s.RecordFallbackContext(ctx, bootID, choice.Manifest.ImageSHA256); err != nil {
				return fmt.Errorf("record verified boot fallback: %w", err)
			}
		}
		if err = p.Ticket(choice); err != nil {
			return fmt.Errorf("record running image: %w", err)
		}
		if choice.Trial {
			if err = p.Arm(ctx, choice); err != nil {
				return fmt.Errorf("arm trial watchdog: %w", err)
			}
		}
		// Never try another root after guard startup or pivot/exec. A failure here
		// reboots; the durably consumed trial cannot be selected on that next boot.
		err = root.Exec()
		if err == nil {
			err = errors.New("init unexpectedly returned")
		}
		return fmt.Errorf("execute selected init: %w", err)
	}
	return fmt.Errorf("no bootable release: %w", errors.Join(failures...))
}
