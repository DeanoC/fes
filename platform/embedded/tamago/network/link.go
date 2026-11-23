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
	"time"

	gnet "github.com/usbarmory/go-net"
)

// rxWaiter is an optional interface for devices that signal RX availability
// via an interrupt channel so Link.Start can block in WFI instead of spinning.
type rxWaiter interface {
	RXReady() <-chan struct{}
}

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
	failure   atomic.Pointer[linkFailure]
	io        sync.RWMutex
	paused    atomic.Bool
	claimed   atomic.Bool
	tapMu     sync.Mutex
	nextTapID uint64
	taps      map[uint64]func([]byte)
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

// SendRawFrame transmits one raw Ethernet frame while obeying the link's
// software pause gate and DMA failure contract.
func (l *Link) SendRawFrame(frame []byte) error {
	if l == nil || l.state == nil || l.device == nil {
		return ErrControl
	}
	if len(frame) < 14 || len(frame) > 1518 {
		return errors.New("network: invalid raw frame length")
	}
	if err := l.Available(); err != nil {
		return err
	}
	if !l.enter() {
		if err := l.Available(); err != nil {
			return err
		}
		return ErrPaused
	}
	defer l.state.io.RUnlock()
	if err := l.device.Transmit(frame); err != nil {
		l.fail(err)
		return err
	}
	return nil
}

// AddTap registers a non-blocking callback invoked with a copy of each inbound
// Ethernet frame received by Link.Start. Call the returned function to remove
// the tap (idempotent).
func (l *Link) AddTap(fn func([]byte)) func() {
	if l == nil || l.state == nil || fn == nil {
		return func() {}
	}
	l.state.tapMu.Lock()
	if l.state.taps == nil {
		l.state.taps = make(map[uint64]func([]byte))
	}
	l.state.nextTapID++
	id := l.state.nextTapID
	l.state.taps[id] = fn
	l.state.tapMu.Unlock()
	return func() {
		l.state.tapMu.Lock()
		delete(l.state.taps, id)
		l.state.tapMu.Unlock()
	}
}

func (l *Link) dispatchTaps(frame []byte) {
	l.state.tapMu.Lock()
	if len(l.state.taps) == 0 {
		l.state.tapMu.Unlock()
		return
	}
	callbacks := make([]func([]byte), 0, len(l.state.taps))
	for _, fn := range l.state.taps {
		callbacks = append(callbacks, fn)
	}
	l.state.tapMu.Unlock()
	cp := append([]byte(nil), frame...)
	for _, fn := range callbacks {
		fn(cp)
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
			if waiter, ok := l.device.(rxWaiter); ok {
				if ready := waiter.RXReady(); ready != nil {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-ready:
					case <-time.After(10 * time.Millisecond):
					}
					continue
				}
			}
			runtime.Gosched()
			continue
		}
		l.dispatchTaps(buf[:n])
		// Invalid remote packets are rejected by the stack, not a device failure.
		_ = l.stack.RecvInboundPacket(buf[:n])
	}
	return ctx.Err()
}
