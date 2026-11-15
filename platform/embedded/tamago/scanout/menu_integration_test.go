// SPDX-License-Identifier: GPL-3.0-or-later
package scanout

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
)

var errMenuInjected = errors.New("menu platform uncertainty")

type menuProbe struct {
	in                          io.WriteCloser
	out                         *bufio.Reader
	command                     *exec.Cmd
	frames                      [2]*image.RGBA
	now                         time.Time
	calls, copies, publications int
	commands                    [4]int
	owned                       bool
	slot                        uint32
	onRead                      func(uint32, uint32) (uint32, error)
	onWrite                     func(uint32, uint32) error
	onCopy                      func() error
	onPublish                   func() error
}

func newMenuProbe(t *testing.T) *menuProbe {
	t.Helper()
	directory := os.Getenv("FES_MENU_CSR_DIR")
	if directory == "" {
		t.Skip("run verify_menu.py for actual menu CSR/AXI/video integration")
	}
	var d struct {
		Executable string
		Frames     [2]string
	}
	raw, err := os.ReadFile(filepath.Join(directory, "descriptor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	p := &menuProbe{now: time.Unix(1, 0), owned: true}
	for i, path := range d.Frames {
		bytes, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if len(bytes) != 1280*720*4 {
			t.Fatal("frame extent")
		}
		frame := image.NewRGBA(image.Rect(0, 0, 1280, 720))
		for j := 0; j < len(bytes); j += 4 {
			frame.Pix[j] = bytes[j+2]
			frame.Pix[j+1] = bytes[j+1]
			frame.Pix[j+2] = bytes[j]
			frame.Pix[j+3] = 255
		}
		p.frames[i] = frame
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	p.command = exec.CommandContext(ctx, d.Executable, d.Frames[0], d.Frames[1])
	p.command.Stderr = os.Stderr
	p.in, err = p.command.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdout, err := p.command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	p.out = bufio.NewReader(stdout)
	if err = p.command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.in.Close()
		if err := p.command.Wait(); err != nil {
			t.Errorf("menu RTL model: %v", err)
		}
		cancel()
	})
	p.must(t, "mode 0")
	return p
}
func (p *menuProbe) call(format string, args ...any) ([]uint64, error) {
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
		return nil, fmt.Errorf("menu CSR rejected: %s", line)
	}
	values := make([]uint64, len(fields)-2)
	for i, field := range fields[2:] {
		v, e := strconv.ParseUint(field, 16, 64)
		if e != nil {
			return nil, e
		}
		values[i] = v
	}
	return values, nil
}
func (p *menuProbe) must(t *testing.T, format string, args ...any) []uint64 {
	t.Helper()
	v, e := p.call(format, args...)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func (p *menuProbe) Read32(offset uint32) (uint32, error) {
	v, err := p.call("read %x", offset)
	if err != nil {
		return 0, err
	}
	if len(v) != 1 {
		return 0, ErrReaderProtocol
	}
	value := uint32(v[0])
	if p.onRead != nil {
		return p.onRead(offset, value)
	}
	return value, nil
}
func (p *menuProbe) Write32(offset, value uint32) error {
	if offset == menuCommand && value < 4 {
		p.commands[value]++
	}
	if _, err := p.call("write %x %x", offset, value); err != nil {
		return err
	}
	if p.onWrite != nil {
		return p.onWrite(offset, value)
	}
	return nil
}
func (p *menuProbe) Size() uint64 { return MenuArenaBytes }
func (p *menuProbe) Write(offset uint64, data []byte) error {
	p.copies++
	p.slot = uint32(offset / menuSlotBytes)
	if _, err := p.call("memory %x %s", offset, hex.EncodeToString(data)); err != nil {
		return err
	}
	if p.onCopy != nil {
		return p.onCopy()
	}
	return nil
}
func (p *menuProbe) Publish() error {
	p.publications++
	if _, err := p.call("publish %x", p.slot); err != nil {
		return err
	}
	if p.onPublish != nil {
		return p.onPublish()
	}
	return nil
}
func (p *menuProbe) hooks() MenuHooks {
	return MenuHooks{IO: p, Memory: p, Owns: func() error {
		if !p.owned {
			return errMenuInjected
		}
		return nil
	}}
}
func (p *menuProbe) limits() MenuLimits {
	return MenuLimits{PublicationTimeout: 4 * time.Second, ReaderLimits: ReaderLimits{Timeout: time.Second, MaxPolls: 64, Now: func() time.Time { return p.now }, Yield: func() {
		if _, err := p.call("tick 10000"); err != nil {
			panic(err)
		}
		p.now = p.now.Add(time.Millisecond)
	}}}
}
func (p *menuProbe) open(t *testing.T) *MenuPresenter {
	t.Helper()
	want := embeddedcontrol.Identity{ABI: MenuControlABI, Build: [4]uint32{1}}
	if err := (embeddedcontrol.Control{IO: p}).Enable(want, 1); err != nil {
		t.Fatal(err)
	}
	m, err := NewMenuPresenter(context.Background(), p.hooks(), want, 1, 0x95000000, p.limits())
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func (p *menuProbe) retired(t *testing.T, m *MenuPresenter) {
	t.Helper()
	before := p.calls
	if s, err := m.PresentFrame(context.Background(), p.frames[0]); !errors.Is(err, ErrReaderRetired) || s != (MenuFrame{}) {
		t.Fatal("retired presenter accepted frame", s, err)
	}
	if s, err := m.Stop(context.Background(), 20); !errors.Is(err, ErrReaderRetired) || s != (ReaderDrain{}) {
		t.Fatal("retired presenter replayed stop", s, err)
	}
	if before != p.calls {
		t.Fatal("retired presenter accessed platform")
	}
}
func TestMenuActualRTL(t *testing.T) {
	t.Run("capacity_retains_one_pending_update_across_scanout_phases", testMenuQueuedCapacity)
	t.Run("slow_publication_preserves_full_hardware_budget", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		// 180 bounded writes (0.9s) plus publication/integrity (1.6s)
		// reproduce the physical CPU cost without waiting on the host clock.
		p.onCopy = func() error { p.now = p.now.Add(5 * time.Millisecond); return nil }
		p.onPublish = func() error { p.now = p.now.Add(1600 * time.Millisecond); return nil }
		p.onWrite = func(offset, value uint32) error {
			if offset == menuCommand && (value == 1 || value == 2) {
				p.now = p.now.Add(900 * time.Millisecond)
			}
			return nil
		}
		for i := 0; i < 2; i++ {
			got, err := m.PresentFrame(context.Background(), p.frames[i])
			if err != nil || got.Sequence != uint32(i+1) || got.Slot != uint32(i) {
				t.Fatal("slow CPU work consumed hardware budget", got, err)
			}
		}
		if _, err := m.Stop(context.Background(), 21); err != nil {
			t.Fatal(err)
		}
	})
	for _, stage := range []string{"copy", "publish", "command"} {
		t.Run(stage+"_deadline_cannot_be_extended", func(t *testing.T) {
			p := newMenuProbe(t)
			m := p.open(t)
			p.onCopy = func() error { p.now = p.now.Add(20 * time.Millisecond); return nil }
			p.onPublish = func() error { p.now = p.now.Add(500 * time.Millisecond); return nil }
			if stage == "copy" {
				p.onCopy = func() error { p.now = p.now.Add(25 * time.Millisecond); return nil }
			}
			if stage == "command" {
				p.onPublish = func() error { return nil }
				p.onWrite = func(offset, value uint32) error {
					if offset == menuCommand {
						p.now = p.now.Add(time.Second)
					}
					return nil
				}
			}
			got, err := m.PresentFrame(context.Background(), p.frames[0])
			var failure *MenuFailure
			if !errors.Is(err, ErrReaderLimit) || !errors.As(err, &failure) || got != (MenuFrame{}) || failure.Stage != stage {
				t.Fatal("phase overrun accepted or misreported", got, err)
			}
			wantCommands := 0
			if stage == "command" {
				wantCommands = 1
			}
			if p.commands[1] != wantCommands || failure.CommandAttempted != (wantCommands == 1) {
				t.Fatal("overrun submitted or replayed START", p.commands, failure)
			}
			p.retired(t, m)
		})
	}
	for _, fault := range []string{"clock", "owner"} {
		t.Run("publication_"+fault+"_cannot_cross_phase_boundary", func(t *testing.T) {
			p := newMenuProbe(t)
			m := p.open(t)
			p.onPublish = func() error {
				if fault == "clock" {
					p.now = p.now.Add(-time.Millisecond)
				} else {
					p.owned = false
				}
				return nil
			}
			got, err := m.PresentFrame(context.Background(), p.frames[0])
			if err == nil || got != (MenuFrame{}) || p.commands[1] != 0 {
				t.Fatal("publication uncertainty crossed phase boundary", got, err)
			}
			p.retired(t, m)
		})
	}
	t.Run("copied_handle_cannot_fork_ownership", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		copy := *m
		before := p.calls
		if _, err := copy.PresentFrame(context.Background(), p.frames[0]); !errors.Is(err, ErrReaderArgument) {
			t.Fatal(err)
		}
		if _, err := copy.Stop(context.Background(), 1); !errors.Is(err, ErrReaderArgument) {
			t.Fatal(err)
		}
		if p.calls != before {
			t.Fatal("copied owner touched platform")
		}
	})
	t.Run("three_frames_and_owned_drain", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		for i, frame := range []int{0, 1, 0} {
			s, err := m.PresentFrame(context.Background(), p.frames[frame])
			if err != nil {
				t.Fatal(err)
			}
			if s.Generation != 1 || s.Sequence != uint32(i+1) || s.Submitted != s.Sequence || s.Slot != uint32(i%2) || s.Underflows != 0 {
				t.Fatal("wrong complete-frame receipt", s)
			}
		}
		s, err := m.Stop(context.Background(), 7)
		if err != nil || s != (ReaderDrain{1, 7}) {
			t.Fatal("owned endpoint drain", s, err)
		}
		if p.commands != [4]int{0, 1, 2, 1} || p.publications != 3 {
			t.Fatal("command/publication sequence", p.commands, p.publications)
		}
		v := p.must(t, "stats")
		if v[2] != 0 || v[3] < 2764800 || v[4] != 3 || v[5] != 0 || v[6] != 0 || v[7] != 1 {
			t.Fatal("actual raster/drain evidence", v)
		}
		p.retired(t, m)
	})
	t.Run("idle_stop_does_not_start_video", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		s, err := m.Stop(context.Background(), 1)
		if err != nil || s != (ReaderDrain{1, 1}) || p.copies != 0 || p.commands != [4]int{} {
			t.Fatal(s, err, p.commands)
		}
		p.retired(t, m)
	})
	t.Run("invalid_frame_preserves_owner", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		before := p.calls
		if _, err := m.PresentFrame(context.Background(), image.NewRGBA(image.Rect(0, 0, 1, 1))); err == nil || before != p.calls {
			t.Fatal("invalid frame mutated platform")
		}
		if _, err := m.PresentFrame(context.Background(), p.frames[0]); err != nil {
			t.Fatal(err)
		}
	})
	for _, stage := range []string{"copy", "publish", "start", "submit", "stop", "snapshot", "owner-after-copy", "owner-after-complete"} {
		t.Run(stage+"_uncertainty", func(t *testing.T) {
			p := newMenuProbe(t)
			m := p.open(t)
			if stage == "submit" || stage == "stop" {
				if _, err := m.PresentFrame(context.Background(), p.frames[0]); err != nil {
					t.Fatal(err)
				}
			}
			switch stage {
			case "copy":
				p.onCopy = func() error { return errMenuInjected }
			case "publish":
				p.onPublish = func() error { return errMenuInjected }
			case "owner-after-copy":
				p.onCopy = func() error { p.owned = false; return nil }
			case "start", "submit", "stop":
				command := uint32(1)
				if stage == "submit" {
					command = 2
				}
				if stage == "stop" {
					command = 3
				}
				p.onWrite = func(o, v uint32) error {
					if o == menuCommand && v == command {
						return errMenuInjected
					}
					return nil
				}
			case "snapshot":
				p.onRead = func(o, v uint32) (uint32, error) {
					if o == menuResultLo && p.commands[1] != 0 {
						return 0, errMenuInjected
					}
					return v, nil
				}
			case "owner-after-complete":
				p.onRead = func(o, v uint32) (uint32, error) {
					if o == 0xa4 && v == 1 {
						p.owned = false
					}
					return v, nil
				}
			}
			var err error
			if stage == "stop" {
				s, e := m.Stop(context.Background(), 1)
				err = e
				if s != (ReaderDrain{}) {
					t.Fatal("failed stop returned drain")
				}
			} else {
				s, e := m.PresentFrame(context.Background(), p.frames[1])
				err = e
				if s != (MenuFrame{}) {
					t.Fatal("failed frame returned success", s)
				}
			}
			if !errors.Is(err, errMenuInjected) {
				t.Fatal("wrong platform failure", err)
			}
			var failure *MenuFailure
			if !errors.As(err, &failure) {
				t.Fatal("missing cached failure")
			}
			before := p.calls
			_ = failure.Error()
			if p.calls != before {
				t.Fatal("failure inspection accessed hardware")
			}
			p.retired(t, m)
		})
	}
	for _, mode := range []uint32{1, 2, 3} {
		t.Run(fmt.Sprintf("memory_failure_%d", mode), func(t *testing.T) {
			p := newMenuProbe(t)
			m := p.open(t)
			p.must(t, "mode %x", mode)
			s, err := m.PresentFrame(context.Background(), p.frames[0])
			if err == nil || s != (MenuFrame{}) {
				t.Fatal("memory failure acknowledged", s, err)
			}
			if p.commands[1] != 1 {
				t.Fatal("start replay", p.commands)
			}
			p.retired(t, m)
		})
	}
	t.Run("held_address_blocks_stop", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		if _, err := m.PresentFrame(context.Background(), p.frames[0]); err != nil {
			t.Fatal(err)
		}
		p.must(t, "mode 1")
		held := false
		for i := 0; i < 64; i++ {
			p.must(t, "tick 1000")
			stats := p.must(t, "stats")
			if stats[8] != 0 && stats[9] == 0 {
				held = true
				break
			}
		}
		if !held {
			t.Fatal("negative control never offered a stalled AXI address")
		}
		s, err := m.Stop(context.Background(), 1)
		if !errors.Is(err, ErrReaderLimit) || s != (ReaderDrain{}) {
			t.Fatal("held read forged drain", s, err)
		}
		if p.commands[3] != 1 {
			t.Fatal("stop replay")
		}
		p.retired(t, m)
	})
	t.Run("concurrent_present_and_stop_reject_before_IO", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		entered, release := make(chan struct{}), make(chan struct{})
		first := true
		p.onCopy = func() error {
			if first {
				first = false
				close(entered)
				<-release
			}
			return nil
		}
		done := make(chan error, 1)
		go func() { _, err := m.PresentFrame(context.Background(), p.frames[0]); done <- err }()
		<-entered
		if _, err := m.PresentFrame(context.Background(), p.frames[1]); !errors.Is(err, ErrReaderBusy) {
			t.Fatal(err)
		}
		if _, err := m.Stop(context.Background(), 1); !errors.Is(err, ErrReaderBusy) {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cancel_after_publication_retires_without_start", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		ctx, cancel := context.WithCancel(context.Background())
		p.onPublish = func() error { cancel(); return nil }
		s, err := m.PresentFrame(ctx, p.frames[0])
		if !errors.Is(err, context.Canceled) || s != (MenuFrame{}) || p.commands[1] != 0 {
			t.Fatal(s, err, p.commands)
		}
		p.retired(t, m)
	})
	t.Run("bad_CSR_requests_do_not_start_or_submit", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		for _, request := range []string{"write 80 1 1", "write 81 1 f", "write 80 4 f", "write 90 2 f", "write 80 2 f", "write 80 3 f", "read d0"} {
			if _, err := p.call(request); err == nil {
				t.Fatal("bad CSR accepted", request)
			}
		}
		if stats := p.must(t, "stats"); stats[0] != 0 {
			t.Fatal("rejected CSR issued DDR", stats)
		}
		if _, err := m.PresentFrame(context.Background(), p.frames[0]); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("clock_regression_after_copy_retires", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		p.onCopy = func() error { p.now = p.now.Add(-time.Millisecond); return nil }
		s, err := m.PresentFrame(context.Background(), p.frames[0])
		if !errors.Is(err, ErrReaderLimit) || s != (MenuFrame{}) || p.commands[1] != 0 {
			t.Fatal(s, err)
		}
		p.retired(t, m)
	})
	t.Run("source_only_reset_cannot_be_adopted", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		if _, err := m.PresentFrame(context.Background(), p.frames[0]); err != nil {
			t.Fatal(err)
		}
		p.must(t, "reset-video")
		s, err := m.PresentFrame(context.Background(), p.frames[1])
		if err == nil || s != (MenuFrame{}) {
			t.Fatal("reset granted a frame", s, err)
		}
		p.retired(t, m)
	})
	t.Run("independent_C_firmware_rechecks_local_stop", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		if _, err := m.PresentFrame(context.Background(), p.frames[0]); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Stop(context.Background(), 7); err != nil {
			t.Fatal(err)
		}
		v := p.must(t, "fw-stop")
		if v[0] != 1 || v[1] != 0 || v[2] != 0 || v[3] == 0 || v[4] != 0 {
			t.Fatal("independent C readback did not verify the existing receipt", v)
		}
	})
	t.Run("independent_C_firmware_stops_active_reader_once", func(t *testing.T) {
		p := newMenuProbe(t)
		m := p.open(t)
		if _, err := m.PresentFrame(context.Background(), p.frames[0]); err != nil {
			t.Fatal(err)
		}
		v := p.must(t, "fw-stop")
		if v[0] != 1 || v[1] != 1 || v[4] != 3 {
			t.Fatal("C callback did not stop active reader once", v)
		}
		_, err := m.PresentFrame(context.Background(), p.frames[1])
		if err == nil {
			t.Fatal("reader remained usable after independent stop")
		}
		p.retired(t, m)
	})
	for _, command := range []string{"fw-layout 400000", "fw-owned 0"} {
		t.Run(strings.ReplaceAll(command, " ", "_"), func(t *testing.T) {
			p := newMenuProbe(t)
			p.open(t)
			p.must(t, command)
			if _, err := p.call("fw-stop"); err == nil {
				t.Fatal("foreign C scope accepted")
			}
			before := p.must(t, "fw-stats")
			if _, err := p.call("fw-stop"); err == nil {
				t.Fatal("C failure replayed")
			}
			after := p.must(t, "fw-stats")
			if before[2] != after[2] || before[3] != after[3] || after[0] != 0 {
				t.Fatal("retired C helper touched devices", before, after)
			}
		})
	}
	t.Run("snapshot_and_geometry_corruption_reject", func(t *testing.T) {
		for _, offset := range []uint32{8, 12, 0xb4, 0x94, 0x98, 0xa0, 0xac} {
			t.Run(fmt.Sprintf("offset_%x", offset), func(t *testing.T) {
				p := newMenuProbe(t)
				want := embeddedcontrol.Identity{ABI: MenuControlABI, Build: [4]uint32{1}}
				if err := (embeddedcontrol.Control{IO: p}).Enable(want, 1); err != nil {
					t.Fatal(err)
				}
				p.onRead = func(o, v uint32) (uint32, error) {
					if o == offset {
						return v ^ 1, nil
					}
					return v, nil
				}
				m, err := NewMenuPresenter(context.Background(), p.hooks(), want, 1, 0x95000000, p.limits())
				if err == nil {
					s, e := m.PresentFrame(context.Background(), p.frames[0])
					if e == nil || s != (MenuFrame{}) {
						t.Fatal("corrupt response accepted", s, e)
					}
					p.retired(t, m)
				}
				if p.commands[1] > 1 {
					t.Fatal("corrupt source replay")
				}
			})
		}
	})
}

