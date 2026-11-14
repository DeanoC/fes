// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// These fixtures exercise native ownership and the real Go CSR driver. The
// separate joined firmware/RTL test covers configuration and physical draining.
type windowCPU struct {
	computerCPU
	registers                   map[uint32]uint32
	crc                         uint32
	starts, dataOpens, releases int
	disables                    int
	lastDrainCookie             uint64
	fault                       string
	after                       func(string)
}

func (c *windowCPU) Invoke(r [7]uint64) ([4]uint64, error) {
	if c.after != nil {
		defer c.after("firmware")
	}
	data := uint64(g.FesEmbeddedBridgeRelease | 8<<g.FesEmbeddedBridgeReleaseShift)
	if r[1] != data && r[1] != uint64(g.FesEmbeddedBridgeClose) {
		out, err := c.computerCPU.Invoke(r)
		if r[1] == uint64(g.FesEmbeddedBridgeControl) && c.after != nil {
			c.after("control")
		}
		return out, err
	}
	c.calls++
	if r[0] != uint64(g.FesEmbeddedBridgeCall) || r[3] != uint64(g.FesEmbeddedBridgeCookie) ||
		r[2] != c.state.Owner || r[4] != c.state.Generation || r[5] != 0 || r[6] != 0 {
		return [4]uint64{}, errors.New("changed window ownership")
	}
	flags := uint32(FabricHandshakeAbsent) << g.FesEmbeddedBridgeFabricShift
	if r[1] == data {
		c.dataOpens++
		if c.state.ResetMask != 13 {
			return [4]uint64{}, errors.New("DDR before control")
		}
		c.state.Generation++
		c.state.ResetMask = 5
		c.state.MastersContained = false
		if c.fault == "data-open" {
			return [4]uint64{}, errors.New("uncertain data opening")
		}
		flags |= 5
	} else {
		c.closes++
		if c.state.ResetMask != 5 {
			return [4]uint64{}, errors.New("unexpected active close")
		}
		if c.fault == "close" {
			return [4]uint64{}, errors.New("all-master stop failed")
		}
		c.state.Generation++
		c.state.ResetMask = 15
		c.state.MastersContained = true
		flags |= 15 | g.FesEmbeddedBridgeMastersContained | g.FesEmbeddedBridgeProducersQuiescent | 8<<g.FesEmbeddedBridgeDrainShift
		if c.fault == "wrong-drain" {
			flags |= 4 << g.FesEmbeddedBridgeDrainShift
		}
		if c.after != nil {
			c.after("closed")
		}
	}
	return [4]uint64{0, uint64(g.FesEmbeddedBridgeTag)<<32 | uint64(flags), c.state.Owner, c.state.Generation}, nil
}
func (c *windowCPU) ReadPhysical32(address uint64) (uint32, error) {
	c.reads++
	if c.state.ResetMask != 5 || address < 0x20010400 || address > 0x200104c8 {
		return 0, errors.New("unowned window read")
	}
	offset := uint32(address - 0x20010400)
	if c.after != nil {
		defer c.after("read")
	}
	if c.fault == "read" {
		return 0, errors.New("read uncertainty")
	}
	if offset == 0 && c.fault == "identity" {
		return 0, nil
	}
	return c.registers[offset], nil
}
func (c *windowCPU) WritePhysical32(address uint64, value uint32) error {
	c.writes++
	if c.state.ResetMask != 5 || address < 0x20010400 || address > 0x200104c8 {
		return errors.New("unowned window write")
	}
	offset := uint32(address - 0x20010400)
	c.registers[offset] = value
	if offset == g.FesEmbeddedRegCommand {
		generation := uint64(c.registers[g.FesEmbeddedRegRequestGenerationHi])<<32 | uint64(c.registers[g.FesEmbeddedRegRequestGenerationLo])
		if value == g.FesEmbeddedCommandEnable {
			previous := uint64(c.registers[g.FesEmbeddedRegGenerationHi])<<32 | uint64(c.registers[g.FesEmbeddedRegGenerationLo])
			if generation <= previous || c.registers[g.FesEmbeddedRegPending] != 0 ||
				(c.registers[g.FesEmbeddedRegStatus] != 0 && c.registers[g.FesEmbeddedRegStatus] != g.FesEmbeddedStatusDrained) {
				return errors.New("invalid reader reenable")
			}
			c.registers[g.FesEmbeddedRegGenerationLo] = uint32(generation)
			c.registers[g.FesEmbeddedRegGenerationHi] = uint32(generation >> 32)
			c.registers[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusEnabled
			c.registers[g.FesEmbeddedRegAckGenerationLo] = 0
			c.registers[g.FesEmbeddedRegAckGenerationHi] = 0
			c.registers[g.FesEmbeddedRegAckCookieLo] = 0
			c.registers[g.FesEmbeddedRegAckCookieHi] = 0
			c.registers[0x90] = (c.registers[0x90] & 4) | 1
		} else if value == g.FesEmbeddedCommandDisable {
			cookie := uint64(c.registers[g.FesEmbeddedRegRequestCookieHi])<<32 | uint64(c.registers[g.FesEmbeddedRegRequestCookieLo])
			if c.registers[g.FesEmbeddedRegStatus] != g.FesEmbeddedStatusEnabled || cookie <= c.lastDrainCookie {
				return errors.New("invalid reader disable")
			}
			c.disables++
			c.lastDrainCookie = cookie
			c.registers[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusDrained
			c.registers[g.FesEmbeddedRegAckGenerationLo] = uint32(generation)
			c.registers[g.FesEmbeddedRegAckGenerationHi] = uint32(generation >> 32)
			c.registers[g.FesEmbeddedRegAckCookieLo] = uint32(cookie)
			c.registers[g.FesEmbeddedRegAckCookieHi] = uint32(cookie >> 32)
			if c.fault == "drain-held" {
				c.registers[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusDraining
				c.registers[g.FesEmbeddedRegPending] = 1
				c.registers[g.FesEmbeddedRegAckGenerationLo] = 0
				c.registers[g.FesEmbeddedRegAckCookieLo] = 0
			}
		} else {
			return errors.New("unexpected reader command")
		}
	}
	if offset == 0x80 {
		c.starts++
		c.registers[0x90], c.registers[0x94], c.registers[0x98], c.registers[0x9c] = 5,
			c.registers[g.FesEmbeddedRegGenerationLo], c.registers[g.FesEmbeddedRegGenerationHi], c.registers[0x8c]
		c.registers[0xa0], c.registers[0xa4], c.registers[0xa8] = 3686400, 3686400, c.crc
		switch c.fault {
		case "stall":
			c.registers[0x90], c.registers[0xa4] = 2, 0
		case "crc":
			c.registers[0xa8] ^= 1
		case "short":
			c.registers[0xa4] -= 16
		case "generation":
			c.registers[0x94]++
		case "cookie":
			c.registers[0x9c]++
		case "response":
			c.registers[0x90] = 16
		case "start":
			return errors.New("uncertain START")
		}
	}
	if c.after != nil {
		c.after("write")
	}
	return nil
}

func windowReadFixture(t *testing.T) (*HPSContextVerifier, *windowCPU, []byte, []byte, *ConfigurationTransfer) {
	t.Helper()
	source, control, memory := windowMemoryFixtures(t)
	cpu := &windowCPU{computerCPU: computerCPU{state: BridgeSnapshot{Capabilities: BridgeCapabilities{15, 2, 12, FabricHandshakeAbsent},
		Owner: 17, Generation: 4, ResetMask: 15, NeverEnabled: true, MastersContained: true}, digest: sha256.Sum256(source)}, registers: make(map[uint32]uint32)}
	v, err := NewWindowHPSContextVerifier(cpu, source, control, memory)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := v.ReserveWindowMemory(windowCPUFrame(), func(b []byte) (*Region, error) {
		return &Region{Address: 0x95000000, Bytes: bytes.Clone(b), Release: func() error { cpu.releases++; return nil }}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cpu.calls = 0
	cpu.crc = publication.CRC32
	cpu.registers[g.FesEmbeddedRegMagic] = g.FesEmbeddedMagic
	cpu.registers[g.FesEmbeddedRegVersion] = g.FesEmbeddedVersion
	cpu.registers[g.FesEmbeddedRegAbi] = scanout.WindowControlABI
	build, _ := BuildWords(v.context.BuildID)
	for i, word := range build {
		cpu.registers[g.FesEmbeddedRegBuild0+uint32(i)*4] = word
	}
	for i, word := range []uint32{0x95000000, 4 << 20, 5120, 5120, 720, 0x31435746, 1} {
		cpu.registers[0xb0+uint32(i)*4] = word
	}
	cpu.registers[0x90] = 1
	controlSHA, memorySHA := sha256.Sum256(control), sha256.Sum256(memory)
	access, _ := json.Marshal(map[string]any{"format": 1, "scope": "sealed-cold-window-read-v1", "package_id": v.context.PackageID, "build_id": v.context.BuildID,
		"control_map_sha256": hex.EncodeToString(controlSHA[:]), "reader_memory_sha256": hex.EncodeToString(memorySHA[:]),
		"adapter_source_sha256": strings.Repeat("a", 64), "adapter_generated_sha256": strings.Repeat("b", 64),
		"arcache": 2, "arprot": 3, "aruser": 224, "read_bytes": 3686400, "open_mask": 8, "close_mask": 10})
	golden, err := os.ReadFile("testdata/hps-window-read.json")
	if err != nil || !bytes.Equal(golden, append(bytes.Clone(access), '\n')) {
		t.Fatal("Python/Go window access contract differs", err)
	}
	return v, cpu, control, append(access, '\n'), computerCompletionFixture(v, &cpu.computerCPU)
}
func windowReadLimits() scanout.ReaderLimits {
	return scanout.ReaderLimits{Timeout: time.Second, MaxPolls: 4, Now: func() time.Time { return time.Unix(100, 0) }, Yield: func() {}}
}

func TestWindowReadFullFrameAndOwnedReturn(t *testing.T) {
	v, cpu, control, access, transfer := windowReadFixture(t)
	got, err := v.ProbeWindowReader(context.Background(), control, access, transfer, windowReadLimits())
	if err != nil || got.Source.Bridges.Generation != 4 || got.After.Generation != 7 || got.After.ResetMask != 15 || !got.After.MastersContained ||
		got.Frame != (scanout.FrameRead{Generation: 1, Cookie: 1, RequestedBytes: 3686400, DeliveredBytes: 3686400, CRC32: cpu.crc}) ||
		got.Memory != v.windowPublication || got.ControlMapSHA256 != sha256.Sum256(control) || got.AccessMapSHA256 != sha256.Sum256(access) ||
		cpu.starts != 1 || cpu.opens != 1 || cpu.dataOpens != 1 || cpu.closes != 1 || cpu.releases != 0 {
		t.Fatal(got, err, cpu)
	}
	before := cpu.calls
	if _, err = v.ProbeWindowReader(context.Background(), control, access, transfer, windowReadLimits()); err == nil || cpu.calls != before || cpu.releases != 0 {
		t.Fatal("window replay", err)
	}
}

func TestWindowReadFailuresKeepMemoryAndDoNotReplay(t *testing.T) {
	for _, fault := range []string{"identity", "read", "start", "crc", "short", "generation", "cookie", "response", "stall", "data-open", "close", "wrong-drain", "late-memory", "lost-owner", "cancel-control", "cancel-frame", "nested"} {
		t.Run(fault, func(t *testing.T) {
			v, cpu, control, access, transfer := windowReadFixture(t)
			cpu.fault = fault
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			limits := windowReadLimits()
			cpu.after = func(event string) {
				if fault == "late-memory" && event == "closed" {
					v.readerMemoryRegion.Bytes[(4<<20)-1] ^= 1
				}
				if fault == "lost-owner" && event == "read" {
					cpu.state.Generation++
				}
				if fault == "cancel-control" && event == "control" || fault == "cancel-frame" && event == "write" && cpu.starts == 1 {
					cancel()
				}
			}
			if fault == "nested" {
				cpu.fault = "stall"
				limits.Yield = func() {
					before := cpu.calls
					if _, err := v.ProbeWindowReader(ctx, control, access, transfer, limits); !errors.Is(err, scanout.ErrReaderBusy) || cpu.calls != before {
						t.Fatal("nested read touched IO", err)
					}
				}
			}
			got, err := v.ProbeWindowReader(ctx, control, access, transfer, limits)
			if err == nil || got != (WindowReadObservation{}) || cpu.releases != 0 || v.readerMemoryRegion == nil || cpu.starts > 1 {
				t.Fatal(got, err, cpu)
			}
			wantCloses := 1
			if fault == "data-open" || fault == "lost-owner" {
				wantCloses = 0
			}
			if cpu.closes != wantCloses {
				t.Fatal("incorrect cleanup attempt", cpu.closes, fault)
			}
			if fault == "identity" && cpu.writes != 0 {
				t.Fatal("wrote to a foreign identity")
			}
			if fault == "cancel-control" && (cpu.dataOpens != 0 || cpu.state.ResetMask != 15) {
				t.Fatal("cancelled control opened data")
			}
			before := cpu.calls
			if _, err = v.ProbeWindowReader(context.Background(), control, access, transfer, windowReadLimits()); err == nil || cpu.calls != before {
				t.Fatal("failed attempt replayed", err)
			}
		})
	}
}

func TestWindowReadRejectsUnboundTransfersAndModifiedMemoryBeforeIO(t *testing.T) {
	for _, fault := range []string{"nil", "generic", "foreign", "incomplete", "error", "pending", "region", "short", "rearm", "selected", "arena", "padding", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			v, cpu, control, access, transfer := windowReadFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "nil":
				transfer = nil
			case "generic":
				transfer.binding = nil
			case "foreign":
				transfer.binding.verifier = &HPSContextVerifier{}
			case "incomplete":
				transfer.done = false
			case "error":
				transfer.err = errors.New("configuration failed")
			case "pending":
				transfer.pending = []configurationBuffer{{}}
			case "region":
				transfer.region = &Region{}
			case "short":
				transfer.accepted = 0
			case "rearm":
				transfer.binding.rearm = true
			case "selected":
				transfer.binding.program = &selectedProgram{}
			case "arena":
				v.readerMemoryRegion.Bytes = v.readerMemoryRegion.Bytes[:2<<20]
			case "padding":
				v.readerMemoryRegion.Bytes[(4<<20)-1] ^= 1
			case "cancelled":
				cancel()
			}
			if got, err := v.ProbeWindowReader(ctx, control, access, transfer, windowReadLimits()); err == nil || got != (WindowReadObservation{}) || cpu.calls != 0 || cpu.reads != 0 || cpu.writes != 0 || cpu.releases != 0 {
				t.Fatal(got, err, cpu)
			}
		})
	}
}

func TestWindowReadAccessContractIsExplicitAndExact(t *testing.T) {
	v, cpu, control, access, transfer := windowReadFixture(t)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(access, &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		t.Run(key, func(t *testing.T) {
			changed := make(map[string]json.RawMessage)
			for k, value := range fields {
				changed[k] = value
			}
			delete(changed, key)
			raw, _ := json.Marshal(changed)
			if _, err := v.ProbeWindowReader(context.Background(), control, append(raw, '\n'), transfer, windowReadLimits()); err == nil || cpu.calls != 0 {
				t.Fatal("omission accepted", key, err)
			}
		})
	}
	for _, raw := range [][]byte{nil, bytes.TrimSpace(access), append(bytes.Clone(access), '\n'), append(bytes.Clone(access), access...)} {
		if _, err := v.ProbeWindowReader(context.Background(), control, raw, transfer, windowReadLimits()); err == nil || cpu.calls != 0 {
			t.Fatal("noncanonical contract accepted", err)
		}
	}
}

func TestRetiredWindowApertureHasNoIO(t *testing.T) {
	_, cpu, _, _, _ := windowReadFixture(t)
	p := &windowIO{cpu: cpu, bridges: &FirmwareBridges{cpu: cpu}, want: cpu.state, base: 0x20010400}
	p.retire()
	if _, err := p.Read32(0); err == nil {
		t.Fatal("retired read accepted")
	}
	if err := p.Write32(0x80, 1); err == nil || cpu.calls != 0 || cpu.reads != 0 || cpu.writes != 0 {
		t.Fatal("retired port touched IO", err)
	}
}
