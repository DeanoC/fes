// SPDX-License-Identifier: GPL-3.0-or-later
package scanout

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

type windowProbe struct {
	in            io.WriteCloser
	out           *bufio.Reader
	command       *exec.Cmd
	want          embeddedcontrol.Identity
	geometry      WindowGeometry
	frames        [2][]byte
	now           time.Time
	calls, starts int
}

func newWindowProbe(t *testing.T) *windowProbe {
	t.Helper()
	folder := os.Getenv("FES_WINDOW_APERTURE_DIR")
	if folder == "" {
		t.Skip("run verify_window.py for the actual Avalon/AXI window model")
	}
	raw, err := os.ReadFile(filepath.Join(folder, "descriptor.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		BuildID     string `json:"build_id"`
		Base, Bytes uint32
		Frames      [2]string
	}
	if err = json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	identity, err := hex.DecodeString(d.BuildID)
	if err != nil || len(identity) != 16 {
		t.Fatal("invalid model identity")
	}
	p := &windowProbe{want: embeddedcontrol.Identity{ABI: WindowControlABI}, geometry: WindowGeometry{d.Base, d.Bytes, 5120, 5120, 720}, now: time.Unix(100, 0)}
	for i := 0; i < 4; i++ {
		p.want.Build[i] = binary.BigEndian.Uint32(identity[(3-i)*4 : (4-i)*4])
	}
	for i, name := range d.Frames {
		p.frames[i], err = os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.frames[i]) != 5120*720 {
			t.Fatal("wrong actual rendered frame")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	p.command = exec.CommandContext(ctx, filepath.Join(folder, "obj/Vfes_de25_embedded"), strconv.FormatUint(uint64(d.Base), 10), d.Frames[0], d.Frames[1])
	if p.in, err = p.command.StdinPipe(); err != nil {
		cancel()
		t.Fatal(err)
	}
	stdout, err := p.command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	p.out = bufio.NewReader(stdout)
	p.command.Stderr = os.Stderr
	if err = p.command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.in.Close()
		if err := p.command.Wait(); err != nil {
			t.Errorf("actual window model: %v", err)
		}
		cancel()
	})
	return p
}
func (p *windowProbe) call(format string, args ...any) ([]uint64, error) {
	p.calls++
	if _, err := fmt.Fprintf(p.in, format+"\n", args...); err != nil {
		return nil, err
	}
	line, err := p.out.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "R" || fields[1] != "1" {
		return nil, fmt.Errorf("Avalon access rejected: %s", line)
	}
	values := make([]uint64, 0, len(fields)-2)
	for _, field := range fields[2:] {
		v, err := strconv.ParseUint(field, 16, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}
func (p *windowProbe) Read32(offset uint32) (uint32, error) {
	v, err := p.call("read %x", offset)
	if err != nil {
		return 0, err
	}
	if len(v) != 1 {
		return 0, errors.New("bad model read")
	}
	return uint32(v[0]), nil
}
func (p *windowProbe) Write32(offset, value uint32) error {
	if offset == windowCommand && value == 1 {
		p.starts++
	}
	_, err := p.call("write %x %x f", offset, value)
	return err
}
func (p *windowProbe) must(t *testing.T, format string, args ...any) []uint64 {
	t.Helper()
	v, err := p.call(format, args...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (p *windowProbe) read(t *testing.T, offset uint32) uint32 {
	t.Helper()
	v, err := p.Read32(offset)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (p *windowProbe) write(t *testing.T, offset, value uint32) {
	t.Helper()
	if err := p.Write32(offset, value); err != nil {
		t.Fatal(err)
	}
}
func (p *windowProbe) limits() ReaderLimits {
	return ReaderLimits{Timeout: time.Second, MaxPolls: 64, Now: func() time.Time { return p.now }, Yield: func() {
		if _, err := p.call("tick 2000"); err != nil {
			panic(err)
		}
		p.now = p.now.Add(time.Millisecond)
	}}
}
func (p *windowProbe) open(t *testing.T, generation uint64) *WindowReader {
	t.Helper()
	if err := (embeddedcontrol.Control{IO: p}).Enable(p.want, generation); err != nil {
		t.Fatal(err)
	}
	r, err := NewWindowReader(context.Background(), p, p.want, generation, p.geometry, p.limits())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (p *windowProbe) drain(t *testing.T, generation, cookie uint64) {
	t.Helper()
	control := embeddedcontrol.Control{IO: p}
	if err := control.Disable(p.want, generation, cookie); err != nil {
		t.Fatal(err)
	}
	p.must(t, "mode 0")
	p.must(t, "tick 4000")
	if err := control.Drained(p.want, generation, cookie); err != nil {
		t.Fatal(err)
	}
}
func (p *windowProbe) crc(index int) uint32 { return crc32.ChecksumIEEE(p.frames[index]) }
func (p *windowProbe) frame(t *testing.T, r *WindowReader, index int, cookie uint32) FrameRead {
	t.Helper()
	p.must(t, "load %x", index)
	s, err := r.ReadFrame(context.Background(), p.crc(index))
	if err != nil {
		t.Fatal(err)
	}
	if s.Generation != r.generation || s.Cookie != cookie || s.RequestedBytes != 3686400 || s.DeliveredBytes != 3686400 || s.CRC32 != p.crc(index) {
		t.Fatalf("wrong full-frame result: %+v", s)
	}
	return s
}
func (p *windowProbe) retired(t *testing.T, r *WindowReader) {
	t.Helper()
	calls := p.calls
	s, err := r.ReadFrame(context.Background(), p.crc(0))
	if !errors.Is(err, ErrReaderRetired) || s != (FrameRead{}) || calls != p.calls {
		t.Fatal("uncertain reader replayed I/O or published a result", s, err)
	}
}

type windowHookIO struct {
	p          *windowProbe
	afterWrite func(uint32, uint32) error
	afterRead  func(uint32, uint32) error
}

func (h windowHookIO) Read32(o uint32) (uint32, error) {
	v, err := h.p.Read32(o)
	if err == nil && h.afterRead != nil {
		err = h.afterRead(o, v)
	}
	return v, err
}
func (h windowHookIO) Write32(o, v uint32) error {
	if err := h.p.Write32(o, v); err != nil {
		return err
	}
	if h.afterWrite != nil {
		return h.afterWrite(o, v)
	}
	return nil
}

func TestWindowReaderActualAvalon(t *testing.T) {
	t.Run("drain-ack-retires-generation", func(t *testing.T) {
		p := newWindowProbe(t)
		r := p.open(t, 1)
		before := p.calls
		if got, err := r.Drain(context.Background(), 0); !errors.Is(err, ErrReaderArgument) || got != (ReaderDrain{}) || p.calls != before {
			t.Fatal("invalid drain changed ownership", got, err)
		}
		p.frame(t, r, 0, 1)
		cookie := uint64(0x8000000000000001)
		got, err := r.Drain(context.Background(), cookie)
		if err != nil || got != (ReaderDrain{1, cookie}) {
			t.Fatal("missing actual drain receipt", got, err)
		}
		p.retired(t, r)
		before = p.calls
		if _, err = r.Drain(context.Background(), cookie); !errors.Is(err, ErrReaderRetired) || p.calls != before {
			t.Fatal("completed stop was replayed", err)
		}
		r = p.open(t, 2)
		p.frame(t, r, 1, 1)
		// Endpoint drain cookies remain monotonic across generations.
		if _, err = r.Drain(context.Background(), cookie+1); err != nil {
			t.Fatal(err)
		}
	})
	for _, test := range []struct {
		name string
		mode int
	}{{"drain-held-address", 1}, {"drain-held-response", 2}} {
		t.Run(test.name, func(t *testing.T) {
			p := newWindowProbe(t)
			r := p.open(t, 1)
			p.must(t, "mode %x", test.mode)
			p.write(t, windowExpectLo, 1)
			p.write(t, windowExpectLo+4, 0)
			p.write(t, windowCookie, 1)
			p.write(t, windowCommand, 1)
			p.must(t, "tick 2000")
			r.limits.MaxPolls = 2
			got, err := r.Drain(context.Background(), 33)
			if !errors.Is(err, ErrReaderLimit) || got != (ReaderDrain{}) {
				t.Fatal("pending traffic supplied a drain", got, err)
			}
			p.retired(t, r)
			control := embeddedcontrol.Control{IO: p}
			if !errors.Is(control.Drained(p.want, 1, 33), embeddedcontrol.ErrBusy) {
				t.Fatal("elapsed polls fabricated an acknowledgement")
			}
			p.must(t, "mode 0")
			p.must(t, "tick 4000")
			if err = control.Drained(p.want, 1, 33); err != nil {
				t.Fatal("real traffic did not drain", err)
			}
		})
	}
	t.Run("drain-command-uncertainty", func(t *testing.T) {
		p := newWindowProbe(t)
		r := p.open(t, 1)
		cause := errors.New("accepted DISABLE response was lost")
		commands := 0
		r.io = windowHookIO{p: p, afterWrite: func(offset, value uint32) error {
			if offset == g.FesEmbeddedRegCommand && value == g.FesEmbeddedCommandDisable {
				commands++
				return cause
			}
			return nil
		}}
		got, err := r.Drain(context.Background(), 41)
		if !errors.Is(err, cause) || got != (ReaderDrain{}) || commands != 1 {
			t.Fatal("uncertain DISABLE was accepted or replayed", got, err, commands)
		}
		p.retired(t, r)
		p.must(t, "tick 4000")
		if err = (embeddedcontrol.Control{IO: p}).Drained(p.want, 1, 41); err != nil {
			t.Fatal("accepted command disappeared", err)
		}
	})
	t.Run("full-generation-token", func(t *testing.T) {
		p := newWindowProbe(t)
		generation := uint64(0x8000000000000001)
		r := p.open(t, generation)
		p.write(t, windowExpectLo, 1)
		p.write(t, windowExpectLo+4, 0)
		p.write(t, windowCookie, 1)
		if err := p.Write32(windowCommand, 1); err == nil {
			t.Fatal("matching low word bypassed the full generation")
		}
		p.frame(t, r, 0, 1)
		p.drain(t, generation, 19)
	})
	t.Run("two-frames-drain-new-owner", func(t *testing.T) {
		p := newWindowProbe(t)
		r := p.open(t, 1)
		p.frame(t, r, 0, 1)
		if _, err := NewWindowReader(context.Background(), p, p.want, 1, p.geometry, p.limits()); !errors.Is(err, ErrReaderProtocol) {
			t.Fatal("already used epoch was reopened", err)
		}
		p.frame(t, r, 1, 2)
		if p.read(t, g.FesEmbeddedRegStatus) != g.FesEmbeddedStatusEnabled || p.read(t, g.FesEmbeddedRegAckCookieLo) != 0 {
			t.Fatal("frame completion invented reader drain")
		}
		p.drain(t, 1, 17)
		r = p.open(t, 2)
		p.frame(t, r, 0, 1)
		p.drain(t, 2, 18)
	})
	t.Run("identity-geometry-arguments", func(t *testing.T) {
		p := newWindowProbe(t)
		p.open(t, 1)
		want := p.want
		want.Build[0] ^= 1
		if _, err := NewWindowReader(context.Background(), p, want, 1, p.geometry, p.limits()); !errors.Is(err, ErrReaderIdentity) {
			t.Fatal(err)
		}
		geometry := p.geometry
		geometry.Base += 4096
		if _, err := NewWindowReader(context.Background(), p, p.want, 1, geometry, p.limits()); !errors.Is(err, ErrReaderIdentity) {
			t.Fatal(err)
		}
		for _, test := range []func(*WindowGeometry, *ReaderLimits, *embeddedcontrol.Identity, *uint64){
			func(g *WindowGeometry, _ *ReaderLimits, _ *embeddedcontrol.Identity, _ *uint64) { g.Rows = 0 },
			func(g *WindowGeometry, _ *ReaderLimits, _ *embeddedcontrol.Identity, _ *uint64) { g.Base++ },
			func(g *WindowGeometry, _ *ReaderLimits, _ *embeddedcontrol.Identity, _ *uint64) { g.Bytes = 16 },
			func(_ *WindowGeometry, l *ReaderLimits, _ *embeddedcontrol.Identity, _ *uint64) { l.MaxPolls = 0 },
			func(_ *WindowGeometry, l *ReaderLimits, _ *embeddedcontrol.Identity, _ *uint64) { l.Timeout = 0 },
			func(_ *WindowGeometry, _ *ReaderLimits, i *embeddedcontrol.Identity, _ *uint64) { i.ABI ^= 1 },
			func(_ *WindowGeometry, _ *ReaderLimits, _ *embeddedcontrol.Identity, g *uint64) { *g = 0 },
		} {
			geometry, limits, want, generation := p.geometry, p.limits(), p.want, uint64(1)
			test(&geometry, &limits, &want, &generation)
			calls := p.calls
			if _, err := NewWindowReader(context.Background(), p, want, generation, geometry, limits); !errors.Is(err, ErrReaderArgument) || p.calls != calls {
				t.Fatal("invalid argument performed I/O", err)
			}
		}
		if p.must(t, "stats")[0] != 0 || p.starts != 0 {
			t.Fatal("rejected caller reached memory")
		}
	})
	t.Run("stale-cookie-and-register-rejection", func(t *testing.T) {
		p := newWindowProbe(t)
		r := p.open(t, 1)
		p.write(t, windowExpectLo, 2)
		p.write(t, windowCookie, 1)
		if err := p.Write32(windowCommand, 1); err == nil {
			t.Fatal("foreign generation started")
		}
		p.write(t, windowExpectLo, 1)
		p.write(t, windowCookie, 0)
		if err := p.Write32(windowCommand, 1); err == nil {
			t.Fatal("zero cookie started")
		}
		p.frame(t, r, 0, 1)
		if err := p.Write32(windowCommand, 1); err == nil {
			t.Fatal("completed cookie was replayed")
		}
		if _, err := p.call("write 8c 2 3"); err == nil {
			t.Fatal("partial write accepted")
		}
		if err := p.Write32(windowBase, 0); err == nil {
			t.Fatal("compiled window was writable")
		}
		if _, err := p.Read32(0xac); err == nil {
			t.Fatal("unknown CSR accepted")
		}
		p.frame(t, r, 1, 2)
	})
	t.Run("latched-snapshot", func(t *testing.T) {
		p := newWindowProbe(t)
		p.open(t, 1)
		p.write(t, windowExpectLo, 1)
		p.write(t, windowCookie, 1)
		p.write(t, windowCommand, 1)
		if err := p.Write32(windowCommand, 1); err == nil {
			t.Fatal("busy start accepted")
		}
		if err := p.Write32(windowExpectLo, 2); err == nil {
			t.Fatal("busy descriptor changed")
		}
		p.must(t, "tick 2000")
		p.read(t, windowStatus)
		before := p.read(t, windowDelivered)
		crc := p.read(t, windowCRC)
		p.must(t, "tick 4000")
		if p.read(t, windowDelivered) != before || p.read(t, windowCRC) != crc {
			t.Fatal("result changed without a new snapshot")
		}
		p.read(t, windowStatus)
		if p.read(t, windowDelivered) <= before {
			t.Fatal("fresh snapshot did not observe progress")
		}
		for i := 0; i < 64; i++ {
			if p.read(t, windowStatus)&windowDone != 0 {
				break
			}
			p.must(t, "tick 2000")
		}
		if p.read(t, windowStatus)&windowDone == 0 || p.read(t, windowCRC) != p.crc(0) || p.read(t, windowDelivered) != 3686400 {
			t.Fatal("full CRC result absent")
		}
	})
	for _, test := range []struct {
		name string
		mode uint32
		want error
	}{
		{"rresp", 3, ErrReaderProtocol}, {"rid", 4, ErrReaderProtocol}, {"rlast", 5, ErrReaderProtocol}, {"corrupt-pixel", 6, ErrReaderData},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := newWindowProbe(t)
			r := p.open(t, 1)
			p.must(t, "mode %x", test.mode)
			s, err := r.ReadFrame(context.Background(), p.crc(0))
			if !errors.Is(err, test.want) || s != (FrameRead{}) {
				t.Fatal("bad data/protocol published a frame", s, err)
			}
			p.retired(t, r)
			if p.read(t, g.FesEmbeddedRegAckCookieLo) != 0 {
				t.Fatal("failure invented drain")
			}
			if test.mode == 6 {
				p.drain(t, 1, 27)
			}
		})
	}
	for _, test := range []struct {
		name string
		mode uint32
	}{{"held-address", 1}, {"missing-responses", 2}} {
		t.Run(test.name, func(t *testing.T) {
			p := newWindowProbe(t)
			r := p.open(t, 1)
			r.limits.MaxPolls = 4
			r.limits.Now = func() time.Time { return time.Unix(100, 0) }
			p.must(t, "mode %x", test.mode)
			var reads, writes uint32
			r.io = windowHookIO{p: p,
				afterRead:  func(uint32, uint32) error { reads++; return nil },
				afterWrite: func(uint32, uint32) error { writes++; return nil },
			}
			s, err := r.ReadFrame(context.Background(), p.crc(0))
			if !errors.Is(err, ErrReaderLimit) || s != (FrameRead{}) {
				t.Fatal(s, err)
			}
			var failure *ReaderFailure
			if !errors.As(err, &failure) || failure.Operation != "frame" || failure.Stage != "poll-limit" || failure.Limit != "polls" ||
				failure.Polls != 4 || failure.SamplePoll != 4 || failure.Reads != reads || failure.Writes != writes || writes != 4 ||
				!failure.StartAttempted || !failure.SnapshotComplete || failure.Flags != windowBusy ||
				failure.Sample.Generation != 1 || failure.Sample.Cookie != 1 ||
				failure.Sample.RequestedBytes == 0 || failure.Sample.DeliveredBytes != 0 {
				t.Fatalf("missing cached failed-frame progress: %+v", failure)
			}
			calls, now := p.calls, p.now
			if fmt.Sprint(err) != ErrReaderLimit.Error() || p.calls != calls || p.now != now {
				t.Fatal("formatting failure evidence performed I/O or changed the error")
			}
			p.retired(t, r)
			control := embeddedcontrol.Control{IO: p}
			if err = control.Disable(p.want, 1, 33); err != nil {
				t.Fatal(err)
			}
			p.must(t, "tick 2000")
			if !errors.Is(control.Drained(p.want, 1, 33), embeddedcontrol.ErrBusy) {
				t.Fatal("elapsed cycles invented drain")
			}
			p.must(t, "mode 0")
			p.must(t, "tick 4000")
			if err = control.Drained(p.want, 1, 33); err != nil {
				t.Fatal(err)
			}
			if p.read(t, windowStatus)&windowDone != 0 {
				t.Fatal("partial return published completion")
			}
			r = p.open(t, 2)
			p.frame(t, r, 1, 1)
		})
	}
	for _, failAt := range []uint32{1, 2} {
		t.Run(fmt.Sprintf("partial-snapshot-%d", failAt), func(t *testing.T) {
			p := newWindowProbe(t)
			r := p.open(t, 1)
			p.must(t, "mode 2")
			r.limits.Now = func() time.Time { return time.Unix(100, 0) }
			cause := errors.New("snapshot read return uncertain")
			var snapshots uint32
			r.io = windowHookIO{p: p, afterRead: func(offset, value uint32) error {
				if offset == windowCRC {
					snapshots++
					if snapshots == failAt {
						return cause
					}
				}
				return nil
			}}
			result, err := r.ReadFrame(context.Background(), p.crc(0))
			var failure *ReaderFailure
			if result != (FrameRead{}) || !errors.Is(err, cause) || !errors.As(err, &failure) ||
				failure.Stage != "poll-snapshot" || failure.Polls != failAt || failure.SamplePoll != failAt-1 ||
				failure.SnapshotComplete != (failAt == 2) {
				t.Fatalf("partial snapshot changed the last complete sample: %+v, %v", failure, err)
			}
			if failAt == 1 && failure.Sample != (FrameRead{}) {
				t.Fatal("partial sample escaped", failure.Sample)
			}
			p.retired(t, r)
			p.drain(t, 1, 43)
		})
	}
	for _, name := range []string{"cancel-after-start", "uncertain-start-write", "late-start-return", "backwards-clock"} {
		t.Run(name, func(t *testing.T) {
			p := newWindowProbe(t)
			r := p.open(t, 1)
			p.must(t, "mode 2")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("accepted write with uncertain return")
			want := failure
			if name == "cancel-after-start" {
				want = context.Canceled
			}
			if name == "late-start-return" || name == "backwards-clock" {
				want = ErrReaderLimit
			}
			r.io = windowHookIO{p: p, afterWrite: func(offset, value uint32) error {
				if offset != windowCommand {
					return nil
				}
				switch name {
				case "cancel-after-start":
					cancel()
				case "late-start-return":
					p.now = p.now.Add(2 * time.Second)
				case "backwards-clock":
					p.now = p.now.Add(-time.Second)
				default:
					return failure
				}
				return nil
			}}
			s, err := r.ReadFrame(ctx, p.crc(0))
			if !errors.Is(err, want) || s != (FrameRead{}) || p.starts != 1 {
				t.Fatal("uncertain START was replayed or published", s, err, p.starts)
			}
			var diagnostic *ReaderFailure
			if !errors.As(err, &diagnostic) || diagnostic.Stage != "start" || !diagnostic.StartAttempted ||
				diagnostic.SnapshotComplete || diagnostic.Sample != (FrameRead{}) || diagnostic.Polls != 0 {
				t.Fatalf("uncertain START fabricated a result: %+v", diagnostic)
			}
			if name == "late-start-return" && (diagnostic.Limit != "timeout" || diagnostic.Elapsed < 2*time.Second) ||
				name == "backwards-clock" && (diagnostic.Limit != "clock" || diagnostic.Elapsed >= 0) {
				t.Fatal("deadline and clock failures were conflated", diagnostic.Limit, diagnostic.Elapsed)
			}
			p.retired(t, r)
			p.must(t, "tick 2000")
			p.drain(t, 1, 41)
		})
	}
	t.Run("closed-before-read", func(t *testing.T) {
		p := newWindowProbe(t)
		r := p.open(t, 1)
		p.drain(t, 1, 51)
		if s, err := r.ReadFrame(context.Background(), p.crc(0)); !errors.Is(err, ErrReaderProtocol) || s != (FrameRead{}) || p.starts != 0 {
			t.Fatal("closed epoch started a frame", s, err)
		}
		p.retired(t, r)
	})
	t.Run("owner-changed-after-frame", func(t *testing.T) {
		p := newWindowProbe(t)
		r := p.open(t, 1)
		finished, changed := false, false
		r.io = windowHookIO{p: p, afterRead: func(offset, value uint32) error {
			if offset == windowStatus && value&windowDone != 0 {
				finished = true
			}
			if offset == windowCRC && finished && !changed {
				changed = true
				p.drain(t, 1, 52)
				return (embeddedcontrol.Control{IO: p}).Enable(p.want, 2)
			}
			return nil
		}}
		if s, err := r.ReadFrame(context.Background(), p.crc(0)); !changed || !errors.Is(err, ErrReaderIdentity) || s != (FrameRead{}) {
			t.Fatal("retired owner published its completed frame", s, err)
		}
		p.retired(t, r)
	})
	t.Run("pre-cancel-and-concurrency", func(t *testing.T) {
		p := newWindowProbe(t)
		r := p.open(t, 1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := p.calls
		if _, err := r.ReadFrame(ctx, p.crc(0)); !errors.Is(err, context.Canceled) || p.calls != calls {
			t.Fatal("pre-cancel touched hardware", err)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		r.io = windowHookIO{p: p, afterWrite: func(offset, value uint32) error {
			if offset == windowExpectLo {
				close(entered)
				<-release
			}
			return nil
		}}
		finished := make(chan error, 1)
		go func() { _, err := r.ReadFrame(context.Background(), p.crc(0)); finished <- err }()
		<-entered
		calls = p.calls
		if _, err := r.ReadFrame(context.Background(), p.crc(0)); !errors.Is(err, ErrReaderBusy) || p.calls != calls {
			t.Fatal("concurrent operation touched hardware", err)
		}
		close(release)
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	})
}
