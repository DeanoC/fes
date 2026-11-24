// SPDX-License-Identifier: GPL-3.0-or-later
package usb

import "time"

// Speed matches USB 2.0 root/hub port speeds (and DWC2 HPRT.PRTSPD encoding).
type Speed uint8

const (
	SpeedHigh Speed = 0 // 480 Mbps
	SpeedFull Speed = 1 // 12 Mbps
	SpeedLow  Speed = 2 // 1.5 Mbps
)

// BusDevice describes an enumerated non-hub USB device attached to a BusHost.
type BusDevice struct {
	Address    uint8
	Speed      Speed
	ParentHub  uint8 // 0 for directly connected root port devices
	ParentPort uint8 // 1..15 when attached behind a hub
	Descriptor DeviceDescriptor
	Config     FullConfiguration
}

// HostEventKind identifies a dynamic hotplug topology transition.
type HostEventKind uint8

const (
	DeviceAttached HostEventKind = iota + 1
	DeviceDetached
)

// HostEvent reports an enumerated device attach or disconnect.
type HostEvent struct {
	Kind   HostEventKind
	Device BusDevice
}

// Pipe is an allocated hardware channel bound to one non-EP0 endpoint on an
// enumerated device. PollInto and Write are designed for zero-allocation hot
// paths in HID, XInput/GIP, and Bluetooth HCI drivers.
type Pipe interface {
	DeviceAddress() uint8
	Endpoint() EndpointDescriptor
	// PollInto copies a completed IN payload into dst and returns its byte count.
	// Returns (0, nil) when no new payload is ready.
	PollInto(dst []byte) (int, error)
	// Write submits an OUT payload (Interrupt OUT or Bulk OUT) up to MPS.
	Write(data []byte) error
	// WriteDone reports whether the in-flight OUT transfer has completed.
	WriteDone() (bool, error)
	// SetIntervalOverride overrides polling interval for Interrupt-IN endpoints
	// (for example 1 ms = 1000 Hz gaming override). Zero restores descriptor rate.
	SetIntervalOverride(interval time.Duration)
	// Close halts and releases this pipe's hardware channel without affecting
	// other pipes or devices.
	Close() error
}

// BusHost is the controller-independent multi-device, hotplug-capable USB host
// interface implemented by dwc2.MultiHost (and mock transports in tests).
type BusHost interface {
	// Service drains RX FIFOs, advances active channels, re-arms due periodic/bulk
	// transfers, and processes root/hub hotplug port changes.
	Service() error
	// NextEvent pops the next attach/detach event, if any.
	NextEvent() (HostEvent, bool)
	// Devices returns a snapshot of currently attached non-hub devices.
	Devices() []BusDevice
	// ControlTransfer executes a bounded synchronous EP0 control transfer on addr.
	// When reqType&0x80 != 0 (IN), it reads up to len(data) bytes into data.
	// When reqType&0x80 == 0 (OUT), it sends data in the DATA OUT stage.
	ControlTransfer(addr uint8, reqType, req byte, value, index uint16, data []byte) (int, error)
	// OpenPipe allocates a dedicated hardware channel for ep on addr.
	OpenPipe(addr uint8, ep EndpointDescriptor) (Pipe, error)
	// Ready returns the interrupt wakeup channel when IRQ mode is enabled, or nil.
	Ready() <-chan struct{}
}

// FindInterface returns the first interface in cfg matching class/subclass/protocol.
// A negative filter value acts as a wildcard.
func (c FullConfiguration) FindInterface(class, subClass, protocol int) (InterfaceDescriptor, bool) {
	for _, iface := range c.Interfaces {
		if iface.AlternateSetting != 0 {
			continue
		}
		if class >= 0 && int(iface.Class) != class {
			continue
		}
		if subClass >= 0 && int(iface.SubClass) != subClass {
			continue
		}
		if protocol >= 0 && int(iface.Protocol) != protocol {
			continue
		}
		return iface, true
	}
	return InterfaceDescriptor{}, false
}

// FindEndpoint returns the first endpoint on iface matching transfer type and direction.
func (i InterfaceDescriptor) FindEndpoint(tt TransferType, in bool) (EndpointDescriptor, bool) {
	for _, ep := range i.Endpoints {
		if ep.TransferType == tt && ep.In() == in {
			return ep, true
		}
	}
	return EndpointDescriptor{}, false
}
