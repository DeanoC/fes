// pc386-diag runs a pc386 M4 diagnostic program on a kit (docs/pc386-machine-design.md,
// phase M4). The fes.pc386-diag package must be running: its bus master
// takes commands from a ring in the HPS DDR window, which this tool reaches
// through /dev/mem. It writes the results file scripts/pc386_diag.py checks;
// cores/fes-pc386/sim/diag_tb.cpp is the same host loop in simulation.
//
//	pc386-diag run PROGRAM RESULTS
//	pc386-diag status
//
// It never loads a core and never touches the GP mailbox, which belongs to
// the runtime. Build for the kit with GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	windowBase    = 0x30000000
	windowBytes   = 0x10000000
	doorbell      = 0x01000000
	status        = 0x01000010
	commands      = 0x01010000
	results       = 0x01020000
	magic         = 0x33384d34
	ring          = 4096
	captureOffset = 0x00a00000
	captureBytes  = 0x00200000
	captureStride = 3200
	stallLimit    = 20 * time.Second
)

type window struct{ mem []byte }

func openWindow() (*window, error) {
	f, err := os.OpenFile("/dev/mem", os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	mem, err := syscall.Mmap(int(f.Fd()), windowBase, windowBytes, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mapping the DDR window: %w", err)
	}
	return &window{mem}, nil
}

// Every access is one aligned 32-bit load or store with a barrier: the
// mapping is uncached and the FPGA reads the same bytes on its own port.
func (w *window) word(offset uint32) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&w.mem[offset])))
}

func (w *window) store(offset, value uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&w.mem[offset])), value)
}

// Bulk copies use plain word stores and loads, then one barrier.
func (w *window) put(offset uint32, data []byte) {
	for n := 0; n+4 <= len(data); n += 4 {
		*(*uint32)(unsafe.Pointer(&w.mem[offset+uint32(n)])) = binary.LittleEndian.Uint32(data[n:])
	}
	w.store(doorbell+12, 0) // unused doorbell word: the store's barriers order the copy
}

func (w *window) get(offset, length uint32, out []byte) []byte {
	for n := uint32(0); n < length; n += 4 {
		out = binary.LittleEndian.AppendUint32(out, *(*uint32)(unsafe.Pointer(&w.mem[offset+n])))
	}
	return out
}

type runner struct {
	w                    *window
	program              []byte
	at                   int
	out                  []byte
	done                 [][4]uint32
	epoch                uint32
	submitted, completed uint32
	start                time.Time
	progress             time.Time
}

func (r *runner) u32(at int) uint32 {
	if at+4 > len(r.program) {
		panic("program file ends early")
	}
	return binary.LittleEndian.Uint32(r.program[at:])
}

func (r *runner) put32(v uint32) { r.out = binary.LittleEndian.AppendUint32(r.out, v) }
func (r *runner) put64(v uint64) { r.out = binary.LittleEndian.AppendUint64(r.out, v) }
func (r *runner) since() uint64  { return uint64(time.Since(r.start).Nanoseconds()) }

// attach waits for the master's status, then for it to finish every command
// the doorbell holds for its epoch: a run that was interrupted leaves its
// commands numbered as this run's would be, and the master would take them
// as this run's. A doorbell of another epoch is one the master ignores.
func (r *runner) attach() error {
	deadline := time.Now().Add(5 * time.Second)
	for r.w.word(status+4) != magic {
		if time.Now().After(deadline) {
			return errors.New("no pc386 bus master status in the window: is fes.pc386-diag running?")
		}
		time.Sleep(time.Millisecond)
	}
	head, progress := r.w.word(status), time.Now()
	for r.w.word(doorbell+4) == r.w.word(status+8) && r.w.word(status) != r.w.word(doorbell) {
		if now := r.w.word(status); now != head {
			head, progress = now, time.Now()
		} else if time.Since(progress) > stallLimit {
			return fmt.Errorf("an earlier run's commands are not finishing (head %d, tail %d)",
				head, r.w.word(doorbell))
		}
		time.Sleep(time.Millisecond)
	}
	r.epoch = r.w.word(status + 8)
	r.submitted = r.w.word(status)
	r.completed = r.submitted
	r.w.store(doorbell, r.submitted)
	r.w.store(doorbell+4, r.epoch)
	return nil
}

func (r *runner) collect() error {
	if time.Since(r.progress) > stallLimit {
		return fmt.Errorf("no command completed for %v (%d of %d done)", stallLimit, r.completed, r.submitted)
	}
	if epoch := r.w.word(status + 8); epoch != r.epoch {
		return fmt.Errorf("the bus master restarted (epoch %08x, attached to %08x)", epoch, r.epoch)
	}
	head := r.w.word(status)
	for r.completed != head {
		base := results + 16*(r.completed%ring)
		result := [4]uint32{r.w.word(base), r.w.word(base + 4), r.w.word(base + 8), r.w.word(base + 12)}
		if result[3] != r.epoch+r.completed {
			// The beat may not have landed yet; the status write follows it.
			return nil
		}
		r.out = append(r.out, 'R')
		for _, v := range result {
			r.put32(v)
		}
		r.done = append(r.done, result)
		r.completed++
		r.progress = time.Now()
	}
	return nil
}

