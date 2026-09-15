//go:build darwin && cgo

package attractvideo

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework CoreGraphics -framework CoreMedia -framework CoreVideo -framework Foundation
#include "player_darwin.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"unsafe"
)

type darwinPlayer struct {
	mu     sync.Mutex
	ptr    unsafe.Pointer
	img    *image.RGBA
	closed bool
}

func takeCString(p *C.char) string {
	if p == nil {
		return ""
	}
	s := C.GoString(p)
	C.free(unsafe.Pointer(p))
	return s
}

// Available reports that Darwin CGO builds can decode attract video.
func Available() bool { return true }

// Open starts an AVAssetReader against a local media file.
func Open(path string) (Player, error) {
	if path == "" {
		return nil, errors.New("video path is empty")
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var cerr *C.char
	ptr := C.fogcast_attract_video_open(cpath, &cerr)
	if ptr == nil {
		msg := takeCString(cerr)
		if msg == "" {
			msg = "video open failed"
		}
		return nil, errors.New(msg)
	}
	var w, h C.int
	C.fogcast_attract_video_dimensions(ptr, &w, &h)
	if w < 1 || h < 1 {
		C.fogcast_attract_video_close(ptr)
		return nil, fmt.Errorf("video dimensions %dx%d are invalid", int(w), int(h))
	}
	p := &darwinPlayer{
		ptr: ptr,
		img: image.NewRGBA(image.Rect(0, 0, int(w), int(h))),
	}
	runtime.SetFinalizer(p, func(d *darwinPlayer) { d.Close() })
	return p, nil
}

func (p *darwinPlayer) Frame() (*image.RGBA, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.ptr == nil {
		return nil, false, errors.New("video player is closed")
	}
	var ended C.int
	var cerr *C.char
	rc := C.fogcast_attract_video_copy_rgba(p.ptr, (*C.uint8_t)(unsafe.Pointer(&p.img.Pix[0])), C.int(p.img.Stride), C.int(p.img.Bounds().Dy()), &ended, &cerr)
	runtime.KeepAlive(p.img)
	switch rc {
	case 1:
		return p.img, ended != 0, nil
	case 0:
		return nil, false, nil
	case -1:
		return nil, true, nil
	default:
		msg := takeCString(cerr)
		if msg == "" {
			msg = "video decode failed"
		}
		return nil, false, errors.New(msg)
	}
}

func (p *darwinPlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.ptr != nil {
		C.fogcast_attract_video_close(p.ptr)
		p.ptr = nil
	}
	runtime.SetFinalizer(p, nil)
}
