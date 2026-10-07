//go:build linux

package tenfoot

import (
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/kitlauncher/controller"
	"golang.org/x/sys/unix"
	"os"
	"testing"
	"time"
)

func TestFramebufferMouseSYNAtomicCapture(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fd := int(r.Fd())
	unix.SetNonblock(fd, true)
	a := NewApp(NewClient("http://not-used", nil), 1280, 720, 10)
	feed := &localPadRecorder{}
	a.localFeed = feed
	a.session = hostclient.SessionResult{State: "active", CoreMouse: true, Input: &hostclient.SessionInput{State: "attached", Ready: true}}
	// The normal hardware-room path selects the same local socket feed.
	a.localPhase = localPhaseRunning
	in := &nativeInput{fd: fd, kind: InputMouse, mapper: controller.NewMouseMapper()}
	ins := &nativeInputs{devices: []*nativeInput{in}}
	for _, v := range [][3]int32{{2, 0, 7}, {2, 1, -4}, {1, 272, 1}} {
		w.Write(nativeTestEvent(uint16(v[0]), uint16(v[1]), v[2]))
	}
	if _, err := ins.poll(a, time.Now()); err != nil {
		t.Fatal(err)
	}
	if a.playHIDTail != nil {
		t.Fatal("partial mouse packet")
	}
	w.Write(nativeTestEvent(0, 0, 0))
	if _, err := ins.poll(a, time.Now()); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	tail := a.playHIDTail
	a.mu.Unlock()
	select {
	case <-tail:
	case <-time.After(time.Second):
		t.Fatal("mouse queue")
	}
	// No real HTTP server or evdev device is involved; held state is inspected
	// independently of the async delivery adapter.
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.playHIDHeld) != 1 {
		t.Fatal(a.playHIDHeld)
	}
	for _, e := range a.playHIDHeld {
		_, _, b, ok := remoteinput.MouseVector(e)
		if !ok || b != 1 {
			t.Fatal(e)
		}
	}
}
