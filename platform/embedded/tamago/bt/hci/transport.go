// SPDX-License-Identifier: GPL-3.0-or-later
package hci

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

const (
	maxHCIEventBytes = 260
	maxHCICmdBytes   = 258
	maxACLUSBPacket  = 512
	maxACLTxQueue    = 16
	defaultACLMaxLen = 256
	defaultACLCredit = 4
)

type aclTxFrame struct {
	data [maxACLUSBPacket]byte
	n    int
}

// LocalVersion captures HCI_Read_Local_Version_Information (0x1001).
type LocalVersion struct {
	HCIVersion     uint8
	HCIRevision    uint16
	LMPVersion     uint8
	Manufacturer   uint16
	LMPSubversion  uint16
}

// Transport drives one USB Bluetooth HCI dongle over a usb.BusHost using EP0
// Control OUT (commands), EP1 Interrupt IN (events), EP2 Bulk IN (ACL RX), and
// EP2 Bulk OUT (ACL TX) with zero steady-state heap allocations.
type Transport struct {
	bus      usb.BusHost
	addr     uint8
	ifaceNum uint8
	spec     DongleSpec

	evPipe     usb.Pipe
	aclInPipe  usb.Pipe
	aclOutPipe usb.Pipe

	version    LocalVersion
	bdAddr     [6]byte
	aclMaxLen  int
	maxCredits int
	credits    int

	evBuf    [maxHCIEventBytes]byte
	aclRxBuf [maxACLUSBPacket]byte
	cmdBuf   [maxHCICmdBytes]byte

	txRing     [maxACLTxQueue]aclTxFrame
	txHead     int
	txCount    int
	txInFlight bool

	// Deferred events received while waiting synchronously for Command Complete.
	deferred      [8][maxHCIEventBytes]byte
	deferredLen   [8]int
	deferredHead  int
	deferredCount int
}

// Open binds a discovered USB Bluetooth HCI device on bus and allocates its 3
// dedicated pipes (Interrupt IN Events, Bulk IN ACL, Bulk OUT ACL).
func Open(bus usb.BusHost, dev usb.BusDevice) (*Transport, error) {
	if bus == nil {
		return nil, errors.New("bt/hci: nil bus host")
	}
	iface, spec, ok := FindBluetoothInterface(dev)
	if !ok {
		return nil, fmt.Errorf("bt/hci: device %04x:%04x has no Bluetooth HCI interface",
			dev.Descriptor.VendorID, dev.Descriptor.ProductID)
	}
	evEP, _ := iface.FindEndpoint(usb.TransferInterrupt, true)
	aclInEP, _ := iface.FindEndpoint(usb.TransferBulk, true)
	aclOutEP, _ := iface.FindEndpoint(usb.TransferBulk, false)

	evPipe, err := bus.OpenPipe(dev.Address, evEP)
	if err != nil {
		return nil, err
	}
	aclInPipe, err := bus.OpenPipe(dev.Address, aclInEP)
	if err != nil {
		_ = evPipe.Close()
		return nil, err
	}
	aclOutPipe, err := bus.OpenPipe(dev.Address, aclOutEP)
	if err != nil {
		_ = evPipe.Close()
		_ = aclInPipe.Close()
		return nil, err
	}
	return &Transport{
		bus:        bus,
		addr:       dev.Address,
		ifaceNum:   iface.Number,
		spec:       spec,
		evPipe:     evPipe,
		aclInPipe:  aclInPipe,
		aclOutPipe: aclOutPipe,
		aclMaxLen:  defaultACLMaxLen,
		maxCredits: defaultACLCredit,
		credits:    defaultACLCredit,
	}, nil
}

// Spec returns the matched dongle metadata and quirks.
func (t *Transport) Spec() DongleSpec { return t.spec }

// BDAddr returns the dongle's 6-byte Bluetooth device address (little-endian).
func (t *Transport) BDAddr() [6]byte { return t.bdAddr }

// LocalVersion returns the dongle's HCI/LMP version discovered during Initialize.
func (t *Transport) LocalVersion() LocalVersion { return t.version }

