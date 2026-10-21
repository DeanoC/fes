//go:build linux

package menudisplay

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/testsock"
	"golang.org/x/sys/unix"
)

// fakeRuntime answers status, grants an ordinary 8 MiB file as the window,
// and alternates slots the way scanout does.
type fakeRuntime struct {
	t          *testing.T
	listener   *net.UnixListener
	generation uint64
	underflows uint64
	window     int
	mu         sync.Mutex
	grants     int
	shown      int
	sequence   uint64
	commits    []map[string]any
	done       chan struct{}
}

func newFakeRuntime(t *testing.T) (*fakeRuntime, string) {
	t.Helper()
	path := testsock.Path(t, "runtime.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	window, err := unix.MemfdCreate("test-menu-window", unix.MFD_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Ftruncate(window, Slots*SlotBytes); err != nil {
		t.Fatal(err)
	}
	f := &fakeRuntime{t: t, listener: listener, generation: 7, window: window, done: make(chan struct{})}
	go f.serve()
	t.Cleanup(func() {
		listener.Close()
		<-f.done
		unix.Close(window)
	})
	return f, path
}

func (f *fakeRuntime) serve() {
	defer close(f.done)
	for {
		conn, err := f.listener.AcceptUnix()
		if err != nil {
			return
		}
		f.handle(conn)
		conn.Close()
	}
}

func (f *fakeRuntime) handle(conn *net.UnixConn) {
	line, fds, err := readLine(conn)
	if err != nil || len(fds) != 0 {
		return
	}
	var request map[string]any
	if json.Unmarshal(line, &request) != nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	write := func(reply string, fd int) {
		var rights []byte
		if fd >= 0 {
			rights = unix.UnixRights(fd)
		}
		_, _, _ = conn.WriteMsgUnix([]byte(reply+"\n"), rights, nil)
	}
	switch request["operation"] {
	case "status":
		write(fmt.Sprintf(`{"protocol":2,"ok":true,"menu_display":{"available":true,"generation":%d,"width":1280,"height":720,"stride":5120,"byte_count":3686400,"slot_bytes":4194304,"slots":2,"format":"bgrx8888","displayed_sequence":%d,"underflows":0}}`, f.generation, f.sequence), -1)
	case "menu_frame_grant":
		if request["expected_generation"] != float64(f.generation) {
			write(`{"protocol":2,"ok":false,"error":{"code":"invalid_request","message":"menu generation changed"}}`, -1)
			return
		}
		f.grants++
		write(fmt.Sprintf(`{"protocol":2,"ok":true,"menu_frame":{"generation":%d,"format":"bgrx8888","slots":2,"slot_bytes":4194304}}`, f.generation), f.window)
	case "menu_frame_begin":
		slot := f.shown ^ 1
		write(fmt.Sprintf(`{"protocol":2,"ok":true,"menu_frame":{"generation":%d,"slot":%d}}`, f.generation, slot), -1)
		line, fds, err := readLine(conn)
		if err != nil || len(fds) != 0 || json.Unmarshal(line, &request) != nil ||
			request["operation"] != "menu_frame_commit" || request["slot"] != float64(slot) {
			write(`{"protocol":2,"ok":false,"error":{"code":"invalid_request","message":"bad commit"}}`, -1)
			return
		}
		f.commits = append(f.commits, request)
		f.shown = slot
		f.sequence++
		write(fmt.Sprintf(`{"protocol":2,"ok":true,"menu_frame":{"generation":%d,"slot":%d,"displayed_sequence":%d,"underflows":%d,"timing":{"clean_us":700,"submit_us":12,"scanout_wait_us":9000}}}`, f.generation, slot, f.sequence, f.underflows), -1)
	}
}

func (f *fakeRuntime) grantCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.grants
}

func (f *fakeRuntime) commit(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commits[i]
}

// slot reads back slot s of the granted window.
func (f *fakeRuntime) slot(s int) []byte {
	pixels := make([]byte, FrameBytes)
	if _, err := unix.Pread(f.window, pixels, int64(s*SlotBytes)); err != nil {
		f.t.Fatal(err)
	}
	return pixels
}

