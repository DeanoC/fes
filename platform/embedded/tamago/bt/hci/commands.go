// SPDX-License-Identifier: GPL-3.0-or-later
package hci

import (
	"encoding/binary"
	"fmt"
)

// Classic BR/EDR HCI Opcodes used by the input-only Bluetooth stack.
const (
	// Link Control (OGF 0x01)
	OpcodeInquiry                   uint16 = 0x0401
	OpcodeInquiryCancel             uint16 = 0x0402
	OpcodeCreateConnection          uint16 = 0x0405
	OpcodeDisconnect                uint16 = 0x0406
	OpcodeCreateConnectionCancel    uint16 = 0x0408
	OpcodeAcceptConnectionRequest   uint16 = 0x0409
	OpcodeRejectConnectionRequest   uint16 = 0x040a
	OpcodeLinkKeyRequestReply       uint16 = 0x040b
	OpcodeLinkKeyRequestNegReply    uint16 = 0x040c
	OpcodeRemoteNameRequest         uint16 = 0x0419
	OpcodeIOCapabilityRequestReply  uint16 = 0x042b
	OpcodeUserConfirmRequestReply   uint16 = 0x042c
	OpcodeUserConfirmNegReply       uint16 = 0x042d

	// Link Policy (OGF 0x02)
	OpcodeWriteDefaultLinkPolicy uint16 = 0x080f

	// Controller & Baseband (OGF 0x03)
	OpcodeSetEventMask         uint16 = 0x0c01
	OpcodeReset                uint16 = 0x0c03
	OpcodeWriteLocalName       uint16 = 0x0c13
	OpcodeWritePageTimeout     uint16 = 0x0c18
	OpcodeWriteScanEnable      uint16 = 0x0c1a
	OpcodeWriteClassOfDevice   uint16 = 0x0c24
	OpcodeWriteSimplePairing   uint16 = 0x0c56

	// Informational (OGF 0x04)
	OpcodeReadLocalVersion uint16 = 0x1001
	OpcodeReadBufferSize   uint16 = 0x1005
	OpcodeReadBDAddr       uint16 = 0x1009
)

// HCI Event Codes handled by the input-only Classic BT stack.
const (
	EventInquiryComplete            byte = 0x01
	EventInquiryResult              byte = 0x02
	EventConnectionComplete         byte = 0x03
	EventConnectionRequest          byte = 0x04
	EventDisconnectionComplete      byte = 0x05
	EventAuthenticationComplete     byte = 0x06
	EventRemoteNameRequestComplete  byte = 0x07
	EventCommandComplete            byte = 0x0e
	EventCommandStatus              byte = 0x0f
	EventNumCompletedPackets        byte = 0x13
	EventLinkKeyRequest             byte = 0x17
	EventLinkKeyNotification        byte = 0x18
	EventInquiryResultWithRSSI      byte = 0x22
	EventExtendedInquiryResult      byte = 0x2f
	EventIOCapabilityRequest        byte = 0x31
	EventUserConfirmationRequest    byte = 0x33
	EventSimplePairingComplete      byte = 0x36
)

// ScanEnable bits for HCI_Write_Scan_Enable (0x0C1A).
const (
	ScanDisabled       byte = 0x00
	ScanInquiryOnly    byte = 0x01
	ScanPageOnly       byte = 0x02
	ScanInquiryAndPage byte = 0x03
)

// ACL Packet Boundary (PB) flags in the 12..13 bit field of the ACL header.
const (
	ACLBoundaryFirstNonFlush  uint8 = 0x00 // First non-automatically-flushable fragment of L2CAP PDU
	ACLBoundaryContinuing     uint8 = 0x01 // Continuation fragment
	ACLBoundaryFirstAutoFlush uint8 = 0x02 // First auto-flushable packet of higher-layer L2CAP PDU
)

func (t *Transport) sendStatusCommand(opcode uint16, params []byte) error {
	var resp [8]byte
	n, err := t.SendCommand(opcode, params, resp[:])
	if err != nil {
		return err
	}
	if n > 0 && resp[0] != 0 {
		return fmt.Errorf("bt/hci: opcode %#04x failed status=%#02x", opcode, resp[0])
	}
	return nil
}

// WriteScanEnable configures inquiry/page scan radio windows.
func (t *Transport) WriteScanEnable(mode byte) error {
	return t.sendStatusCommand(OpcodeWriteScanEnable, []byte{mode})
}

// SetGameplayRadioMode disables inquiry & page scan (0x00) while a core is
// actively running so RF scan windows cannot cause wireless input jitter, and
// restores Page Scan only (0x02) in menu/idle so bonded pads reconnect quickly.
func (t *Transport) SetGameplayRadioMode(active bool) error {
	if active {
		return t.WriteScanEnable(ScanDisabled)
	}
	return t.WriteScanEnable(ScanPageOnly)
}

// WriteLocalName sets the host's Classic BT friendly name (up to 248 bytes).
func (t *Transport) WriteLocalName(name string) error {
	var params [248]byte
	copy(params[:247], name)
	return t.sendStatusCommand(OpcodeWriteLocalName, params[:])
}

