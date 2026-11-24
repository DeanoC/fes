package input

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
)

var errLocalRecoveryPending = errors.New("local input recovery awaits old core retirement")

// ObserveLocalStartupRecovery samples once on first use, before admitting
// local input. An already-active core is an old authority until a fresh observation proves it
// idle or replaced. This does not stop the core or prevent menu/Stop actions.
func (c *TargetController) ObserveLocalStartupRecovery(ctx context.Context) (bool, error) {
	if c == nil || ctx == nil {
		return false, errLocalRecoveryPending
	}
	if _, err := c.LocalAgentInstance(); err != nil {
		return false, err
	}
	if err := lockInputMutex(ctx, &c.localRecoveryMu); err != nil {
		return false, err
	}
	defer c.localRecoveryMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c.mu.Lock()
	observe, closed := c.observe, c.closed
	c.mu.Unlock()
	if closed {
		return false, errLocalRecoveryPending
	}
	if c.localRecoveryReady {
		return true, nil
	}
	if observe == nil {
		return false, errLocalRecoveryPending
	}
	probe, cancel := context.WithTimeout(ctx, localCoreObserveTimeout)
	obs, err := observe(probe)
	if err == nil {
		err = probe.Err()
	}
	cancel()
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	closed = c.closed
	c.mu.Unlock()
	if closed || ctx.Err() != nil {
		return false, errLocalRecoveryPending
	}
	if !validObservedMatrixInstance(obs.RuntimeInstance) {
		return false, errLocalRecoveryPending
	}
	idle := obs.IdleConfirmed && !obs.Active && obs.PackageID == "" && obs.Generation == 0
	active := obs.Active && !obs.IdleConfirmed && obs.Generation != 0 && (obs.PackageID == "" || localIdentity.MatchString(obs.PackageID))
	if !idle && !active {
		return false, errLocalRecoveryPending
	}
	if !c.localRecoverySeen {
		c.localRecoverySeen = true
		if idle {
			c.localRecoveryReady = true
			return true, nil
		}
		c.localRecoveryStart = MatrixBinding{RuntimeInstance: obs.RuntimeInstance, PackageID: obs.PackageID, Generation: obs.Generation}
		return false, errLocalRecoveryPending
	}
	old := c.localRecoveryStart
	if idle || active && (obs.RuntimeInstance != old.RuntimeInstance || obs.PackageID != old.PackageID || obs.Generation != old.Generation) {
		c.localRecoveryReady = true
		return true, nil
	}
	return false, errLocalRecoveryPending
}

// localRecoveryBudget bounds one status sample without starting a background
// operation; a failed sample never grants input.
const localRecoveryBudget = 500 * time.Millisecond

// localRecoveryReply must run only from the single serialized local reader.
// It does not grant raw keyboard input; it reports whether a predecessor's
// reader drained or a reconstructed agent's startup core retired.
func (c *TargetController) localRecoveryReply(ctx context.Context, req localhid.Request, sourceBusy bool) (localhid.Reply, error) {
	if ctx == nil || req.Op != "recover" || req.Version != 2 || req.Request == 0 || !validObservedMatrixInstance(req.AgentInstance) || !localhid.ValidRecoveryIdentity(req.TargetID) || !localhid.ValidRecoveryIdentity(req.BootID) {
		return localhid.Reply{}, localhid.ErrRequest
	}
	instance, err := c.LocalAgentInstance()
	if err != nil {
		return localhid.Reply{}, err
	}
	if err := lockInputMutex(ctx, &c.localRecoveryMu); err != nil {
		return localhid.Reply{}, err
	}
	targetID, bootID := c.localTargetID, c.localBootID
	c.localRecoveryMu.Unlock()
	if targetID == "" || bootID == "" || req.TargetID != targetID || req.BootID != bootID {
		return localhid.Reply{}, errLocalRecoveryPending
	}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: instance, TargetID: targetID, BootID: bootID, Status: "unavailable"}
	if sourceBusy {
		return reply, nil
	}
	check, cancel := context.WithTimeout(ctx, localRecoveryBudget)
	ready, err := c.ObserveLocalStartupRecovery(check)
	cancel()
	if err != nil || !ready {
		return reply, nil
	}
	if req.AgentInstance == instance {
		reply.Status = "reader-drained"
	} else {
		reply.Status = "startup-retired"
	}
	return reply, nil
}
