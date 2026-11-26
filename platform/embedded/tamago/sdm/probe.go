// SPDX-License-Identifier: GPL-3.0-or-later

// Package sdm implements read-only Agilex SDM discovery through TF-A's SiP
// mailbox service. Configuration status is not a core identity or DMA proof.
// Its separate ConfigModel describes a hypothetical contained configuration
// endpoint; that model never invokes firmware or authorizes physical access.
package sdm

import (
	"errors"
	"fmt"
	"sync"
)

const (
	MailboxService = 0xc200003c
	configStatus   = 0x04
	getIDCode      = 0x10
)

// Invoke implements the SMCCC x0..x6 request and x0..x3 return registers.
// It must return synchronously with firmware no longer accessing the response
// arena. A caller deadline cannot interrupt an SMC that never returns.
type Invoke func([7]uint64) [4]uint64

// Client owns a reserved response window for its lifetime, including failures.
// Firmware service calls are serialized. No raw mailbox or configuration
// operation is exposed: FPGA_CONFIG_ISDONE can reopen bridges in TF-A v2.12.
type Client struct {
	mu       sync.Mutex
	invoke   Invoke
	response []uint32
	address  uint64
	retired  bool
}

// New requires eight response words backed by reserved, firmware-accessible
// noncacheable DDR, followed by a reserved word used as a zero-length argument
// address. On hardware these must not be Go-heap or stack buffers. The board
// adapter, not this protocol client, proves mapping and exclusive ownership.
func New(invoke Invoke, response []uint32, address uint64) (*Client, error) {
	if invoke == nil || len(response) != 8 || address%64 != 0 || address < 0x82000000 || address > 0xc0000000-36 {
		return nil, errors.New("SDM: invalid service or reserved response window")
	}
	return &Client{invoke: invoke, response: response, address: address}, nil
}

// Known CONFIG_STATUS state codes aligned with TF-A sdm_input_mailbox.h.
const (
	StateIdle          uint32 = 0x00000000
	StateInProgress    uint32 = 0x10000000
	StateFailAck       uint32 = 0x08000000
	StateVABPreAuth    uint32 = 0x20000000
	StateErrorInvalid  uint32 = 0xf0000001
	StateErrorCorrupt  uint32 = 0xf0000002
	StateErrorAuth     uint32 = 0xf0000003
	StateErrorCoreIO   uint32 = 0xf0000004
	StateErrorHardware uint32 = 0xf0000005
	StateErrorFake     uint32 = 0xf0000006
	StateErrorBootInfo uint32 = 0xf0000007
	StateErrorQSPI     uint32 = 0xf0000008
)

// Observation contains SDM observations only. IDCode identifies silicon; it
// cannot establish which package is running, bridge isolation, or scanout drain.
type Observation struct {
	IDCode        uint32
	State         uint32
	Version       uint32
	Pins          uint32
	Soft          uint32
	ErrorLocation uint32
	ErrorDetails  uint32
}

// PinStatus holds decoded pin status bits from Observation.Pins
// (bit 31: nSTATUS, bit 30: nCONFIG, bits 3:0: MSEL).
type PinStatus struct {
	NStatus bool
	NConfig bool
	MSEL    uint8
	Raw     uint32
}

// SoftStatus holds decoded soft-function status bits from Observation.Soft.
type SoftStatus struct {
	ConfDone bool
	InitDone bool
	CVPDone  bool
	SEUError bool
	Raw      uint32
}

// IDCodeInfo holds decoded IEEE 1149.1 JTAG IDCODE fields from Observation.IDCode.
type IDCodeInfo struct {
	Version      uint8
	PartNumber   uint16
	Manufacturer uint16
	MarkerBit    bool
	Valid        bool
	Raw          uint32
}

func (o Observation) Configured() bool {
	pins := o.PinStatus()
	soft := o.SoftStatus()
	return o.State == StateIdle && pins.NStatus && soft.ConfDone && soft.InitDone && !soft.SEUError
}