// WriteClassOfDevice sets the 24-bit Class of Device (e.g. 0x000104 Desktop/Console).
func (t *Transport) WriteClassOfDevice(cod uint32) error {
	params := [3]byte{byte(cod), byte(cod >> 8), byte(cod >> 16)}
	return t.sendStatusCommand(OpcodeWriteClassOfDevice, params[:])
}

// AcceptConnectionRequest accepts an incoming BR/EDR connection. Role 0x00
// requests Master/Central role switch for deterministic low-latency polling.
func (t *Transport) AcceptConnectionRequest(bdAddr [6]byte, role byte) error {
	var params [7]byte
	copy(params[:6], bdAddr[:])
	params[6] = role
	return t.sendStatusCommand(OpcodeAcceptConnectionRequest, params[:])
}

// RejectConnectionRequest rejects an incoming connection with an HCI reason code
// (e.g. 0x0F = Rejected due to Security Reasons).
func (t *Transport) RejectConnectionRequest(bdAddr [6]byte, reason byte) error {
	var params [7]byte
	copy(params[:6], bdAddr[:])
	params[6] = reason
	return t.sendStatusCommand(OpcodeRejectConnectionRequest, params[:])
}

// CreateConnection initiates an outgoing BR/EDR ACL connection to bdAddr.
func (t *Transport) CreateConnection(bdAddr [6]byte, pageScanRepMode byte, clockOffset uint16) error {
	var params [13]byte
	copy(params[0:6], bdAddr[:])
	binary.LittleEndian.PutUint16(params[6:8], 0xcc18) // DM1/DH1/DM3/DH3/DM5/DH5
	params[8] = pageScanRepMode
	params[9] = 0
	binary.LittleEndian.PutUint16(params[10:12], clockOffset)
	params[12] = 0x01 // Allow role switch
	return t.sendStatusCommand(OpcodeCreateConnection, params[:])
}

// CreateConnectionCancel aborts an in-flight outgoing CreateConnection page.
func (t *Transport) CreateConnectionCancel(bdAddr [6]byte) error {
	return t.sendStatusCommand(OpcodeCreateConnectionCancel, bdAddr[:])
}

// Disconnect terminates an active ACL connection handle.
func (t *Transport) Disconnect(handle uint16, reason byte) error {
	var params [3]byte
	binary.LittleEndian.PutUint16(params[0:2], handle&0x0fff)
	params[2] = reason
	return t.sendStatusCommand(OpcodeDisconnect, params[:])
}

// StartInquiry begins a bounded Classic BT inquiry scan (length * 1.28 s).
func (t *Transport) StartInquiry(lengthUnits, maxResponses byte) error {
	// General/Unlimited Inquiry Access Code (GIAC): 0x9E8B33
	params := [5]byte{0x33, 0x8b, 0x9e, lengthUnits, maxResponses}
	return t.sendStatusCommand(OpcodeInquiry, params[:])
}

// CancelInquiry stops an active Classic BT inquiry scan.
func (t *Transport) CancelInquiry() error {
	return t.sendStatusCommand(OpcodeInquiryCancel, nil)
}

// RemoteNameRequest queries a remote device's UTF-8 friendly name.
func (t *Transport) RemoteNameRequest(bdAddr [6]byte) error {
	var params [10]byte
	copy(params[0:6], bdAddr[:])
	params[6] = 0x01 // R1 page scan repetition mode
	return t.sendStatusCommand(OpcodeRemoteNameRequest, params[:])
}

// IOCapabilityRequestReply replies with NoInputNoOutput (Just Works SSP).
func (t *Transport) IOCapabilityRequestReply(bdAddr [6]byte) error {
	var params [9]byte
	copy(params[0:6], bdAddr[:])
	params[6] = 0x03 // NoInputNoOutput
	params[7] = 0x00 // OOB data not present
	params[8] = 0x04 // MITM not required, Dedicated Bonding
	return t.sendStatusCommand(OpcodeIOCapabilityRequestReply, params[:])
}

// UserConfirmationRequestReply accepts or rejects an SSP Just Works confirmation.
func (t *Transport) UserConfirmationRequestReply(bdAddr [6]byte, accept bool) error {
	op := OpcodeUserConfirmNegReply
	if accept {
		op = OpcodeUserConfirmRequestReply
	}
	return t.sendStatusCommand(op, bdAddr[:])
}

// LinkKeyRequestReply supplies a stored 16-byte link key for fast reconnect.
func (t *Transport) LinkKeyRequestReply(bdAddr [6]byte, linkKey [16]byte) error {
	var params [22]byte
	copy(params[0:6], bdAddr[:])
	copy(params[6:22], linkKey[:])
	return t.sendStatusCommand(OpcodeLinkKeyRequestReply, params[:])
}

// LinkKeyRequestNegativeReply informs the controller no stored link key exists.
func (t *Transport) LinkKeyRequestNegativeReply(bdAddr [6]byte) error {
	return t.sendStatusCommand(OpcodeLinkKeyRequestNegReply, bdAddr[:])
}
