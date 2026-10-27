package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
	"golang.org/x/sys/unix"
)

type AcknowledgeRequest struct {
	Operation  string `json:"operation"`
	Generation uint64 `json:"generation"`
	Selected   string `json:"selected"`
	Base       string `json:"base"`
	Boot       string `json:"boot"`
	Action     string `json:"action"`
}
type AcknowledgeResult struct {
	Action     string `json:"action"`
	Operation  string `json:"operation"`
	Selected   string `json:"selected"`
	Generation uint64 `json:"generation"`
	Completed  bool   `json:"completed"`
}
type acknowledgmentAudit struct {
	Request  AcknowledgeRequest `json:"request"`
	Failed   Operation          `json:"failed_operation"`
	Previous string             `json:"previous"`
}

func (r AcknowledgeRequest) validate() error {
	if !software.ValidID(r.Operation) || !software.ValidID(r.Action) || !software.ValidID(r.Selected) || !software.ValidID(r.Base) || r.Operation == r.Action {
		return errors.New("acknowledgment requires distinct operation/action IDs and exact selected/base hashes")
	}
	parts := strings.Split(r.Boot, "-")
	if len(parts) != 5 {
		return errors.New("invalid expected boot ID")
	}
	for i, n := range []int{8, 4, 4, 4, 12} {
		if len(parts[i]) != n || strings.Trim(parts[i], "0123456789abcdef") != "" {
			return errors.New("invalid expected boot ID")
		}
	}
	return nil
}
func acknowledgmentReason(a acknowledgmentAudit) string {
	return "operator acknowledgment " + a.Request.Action + " of already-running prior release; automatic recovery remains spent; original failure: " + a.Failed.Reason
}

// failedReservation is the failed operation's existing maintenance reservation.
// Held only reads it. Release ends it with the operation's stored grant.
type failedReservation interface {
	Held(context.Context, Request) error
	Release(context.Context, Request) error
}

// acknowledgePrior has no restart facility. Its only authority is to finish an
// exact blocked operation after independently proving its prior group healthy.
// WorkerLock excludes the live supervisor, including its unreleased retry path.
func acknowledgePrior(ctx context.Context, repo *FileRepository, reservation failedReservation, req AcknowledgeRequest, stable func(context.Context) error) (AcknowledgeResult, error) {
	result := AcknowledgeResult{Action: req.Action, Operation: req.Operation, Selected: req.Selected, Generation: req.Generation}
	if err := req.validate(); err != nil {
		return result, err
	}
	unlock, err := repo.WorkerLock()
	if err != nil {
		return result, err
	}
	defer unlock()
	state, err := repo.Read()
	if err != nil {
		return result, err
	}
	if state.Selected != req.Selected || state.Generation != req.Generation {
		return result, ErrConflict
	}
	if err := repo.repairActionTaken(req.Action, "acknowledgment"); err != nil {
		return result, err
	}
	entries, err := repo.entries()
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "acknowledgment-") && strings.HasSuffix(name, ".json") {
			action := strings.TrimSuffix(strings.TrimPrefix(name, "acknowledgment-"), ".json")
			prior, _, err := repo.readAcknowledgment(action)
			if err != nil {
				return result, err
			}
			if prior.Request.Operation == req.Operation && prior.Request.Action != req.Action {
				return result, ErrConflict
			}
		}
	}
	audit, exists, err := repo.readAcknowledgment(req.Action)
	if err != nil {
		return result, err
	}
	if exists && audit.Request != req {
		return result, ErrConflict
	}
	if !exists {
		if state.Pending == nil || state.Pending.Phase != "blocked" || state.Pending.ID != req.Operation || state.Pending.Previous != req.Selected || state.Unreleased != nil {
			return result, ErrConflict
		}
		audit = acknowledgmentAudit{Request: req, Failed: *state.Pending, Previous: state.Previous}
	}
	if state.Previous != audit.Previous {
		return result, ErrConflict
	}
	receipt, completed := state.Receipts[req.Operation]
	if completed {
		if !exists || state.Pending != nil || receipt.Request != audit.Failed.Request || receipt.Result != "recovered" || receipt.Reason != acknowledgmentReason(audit) {
			return result, ErrConflict
		}
		if state.Unreleased == nil {
			result.Completed = true
			return result, nil
		}
		if *state.Unreleased != audit.Failed.Request {
			return result, ErrConflict
		}
	} else {
		if state.Pending == nil || *state.Pending != audit.Failed || audit.Failed.Phase != "blocked" {
			return result, ErrConflict
		}
		// Prove the prior group healthy before anything changes. The reservation
		// stays as the failed recovery left it, so a failed proof still leaves
		// prepare-power-cycle its blocked reservation. Nothing resumes it: the
		// release below ends a blocked reservation, as it does after any
		// activation whose agent restarted.
		if err := reservation.Held(ctx, audit.Failed.Request); err != nil {
			return result, err
		}
		if err := stable(ctx); err != nil {
			return result, err
		}
		if err := reservation.Held(ctx, audit.Failed.Request); err != nil {
			return result, err
		}
		if err := repo.writeAcknowledgment(audit); err != nil {
			return result, err
		}
		// Keep old-supervisor-compatible receipt semantics. The separate immutable
		// audit and explicit reason preserve both the spent attempt and operator act.
		if err := complete(repo, audit.Failed, "blocked", "recovered", acknowledgmentReason(audit)); err != nil {
			return result, err
		}
	}
	// A crash/lost reply here leaves Unreleased for the existing supervisor's
	// exact idempotent release, never a new activation or a reset recovery budget.
	if err := reservation.Release(ctx, audit.Failed.Request); err != nil {
		return result, err
	}
	if err := repo.Update(func(s *State) error {
		if s.Selected != req.Selected || s.Generation != req.Generation || s.Unreleased == nil || *s.Unreleased != audit.Failed.Request {
			return ErrConflict
		}
		s.Unreleased = nil
		return nil
	}); err != nil {
		return result, err
	}
	result.Completed = true
	return result, nil
}

