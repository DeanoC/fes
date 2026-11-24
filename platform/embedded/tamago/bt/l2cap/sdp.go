// SPDX-License-Identifier: GPL-3.0-or-later
package l2cap

import (
	"encoding/binary"

	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
)

const (
	// PSMSDP is the Classic Bluetooth Service Discovery Protocol L2CAP PSM.
	PSMSDP uint16 = 0x0001

	sdpErrorRsp             byte   = 0x01
	sdpServiceSearchAttrReq byte   = 0x06
	sdpServiceSearchAttrRsp byte   = 0x07
	sdpUUIDPnPInfo          uint16 = 0x1200
	sdpAttrVendorID         uint16 = 0x0201
	sdpAttrProductID        uint16 = 0x0202
	sdpMaxContinuations     uint8  = 4
)

func (m *Manager) startSDPQuery(conn *aclConn) {
	if conn == nil || conn.sdpQueried {
		return
	}
	conn.sdpQueried = true
	conn.sdpBufLen = 0
	conn.sdpContLen = 0
	conn.sdpContCount = 0
	conn.sdpTxID = 0
	if err := m.OpenChannel(conn.handle, PSMSDP); err != nil {
		conn.pendingOpenSDP = true
	}
}

func (m *Manager) pumpSDP(conn *aclConn) {
	if conn == nil || !conn.sdpSendPending {
		return
	}
	ch := conn.findByPSM(PSMSDP)
	if ch == nil || !ch.open {
		return
	}
	txID := conn.sdpTxID + 1
	if txID == 0 {
		txID = 1
	}

	// SDP ServiceSearchAttributeRequest querying PnPInformation (UUID 0x1200)
	// attributes VendorID (0x0201) and ProductID (0x0202).
	var pdu [40]byte
	pdu[0] = sdpServiceSearchAttrReq
	binary.BigEndian.PutUint16(pdu[1:3], txID)
	binary.BigEndian.PutUint16(pdu[3:5], uint16(16+int(conn.sdpContLen)))
	// ServiceSearchPattern: DES(len=3) { UUID16(0x1200) }
	pdu[5], pdu[6], pdu[7] = 0x35, 0x03, 0x19
	binary.BigEndian.PutUint16(pdu[8:10], sdpUUIDPnPInfo)
	// MaximumAttributeByteCount = 256
	binary.BigEndian.PutUint16(pdu[10:12], uint16(len(conn.sdpBuf)))
	// AttributeIDList: DES(len=6) { uint16(0x0201), uint16(0x0202) }
	pdu[12], pdu[13] = 0x35, 0x06
	pdu[14] = 0x09
	binary.BigEndian.PutUint16(pdu[15:17], sdpAttrVendorID)
	pdu[17] = 0x09
	binary.BigEndian.PutUint16(pdu[18:20], sdpAttrProductID)
	pdu[20] = conn.sdpContLen
	if conn.sdpContLen > 0 {
		copy(pdu[21:21+int(conn.sdpContLen)], conn.sdpCont[:conn.sdpContLen])
	}
	if err := m.sendL2CAPPDU(conn.handle, ch.remoteCID, pdu[:21+int(conn.sdpContLen)]); err != nil {
		return
	}
	conn.sdpTxID = txID
	conn.sdpSendPending = false
}

func (m *Manager) closeChannel(conn *aclConn, ch *l2capChannel) {
	if conn == nil || ch == nil || !ch.inUse {
		return
	}
	var req [4]byte
	binary.LittleEndian.PutUint16(req[0:2], ch.remoteCID)
	binary.LittleEndian.PutUint16(req[2:4], ch.localCID)
	_ = m.sendSig(conn.handle, sigDisconnReq, conn.allocSigID(), req[:])
	*ch = l2capChannel{}
}

