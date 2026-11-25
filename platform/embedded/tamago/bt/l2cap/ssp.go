// SPDX-License-Identifier: GPL-3.0-or-later
package l2cap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
)

// Manager integrates HCI events, fail-closed SSP pairing, BondStore, and
// zero-allocation L2CAP HID Control/Interrupt routing for up to 4 concurrent
// Classic Bluetooth controllers.
type Manager struct {
	tr    *hci.Transport
	bonds *BondStore
	now   func() time.Time

	conns     [maxConnections]aclConn
	txScratch [maxL2CAPPayload + 4]byte

	pairingActive        bool
	pairingUntil         time.Time
	pairingGeneration    uint64
	connecting           bool
	connectAddr          BDAddr
	connectGeneration    uint64
	inquiryActive        bool
	inquiryRetryPending  bool
	inquiryRetryAt       time.Time
	cancelInquiryPending bool
	cancelConnPending    bool
	cancelConnAddr       BDAddr
	cancelRetryAt        time.Time
	pairingAccepted      [maxConnections]BDAddr
	pairingAcceptedGen   [maxConnections]uint64
	pairingAcceptedUsed  [maxConnections]bool

	OnChannelOpen  func(handle uint16, addr BDAddr, name string, kind controllers.Kind, psm uint16)
	OnChannelClose func(handle uint16, addr BDAddr, psm uint16)
	OnDisconnect   func(handle uint16, addr BDAddr)
	OnNameResolved func(handle uint16, addr BDAddr, name string, kind controllers.Kind)
	OnHIDControl   func(handle uint16, payload []byte)
	OnHIDInterrupt func(handle uint16, payload []byte)
}

// NewManager creates an L2CAP + SSP manager on top of an initialized hci.Transport.
func NewManager(tr *hci.Transport, bonds *BondStore, now func() time.Time) (*Manager, error) {
	if tr == nil {
		return nil, errors.New("bt/l2cap: nil HCI transport")
	}
	if bonds == nil {
		bonds = NewBondStore()
	}
	if now == nil {
		now = time.Now
	}
	return &Manager{tr: tr, bonds: bonds, now: now}, nil
}

// Bonds returns the underlying BondStore for persistence checks.
func (m *Manager) Bonds() *BondStore { return m.bonds }

// PairingActive reports whether SSP pairing window is currently open.
func (m *Manager) PairingActive() bool { return m.pairingActive }

// PairingRemaining returns the remaining duration of an active SSP pairing
// window, or 0 if inactive or expired.
func (m *Manager) PairingRemaining() time.Duration {
	if m == nil || !m.pairingActive {
		return 0
	}
	rem := m.pairingUntil.Sub(m.now())
	if rem <= 0 {
		return 0
	}
	return rem
}