// repairActionTaken refuses an action identifier that another operator repair
// has already recorded. One identifier names one operator act.
func (r *FileRepository) repairActionTaken(action, own string) error {
	if !software.ValidID(action) {
		return ErrConflict
	}
	for _, kind := range []string{"acknowledgment", "power-cycle", "factory-selection"} {
		if kind == own {
			continue
		}
		var st unix.Stat_t
		err := unix.Fstatat(int(r.dir.Fd()), kind+"-"+action+".json", &st, unix.AT_SYMLINK_NOFOLLOW)
		if err == nil {
			return ErrConflict
		}
		if !errors.Is(err, unix.ENOENT) {
			return err
		}
	}
	return nil
}
func (r *FileRepository) readAcknowledgment(action string) (acknowledgmentAudit, bool, error) {
	var audit acknowledgmentAudit
	if !software.ValidID(action) {
		return audit, false, ErrConflict
	}
	fd, err := unix.Openat(int(r.dir.Fd()), "acknowledgment-"+action+".json", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return audit, false, nil
	}
	if err != nil {
		return audit, false, err
	}
	f := os.NewFile(uintptr(fd), "acknowledgment")
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return audit, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > maxStateSize {
		return audit, false, errors.New("invalid acknowledgment audit")
	}
	decoder := json.NewDecoder(io.LimitReader(f, maxStateSize+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&audit); err != nil {
		return audit, false, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return audit, false, errors.New("trailing acknowledgment audit data")
	}
	if audit.Request.Action != action || audit.Request.validate() != nil || audit.Failed.ID != audit.Request.Operation || audit.Failed.Previous != audit.Request.Selected || audit.Failed.Generation != audit.Request.Generation || audit.Failed.Phase != "blocked" {
		return audit, false, errors.New("invalid acknowledgment audit identity")
	}
	return audit, true, nil
}
func (r *FileRepository) writeAcknowledgment(audit acknowledgmentAudit) error {
	if err := r.check(); err != nil {
		return err
	}
	old, exists, err := r.readAcknowledgment(audit.Request.Action)
	if err != nil {
		return err
	}
	if exists {
		if old != audit {
			return ErrConflict
		}
		return nil
	}
	b, err := json.Marshal(audit)
	if err != nil {
		return err
	}
	if len(b) > maxStateSize {
		return errors.New("acknowledgment audit too large")
	}
	f, err := os.CreateTemp(r.root, ".acknowledgment-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(b)
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	// Link publishes without overwriting a conflicting action, even if another
	// root process fails to observe WorkerLock. The inode is already durable.
	if err := os.Link(f.Name(), filepath.Join(r.root, "acknowledgment-"+audit.Request.Action+".json")); err != nil {
		return err
	}
	if err := os.Remove(f.Name()); err != nil {
		return err
	}
	return r.dir.Sync()
}

func stableAcknowledgment(ctx context.Context, probe func(context.Context) error) error {
	return stableAcknowledgmentWindow(ctx, probe, time.Minute, 10*time.Second, 250*time.Millisecond)
}

func stableAcknowledgmentWindow(ctx context.Context, probe func(context.Context) error, deadline, stable, poll time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	var start time.Time
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := probe(ctx); err != nil {
			return fmt.Errorf("prior release cannot be acknowledged: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if start.IsZero() {
			start = time.Now()
		}
		if time.Since(start) >= stable {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}
