package kitlease

import (
	"context"
	"crypto/subtle"
	"time"

	contract "github.com/DeanoC/FogCast/kitlease"
)

func (m *Manager) maintenanceActiveLocked() bool {
	return m.maintenance != nil && m.maintenance.State != "completed"
}
func matchesCredential(a, b string) bool {
	return a != "" && b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func (m *Manager) MaintenanceStatus() contract.MaintenanceStatus {
	status, _ := m.MaintenanceStatusChecked()
	return status
}

// MaintenanceStatusChecked distinguishes an absent reservation from startup
// that has not read the persisted authority yet. Clients must retain their
// journals when absence cannot be established.
func (m *Manager) MaintenanceStatusChecked() (contract.MaintenanceStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.maintenanceStore != nil && !m.maintenanceLoaded {
		return contract.MaintenanceStatus{State: "unavailable", Reason: "maintenance startup reconciliation is pending"}, ErrBlocked
	}
	if m.maintenanceFault {
		return contract.MaintenanceStatus{State: "unavailable", Reason: "maintenance reservation unavailable; operator recovery required"}, ErrBlocked
	}
	if m.maintenance == nil {
		return contract.MaintenanceStatus{State: "none"}, nil
	}
	s := m.maintenance.MaintenanceStatus
	if m.status.State == "revoking" && s.State != "completed" {
		s.State = "preparing"
		return s, nil
	}
	if s.State == "completed" {
		s.State = "none"
	}
	return s, nil
}
func (m *Manager) maintenanceGrantLocked() contract.MaintenanceGrant {
	return contract.MaintenanceGrant{Status: m.maintenance.MaintenanceStatus, Token: m.maintenance.Token}
}
func (m *Manager) maintenanceAdmissionLocked() {
	r := m.maintenance
	state := "maintenance"
	if r.State == "blocked" {
		state = "recovery-required"
	}
	m.status = Status{State: state, Generation: r.Generation, Owner: r.Owner, Purpose: r.Purpose, Reason: r.Reason}
}
func (m *Manager) persistMaintenanceLocked(r maintenanceRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	expected := ""
	if m.maintenance != nil {
		expected = m.maintenance.Generation
	}
	if err := m.maintenanceStore.save(ctx, r, expected); err != nil {
		return err
	}
	m.maintenance = &r
	return nil
}

// StartMaintenance exchanges an owned execution grant for durable exclusion.
// The returned preparing reservation is not permission to replace software;
// callers wait for ready. Retries preserve identity and credentials.
func (m *Manager) StartMaintenance(token string, r contract.MaintenanceStartRequest) (contract.MaintenanceGrant, error) {
	if !valid(ClaimRequest{RequestID: r.RequestID, Owner: "maintenance", Purpose: r.Purpose}) || !hexIdentity(r.RequestID, len(r.RequestID)) {
		return contract.MaintenanceGrant{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed || m.maintenanceStore == nil || m.maintenanceFault {
		return contract.MaintenanceGrant{}, ErrBlocked
	}
	if m.maintenance != nil && m.maintenance.OperationID == r.RequestID {
		if m.maintenance.Purpose != r.Purpose {
			return contract.MaintenanceGrant{}, ErrInvalid
		}
		if m.maintenance.State == "completed" || !matchesCredential(token, m.maintenance.StartToken) && !matchesCredential(token, m.maintenance.Token) {
			return contract.MaintenanceGrant{}, ErrLease
		}
		return m.maintenanceGrantLocked(), nil
	}
	if m.executionFence {
		return contract.MaintenanceGrant{}, ErrBlocked
	}
	if m.maintenanceActiveLocked() {
		return contract.MaintenanceGrant{}, ErrBusy
	}
	if m.queuedMaintenance != nil {
		return contract.MaintenanceGrant{}, ErrBusy
	}
	if m.status.State != "held" || !m.ownsLocked(token) {
		return contract.MaintenanceGrant{}, ErrLease
	}
	if m.claim.Owner == contract.HostlessOwner {
		return contract.MaintenanceGrant{}, ErrLease
	}
	record := maintenanceRecord{Schema: 1, MaintenanceStatus: contract.MaintenanceStatus{State: "preparing", OperationID: r.RequestID, Generation: secret(), Owner: m.claim.Owner, Purpose: r.Purpose, BootID: m.maintenanceStore.bootID}, Token: secret(), StartToken: token}
	if err := m.persistMaintenanceLocked(record); err != nil {
		// An fsync failure can leave a marker despite an unsuccessful response.
		// Do not return to admission until restart reconciles the on-target bytes.
		m.maintenanceFault = true
		m.revokeLocked("maintenance reservation persistence failed")
		return contract.MaintenanceGrant{}, ErrBlocked
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.retired[m.claim.RequestID] = true
	m.pending = nil
	m.pendingReady = ""
	m.takeover = nil
	m.maintenanceAdmissionLocked()
	m.runMaintenanceCleanupLocked(false)
	return m.maintenanceGrantLocked(), nil
}

// EndMaintenance requires the exact reservation credential and generation.
// It checks idle again before admitting anyone; failure keeps exclusion.
func (m *Manager) EndMaintenance(token string, r contract.MaintenanceEndRequest) (contract.MaintenanceStatus, error) {
	if !hexIdentity(r.ExpectedGeneration, 64) || !valid(ClaimRequest{RequestID: r.OperationID, Owner: "maintenance", Purpose: "finish"}) || !hexIdentity(r.OperationID, len(r.OperationID)) {
		return contract.MaintenanceStatus{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.maintenanceFault || m.maintenanceStore == nil {
		return contract.MaintenanceStatus{}, ErrBlocked
	}
	record := m.maintenance
	if record == nil || record.OperationID != r.OperationID || record.Generation != r.ExpectedGeneration || !matchesCredential(token, record.Token) {
		return contract.MaintenanceStatus{}, ErrLease
	}
	if record.State == "completed" {
		s := record.MaintenanceStatus
		s.State = "none"
		return s, nil
	}
	if record.State == "finishing" {
		return record.MaintenanceStatus, nil
	}
	// Startup reconciliation is still running even if disk says ready.
	if m.status.State == "revoking" || record.State == "preparing" {
		return record.MaintenanceStatus, ErrBusy
	}
	next := *record
	next.State = "finishing"
	next.Reason = ""
	if err := m.persistMaintenanceLocked(next); err != nil {
		return record.MaintenanceStatus, ErrBlocked
	}
	m.maintenanceAdmissionLocked()
	m.runMaintenanceCleanupLocked(true)
	return m.maintenance.MaintenanceStatus, nil
}

func (m *Manager) runMaintenanceCleanupLocked(finishing bool) {
	drained := m.drained
	if m.refs == 0 {
		drained = make(chan struct{})
		close(drained)
	}
	m.finished = make(chan struct{})
	finished := m.finished
	generation := m.maintenance.Generation
	go func() {
		defer close(finished)
		<-drained
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var err error
		if m.cleanup == nil {
			err = ErrBlocked
		} else {
			err = m.cleanup(ctx)
		}
		cancel()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.maintenance == nil || m.maintenance.Generation != generation {
			return
		}
		next := *m.maintenance
		if err != nil || m.closed {
			next.State = "blocked"
			next.Reason = "maintenance cleanup failed or agent shutting down; explicit finish required"
		} else if finishing {
			next.State = "completed"
			next.Reason = ""
		} else {
			next.State = "ready"
			next.Reason = ""
		}
		if saveErr := m.persistMaintenanceLocked(next); saveErr != nil {
			// Keep memory and persisted exclusion active on ambiguous I/O failure.
			m.maintenance.State = "blocked"
			m.maintenance.Reason = "maintenance state could not be persisted; explicit finish required"
			m.maintenanceAdmissionLocked()
			return
		}
		if next.State == "completed" {
			m.claim = ClaimRequest{}
			m.token = ""
			m.status = Status{State: "free", Generation: secret()}
		} else {
			m.maintenanceAdmissionLocked()
		}
	}()
}

// A new process cannot infer whether a software transaction completed. Keep a
// surviving reservation blocked after startup cleanup; its exact owner finishes
// explicitly. A marker from a different boot can be retired only after idle.
func (m *Manager) reconcileMaintenanceLocked(cleanupErr error) {
	next := *m.maintenance
	if next.BootID != m.maintenanceStore.bootID && cleanupErr == nil && !m.closed {
		next.State = "completed"
		next.Reason = ""
	} else {
		next.State = "blocked"
		next.Reason = "agent restarted during maintenance; explicit finish required"
		if cleanupErr != nil {
			next.Reason = "maintenance startup cleanup failed; explicit finish required"
		}
	}
	// Persist outside the expired cleanup context with a bounded lock attempt.
	if err := m.persistMaintenanceLocked(next); err != nil {
		m.maintenanceFault = true
		m.status.State = "blocked"
		m.status.Reason = "maintenance reconciliation could not be persisted"
		return
	}
	if next.State == "completed" {
		m.status = Status{State: "free", Generation: secret()}
	} else {
		m.maintenanceAdmissionLocked()
	}
}

// ResumeMaintenance explicitly rechecks idle after a service restart or a
// failed preparation. It keeps the same reservation, generation and token.
func (m *Manager) ResumeMaintenance(token string, r contract.MaintenanceResumeRequest) (contract.MaintenanceStatus, error) {
	if !hexIdentity(r.ExpectedGeneration, 64) || !valid(ClaimRequest{RequestID: r.OperationID, Owner: "maintenance", Purpose: "resume"}) || !hexIdentity(r.OperationID, len(r.OperationID)) {
		return contract.MaintenanceStatus{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.maintenanceFault || m.maintenanceStore == nil {
		return contract.MaintenanceStatus{}, ErrBlocked
	}
	record := m.maintenance
	if record == nil || record.OperationID != r.OperationID || record.Generation != r.ExpectedGeneration || record.State == "completed" || !matchesCredential(token, record.Token) {
		return contract.MaintenanceStatus{}, ErrLease
	}
	if m.status.State == "revoking" || record.State == "finishing" {
		return record.MaintenanceStatus, ErrBusy
	}
	if record.State == "ready" || record.State == "preparing" {
		return record.MaintenanceStatus, nil
	}
	next := *record
	next.State = "preparing"
	next.Reason = ""
	if err := m.persistMaintenanceLocked(next); err != nil {
		return record.MaintenanceStatus, ErrBlocked
	}
	m.maintenanceAdmissionLocked()
	m.runMaintenanceCleanupLocked(false)
	return m.maintenance.MaintenanceStatus, nil
}
