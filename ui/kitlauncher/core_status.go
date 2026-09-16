package kitlauncher

import (
	"github.com/DeanoC/FogCast/hostclient"
	"strings"
)

// ApplyCoreStatuses publishes a fresh host read model without putting
// target/package state into the durable catalog snapshot.
func (m *Model) ApplyCoreStatuses(statuses []hostclient.CoreAvailability) {
	if m == nil {
		return
	}
	next := append([]hostclient.CoreAvailability(nil), statuses...)
	changed := m.CoreStatusUnavailable || !coreStatusesEqual(m.CoreStatuses, next)
	m.CoreStatuses = next
	m.CoreStatusUnavailable = false
	if changed {
		m.CoreStatusRevision++
	}
}

// ClearCoreStatuses removes the last host observation. unavailable distinguishes
// a failed read from a successful empty core inventory.
func (m *Model) ClearCoreStatuses(unavailable bool) {
	if m == nil {
		return
	}
	changed := len(m.CoreStatuses) != 0 || m.CoreStatusUnavailable != unavailable
	m.CoreStatuses = nil
	m.CoreStatusUnavailable = unavailable
	if changed {
		m.CoreStatusRevision++
	}
}

// CoreStatusForGame returns the exact selected-package status for one catalog
// game. The slice is intentionally small and preserves API entry order.
func (m Model) CoreStatusForGame(gameID string) (hostclient.CoreAvailability, bool) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return hostclient.CoreAvailability{}, false
	}
	for _, status := range m.CoreStatuses {
		if strings.TrimSpace(status.GameID) == gameID {
			return status, true
		}
	}
	return hostclient.CoreAvailability{}, false
}

func coreStatusesEqual(a, b []hostclient.CoreAvailability) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
