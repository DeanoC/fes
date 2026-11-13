// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestRetainedProgramNeverReusesCompletedOrFailedSource(t *testing.T) {
	for _, failCompletion := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "failed"}[failCompletion], func(t *testing.T) {
			memory := make([]byte, 8<<20)
			calls := 0
			a, err := NewRetainedProgramArena(0x94800000, memory, func() error { calls++; return nil })
			if err != nil {
				t.Fatal(err)
			}
			r, err := a.Reserve([]byte{1, 2, 3, 4})
			if err != nil {
				t.Fatal(err)
			}
			source, binding := bindingFixture(t)
			header, err := encodeProgramBinding(source, binding)
			if err != nil || r.PublishConfiguration(header) != nil {
				t.Fatal("publication failed", err)
			}
			if failCompletion {
				a.barrier = func() error { calls++; return errors.New("mapping changed") }
			}
			err = r.Release()
			if (err != nil) != failCompletion {
				t.Fatal("wrong completion result", err)
			}
			before := bytes.Clone(memory)
			oldCalls := calls
			if _, err = a.Reserve([]byte{9, 8, 7, 6}); err == nil {
				t.Fatal("retained source reused")
			}
			if calls != oldCalls || !bytes.Equal(memory, before) {
				t.Fatal("rejected reuse changed retained storage")
			}
			scope, request := tapePublicationRequest(512)
			caller := transferHashCaller(func(_ [7]uint64) ([4]uint64, error) {
				t.Fatal("retained tape reached firmware")
				return [4]uint64{}, nil
			})
			if _, err = a.PublishInitialTape(context.Background(), caller, scope, request); err == nil {
				t.Fatal("tape overwrote retained source")
			}
			if calls != oldCalls || !bytes.Equal(memory, before) {
				t.Fatal("rejected tape changed retained storage")
			}
		})
	}
}
