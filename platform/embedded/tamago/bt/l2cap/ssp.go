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

	pairingActive bool
	pairingUntil  time.Time
	connecting    bool
	connectAddr   BDAddr

	OnChannelOpen  func(handle uint16, addr BDAddr, name string, kind controllers.Kind, psm uint16)
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

// SetPairingMode opens or closes a bounded SSP pairing window. Outside pairing
// mode, unknown devices and SSP User Confirmation requests are rejected fail-closed.
func (m *Manager) SetPairingMode(enabled bool, duration time.Duration) error {
	pendingConnecting, pendingAddr := m.connecting, m.connectAddr
	if enabled {
		if duration <= 0 {
			duration = 30 * time.Second
		}
		m.pairingActive = true
		m.pairingUntil = m.now().Add(duration)
		m.connecting = false
		m.connectAddr = BDAddr{}
		if pendingConnecting {
			_ = m.tr.CreateConnectionCancel(pendingAddr)
		}
		if err := m.tr.WriteScanEnable(hci.ScanInquiryAndPage); err != nil {
			m.pairingActive = false
			m.pairingUntil = time.Time{}
			return err
		}
		// 8 * 1.28s = 10.24s inquiry burst (restarted if still pairing)
		if err := m.tr.StartInquiry(8, 0); err != nil {
			m.pairingActive = false
			m.pairingUntil = time.Time{}
			m.connecting = false
			m.connectAddr = BDAddr{}
			_ = m.tr.CancelInquiry()
			_ = m.tr.WriteScanEnable(hci.ScanPageOnly)
			return err
		}
		return nil
	}
	m.pairingActive = false
	m.pairingUntil = time.Time{}
	m.connecting = false
	m.connectAddr = BDAddr{}
	if pendingConnecting {
		_ = m.tr.CreateConnectionCancel(pendingAddr)
	}
	_ = m.tr.CancelInquiry()
	return m.tr.WriteScanEnable(hci.ScanPageOnly)
}

