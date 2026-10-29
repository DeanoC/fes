//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package display

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math/bits"
	"runtime"
	"sync"
	"time"

	"github.com/usbarmory/tamago/dma"
)

const (
	fwBase      = uint64(0x09020000) // QEMU virt only.
	fwDirectory = uint16(0x19)
	dmaError    = uint32(1)
	dmaSelect   = uint32(8)
	dmaWrite    = uint32(16)
)

func read8(address uint64) byte
func write16(address uint64, value uint16)
func write64(address uint64, value uint64)
func barrier()

// RAMFB owns a persistent DMA allocation for QEMU's scanout. There is no Close:
// the buffer must remain allocated while QEMU may read it, through guest exit.
// Present copies a private software frame into this separate scanout buffer.
type RAMFB struct {
	mu                    sync.Mutex
	width, height, stride int
	buffer                []byte
}

// Open configures the QEMU ramfb device. The supplied region must be outside
// the Go runtime heap and mapped uncached by the QEMU board package. Caller
// retains that region for the entire boot. Run QEMU with -device ramfb.
func Open(width, height int, region *dma.Region) (*RAMFB, error) {
	if region == nil {
		return nil, errors.New("ramfb needs an owned DMA region")
	}
	if _, err := Config(1, width, height, width*4); err != nil {
		return nil, err
	}
	// The caller dedicates this region to display. Check enough contiguous
	// space for the frame and aligned temporary buffers before Reserve,
	// whose allocator reports exhaustion with a panic rather than an error.
	needed := uint(width*height*4 + ConfigBytes + 16 + 128)
	available := false
	for _, size := range region.FreeBlocks() {
		if size >= needed {
			available = true
			break
		}
	}
	if !available {
		return nil, errors.New("QEMU ramfb DMA region is too small or fragmented")
	}
	selectItem(0)
	var signature [4]byte
	readData(signature[:])
	if string(signature[:]) != "QEMU" {
		return nil, errors.New("QEMU fw_cfg signature is missing")
	}
	selectItem(1)
	var revision [4]byte
	readData(revision[:])
	if binary.LittleEndian.Uint32(revision[:])&2 == 0 {
		return nil, errors.New("QEMU fw_cfg DMA is unavailable")
	}
	selector, size, err := findFile("etc/ramfb")
	if err != nil {
		return nil, err
	}
	if size != ConfigBytes {
		return nil, fmt.Errorf("QEMU ramfb config is %d bytes, expected %d", size, ConfigBytes)
	}
	stride := width * 4
	address, buffer := region.Reserve(stride*height, 64)
	for i := range buffer {
		buffer[i] = 0
	}
	config, _ := Config(uint64(address), width, height, stride)
	configAddress, configDMA := region.Reserve(ConfigBytes, 16)
	for i, value := range config {
		configDMA[i] = value
	}
	descriptorAddress, descriptor := region.Reserve(16, 16)
	// Only aligned stores/byte writes go to uncached DMA memory.
	put32(descriptor[0:4], uint32(selector)<<16|dmaSelect|dmaWrite)
	put32(descriptor[4:8], ConfigBytes)
	put64(descriptor[8:16], uint64(configAddress))
	barrier()
	write64(fwBase+16, bits.ReverseBytes64(uint64(descriptorAddress)))
	deadline := time.Now().Add(time.Second)
	for polls := 0; ; polls++ {
		barrier()
		var control [4]byte
		for i := range control {
			control[i] = descriptor[i]
		}
		status := binary.BigEndian.Uint32(control[:])
		if status&dmaError != 0 {
			return nil, errors.New("QEMU rejected ramfb DMA configuration")
		}
		if status == 0 {
			break
		}
		if polls >= 1000000 || time.Now().After(deadline) {
			// No release: an uncertain DMA request may still own these buffers.
			return nil, errors.New("QEMU ramfb DMA configuration timed out")
		}
		runtime.Gosched()
	}
	region.Release(configAddress)
	region.Release(descriptorAddress)
	return &RAMFB{width: width, height: height, stride: stride, buffer: buffer}, nil
}

// Present publishes an RGBA software frame to QEMU's XRGB8888 scanout. The
// copy is not a hardware vblank fence or a DE25 DMA ownership protocol.
func (fb *RAMFB) Present(frame *image.RGBA) error {
	if fb == nil || frame == nil || frame.Rect.Dx() != fb.width || frame.Rect.Dy() != fb.height ||
		frame.Stride < fb.width*4 || len(frame.Pix) < (fb.height-1)*frame.Stride+fb.width*4 {
		return errors.New("frame does not match ramfb geometry")
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	for y := 0; y < fb.height; y++ {
		// Scanout uses uncached memory; bound each cooperative copy to four rows.
		if y&3 == 0 {
			runtime.Gosched()
		}
		source := frame.Pix[y*frame.Stride:]
		dest := fb.buffer[y*fb.stride:]
		for x := 0; x < fb.width*4; x += 4 {
			dest[x] = source[x+2]
			dest[x+1] = source[x+1]
			dest[x+2] = source[x]
			dest[x+3] = 0xff
		}
	}
	barrier()
	return nil
}

func selectItem(selector uint16) {
	write16(fwBase+8, bits.ReverseBytes16(selector))
	barrier()
}
func readData(target []byte) {
	for i := range target {
		target[i] = read8(fwBase)
	}
}

func findFile(name string) (uint16, uint32, error) {
	selectItem(fwDirectory)
	var countBytes [4]byte
	readData(countBytes[:])
	count := binary.BigEndian.Uint32(countBytes[:])
	if count > 4096 {
		return 0, 0, errors.New("QEMU fw_cfg file count is excessive")
	}
	for i := uint32(0); i < count; i++ {
		var entry [64]byte
		readData(entry[:])
		end := 8
		for end < len(entry) && entry[end] != 0 {
			end++
		}
		if string(entry[8:end]) == name {
			return binary.BigEndian.Uint16(entry[4:6]), binary.BigEndian.Uint32(entry[0:4]), nil
		}
	}
	return 0, 0, fmt.Errorf("QEMU fw_cfg file %q is unavailable", name)
}

func put32(target []byte, value uint32) {
	for i := 0; i < 4; i++ {
		target[i] = byte(value >> uint(24-i*8))
	}
}
func put64(target []byte, value uint64) {
	for i := 0; i < 8; i++ {
		target[i] = byte(value >> uint(56-i*8))
	}
}
