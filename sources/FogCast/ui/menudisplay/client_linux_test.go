//go:build linux

package menudisplay

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/testsock"
	"golang.org/x/sys/unix"
)

// fakeRuntime answers status, grants an ordinary file as the window, and
// alternates slots the way scanout does. With blitter set it grants the
// blitter's layout and runs list commits through blitReference, reading the
// list and heap from ddr: only what a commit names is written back there,
// as FES_WINDOW_CLEAN does on the kit.
type fakeRuntime struct {
	t                     *testing.T
	listener              *net.UnixListener
	generation            uint64
	underflows            uint64
	window                int
	mapping               []byte
	mu                    sync.Mutex
	grants                int
	shown                 int
	sequence              uint64
	commits               []map[string]any
	done                  chan struct{}
	blitter               bool
	frameMapping          bool
	misreportFrameMapping bool
	checkCommit           func() error
	badPreparation        bool
	onAbandon             func()
	// blitterMinor is the menu-blitter minor a blitter grant declares.
	blitterMinor int
	// listedGen is the generation whose grant took a list.
	listedGen uint64
	ddr       []byte
	// refuse fails the next commit with this message.
	refuse string
	// preparations counts begins on preparedGen.
	preparedGen, preparations uint64
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
	if err := unix.Ftruncate(window, BlitWindowBytes); err != nil {
		t.Fatal(err)
	}
	mapping, err := unix.Mmap(window, 0, BlitWindowBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRuntime{t: t, listener: listener, generation: 7, window: window, mapping: mapping, done: make(chan struct{}), ddr: make([]byte, BlitWindowBytes)}
	go f.serve()
	t.Cleanup(func() {
		listener.Close()
		<-f.done
		unix.Munmap(mapping)
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
	frame := ""
	if f.frameMapping {
		frame = `,"frame_mapping":true`
	}
	switch request["operation"] {
	case "status":
		write(fmt.Sprintf(`{"protocol":2,"ok":true,"menu_display":{"available":true,"generation":%d,"width":1280,"height":720,"stride":5120,"byte_count":3686400,"slot_bytes":4194304,"slots":2,"format":"bgrx8888","displayed_sequence":%d,"underflows":0%s}}`, f.generation, f.sequence, frame), -1)
	case "menu_frame_grant":
		_, declared := request["frame_mapping"]
		if (f.frameMapping && request["frame_mapping"] != true) || (!f.frameMapping && declared) {
			write(`{"protocol":2,"ok":false,"error":{"code":"unsupported_interface","message":"mapping agreement missing"}}`, -1)
			return
		}
		if request["expected_generation"] != float64(f.generation) {
			write(`{"protocol":2,"ok":false,"error":{"code":"invalid_request","message":"menu generation changed"}}`, -1)
			return
		}
		f.grants++
		blitter := ""
		if f.blitter {
			blitter = fmt.Sprintf(`,"blitter":{"minor":%d,"list_offset":8388608,"commands":32768,"command_bytes":32,"heap_offset":16777216,"heap_bytes":67108864}`, f.blitterMinor)
		}
		if f.misreportFrameMapping {
			frame = ""
		}
		write(fmt.Sprintf(`{"protocol":2,"ok":true,"menu_frame":{"generation":%d,"format":"bgrx8888","slots":2,"slot_bytes":4194304%s%s}}`, f.generation, blitter, frame), f.window)
	case "menu_frame_begin":
		slot := f.shown ^ 1
		if f.preparedGen != f.generation {
			f.preparedGen, f.preparations = f.generation, 0
		}
		f.preparations++
		preparedGeneration := f.generation
		if f.badPreparation {
			preparedGeneration++
		}
		write(fmt.Sprintf(`{"protocol":2,"ok":true,"menu_frame":{"generation":%d,"slot":%d,"preparation":%d}}`, preparedGeneration, slot, f.preparations), -1)
		line, fds, err := readLine(conn)
		if err != nil || len(fds) != 0 || json.Unmarshal(line, &request) != nil ||
			request["operation"] != "menu_frame_commit" || request["slot"] != float64(slot) {
			if f.onAbandon != nil {
				f.onAbandon()
			}
			write(`{"protocol":2,"ok":false,"error":{"code":"invalid_request","message":"bad commit"}}`, -1)
			return
		}
		f.commits = append(f.commits, request)
		if f.checkCommit != nil {
			if err := f.checkCommit(); err != nil {
				write(fmt.Sprintf(`{"protocol":2,"ok":false,"error":{"code":"invalid_request","message":%q}}`, err.Error()), -1)
				return
			}
		}
		if message := f.commitError(request, slot); message != "" {
			write(fmt.Sprintf(`{"protocol":2,"ok":false,"error":{"code":"invalid_request","message":%q}}`, message), -1)
			return
		}
		f.shown = slot
		f.sequence++
		write(fmt.Sprintf(`{"protocol":2,"ok":true,"menu_frame":{"generation":%d,"slot":%d,"displayed_sequence":%d,"underflows":%d,"timing":{"clean_us":700,"submit_us":12,"scanout_wait_us":9000}}}`, f.generation, slot, f.sequence, f.underflows), -1)
	}
}

// commitError applies the runtime's commit rules and runs a list.
func (f *fakeRuntime) commitError(request map[string]any, slot int) string {
	if f.refuse != "" {
		message := f.refuse
		f.refuse = ""
		return message
	}
	list, isList := request["list"].(map[string]any)
	if !isList {
		if f.blitter && f.listedGen == f.generation {
			return "menu grant takes only lists after its first list"
		}
		return ""
	}
	if !f.blitter {
		return "menu grant has no blitter"
	}
	if _, written := request["written"]; written {
		return "menu commit names written regions or a list, not both"
	}
	first, count := int(list["first"].(float64)), int(list["count"].(float64))
	if first < 0 || count < 1 || first+count > ListCommands {
		return "menu list is outside the list region"
	}
	at := ListOffset + first*CommandBytes
	copy(f.ddr[at:at+count*CommandBytes], f.mapping[at:at+count*CommandBytes])
	if uploaded, ok := request["uploaded"].([]any); ok {
		if len(uploaded) < 1 || len(uploaded) > MaxUploadSpans {
			return "menu uploads are 1 to 16 spans"
		}
		for _, raw := range uploaded {
			span := raw.([]any)
			offset, bytes := int(span[0].(float64)), int(span[1].(float64))
			if offset < HeapOffset || bytes < 1 || offset+bytes > BlitWindowBytes {
				return "menu upload is outside the texture heap"
			}
			copy(f.ddr[offset:offset+bytes], f.mapping[offset:offset+bytes])
		}
	}
	if err := blitReference(f.ddr[at:at+count*CommandBytes], f.ddr, f.mapping[slot*SlotBytes:slot*SlotBytes+FrameBytes], f.blitterMinor); err != nil {
		return "menu blitter fault: " + err.Error()
	}
	f.listedGen = f.generation
	return ""
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
	// B, G, R, then the pixel's alpha: the XRGB8888 word 0xff112233 the menu
	// scanout reads, opaque to a session plane.
	if got := runtime.slot(1)[:4]; string(got) != "\x33\x22\x11\xff" {
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
	if got := runtime.slot(0)[:4]; string(got) != "\x66\x55\x44\xff" || runtime.grantCount() != 1 {
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
	testPresentDamageWritesWhatTheSlotMissed(t, false)
}

func TestFrameMappingPreservesDamageAcrossRemap(t *testing.T) {
	testPresentDamageWritesWhatTheSlotMissed(t, true)
}

func testPresentDamageWritesWhatTheSlotMissed(t *testing.T, frameMapping bool) {
	runtime, path := newFakeRuntime(t)
	runtime.mu.Lock()
	runtime.frameMapping = frameMapping
	runtime.mu.Unlock()
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
	}{{100, 55, "\xa2\xa1\xa0\xff"}, {645, 302, "\xb2\xb1\xb0\xff"}, {0, 0, "\x00\x00\x00\xff"}} {
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
	runtime, path := newFakeRuntime(t)
	client := New(path)
	defer client.Close()
	if _, err := client.Present(context.Background(), 7, filled(1, 2, 3)); err != nil {
		t.Fatal(err)
	}
	// A revoked grant leaves the mapping without pages, so the next access
	// faults; truncating the window does the same. Unmapping it instead
	// would free the range for the next thread stack the runtime maps.
	if err := unix.Ftruncate(runtime.window, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Present(context.Background(), 7, filled(1, 2, 3)); err == nil {
		t.Fatal("present into a revoked window succeeded")
	}
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

func TestReleaseWindowAllowsSameGenerationRemap(t *testing.T) {
	runtime, path := newFakeRuntime(t)
	client := New(path)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.Grant(ctx, 7); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	client.window.mapping[0] = 0x45
	client.mu.Unlock()
	if err := client.ReleaseWindow(); err != nil {
		t.Fatal(err)
	}
	if err := client.ReleaseWindow(); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	released := client.window == nil
	client.mu.Unlock()
	if !released {
		t.Fatal("client retained mapping")
	}
	if _, err := client.Grant(ctx, 7); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	pixel := client.window.mapping[0]
	client.mu.Unlock()
	runtime.mu.Lock()
	grants := runtime.grants
	runtime.mu.Unlock()
	if grants != 2 || pixel != 0x45 {
		t.Fatalf("grants=%d pixel=%x; remap must preserve same-generation storage", grants, pixel)
	}
}

// Count actual mappings of this grant's inode. The fake runtime retains its
// own view; the client must have removed its separate view before commit.
func grantMappingCount(fd int) (int, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return 0, err
	}
	data, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 6 && fields[4] == strconv.FormatUint(st.Ino, 10) && strings.Contains(fields[5], "memfd:test-menu-window") {
			count++
		}
	}
	return count, nil
}

func TestFrameMappingEndsBeforeCommitAndFailureReply(t *testing.T) {
	f, path := newFakeRuntime(t)
	f.mu.Lock()
	f.frameMapping = true
	baseline, err := grantMappingCount(f.window)
	if err != nil {
		t.Fatal(err)
	}
	f.checkCommit = func() error {
		count, err := grantMappingCount(f.window)
		if err != nil {
			return err
		}
		if count != baseline {
			return fmt.Errorf("client mapping still live at commit: %d vs %d", count, baseline)
		}
		return nil
	}
	f.mu.Unlock()
	c := New(path)
	defer c.Close()
	if _, err := c.Grant(context.Background(), f.generation); err != nil {
		t.Fatal(err)
	}
	if len(c.window.mapping) != 0 {
		t.Fatal("grant inspection retained a frame mapping")
	}
	pix := make([]byte, FrameBytes)
	for i := 0; i < 3; i++ {
		pix[0] = byte(i + 10)
		result, err := c.Present(context.Background(), f.generation, pix)
		if err != nil {
			t.Fatal(err)
		}
		if result.DisplayedSequence != uint64(i+1) || len(c.window.mapping) != 0 {
			t.Fatal("frame did not complete with released mapping")
		}
		f.mu.Lock()
		got := f.mapping[f.shown*SlotBytes+2] // RGBA input red is byte2 in BGRX scanout.
		f.mu.Unlock()
		if got != pix[0] {
			t.Fatal("unmapping lost the submitted pixels")
		}
	}
	f.mu.Lock()
	f.refuse = "injected frame fault"
	f.mu.Unlock()
	if _, err := c.Present(context.Background(), f.generation, pix); err == nil || !strings.Contains(err.Error(), "injected frame fault") {
		t.Fatalf("failure reply: %v", err)
	}
	if len(c.window.mapping) != 0 {
		t.Fatal("failure reply retained mapping")
	}
	if _, err := c.Present(context.Background(), f.generation, pix); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	grants := f.grants
	f.mu.Unlock()
	if grants != 6 {
		t.Fatalf("expected a new grant for each frame, got %d", grants)
	}
}

func TestFrameMappingGrantMustMatchNegotiatedStatus(t *testing.T) {
	f, path := newFakeRuntime(t)
	f.mu.Lock()
	f.frameMapping, f.misreportFrameMapping = true, true
	f.mu.Unlock()
	c := New(path)
	defer c.Close()
	if _, err := c.Grant(context.Background(), f.generation); err == nil {
		t.Fatal("accepted a grant without its negotiated mapping contract")
	}
	if c.window != nil {
		t.Fatal("mapped contradictory grant")
	}
}

func TestFrameMappingEndsBeforePreparationAbandonment(t *testing.T) {
	f, path := newFakeRuntime(t)
	baseline, err := grantMappingCount(f.window)
	if err != nil {
		t.Fatal(err)
	}
	abandoned := make(chan error, 1)
	f.mu.Lock()
	f.frameMapping, f.badPreparation = true, true
	f.onAbandon = func() {
		count, err := grantMappingCount(f.window)
		if err == nil && count != baseline {
			err = fmt.Errorf("client mapping live at abandonment: %d vs %d", count, baseline)
		}
		abandoned <- err
	}
	f.mu.Unlock()
	c := New(path)
	defer c.Close()
	if _, err := c.Present(context.Background(), f.generation, make([]byte, FrameBytes)); err == nil {
		t.Fatal("accepted invalid preparation")
	}
	select {
	case err := <-abandoned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("preparation connection did not close")
	}
	if c.window == nil || len(c.window.mapping) != 0 {
		t.Fatal("preparation failure did not release peer view")
	}
}