// SetPairingMode opens or closes a bounded SSP pairing window. Outside pairing
// mode, unknown devices and SSP User Confirmation requests are rejected fail-closed.
func (m *Manager) SetPairingMode(enabled bool, duration time.Duration) error {
	pendingConnecting, pendingAddr := m.connecting, m.connectAddr
	hasCancelTarget := pendingConnecting || m.cancelConnPending
	cancelTarget := pendingAddr
	if !pendingConnecting {
		cancelTarget = m.cancelConnAddr
	}
	if enabled {
		if duration <= 0 {
			duration = 30 * time.Second
		}
		if m.pairingActive {
			m.pairingUntil = m.now().Add(duration)
			m.cancelInquiryPending = false
			if !m.inquiryActive && !m.connecting {
				if err := m.tr.StartInquiry(8, 0); err != nil {
					m.inquiryRetryPending = true
					m.inquiryRetryAt = m.now().Add(nameRetryBackoff)
					return err
				}
				m.inquiryActive = true
			}
			m.inquiryRetryPending = false
			m.inquiryRetryAt = time.Time{}
			return nil
		}
		if hasCancelTarget {
			if err := m.tr.CreateConnectionCancel(cancelTarget); err != nil {
				m.connecting = false
				m.connectAddr = BDAddr{}
				m.connectGeneration = 0
				m.inquiryRetryPending = false
				m.inquiryRetryAt = time.Time{}
				m.cancelConnPending = true
				m.cancelConnAddr = cancelTarget
				m.cancelRetryAt = m.now().Add(nameRetryBackoff)
				return err
			}
			m.cancelConnPending = false
			m.cancelConnAddr = BDAddr{}
		}
		m.connecting = false
		m.connectAddr = BDAddr{}
		m.connectGeneration = 0
		if err := m.tr.WriteScanEnable(hci.ScanInquiryAndPage); err != nil {
			return err
		}
		// 8 * 1.28s = 10.24s inquiry burst (restarted if still pairing)
		if !m.inquiryActive {
			if err := m.tr.StartInquiry(8, 0); err != nil {
				_ = m.tr.WriteScanEnable(hci.ScanPageOnly)
				return err
			}
			m.inquiryActive = true
		}
		m.pairingGeneration++
		if m.pairingGeneration == 0 {
			m.pairingGeneration++
		}
		for i := range m.pairingAcceptedUsed {
			m.pairingAcceptedUsed[i] = false
			m.pairingAccepted[i] = BDAddr{}
			m.pairingAcceptedGen[i] = 0
		}
		m.pairingActive = true
		m.pairingUntil = m.now().Add(duration)
		m.inquiryRetryPending = false
		m.inquiryRetryAt = time.Time{}
		m.cancelInquiryPending = false
		return nil
	}
	m.pairingActive = false
	m.pairingUntil = time.Time{}
	m.connecting = false
	m.connectAddr = BDAddr{}
	m.connectGeneration = 0
	m.inquiryRetryPending = false
	m.inquiryRetryAt = time.Time{}

	var firstErr error
	if hasCancelTarget {
		if err := m.tr.CreateConnectionCancel(cancelTarget); err != nil {
			m.cancelConnPending = true
			m.cancelConnAddr = cancelTarget
			m.cancelRetryAt = m.now().Add(nameRetryBackoff)
			firstErr = err
		} else {
			m.cancelConnPending = false
			m.cancelConnAddr = BDAddr{}
		}
	}
	if m.inquiryActive || m.cancelInquiryPending {
		if err := m.tr.CancelInquiry(); err != nil {
			m.cancelInquiryPending = true
			m.cancelRetryAt = m.now().Add(nameRetryBackoff)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			m.inquiryActive = false
			m.cancelInquiryPending = false
		}
	}
	if err := m.tr.WriteScanEnable(hci.ScanPageOnly); err != nil && firstErr == nil {
		firstErr = err
	}
	for i := range m.conns {
		conn := &m.conns[i]
		if conn.active && conn.pairingConn {
			if _, bonded := m.bonds.Lookup(conn.addr); !bonded {
				m.failConnSecurity(conn)
			}
		}
	}
	return firstErr
}

func (m *Manager) recordPairingAccepted(addr BDAddr) {
	for i := range m.pairingAcceptedUsed {
		if m.pairingAcceptedUsed[i] && m.pairingAccepted[i] == addr {
			m.pairingAcceptedGen[i] = m.pairingGeneration
			return
		}
	}
	for i := range m.pairingAcceptedUsed {
		if !m.pairingAcceptedUsed[i] {
			m.pairingAcceptedUsed[i] = true
			m.pairingAccepted[i] = addr
			m.pairingAcceptedGen[i] = m.pairingGeneration
			return
		}
	}
}

func (m *Manager) consumePairingAccepted(addr BDAddr) (uint64, bool) {
	for i := range m.pairingAcceptedUsed {
		if m.pairingAcceptedUsed[i] && m.pairingAccepted[i] == addr {
			generation := m.pairingAcceptedGen[i]
			m.pairingAcceptedUsed[i] = false
			m.pairingAccepted[i] = BDAddr{}
			m.pairingAcceptedGen[i] = 0
			return generation, true
		}
	}
	return 0, false
}