// PinStatus decodes the raw CONFIG_STATUS pin status word.
func (o Observation) PinStatus() PinStatus {
	return PinStatus{
		NStatus: o.Pins&(1<<31) != 0,
		NConfig: o.Pins&(1<<30) != 0,
		MSEL:    uint8(o.Pins & 0xf),
		Raw:     o.Pins,
	}
}

// SoftStatus decodes the raw CONFIG_STATUS soft-function status word.
func (o Observation) SoftStatus() SoftStatus {
	return SoftStatus{
		ConfDone: o.Soft&(1<<0) != 0,
		InitDone: o.Soft&(1<<1) != 0,
		CVPDone:  o.Soft&(1<<2) != 0,
		SEUError: o.Soft&(1<<3) != 0,
		Raw:      o.Soft,
	}
}

// IDCodeInfo decodes IEEE 1149.1 JTAG IDCODE fields from Observation.IDCode.
func (o Observation) IDCodeInfo() IDCodeInfo {
	marker := o.IDCode&1 != 0
	return IDCodeInfo{
		Version:      uint8(o.IDCode >> 28),
		PartNumber:   uint16((o.IDCode >> 12) & 0xffff),
		Manufacturer: uint16((o.IDCode >> 1) & 0x7ff),
		MarkerBit:    marker,
		Valid:        o.IDCode != 0 && o.IDCode != 0xffffffff && marker,
		Raw:          o.IDCode,
	}
}

// HasError reports whether SDM CONFIG_STATUS indicates an error state,
// non-zero ErrorLocation, or soft SEU error. Note that response word 5
// (ErrorDetails) is reserved on Agilex 5 and may be non-zero when healthy.
func (o Observation) HasError() bool {
	stateErr := o.State != StateIdle && o.State != StateInProgress && o.State != StateVABPreAuth
	return stateErr || o.ErrorLocation != 0 || o.SoftStatus().SEUError
}

// String formats a concise diagnostic summary of the SDM observation.
func (o Observation) String() string {
	return fmt.Sprintf(
		"idcode=%#08x state=%#x configured=%t error=%t version=%#x pins=%#08x soft=%#08x err=(%#x,%#x)",
		o.IDCode, o.State, o.Configured(), o.HasError(), o.Version, o.Pins, o.Soft, o.ErrorLocation, o.ErrorDetails,
	)
}

// Probe uses generic CONFIG_STATUS, never the mutating FPGA_CONFIG_ISDONE.
// Malformed/error responses retire the client: it never retries an ambiguous
// firmware request or releases its arena for another owner.
func (c *Client) Probe() (o Observation, err error) {
	if c == nil {
		return o, errors.New("SDM: no service client")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retired || c.invoke == nil {
		return o, errors.New("SDM: service retired; reboot required")
	}
	defer func() {
		if err != nil {
			c.retired = true
		}
	}()
	if err = c.command(configStatus, 6); err != nil {
		return Observation{}, err
	}
	o.State, o.Version, o.Pins, o.Soft = c.response[0], c.response[1], c.response[2], c.response[3]
	o.ErrorLocation, o.ErrorDetails = c.response[4], c.response[5]
	if err = c.command(getIDCode, 1); err != nil {
		return Observation{}, err
	}
	o.IDCode = c.response[0]
	if o.IDCode == 0 || o.IDCode == 0xffffffff {
		return Observation{}, errors.New("SDM: invalid silicon IDCODE")
	}
	return o, nil
}

func (c *Client) command(command, words uint64) error {
	clear(c.response)
	// TF-A validates the argument address even when argument length is zero.
	// Always reserve a valid DDR address rather than passing NULL or a stack.
	r := c.invoke([7]uint64{MailboxService, command, c.address + 32, 0, 0, c.address, 8})
	if r[0] != 0 || r[1] != 0 || r[2] != words {
		return fmt.Errorf("SDM command %#x: service=%#x mailbox=%#x words=%d (want %d)", command, r[0], r[1], r[2], words)
	}
	return nil
}
