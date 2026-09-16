//go:build linux

package bootlinux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	socfpgaSystemManager  = 0xffd08000
	socfpgaPreloaderState = 0xc8
	socfpgaWarmRAMEnable  = 0xe0
	socfpgaPreloaderValid = 0x49535756
	socfpgaWarmRAMMagic   = 0xae9efebc
)

// PrepareSoCFPGAWatchdogReset makes subsequent watchdog warm resets reload SPL
// from the SD card. It must succeed before opening the watchdog on the supported
// DE10-nano. It neither opens the watchdog nor requests a reset.
//
// Locked MiSTer U-Boot enables retained OCRAM boot (0xAE9EFEBC at SYSMGR+0xE0),
// which did not recover in physical watchdog tests. Zero disables that path.
// Reaching this bootstrap proves the current preloader completed; marking it
// valid prevents Boot ROM advancing through its four SPL copies on each reset.
// The register values are defined in Intel SoCAL alt_sysmgr.h: INITSWSTATE and
// ROMCODE_WARMRAM_EN. These are preloader state, not appliance trial confirmation.
func PrepareSoCFPGAWatchdogReset() (err error) {
	model, err := os.ReadFile("/sys/firmware/devicetree/base/model")
	if err != nil {
		return fmt.Errorf("read reset board model: %w", err)
	}
	compatible, err := os.ReadFile("/sys/firmware/devicetree/base/compatible")
	if err != nil {
		return fmt.Errorf("read reset board compatible: %w", err)
	}
	if err = validateSoCFPGABoard(runtime.GOARCH, model, compatible); err != nil {
		return err
	}
	fd, err := unix.Open("/dev/mem", unix.O_RDWR|unix.O_SYNC|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open reset registers: %w", err)
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(uint64(st.Rdev)) != 1 || unix.Minor(uint64(st.Rdev)) != 1 {
		return errors.New("reset registers require /dev/mem character device 1:1")
	}
	// An MMIO bus fault must fail guard startup rather than allow arming. This
	// setting affects only this goroutine and is restored before returning.
	previous := debug.SetPanicOnFault(true)
	defer debug.SetPanicOnFault(previous)
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("reset register access fault: %v", fault)
		}
	}()
	registers, err := mapSoCFPGARegisters(fd, socfpgaSystemManager)
	if err != nil {
		return fmt.Errorf("map reset registers: %w", err)
	}
	defer func() { err = errors.Join(err, registers.Close()) }()
	return configureSoCFPGAReset(registers)
}

func validateSoCFPGABoard(arch string, model, compatible []byte) error {
	if arch != "arm" || !bytes.Equal(model, []byte("Terasic DE10-nano\x00")) ||
		!bytes.Equal(compatible, []byte("altr,socfpga-cyclone5\x00altr,socfpga\x00")) {
		return errors.New("watchdog reset preparation requires ARM Terasic DE10-nano Cyclone V")
	}
	return nil
}

type resetRegisters interface {
	Read32(int) (uint32, error)
	Write32(int, uint32) error
}

func configureSoCFPGAReset(registers resetRegisters) error {
	warm, err := registers.Read32(socfpgaWarmRAMEnable)
	if err != nil {
		return fmt.Errorf("read warm boot policy: %w", err)
	}
	if warm != 0 && warm != socfpgaWarmRAMMagic {
		return fmt.Errorf("unexpected warm boot policy %#x", warm)
	}
	if _, err = registers.Read32(socfpgaPreloaderState); err != nil {
		return fmt.Errorf("read preloader state: %w", err)
	}
	for _, word := range []struct {
		offset int
		value  uint32
	}{
		{socfpgaPreloaderState, socfpgaPreloaderValid},
		{socfpgaWarmRAMEnable, 0},
	} {
		if err = registers.Write32(word.offset, word.value); err != nil {
			return fmt.Errorf("write reset register %#x: %w", word.offset, err)
		}
		actual, err := registers.Read32(word.offset)
		if err != nil {
			return fmt.Errorf("read back reset register %#x: %w", word.offset, err)
		}
		if actual != word.value {
			return fmt.Errorf("reset register %#x readback %#x, expected %#x", word.offset, actual, word.value)
		}
	}
	return nil
}

type mappedResetRegisters struct{ page []byte }

func mapSoCFPGARegisters(fd int, offset int64) (*mappedResetRegisters, error) {
	// The pinned ARM kernel's phys_mem_access_prot maps this non-RAM /dev/mem
	// page pgprot_noncached (strongly ordered). Do not substitute a cached RAM
	// mapping: atomic barriers alone do not establish device memory attributes.
	page, err := unix.Mmap(fd, offset, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	return &mappedResetRegisters{page}, nil
}
func (r *mappedResetRegisters) Read32(offset int) (uint32, error) {
	// On the selected ARM toolchain this is a word load followed by DMB ISH;
	// writes use DMB ISH / word store / DMB ISH. configure also reads back each
	// write on the strongly ordered device mapping before allowing arming.
	value := atomic.LoadUint32((*uint32)(unsafe.Pointer(&r.page[offset])))
	runtime.KeepAlive(r)
	return value, nil
}
func (r *mappedResetRegisters) Write32(offset int, value uint32) error {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&r.page[offset])), value)
	runtime.KeepAlive(r)
	return nil
}
func (r *mappedResetRegisters) Close() error { return unix.Munmap(r.page) }
