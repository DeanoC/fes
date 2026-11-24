//go:build linux

package tenfoot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

func TestNativeKeyboardBitmapUsesExactFDAndFullProof(t *testing.T) {
	for _, arch := range []string{"386", "amd64", "arm", "arm64"} {
		if !nativeKeyboardBitmapArchitectureSupported(arch) {
			t.Fatalf("supported architecture %s rejected", arch)
		}
	}
	for _, arch := range []string{"mips", "mips64", "ppc64", "s390x", "riscv64"} {
		if nativeKeyboardBitmapArchitectureSupported(arch) {
			t.Fatalf("unverified architecture %s accepted", arch)
		}
	}
	for _, held := range []bool{false, true} {
		wantRequest := uintptr(0x80604521)
		if held {
			wantRequest = 0x80604518
		}
		got, err := nativeKeyboardBitmap(17, held, func(fd int, request uintptr, bitmap []byte) (int, error) {
			if fd != 17 || request != wantRequest || len(bitmap) != nativeKeyBitmapBytes {
				t.Fatal("incorrect ioctl identity or request")
			}
			bitmap[5] = 4
			return len(bitmap), nil
		})
		if err != nil || len(got) != nativeKeyBitmapBytes || got[5] != 4 {
			t.Fatalf("bitmap %v %v", got, err)
		}
	}
	if _, err := nativeKeyboardBitmap(17, false, func(int, uintptr, []byte) (int, error) { return 31, nil }); err == nil {
		t.Fatal("short bitmap accepted")
	}
	if _, err := nativeKeyboardBitmap(-1, false, func(int, uintptr, []byte) (int, error) { t.Fatal("invalid fd used"); return 0, nil }); err == nil {
		t.Fatal("invalid fd accepted")
	}
}

func snapshotTestEvent(typ, code uint16, value int32) []byte {
	b := make([]byte, nativeEventSize)
	tail := b[len(b)-8:]
	binary.NativeEndian.PutUint16(tail, typ)
	binary.NativeEndian.PutUint16(tail[2:], code)
	binary.NativeEndian.PutUint32(tail[4:], uint32(value))
	return b
}

func TestNativeKeyboardSnapshotDiscardsOnlyBufferedKeyRecords(t *testing.T) {
	keyDown := snapshotTestEvent(1, 30, 1)
	rel := snapshotTestEvent(2, 0, 4)
	report := snapshotTestEvent(0, 0, 0)
	keyUp := snapshotTestEvent(1, 30, 0)
	uiRelease := snapshotTestEvent(1, 113, 0)
	pending := bytes.Join([][]byte{keyDown, rel, keyUp, uiRelease, report}, nil)
	original := bytes.Clone(pending)
	bitmap := make([]byte, nativeKeyBitmapBytes)
	bitmap[30/8] |= 1 << (30 % 8)   // A
	bitmap[42/8] |= 1 << (42 % 8)   // left Shift
	bitmap[113/8] |= 1 << (113 % 8) // UI key without a mapped HID usage
	called := 0
	held, kept, err := snapshotNativeKeyboard(pending, bitmap, func() ([]byte, error) {
		called++
		return bitmap, nil
	})
	if err != nil || called != 1 || !reflect.DeepEqual(held, []uint16{30, 42, 113}) {
		t.Fatalf("snapshot = %v, calls %d, error %v", held, called, err)
	}
	if !bytes.Equal(kept, bytes.Join([][]byte{rel, report}, nil)) || !bytes.Equal(pending, original) {
		t.Fatal("snapshot changed input or failed to preserve non-key records")
	}
}

func validSnapshotCaps() []byte {
	caps := make([]byte, nativeKeyBitmapBytes)
	caps[30/8] |= 1 << (30 % 8)
	return caps
}

func TestNativeKeyboardSnapshotFailsClosed(t *testing.T) {
	ioErr := errors.New("disconnected")
	for _, tc := range []struct {
		name         string
		pending      []byte
		bitmap       []byte
		ioErr        error
		called       bool
		capabilities []byte
	}{
		{"partial-record", []byte{1}, nil, nil, false, validSnapshotCaps()},
		{"oversized", make([]byte, (maxNativeSnapshotEvents+1)*nativeEventSize), nil, nil, false, validSnapshotCaps()},
		{"dropped", snapshotTestEvent(0, 3, 0), nil, nil, false, validSnapshotCaps()},
		{"unadvertised-button", snapshotTestEvent(1, 272, 1), nil, nil, false, validSnapshotCaps()},
		{"ioctl-failed", nil, nil, ioErr, true, validSnapshotCaps()},
		{"short-bitmap", nil, make([]byte, nativeKeyBitmapBytes-1), nil, true, validSnapshotCaps()},
		{"long-bitmap", nil, make([]byte, nativeKeyBitmapBytes+1), nil, true, validSnapshotCaps()},
		{"empty-capabilities", nil, nil, nil, false, make([]byte, nativeKeyBitmapBytes)},
		{"missing-capabilities", nil, nil, nil, false, nil},
		{"short-capabilities", nil, nil, nil, false, make([]byte, nativeKeyBitmapBytes-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			held, kept, err := snapshotNativeKeyboard(tc.pending, tc.capabilities, func() ([]byte, error) {
				calls++
				return tc.bitmap, tc.ioErr
			})
			if err == nil || held != nil || kept != nil || (calls != 0) != tc.called {
				t.Fatalf("held %v kept %v calls %d error %v", held, kept, calls, err)
			}
		})
	}
	if held, kept, err := snapshotNativeKeyboard(nil, make([]byte, nativeKeyBitmapBytes), nil); err == nil || held != nil || kept != nil {
		t.Fatal("nil snapshot function was accepted")
	}
}

func TestNativeKeyboardSnapshotRejectsHybridBeforeIOCTL(t *testing.T) {
	for _, code := range []int{0, 256, 272, 544, 767} {
		caps := make([]byte, nativeKeyBitmapBytes)
		caps[code/8] |= 1 << uint(code%8)
		called := false
		held, kept, err := snapshotNativeKeyboard(nil, caps, func() ([]byte, error) {
			called = true
			return make([]byte, nativeKeyBitmapBytes), nil
		})
		if err == nil || called || held != nil || kept != nil {
			t.Fatalf("code %d held %v kept %v called %v error %v", code, held, kept, called, err)
		}
	}
}

func TestNativeKeyboardSnapshotRejectsHeldOutsideCapabilities(t *testing.T) {
	caps := validSnapshotCaps()
	bitmap := make([]byte, nativeKeyBitmapBytes)
	bitmap[42/8] |= 1 << (42 % 8)
	held, kept, err := snapshotNativeKeyboard(nil, caps, func() ([]byte, error) { return bitmap, nil })
	if err == nil || held != nil || kept != nil {
		t.Fatalf("held %v kept %v error %v", held, kept, err)
	}
}
