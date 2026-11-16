// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"errors"
	"testing"
)

func retainedReservationFixture(t *testing.T) (*ConfigurationArena, *Region, []byte) {
	t.Helper()
	memory := make([]byte, 8<<20)
	a, err := NewRetainedProgramArena(0x94800000, memory, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.Reserve([]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	source, mapping := bindingFixture(t)
	header, err := encodeProgramBinding(source, mapping)
	if err != nil || r.PublishConfiguration(header) != nil || r.Release() != nil {
		t.Fatal("retained fixture publication/completion failed", err)
	}
	return a, r, memory
}

func TestRetainedSuccessorCopiesOnlyAfterSecureRelease(t *testing.T) {
	a, previous, memory := retainedReservationFixture(t)
	old := bytes.Clone(memory)
	nextBytes := []byte{7, 8, 9, 10, 11}
	calls := 0
	next, err := previous.reserveNext(nextBytes, func() error {
		calls++
		if !bytes.Equal(memory, old) {
			t.Fatal("successor copied before secure release")
		}
		// An injected transport must not change which successor was admitted.
		nextBytes[0] = 99
		return nil
	})
	if err != nil || calls != 1 || !bytes.Equal(next.Bytes, []byte{7, 8, 9, 10, 11}) || next.Address != previous.Address {
		t.Fatal("successor reservation failed", err, calls)
	}
	if next == previous || a.active != next || a.ready || !a.retained {
		t.Fatal("successor lost retained lease identity")
	}
	if err := previous.Release(); err == nil {
		t.Fatal("stale transfer completion released the successor")
	}
	before := bytes.Clone(memory)
	if _, err := previous.reserveNext([]byte{9}, func() error { t.Fatal("stale release reached firmware"); return nil }); err == nil {
		t.Fatal("stale source release replayed")
	}
	if _, err := a.Reserve([]byte{9}); err == nil || !bytes.Equal(memory, before) {
		t.Fatal("public reservation bypassed retained successor")
	}
}

func TestRetainedSourceReleaseFailuresKeepTheArenaPinned(t *testing.T) {
	for _, name := range []string{"transport", "source-before", "metadata-before", "source-during", "metadata-during", "mapping-before", "mapping-after", "mapping-copy"} {
		t.Run(name, func(t *testing.T) {
			a, previous, memory := retainedReservationFixture(t)
			calls, barriers := 0, 0
			if name == "source-before" {
				memory[0] ^= 1
			}
			if name == "metadata-before" {
				a.metadata[0] ^= 1
			}
			a.barrier = func() error {
				barriers++
				if name == "mapping-before" && barriers == 1 || name == "mapping-after" && barriers == 2 || name == "mapping-copy" && barriers == 3 {
					return errors.New("mapping changed")
				}
				return nil
			}
			before := bytes.Clone(memory)
			_, err := previous.reserveNext([]byte{8, 7, 6, 5}, func() error {
				calls++
				if !bytes.Equal(memory, before) {
					t.Fatal("new source copied before release")
				}
				if name == "source-during" {
					memory[0] ^= 1
				}
				if name == "metadata-during" {
					a.metadata[0] ^= 1
				}
				if name == "transport" {
					return errors.New("unknown secure call outcome")
				}
				return nil
			})
			if err == nil || !a.failed || a.active != previous {
				t.Fatal("uncertain release did not pin predecessor", err)
			}
			if name == "mapping-before" || name == "source-before" || name == "metadata-before" {
				if calls != 0 {
					t.Fatal("invalid predecessor reached firmware")
				}
			} else if calls != 1 {
				t.Fatal("secure release not issued exactly once", calls)
			}
			after := bytes.Clone(memory)
			if _, err := previous.reserveNext([]byte{9}, func() error { t.Fatal("failed release retried"); return nil }); err == nil {
				t.Fatal("failed source release accepted a retry")
			}
			if _, err := a.Reserve([]byte{9}); err == nil || !bytes.Equal(memory, after) {
				t.Fatal("uncertain source was overwritten")
			}
			if name != "source-during" && name != "metadata-during" && !bytes.Equal(memory, before) {
				t.Fatal("failed release changed retained bytes")
			}
		})
	}
}

func TestRetainedSuccessorCopyFailureKeepsItsNewLeasePinned(t *testing.T) {
	a, previous, memory := retainedReservationFixture(t)
	barriers, calls := 0, 0
	a.barrier = func() error {
		barriers++
		if barriers == 4 {
			return errors.New("mapping changed after successor copy")
		}
		return nil
	}
	next := []byte{8, 7, 6, 5}
	if _, err := previous.reserveNext(next, func() error { calls++; return nil }); err == nil || calls != 1 {
		t.Fatal("uncertain copy passed or retried release", err, calls)
	}
	if !a.failed || a.active == nil || a.active == previous || !bytes.Equal(memory[:len(next)], next) {
		t.Fatal("uncertain successor storage was lost or rolled back")
	}
	before := bytes.Clone(memory)
	if _, err := a.Reserve([]byte{9}); err == nil || !bytes.Equal(memory, before) {
		t.Fatal("uncertain successor was reused")
	}
}
