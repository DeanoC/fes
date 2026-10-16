package kitlease

import contract "github.com/DeanoC/FogCast/kitlease"

// QueueIdleMaintenance gives an already durable software intent priority at
// the next unowned idle boundary. It neither revokes nor stops the current
// owner: that owner's renewals and admitted operations continue normally.
// The caller owns intent persistence and must reinstall it after agent restart
// before serving new claims. This queue is not an execution grant.
func (m *Manager) QueueIdleMaintenance(request ClaimRequest, credential string) error {
	if !valid(request) || !hexIdentity(request.RequestID, len(request.RequestID)) || !hexIdentity(credential, 64) {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.maintenanceStore == nil || m.maintenanceFault {
		return ErrBlocked
	}
	if !m.maintenanceLoaded {
		return ErrBusy
	}
	if m.queuedMaintenance != nil {
		if *m.queuedMaintenance == request {
			if !matchesCredential(credential, m.queuedMaintenanceCredential) {
				return ErrLease
			}
			return nil
		}
		if m.queuedMaintenance.RequestID == request.RequestID {
			return ErrInvalid
		}
		return ErrBusy
	}
	if m.maintenanceActiveLocked() || m.pending != nil {
		return ErrBusy
	}
	if m.maintenance != nil && m.maintenance.OperationID == request.RequestID {
		// A completed operation cannot be replayed into a fresh reservation.
		return ErrLease
	}
	if m.status.State == "blocked" || m.status.State == "recovery-required" {
		return ErrBlocked
	}
	copy := request
	m.queuedMaintenance = &copy
	m.queuedMaintenanceCredential = credential
	return nil
}

// QueueHeldSuccessor installs the next idle intent while the caller still holds
// the active maintenance reservation. The reservation stays in force until
// EndMaintenance. A caller without that reservation token cannot queue across
// it, so diagnostic maintenance keeps its existing exclusion.
func (m *Manager) QueueHeldSuccessor(request ClaimRequest, credential, heldToken string) error {
	if !valid(request) || !hexIdentity(request.RequestID, len(request.RequestID)) || !hexIdentity(credential, 64) || !hexIdentity(heldToken, 64) {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.maintenanceStore == nil || m.maintenanceFault {
		return ErrBlocked
	}
	if !m.maintenanceLoaded {
		return ErrBusy
	}
	if !m.maintenanceActiveLocked() || !matchesCredential(heldToken, m.maintenance.Token) {
		return ErrLease
	}
	if m.maintenance.State == "blocked" || m.status.State == "blocked" || m.status.State == "recovery-required" {
		return ErrBlocked
	}
	if request.RequestID == m.maintenance.OperationID {
		return ErrInvalid
	}
	if m.pending != nil {
		return ErrBusy
	}
	if m.queuedMaintenance != nil {
		if *m.queuedMaintenance == request {
			if !matchesCredential(credential, m.queuedMaintenanceCredential) {
				return ErrLease
			}
			return nil
		}
		if m.queuedMaintenance.RequestID == request.RequestID {
			return ErrInvalid
		}
		return ErrBusy
	}
	copy := request
	m.queuedMaintenance = &copy
	m.queuedMaintenanceCredential = credential
	return nil
}

// CancelQueuedMaintenance removes only the matching pending intent. Once it
// has become maintenance, ResumeMaintenance/EndMaintenance govern recovery;
// cancellation must not reopen execution during a service replacement.
func (m *Manager) CancelQueuedMaintenance(requestID string) error {
	if !valid(ClaimRequest{RequestID: requestID, Owner: "maintenance", Purpose: "cancel"}) || !hexIdentity(requestID, len(requestID)) {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrBlocked
	}
	if m.queuedMaintenance == nil {
		if m.maintenanceActiveLocked() && m.maintenance.OperationID == requestID {
			return ErrBusy
		}
		return nil
	}
	if m.queuedMaintenance.RequestID != requestID {
		return ErrLease
	}
	m.queuedMaintenance = nil
	m.queuedMaintenanceCredential = ""
	return nil
}

// StartQueuedMaintenance atomically converts a settled free kit into the
// existing maintenance reservation. It never claims, takes over or releases
// another owner's execution. Preparing is not permission to replace software;
// the caller waits for ready after the existing idle cleanup confirms it.
func (m *Manager) StartQueuedMaintenance(request ClaimRequest, credential string) (contract.MaintenanceGrant, error) {
	if !valid(request) || !hexIdentity(request.RequestID, len(request.RequestID)) || !hexIdentity(credential, 64) {
		return contract.MaintenanceGrant{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.maintenanceStore == nil || m.maintenanceFault {
		return contract.MaintenanceGrant{}, ErrBlocked
	}
	if !m.maintenanceLoaded {
		return contract.MaintenanceGrant{}, ErrBusy
	}
	if m.maintenance != nil && m.maintenance.OperationID == request.RequestID {
		if m.maintenance.Owner != request.Owner || m.maintenance.Purpose != request.Purpose {
			return contract.MaintenanceGrant{}, ErrInvalid
		}
		if m.maintenance.State == "completed" || !matchesCredential(credential, m.maintenance.StartToken) {
			return contract.MaintenanceGrant{}, ErrLease
		}
		return m.maintenanceGrantLocked(), nil
	}
	if m.queuedMaintenance == nil || *m.queuedMaintenance != request || !matchesCredential(credential, m.queuedMaintenanceCredential) {
		return contract.MaintenanceGrant{}, ErrLease
	}
	if m.maintenanceActiveLocked() || m.status.State != "free" || m.refs != 0 {
		return contract.MaintenanceGrant{}, ErrBusy
	}
	record := maintenanceRecord{Schema: 1, MaintenanceStatus: contract.MaintenanceStatus{
		State: "preparing", OperationID: request.RequestID, Generation: secret(),
		Owner: request.Owner, Purpose: request.Purpose, BootID: m.maintenanceStore.bootID,
	}, Token: secret(), StartToken: credential}
	if err := m.persistMaintenanceLocked(record); err != nil {
		// An ambiguous fsync may have published the marker. Startup must
		// reconcile it before any execution can be admitted again.
		m.maintenanceFault = true
		m.status = Status{State: "blocked", Generation: secret(), Reason: "queued maintenance reservation could not be persisted"}
		return contract.MaintenanceGrant{}, ErrBlocked
	}
	m.queuedMaintenance = nil
	m.queuedMaintenanceCredential = ""
	m.pending = nil
	m.pendingReady = ""
	m.takeover = nil
	m.maintenanceAdmissionLocked()
	m.runMaintenanceCleanupLocked(false)
	return m.maintenanceGrantLocked(), nil
}
