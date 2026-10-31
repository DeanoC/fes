// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"testing"
)

type bridgeFirmwareFunc func([7]uint64) ([4]uint64, error)

func (f bridgeFirmwareFunc) Invoke(r [7]uint64) ([4]uint64, error) { return f(r) }

func TestFirmwareBridgesUsePrivateReadOnlyProbeAndDecodeOwnedReplies(t *testing.T) {
	requests := [][7]uint64{
		{0xc2000301, 0, 0, 0x46455342},
		{0xc2000301, 0x101, 17, 0x46455342, 1},
		{0xc2000301, 2, 17, 0x46455342, 2},
	}
	replies := [][4]uint64{{0, 0x46424f3100000020, 17, 1}, {0, 0x46424f31000033af, 17, 2}, {0, 0x46424f31000000ad, 17, 3}}
	count := 0
	cpu := bridgeFirmwareFunc(func(r [7]uint64) ([4]uint64, error) {
		if count >= len(requests) || r != requests[count] {
			t.Fatalf("unexpected bridge request: %x", r)
		}
		out := replies[count]
		count++
		return out, nil
	})
	b, err := NewFirmwareBridges(cpu)
	if err != nil {
		t.Fatal(err)
	}
	before := bridgeState(FabricHandshakePresent)
	before.Owner = 17
	c, err := b.CloseBridges(BridgeCloseRequest{Before: before, RequireFabricHandshake: true})
	if err != nil || c.HardwareDrainedMask != 12 || !c.FabricAcknowledged || !c.ProducersQuiescent || c.DataRemainedContained {
		t.Fatal(c, err)
	}
	s, err := b.OpenBridges(BridgeOpenRequest{Before: c.After, Mask: 2})
	if err != nil || s.ResetMask != 13 || !s.MastersContained || count != 3 {
		t.Fatal(s, err)
	}
}

func TestFirmwareBridgesRejectUnreadyLegacyAndMalformedRepliesWithoutRetry(t *testing.T) {
	for _, reply := range [][4]uint64{
		{5, 0x46424f3100000000, 0, 0},
		{0, 0x46455343, 0x10000, 15},   // old contained query tuple
		{0xffffffff, 0, 0, 0},          // unknown private call in older firmware
		{0, 0x46424f31000001ef, 17, 1}, // close-only flags in snapshot
		{0, 0x46424f31000000ef, 0, 1},
		{0, 0x46424f31000000ef, 17, 0},
		{0, 0x46424f31000000cf, 17, 1}, // missing capability
		{0, 0x46424f31000000ee, 17, 1}, // claimed never-enabled with open bridge
		{1, 0x46424f3100000000, 0, 0},
	} {
		count := 0
		_, err := NewFirmwareBridges(bridgeFirmwareFunc(func(r [7]uint64) ([4]uint64, error) {
			count++
			if r[0] == 0xc2000032 {
				t.Fatal("query reached old mutable HPS bridge service")
			}
			return reply, nil
		}))
		if err == nil || count != 1 {
			t.Fatal("unqualified reply accepted/retried", reply, err, count)
		}
		if reply[0] == 5 && !errors.Is(err, ErrBridgeNotReady) {
			t.Fatal(err)
		}
	}
	transportError := errors.New("lost firmware return")
	_, err := NewFirmwareBridges(bridgeFirmwareFunc(func([7]uint64) ([4]uint64, error) { return [4]uint64{}, transportError }))
	if !errors.Is(err, transportError) {
		t.Fatal(err)
	}
}