func (m *Manager) handleSDPPDU(conn *aclConn, ch *l2capChannel, payload []byte) {
	if len(payload) < 5 {
		return
	}
	pduID := payload[0]
	txID := binary.BigEndian.Uint16(payload[1:3])
	paramLen := int(binary.BigEndian.Uint16(payload[3:5]))
	if pduID == sdpErrorRsp {
		m.closeChannel(conn, ch)
		return
	}
	if pduID != sdpServiceSearchAttrRsp || txID != conn.sdpTxID || 5+paramLen > len(payload) || paramLen < 3 {
		return
	}

	params := payload[5 : 5+paramLen]
	listLen := int(binary.BigEndian.Uint16(params[0:2]))
	if 2+listLen+1 > len(params) {
		m.closeChannel(conn, ch)
		return
	}
	take := listLen
	if rem := len(conn.sdpBuf) - conn.sdpBufLen; take > rem {
		take = rem
	}
	copy(conn.sdpBuf[conn.sdpBufLen:], params[2:2+take])
	conn.sdpBufLen += take

	contLen := int(params[2+listLen])
	if contLen > len(conn.sdpCont) || 2+listLen+1+contLen > len(params) {
		m.closeChannel(conn, ch)
		return
	}
	if contLen > 0 && conn.sdpContCount < sdpMaxContinuations && conn.sdpBufLen < len(conn.sdpBuf) {
		copy(conn.sdpCont[:contLen], params[2+listLen+1:2+listLen+1+contLen])
		conn.sdpContLen = uint8(contLen)
		conn.sdpContCount++
		conn.sdpSendPending = true
		m.pumpSDP(conn)
		return
	}

	m.closeChannel(conn, ch)
	vid, pid, ok := parseSDPPnPVIDPID(conn.sdpBuf[:conn.sdpBufLen])
	if !ok {
		return
	}
	conn.vendorID = vid
	conn.productID = pid
	kind := controllers.IdentifyByVIDPID(vid, pid)
	if kind != controllers.KindUnknown {
		conn.kind = kind
	}
	if rec, found := m.bonds.Lookup(conn.addr); found {
		rec.VendorID = vid
		rec.ProductID = pid
		if kind != controllers.KindUnknown {
			rec.Kind = kind
		}
		_ = m.bonds.Upsert(rec)
	}
	if kind != controllers.KindUnknown && m.OnNameResolved != nil {
		m.OnNameResolved(conn.handle, conn.addr, conn.name, conn.kind)
	}
}

func readSDPDataElement(buf []byte) (elemType byte, val []byte, rest []byte, ok bool) {
	if len(buf) < 1 {
		return 0, nil, nil, false
	}
	hdr := buf[0]
	elemType = hdr >> 3
	if elemType > 8 {
		return 0, nil, nil, false
	}
	sizeDesc := hdr & 0x07
	hdrLen := 1
	var size int
	if elemType == 0 {
		size = 0
	} else {
		switch sizeDesc {
		case 0:
			size = 1
		case 1:
			size = 2
		case 2:
			size = 4
		case 3:
			size = 8
		case 4:
			size = 16
		case 5:
			if len(buf) < 2 {
				return 0, nil, nil, false
			}
			hdrLen = 2
			size = int(buf[1])
		case 6:
			if len(buf) < 3 {
				return 0, nil, nil, false
			}
			hdrLen = 3
			size = int(binary.BigEndian.Uint16(buf[1:3]))
		case 7:
			if len(buf) < 5 {
				return 0, nil, nil, false
			}
			hdrLen = 5
			u32 := binary.BigEndian.Uint32(buf[1:5])
			if u32 > uint32(len(buf)-hdrLen) {
				return 0, nil, nil, false
			}
			size = int(u32)
		}
	}
	if size < 0 || hdrLen+size > len(buf) {
		return 0, nil, nil, false
	}
	return elemType, buf[hdrLen : hdrLen+size], buf[hdrLen+size:], true
}

func parseSDPPnPVIDPID(data []byte) (vid, pid uint16, ok bool) {
	outerType, outerVal, _, outerOK := readSDPDataElement(data)
	if !outerOK || outerType != 6 {
		return 0, 0, false
	}
	for len(outerVal) > 0 {
		recType, recVal, nextRec, recOK := readSDPDataElement(outerVal)
		if !recOK {
			return 0, 0, false
		}
		outerVal = nextRec
		if recType != 6 {
			continue
		}
		var haveVID, havePID bool
		for len(recVal) > 0 {
			idType, idVal, afterID, idOK := readSDPDataElement(recVal)
			if !idOK {
				break
			}
			valType, valBytes, afterVal, valOK := readSDPDataElement(afterID)
			if !valOK {
				break
			}
			recVal = afterVal
			if idType != 1 || len(idVal) != 2 || valType != 1 || len(valBytes) != 2 {
				continue
			}
			attrID := binary.BigEndian.Uint16(idVal)
			attrVal := binary.BigEndian.Uint16(valBytes)
			switch attrID {
			case sdpAttrVendorID:
				vid = attrVal
				haveVID = true
			case sdpAttrProductID:
				pid = attrVal
				havePID = true
			}
			if haveVID && havePID {
				return vid, pid, true
			}
		}
	}
	return 0, 0, false
}
