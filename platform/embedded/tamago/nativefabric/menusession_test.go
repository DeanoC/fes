// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	stdimage "image"
	"os"
	"reflect"
	"strings"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

func menuMaps(t *testing.T) (source, control, memory, access []byte) {
	t.Helper()
	source, control, _ = windowMemoryFixtures(t)
	var ctx map[string]any
	json.Unmarshal(source, &ctx)
	ctx["programming_profile"] = HPSFirstMenuProfile
	ctx["control_abi"] = scanout.MenuControlABI
	encode := func(v any) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return append(b, '\n')
	}
	source = encode(ctx)
	var ctrl map[string]any
	json.Unmarshal(control, &ctrl)
	ctrl["fabric_stop"].(map[string]any)["scope"] = "sealed-menu-master-stop-v1"
	control = encode(ctrl)
	memory = encode(map[string]any{"format": 1, "scope": "sealed-cold-menu-reader-memory-v1", "package_id": ctx["package_id"], "build_id": ctx["build_id"],
		"cpu_base": 0x95000000, "bus_base": 0x95000000, "bytes": 8 << 20, "arena_base": 0x95000000, "arena_bytes": 8 << 20,
		"cpu_attributes": "normal-uncached", "lifetime": "until-reboot", "fpga_addressing": "physical-identity-unqualified",
		"row_bytes": 5120, "stride": 5120, "rows": 720, "frame_bytes": 3686400, "pixel_format": "xrgb8888", "slots": 2, "slot_bytes": 4 << 20})
	ch, mh := sha256.Sum256(control), sha256.Sum256(memory)
	access = encode(map[string]any{"format": 1, "scope": "sealed-cold-menu-read-v1", "package_id": ctx["package_id"], "build_id": ctx["build_id"],
		"reader_memory_sha256": hex.EncodeToString(mh[:]), "control_map_sha256": hex.EncodeToString(ch[:]), "adapter_source_sha256": strings.Repeat("a", 64), "adapter_generated_sha256": strings.Repeat("b", 64),
		"arcache": 2, "arprot": 3, "aruser": 224, "read_bytes": 3686400, "open_mask": 8, "close_mask": 10})
	for name, want := range map[string][]byte{"hps-menu-memory.json": memory, "hps-menu-read.json": access} {
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil || !bytes.Equal(raw, want) {
			t.Fatal("Python/native menu map differs", name, err)
		}
	}
	return
}

// Firmware and device responses are injected here to test session ownership.
// scanout/verify_menu.py separately drives the real CSR/video/DDR RTL, while
// the existing joined firmware suite exercises the actual C bridge service.
type menuCPU struct {
	windowCPU
	quiesces, independentStops int
	accepted, displayed, slot  uint32
}

func (c *menuCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[1] == uint64(g.FesEmbeddedBridgeClose) && c.state.ResetMask == 5 {
		c.independentStops++
		if c.fault == "master-stop" {
			c.calls++
			return [4]uint64{}, errors.New("independent master stop failed")
		}
		// The injected firmware independently fences the known source; native
		// failed-frame state cannot manufacture a normal session-return receipt.
		c.registers[g.FesEmbeddedRegStatus] = g.FesEmbeddedStatusDrained
		c.registers[g.FesEmbeddedRegPending] = 0
	}
	return c.windowCPU.Invoke(a)
}
func (c *menuCPU) ReadPhysical32(address uint64) (uint32, error) {
	if address == 0x20010494 {
		c.registers[0x98] = 0
		if c.accepted != 0 {
			c.registers[0x98] = 1
		}
		c.registers[0x9c] = 0
		c.registers[0xa0] = c.accepted
		c.registers[0xa4] = c.displayed
		c.registers[0xa8] = 0
		c.registers[0xac] = c.slot
	}
	return c.windowCPU.ReadPhysical32(address)
}
func (c *menuCPU) WritePhysical32(address uint64, value uint32) error {
	offset := uint32(address - 0x20010400)
	if offset < 0x80 {
		oldSlot := c.registers[0x90]
		err := c.windowCPU.WritePhysical32(address, value)
		c.registers[0x90] = oldSlot
		return err
	}
	c.writes++
	if c.state.ResetMask != 5 || address > 0x200104c8 {
		return errors.New("unowned menu write")
	}
	c.registers[offset] = value
	if offset == 0x80 {
		sequence, slot := c.registers[0x8c], c.registers[0x90]
		if value == 1 || value == 2 {
			if sequence <= c.accepted || value == 2 && slot == c.slot {
				return errors.New("replayed menu command")
			}
			c.accepted = sequence
			c.slot = slot
			c.displayed = sequence
			c.starts++
			c.registers[0x94] = 6
			if c.fault == "frame-stall" {
				c.displayed = sequence - 1
				c.registers[0x94] = 2
			}
			if c.fault == "frame-command" {
				return errors.New("menu command uncertain")
			}
		} else if value == 3 {
			c.quiesces++
			c.registers[0x94] = 8
			if c.fault == "local-stop" {
				return errors.New("menu stop uncertain")
			}
		} else {
			return errors.New("unknown menu command")
		}
	}
	if c.after != nil {
		c.after("write")
	}
	return nil
}

