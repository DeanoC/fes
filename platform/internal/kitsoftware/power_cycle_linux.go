package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// PowerCyclePreparationResult records abandonment of a failed software request,
// not physical recovery. Only a separate operator power-supply cycle and fresh
// postboot evidence can establish recovery. New requests from other hosts remain
// legitimate and are never overwritten by replaying this action.
type PowerCyclePreparationResult struct {
	Action     string `json:"action"`
	Operation  string `json:"operation"`
	Selected   string `json:"selected"`
	Generation uint64 `json:"generation"`
	Outcome    string `json:"outcome"`
}
type nativePowerCycleProof struct {
	nativeStatus
	RuntimePID        int    `json:"runtime_pid"`
	RuntimeStartTime  string `json:"runtime_start_time"`
	RuntimeExecutable string `json:"runtime_executable"`
}
type powerCycleEvidence struct {
	Native      nativePowerCycleProof `json:"native"`
	Maintenance MaintenanceStatus     `json:"maintenance"`
}
type powerCycleAudit struct {
	Request  AcknowledgeRequest `json:"request"`
	Failed   Operation          `json:"failed_operation"`
	Previous string             `json:"previous"`
	Evidence powerCycleEvidence `json:"evidence"`
}

func powerCycleReason(a powerCycleAudit) string {
	return "operator action " + a.Request.Action + ": failed software operation abandoned; awaiting physical power-supply cycle; automatic recovery remains spent; original failure: " + a.Failed.Reason
}

