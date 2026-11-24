// SPDX-License-Identifier: GPL-3.0-or-later
package l2cap

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
)

const (
	CIDSignaling    uint16 = 0x0001
	PSMHIDControl   uint16 = 0x0011
	PSMHIDInterrupt uint16 = 0x0013

	sigCmdReject    byte = 0x01
	sigConnReq      byte = 0x02
	sigConnRsp      byte = 0x03
	sigConfigReq    byte = 0x04
	sigConfigRsp    byte = 0x05
	sigDisconnReq   byte = 0x06
	sigDisconnRsp   byte = 0x07
	sigInfoReq      byte = 0x0a
	sigInfoRsp      byte = 0x0b

	defaultMTU       uint16 = 672
	maxL2CAPPayload         = 680
	maxConnections          = 4
	maxChansPerConn         = 4
	firstDynamicCID  uint16 = 0x0040
)

type l2capChannel struct {
	inUse       bool
	psm         uint16
	localCID    uint16
	remoteCID   uint16
	localCfgOK  bool
	remoteCfgOK bool
	open        bool
}

type aclConn struct {
	active         bool
	handle         uint16
	addr           BDAddr
	name           string
	kind           controllers.Kind
	hostInitiated  bool
	pendingOpenPSM uint16

	chans   [maxChansPerConn]l2capChannel
	nextCID uint16
	nextSig byte

	// Inline ACL->L2CAP reassembly buffer (0 heap allocations on HID hot path).
	reasmBuf  [maxL2CAPPayload]byte
	reasmLen  int
	reasmWant int
	reasmCID  uint16
	reasmLive bool
}

func (c *aclConn) allocSigID() byte {
	c.nextSig++
	if c.nextSig == 0 {
		c.nextSig = 1
	}
	return c.nextSig
}

func (c *aclConn) allocChannel(psm uint16) *l2capChannel {
	for i := range c.chans {
		if !c.chans[i].inUse {
			cid := c.nextCID
			if cid < firstDynamicCID {
				cid = firstDynamicCID
			}
			c.nextCID = cid + 1
			c.chans[i] = l2capChannel{
				inUse:    true,
				psm:      psm,
				localCID: cid,
			}
			return &c.chans[i]
		}
	}
	return nil
}

func (c *aclConn) findByLocalCID(cid uint16) *l2capChannel {
	for i := range c.chans {
		if c.chans[i].inUse && c.chans[i].localCID == cid {
			return &c.chans[i]
		}
	}
	return nil
}

func (c *aclConn) findByPSM(psm uint16) *l2capChannel {
	for i := range c.chans {
		if c.chans[i].inUse && c.chans[i].psm == psm {
			return &c.chans[i]
		}
	}
	return nil
}

// handleACL reassembles incoming HCI ACL fragments into complete L2CAP PDUs
// using the connection's inline reasmBuf (zero heap allocations).
func (m *Manager) handleACL(handle uint16, pb uint8, payload []byte) {
	conn := m.findConnByHandle(handle)
	if conn == nil || len(payload) == 0 {
		return
	}
	switch pb {
	case hci.ACLBoundaryFirstNonFlush, hci.ACLBoundaryFirstAutoFlush:
		if len(payload) < 4 {
			conn.reasmLive = false
			return
		}
		want := int(binary.LittleEndian.Uint16(payload[0:2]))
		cid := binary.LittleEndian.Uint16(payload[2:4])
		body := payload[4:]
		if want > maxL2CAPPayload {
			conn.reasmLive = false
			return
		}
		if len(body) >= want {
			// Fast path: single-packet L2CAP PDU (nearly all DualSense/Switch Pro reports!).
			conn.reasmLive = false
			m.dispatchPDU(conn, cid, body[:want])
			return
		}
		copy(conn.reasmBuf[:], body)
		conn.reasmLen = len(body)
		conn.reasmWant = want
		conn.reasmCID = cid
		conn.reasmLive = true

	case hci.ACLBoundaryContinuing:
		if !conn.reasmLive {
			return
		}
		rem := conn.reasmWant - conn.reasmLen
		take := min(len(payload), rem)
		copy(conn.reasmBuf[conn.reasmLen:], payload[:take])
		conn.reasmLen += take
		if conn.reasmLen >= conn.reasmWant {
			conn.reasmLive = false
			m.dispatchPDU(conn, conn.reasmCID, conn.reasmBuf[:conn.reasmWant])
		}
	}
}

func (m *Manager) dispatchPDU(conn *aclConn, cid uint16, payload []byte) {
	if cid == CIDSignaling {
		m.handleSignaling(conn, payload)
		return
	}
	ch := conn.findByLocalCID(cid)
	if ch == nil || !ch.open {
		return
	}
	switch ch.psm {
	case PSMHIDControl:
		if m.OnHIDControl != nil {
			m.OnHIDControl(conn.handle, payload)
		}
	case PSMHIDInterrupt:
		if m.OnHIDInterrupt != nil {
			m.OnHIDInterrupt(conn.handle, payload)
		}
	}
}

