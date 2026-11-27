// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

func initialReceipt(request InitialTapeRequest) InitialTapeReceipt {
	words := (len(request.Image)+1)/2 + 3*((len(request.Image)+511)/512) + 32 + 12 + 4 + 1
	bit := request.GPO & g.FesComputerRequestMask
	if words%2 != 0 {
		bit ^= g.FesComputerRequestMask
	}
	return InitialTapeReceipt{GPO: bit | g.FesComputerOpcodeIdentity<<24 | 15<<16, Bytes: uint32(len(request.Image)), CRC32: crc32.ChecksumIEEE(request.Image)}
}

func TestInitialTapeHookPrivateSourceAndProtocolContinuation(t *testing.T) {
	for _, size := range []int{1, 2, 511, 512, 513, 917, 1025, 16375} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			want := wanted()
			f := fixture(want)
			data := bytes.Repeat([]byte{3}, size)
			calls := 0
			load := func(_ context.Context, request InitialTapeRequest) (InitialTapeReceipt, error) {
				calls++
				if request.Identity != want || request.CRC32 != crc32.ChecksumIEEE(data) || !bytes.Equal(request.Image, data) {
					t.Fatal("selected source lost")
				}
				data[0] = 9
				r := initialReceipt(request)
				f.gpo = r.GPO
				f.gpi = g.FesComputerSignature
				if r.GPO&g.FesComputerRequestMask != 0 {
					f.gpi |= g.FesComputerAckMask
				}
				return r, nil
			}
			c, err := OpenWithInitialTape(context.Background(), f, limits(), want, load)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Hold(context.Background()); err != nil {
				t.Fatal(err)
			}
			writes := len(f.writes)
			if err = c.LoadInitialTape(context.Background(), data); err != nil || calls != 1 || len(f.writes) != writes {
				t.Fatal(err, calls, len(f.writes))
			}
			if err = c.Run(context.Background()); err != nil {
				t.Fatal("returned protocol state rejected", err)
			}
		})
	}
}

func TestInitialTapeHookFailuresRetireWithoutRunOrFallback(t *testing.T) {
	for _, fault := range []string{"error", "bytes", "crc", "gpo", "parity", "mutation", "deadline", "backwards", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			f := fixture(wanted())
			now := time.Unix(100, 0)
			limit := limits()
			limit.Now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			load := func(_ context.Context, request InitialTapeRequest) (InitialTapeReceipt, error) {
				calls++
				r := initialReceipt(request)
				switch fault {
				case "error":
					return r, errors.New("uncertain secure transfer")
				case "bytes":
					r.Bytes++
				case "crc":
					r.CRC32++
				case "gpo":
					r.GPO ^= 1
				case "parity":
					r.GPO ^= g.FesComputerRequestMask
				case "mutation":
					request.Image[0] ^= 1
				case "deadline":
					budget, _ := TapeTransferBudget(len(request.Image), limit.Timeout)
					now = now.Add(budget)
				case "backwards":
					now = now.Add(-time.Second)
				case "cancel":
					cancel()
				}
				return r, nil
			}
			c, err := OpenWithInitialTape(ctx, f, limit, wanted(), load)
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Hold(ctx); err != nil {
				t.Fatal(err)
			}
			writes := len(f.writes)
			if err = c.LoadInitialTape(ctx, []byte{1, 2, 3}); err == nil {
				t.Fatal("failed receipt accepted")
			}
			if !errors.Is(c.Run(context.Background()), ErrRetired) || !errors.Is(c.LoadInitialTape(context.Background(), []byte{1}), ErrRetired) || calls != 1 || len(f.writes) != writes {
				t.Fatal("failed hook replayed or fell back", calls, len(f.writes))
			}
		})
	}
}