const (
	maxNameRetries   uint8 = 5
	maxSecRetries    uint8 = 3
	nameRetryBackoff       = 250 * time.Millisecond
)

func (m *Manager) requestAuthentication(conn *aclConn) {
	if conn == nil || !conn.active || conn.secFailed || conn.authenticated {
		return
	}
	conn.authRequested = true
	conn.secRetryPending = false
	conn.secRetryAt = time.Time{}
	if err := m.tr.AuthenticationRequested(conn.handle); err != nil {
		conn.authRequested = false
		if conn.secRetries < maxSecRetries {
			conn.secRetries++
			conn.secRetryPending = true
			conn.secRetryAt = m.now().Add(nameRetryBackoff)
		} else {
			m.failConnSecurity(conn)
		}
	}
}

func (m *Manager) requestEncryption(conn *aclConn) {
	if conn == nil || !conn.active || conn.secFailed || !conn.authenticated || conn.encrypted {
		return
	}
	conn.encRequested = true
	conn.secRetryPending = false
	conn.secRetryAt = time.Time{}
	if err := m.tr.SetConnectionEncryption(conn.handle, true); err != nil {
		conn.encRequested = false
		if conn.secRetries < maxSecRetries {
			conn.secRetries++
			conn.secRetryPending = true
			conn.secRetryAt = m.now().Add(nameRetryBackoff)
		} else {
			m.failConnSecurity(conn)
		}
	}
}

func (m *Manager) onConnSecured(conn *aclConn) {
	if conn == nil || !conn.active || !conn.secured() {
		return
	}
	if conn.pairingConn && !m.pairingActive {
		if _, bonded := m.bonds.Lookup(conn.addr); !bonded {
			m.failConnSecurity(conn)
			conn.pairingConn = false
			return
		}
	}
	for i := range conn.chans {
		ch := &conn.chans[i]
		if ch.inUse && ch.awaitingAuth {
			ch.awaitingAuth = false
			ch.pendingAuthRsp = false
			ch.pendingConnRsp = true
			ch.pendingCfg = true
			m.pumpChannelSignaling(conn, ch)
		}
	}
	if conn.hostInitiated && conn.findByPSM(PSMHIDControl) == nil {
		if err := m.OpenChannel(conn.handle, PSMHIDControl); err != nil {
			conn.pendingOpenPSM = PSMHIDControl
		}
	}
}

func (m *Manager) disconnectConn(conn *aclConn, reason byte) error {
	if conn == nil || !conn.active {
		return nil
	}
	for i := range conn.chans {
		ch := &conn.chans[i]
		if !ch.inUse {
			continue
		}
		if ch.awaitingAuth {
			var rsp [8]byte
			binary.LittleEndian.PutUint16(rsp[2:4], ch.remoteCID)
			binary.LittleEndian.PutUint16(rsp[4:6], 0x0003) // Connection refused - security block
			m.sendOrQueueSig(conn, sigConnRsp, ch.pendingConnRspID, rsp[:])
			*ch = l2capChannel{}
			continue
		}
		if ch.open && (ch.psm == PSMHIDControl || ch.psm == PSMHIDInterrupt) {
			psm := ch.psm
			*ch = l2capChannel{}
			if m.OnChannelClose != nil {
				m.OnChannelClose(conn.handle, conn.addr, psm)
			}
		}
	}
	conn.secFailed = true
	conn.authenticated = false
	conn.encrypted = false
	conn.authRequested = false
	conn.encRequested = false
	conn.secRetryPending = false
	conn.secRetryAt = time.Time{}
	conn.pendingOpenPSM = 0
	conn.pendingOpenSDP = false
	conn.sdpSendPending = false
	err := m.tr.Disconnect(conn.handle, reason)
	if err != nil {
		conn.disconnectPending = true
		conn.disconnectReason = reason
		conn.disconnectRetryAt = m.now().Add(nameRetryBackoff)
		return err
	}
	conn.disconnectPending = false
	conn.disconnectRetryAt = time.Time{}
	return nil
}