func TestMenuLimitsRejectUnboundedPublication(t *testing.T) {
	limits := MenuLimits{ReaderLimits: ReaderLimits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: func() {}}, PublicationTimeout: 4 * time.Second}
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, duration := range []time.Duration{-1, 0, 4*time.Second + 1} {
		limits.PublicationTimeout = duration
		if err := limits.Validate(); !errors.Is(err, ErrReaderArgument) {
			t.Fatal(duration, err)
		}
	}
}

func testMenuQueuedCapacity(t *testing.T) {
	p := newMenuProbe(t)
	m := p.open(t)
	first, err := m.PresentFrame(context.Background(), p.frames[0])
	if err != nil {
		t.Fatal(first, err)
	}
	// More than one complete 720p frame passes without another submission.
	// CSR capacity must stay available through the video reader's fill phases.
	for n := 0; n < 32; n++ {
		values, err := p.call("read %x", menuStatus)
		if err != nil || len(values) != 1 {
			t.Fatal(values, err)
		}
		if uint32(values[0]) != menuRunning|menuSubmitReady {
			t.Fatalf("CSR capacity disappeared without a new request at step%d: flags=%x", n, values[0])
		}
		p.must(t, "tick 10000")
		p.now = p.now.Add(time.Duration(uint64(65536) * uint64(time.Second) / 74250000))
	}
	checked := false
	p.onWrite = func(offset, value uint32) error {
		if offset != menuCommand || value != 2 {
			return nil
		}
		// A second outstanding descriptor must reject even if software changes the
		// staging registers. The accepted descriptor must remain sequence2/slot1.
		p.must(t, "write 8c 3")
		p.must(t, "write 90 0")
		if _, err := p.call("write 80 2"); err == nil {
			t.Fatal("accepted a second outstanding frame")
		}
		p.must(t, "write 8c 2")
		p.must(t, "write 90 1")
		checked = true
		return nil
	}
	next, err := m.PresentFrame(context.Background(), p.frames[1])
	if err != nil || !checked || next.Generation != 1 || next.Sequence != 2 || next.Submitted != 2 || next.Slot != 1 || next.Underflows != 0 || p.commands[2] != 1 {
		t.Fatal("queued frame did not complete with one command", next, err, p.commands)
	}
	if _, err := m.Stop(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	t.Log("CSR capacity stays available through scanout phases; one descriptor accepted, second rejected, exact frame2/slot1 displayed and reader drained without retry")
}
