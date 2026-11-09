// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"

	gnet "github.com/usbarmory/go-net"
)

type linkFailure struct{ err error }

// Link connects the pinned Lneto IP stack to Ethernet. It installs the TX callback
// before Configure starts stack workers. One receive owner calls Start.
type Link struct {
	stack  gnet.Stack
	device gnet.NetworkDevice
	state  *linkState
}

// Handles may be copied; all mutable ownership remains in this private object.
type linkState struct {
	failure atomic.Pointer[linkFailure]
	io      sync.RWMutex
	paused  atomic.Bool
	claimed atomic.Bool
}

func NewLink(stack *gnet.LnetoStack, device gnet.NetworkDevice, address, mac, gateway string) (*Link, error) {
	if stack == nil || device == nil {
		return nil, errors.New("network: missing stack/device")
	}
	prefix, err := netip.ParsePrefix(address)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() < 1 || prefix.Bits() > 30 || !usableHost(prefix, prefix.Addr()) {
		return nil, errors.New("network: invalid link IPv4 prefix")
	}
	hardware, err := net.ParseMAC(mac)
	if err != nil || len(hardware) != 6 || hardware[0]&1 != 0 || hardware.String() == "00:00:00:00:00:00" {
		return nil, errors.New("network: invalid link MAC")
	}
	var gw netip.Addr
	if gateway != "" {
		if _, err = staticConfig(address, gateway); err != nil {
			return nil, err
		}
		gw = netip.MustParseAddr(gateway)
	}
	link := &Link{stack: stack, device: device, state: &linkState{}}
	stack.SetWriteNotify(link.transmit)
	if err = stack.Configure(hardware, prefix, gw); err != nil {
		return nil, err
	}
	return link, nil
}
func (l *Link) Err() error {
	if l == nil || l.state == nil {
		return ErrControl
	}
	if failure := l.state.failure.Load(); failure != nil {
		return failure.err
	}
	if device, ok := l.device.(interface{ Err() error }); ok {
		return device.Err()
	}
	return nil
}
func (l *Link) fail(err error) { l.state.failure.CompareAndSwap(nil, &linkFailure{err: err}) }
func (l *Link) transmit(buf []byte) {
	if !l.enter() {
		return
	}
	defer l.state.io.RUnlock()
	if len(buf) == 0 {
		buf = make([]byte, 1518)
	}
	n, err := l.stack.WriteOutboundPacket(buf)
	if err != nil {
		l.fail(err)
		return
	}
	if n < 0 || n > len(buf) {
		l.fail(errors.New("network: stack returned invalid frame length"))
		return
	}
	if n < 14 {
		return
	}
	if err = l.device.Transmit(buf[:n]); err != nil {
		l.fail(err)
	}
}
func (l *Link) Start(ctx context.Context) error {
	defer func() {
		if err := ctx.Err(); err != nil {
			l.fail(err)
		}
	}()
	buf := make([]byte, 1518)
	for ctx.Err() == nil {
		if err := l.Err(); err != nil {
			return err
		}
		if !l.enter() {
			runtime.Gosched()
			continue
		}
		n, err := l.device.Receive(buf)
		l.state.io.RUnlock()
		if err != nil {
			l.fail(err)
			return err
		}
		if n < 0 || n > len(buf) {
			err := errors.New("network: device returned invalid frame length")
			l.fail(err)
			return err
		}
		if n == 0 {
			runtime.Gosched()
			continue
		}
		// Invalid remote packets are rejected by the stack, not a device failure.
		_ = l.stack.RecvInboundPacket(buf[:n])
	}
	return ctx.Err()
}
