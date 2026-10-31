// pc386-host is the HPS side of pc386_host_link on a kit (docs/pc386-machine-design.md,
// phase M5). The fes.pc386 package must be running: until the fes.computer 1.1
// contracts land, its machine waits in reset for the HPS to stage the ATA image
// and a configuration block in the DDR window, which this tool reaches through
// /dev/mem. cores/fes-pc386/sim/system_tb.cpp is the same host in simulation,
// and prints the same lines.
//
//	pc386-host boot DISK [SECONDS]   stage DISK, start the machine, follow it to the prompt
//	pc386-host status                print the status block and the text screen
//
// boot prints ATTACH, CONFIG, each new POST code, each debug-port line, PROMPT,
// then the screen, the status block and END <reason>: prompt, stopped (the CPU
// stopped for good), stalled (nothing retired for 2 s) or limit. It exits 0 at
// the prompt. It never loads a core and never touches the GP mailbox, which
// belongs to the runtime. Build for the kit with
// GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	windowBase   = 0x30000000
	windowBytes  = 0x10000000
	config       = 0x01000000
	status       = 0x01000040
	ring         = 0x01001000
	ringBytes    = 4096
	image        = 0x08000000
	imageBytes   = 128 << 20
	vram         = 0x00800000
	configMagic  = 0x43363833 // "386C"
	statusMagic  = 0x53363833 // "386S"
	prompt       = `C:\>`
	stallLimit   = 2 * time.Second
	screenPeriod = 50 * time.Millisecond
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

// A 64-bit word, low half first. Its high half is stored first, so the
// link, which reads the whole word, never sees a new low half beside an old
// high half.
func (w *window) word64(offset uint32) uint64 {
	return uint64(w.word(offset)) | uint64(w.word(offset+4))<<32
}

func (w *window) store64(offset uint32, value uint64) {
	w.store(offset+4, uint32(value>>32))
	w.store(offset, uint32(value))
}

// Bulk copies use plain word stores, then one barrier.
func (w *window) put(offset uint32, data []byte) {
	for n := 0; n+4 <= len(data); n += 4 {
		*(*uint32)(unsafe.Pointer(&w.mem[offset+uint32(n)])) = binary.LittleEndian.Uint32(data[n:])
	}
	w.store(config+60, 0) // an unused configuration word: the store's barrier orders the copy
}

func (w *window) byte(offset uint32) byte { return byte(w.word(offset&^3) >> (8 * (offset & 3))) }

// Text mode: character n at planar offset 2n in plane 0; VRAM word w holds
// planes 0-3 at window offset 0x800000 + 4w.
func (w *window) screenRow(row int) string {
	var text strings.Builder
	for column := 0; column < 80; column++ {
		c := w.byte(vram + 4*uint32(2*(row*80+column)))
		if c < 0x20 || c >= 0x7f {
			c = ' '
		}
		text.WriteByte(c)
	}
	return text.String()
}

func (w *window) lastRow() string {
	for row := 24; row >= 0; row-- {
		if text := strings.TrimRight(w.screenRow(row), " "); text != "" {
			return text
		}
	}
	return ""
}

// The geometry system_tb and machine_tb infer: the first partition's end
// head and sector, else 16 heads of 63 sectors.
func geometry(disk []byte) (cylinders uint64, heads, spt uint64) {
	heads, spt = 16, 63
	if len(disk) >= 512 && disk[510] == 0x55 && disk[511] == 0xaa && disk[0x1be+4] != 0 {
		if end := uint64(disk[0x1be+6] & 0x3f); end != 0 {
			heads, spt = uint64(disk[0x1be+5])+1, end
		}
	}
	cylinders = min(uint64(len(disk))/512/(heads*spt), 16383)
	return
}

func printStatus(w *window) {
	s1, s2, s3 := w.word64(status+8), w.word64(status+16), w.word64(status+24)
	low := func(n uint32) uint64 { return w.word64(status+8*n) & 0xffffffff }
	high := func(n uint32) uint64 { return w.word64(status+8*n) >> 32 }
	fmt.Printf("STATUS running=%d debug_bytes=%d debug_drops=%d halted=%d stopped=%d a20m=%d intr=%d post=%02x "+
		"cpu_resets=%d retired=%d cs=%04x vector=%02x eip=%08x frames_shown=%d scanout_underflows=%d "+
		"capture_drops=%d present_underflows=%d plane_underflows=%d samples=%d dma_cycles=%d opl2_samples=%d\n",
		s1&1, s1>>32, s1>>16&0xffff, s2&1, s2>>1&1, s2>>2&1, s2>>3&1, s2>>8&0xff, s2>>16&0xff, s2>>32,
		s3&0xffff, s3>>24&0xff, s3>>32, low(4), high(4), low(5), high(5), low(6), high(6), low(7), high(7))
}