const (
	maxNameRetries   uint8 = 5
	nameRetryBackoff       = 250 * time.Millisecond
)

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
		}
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
	for i := range m.conns {
		conn := &m.conns[i]
		if !conn.active {
			continue
		}
		if conn.namePending && !conn.nameRetryAt.IsZero() && !m.now().Before(conn.nameRetryAt) {
			m.requestRemoteName(conn)
		}
		if conn.pendingOpenPSM != 0 {
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
			if ch.inUse && (ch.pendingConnRsp || ch.pendingCfg || ch.pendingCfgRsp) {
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

func isHIDPeripheralCoD(cod uint32) bool {
	major := (cod >> 8) & 0x1f
	return major == 0x05 // Major Device Class: Peripheral (Gamepad/Joystick/Keyboard/Mouse)
}

func (m *Manager) resumeInquiryIfPairing() {
	if m.pairingActive && !m.connecting && m.now().Before(m.pairingUntil) {
		_ = m.tr.StartInquiry(8, 0)
	}
}

func (m *Manager) maybeInitiatePair(addr BDAddr, cod uint32, pageScanRep byte, clockOff uint16) {
	if !m.pairingActive || m.connecting || !isHIDPeripheralCoD(cod) {
		return
	}
	if m.findConnByAddr(addr) != nil {
		return
	}
	m.connecting = true
	m.connectAddr = addr
	_ = m.tr.CancelInquiry()
	if err := m.tr.CreateConnection(addr, pageScanRep, clockOff); err != nil {
		m.connecting = false
		m.connectAddr = BDAddr{}
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
		if bonded || (m.pairingActive && isHIDPeripheralCoD(cod)) {
			// Role 0x00: request Master/Central role for deterministic polling slots!
			_ = m.tr.AcceptConnectionRequest(addr, 0x00)
		} else {
			// Fail-closed: reject unknown or non-HID devices even during pairing.
			_ = m.tr.RejectConnectionRequest(addr, 0x0f)
		}

	case hci.EventConnectionComplete:
		// [0]=status [1..2]=handle [3..8]=BD_ADDR [9]=link_type [10]=enc
		if len(params) < 11 || params[9] != 0x01 {
			return
		}
		var addr BDAddr
		copy(addr[:], params[3:9])
		if m.connecting && m.connectAddr == addr {
			m.connecting = false
			m.connectAddr = BDAddr{}
		}
		if params[0] != 0 {
			m.resumeInquiryIfPairing()
			return
		}
		handle := binary.LittleEndian.Uint16(params[1:3]) & 0x0fff
		conn := m.allocConn(handle, addr)
		if conn == nil {
			_ = m.tr.Disconnect(handle, 0x13)
			return
		}
		if rec, ok := m.bonds.Lookup(addr); ok {
			conn.name = rec.Name
			conn.kind = rec.Kind
			conn.vendorID = rec.VendorID
			conn.productID = rec.ProductID
			if conn.kind == controllers.KindUnknown && (rec.VendorID != 0 || rec.ProductID != 0) {
				conn.kind = controllers.IdentifyByVIDPID(rec.VendorID, rec.ProductID)
			}
		}
		if m.pairingActive {
			conn.hostInitiated = true
		}
		if conn.kind == controllers.KindUnknown || conn.name == "" {
			m.requestRemoteName(conn)
		}
		if conn.hostInitiated {
			if err := m.OpenChannel(handle, PSMHIDControl); err != nil {
				conn.pendingOpenPSM = PSMHIDControl
			}
		}

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
		var key [16]byte
		copy(addr[:], params[0:6])
		copy(key[:], params[6:22])
		rec := BondRecord{Addr: addr, LinkKey: key}
		if existing, ok := m.bonds.Lookup(addr); ok {
			rec.Name = existing.Name
			rec.Kind = existing.Kind
			rec.VendorID = existing.VendorID
			rec.ProductID = existing.ProductID
			rec.Slot = existing.Slot
		}
		if conn := m.findConnByAddr(addr); conn != nil {
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
		}
		_ = m.bonds.Upsert(rec)

	case hci.EventInquiryResult:
		// [0]=num [1..]: 14 bytes per entry (addr[6], psrm, reserved[2], cod[3], clk[2])
		if len(params) < 1 {
			return
		}
		n := int(params[0])
		for i := 0; i < n && 1+(i+1)*14 <= len(params); i++ {
			off := 1 + i*14
			var addr BDAddr
			copy(addr[:], params[off:off+6])
			cod := uint32(params[off+9]) | uint32(params[off+10])<<8 | uint32(params[off+11])<<16
			clk := binary.LittleEndian.Uint16(params[off+12 : off+14])
			m.maybeInitiatePair(addr, cod, params[off+6], clk)
		}

	case hci.EventInquiryResultWithRSSI:
		// [0]=num [1..]: 14 bytes per entry (addr[6], psrm, reserved, cod[3], clk[2], rssi)
		if len(params) < 1 {
			return
		}
		n := int(params[0])
		for i := 0; i < n && 1+(i+1)*14 <= len(params); i++ {
			off := 1 + i*14
			var addr BDAddr
			copy(addr[:], params[off:off+6])
			cod := uint32(params[off+8]) | uint32(params[off+9])<<8 | uint32(params[off+10])<<16
			clk := binary.LittleEndian.Uint16(params[off+11 : off+13])
			m.maybeInitiatePair(addr, cod, params[off+6], clk)
		}

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
		}
		m.resumeInquiryIfPairing()

	case hci.EventAuthenticationComplete:
		// [0]=status [1..2]=handle
		if len(params) < 3 || params[0] == 0 {
			return
		}
		handle := binary.LittleEndian.Uint16(params[1:3]) & 0x0fff
		if conn := m.findConnByHandle(handle); conn != nil && m.connecting && m.connectAddr == conn.addr {
			m.connecting = false
			m.connectAddr = BDAddr{}
		}
		m.resumeInquiryIfPairing()
	}
}
