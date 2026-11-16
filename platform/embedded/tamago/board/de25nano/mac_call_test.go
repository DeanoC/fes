// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

func TestMACTransportRejectsBeforeInvocation(t *testing.T) {
	base := [7]uint64{uint64(g.FesEmbeddedMacCall), uint64(g.FesEmbeddedMacAcquire), 17, uint64(g.FesEmbeddedMacCookie), 7}
	for _, tc := range []struct {
		name  string
		index int
		value uint64
	}{
		{"foreign-call", 0, uint64(g.FesEmbeddedUsbCall)},
		{"unknown-operation", 1, 4}, {"wide-operation", 1, 1 << 32},
		{"wrong-cookie", 3, 0}, {"wide-cookie", 3, uint64(g.FesEmbeddedMacCookie) | 1<<32},
		{"reserved-five", 5, 1}, {"reserved-six", 6, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			request[tc.index] = tc.value
			called := false
			result, err := invokeMAC(request, true, func([7]uint64) ([4]uint64, error) { called = true; return [4]uint64{1}, nil })
			if err == nil || called || result != [4]uint64{} {
				t.Fatal("unselected protocol reached firmware", result, err, called)
			}
		})
	}
	for _, selected := range []bool{false, true} {
		var call func([7]uint64) ([4]uint64, error)
		if !selected {
			call = func([7]uint64) ([4]uint64, error) {
				t.Fatal("disabled profile invoked firmware")
				return [4]uint64{}, nil
			}
		}
		if _, err := invokeMAC(base, selected, call); err == nil {
			t.Fatal("disabled profile/missing transport accepted")
		}
	}
}

func TestMACTransportPreservesOwnerGenerationAndReply(t *testing.T) {
	for op := uint64(g.FesEmbeddedMacQuery); op <= uint64(g.FesEmbeddedMacStop); op++ {
		request := [7]uint64{uint64(g.FesEmbeddedMacCall), op, 0x123456789abcdef0, uint64(g.FesEmbeddedMacCookie), 0xfedcba9876543210}
		want := [4]uint64{5, uint64(g.FesEmbeddedMacTag) << 32, 17, 9}
		wireErr := errors.New("injected transport loss")
		result, err := invokeMAC(request, true, func(got [7]uint64) ([4]uint64, error) {
			if got != request {
				t.Fatal("request changed", got, request)
			}
			return want, wireErr
		})
		if result != want || !errors.Is(err, wireErr) {
			t.Fatal("transport rewrote firmware observation", result, err)
		}
	}
}
