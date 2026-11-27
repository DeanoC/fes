// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/internal/tapetest"
	"testing"
)

type transferHashCaller func([7]uint64) ([4]uint64, error)

func (f transferHashCaller) Invoke(r [7]uint64) ([4]uint64, error) { return f(r) }

func retainedTapeProgram(t *testing.T) (*ConfigurationArena, *Region, []byte) {
	t.Helper()
	backing := make([]byte, 8<<20)
	p, e := NewRetainedProgramArena(0x94800000, backing, func() error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	r, e := p.Reserve([]byte{1, 2, 3, 4})
	if e != nil {
		t.Fatal(e)
	}
	source, binding := bindingFixture(t)
	header, e := encodeProgramBinding(source, binding)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.PublishConfiguration(header); e != nil {
		t.Fatal(e)
	}
	if e = r.Release(); e != nil {
		t.Fatal(e)
	}
	return p, r, backing
}

func TestIndependentTapePublicationPreservesRetainedConfiguration(t *testing.T) {
	p, old, protected := retainedTapeProgram(t)
	before := bytes.Clone(protected)
	storage := bytes.Repeat([]byte{0xa5}, InitialTapeArenaBytes)
	checks, calls := 0, 0
	a, e := NewInitialTapeArena(InitialTapeArenaBase, storage, p, func() error { checks++; return nil })
	if e != nil {
		t.Fatal(e)
	}
	scope, request := tapePublicationRequest(917)
	caller := transferHashCaller(func(args [7]uint64) ([4]uint64, error) { calls++; return tapeStepReply(request, args[4], args[1]), nil })
	copyHandle := *a
	receipt, e := a.PublishInitialTape(context.Background(), caller, scope, request)
	if e != nil || receipt.Bytes != 917 || calls != 4 || checks != 3 || !a.state.ready || a.state.failed {
		t.Fatal(receipt, e, calls, checks)
	}
	if !bytes.Equal(protected, before) || !p.retained || !p.ready || p.failed || p.active != old {
		t.Fatal("publication changed or released retained program")
	}
	if !bytes.Equal(storage[InitialTapePayloadBytes:InitialTapeMetadataOffset], bytes.Repeat([]byte{0xa5}, InitialTapeMetadataOffset-InitialTapePayloadBytes)) {
		t.Fatal("publication wrote unused pinned storage")
	}
	if _, err := copyHandle.PublishInitialTape(context.Background(), caller, scope, request); err == nil || calls != 4 || checks != 3 {
		t.Fatal("copied tape handle replayed publication")
	}
	if _, e = p.Reserve([]byte{5}); e == nil {
		t.Fatal("tape completion released protected program")
	}
	for _, generation := range []uint64{scope.Generation, scope.Generation + 1} {
		s := scope
		s.Generation = generation
		if _, e = a.PublishInitialTape(context.Background(), caller, s, request); e == nil || calls != 4 || checks != 3 {
			t.Fatal("new generation bypassed completed-program lease", e)
		}
	}
	if e = old.Release(); e == nil || !a.state.ready {
		t.Fatal("old program completion changed tape lease")
	}
	next, e := old.reserveNext([]byte{5, 6, 7, 8}, func() error { return nil })
	if e != nil {
		t.Fatal(e)
	} // modeled secure release only
	source, binding := bindingFixture(t)
	header, e := encodeProgramBinding(source, binding)
	if e != nil {
		t.Fatal(e)
	}
	if e = next.PublishConfiguration(header); e != nil {
		t.Fatal(e)
	}
	if e = next.Release(); e != nil {
		t.Fatal(e)
	}
	scope.Generation++
	if _, e = a.PublishInitialTape(context.Background(), caller, scope, request); e != nil || calls != 8 {
		t.Fatal("next completed program not accepted", e)
	}
}

func TestIndependentTapeConstructorRejectsAliasesBeforeAccess(t *testing.T) {
	p, _, backing := retainedTapeProgram(t)
	checks := 0
	guard := func() error { checks++; return nil }
	before := bytes.Clone(backing)
	for _, tc := range []struct {
		address uint64
		memory  []byte
		program *ConfigurationArena
	}{
		{0x94800000, make([]byte, InitialTapeArenaBytes), p},
		{InitialTapeArenaBase, make([]byte, InitialTapeArenaBytes-1), p},
		{InitialTapeArenaBase, make([]byte, InitialTapeArenaBytes), nil},
		{InitialTapeArenaBase, backing[:InitialTapeArenaBytes], p},
		{InitialTapeArenaBase, backing[len(backing)-InitialTapeArenaBytes:], p},
	} {
		if _, e := NewInitialTapeArena(tc.address, tc.memory, tc.program, guard); e == nil {
			t.Fatal("invalid or aliased reservation accepted")
		}
	}
	if checks != 0 || !bytes.Equal(backing, before) {
		t.Fatal("constructor accessed memory or ordering hook")
	}
	scope, request := tapePublicationRequest(1)
	if _, e := (&InitialTapeArena{}).PublishInitialTape(context.Background(), transferHashCaller(nil), scope, request); e == nil {
		t.Fatal("zero publisher accepted")
	}
}

func TestIndependentTapeFailureRetainsBothMappings(t *testing.T) {
	for _, fault := range []string{"transport", "source", "metadata", "late", "incomplete"} {
		t.Run(fault, func(t *testing.T) {
			p, _, source := retainedTapeProgram(t)
			before := bytes.Clone(source)
			checks, calls := 0, 0
			a, e := NewInitialTapeArena(InitialTapeArenaBase, make([]byte, InitialTapeArenaBytes), p, func() error {
				checks++
				if fault == "late" && checks == 3 {
					return errors.New("mapping lost")
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			if fault == "incomplete" {
				p.ready = false
			}
			scope, request := tapePublicationRequest(917)
			caller := transferHashCaller(func(args [7]uint64) ([4]uint64, error) {
				calls++
				if fault == "transport" {
					return [4]uint64{}, errors.New("uncertain SMC")
				}
				if calls == 1 && fault == "source" {
					source[0] ^= 1
				}
				if calls == 1 && fault == "metadata" {
					source[len(source)-ProgramMetadataBytes] ^= 1
				}
				return tapeStepReply(request, args[4], args[1]), nil
			})
			if _, e = a.PublishInitialTape(context.Background(), caller, scope, request); e == nil {
				t.Fatal("fault accepted")
			}
			if fault == "incomplete" {
				if calls != 0 || checks != 0 || a.state.failed {
					t.Fatal("incomplete source accessed or poisoned")
				}
				return
			}
			if !a.state.failed {
				t.Fatal("uncertain tape lease not retained")
			}
			savedCalls, savedChecks := calls, checks
			if _, e = a.PublishInitialTape(context.Background(), caller, scope, request); e == nil || calls != savedCalls || checks != savedChecks {
				t.Fatal("failed tape publication replayed")
			}
			if fault == "source" || fault == "metadata" {
				if !p.failed {
					t.Fatal("changed source not retired")
				}
			} else if !bytes.Equal(source, before) || p.failed || !p.ready {
				t.Fatal("tape failure changed protected source")
			}
		})
	}
}

func TestIndependentTapeRejectsCopiedSourceOwnerAndMetadataAlias(t *testing.T) {
	p, _, _ := retainedTapeProgram(t)
	var copied ConfigurationArena
	reflect.ValueOf(&copied).Elem().Set(reflect.ValueOf(p).Elem()) // adversarial value-copy; no concurrent use
	if _, err := NewInitialTapeArena(InitialTapeArenaBase, make([]byte, InitialTapeArenaBytes), &copied, func() error { return nil }); err == nil {
		t.Fatal("copied source owner admitted")
	}
	backing := make([]byte, (8<<20)+InitialTapeArenaBytes)
	source, err := NewRetainedProgramArena(0x94800000, backing[:8<<20:8<<20], func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	alias := backing[(8<<20)-ProgramMetadataBytes : (8<<20)-ProgramMetadataBytes+InitialTapeArenaBytes]
	if _, err = NewInitialTapeArena(InitialTapeArenaBase, alias, source, func() error { return nil }); err == nil {
		t.Fatal("metadata-only alias admitted")
	}
}

func TestIndependentTapeJoinsRetainedProgramToActualCService(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "service")
	command := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I../../firmware", "../../firmware/nano_tape.c", "../../firmware/nano_tape_service.c", "../../firmware/tests/nano_tape_service_test.c", "-o", binary}
	if out, err := exec.Command("cc", command...).CombinedOutput(); err != nil {
		t.Fatalf("C fixture: %v\n%s", err, out)
	}
	for _, size := range []int{1, 512, 917, 16375} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			p, _, protected := retainedTapeProgram(t)
			before := bytes.Clone(protected)
			storage := make([]byte, InitialTapeArenaBytes)
			a, err := NewInitialTapeArena(InitialTapeArenaBase, storage, p, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			scope, request := tapePublicationRequest(size)
			caller := transferHashCaller(tapetest.Caller(t, binary, 0, func() ([]byte, []byte) { return storage[InitialTapeMetadataOffset:][:128], storage[:size] }))
			receipt, err := a.PublishInitialTape(context.Background(), caller, scope, request)
			if err != nil || computer.ValidateInitialTapeReceipt(request, receipt) != nil || !bytes.Equal(before, protected) || p.failed || !p.ready || !p.retained {
				t.Fatal("independent publication/C service or protected source differs", receipt, err)
			}
		})
	}
}