func TestInitialTapeHookSelectionAndSerialization(t *testing.T) {
	f := fixture(wanted())
	if _, err := OpenWithInitialTape(context.Background(), f, limits(), wanted(), nil); err == nil || f.reads != 0 {
		t.Fatal("missing selection touched IO")
	}
	var c *Computer
	calls := 0
	load := func(_ context.Context, r InitialTapeRequest) (InitialTapeReceipt, error) {
		calls++
		if !errors.Is(c.Run(context.Background()), ErrBusy) {
			t.Fatal("hook permitted concurrent command")
		}
		return initialReceipt(r), nil
	}
	var err error
	c, err = OpenWithInitialTape(context.Background(), f, limits(), wanted(), load)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Hold(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = c.LoadInitialTape(context.Background(), []byte{1}); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestMediaSizeBudgetKeepsAnIndependentAcknowledgementDeadline(t *testing.T) {
	f := fixture(wanted())
	now := time.Unix(100, 0)
	limit := limits()
	limit.Now = func() time.Time { return now }
	c, err := Open(context.Background(), f, limit, wanted())
	if err != nil {
		t.Fatal(err)
	}
	// MediaInfo's first acknowledgement is deliberately slow. The file's size
	// budget is several seconds, but one command must still fail at one second.
	f.readHook = func(uint32) (uint32, error, bool) { now = now.Add(250 * time.Millisecond); return 0, nil, false }
	err = c.LoadTape(context.Background(), make([]byte, 917))
	if !errors.Is(err, ErrLimit) || !strings.Contains(err.Error(), "reason=ack-deadline") || !errors.Is(c.Run(context.Background()), ErrRetired) {
		t.Fatal(err)
	}
}

type spectrumMediaFaultIO struct {
	*faultIO
	mediaState uint16
	header     [4]uint16
	chunk      [3]uint16
	received   []byte
	batches    int
}

func (s *spectrumMediaFaultIO) Write32(offset, word uint32) error {
	if err := s.faultIO.Write32(offset, word); err != nil {
		return err
	}
	op, index, val := (word>>24)&0x7f, (word>>16)&0xff, uint16(word)
	switch op {
	case g.FesComputerOpcodeMediaInfo:
		vals := [6]uint16{
			uint16(uint32(g.FesComputerSpectrumTapeMinBytes) & 0xffff), uint16(uint32(g.FesComputerSpectrumTapeMinBytes) >> 16),
			uint16(uint32(g.FesComputerSpectrumTapeMaxBytes) & 0xffff), uint16(uint32(g.FesComputerSpectrumTapeMaxBytes) >> 16),
			uint16(g.FesComputerMediaChunkMaxBytes), s.mediaState,
		}
		s.gpi = (s.gpi &^ g.FesComputerResponseMask) | uint32(vals[index])
	case g.FesComputerOpcodeMediaBegin:
		s.header[index] = val
		if index == 0 {
			s.received = nil
		}
	case g.FesComputerOpcodeMediaChunk:
		s.chunk[index] = val
	case g.FesComputerOpcodeMediaData:
		s.received = append(s.received, byte(val))
		if len(s.received) < int(s.chunk[0])|int(s.chunk[1])<<16+int(s.chunk[2]) {
			s.received = append(s.received, byte(val>>8))
		}
	case g.FesComputerOpcodeMediaCommit:
		s.mediaState = uint16(g.FesComputerMediaStateReady)
	}
	return nil
}

type spectrumMediaBatchedIO struct {
	*spectrumMediaFaultIO
}

func (b *spectrumMediaBatchedIO) WithMediaChunkBatch(send func(IO) error) error {
	b.batches++
	return send(b.spectrumMediaFaultIO)
}

func TestLoadInitialSpectrumTapeUsesMediaChunkBatcher(t *testing.T) {
	pzx := make([]byte, int(g.FesComputerMediaChunkMaxBytes)+17)
	for i := range pzx {
		pzx[i] = byte(i*13 + 5)
	}
	want := wanted()
	want.Capabilities |= g.FesComputerCapabilityMediaSpectrumTape
	for _, batched := range []bool{true, false} {
		t.Run(fmt.Sprintf("batched=%t", batched), func(t *testing.T) {
			raw := &spectrumMediaFaultIO{
				faultIO:    fixture(want),
				mediaState: uint16(g.FesComputerMediaStateEmpty),
			}
			var ioPort IO = raw
			var batcher *spectrumMediaBatchedIO
			if batched {
				batcher = &spectrumMediaBatchedIO{raw}
				ioPort = batcher
			}
			c, err := Open(context.Background(), ioPort, limits(), want)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Hold(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := c.LoadInitialSpectrumTape(context.Background(), pzx); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw.received, pzx) {
				t.Fatalf("uploaded tape mismatch len=%d want=%d", len(raw.received), len(pzx))
			}
			wantBatches := 0
			if batched {
				wantBatches = 2
				if batcher.batches != wantBatches {
					t.Fatalf("expected %d media chunk batches, got %d", wantBatches, batcher.batches)
				}
			}
		})
	}
}