func (m *Manager) sendL2CAPPDU(handle, remoteCID uint16, payload []byte) error {
	if 4+len(payload) > len(m.txScratch) {
		return fmt.Errorf("bt/l2cap: PDU too large (%d)", len(payload))
	}
	binary.LittleEndian.PutUint16(m.txScratch[0:2], uint16(len(payload)))
	binary.LittleEndian.PutUint16(m.txScratch[2:4], remoteCID)
	copy(m.txScratch[4:], payload)
	return m.tr.SendACL(handle, hci.ACLBoundaryFirstAutoFlush, m.txScratch[:4+len(payload)])
}

func (m *Manager) sendSig(handle uint16, code, id byte, body []byte) error {
	var sig [4 + 32]byte
	if len(body) > 32 {
		return errors.New("bt/l2cap: signaling body too large")
	}
	sig[0] = code
	sig[1] = id
	binary.LittleEndian.PutUint16(sig[2:4], uint16(len(body)))
	copy(sig[4:], body)
	return m.sendL2CAPPDU(handle, CIDSignaling, sig[:4+len(body)])
}

// OpenChannel initiates an outgoing L2CAP channel (PSMHIDControl or PSMHIDInterrupt)
// to a connected controller.
func (m *Manager) OpenChannel(handle, psm uint16) error {
	conn := m.findConnByHandle(handle)
	if conn == nil {
		return fmt.Errorf("bt/l2cap: unknown handle %#04x", handle)
	}
	if ch := conn.findByPSM(psm); ch != nil {
		return nil // Already opening or open
	}
	ch := conn.allocChannel(psm)
	if ch == nil {
		return errors.New("bt/l2cap: no free L2CAP channels")
	}
	var body [4]byte
	binary.LittleEndian.PutUint16(body[0:2], psm)
	binary.LittleEndian.PutUint16(body[2:4], ch.localCID)
	if err := m.sendSig(handle, sigConnReq, conn.allocSigID(), body[:]); err != nil {
		*ch = l2capChannel{}
		return err
	}
	return nil
}

// SendChannel transmits an L2CAP payload on the open channel for psm
// (PSMHIDControl or PSMHIDInterrupt) with zero heap allocations.
func (m *Manager) SendChannel(handle, psm uint16, payload []byte) error {
	conn := m.findConnByHandle(handle)
	if conn == nil {
		return fmt.Errorf("bt/l2cap: unknown handle %#04x", handle)
	}
	ch := conn.findByPSM(psm)
	if ch == nil || !ch.open {
		return fmt.Errorf("bt/l2cap: PSM %#04x not open on handle %#04x", psm, handle)
	}
	return m.sendL2CAPPDU(handle, ch.remoteCID, payload)
}

func (m *Manager) sendConfigRequest(conn *aclConn, ch *l2capChannel) {
	// Request MTU = 672 bytes (option type 0x01, len 0x02, value 672 LE).
	var body [8]byte
	binary.LittleEndian.PutUint16(body[0:2], ch.remoteCID)
	binary.LittleEndian.PutUint16(body[2:4], 0x0000) // flags
	body[4] = 0x01                                   // MTU option
	body[5] = 0x02
	binary.LittleEndian.PutUint16(body[6:8], defaultMTU)
	_ = m.sendSig(conn.handle, sigConfigReq, conn.allocSigID(), body[:])
}

func (m *Manager) checkChannelOpen(conn *aclConn, ch *l2capChannel) {
	if ch.open || !ch.localCfgOK || !ch.remoteCfgOK {
		return
	}
	ch.open = true
	if m.OnChannelOpen != nil {
		m.OnChannelOpen(conn.handle, conn.addr, conn.name, conn.kind, ch.psm)
	}
	// If host initiated PSMHIDControl during pairing, immediately open PSMHIDInterrupt.
	if conn.hostInitiated && ch.psm == PSMHIDControl && conn.findByPSM(PSMHIDInterrupt) == nil {
		if err := m.OpenChannel(conn.handle, PSMHIDInterrupt); err != nil {
			conn.pendingOpenPSM = PSMHIDInterrupt
		}
	}
}

