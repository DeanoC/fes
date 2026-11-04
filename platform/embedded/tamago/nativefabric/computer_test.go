// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

type computerCPU struct {
	state                                  BridgeSnapshot
	digest                                 [32]byte
	port                                   computer.IO
	words                                  [16]uint16
	gpo, gpi, status                       uint32
	fetches, fetchAddress                  uint32
	calls, reads, writes, opens, closes    int
	stall, loseOpen, loseClose, badClosure bool
	changeAfterRead                        bool
	changeOnFetch                          bool
	keyboardRows                           [9]uint16
	keyboardWrites, failKeyboardAt         int
}

func (c *computerCPU) Invoke(r [7]uint64) ([4]uint64, error) {
	c.calls++
	if r[0] != uint64(g.FesEmbeddedBridgeCall) || r[3] != uint64(g.FesEmbeddedBridgeCookie) || r[5] != 0 || r[6] != 0 {
		return [4]uint64{}, errors.New("unexpected native channel")
	}
	flags := c.state.ResetMask | uint32(c.state.Capabilities.FabricHandshake)<<g.FesEmbeddedBridgeFabricShift
	if c.state.NeverEnabled {
		flags |= g.FesEmbeddedBridgeNeverEnabled
	}
	if c.state.MastersContained {
		flags |= g.FesEmbeddedBridgeMastersContained
	}
	reply := func() [4]uint64 {
		return [4]uint64{0, uint64(g.FesEmbeddedBridgeTag)<<32 | uint64(flags), c.state.Owner, c.state.Generation}
	}
	if r[1] == uint64(g.FesEmbeddedBridgeSnapshot) {
		return reply(), nil
	}
	if r[2] != c.state.Owner || r[4] != c.state.Generation {
		return [4]uint64{}, errors.New("stale computer owner/generation")
	}
	if r[1] == uint64(g.FesEmbeddedBridgeContextPart0) || r[1] == uint64(g.FesEmbeddedBridgeContextPart1) {
		if c.state.ResetMask != 15 {
			return [4]uint64{}, errors.New("context while control open")
		}
		part := int(r[1] - uint64(g.FesEmbeddedBridgeContextPart0))
		var a, b uint64
		for i := range 8 {
			a = a<<8 | uint64(c.digest[part*16+i])
			b = b<<8 | uint64(c.digest[part*16+8+i])
		}
		return [4]uint64{0, uint64(g.FesEmbeddedBridgeContextTag)<<32 | uint64(part), a, b}, nil
	}
	switch r[1] {
	case uint64(g.FesEmbeddedBridgeControl):
		c.opens++
		if c.loseOpen {
			return [4]uint64{}, errors.New("lost open")
		}
		if c.state.ResetMask != 15 {
			return [4]uint64{}, errors.New("open was not closed")
		}
		c.state.Generation++
		c.state.ResetMask = 13
		c.state.NeverEnabled = false
		flags = c.state.ResetMask | uint32(c.state.Capabilities.FabricHandshake)<<g.FesEmbeddedBridgeFabricShift | g.FesEmbeddedBridgeMastersContained
		return reply(), nil
	case uint64(g.FesEmbeddedBridgeClose | g.FesEmbeddedBridgeRequestHeld):
		c.closes++
		if c.loseClose {
			return [4]uint64{}, errors.New("lost close")
		}
		if c.state.ResetMask != 13 {
			return [4]uint64{}, errors.New("close was not control-only")
		}
		c.state.Generation++
		c.state.ResetMask = 15
		flags = 15 | uint32(c.state.Capabilities.FabricHandshake)<<g.FesEmbeddedBridgeFabricShift | g.FesEmbeddedBridgeMastersContained | g.FesEmbeddedBridgeProducersQuiescent | g.FesEmbeddedBridgeDataHeld
		if c.badClosure {
			flags &^= g.FesEmbeddedBridgeDataHeld
		}
		return reply(), nil
	default:
		return [4]uint64{}, errors.New("computer attempted unqualified data release or configuration")
	}
}
func (c *computerCPU) ReadPhysical32(address uint64) (uint32, error) {
	c.reads++
	if c.state.ResetMask != 13 || address < 0x20010300 || address >= 0x20010320 {
		return 0, errors.New("invalid computer physical aperture")
	}
	if c.changeAfterRead {
		c.state.Generation++
	}
	offset := uint32(address - 0x20010300)
	if c.changeOnFetch && offset == cg.FesDe25ComputerFetchCountOffset {
		c.state.Generation++
	}
	if c.port != nil {
		return c.port.Read32(offset)
	}
	switch offset {
	case cg.FesDe25ComputerIdOffset:
		return cg.FesDe25ComputerIdentity, nil
	case cg.FesDe25ComputerVersionOffset:
		return cg.FesDe25ComputerVersion, nil
	case cg.FesDe25ComputerStatusOffset:
		return c.status, nil
	case cg.FesDe25ComputerGpoOffset:
		return c.gpo, nil
	case cg.FesDe25ComputerGpiOffset:
		return c.gpi, nil
	case cg.FesDe25ComputerFaultOffset:
		return 0, nil
	case cg.FesDe25ComputerFetchCountOffset:
		return c.fetches, nil
	case cg.FesDe25ComputerFetchAddressOffset:
		return c.fetchAddress, nil
	}
	return 0, errors.New("undefined computer CSR")
}
func (c *computerCPU) WritePhysical32(address uint64, word uint32) error {
	c.writes++
	if c.state.ResetMask != 13 || address != 0x2001030c {
		return errors.New("invalid computer write")
	}
	if c.port != nil {
		return c.port.Write32(cg.FesDe25ComputerGpoOffset, word)
	}
	if word>>24&127 == cg.FesComputerOpcodeKeyboardHid {
		c.keyboardWrites++
		if c.failKeyboardAt != 0 && c.keyboardWrites == c.failKeyboardAt {
			return errors.New("lost keyboard write")
		}
		row := word >> 16 & 255
		if row >= 9 {
			return errors.New("invalid keyboard row")
		}
		c.keyboardRows[row] = uint16(word)
	}
	c.gpo = word
	if c.stall {
		c.status = 3
		return nil
	}
	c.gpi = cg.FesComputerSignature
	if word&cg.FesComputerRequestMask != 0 {
		c.gpi |= cg.FesComputerAckMask
	}
	if word>>24&127 == cg.FesComputerOpcodeIdentity {
		c.gpi |= uint32(c.words[word>>16&255])
	}
	return nil
}

