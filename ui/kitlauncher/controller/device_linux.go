//go:build linux

package controller

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/inputmap"
	"golang.org/x/sys/unix"
	"unsafe"
)

// Device reads one physical evdev interface. Hub opens every eligible pad;
// discovery never opens js duplicates or the retained virtual pad.
type Device struct {
	fd      int
	mapper  *Mapper
	pending []byte
	Name    string
	Path    string
	remap   *inputmap.Remapper
}

func ioctl(fd int, nr byte, b []byte) error {
	if len(b) == 0 {
		return errors.New("empty ioctl")
	}
	request := uintptr(2<<30) | uintptr(len(b))<<16 | uintptr('E')<<8 | uintptr(nr)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&b[0])))
	if errno != 0 {
		return errno
	}
	return nil
}
func bit(b []byte, n int) bool { return n/8 < len(b) && b[n/8]&(1<<uint(n%8)) != 0 }
func openDevice(path string) (*Device, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = unix.Close(fd)
		}
	}()
	id := make([]byte, 8)
	name := make([]byte, 256)
	keys := make([]byte, 96)
	for _, q := range []struct {
		nr byte
		b  []byte
	}{{2, id}, {6, name}, {0x21, keys}} {
		if err := ioctl(fd, q.nr, q.b); err != nil {
			return nil, err
		}
	}
	bus, vendor, product := binary.LittleEndian.Uint16(id), binary.LittleEndian.Uint16(id[2:]), binary.LittleEndian.Uint16(id[4:])
	fixture := vendor == 0x081f && product == 0xe401
	isPad := bit(keys, 304) || (fixture && bit(keys, 288))
	isKey := !isPad && bit(keys, 30)
	if !eligible(bus, cstring(name), isPad) && !eligibleKeyboard(bus, cstring(name), isKey) {
		return nil, errors.New("not a physical gamepad")
	}
	var mapper *Mapper
	if isKey && !isPad {
		mapper = NewKeyboardMapper()
	} else {
		axes := map[uint16]Range{}
		for _, code := range []uint16{0, 1, 16, 17} {
			info := make([]byte, 24)
			if ioctl(fd, byte(0x40+code), info) == nil {
				axes[code] = Range{int32(binary.LittleEndian.Uint32(info[4:])), int32(binary.LittleEndian.Uint32(info[8:]))}
			}
		}
		mapper = NewMapper(vendor, product, axes)
	}
	held := make([]byte, 96)
	if err := ioctl(fd, 0x18, held); err != nil {
		return nil, err
	}
	for code := 0; code < len(held)*8; code++ {
		if bit(held, code) {
			mapper.Suppress(uint16(code))
		}
	}
	success = true
	return &Device{fd: fd, mapper: mapper, Name: cstring(name), Path: path}, nil
}

func (d *Device) Info() (string, string) {
	if d == nil {
		return "", ""
	}
	return d.Path, d.Name
}

func (d *Device) mapEvent(typ, code uint16, value int32) (remoteinput.Event, bool) {
	return d.mapper.mapWith(d.remap, typ, code, value)
}
func (d *Device) Close() error {
	if d.fd < 0 {
		return nil
	}
	fd := d.fd
	d.fd = -1
	return unix.Close(fd)
}
func (d *Device) Poll() ([]remoteinput.Event, error) {
	if d.fd < 0 {
		return nil, errors.New("controller closed")
	}
	size := int(unsafe.Sizeof(unix.Timeval{})) + 8
	buf := make([]byte, size*32)
	var out []remoteinput.Event
	// Bound work per tick so a noisy device cannot starve network/Stop handling.
	for reads := 0; reads < 8; reads++ {
		n, err := unix.Read(d.fd, buf)
		if err == unix.EAGAIN {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, errors.New("controller disconnected")
		}
		d.pending = append(d.pending, buf[:n]...)
		for len(d.pending) >= size {
			typ, code, value, err := decode(d.pending[:size])
			d.pending = d.pending[size:]
			if err != nil {
				return nil, err
			}
			if typ == 0 && code == 3 {
				return nil, fmt.Errorf("controller events lost; reconnecting")
			}
			if e, ok := d.mapEvent(typ, code, value); ok {
				out = append(out, e)
			}
		}
	}
	return out, nil
}