func filled(r, g, b byte) []byte {
	pix := make([]byte, FrameBytes)
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = r, g, b, 0xff
	}
	return pix
}

func fillRect(pix []byte, rect Rect, r, g, b byte) {
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x++ {
			at := y*Stride + x*4
			pix[at], pix[at+1], pix[at+2] = r, g, b
		}
	}
}

func TestStatusGrantAndPresentWriteTheSlotInScanoutOrder(t *testing.T) {
	runtime, path := newFakeRuntime(t)
	client := New(path)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil || !status.Available || status.Generation != 7 || status.Format != Format {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	result, err := client.Present(ctx, 7, filled(0x11, 0x22, 0x33))
	if err != nil || result.DisplayedSequence != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// B, G, R, X: the XRGB8888 word 0x00112233 the menu scanout reads.
	if got := runtime.slot(1)[:4]; string(got) != "\x33\x22\x11\x00" {
		t.Fatalf("slot pixel % x", got)
	}
	if _, partial := runtime.commit(0)["written"]; partial {
		t.Fatal("a whole frame named written regions")
	}
	if tm := result.Timing; tm.RuntimeCleanUS != 700 || tm.RuntimeSubmitUS != 12 || tm.RuntimeScanoutWaitUS != 9000 ||
		tm.StagedPixels != Width*Height || tm.BeginUS <= 0 || tm.StageUS <= 0 || tm.CommitUS <= 0 {
		t.Fatalf("timing=%+v", tm)
	}
	if _, err := client.Present(ctx, 7, filled(0x44, 0x55, 0x66)); err != nil {
		t.Fatal(err)
	}
	if got := runtime.slot(0)[:4]; string(got) != "\x66\x55\x44\x00" || runtime.grantCount() != 1 {
		t.Fatalf("second slot % x after %d grants", got, runtime.grantCount())
	}
	// A new generation is a new grant.
	runtime.mu.Lock()
	runtime.generation = 8
	runtime.mu.Unlock()
	if _, err := client.Present(ctx, 7, filled(0, 0, 0)); err == nil {
		t.Fatal("stale generation presented")
	}
	if _, err := client.Present(ctx, 8, filled(0, 0, 0)); err != nil || runtime.grantCount() != 2 {
		t.Fatalf("regrant: %v after %d grants", err, runtime.grantCount())
	}
}

func TestPresentDamageWritesWhatTheSlotMissed(t *testing.T) {
	runtime, path := newFakeRuntime(t)
	client := New(path)
	defer client.Close()
	ctx := context.Background()
	a := Rect{X: 96, Y: 50, W: 32, H: 10}
	b := Rect{X: 640, Y: 300, W: 16, H: 4}
	frame := filled(0x10, 0x10, 0x10)
	for _, want := range []uint64{1, 2} {
		if result, err := client.Present(ctx, 7, frame); err != nil || result.DisplayedSequence != want {
			t.Fatalf("whole frame %d: %+v %v", want, result, err)
		}
	}
	if _, err := client.PresentDamage(ctx, 7, frame, Damage{BaseSequence: 2}); err == nil {
		t.Fatal("empty damage accepted")
	}
	// Slot 1 holds frame 1 and frame 2 was whole, so frame 3 goes whole.
	fillRect(frame, a, 0xa0, 0xa1, 0xa2)
	if result, err := client.PresentDamage(ctx, 7, frame, Damage{BaseSequence: 2, Rects: []Rect{a}}); err != nil ||
		result.Timing.StagedPixels != Width*Height {
		t.Fatalf("frame 3: %+v %v", result, err)
	}
	// Slot 0 holds frame 2: write frame 4's damage and frame 3's.
	fillRect(runtime.slotView(0), Rect{0, 0, Width, Height}, 0, 0, 0) // poison what must not be rewritten
	fillRect(frame, b, 0xb0, 0xb1, 0xb2)
	result, err := client.PresentDamage(ctx, 7, frame, Damage{BaseSequence: 3, Rects: []Rect{b}})
	if err != nil || result.Timing.StagedPixels != int64(a.W*a.H+b.W*b.H) {
		t.Fatalf("frame 4: %+v %v", result, err)
	}
	body, _ := json.Marshal(runtime.commit(3)["written"])
	if string(body) != `[[640,300,16,4],[96,50,32,10]]` {
		t.Fatalf("written %s", body)
	}
	slot := runtime.slot(0)
	for _, check := range []struct {
		x, y int
		want string
	}{{100, 55, "\xa2\xa1\xa0\x00"}, {645, 302, "\xb2\xb1\xb0\x00"}, {0, 0, "\x00\x00\x00\x00"}} {
		at := check.y*Stride + check.x*4
		if got := string(slot[at : at+4]); got != check.want {
			t.Fatalf("slot pixel %d,%d = % x", check.x, check.y, got)
		}
	}
	// A base that is not the frame shown goes whole.
	if result, err := client.PresentDamage(ctx, 7, frame, Damage{BaseSequence: 2, Rects: []Rect{b}}); err != nil ||
		result.Timing.StagedPixels != Width*Height {
		t.Fatalf("stale base: %+v %v", result, err)
	}
}

// slotView poisons through a second mapping of the window.
func (f *fakeRuntime) slotView(s int) []byte {
	mapping, err := unix.Mmap(f.window, int64(s*SlotBytes), SlotBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { unix.Munmap(mapping) })
	return mapping
}

func TestRevokedWindowFaultIsAnError(t *testing.T) {
	_, path := newFakeRuntime(t)
	client := New(path)
	if _, err := client.Present(context.Background(), 7, filled(1, 2, 3)); err != nil {
		t.Fatal(err)
	}
	// A revoked grant leaves the mapping without pages; unmapping it under
	// the client gives the same fault.
	client.mu.Lock()
	mapping := client.window.mapping
	client.mu.Unlock()
	if err := unix.Munmap(mapping); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Present(context.Background(), 7, filled(1, 2, 3)); err == nil {
		t.Fatal("present into a revoked window succeeded")
	}
	client.mu.Lock()
	client.window = nil
	client.mu.Unlock()
}

func TestPresentUnderflowCap(t *testing.T) {
	for _, tc := range []struct {
		underflows uint64
		ok         bool
	}{
		{10, true},
		{TransientUnderflowCap, true},
		{TransientUnderflowCap + 1, false},
	} {
		t.Run(u64(tc.underflows), func(t *testing.T) {
			runtime, path := newFakeRuntime(t)
			runtime.underflows = tc.underflows
			client := New(path)
			defer client.Close()
			result, err := client.Present(context.Background(), 7, make([]byte, FrameBytes))
			if tc.ok {
				if err != nil || result.Underflows != tc.underflows || result.DisplayedSequence != 1 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else if err == nil {
				t.Fatal("underflow above the cap accepted")
			}
		})
	}
}

func u64(value uint64) string {
	return strconv.FormatUint(value, 10)
}

func TestPresentRejectsInvalidFrame(t *testing.T) {
	client := New(filepath.Join(t.TempDir(), "missing"))
	if _, err := client.Present(context.Background(), 0, make([]byte, FrameBytes)); err == nil {
		t.Fatal("zero generation accepted")
	}
	if _, err := client.Present(context.Background(), 1, make([]byte, 1)); err == nil {
		t.Fatal("short pixels accepted")
	}
}

var errUnexpected = &unexpected{}

type unexpected struct{}

func (*unexpected) Error() string { return "unexpected protocol value" }

func readLine(conn *net.UnixConn) ([]byte, []int, error) {
	var line []byte
	var fds []int
	for {
		buf := make([]byte, 4096)
		oob := make([]byte, unix.CmsgSpace(4))
		n, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
		if err != nil {
			return nil, nil, err
		}
		line = append(line, buf[:n]...)
		if oobn > 0 {
			msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
			if err != nil {
				return nil, nil, err
			}
			for _, msg := range msgs {
				got, err := unix.ParseUnixRights(&msg)
				if err != nil {
					return nil, nil, err
				}
				fds = append(fds, got...)
			}
		}
		if len(line) > 0 && line[len(line)-1] == '\n' {
			return line[:len(line)-1], fds, nil
		}
		if len(line) > 65536 {
			return nil, nil, errUnexpected
		}
	}
}
