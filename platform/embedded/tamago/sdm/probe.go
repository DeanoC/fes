// SPDX-License-Identifier: GPL-3.0-or-later

// Package sdm implements read-only Agilex SDM discovery through TF-A's SiP
// mailbox service. Configuration status is not a core identity or DMA proof.
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

func (o Observation) Configured() bool {
	return o.State == 0 && o.Pins&(1<<31) != 0 && o.Soft&3 == 3 && o.Soft&(1<<3) == 0
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
