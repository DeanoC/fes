package sdm

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"testing"
	"time"
)

const modelBase = 0x96000000
const completionBase = modelBase - 0x80000000

func startedTransfer(t *testing.T, size int) *ConfigModel {
	t.Helper()
	m, err := NewConfigModel(HeldBridgeModel, bytes.Repeat([]byte{0xa5}, size), modelBase, 16, 0, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Next(0)
	if err != nil || c == nil || c.Operation != StartConfig {
		t.Fatal(c, err)
	}
	if err = m.Complete(c.Token, ConfigReply{}, 0); err != nil {
		t.Fatal(err)
	}
	return m
}
func issued(t *testing.T, m *ConfigModel, op ConfigOperation) *ConfigCommand {
	t.Helper()
	c, err := m.Next(0)
	if err != nil || c == nil || c.Operation != op {
		t.Fatalf("want %s: %+v %v", op, c, err)
	}
	if replay, err := m.Next(0); replay != nil || err != nil {
		t.Fatal("replayed outstanding command", err)
	}
	return c
}

func TestAcceptedBusyNeverReplaysBytes(t *testing.T) {
	m := startedTransfer(t, 80)
	var addresses []uint64
	for range 4 {
		c := issued(t, m, WriteConfig)
		addresses = append(addresses, c.Registers[1]-0x80000000)
		status := uint64(0)
		if len(addresses) == 4 {
			status = 1
		}
		if err := m.Complete(c.Token, ConfigReply{Registers: [4]uint64{status}}, 0); err != nil {
			t.Fatal(err)
		}
	}
	if s := m.Status(); s.AcceptedBytes != 64 || s.Outstanding != 4 {
		t.Fatal(s)
	}
	c := issued(t, m, PollWrites)
	if err := m.Complete(c.Token, ConfigReply{Registers: [4]uint64{1}}, 0); err != nil {
		t.Fatal(err)
	}
	c = issued(t, m, PollWrites)
	if err := m.Complete(c.Token, ConfigReply{Registers: [4]uint64{0, addresses[0], addresses[1], addresses[2]}}, 0); err != nil {
		t.Fatal(err)
	}
	c = issued(t, m, WriteConfig)
	if c.Registers[1] != modelBase+64 {
		t.Fatal("BUSY buffer replayed", c.Registers)
	}
	if err := m.Complete(c.Token, ConfigReply{}, 0); err != nil {
		t.Fatal(err)
	}
	c = issued(t, m, PollWrites)
	if err := m.Complete(c.Token, ConfigReply{Registers: [4]uint64{0, completionBase + 48, completionBase + 64}}, 0); err != nil {
		t.Fatal(err)
	}
	c = issued(t, m, PollConfigured)
	if err := m.Complete(c.Token, ConfigReply{Registers: [4]uint64{1}}, 0); err != nil {
		t.Fatal(err)
	}
	c = issued(t, m, PollConfigured)
	if err := m.Complete(c.Token, ConfigReply{}, 0); err != nil {
		t.Fatal(err)
	}
	if s := m.Status(); !s.Done || s.Failed || s.AcceptedBytes != 80 || s.CompletedBytes != 80 || s.Outstanding != 0 || s.RetainedBytes != 160 {
		t.Fatal(s)
	}
}

func TestCompletionBatchesAreValidatedBeforeRetirement(t *testing.T) {
	for name, reply := range map[string][4]uint64{
		"duplicate":           {0, completionBase, completionBase},
		"foreign after valid": {0, completionBase, completionBase + 99},
		"hole":                {0, completionBase, 0, completionBase + 16},
		"CPU namespace":       {0, modelBase},
		"missing":             {}, "busy with address": {1, completionBase}, "error": {4}, "unknown": {99},
	} {
		t.Run(name, func(t *testing.T) {
			m := startedTransfer(t, 64)
			for range 4 {
				c := issued(t, m, WriteConfig)
				if err := m.Complete(c.Token, ConfigReply{}, 0); err != nil {
					t.Fatal(err)
				}
			}
			c := issued(t, m, PollWrites)
			if err := m.Complete(c.Token, ConfigReply{Registers: reply}, 0); err == nil {
				t.Fatal("bad completion accepted")
			}
			s := m.Status()
			if !s.Failed || s.Outstanding != 4 || s.CompletedBytes != 0 || s.RetainedBytes != 128 || m.issued == nil {
				t.Fatal("partial retirement", s)
			}
			if next, err := m.Next(0); err == nil || next != nil {
				t.Fatal("failed transfer replayed")
			}
			if err := m.Complete(c.Token, ConfigReply{}, 0); !errors.Is(err, ErrConfigStale) || m.Status() != s {
				t.Fatal("late completion changed retired transfer")
			}
		})
	}
}

func TestUnknownWriteAndAsynchronousMutationRetainBuffers(t *testing.T) {
	for _, status := range []uint64{2, 3, 4, 0xffffffff} {
		m := startedTransfer(t, 32)
		first := issued(t, m, WriteConfig)
		if err := m.Complete(first.Token, ConfigReply{}, 0); err != nil {
			t.Fatal(err)
		}
		second := issued(t, m, WriteConfig)
		if err := m.Complete(second.Token, ConfigReply{Registers: [4]uint64{status}}, 0); err == nil {
			t.Fatal("write failure accepted")
		}
		if s := m.Status(); s.AcceptedBytes != 16 || s.Outstanding != 1 || s.RetainedBytes != 64 || !s.Failed || m.issued == nil || &m.issued.Bytes[0] != &second.Bytes[0] {
			t.Fatal("lost accepted or uncertain buffer", s)
		}
	}
	m := startedTransfer(t, 16)
	c := issued(t, m, WriteConfig)
	if err := m.Complete(c.Token, ConfigReply{}, 0); err != nil {
		t.Fatal(err)
	}
	c.Bytes[0] ^= 1
	poll := issued(t, m, PollWrites)
	if err := m.Complete(poll.Token, ConfigReply{Registers: [4]uint64{0, completionBase}}, 0); err == nil || m.Status().Outstanding != 1 {
		t.Fatal("asynchronously mutated buffer retired")
	}
	m = startedTransfer(t, 16)
	c = issued(t, m, WriteConfig)
	c.Bytes[0] ^= 1
	if err := m.Complete(c.Token, ConfigReply{}, 0); err == nil || m.Status().AcceptedBytes != 0 || m.Status().RetainedBytes != 32 {
		t.Fatal("issued mutation accepted")
	}
}

func TestStaleTokensAndDeadlines(t *testing.T) {
	a, b := startedTransfer(t, 16), startedTransfer(t, 16)
	ca, cb := issued(t, a, WriteConfig), issued(t, b, WriteConfig)
	before := a.Status()
	if err := a.Complete(cb.Token, ConfigReply{}, 0); !errors.Is(err, ErrConfigStale) || a.Status() != before {
		t.Fatal("foreign transfer token accepted")
	}
	if err := a.Complete(ca.Token, ConfigReply{}, 0); err != nil {
		t.Fatal(err)
	}
	if err := a.Complete(ca.Token, ConfigReply{}, 0); !errors.Is(err, ErrConfigStale) {
		t.Fatal("duplicate completion accepted")
	}
	poll := issued(t, a, PollWrites)
	if err := a.Complete(poll.Token, ConfigReply{Registers: [4]uint64{0, completionBase}}, time.Second); !errors.Is(err, ErrConfigDeadline) {
		t.Fatal(err)
	}
	if s := a.Status(); s.Outstanding != 1 || s.CompletedBytes != 0 || !s.Failed || s.RetainedBytes != 32 {
		t.Fatal(s)
	}
	m := startedTransfer(t, 16)
	if _, err := m.Next(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Next(0); err == nil || !m.Status().Failed {
		t.Fatal("regressing clock accepted")
	}
	if _, err := (&ConfigModel{}).Next(0); err == nil {
		t.Fatal("uninitialized model accepted")
	}
}

func TestConstructorAndStartFailures(t *testing.T) {
	for _, firmware := range []ConfigFirmware{"", ReferenceTFAV212, "other-firmware"} {
		if m, err := NewConfigModel(firmware, []byte("payload"), modelBase, 16, 0, time.Second); m != nil || !errors.Is(err, ErrUnsafeFirmware) {
			t.Fatal("unsafe firmware admitted", firmware)
		}
	}
	for _, base := range []uint64{0, 0x80000000, modelBase + 1, 0xc0000000, ^uint64(0) - 63} {
		if m, err := NewConfigModel(HeldBridgeModel, []byte("payload"), base, 16, 0, time.Second); err == nil || m != nil {
			t.Fatal("bad DDR bounds", base)
		}
	}
	for _, chunk := range []int{-1, 0, 0xffff1} {
		if _, err := NewConfigModel(HeldBridgeModel, []byte("payload"), modelBase, chunk, 0, time.Second); err == nil {
			t.Fatal("invalid chunk")
		}
	}
	for _, reply := range []ConfigReply{{Registers: [4]uint64{4}}, {Registers: [4]uint64{0, 1}}, {Err: errors.New("transport uncertain")}} {
		m, _ := NewConfigModel(HeldBridgeModel, []byte("payload"), modelBase, 16, 0, time.Second)
		c := issued(t, m, StartConfig)
		if err := m.Complete(c.Token, reply, 0); err == nil || !m.Status().Failed || m.Status().RetainedBytes != 7 {
			t.Fatal("uncertain START accepted or lost source")
		}
	}
}

func TestVariableChunksAndCompletionSchedules(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 100 {
		data := make([]byte, 1+rng.IntN(1024))
		for i := range data {
			data[i] = byte(i)
		}
		original := bytes.Clone(data)
		m, err := NewConfigModel(HeldBridgeModel, data, modelBase, 1+rng.IntN(64), 0, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		clear(data)
		var received []byte
		var pending []uint64
		for steps := 0; steps < 5000; steps++ {
			c, err := m.Next(time.Duration(steps))
			if err != nil {
				t.Fatal(err)
			}
			if c == nil {
				break
			}
			var r [4]uint64
			switch c.Operation {
			case WriteConfig:
				if c.Registers[1] != modelBase+uint64(len(received)) || len(pending) >= 4 {
					t.Fatal("write replay or queue overflow")
				}
				received = append(received, c.Bytes...)
				pending = append(pending, c.Registers[1]-0x80000000)
				if rng.IntN(3) == 0 || len(pending) == 4 {
					r[0] = 1
				}
			case PollWrites:
				if rng.IntN(4) == 0 {
					r[0] = 1
					break
				}
				n := 1 + rng.IntN(min(3, len(pending)))
				for i := 0; i < n; i++ {
					j := rng.IntN(len(pending))
					r[i+1] = pending[j]
					pending = append(pending[:j], pending[j+1:]...)
				}
			case PollConfigured:
				if len(pending) != 0 {
					t.Fatal("ISDONE before drain")
				}
			}
			if err = m.Complete(c.Token, ConfigReply{Registers: r}, time.Duration(steps)); err != nil {
				t.Fatal(err)
			}
		}
		if !m.Status().Done || !bytes.Equal(received, original) || m.Status().CompletedBytes != len(original) {
			t.Fatal("schedule lost exact bytes", m.Status())
		}
	}
}