// verify returns a held maintenance-marker lock. It authenticates the saved
// credential read-only and obtains native proof without resuming cleanup.
func preparePowerCycle(ctx context.Context, repo *FileRepository, req AcknowledgeRequest, verify func(context.Context, Operation) (powerCycleEvidence, func(), error)) (PowerCyclePreparationResult, error) {
	result := PowerCyclePreparationResult{Action: req.Action, Operation: req.Operation, Selected: req.Selected, Generation: req.Generation}
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
	if err := repo.repairActionTaken(req.Action, "power-cycle"); err != nil {
		return result, err
	}
	entries, err := repo.entries()
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "power-cycle-") && strings.HasSuffix(name, ".json") {
			action := strings.TrimSuffix(strings.TrimPrefix(name, "power-cycle-"), ".json")
			prior, _, err := repo.readPowerCycleAudit(action)
			if err != nil {
				return result, err
			}
			if prior.Request.Operation == req.Operation && prior.Request.Action != req.Action {
				return result, ErrConflict
			}
		}
	}
	audit, exists, err := repo.readPowerCycleAudit(req.Action)
	if err != nil {
		return result, err
	}
	if exists && audit.Request != req {
		return result, ErrConflict
	}
	if receipt, complete := state.Receipts[req.Operation]; complete {
		if !exists || receipt.Request != audit.Failed.Request || receipt.Result != "cancelled" || receipt.Reason != powerCycleReason(audit) || state.Previous != audit.Previous {
			return result, ErrConflict
		}
		// A later queued operation is independent intent. Never cancel or replace it
		// and never replay the previous boot's private maintenance credential.
		result.Outcome = "prepared-awaiting-physical-cycle"
		return result, nil
	}
	if state.Pending == nil || state.Pending.ID != req.Operation || state.Pending.Generation != req.Generation || state.Pending.Previous != req.Selected || state.Pending.Phase != "blocked" || state.Unreleased != nil {
		return result, ErrConflict
	}
	if exists && (audit.Failed != *state.Pending || audit.Previous != state.Previous) {
		return result, ErrConflict
	}
	proof, release, err := verify(ctx, *state.Pending)
	if err != nil {
		return result, err
	}
	if release == nil {
		return result, errors.New("maintenance proof did not retain its lock")
	}
	defer release()
	if !validPowerCycleEvidence(proof, req, *state.Pending) {
		return result, errors.New("power-cycle evidence does not identify this failed operation")
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !exists {
		audit = powerCycleAudit{Request: req, Failed: *state.Pending, Previous: state.Previous, Evidence: proof}
	}
	if err := repo.writePowerCycleAudit(audit); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	err = repo.Update(func(current *State) error {
		if current.Selected != req.Selected || current.Previous != audit.Previous || current.Generation != req.Generation || current.Pending == nil || *current.Pending != audit.Failed || current.Unreleased != nil {
			return ErrConflict
		}
		current.Receipts[req.Operation] = Receipt{Request: audit.Failed.Request, Result: "cancelled", Reason: powerCycleReason(audit)}
		current.Pending = nil
		// Deliberately no Unreleased: the old supervisor must not call /end. Its
		// existing blocked reservation stays fenced until an actual power cycle.
		return nil
	})
	if err != nil {
		return result, err
	}
	result.Outcome = "prepared-awaiting-physical-cycle"
	return result, nil
}
func (r *FileRepository) readPowerCycleAudit(action string) (powerCycleAudit, bool, error) {
	var audit powerCycleAudit
	if len(action) != 64 || strings.Trim(action, "0123456789abcdef") != "" {
		return audit, false, ErrConflict
	}
	fd, err := unix.Openat(int(r.dir.Fd()), "power-cycle-"+action+".json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return audit, false, nil
	}
	if err != nil {
		return audit, false, err
	}
	f := os.NewFile(uintptr(fd), "power-cycle-audit")
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return audit, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > maxStateSize {
		return audit, false, errors.New("invalid power-cycle audit")
	}
	d := json.NewDecoder(io.LimitReader(f, maxStateSize+1))
	d.DisallowUnknownFields()
	if err := d.Decode(&audit); err != nil {
		return audit, false, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return audit, false, errors.New("trailing power-cycle audit data")
	}
	if audit.Request.Action != action || audit.Request.validate() != nil || audit.Failed.ID != audit.Request.Operation || audit.Failed.Previous != audit.Request.Selected || audit.Failed.Generation != audit.Request.Generation || audit.Failed.Phase != "blocked" {
		return audit, false, errors.New("invalid power-cycle audit identity")
	}
	if !validPowerCycleEvidence(audit.Evidence, audit.Request, audit.Failed) {
		return audit, false, errors.New("invalid power-cycle audit proof")
	}
	return audit, true, nil
}
func (r *FileRepository) writePowerCycleAudit(a powerCycleAudit) error {
	if err := r.check(); err != nil {
		return err
	}
	old, exists, err := r.readPowerCycleAudit(a.Request.Action)
	if err != nil {
		return err
	}
	if exists {
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(a)
		if string(before) != string(after) {
			return ErrConflict
		}
		return nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if len(b) > maxStateSize {
		return errors.New("power-cycle audit too large")
	}
	f, err := os.CreateTemp(r.root, ".power-cycle-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, werr := f.Write(b)
	if err := errors.Join(werr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if err := os.Link(f.Name(), filepath.Join(r.root, "power-cycle-"+a.Request.Action+".json")); err != nil {
		return err
	}
	if err := os.Remove(f.Name()); err != nil {
		return err
	}
	return r.dir.Sync()
}

func validPowerCycleEvidence(proof powerCycleEvidence, req AcknowledgeRequest, op Operation) bool {
	return !(proof.Native.Protocol != 2 || proof.Native.State != "reboot_required" || !proof.Native.OK || proof.Native.Execution != "none" || proof.Native.Error == nil || proof.Native.Error.Code != "idle_failed" || proof.Native.Error.Phase != "recovery" || proof.Native.RuntimePID <= 1 || proof.Native.RuntimeStartTime == "" || proof.Native.RuntimeExecutable == "" || proof.Maintenance.State != "blocked" || proof.Maintenance.OperationID != req.Operation || proof.Maintenance.BootID != req.Boot || proof.Maintenance.Owner != op.Requester || proof.Maintenance.Purpose != "kit-software-activation") && proof.Native.Version != "" && filepath.IsAbs(proof.Native.RuntimeExecutable) && len(proof.Maintenance.Generation) == 64 && strings.Trim(proof.Maintenance.Generation, "0123456789abcdef") == ""
}