type menuFixture struct {
	v                               *HPSContextVerifier
	cpu                             *menuCPU
	source, control, memory, access []byte
	transfer                        *ConfigurationTransfer
	publications                    int
	barrier                         func() error
}

func newMenuFixture(t *testing.T) *menuFixture {
	t.Helper()
	f := &menuFixture{}
	f.source, f.control, f.memory, f.access = menuMaps(t)
	f.cpu = &menuCPU{windowCPU: windowCPU{computerCPU: computerCPU{state: BridgeSnapshot{Capabilities: BridgeCapabilities{15, 2, 12, FabricHandshakeAbsent}, Owner: 17, Generation: 4, ResetMask: 15, NeverEnabled: true, MastersContained: true}, digest: sha256.Sum256(f.source)}, registers: make(map[uint32]uint32)}}
	var err error
	f.v, err = NewMenuHPSContextVerifier(f.cpu, f.source, f.control, f.memory, f.access)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := f.v.ReserveMenuMemory(func(data []byte) (*Region, error) {
		return &Region{Address: 0x95000000, Bytes: bytes.Clone(data), Release: func() error { f.cpu.releases++; return nil }, OrderFrameStores: func() error {
			f.publications++
			if f.barrier != nil {
				return f.barrier()
			}
			return nil
		}}, nil
	})
	if err != nil || observation.ArenaBytes != 8<<20 || observation.Slots != 2 || observation.SlotBytes != 4<<20 {
		t.Fatal(observation, err)
	}
	f.cpu.registers[g.FesEmbeddedRegMagic] = g.FesEmbeddedMagic
	f.cpu.registers[g.FesEmbeddedRegVersion] = g.FesEmbeddedVersion
	f.cpu.registers[g.FesEmbeddedRegAbi] = scanout.MenuControlABI
	build, _ := BuildWords(f.v.context.BuildID)
	for i, w := range build {
		f.cpu.registers[g.FesEmbeddedRegBuild0+uint32(i)*4] = w
	}
	for i, w := range []uint32{0x95000000, 8 << 20, 5120, 1280, 720, 0x31534d46, 1} {
		f.cpu.registers[0xb0+uint32(i)*4] = w
	}
	f.cpu.registers[0x94] = 1
	f.transfer = computerCompletionFixture(f.v, &f.cpu.computerCPU)
	return f
}
func (f *menuFixture) open(t *testing.T, ctx context.Context) *MenuSession {
	t.Helper()
	s, err := f.v.OpenMenuSession(ctx, f.control, f.access, f.transfer, windowReadLimits())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func menuFrame(red byte) *stdimage.RGBA {
	frame := stdimage.NewRGBA(stdimage.Rect(0, 0, 1280, 720))
	for i := 0; i < len(frame.Pix); i += 4 {
		copy(frame.Pix[i:i+4], []byte{red, 0x3a, 0xa7, 0x20})
	}
	return frame
}
func TestMenuSessionFramesAndIndependentReturn(t *testing.T) {
	f := newMenuFixture(t)
	s := f.open(t, context.Background())
	for i := 0; i < 3; i++ {
		frame := menuFrame(byte(i + 1))
		got, err := s.PresentFrame(context.Background(), frame)
		if err != nil || got.Sequence != uint32(i+1) || got.Slot != uint32(i%2) {
			t.Fatal(got, err)
		}
		offset := (i % 2) * (4 << 20)
		expected := []byte{0xa7, 0x3a, byte(i + 1), 0xff}
		if !bytes.Equal(s.bytes[offset:offset+4], expected) {
			t.Fatal("wrong published XRGB")
		}
		frame.Pix[0] ^= 1
		if !bytes.Equal(s.bytes[offset:offset+4], expected) {
			t.Fatal("caller storage escaped")
		}
	}
	returned, err := s.Close()
	if err != nil || returned.Frames != 3 || returned.After.Generation != 7 || returned.After.ResetMask != 15 || !returned.After.MastersContained ||
		f.cpu.independentStops != 1 || f.cpu.quiesces != 1 || f.cpu.disables != 1 || f.publications != 3 || f.cpu.releases != 0 {
		t.Fatal(returned, err, f.cpu)
	}
	calls := f.cpu.calls + f.cpu.reads + f.cpu.writes
	again, err := s.Close()
	if err != nil || again != returned || calls != f.cpu.calls+f.cpu.reads+f.cpu.writes {
		t.Fatal("closure replay")
	}
	if _, err = s.PresentFrame(context.Background(), menuFrame(4)); !errors.Is(err, scanout.ErrReaderRetired) {
		t.Fatal(err)
	}
	if _, err = f.v.OpenMenuSession(context.Background(), f.control, f.access, f.transfer, windowReadLimits()); err == nil || calls != f.cpu.calls+f.cpu.reads+f.cpu.writes {
		t.Fatal("completion replay", err)
	}
	if !bytes.Equal(f.v.readerMemoryRegion.Bytes, f.v.menuExpected) {
		t.Fatal("return lost private expected arena")
	}
}
func TestMenuSessionFailuresRemainPinnedAndDoNotResume(t *testing.T) {
	for _, fault := range []string{"data-open", "identity", "frame-command", "frame-stall", "publication", "other-slot-corruption", "padding-corruption", "local-stop", "master-stop", "wrong-drain", "late-owner", "late-memory", "cancelled-caller"} {
		t.Run(fault, func(t *testing.T) {
			f := newMenuFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "data-open" || fault == "identity" {
				f.cpu.fault = fault
			}
			s, openErr := f.v.OpenMenuSession(ctx, f.control, f.access, f.transfer, windowReadLimits())
			if openErr == nil {
				f.cpu.fault = fault
				f.barrier = func() error {
					switch fault {
					case "publication":
						return errors.New("uncertain stores")
					case "other-slot-corruption":
						s.bytes[4<<20] ^= 1
					case "padding-corruption":
						s.bytes[3686400] ^= 1
					}
					return nil
				}
				_, frameErr := s.PresentFrame(ctx, menuFrame(1))
				if fault == "late-owner" {
					f.cpu.state.Generation++
				}
				if fault == "late-memory" {
					s.bytes[len(s.bytes)-1] ^= 1
				}
				if fault == "cancelled-caller" {
					cancel()
				}
				returned, closeErr := s.Close()
				if fault == "cancelled-caller" {
					if closeErr != nil || returned.After.ResetMask != 15 {
						t.Fatal("cancellation prevented independent close", returned, closeErr)
					}
				} else if frameErr == nil && closeErr == nil || returned != (MenuSessionReturn{}) {
					t.Fatal("failed scope yielded return", returned, frameErr, closeErr)
				}
			} else if s == nil || f.v.menuSession != s {
				t.Fatal("lost uncertain session", openErr)
			}
			if f.cpu.releases != 0 || f.v.readerMemoryRegion == nil {
				t.Fatal("failed reservation released")
			}
			count := f.cpu.calls + f.cpu.reads + f.cpu.writes
			if s != nil {
				s.Close()
				if _, err := s.PresentFrame(context.Background(), menuFrame(2)); !errors.Is(err, scanout.ErrReaderRetired) {
					t.Fatal(err)
				}
			}
			if count != f.cpu.calls+f.cpu.reads+f.cpu.writes {
				t.Fatal("terminal scope replayed I/O")
			}
		})
	}
}
func TestMenuSessionRejectsForeignCompletionAndProtectsOneOwner(t *testing.T) {
	for _, fault := range []string{"foreign", "partial", "program", "rearm", "consumed"} {
		t.Run(fault, func(t *testing.T) {
			f := newMenuFixture(t)
			switch fault {
			case "foreign":
				f.transfer.binding.verifier = nil
			case "partial":
				f.transfer.offset = 0
			case "program":
				f.transfer.binding.program = &selectedProgram{}
			case "rearm":
				f.transfer.binding.rearm = true
			case "consumed":
				f.transfer.binding.menuOpened = true
			}
			before := f.cpu.calls + f.cpu.reads + f.cpu.writes
			if s, err := f.v.OpenMenuSession(context.Background(), f.control, f.access, f.transfer, windowReadLimits()); err == nil || s != nil || before != f.cpu.calls+f.cpu.reads+f.cpu.writes {
				t.Fatal("foreign transfer reached I/O", err)
			}
		})
	}
	f := newMenuFixture(t)
	s := f.open(t, context.Background())
	copy := *s
	before := f.cpu.calls + f.cpu.reads + f.cpu.writes
	if _, err := copy.Close(); !errors.Is(err, scanout.ErrReaderArgument) {
		t.Fatal(err)
	}
	if _, err := f.v.OpenMenuSession(context.Background(), f.control, f.access, f.transfer, windowReadLimits()); !errors.Is(err, scanout.ErrReaderBusy) {
		t.Fatal(err)
	}
	if before != f.cpu.calls+f.cpu.reads+f.cpu.writes {
		t.Fatal("forked owner reached I/O")
	}
	if _, err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestMenuMapsRejectOlderAndMutatedGrantsWithoutIO(t *testing.T) {
	source, control, memory, access := menuMaps(t)
	cpu := bridgeFirmwareFunc(func([7]uint64) ([4]uint64, error) { t.Fatal("map parsing performed I/O"); return [4]uint64{}, nil })
	if _, err := NewHPSContextVerifier(cpu, source); err == nil {
		t.Fatal("default constructor admitted menu")
	}
	if _, err := NewWindowHPSContextVerifier(cpu, source, control, memory); err == nil {
		t.Fatal("window inherited menu")
	}
	v, err := NewMenuHPSContextVerifier(cpu, source, control, memory, access)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(v).(BootHandoffVerifier); ok {
		t.Fatal("source metadata became boot authority")
	}
	for i, raw := range [][]byte{source, control, memory, access} {
		var fields map[string]json.RawMessage
		json.Unmarshal(raw, &fields)
		for name, value := range fields {
			var decoded any
			json.Unmarshal(value, &decoded)
			switch x := decoded.(type) {
			case string:
				decoded = x + "x"
			case float64:
				decoded = x + 1
				if name == "core_size" {
					decoded = 0
				}
			case map[string]any:
				x["scope"] = "foreign"
				decoded = x
			default:
				t.Fatalf("uncovered map field %s", name)
			}
			modified := make(map[string]json.RawMessage)
			for k, v := range fields {
				modified[k] = v
			}
			modified[name], _ = json.Marshal(decoded)
			bad, _ := json.Marshal(modified)
			maps := [][]byte{source, control, memory, access}
			maps[i] = append(bad, '\n')
			if _, err := NewMenuHPSContextVerifier(cpu, maps[0], maps[1], maps[2], maps[3]); err == nil {
				t.Fatalf("mutated map field accepted: %d %s", i, name)
			}
		}
	}
	if reflect.DeepEqual(v.menuMemory, nil) {
		t.Fatal("menu map absent")
	}
}
func TestMenuMemoryReservationRetainsFailuresAndConsumesAttempt(t *testing.T) {
	for _, fault := range []string{"short", "address", "bytes", "barrier", "allocation", "late-owner", "late-memory"} {
		t.Run(fault, func(t *testing.T) {
			source, control, memory, access := menuMaps(t)
			calls := 0
			var region *Region
			mutate := func(i int, r *[4]uint64) {
				if i == 4 && fault == "late-owner" {
					r[2]++
				}
				if i == 4 && fault == "late-memory" {
					region.Bytes[7<<20] ^= 1
				}
			}
			cpu := readerMemoryCPU(t, source, -1, &calls, mutate)
			v, err := NewMenuHPSContextVerifier(cpu, source, control, memory, access)
			if err != nil {
				t.Fatal(err)
			}
			releases, allocations := 0, 0
			got, err := v.ReserveMenuMemory(func(data []byte) (*Region, error) {
				allocations++
				region = &Region{Address: 0x95000000, Bytes: bytes.Clone(data), Release: func() error { releases++; return nil }, OrderFrameStores: func() error { return nil }}
				switch fault {
				case "short":
					region.Bytes = region.Bytes[:4<<20]
				case "address":
					region.Address += 4096
				case "bytes":
					region.Bytes[5<<20] = 1
				case "barrier":
					region.OrderFrameStores = nil
				case "allocation":
					return region, errors.New("uncertain allocation")
				}
				data[0] ^= 1
				return region, nil
			})
			if err == nil || got != (MenuMemoryObservation{}) || releases != 0 || v.readerMemoryRegion != region {
				t.Fatal("failed reservation yielded authority", got, err)
			}
			before := calls
			if _, err = v.ReserveMenuMemory(func([]byte) (*Region, error) { allocations++; return nil, nil }); err == nil || calls != before || allocations != 1 || releases != 0 {
				t.Fatal("reservation replay")
			}
		})
	}
}
