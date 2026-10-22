package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast/appliance/software"
	"golang.org/x/sys/unix"
)

// FactorySelectionResult records an operator's way out of spent recovery. The
// supervisor starts the selected factory software at its next startup attempt;
// only that start and fresh status evidence establish that the kit runs again.
type FactorySelectionResult struct {
	Action     string `json:"action"`
	Operation  string `json:"operation"`
	Selected   string `json:"selected"`
	Generation uint64 `json:"generation"`
	Outcome    string `json:"outcome"`
}
type factorySelectionAudit struct {
	Request  AcknowledgeRequest `json:"request"`
	Failed   Operation          `json:"failed_operation"`
	Previous string             `json:"previous"`
	Factory  string             `json:"factory"`
}

func factorySelectionReason(a factorySelectionAudit) string {
	return "operator action " + a.Request.Action + ": failed software operation abandoned after recovery was exhausted; selected factory software " + a.Factory + "; original failure: " + a.Failed.Reason
}

// factorySelected reports whether the journal still holds exactly the
// selection this audit made, so a replay never reports a replaced outcome.
func factorySelected(s State, a factorySelectionAudit) bool {
	if a.Request.Selected == a.Factory {
		return s.Selected == a.Factory && s.Previous == a.Previous && s.Generation == a.Request.Generation
	}
	return s.Selected == a.Factory && s.Previous == a.Request.Selected && s.Generation == a.Request.Generation+1
}

// selectFactory abandons an exact blocked operation that no running group can
// acknowledge and commits this image's factory software as the selection. It
// never starts, stops or signals a service and never calls admission. idle must
// prove that nothing supervised runs and that this boot holds no maintenance
// reservation, which is the state Boot leaves after refusing spent recovery.
func selectFactory(ctx context.Context, repo *FileRepository, req AcknowledgeRequest, factory string, idle func(context.Context) error) (FactorySelectionResult, error) {
	result := FactorySelectionResult{Action: req.Action, Operation: req.Operation, Selected: factory}
	if err := req.validate(); err != nil {
		return result, err
	}
	if !software.ValidID(factory) {
		return result, errors.New("invalid factory software identity")
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
	if err := repo.repairActionTaken(req.Action, "factory-selection"); err != nil {
		return result, err
	}
	// An attempt whose journal write never happened leaves only its audit. This
	// command is the one that runs after a reboot, when that attempt's boot
	// identity can no longer be repeated, so a fresh action may follow it. The
	// journal update below still lets only one action end the operation, and a
	// replay must match the receipt that action wrote.
	audit, exists, err := repo.readFactorySelectionAudit(req.Action)
	if err != nil {
		return result, err
	}
	if exists && (audit.Request != req || audit.Factory != factory) {
		return result, ErrConflict
	}
	if receipt, done := state.Receipts[req.Operation]; done {
		if !exists || receipt.Request != audit.Failed.Request || receipt.Result != "cancelled" || receipt.Reason != factorySelectionReason(audit) || !factorySelected(state, audit) {
			return result, ErrConflict
		}
		result.Generation, result.Outcome = state.Generation, "factory-selected"
		return result, nil
	}
	if state.Selected != req.Selected || state.Generation != req.Generation {
		return result, ErrConflict
	}
	if state.Pending == nil || state.Pending.ID != req.Operation || state.Pending.Phase != "blocked" || state.Unreleased != nil {
		return result, ErrConflict
	}
	if exists && (audit.Failed != *state.Pending || audit.Previous != state.Previous) {
		return result, ErrConflict
	}
	if err := idle(ctx); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !exists {
		audit = factorySelectionAudit{Request: req, Failed: *state.Pending, Previous: state.Previous, Factory: factory}
	}
	if err := repo.writeFactorySelectionAudit(audit); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	generation := req.Generation
	err = repo.Update(func(current *State) error {
		if current.Selected != req.Selected || current.Previous != audit.Previous || current.Generation != req.Generation || current.Pending == nil || *current.Pending != audit.Failed || current.Unreleased != nil {
			return ErrConflict
		}
		current.Receipts[req.Operation] = Receipt{Request: audit.Failed.Request, Result: "cancelled", Reason: factorySelectionReason(audit)}
		current.Pending = nil
		// Nothing is unreleased: this boot holds no reservation for the failed
		// operation. Boot starts the selection and ends its own boot admission.
		if current.Selected != factory {
			if current.Generation == ^uint64(0) {
				return errors.New("selection generation exhausted")
			}
			current.Previous, current.Selected = current.Selected, factory
			current.Generation++
		}
		generation = current.Generation
		return nil
	})
	if err != nil {
		return result, err
	}
	result.Generation, result.Outcome = generation, "factory-selected"
	return result, nil
}
func (r *FileRepository) readFactorySelectionAudit(action string) (factorySelectionAudit, bool, error) {
	var audit factorySelectionAudit
	if !software.ValidID(action) {
		return audit, false, ErrConflict
	}
	fd, err := unix.Openat(int(r.dir.Fd()), "factory-selection-"+action+".json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return audit, false, nil
	}
	if err != nil {
		return audit, false, err
	}
	f := os.NewFile(uintptr(fd), "factory-selection-audit")
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return audit, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > maxStateSize {
		return audit, false, errors.New("invalid factory-selection audit")
	}
	d := json.NewDecoder(io.LimitReader(f, maxStateSize+1))
	d.DisallowUnknownFields()
	if err := d.Decode(&audit); err != nil {
		return audit, false, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return audit, false, errors.New("trailing factory-selection audit data")
	}
	if audit.Request.Action != action || audit.Request.validate() != nil || audit.Failed.ID != audit.Request.Operation || audit.Failed.Previous != audit.Request.Selected || audit.Failed.Generation != audit.Request.Generation || audit.Failed.Phase != "blocked" || !software.ValidID(audit.Factory) {
		return audit, false, errors.New("invalid factory-selection audit identity")
	}
	return audit, true, nil
}
func (r *FileRepository) writeFactorySelectionAudit(a factorySelectionAudit) error {
	if err := r.check(); err != nil {
		return err
	}
	old, exists, err := r.readFactorySelectionAudit(a.Request.Action)
	if err != nil {
		return err
	}
	if exists {
		if old != a {
			return ErrConflict
		}
		return nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if len(b) > maxStateSize {
		return errors.New("factory-selection audit too large")
	}
	f, err := os.CreateTemp(r.root, ".factory-selection-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, werr := f.Write(b)
	if err := errors.Join(werr, f.Sync(), f.Close()); err != nil {
		return err
	}
	// Link publishes without overwriting a conflicting action. The inode is
	// already durable.
	if err := os.Link(f.Name(), filepath.Join(r.root, "factory-selection-"+a.Request.Action+".json")); err != nil {
		return err
	}
	if err := os.Remove(f.Name()); err != nil {
		return err
	}
	return r.dir.Sync()
}