func computerFixture(t *testing.T, build string) (*HPSContextVerifier, *computerCPU, []byte, []byte) {
	t.Helper()
	raw := hpsContextFixture(t)
	control := controlMapFixture(t)
	mapping, err := os.ReadFile("testdata/hps-computer.json")
	if err != nil {
		t.Fatal(err)
	}
	if build != "" {
		for _, p := range []*[]byte{&raw, &control, &mapping} {
			var fields map[string]any
			if err := json.Unmarshal(*p, &fields); err != nil {
				t.Fatal(err)
			}
			fields["build_id"] = build
			*p, _ = json.Marshal(fields)
			*p = append(*p, '\n')
		}
		var fields map[string]any
		_ = json.Unmarshal(mapping, &fields)
		digest := sha256.Sum256(control)
		fields["control_map_sha256"] = hex.EncodeToString(digest[:])
		mapping, _ = json.Marshal(fields)
		mapping = append(mapping, '\n')
	}
	cpu := &computerCPU{state: BridgeSnapshot{Capabilities: BridgeCapabilities{15, 2, 12, FabricHandshakeAbsent}, Owner: 17, Generation: 1, ResetMask: 15, NeverEnabled: true, MastersContained: true}, digest: sha256.Sum256(raw), status: 1, gpi: cg.FesComputerSignature}
	v, err := NewHPSContextVerifier(cpu, raw)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := hex.DecodeString(v.context.BuildID)
	cpu.words = [16]uint16{0x4546, 0x3153, 1, 0, 4, 1, 0, 0x803}
	for i := range 8 {
		cpu.words[8+i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return v, cpu, control, mapping
}

// Scope-only fixtures model an already completed compiled-source transfer.
// The joined reload fixture separately drives real ConfigurationTransfer steps.
func computerCompletionFixture(v *HPSContextVerifier, cpu *computerCPU) *ConfigurationTransfer {
	return &ConfigurationTransfer{done: true, phase: "complete", source: []byte{1}, accepted: 1, offset: 1,
		binding: &configurationBinding{verifier: v, source: HPSBootObservation{Digest: v.digest, Bridges: cpu.state}, rearm: !cpu.state.NeverEnabled}}
}

func computerLimits() computer.Limits {
	return computer.Limits{Timeout: time.Second, MaxPolls: 4, Now: func() time.Time { return time.Unix(100, 0) }, Yield: func() {}}
}

func TestNativeComputerScopeClosesAndRevokesIO(t *testing.T) {
	v, cpu, control, mapping := computerFixture(t, "")
	completed := computerCompletionFixture(v, cpu)
	s, err := v.OpenComputer(context.Background(), control, mapping, completed, computerLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Computer().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Computer().Keyboard(context.Background(), [9]uint16{1: 16}); err != nil {
		t.Fatal(err)
	}
	out, err := s.Close(context.Background())
	if err != nil || !out.HoldAcknowledged || out.After.Generation != 3 || cpu.opens != 1 || cpu.closes != 1 || cpu.state.ResetMask != 15 {
		t.Fatal(out, err, cpu)
	}
	before := cpu.calls
	writes := cpu.writes
	if err := s.Computer().Run(context.Background()); err == nil {
		t.Fatal("closed client retained IO")
	}
	if _, err := s.Close(context.Background()); err == nil {
		t.Fatal("close replayed")
	}
	if _, err := v.OpenComputer(context.Background(), control, mapping, completed, computerLimits()); err == nil {
		t.Fatal("open replayed")
	}
	if cpu.calls != before || cpu.writes != writes {
		t.Fatal("retired session touched firmware or registers")
	}
	if _, ok := any(v).(BootHandoffVerifier); ok {
		t.Fatal("computer scope became ordinary runtime admission")
	}
}

func TestNativeComputerActivityKeepsOwnershipAndRevocation(t *testing.T) {
	for _, loseOwnership := range []bool{false, true} {
		v, cpu, control, mapping := computerFixture(t, "")
		s, err := v.OpenComputer(context.Background(), control, mapping, computerCompletionFixture(v, cpu), computerLimits())
		if err != nil {
			t.Fatal(err)
		}
		cpu.fetches, cpu.fetchAddress = 37, 0x1234
		writes := cpu.writes
		observed, err := s.Computer().Activity(context.Background())
		if err != nil || observed != (computer.FetchActivity{Cycles: 37, Address: 0x1234}) || cpu.writes != writes {
			t.Fatal(observed, err, cpu)
		}
		if loseOwnership {
			cpu.changeOnFetch = true
			if _, err := s.Computer().Activity(context.Background()); err == nil {
				t.Fatal("activity accepted ownership loss during its count read")
			}
		} else if _, err := s.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		calls, reads, writes := cpu.calls, cpu.reads, cpu.writes
		if _, err := s.Computer().Activity(context.Background()); err == nil {
			t.Fatal("retired session observed activity")
		}
		if cpu.calls != calls || cpu.reads != reads || cpu.writes != writes {
			t.Fatal("retired observation touched firmware or physical registers")
		}
	}
}

func TestNativeComputerFailuresKeepAdmissionClosedAndDoNotReplay(t *testing.T) {
	for _, mode := range []string{"wrong-build", "changed-owner", "lost-open", "ack-timeout", "lost-close", "bad-closure"} {
		t.Run(mode, func(t *testing.T) {
			v, cpu, control, mapping := computerFixture(t, "")
			switch mode {
			case "wrong-build":
				cpu.words[15] ^= 1
			case "changed-owner":
				cpu.changeAfterRead = true
			case "lost-open":
				cpu.loseOpen = true
			}
			s, err := v.OpenComputer(context.Background(), control, mapping, computerCompletionFixture(v, cpu), computerLimits())
			if mode == "wrong-build" || mode == "changed-owner" || mode == "lost-open" {
				if err == nil || s != nil {
					t.Fatal("uncertain open accepted")
				}
				if mode == "wrong-build" && (cpu.state.ResetMask != 15 || cpu.closes != 1 || cpu.writes != 16) {
					t.Fatal("identity rejection did not reclose without a Hold write", cpu)
				}
				if mode == "changed-owner" && cpu.reads != 1 {
					t.Fatal("ownership error repeated a read")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "ack-timeout" {
				cpu.stall = true
				if err := s.Computer().Run(context.Background()); !errors.Is(err, computer.ErrLimit) {
					t.Fatal(err)
				}
			}
			cpu.loseClose = mode == "lost-close"
			cpu.badClosure = mode == "bad-closure"
			writes := cpu.writes
			out, err := s.Close(context.Background())
			if err == nil {
				t.Fatal("failed return reported success")
			}
			if mode == "ack-timeout" && (out.HoldAcknowledged || cpu.writes != writes || out.After.ResetMask != 15) {
				t.Fatal("uncertain command replayed or lost containment", out)
			}
			if mode != "ack-timeout" && out.After != (BridgeSnapshot{}) {
				t.Fatal("unverified closure published a receipt", out)
			}
			calls := cpu.calls
			_, _ = s.Close(context.Background())
			if cpu.calls != calls || cpu.closes != 1 {
				t.Fatal("closure retried")
			}
		})
	}
}

func TestNativeComputerClosureRejectsNewRunBeforeItsFinalHoldCanRace(t *testing.T) {
	v, cpu, control, mapping := computerFixture(t, "")
	s, err := v.OpenComputer(context.Background(), control, mapping, computerCompletionFixture(v, cpu), computerLimits())
	if err != nil {
		t.Fatal(err)
	}
	s.port.beginClose()
	writes := cpu.writes
	if err := s.Computer().Run(context.Background()); err == nil || cpu.writes != writes {
		t.Fatal("closing scope accepted Run", err)
	}
	out, err := s.Close(context.Background())
	if err == nil || out.HoldAcknowledged || out.After.ResetMask != 15 || cpu.writes != writes {
		t.Fatal("racing closure fabricated a Hold receipt", out, err)
	}
}

func TestNativeComputerRejectsUncompiledMapBeforeFirmware(t *testing.T) {
	for _, key := range []string{"cpu_base", "span", "scope", "package_id", "build_id", "control_map_sha256", "handoff_sha256", "abi_minor", "capabilities", "hps_master", "endpoint_sha256", "wrapper_sha256", "missing", "duplicate", "unknown"} {
		t.Run(key, func(t *testing.T) {
			v, cpu, control, mapping := computerFixture(t, "")
			var m map[string]any
			_ = json.Unmarshal(mapping, &m)
			switch key {
			case "cpu_base", "span", "abi_minor", "capabilities":
				m[key] = 123
			case "hps_master":
				m[key] = true
			case "missing":
				delete(m, "abi_minor")
			case "unknown":
				m[key] = 1
			default:
				m[key] = "invalid"
			}
			mapping, _ = json.Marshal(m)
			mapping = append(mapping, '\n')
			if key == "duplicate" {
				mapping = append([]byte("{\"format\":1,"), mapping[1:]...)
			}
			if _, err := v.OpenComputer(context.Background(), control, mapping, computerCompletionFixture(v, cpu), computerLimits()); err == nil || cpu.calls != 0 || cpu.reads != 0 {
				t.Fatal("uncompiled map touched firmware", err, cpu.calls)
			}
		})
	}
}

func TestSelectedComputerRTLUsesOwnedNativeScope(t *testing.T) {
	executable := os.Getenv("FES_COMPUTER_MODEL")
	if executable == "" {
		t.Skip("run computer/verify.py with an explicit captured project")
	}
	v, cpu, control, mapping := computerFixture(t, os.Getenv("FES_COMPUTER_BUILD_ID"))
	p := &producedProbe{command: exec.Command(executable)}
	p.command.Dir = os.Getenv("FES_COMPUTER_MODEL_DIRECTORY")
	var err error
	p.input, err = p.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := p.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.output = bufio.NewReader(stdout)
	p.command.Stderr = os.Stderr
	if err := p.command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.input.Close()
		if err := p.command.Wait(); err != nil {
			t.Error(err)
		}
	})
	cpu.port = p
	limit := computerLimits()
	limit.Timeout = 20 * time.Second
	limit.Now = time.Now
	s, err := v.OpenComputer(context.Background(), control, mapping, computerCompletionFixture(v, cpu), limit)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Computer().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.call("advance %x", 100000); err != nil {
		t.Fatal(err)
	}
	state, err := p.call("inspect")
	if err != nil || len(state) != 5 || state[0] != 0 || state[1] < 100 {
		t.Fatal(state, err)
	}
	active, err := s.Computer().Activity(context.Background())
	if err != nil || active.Cycles == 0 {
		t.Fatal("owned native scope missed actual fetches", active, err)
	}
	if err := s.Computer().Hold(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.call("advance %x", 1000); err != nil {
		t.Fatal(err)
	}
	held, err := s.Computer().Activity(context.Background())
	if err != nil || held.Cycles < active.Cycles {
		t.Fatal("native Hold erased fetch observations", active, held, err)
	}
	if _, err := p.call("advance %x", 10000); err != nil {
		t.Fatal(err)
	}
	next, err := s.Computer().Activity(context.Background())
	if err != nil || next != held {
		t.Fatal("native held fetches continued", held, next, err)
	}
	if err := s.Computer().LoadTape(context.Background(), []byte{1, 2, 3, 4, 5}); err != nil {
		t.Fatal(err)
	}
	if err := s.Computer().EjectTape(context.Background()); err != nil {
		t.Fatal(err)
	}
	out, err := s.Close(context.Background())
	if err != nil || !out.HoldAcknowledged || out.After.ResetMask != 15 {
		t.Fatal(out, err)
	}
}