func (m *Manager) handleSignaling(conn *aclConn, payload []byte) {
	for len(payload) >= 4 {
		code := payload[0]
		id := payload[1]
		length := int(binary.LittleEndian.Uint16(payload[2:4]))
		if 4+length > len(payload) {
			return
		}
		body := payload[4 : 4+length]
		payload = payload[4+length:]

		switch code {
		case sigConnReq:
			if len(body) < 4 {
				continue
			}
			psm := binary.LittleEndian.Uint16(body[0:2])
			srcCID := binary.LittleEndian.Uint16(body[2:4])
			var rsp [8]byte
			binary.LittleEndian.PutUint16(rsp[2:4], srcCID)
			if psm != PSMHIDControl && psm != PSMHIDInterrupt {
				// Reject unsupported PSM (e.g. SDP/audio) cleanly with 0x0002.
				binary.LittleEndian.PutUint16(rsp[4:6], 0x0002)
				_ = m.sendSig(conn.handle, sigConnRsp, id, rsp[:])
				continue
			}
			ch := conn.allocChannel(psm)
			if ch == nil {
				binary.LittleEndian.PutUint16(rsp[4:6], 0x0004) // No resources
				_ = m.sendSig(conn.handle, sigConnRsp, id, rsp[:])
				continue
			}
			ch.remoteCID = srcCID
			binary.LittleEndian.PutUint16(rsp[0:2], ch.localCID)
			binary.LittleEndian.PutUint16(rsp[4:6], 0x0000) // Connection successful
			_ = m.sendSig(conn.handle, sigConnRsp, id, rsp[:])
			m.sendConfigRequest(conn, ch)

		case sigConnRsp:
			if len(body) < 8 {
				continue
			}
			dstCID := binary.LittleEndian.Uint16(body[0:2])
			srcCID := binary.LittleEndian.Uint16(body[2:4])
			result := binary.LittleEndian.Uint16(body[4:6])
			ch := conn.findByLocalCID(srcCID)
			if ch == nil {
				continue
			}
			if result == 0x0000 { // Success
				ch.remoteCID = dstCID
				m.sendConfigRequest(conn, ch)
			} else if result != 0x0001 { // 0x0001 = Pending; anything else = failure
				*ch = l2capChannel{}
			}

		case sigConfigReq:
			if len(body) < 4 {
				continue
			}
			dstCID := binary.LittleEndian.Uint16(body[0:2])
			ch := conn.findByLocalCID(dstCID)
			if ch == nil {
				continue
			}
			var rsp [6]byte
			binary.LittleEndian.PutUint16(rsp[0:2], ch.remoteCID)
			binary.LittleEndian.PutUint16(rsp[2:4], 0x0000) // flags
			binary.LittleEndian.PutUint16(rsp[4:6], 0x0000) // Success
			_ = m.sendSig(conn.handle, sigConfigRsp, id, rsp[:])
			ch.remoteCfgOK = true
			m.checkChannelOpen(conn, ch)

		case sigConfigRsp:
			if len(body) < 6 {
				continue
			}
			srcCID := binary.LittleEndian.Uint16(body[0:2])
			result := binary.LittleEndian.Uint16(body[4:6])
			ch := conn.findByLocalCID(srcCID)
			if ch != nil && result == 0x0000 {
				ch.localCfgOK = true
				m.checkChannelOpen(conn, ch)
			}

		case sigDisconnReq:
			if len(body) < 4 {
				continue
			}
			dstCID := binary.LittleEndian.Uint16(body[0:2])
			srcCID := binary.LittleEndian.Uint16(body[2:4])
			if ch := conn.findByLocalCID(dstCID); ch != nil {
				*ch = l2capChannel{}
			}
			var rsp [4]byte
			binary.LittleEndian.PutUint16(rsp[0:2], dstCID)
			binary.LittleEndian.PutUint16(rsp[2:4], srcCID)
			_ = m.sendSig(conn.handle, sigDisconnRsp, id, rsp[:])

		case sigDisconnRsp:
			if len(body) < 4 {
				continue
			}
			srcCID := binary.LittleEndian.Uint16(body[2:4])
			if ch := conn.findByLocalCID(srcCID); ch != nil {
				*ch = l2capChannel{}
			}

		case sigInfoReq:
			if len(body) < 2 {
				continue
			}
			infoType := binary.LittleEndian.Uint16(body[0:2])
			switch infoType {
			case 0x0002: // Extended Features Supported Mask
				var rsp [8]byte
				binary.LittleEndian.PutUint16(rsp[0:2], infoType)
				binary.LittleEndian.PutUint16(rsp[2:4], 0x0000) // Success
				// Feature mask 0 (basic L2CAP only, lowest latency)
				_ = m.sendSig(conn.handle, sigInfoRsp, id, rsp[:])
			case 0x0003: // Fixed Channels Supported
				var rsp [12]byte
				binary.LittleEndian.PutUint16(rsp[0:2], infoType)
				binary.LittleEndian.PutUint16(rsp[2:4], 0x0000) // Success
				rsp[4] = 0x02                                    // Bit 1 = L2CAP Signaling (CID 0x0001)
				_ = m.sendSig(conn.handle, sigInfoRsp, id, rsp[:])
			default:
				var rsp [4]byte
				binary.LittleEndian.PutUint16(rsp[0:2], infoType)
				binary.LittleEndian.PutUint16(rsp[2:4], 0x0001) // Not supported
				_ = m.sendSig(conn.handle, sigInfoRsp, id, rsp[:])
			}

		case sigCmdReject:
			// Ignore remote command rejections gracefully.
		}
	}
}
