//go:build linux

package tenfoot

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// These Linux architectures use the generic ioctl encoding and little-endian
// bitmaps expected below. Other Linux ABIs remain unavailable until their
// ioctl encoding and kernel bitmap word order are implemented and tested.
func nativeKeyboardBitmapArchitectureSupported(arch string) bool {
	switch arch {
	case "386", "amd64", "arm", "arm64":
		return true
	default:
		return false
	}
}

const (
	// KEY_MAX is 0x2ff; include every key bit, not only supported usages.
	nativeKeyBitmapBytes = 96
	// poll reads at most eight batches of 32 events at a time.
	maxNativeSnapshotEvents = 8 * 32
)

// nativeKeyboardBitmap performs one bounded query on an already-open fd.
// Capability and held-state reads must be made by its sole event-loop owner.
// The injected syscall permits pure tests; no function here opens a device.
func nativeKeyboardBitmap(fd int, held bool, ioctl func(int, uintptr, []byte) (int, error)) ([]byte, error) {
	if !nativeKeyboardBitmapArchitectureSupported(runtime.GOARCH) || fd < 0 || ioctl == nil {
		return nil, errors.New("invalid native keyboard fd")
	}
	requestNumber := uintptr(0x21) // EVIOCGBIT(EV_KEY)
	if held {
		requestNumber = 0x18
	} // EVIOCGKEY
	request := uintptr(0x80000000|(nativeKeyBitmapBytes<<16)|('E'<<8)) | requestNumber
	bitmap := make([]byte, nativeKeyBitmapBytes)
	n, err := ioctl(fd, request, bitmap)
	if err != nil {
		return nil, err
	}
	if n != len(bitmap) {
		return nil, errors.New("incomplete native keyboard bitmap")
	}
	return bitmap, nil
}

func nativeKeyboardBitmapIOCTL(fd int, request uintptr, bitmap []byte) (int, error) {
	if !nativeKeyboardBitmapArchitectureSupported(runtime.GOARCH) || fd < 0 || len(bitmap) != nativeKeyBitmapBytes {
		return 0, errors.New("invalid native keyboard ioctl")
	}
	n, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&bitmap[0])))
	if err != 0 {
		return 0, err
	}
	return int(n), nil
}

// snapshotNativeKeyboard establishes a per-fd snapshot boundary while the
// caller has confirmed keyboard classification, fenced both app and UI
// dispatch, and exclusively owns the fd and pending bytes in its event loop.
// ioctl must issue EVIOCGKEY on that same fd: the kernel returns its current
// bitmap and flushes queued EV_KEY records belonging to that client. Already
// read EV_KEY records must also be discarded, since they precede the ioctl.
// The snapshot contains raw Linux codes to reconcile and suppress, not
// presses to deliver. This helper does not perform that reconciliation.
// This flush includes button events on hybrid devices. A complete capability
// bitmap for this same fd must show only non-reserved EV_KEY codes below 256.
// Reserved or non-keyboard capabilities fail before ioctl, without flushing anything.
// The caller must separately verify exclusive ownership of the fd and tie the
// capability proof to this exact device incarnation.
//
// SYN_DROPPED must be handled through the following SYN_REPORT before calling
// this helper. On any error the caller must keep the source quarantined.
func snapshotNativeKeyboard(pending, capabilities []byte, ioctl func() ([]byte, error)) (held []uint16, reconciled []byte, err error) {
	if ioctl == nil || len(pending)%nativeEventSize != 0 || len(pending) > maxNativeSnapshotEvents*nativeEventSize {
		return nil, nil, errors.New("invalid native keyboard snapshot input")
	}
	if len(capabilities) != nativeKeyBitmapBytes {
		return nil, nil, errors.New("missing native keyboard capability proof")
	}
	hasKey := false
	for code := 0; code < len(capabilities)*8; code++ {
		if capabilities[code/8]&(1<<uint(code%8)) == 0 {
			continue
		}
		if code == 0 || code >= 256 {
			return nil, nil, errors.New("native input has unknown or non-keyboard capability")
		}
		hasKey = true
	}
	if !hasKey {
		return nil, nil, errors.New("native keyboard capability proof is empty")
	}
	for off := 0; off < len(pending); off += nativeEventSize {
		typ, code, _ := decodeNativeEvent(pending[off : off+nativeEventSize])
		if typ == 0 && code == 3 { // EV_SYN / SYN_DROPPED
			return nil, nil, errors.New("native input requires dropped-event recovery")
		}
		if typ == 1 && (int(code) >= len(capabilities)*8 || capabilities[code/8]&(1<<uint(code%8)) == 0) {
			return nil, nil, errors.New("native input event exceeds capability proof")
		}
	}
	bitmap, err := ioctl()
	if err != nil {
		return nil, nil, err
	}
	if len(bitmap) != nativeKeyBitmapBytes {
		return nil, nil, errors.New("invalid native keyboard state bitmap")
	}
	for i := range bitmap {
		if bitmap[i]&^capabilities[i] != 0 {
			return nil, nil, errors.New("native keyboard state exceeds capability proof")
		}
	}
	for code := 0; code < len(bitmap)*8; code++ {
		if bitmap[code/8]&(1<<uint(code%8)) == 0 {
			continue
		}
		held = append(held, uint16(code))
	}
	for off := 0; off < len(pending); off += nativeEventSize {
		record := pending[off : off+nativeEventSize]
		typ, _, _ := decodeNativeEvent(record)
		if typ != 1 { // EV_KEY
			reconciled = append(reconciled, record...)
		}
	}
	return held, reconciled, nil
}