// ACLCredits returns currently available controller ACL TX buffer slots.
func (t *Transport) ACLCredits() int { return t.credits }

// Close releases the 3 USB pipes held by this Bluetooth dongle.
func (t *Transport) Close() error {
	var firstErr error
	for _, p := range []usb.Pipe{t.evPipe, t.aclInPipe, t.aclOutPipe} {
		if p != nil {
			if err := p.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (t *Transport) queueDeferredEvent(ev []byte) {
	if len(ev) == 0 || len(ev) > maxHCIEventBytes {
		return
	}
	if t.deferredCount == len(t.deferred) {
		t.deferredHead = (t.deferredHead + 1) % len(t.deferred)
		t.deferredCount--
	}
	idx := (t.deferredHead + t.deferredCount) % len(t.deferred)
	copy(t.deferred[idx][:], ev)
	t.deferredLen[idx] = len(ev)
	t.deferredCount++
}

// SendCommand transmits one HCI command via USB EP0 Class Control OUT
// (bmRequestType=0x20, bRequest=0x00) and waits synchronously for its matching
// Command Complete (0x0E) or Command Status (0x0F) event. Any return parameters
// (starting with the Status byte) are copied into resp.
func (t *Transport) SendCommand(opcode uint16, params []byte, resp []byte) (int, error) {
	if len(params) > 255 {
		return 0, fmt.Errorf("bt/hci: command %#04x params too long (%d)", opcode, len(params))
	}
	binary.LittleEndian.PutUint16(t.cmdBuf[0:2], opcode)
	t.cmdBuf[2] = byte(len(params))
	copy(t.cmdBuf[3:3+len(params)], params)
	if _, err := t.bus.ControlTransfer(t.addr, 0x20, 0x00, 0, uint16(t.ifaceNum), t.cmdBuf[:3+len(params)]); err != nil {
		return 0, err
	}

	// Pump USB host + Event pipe until matching Command Complete / Status arrives.
	for polls := 0; polls < 4096; polls++ {
		if err := t.bus.Service(); err != nil {
			return 0, err
		}
		n, err := t.evPipe.PollInto(t.evBuf[:])
		if err != nil {
			return 0, err
		}
		if n < 2 {
			continue
		}
		ev := t.evBuf[:n]
		switch ev[0] {
		case EventCommandComplete:
			if n >= 6 && binary.LittleEndian.Uint16(ev[3:5]) == opcode {
				copied := copy(resp, ev[5:n])
				return copied, nil
			}
		case EventCommandStatus:
			if n >= 6 && binary.LittleEndian.Uint16(ev[4:6]) == opcode {
				if len(resp) > 0 {
					resp[0] = ev[2]
					return 1, nil
				}
				return 0, nil
			}
		case EventNumCompletedPackets:
			t.handleNumCompletedPackets(ev)
			continue
		}
		// Preserve unrelated events (connect/disconnect/inquiry) for Service().
		t.queueDeferredEvent(ev)
	}
	return 0, fmt.Errorf("bt/hci: timeout waiting for command %#04x", opcode)
}

func (t *Transport) readLocalVersion() error {
	var resp [16]byte
	n, err := t.SendCommand(OpcodeReadLocalVersion, nil, resp[:])
	if err != nil {
		return err
	}
	if n < 9 || resp[0] != 0 {
		return fmt.Errorf("bt/hci: Read_Local_Version status=%#x n=%d", resp[0], n)
	}
	t.version = LocalVersion{
		HCIVersion:    resp[1],
		HCIRevision:   binary.LittleEndian.Uint16(resp[2:4]),
		LMPVersion:    resp[4],
		Manufacturer:  binary.LittleEndian.Uint16(resp[5:7]),
		LMPSubversion: binary.LittleEndian.Uint16(resp[7:9]),
	}
	return nil
}

// Initialize resets the dongle, downloads Realtek RTL8761B/BU firmware when
// required and provided by fw, queries buffer limits and BD_ADDR, enables SSP,
// disables Hold/Sniff/Park link policies for deterministic gaming latency, and
// enters Page Scan only mode (0x02) for fast bonded controller reconnects.
func (t *Transport) Initialize(fw FirmwareLoader) error {
	if err := t.sendStatusCommand(OpcodeReset, nil); err != nil {
		return fmt.Errorf("bt/hci: initial reset: %w", err)
	}
	if err := t.readLocalVersion(); err != nil {
		return err
	}

	// Realtek RTL8761B/BU/BUV (including 0bda:a728): upload EPATCH + config when
	// matched by curated VID:PID or Realtek LMP subversion 0x8761.
	needRTL := t.spec.Quirks&QuirkRTL8761BU != 0 || t.version.LMPSubversion == rtlLMPSub8761B
	if needRTL && fw != nil {
		fwName := t.spec.FirmwareFile
		if fwName == "" {
			fwName = "rtl8761bu_fw.bin"
		}
		cfgName := t.spec.ConfigFile
		if cfgName == "" {
			cfgName = "rtl8761bu_config.bin"
		}
		fwBytes, err := fw.LoadFirmware(fwName)
		if err != nil {
			return fmt.Errorf("bt/hci: load %s: %w", fwName, err)
		}
		cfgBytes, _ := fw.LoadFirmware(cfgName) // Config is optional for some RTL chips
		if err := UploadRTLFirmware(t, fwBytes, cfgBytes); err != nil {
			return err
		}
		if err := t.sendStatusCommand(OpcodeReset, nil); err != nil {
			return fmt.Errorf("bt/hci: post-firmware reset: %w", err)
		}
		if err := t.readLocalVersion(); err != nil {
			return err
		}
	}

	// Query ACL MTU and controller packet credit count.
	var resp [16]byte
	if n, err := t.SendCommand(OpcodeReadBufferSize, nil, resp[:]); err == nil && n >= 8 && resp[0] == 0 {
		aclLen := int(binary.LittleEndian.Uint16(resp[1:3]))
		aclNum := int(binary.LittleEndian.Uint16(resp[4:6]))
		if aclLen >= 64 && aclLen <= maxACLUSBPacket-4 {
			t.aclMaxLen = aclLen
		} else if aclLen > maxACLUSBPacket-4 {
			t.aclMaxLen = maxACLUSBPacket - 4
		}
		if aclNum > 0 {
			t.maxCredits = aclNum
			t.credits = aclNum
		}
	}

	// Read local Bluetooth MAC address.
	if n, err := t.SendCommand(OpcodeReadBDAddr, nil, resp[:]); err == nil && n >= 7 && resp[0] == 0 {
		copy(t.bdAddr[:], resp[1:7])
	}

	// Event mask: standard Classic BR/EDR + SSP + Inquiry + Link Key events.
	eventMask := [8]byte{0xff, 0xff, 0xfb, 0xff, 0x07, 0xf8, 0xbf, 0x3d}
	if err := t.sendStatusCommand(OpcodeSetEventMask, eventMask[:]); err != nil {
		return err
	}
	// Enable Secure Simple Pairing (SSP).
	if err := t.sendStatusCommand(OpcodeWriteSimplePairing, []byte{0x01}); err != nil {
		return err
	}
	// Gaming latency link policy: Enable Role Switch (bit 0) ONLY; disable
	// Hold (bit 1), Sniff (bit 2), and Park (bit 3) to prevent power-save spikes.
	policy := [2]byte{0x01, 0x00}
	if err := t.sendStatusCommand(OpcodeWriteDefaultLinkPolicy, policy[:]); err != nil {
		return err
	}
	// Bounded page timeout (~5.12s = 0x2000 * 0.625 ms).
	pageTO := [2]byte{0x00, 0x20}
	if err := t.sendStatusCommand(OpcodeWritePageTimeout, pageTO[:]); err != nil {
		return err
	}
	if err := t.WriteClassOfDevice(0x000104); err != nil {
		return err
	}
	if err := t.WriteLocalName("FES DE25-Nano"); err != nil {
		return err
	}
	// Page Scan only when idle/menu so bonded DualSense / Switch Pro reconnect fast.
	return t.WriteScanEnable(ScanPageOnly)
}

// SendACL enqueues an ACL payload for connection handle, automatically
// fragmenting payloads larger than the controller's ACL data length across
// PB=0x02 (First) and PB=0x01 (Continuing) packets. Zero heap allocations.
func (t *Transport) SendACL(handle uint16, pb uint8, payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	maxChunk := t.aclMaxLen
	if maxChunk <= 0 || maxChunk > maxACLUSBPacket-4 {
		maxChunk = defaultACLMaxLen
	}
	needed := (len(payload) + maxChunk - 1) / maxChunk
	if t.txCount+needed > maxACLTxQueue {
		return errors.New("bt/hci: ACL TX queue full")
	}
	curPB := pb
	for off := 0; off < len(payload); off += maxChunk {
		chunk := payload[off:min(off+maxChunk, len(payload))]
		idx := (t.txHead + t.txCount) % maxACLTxQueue
		f := &t.txRing[idx]
		hdrHandle := (handle & 0x0fff) | (uint16(curPB&0x03) << 12)
		binary.LittleEndian.PutUint16(f.data[0:2], hdrHandle)
		binary.LittleEndian.PutUint16(f.data[2:4], uint16(len(chunk)))
		copy(f.data[4:4+len(chunk)], chunk)
		f.n = 4 + len(chunk)
		t.txCount++
		curPB = ACLBoundaryContinuing
	}
	return t.pumpACLTx()
}

func (t *Transport) pumpACLTx() error {
	if t.txInFlight {
		done, err := t.aclOutPipe.WriteDone()
		if err != nil {
			t.txInFlight = false
			return err
		}
		if !done {
			return nil
		}
		t.txInFlight = false
	}
	if t.txCount == 0 || t.credits <= 0 {
		return nil
	}
	f := &t.txRing[t.txHead]
	if err := t.aclOutPipe.Write(f.data[:f.n]); err != nil {
		return err
	}
	t.txHead = (t.txHead + 1) % maxACLTxQueue
	t.txCount--
	t.credits--
	t.txInFlight = true
	return nil
}

func (t *Transport) handleNumCompletedPackets(ev []byte) {
	if len(ev) < 3 {
		return
	}
	numHandles := int(ev[2])
	if len(ev) < 3+numHandles*4 {
		return
	}
	for i := 0; i < numHandles; i++ {
		completed := int(binary.LittleEndian.Uint16(ev[3+numHandles*2+i*2:]))
		t.credits += completed
		if t.credits > t.maxCredits {
			t.credits = t.maxCredits
		}
	}
}

// Service pumps deferred and live HCI Events, drains incoming ACL Data packets
// into onACL with zero heap allocations, and submits queued ACL TX packets as
// controller flow-control credits become available.
func (t *Transport) Service(onEvent func(ev []byte), onACL func(handle uint16, pb uint8, payload []byte)) error {
	// 1. Drain deferred and live HCI events (bounded so one burst cannot starve ACL RX).
	for range 8 {
		if t.deferredCount > 0 {
			idx := t.deferredHead
			n := t.deferredLen[idx]
			t.deferredHead = (t.deferredHead + 1) % len(t.deferred)
			t.deferredCount--
			if onEvent != nil {
				onEvent(t.deferred[idx][:n])
			}
			continue
		}
		n, err := t.evPipe.PollInto(t.evBuf[:])
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		ev := t.evBuf[:n]
		if ev[0] == EventNumCompletedPackets {
			t.handleNumCompletedPackets(ev)
			continue
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}

	// 3. Drain incoming ACL packets from EP2 Bulk IN (hot path for HIDP reports!).
	for range 8 {
		n, err := t.aclInPipe.PollInto(t.aclRxBuf[:])
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		if n >= 4 && onACL != nil {
			hdr := binary.LittleEndian.Uint16(t.aclRxBuf[0:2])
			length := int(binary.LittleEndian.Uint16(t.aclRxBuf[2:4]))
			if 4+length <= n {
				handle := hdr & 0x0fff
				pb := uint8((hdr >> 12) & 0x03)
				onACL(handle, pb, t.aclRxBuf[4:4+length])
			}
		}
	}

	// 4. Advance credit-gated ACL TX queue.
	return t.pumpACLTx()
}