func (r *runner) drain() error {
	for r.completed != r.submitted {
		if err := r.collect(); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) run(holdMarks bool) error {
	if string(r.program[:8]) != "P386PRG1" {
		return errors.New("not a pc386 diagnostic program")
	}
	r.at = 12
	r.out = append(r.out, "P386RES1"...)
	if err := r.attach(); err != nil {
		return err
	}
	r.start = time.Now()
	r.progress = r.start
	stdout := bufio.NewWriter(os.Stdout)
	defer stdout.Flush()
	for {
		kind := r.program[r.at]
		if kind == 'C' {
			rang := false
			for r.program[r.at] == 'C' && r.submitted-r.completed < ring-16 {
				slot := commands + 16*(r.submitted%ring)
				r.w.store(slot, r.u32(r.at+1))
				r.w.store(slot+4, r.u32(r.at+5))
				r.w.store(slot+8, r.u32(r.at+9))
				r.w.store(slot+12, r.epoch+r.submitted)
				r.submitted++
				r.at += 21
				rang = true
			}
			if rang {
				r.w.store(doorbell, r.submitted)
			}
			if err := r.collect(); err != nil {
				return err
			}
			continue
		}
		if err := r.drain(); err != nil {
			return err
		}
		switch kind {
		case 'S':
			offset, length := r.u32(r.at+1), r.u32(r.at+5)
			began := time.Now()
			r.w.put(offset, r.program[r.at+9:r.at+9+int(length)])
			r.out = append(r.out, 'S')
			r.put32(offset)
			r.put32(length)
			r.put64(uint64(time.Since(began).Nanoseconds()))
			r.at += 9 + int(length)
		case 'M':
			length := int(r.program[r.at+1])
			name := string(r.program[r.at+2 : r.at+2+length])
			hold := r.u32(r.at + 2 + length)
			fmt.Fprintf(stdout, "MARK %s %d\n", name, r.since())
			stdout.Flush()
			r.out = append(r.out, 'M', byte(length))
			r.out = append(r.out, name...)
			r.put64(r.since())
			r.put32(r.completed)
			if holdMarks && hold > 0 {
				time.Sleep(time.Duration(hold) * time.Millisecond)
			}
			r.progress = time.Now()
			r.at += 2 + length + 4
		case 'D':
			offset, length := r.u32(r.at+1), r.u32(r.at+5)
			r.out = append(r.out, 'D')
			r.put32(offset)
			r.put32(length)
			r.out = r.w.get(offset, length, r.out)
			r.at += 9
		case 'F':
			if len(r.done) < 2 {
				return errors.New("a frame record needs READ 6 and READ 7 before it")
			}
			size, frame := r.done[len(r.done)-2][0], r.done[len(r.done)-1][0]
			width, height, buffer := size&0x3ff, size>>16&0x3ff, frame>>16&3
			r.out = append(r.out, 'F')
			r.put32(width)
			r.put32(height)
			r.put32(buffer)
			r.put32(frame & 0xffff)
			base := captureOffset + buffer*captureBytes
			for y := uint32(0); y < height; y++ {
				r.out = r.w.get(base+y*captureStride, 4*width, r.out)
			}
			r.at++
		case 'E':
			r.out = append(r.out, 'E')
			r.put64(r.since())
			fmt.Fprintf(stdout, "DONE %d commands %d ns\n", r.completed, r.since())
			return nil
		default:
			return fmt.Errorf("bad program record %q at %d", kind, r.at)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pc386-diag run PROGRAM RESULTS | pc386-diag status")
		os.Exit(2)
	}
	w, err := openWindow()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pc386-diag:", err)
		os.Exit(1)
	}
	switch os.Args[1] {
	case "status":
		fmt.Printf("status head %d magic %08x epoch %08x clock %08x; doorbell tail %d epoch %08x\n",
			w.word(status), w.word(status+4), w.word(status+8), w.word(status+12), w.word(doorbell), w.word(doorbell+4))
	case "run":
		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, "usage: pc386-diag run PROGRAM RESULTS")
			os.Exit(2)
		}
		program, err := os.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "pc386-diag:", err)
			os.Exit(1)
		}
		r := &runner{w: w, program: program}
		runErr := r.run(true)
		if runErr != nil {
			fmt.Fprintln(os.Stderr, "pc386-diag:", runErr)
		}
		if err := os.WriteFile(os.Args[3], r.out, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "pc386-diag:", err)
			os.Exit(1)
		}
		if runErr != nil {
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "pc386-diag: unknown command", os.Args[1])
		os.Exit(2)
	}
}