func (m *Manager) failConnSecurity(conn *aclConn) {
	_ = m.disconnectConn(conn, 0x05)
}

// DisconnectAddr cancels a pending outgoing connection or disconnects any active
// ACL link to addr with Remote User Terminated Connection (0x13), closing its
// HID channels immediately so downstream input neutralizes at once.
func (m *Manager) DisconnectAddr(addr BDAddr) (bool, error) {
	if m == nil {
		return false, nil
	}
	var matched bool
	var firstErr error
	if m.connecting && m.connectAddr == addr {
		matched = true
		m.connecting = false
		m.connectAddr = BDAddr{}
		m.connectGeneration = 0
		if err := m.tr.CreateConnectionCancel(addr); err != nil {
			m.cancelConnPending = true
			m.cancelConnAddr = addr
			m.cancelRetryAt = m.now().Add(nameRetryBackoff)
			firstErr = err
		} else if m.cancelConnPending && m.cancelConnAddr == addr {
			m.cancelConnPending = false
			m.cancelConnAddr = BDAddr{}
		}
	}
	if conn := m.findConnByAddr(addr); conn != nil {
		matched = true
		if err := m.disconnectConn(conn, 0x13); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return matched, firstErr
}

func (m *Manager) requestRemoteName(conn *aclConn) {
	if conn == nil {
		return
	}
	conn.namePending = true
	conn.nameRetryAt = time.Time{}
	if err := m.tr.RemoteNameRequest(conn.addr); err != nil {
		if conn.nameRetries < maxNameRetries {
			conn.nameRetries++
			conn.nameRetryAt = m.now().Add(nameRetryBackoff)
		} else {
			conn.namePending = false
			if conn.kind == controllers.KindUnknown {
				m.startSDPQuery(conn)
			}
		}
	}
}

func (m *Manager) pumpCancellations() {
	retrying := false
	if m.cancelConnPending {
		if err := m.tr.CreateConnectionCancel(m.cancelConnAddr); err == nil {
			m.cancelConnPending = false
			m.cancelConnAddr = BDAddr{}
		} else {
			retrying = true
		}
	}
	if m.cancelInquiryPending {
		if err := m.tr.CancelInquiry(); err == nil {
			m.inquiryActive = false
			m.cancelInquiryPending = false
		} else {
			retrying = true
		}
	}
	if retrying {
		m.cancelRetryAt = m.now().Add(nameRetryBackoff)
	} else {
		m.cancelRetryAt = time.Time{}
	}
}

// Service pumps HCI events & ACL packets, enforces pairing mode expiry, and
// advances L2CAP channels with zero steady-state allocations.
func (m *Manager) Service() error {
	if m.pairingActive && !m.now().Before(m.pairingUntil) {
		_ = m.SetPairingMode(false, 0)
	}
	if err := m.tr.Service(m.handleEvent, m.handleACL); err != nil {
		return err
	}
	if (m.cancelConnPending || m.cancelInquiryPending) && (m.cancelRetryAt.IsZero() || !m.now().Before(m.cancelRetryAt)) {
		m.pumpCancellations()
	}
	if m.inquiryRetryPending && (m.inquiryRetryAt.IsZero() || !m.now().Before(m.inquiryRetryAt)) {
		m.resumeInquiryIfPairing()
	}
	for i := range m.conns {
		conn := &m.conns[i]
		if !conn.active {
			continue
		}
		if conn.disconnectPending && (conn.disconnectRetryAt.IsZero() || !m.now().Before(conn.disconnectRetryAt)) {
			if err := m.tr.Disconnect(conn.handle, conn.disconnectReason); err == nil {
				conn.disconnectPending = false
				conn.disconnectRetryAt = time.Time{}
			} else {
				conn.disconnectRetryAt = m.now().Add(nameRetryBackoff)
			}
		}
		if conn.secRetryPending && !conn.secRetryAt.IsZero() && !m.now().Before(conn.secRetryAt) {
			if !conn.authenticated {
				m.requestAuthentication(conn)
			} else if !conn.encrypted {
				m.requestEncryption(conn)
			} else {
				conn.secRetryPending = false
				conn.secRetryAt = time.Time{}
			}
		}
		if conn.namePending && !conn.nameRetryAt.IsZero() && !m.now().Before(conn.nameRetryAt) {
			m.requestRemoteName(conn)
		}
		if conn.pendingOpenPSM != 0 && conn.secured() {
			if err := m.OpenChannel(conn.handle, conn.pendingOpenPSM); err == nil {
				conn.pendingOpenPSM = 0
			}
		}
		if conn.pendingOpenSDP {
			if err := m.OpenChannel(conn.handle, PSMSDP); err == nil {
				conn.pendingOpenSDP = false
			}
		}
		if conn.sdpSendPending {
			m.pumpSDP(conn)
		}
		m.pumpConnSignaling(conn)
		for j := range conn.chans {
			ch := &conn.chans[j]
			if ch.inUse && (ch.pendingAuthRsp || ch.pendingConnRsp || ch.pendingCfg || ch.pendingCfgRsp) {
				m.pumpChannelSignaling(conn, ch)
			}
		}
	}
	return nil
}

func (m *Manager) findConnByHandle(handle uint16) *aclConn {
	h := handle & 0x0fff
	for i := range m.conns {
		if m.conns[i].active && m.conns[i].handle == h {
			return &m.conns[i]
		}
	}
	return nil
}

func (m *Manager) findConnByAddr(addr BDAddr) *aclConn {
	for i := range m.conns {
		if m.conns[i].active && m.conns[i].addr == addr {
			return &m.conns[i]
		}
	}
	return nil
}

func (m *Manager) allocConn(handle uint16, addr BDAddr) *aclConn {
	if c := m.findConnByHandle(handle); c != nil {
		return c
	}
	for i := range m.conns {
		if !m.conns[i].active {
			m.conns[i] = aclConn{
				active:  true,
				handle:  handle & 0x0fff,
				addr:    addr,
				nextCID: firstDynamicCID,
			}
			return &m.conns[i]
		}
	}
	return nil
}

func isControllerPeripheralCoD(cod uint32) bool {
	major := (cod >> 8) & 0x1f
	minor := (cod >> 2) & 0x3f
	// Bits 6:7 of the Peripheral minor field indicate keyboard/pointing support;
	// only controller subtypes without those capabilities are eligible here.
	if major != 0x05 || minor&0x30 != 0 {
		return false
	}
	switch minor & 0x0f {
	case 0x01, 0x02: // Joystick or Gamepad
		return true
	default:
		return false
	}
}

func (m *Manager) resumeInquiryIfPairing() {
	if !m.pairingActive || m.connecting || !m.now().Before(m.pairingUntil) {
		m.inquiryRetryPending = false
		m.inquiryRetryAt = time.Time{}
		return
	}
	if err := m.tr.StartInquiry(8, 0); err != nil {
		m.inquiryRetryPending = true
		m.inquiryRetryAt = m.now().Add(nameRetryBackoff)
		return
	}
	m.inquiryActive = true
	m.cancelInquiryPending = false
	m.inquiryRetryPending = false
	m.inquiryRetryAt = time.Time{}
}

func (m *Manager) handleInquiryRecords(params []byte, codOff, clkOff int) {
	if len(params) < 1 {
		return
	}
	n := int(params[0])
	if n <= 0 || len(params) < 1+n*14 {
		return
	}
	for i := 0; i < n; i++ {
		rec := params[1+i*14 : 1+(i+1)*14]
		var addr BDAddr
		copy(addr[:], rec[0:6])
		cod := uint32(rec[codOff]) | uint32(rec[codOff+1])<<8 | uint32(rec[codOff+2])<<16
		clk := binary.LittleEndian.Uint16(rec[clkOff : clkOff+2])
		m.maybeInitiatePair(addr, cod, rec[6], clk)
	}
}

func (m *Manager) maybeInitiatePair(addr BDAddr, cod uint32, pageScanRep byte, clockOff uint16) {
	if !m.pairingActive || m.connecting || !isControllerPeripheralCoD(cod) {
		return
	}
	if m.findConnByAddr(addr) != nil {
		return
	}
	m.connecting = true
	m.connectAddr = addr
	m.connectGeneration = m.pairingGeneration
	m.inquiryRetryPending = false
	m.inquiryRetryAt = time.Time{}
	if m.tr.CancelInquiry() == nil {
		m.inquiryActive = false
		m.cancelInquiryPending = false
	}
	if err := m.tr.CreateConnection(addr, pageScanRep, clockOff); err != nil {
		m.connecting = false
		m.connectAddr = BDAddr{}
		m.connectGeneration = 0
		m.resumeInquiryIfPairing()
	}
}

func (m *Manager) handleEvent(ev []byte) {
	if len(ev) < 2 {
		return
	}
	code := ev[0]
	params := ev[2:]
	switch code {
	case hci.EventConnectionRequest:
		// [0..5]=BD_ADDR [6..8]=CoD [9]=LinkType (0x01=ACL)
		if len(params) < 10 || params[9] != 0x01 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[0:6])
		cod := uint32(params[6]) | uint32(params[7])<<8 | uint32(params[8])<<16
		_, bonded := m.bonds.Lookup(addr)
		controllerCandidate := m.pairingActive && isControllerPeripheralCoD(cod)
		if bonded || controllerCandidate {
			// Role 0x00: request Master/Central role for deterministic polling slots!
			if controllerCandidate {
				m.recordPairingAccepted(addr)
			}
			_ = m.tr.AcceptConnectionRequest(addr, 0x00)
		} else {
			// Fail-closed: reject unknown or non-controller devices during pairing.
			_ = m.tr.RejectConnectionRequest(addr, 0x0f)
		}

	case hci.EventConnectionComplete:
		// [0]=status [1..2]=handle [3..8]=BD_ADDR [9]=link_type [10]=enc
		if len(params) < 11 || params[9] != 0x01 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[3:9])
		outgoing := m.connecting && m.connectAddr == addr
		outgoingGeneration := m.connectGeneration
		if outgoing {
			m.connecting = false
			m.connectAddr = BDAddr{}
			m.connectGeneration = 0
		}
		canceled := m.cancelConnPending && m.cancelConnAddr == addr
		if canceled {
			m.cancelConnPending = false
			m.cancelConnAddr = BDAddr{}
		}
		acceptedGeneration, accepted := m.consumePairingAccepted(addr)
		fromPairing := accepted || outgoing || canceled
		pairingGeneration := uint64(0)
		if m.pairingActive && outgoing && outgoingGeneration == m.pairingGeneration {
			pairingGeneration = m.pairingGeneration
		}
		if m.pairingActive && accepted && acceptedGeneration == m.pairingGeneration {
			pairingGeneration = m.pairingGeneration
		}
		if params[0] != 0 {
			m.resumeInquiryIfPairing()
			return
		}
		handle := binary.LittleEndian.Uint16(params[1:3]) & 0x0fff
		rec, bonded := m.bonds.Lookup(addr)
		if !bonded && ((!m.pairingActive && fromPairing) || (m.pairingActive && pairingGeneration == 0)) {
			_ = m.tr.Disconnect(handle, 0x13)
			return
		}
		conn := m.allocConn(handle, addr)
		if conn == nil {
			_ = m.tr.Disconnect(handle, 0x13)
			return
		}
		conn.pairingConn = !bonded && pairingGeneration != 0
		conn.pairingGeneration = pairingGeneration
		if bonded {
			conn.name = rec.Name
			conn.kind = rec.Kind
			conn.vendorID = rec.VendorID
			conn.productID = rec.ProductID
			if conn.kind == controllers.KindUnknown && (rec.VendorID != 0 || rec.ProductID != 0) {
				conn.kind = controllers.IdentifyByVIDPID(rec.VendorID, rec.ProductID)
			}
		}
		if outgoing {
			conn.hostInitiated = true
		}
		if conn.kind == controllers.KindUnknown || conn.name == "" {
			m.requestRemoteName(conn)
		}
		// Require explicit link authentication + encryption before opening or
		// accepting HID Control/Interrupt L2CAP channels.
		m.requestAuthentication(conn)

	case hci.EventDisconnectionComplete:
		// [0]=status [1..2]=handle [3]=reason
		if len(params) < 4 || params[0] != 0 {
			return
		}
		handle := binary.LittleEndian.Uint16(params[1:3]) & 0x0fff
		m.tr.CancelACL(handle)
		if conn := m.findConnByHandle(handle); conn != nil {
			addr := conn.addr
			*conn = aclConn{}
			if m.OnDisconnect != nil {
				m.OnDisconnect(handle, addr)
			}
		}
		m.resumeInquiryIfPairing()

	case hci.EventRemoteNameRequestComplete:
		// [0]=status [1..6]=BD_ADDR [7..254]=UTF-8 name (null-terminated)
		if len(params) < 7 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[1:7])
		if params[0] != 0 {
			if conn := m.findConnByAddr(addr); conn != nil && conn.namePending {
				if conn.nameRetries < maxNameRetries {
					conn.nameRetries++
					conn.nameRetryAt = m.now().Add(nameRetryBackoff)
				} else {
					conn.namePending = false
					conn.nameRetryAt = time.Time{}
					if conn.kind == controllers.KindUnknown {
						m.startSDPQuery(conn)
					}
				}
			}
			return
		}
		if len(params) < 8 {
			return
		}
		rawName := params[7:]
		if nul := bytes.IndexByte(rawName, 0); nul >= 0 {
			rawName = rawName[:nul]
		}
		name := string(rawName)
		kind := controllers.IdentifyByName(name)
		if rec, ok := m.bonds.Lookup(addr); ok {
			rec.Name = name
			if kind != controllers.KindUnknown {
				rec.Kind = kind
			}
			_ = m.bonds.Upsert(rec)
		}
		if conn := m.findConnByAddr(addr); conn != nil {
			conn.namePending = false
			conn.nameRetries = 0
			conn.nameRetryAt = time.Time{}
			conn.name = name
			if kind != controllers.KindUnknown {
				conn.kind = kind
			}
			if m.OnNameResolved != nil {
				m.OnNameResolved(conn.handle, addr, name, conn.kind)
			}
			if conn.kind == controllers.KindUnknown {
				m.startSDPQuery(conn)
			}
		}

	case hci.EventIOCapabilityRequest:
		if len(params) < 6 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[0:6])
		_ = m.tr.IOCapabilityRequestReply(addr)

	case hci.EventUserConfirmationRequest:
		if len(params) < 6 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[0:6])
		// Fail-closed: only accept Just Works SSP while the explicit pairing window is open.
		_ = m.tr.UserConfirmationRequestReply(addr, m.pairingActive && m.now().Before(m.pairingUntil))

	case hci.EventLinkKeyRequest:
		if len(params) < 6 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[0:6])
		// During an explicit unexpired pairing window, negatively reply even for
		// previously bonded peers so controllers that erased their key enter SSP
		// and replace the stale bond.
		if !(m.pairingActive && m.now().Before(m.pairingUntil)) {
			if rec, ok := m.bonds.Lookup(addr); ok {
				_ = m.tr.LinkKeyRequestReply(addr, rec.LinkKey)
				return
			}
		}
		_ = m.tr.LinkKeyRequestNegativeReply(addr)

	case hci.EventLinkKeyNotification:
		// [0..5]=BD_ADDR [6..21]=LinkKey [22]=KeyType
		if len(params) < 23 || !m.pairingActive || !m.now().Before(m.pairingUntil) {
			return
		}
		var addr BDAddr
		copy(addr[:], params[0:6])
		conn := m.findConnByAddr(addr)
		if conn == nil || conn.secFailed || conn.pairingGeneration != m.pairingGeneration {
			return
		}
		var key [16]byte
		copy(key[:], params[6:22])
		rec := BondRecord{Addr: addr, LinkKey: key}
		if existing, ok := m.bonds.Lookup(addr); ok {
			rec.Name = existing.Name
			rec.Kind = existing.Kind
			rec.VendorID = existing.VendorID
			rec.ProductID = existing.ProductID
			rec.Slot = existing.Slot
		}
		conn.pairingConn = false
		if conn.name != "" {
			rec.Name = conn.name
		}
		if conn.kind != controllers.KindUnknown {
			rec.Kind = conn.kind
		}
		if conn.vendorID != 0 || conn.productID != 0 {
			rec.VendorID = conn.vendorID
			rec.ProductID = conn.productID
		}
		_ = m.bonds.Upsert(rec)

	case hci.EventInquiryResult:
		// Interleaved 14-byte records per response (Num_Responses = n):
		// [0..5]=addr, [6]=psrm, [7..8]=reserved, [9..11]=cod, [12..13]=clk
		m.handleInquiryRecords(params, 9, 12)

	case hci.EventInquiryResultWithRSSI:
		// Interleaved 14-byte records per response (Num_Responses = n):
		// [0..5]=addr, [6]=psrm, [7]=reserved, [8..10]=cod, [11..12]=clk, [13]=rssi
		m.handleInquiryRecords(params, 8, 11)

	case hci.EventExtendedInquiryResult:
		// [0]=num(1) [1..6]=addr [7]=psrm [8]=res [9..11]=cod [12..13]=clk [14]=rssi [15..]=EIR
		if len(params) < 15 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[1:7])
		cod := uint32(params[9]) | uint32(params[10])<<8 | uint32(params[11])<<16
		clk := binary.LittleEndian.Uint16(params[12:14])
		m.maybeInitiatePair(addr, cod, params[7], clk)

	case hci.EventInquiryComplete:
		m.inquiryActive = false
		m.cancelInquiryPending = false
		m.resumeInquiryIfPairing()

	case hci.EventSimplePairingComplete:
		// [0]=status [1..6]=BD_ADDR
		if len(params) < 7 || params[0] == 0 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[1:7])
		if m.connecting && m.connectAddr == addr {
			m.connecting = false
			m.connectAddr = BDAddr{}
			m.connectGeneration = 0
		}
		m.resumeInquiryIfPairing()

	case hci.EventAuthenticationComplete:
		// [0]=status [1..2]=handle
		if len(params) < 3 {
			return
		}
		handle := binary.LittleEndian.Uint16(params[1:3]) & 0x0fff
		conn := m.findConnByHandle(handle)
		if params[0] != 0 {
			if conn != nil {
				if m.connecting && m.connectAddr == conn.addr {
					m.connecting = false
					m.connectAddr = BDAddr{}
					m.connectGeneration = 0
				}
				m.failConnSecurity(conn)
			}
			m.resumeInquiryIfPairing()
			return
		}
		if conn == nil || conn.secFailed {
			return
		}
		conn.authRequested = false
		conn.secRetries = 0
		conn.secRetryPending = false
		conn.secRetryAt = time.Time{}
		conn.authenticated = true
		if conn.encrypted {
			m.onConnSecured(conn)
			return
		}
		m.requestEncryption(conn)

	case hci.EventEncryptionChange:
		// [0]=status [1..2]=handle [3]=encryption_enabled
		if len(params) < 4 {
			return
		}
		handle := binary.LittleEndian.Uint16(params[1:3]) & 0x0fff
		conn := m.findConnByHandle(handle)
		if conn == nil || conn.secFailed {
			return
		}
		if params[0] != 0 || params[3] == 0 {
			m.failConnSecurity(conn)
			m.resumeInquiryIfPairing()
			return
		}
		conn.encRequested = false
		conn.secRetries = 0
		conn.secRetryPending = false
		conn.secRetryAt = time.Time{}
		conn.encrypted = true
		if conn.authenticated {
			m.onConnSecured(conn)
		}
	}
}