func boot(w *window, disk []byte, limit time.Duration) (string, error) {
	if len(disk)%512 != 0 || len(disk) > imageBytes {
		return "", errors.New("the disk image must be whole sectors and at most 128 MiB")
	}
	start := time.Now()
	deadline := start.Add(5 * time.Second)
	for uint32(w.word64(status)) != statusMagic {
		if time.Now().After(deadline) {
			return "", errors.New("no pc386 host link status in the window: is fes.pc386 running?")
		}
		time.Sleep(time.Millisecond)
	}
	if w.word64(status+8)&1 != 0 {
		return "", errors.New("the machine is already running: relaunch fes.pc386 to stage a disk")
	}
	epoch := w.word64(status) >> 32
	fmt.Printf("ATTACH %d %08x\n", time.Since(start).Microseconds(), epoch)
	// The image, then the configuration, word 0 last.
	w.put(image, disk)
	cylinders, heads, spt := geometry(disk)
	now := time.Now()
	w.store64(config+8, 640|15360<<16)
	w.store64(config+16, 0x22|7<<8|1<<16|1<<32|1<<34)
	w.store64(config+24, uint64(len(disk)/512)|cylinders<<32|heads<<48|spt<<56)
	w.store64(config+32, uint64(now.Year())|uint64(now.Month())<<16|uint64(now.Day())<<24|uint64(now.Hour())<<32|
		uint64(now.Minute())<<40|uint64(now.Second())<<48|uint64(now.Weekday()+1)<<56)
	w.store64(config, epoch<<32|configMagic)
	fmt.Printf("CONFIG %d\n", time.Since(start).Microseconds())

	lastPost, retired := -1, uint64(0)
	var debugRead uint32
	var line strings.Builder
	progress, lastScreen := time.Now(), time.Now()
	promptSeen := false
	for {
		if time.Since(start) > limit {
			return "limit", nil
		}
		time.Sleep(time.Millisecond)
		s1, s2 := w.word64(status+8), w.word64(status+16)
		if s1&1 == 0 {
			continue // the machine is not running yet
		}
		if post := int(s2 >> 8 & 0xff); post != lastPost {
			fmt.Printf("POST %02x %d\n", post, time.Since(start).Microseconds())
			lastPost = post
		}
		written := uint32(s1 >> 32)
		if written-debugRead > ringBytes {
			return "", errors.New("the debug ring overran the host")
		}
		for ; debugRead != written; debugRead++ {
			switch c := w.byte(ring + debugRead%ringBytes); c {
			case '\n':
				fmt.Println("DEBUG", line.String())
				line.Reset()
			case '\r':
			default:
				line.WriteByte(c)
			}
		}
		if s2>>1&1 != 0 {
			return "stopped", nil
		}
		if s2>>32 != retired {
			retired, progress = s2>>32, time.Now()
		} else if time.Since(progress) > stallLimit {
			return "stalled", nil
		}
		if time.Since(lastScreen) >= screenPeriod {
			lastScreen = time.Now()
			shown := w.lastRow() == prompt
			if shown && promptSeen {
				return "prompt", nil
			} else if shown {
				fmt.Printf("PROMPT %d\n", time.Since(start).Microseconds())
			}
			promptSeen = shown
		}
	}
}

func finish(w *window) {
	for row := 0; row < 25; row++ {
		fmt.Printf("SCREEN %d |%s|\n", row, w.screenRow(row))
	}
	printStatus(w)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pc386-host boot DISK [SECONDS] | pc386-host status")
		os.Exit(2)
	}
	w, err := openWindow()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pc386-host:", err)
		os.Exit(1)
	}
	switch os.Args[1] {
	case "status":
		finish(w)
	case "boot":
		if len(os.Args) < 3 || len(os.Args) > 4 {
			fmt.Fprintln(os.Stderr, "usage: pc386-host boot DISK [SECONDS]")
			os.Exit(2)
		}
		seconds := 60.0
		if len(os.Args) == 4 {
			if seconds, err = strconv.ParseFloat(os.Args[3], 64); err != nil || seconds <= 0 {
				fmt.Fprintln(os.Stderr, "pc386-host: SECONDS must be a positive number")
				os.Exit(2)
			}
		}
		disk, err := os.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "pc386-host:", err)
			os.Exit(1)
		}
		start := time.Now()
		reason, err := boot(w, disk, time.Duration(seconds*float64(time.Second)))
		if err != nil {
			fmt.Fprintln(os.Stderr, "pc386-host:", err)
			os.Exit(1)
		}
		finish(w)
		fmt.Printf("END %s seconds=%.3f\n", reason, time.Since(start).Seconds())
		if reason != "prompt" {
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "pc386-host: unknown command", os.Args[1])
		os.Exit(2)
	}
}
