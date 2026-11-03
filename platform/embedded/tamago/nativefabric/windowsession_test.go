// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"hash/crc32"
	stdimage "image"
	"math"
	"reflect"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

type presentationFixture struct {
	v               *HPSContextVerifier
	cpu             *windowCPU
	control, access []byte
	transfer        *ConfigurationTransfer
	region          *Region
	frame           *stdimage.RGBA
	expected        []byte
	publications    int
	barrier         func() error
}

func newPresentationFixture(t *testing.T) *presentationFixture {
	t.Helper()
	v, cpu, control, access, transfer := windowReadFixture(t)
	f := &presentationFixture{v: v, cpu: cpu, control: control, access: access, transfer: transfer, region: v.readerMemoryRegion,
		frame: stdimage.NewRGBA(stdimage.Rect(0, 0, 1280, 720)), expected: make([]byte, 3686400)}
	f.colour(1)
	f.region.OrderFrameStores = func() error {
		f.publications++
		if cpu.registers[g.FesEmbeddedRegStatus] != g.FesEmbeddedStatusDrained || cpu.registers[g.FesEmbeddedRegPending] != 0 ||
			cpu.registers[g.FesEmbeddedRegAckGenerationLo] != cpu.registers[g.FesEmbeddedRegGenerationLo] || cpu.registers[g.FesEmbeddedRegAckCookieLo] == 0 {
			return errors.New("CPU publication reached an unstopped reader")
		}
		cpu.crc = crc32.ChecksumIEEE(f.expected)
		if f.barrier != nil {
			return f.barrier()
		}
		return nil
	}
	return f
}
func (f *presentationFixture) colour(red byte) {
	for i := 0; i < len(f.expected); i += 4 {
		copy(f.frame.Pix[i:i+4], []byte{red, 0x3a, 0xa7, 0x20})
		copy(f.expected[i:i+4], []byte{0xa7, 0x3a, red, 0xff})
	}
}
func (f *presentationFixture) open(t *testing.T, ctx context.Context) *WindowSession {
	t.Helper()
	s, err := f.v.OpenWindowSession(ctx, f.control, f.access, f.transfer, windowReadLimits())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWindowSessionRepeatedPresentationAndReturn(t *testing.T) {
	f := newPresentationFixture(t)
	s := f.open(t, context.Background())
	seen := make(map[uint32]bool)
	for i := 1; i <= 12; i++ {
		f.colour(byte(i))
		got, err := s.PresentFrame(context.Background(), f.frame)
		if err != nil || got.Frame.Generation != uint64(i+1) || got.Frame.Cookie != 1 || got.Frame.CRC32 != crc32.ChecksumIEEE(f.expected) ||
			got.Frame.RequestedBytes != 3686400 || got.Frame.DeliveredBytes != 3686400 {
			t.Fatal(i, got, err)
		}
		if !bytes.Equal(s.bytes[:3686400], f.expected) || !bytes.Equal(s.bytes[3686400:], make([]byte, (4<<20)-3686400)) {
			t.Fatal("wrong pixels or overwritten padding", i)
		}
		if f.v.windowPublication.FrameSHA256 != sha256.Sum256(f.expected) || f.v.windowArenaSHA != sha256.Sum256(s.bytes) {
			t.Fatal("publication digests lost expected pixels", i)
		}
		if got.Memory.FrameSHA256 != sha256.Sum256(f.expected) || got.Memory.CRC32 != got.Frame.CRC32 ||
			got.Drain != (scanout.ReaderDrain{Generation: uint64(i + 1), Cookie: uint64(i + 1)}) {
			t.Fatal("presentation receipt lost its publication or local drain", i, got)
		}
		if !s.drained || f.cpu.registers[g.FesEmbeddedRegStatus] != g.FesEmbeddedStatusDrained || f.cpu.lastDrainCookie != uint64(i+1) {
			t.Fatal("frame returned without its own drain", i)
		}
		seen[got.Frame.CRC32] = true
		// Application storage is not retained as DMA backing.
		f.frame.Pix[0] ^= 1
		if !bytes.Equal(s.bytes[:3686400], f.expected) {
			t.Fatal("caller storage escaped into the DMA window")
		}
	}
	returned, err := s.Close()
	if err != nil || returned.Frames != 12 || returned.LastFrame.Generation != 13 || returned.Source.Bridges.Generation != 4 ||
		returned.After.Generation != 7 || returned.After.ResetMask != 15 || !returned.After.MastersContained ||
		f.publications != 12 || len(seen) != 12 || f.cpu.starts != 12 || f.cpu.disables != 13 || f.cpu.closes != 1 || f.cpu.opens != 1 || f.cpu.dataOpens != 1 || f.cpu.releases != 0 {
		t.Fatal(returned, err, f.cpu, f.publications)
	}
	before := f.cpu.calls + f.cpu.reads + f.cpu.writes
	again, err := s.Close()
	if err != nil || again != returned || f.cpu.calls+f.cpu.reads+f.cpu.writes != before {
		t.Fatal("return was replayed", again, err)
	}
	if _, err = s.PresentFrame(context.Background(), f.frame); !errors.Is(err, scanout.ErrReaderRetired) || f.cpu.calls+f.cpu.reads+f.cpu.writes != before {
		t.Fatal("returned presenter accessed hardware", err)
	}
}

func TestWindowSessionValidatesBeforeMutation(t *testing.T) {
	f := newPresentationFixture(t)
	hook := f.region.OrderFrameStores
	f.region.OrderFrameStores = nil
	if _, err := f.v.OpenWindowSession(context.Background(), f.control, f.access, f.transfer, windowReadLimits()); err == nil || f.cpu.calls != 0 || f.v.ddrReaderAttempted {
		t.Fatal("missing publication hook consumed ownership", err)
	}
	f.region.OrderFrameStores = hook
	s := f.open(t, context.Background())
	before := f.cpu.calls + f.cpu.reads + f.cpu.writes
	// Reflect intentionally exercises a complete value copy despite atomic.Bool's
	// static no-copy annotation. Neither a copied nor a zero handle owns the lock.
	copied := &WindowSession{}
	reflect.ValueOf(copied).Elem().Set(reflect.ValueOf(s).Elem())
	for _, invalid := range []*WindowSession{copied, {}} {
		if _, err := invalid.PresentFrame(context.Background(), f.frame); !errors.Is(err, scanout.ErrReaderArgument) {
			t.Fatal("forged presenter accepted", err)
		}
		if _, err := invalid.Close(); !errors.Is(err, scanout.ErrReaderArgument) {
			t.Fatal("forged presenter stole return ownership", err)
		}
	}
	if f.cpu.calls+f.cpu.reads+f.cpu.writes != before {
		t.Fatal("forged presenter accessed hardware")
	}
	for _, frame := range []*stdimage.RGBA{nil, stdimage.NewRGBA(stdimage.Rect(0, 0, 1, 1))} {
		if _, err := s.PresentFrame(context.Background(), frame); err == nil || f.cpu.calls+f.cpu.reads+f.cpu.writes != before || f.publications != 0 {
			t.Fatal("invalid frame mutated the reader", err)
		}
	}
	if err := s.Present(f.frame); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowSessionFailuresRetainArenaAndDoNotReplay(t *testing.T) {
	for _, fault := range []string{"drain-held", "barrier", "corrupt-bytes", "padding", "owner", "region", "slice", "hook-removed", "crc", "short", "generation", "cookie", "response", "start", "drain-after-frame", "cancel-copy", "cancel-start"} {
		t.Run(fault, func(t *testing.T) {
			f := newPresentationFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := f.open(t, ctx)
			cause := errors.New("injected publication barrier uncertainty")
			f.cpu.fault = fault
			f.barrier = func() error {
				switch fault {
				case "barrier":
					return cause
				case "corrupt-bytes":
					s.bytes[0] ^= 1
				case "padding":
					s.bytes[len(s.bytes)-1] ^= 1
				case "owner":
					f.cpu.state.Owner++
				case "region":
					f.v.readerMemoryRegion = nil
				case "slice":
					f.region.Bytes = bytes.Clone(f.region.Bytes)
				case "hook-removed":
					f.region.OrderFrameStores = nil
				}
				return nil
			}
			f.cpu.after = func(event string) {
				if fault == "cancel-copy" && s.memory.written > 0 && s.memory.written < 3686400 {
					cancel()
				}
				if fault == "cancel-start" && f.cpu.starts > 0 {
					cancel()
				}
				if fault == "drain-after-frame" && f.cpu.starts > 0 {
					f.cpu.fault = "drain-held"
				}
			}
			got, err := s.PresentFrame(ctx, f.frame)
			if err == nil || got != (WindowPresentation{}) {
				t.Fatal("failed presentation supplied a frame", got, err)
			}
			if fault == "barrier" && !errors.Is(err, cause) {
				t.Fatal("lost publication error", err)
			}
			if (fault == "drain-held" || fault == "cancel-copy") && (f.publications != 0 || f.cpu.starts != 0) {
				t.Fatal("unstopped or partial frame was published")
			}
			if fault == "barrier" || fault == "corrupt-bytes" || fault == "padding" || fault == "owner" || fault == "region" || fault == "slice" || fault == "hook-removed" {
				if f.cpu.starts != 0 {
					t.Fatal("uncertain CPU publication reached START")
				}
			}
			before := f.cpu.calls + f.cpu.reads + f.cpu.writes
			if _, err = s.PresentFrame(context.Background(), f.frame); !errors.Is(err, scanout.ErrReaderRetired) || f.cpu.calls+f.cpu.reads+f.cpu.writes != before {
				t.Fatal("failed presenter replayed work", err)
			}
			returned, closeErr := s.Close()
			if closeErr == nil || returned != (WindowSessionReturn{}) || f.cpu.releases != 0 {
				t.Fatal("failed presentation granted reuse or released memory", returned, closeErr)
			}
			before = f.cpu.calls + f.cpu.reads + f.cpu.writes
			if _, again := s.Close(); again != closeErr || f.cpu.calls+f.cpu.reads+f.cpu.writes != before {
				t.Fatal("failed close was replayed", again)
			}
		})
	}
}

func TestWindowSessionConcurrentPresentationAndClose(t *testing.T) {
	f := newPresentationFixture(t)
	s := f.open(t, context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	f.barrier = func() error { close(entered); <-release; return nil }
	done := make(chan error, 1)
	go func() { done <- s.Present(f.frame) }()
	<-entered
	before := f.cpu.calls + f.cpu.reads + f.cpu.writes
	if err := s.Present(f.frame); !errors.Is(err, scanout.ErrReaderBusy) {
		t.Fatal("concurrent CPU frame admitted", err)
	}
	if _, err := s.Close(); !errors.Is(err, scanout.ErrReaderBusy) {
		t.Fatal("active frame ownership was closed", err)
	}
	if _, err := f.v.ProbeWindowReader(context.Background(), f.control, f.access, f.transfer, windowReadLimits()); !errors.Is(err, scanout.ErrReaderBusy) {
		t.Fatal("completed transfer was stolen", err)
	}
	if f.cpu.calls+f.cpu.reads+f.cpu.writes != before {
		t.Fatal("busy operations touched hardware")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowSessionExhaustionDoesNotOverwrite(t *testing.T) {
	for _, which := range []string{"generation", "cookie"} {
		t.Run(which, func(t *testing.T) {
			f := newPresentationFixture(t)
			s := f.open(t, context.Background())
			before := sha256.Sum256(s.bytes)
			if which == "generation" {
				s.generation = math.MaxUint64
			} else {
				s.nextDrain = math.MaxUint64
			}
			got, err := s.PresentFrame(context.Background(), f.frame)
			if !errors.Is(err, scanout.ErrReaderLimit) || got != (WindowPresentation{}) || sha256.Sum256(s.bytes) != before || f.publications != 0 || f.cpu.starts != 0 {
				t.Fatal("exhaustion wrapped a token or overwrote a frame", got, err)
			}
			if _, err = s.Close(); err == nil || f.cpu.releases != 0 {
				t.Fatal("exhausted session supplied a new grant", err)
			}
		})
	}
}
