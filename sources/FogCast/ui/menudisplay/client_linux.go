//go:build linux

// Package menudisplay presents RGBA frames through the local runtime. The
// runtime grants both menu slots of the FPGA scanout window; the client maps
// them cached, writes each frame into the slot scanout is not showing, and
// commits it. The runtime writes back those lines and controls the FPGA.
package menudisplay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	Width      = 1280
	Height     = 720
	Stride     = Width * 4
	FrameBytes = Stride * Height
	SlotBytes  = 4 << 20
	Slots      = 2
	// Format is the slot pixel layout: XRGB8888 words, B, G, R, X bytes. The
	// client writes each pixel's alpha as X: idle MENU ignores it and a session
	// plane blends with it as premultiplied alpha.
	Format   = "bgrx8888"
	maxReply = 64 << 10
)

// Client serializes presents; one MenuDisplay worker drives it.
type Client struct {
	Path   string
	mu     sync.Mutex
	window *window
}

// window is one generation's grant, mapped once: both slots, and on a
// blitter grant the list region and texture heap too.
type window struct {
	generation uint64
	mapping    []byte
	// held[s] is the displayed sequence of the frame slot s holds, zero
	// when unknown. damage is the shown frame's damage against the frame
	// before it, nil for a whole frame.
	held   [Slots]uint64
	damage []Rect
	// heap is the textures resident in a blitter grant's heap. Every socket
	// peer shares that heap, so it holds only while no other peer has begun
	// a frame: prepared is this client's last preparation of the generation.
	heap     *blitHeap
	prepared uint64
	// minor is a blitter grant's menu-blitter minor.
	minor int
}
type Status struct {
	Available         bool   `json:"available"`
	Session           bool   `json:"session"`
	PackageID         string `json:"package_id"`
	CoreGeneration    uint64 `json:"core_generation"`
	Generation        uint64 `json:"generation"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	Stride            int    `json:"stride"`
	ByteCount         int    `json:"byte_count"`
	SlotBytes         int    `json:"slot_bytes"`
	Slots             int    `json:"slots"`
	Format            string `json:"format"`
	DisplayedSequence uint64 `json:"displayed_sequence"`
	Underflows        uint64 `json:"underflows"`
	// Picture is a session plane's machine picture as x, y, width and height.
	Picture []int `json:"picture"`
}
type Result struct {
	Generation        uint64 `json:"generation"`
	DisplayedSequence uint64 `json:"displayed_sequence"`
	Underflows        uint64 `json:"underflows"`
	// Timing is where this present's time went (diagnostics only).
	Timing Timing `json:"-"`
}
type reply struct {
	OK          bool    `json:"ok"`
	MenuDisplay *Status `json:"menu_display"`
	MenuFrame   *struct {
		Generation        uint64  `json:"generation"`
		Slot              *int    `json:"slot"`
		Preparation       uint64  `json:"preparation"`
		Format            string  `json:"format"`
		Slots             int     `json:"slots"`
		SlotBytes         int     `json:"slot_bytes"`
		DisplayedSequence *uint64 `json:"displayed_sequence"`
		Underflows        uint64  `json:"underflows"`
		Blitter           *struct {
			Minor        int `json:"minor"`
			ListOffset   int `json:"list_offset"`
			Commands     int `json:"commands"`
			CommandBytes int `json:"command_bytes"`
			HeapOffset   int `json:"heap_offset"`
			HeapBytes    int `json:"heap_bytes"`
		} `json:"blitter"`
		Timing *struct {
			CleanUS       int64 `json:"clean_us"`
			BlitUS        int64 `json:"blit_us"`
			SubmitUS      int64 `json:"submit_us"`
			ScanoutWaitUS int64 `json:"scanout_wait_us"`
		} `json:"timing"`
	} `json:"menu_frame"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func New(path string) *Client { return &Client{Path: path} }

func (c *Client) dial(ctx context.Context) (*net.UnixConn, error) {
	if c == nil || c.Path == "" {
		return nil, errors.New("menu runtime socket unavailable")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.Path)
	if err != nil {
		return nil, err
	}
	u, ok := conn.(*net.UnixConn)
	if !ok {
		conn.Close()
		return nil, errors.New("menu runtime is not a Unix socket")
	}
	deadline := time.Now().Add(4 * time.Second)
	if user, ok := ctx.Deadline(); ok && user.Before(deadline) {
		deadline = user
	}
	if err := u.SetDeadline(deadline); err != nil {
		u.Close()
		return nil, err
	}
	return u, nil
}

func send(conn *net.UnixConn, message any, fd int) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	var rights []byte
	if fd >= 0 {
		rights = unix.UnixRights(fd)
	}
	n, _, err := conn.WriteMsgUnix(data, rights, nil)
	if err != nil {
		return err
	}
	if n != len(data) {
		return errors.New("short menu request")
	}
	return nil
}

func receive(conn *net.UnixConn) (reply, []int, error) {
	var line []byte
	var fds []int
	bad := func(err error) (reply, []int, error) {
		for _, fd := range fds {
			unix.Close(fd)
		}
		return reply{}, nil, err
	}
	for {
		var buf [4096]byte
		var oob [128]byte
		n, oobn, flags, _, err := conn.ReadMsgUnix(buf[:], oob[:])
		if err != nil {
			return bad(err)
		}
		if n == 0 {
			return bad(errors.New("menu response ended before newline"))
		}
		if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
			return bad(errors.New("truncated menu response"))
		}
		if oobn > 0 {
			messages, err := unix.ParseSocketControlMessage(oob[:oobn])
			if err != nil {
				return bad(err)
			}
			for _, control := range messages {
				if control.Header.Level != unix.SOL_SOCKET || control.Header.Type != unix.SCM_RIGHTS {
					return bad(errors.New("invalid menu descriptor"))
				}
				got, err := unix.ParseUnixRights(&control)
				if err != nil {
					return bad(err)
				}
				for _, fd := range got {
					unix.CloseOnExec(fd)
				}
				fds = append(fds, got...)
			}
			if len(fds) > 1 {
				return bad(errors.New("too many menu descriptors"))
			}
		}
		line = append(line, buf[:n]...)
		if len(line) > maxReply {
			return bad(errors.New("menu response too large"))
		}
		if end := bytes.IndexByte(line, '\n'); end >= 0 {
			if end != len(line)-1 {
				return bad(errors.New("extra menu response bytes"))
			}
			var result reply
			if err := json.Unmarshal(line[:end], &result); err != nil {
				return bad(err)
			}
			if !result.OK {
				return bad(fmt.Errorf("menu request rejected: %s %s", result.Error.Code, result.Error.Message))
			}
			return result, fds, nil
		}
	}
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return Status{}, err
	}
	defer conn.Close()
	if err := send(conn, map[string]any{"protocol": 2, "operation": "status"}, -1); err != nil {
		return Status{}, err
	}
	result, fds, err := receive(conn)
	for _, fd := range fds {
		unix.Close(fd)
	}
	if err != nil {
		return Status{}, err
	}
	if len(fds) != 0 {
		return Status{}, errors.New("status returned a descriptor")
	}
	if result.MenuDisplay == nil {
		return Status{}, nil
	}
	s := *result.MenuDisplay
	if !s.Available {
		return s, nil
	}
	if s.Generation == 0 || s.Width != Width || s.Height != Height || s.Stride != Stride || s.ByteCount != FrameBytes ||
		s.SlotBytes != SlotBytes || s.Slots != Slots || s.Format != Format {
		return Status{}, errors.New("unsupported menu geometry")
	}
	return s, nil
}

// Close unmaps the granted window.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeWindowLocked()
}

func (c *Client) closeWindowLocked() error {
	if c.window == nil {
		return nil
	}
	err := unix.Munmap(c.window.mapping)
	c.window = nil
	return err
}

// windowLocked maps generation's grant, once per generation. A blitter
// grant is mapped through its heap; any other grant covers the two slots.
func (c *Client) windowLocked(ctx context.Context, generation uint64) (*window, error) {
	if c.window != nil && c.window.generation == generation {
		return c.window, nil
	}
	if err := c.closeWindowLocked(); err != nil {
		return nil, err
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := send(conn, map[string]any{"protocol": 2, "operation": "menu_frame_grant", "expected_generation": generation}, -1); err != nil {
		return nil, err
	}
	granted, fds, err := receive(conn)
	if err != nil {
		return nil, err
	}
	if len(fds) != 1 {
		for _, fd := range fds {
			unix.Close(fd)
		}
		return nil, errors.New("menu grant did not return one descriptor")
	}
	defer unix.Close(fds[0])
	g := granted.MenuFrame
	if g == nil || g.Generation != generation || g.Format != Format || g.Slots != Slots || g.SlotBytes != SlotBytes {
		return nil, errors.New("invalid menu grant")
	}
	size := Slots * SlotBytes
	if b := g.Blitter; b != nil {
		if b.ListOffset != ListOffset || b.Commands != ListCommands || b.CommandBytes != CommandBytes ||
			b.HeapOffset != HeapOffset || b.HeapBytes != HeapBytes {
			return nil, errors.New("unsupported menu blitter layout")
		}
		size = BlitWindowBytes
	}
	mapping, err := unix.Mmap(fds[0], 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	c.window = &window{generation: generation, mapping: mapping}
	if g.Blitter != nil {
		c.window.heap = newBlitHeap()
		c.window.minor = g.Blitter.Minor
	}
	return c.window, nil
}

// write draws rects of frame into slot.
func (w *window) write(slot int, frame []byte, rects []Rect) error {
	return w.guard(func() {
		dst := w.mapping[slot*SlotBytes : slot*SlotBytes+FrameBytes]
		for _, r := range rects {
			writeRect(dst, frame, r)
		}
	})
}

// guard runs a write into the mapping. The runtime revokes the grant if a
// lifecycle operation outlasts the preparation; the fault that follows is
// returned as an error instead of ending the process.
func (w *window) guard(write func()) (err error) {
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if r := recover(); r != nil {
			if _, fault := r.(interface{ Addr() uintptr }); !fault {
				panic(r)
			}
			err = errors.New("menu window revoked while drawing")
		}
	}()
	write()
	return nil
}

// Grant maps generation's grant if it is not mapped yet and reports what it
// offers. Present and PresentBlit use the same mapping.
func (c *Client) Grant(ctx context.Context, generation uint64) (Grant, error) {
	if generation == 0 {
		return Grant{}, errors.New("invalid menu generation")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w, err := c.windowLocked(ctx, generation)
	if err != nil {
		return Grant{}, err
	}
	return Grant{Blitter: w.heap != nil, BlitterMinor: w.minor}, nil
}

func (c *Client) Present(ctx context.Context, generation uint64, pix []byte) (Result, error) {
	return c.present(ctx, generation, pix, nil)
}

// PresentDamage presents pix, a complete frame that differs from the frame
// the runtime is showing as damage.BaseSequence only inside damage.Rects.
// When the slot being drawn holds the frame before that one, only both
// frames' damage is written; otherwise the whole frame is.
func (c *Client) PresentDamage(ctx context.Context, generation uint64, pix []byte, damage Damage) (Result, error) {
	if !damage.valid() {
		return Result{}, errors.New("invalid menu damage")
	}
	return c.present(ctx, generation, pix, &damage)
}

func (c *Client) present(ctx context.Context, generation uint64, pix []byte, damage *Damage) (Result, error) {
	if generation == 0 || len(pix) != FrameBytes {
		return Result{}, errors.New("invalid menu frame")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var timing Timing
	began := time.Now()
	w, err := c.windowLocked(ctx, generation)
	if err != nil {
		return Result{}, err
	}
	conn, slot, err := c.begin(ctx, w, generation)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	staged := time.Now()
	timing.BeginUS = staged.Sub(began).Microseconds()
	rects := fullFrame
	if damage != nil && w.held[slot] != 0 && w.held[slot^1] == uint64(damage.BaseSequence) && w.held[slot]+1 == w.held[slot^1] {
		rects = slotUpdate(damage.Rects, w.damage)
	}
	// Until the commit completes, the slot holds no known frame.
	w.held[slot] = 0
	if err := w.write(slot, pix, rects); err != nil {
		w.held = [Slots]uint64{}
		return Result{}, err
	}
	committed := time.Now()
	timing.StageUS = committed.Sub(staged).Microseconds()
	timing.StagedPixels = int64(Damage{Rects: rects}.Pixels())
	commit := map[string]any{"protocol": 2, "operation": "menu_frame_commit", "generation": generation, "slot": slot}
	if len(rects) != 1 || rects[0] != fullFrame[0] {
		written := make([][4]int, len(rects))
		for i, r := range rects {
			written[i] = [4]int{r.X, r.Y, r.W, r.H}
		}
		commit["written"] = written
	}
	result, err := finishFrame(conn, commit, generation, slot, committed, &timing)
	if err != nil {
		return Result{}, err
	}
	w.held[slot] = result.DisplayedSequence
	w.damage = nil
	if damage != nil {
		w.damage = damage.Rects
	}
	return result, nil
}

// PresentBlit has the blitter draw frame into the slot scanout is not
// showing. It uploads the sources the heap does not hold, writes the list
// and commits it. A frame the blitter cannot hold returns ErrBlitTooLarge
// before any request. A blitter grant takes only lists after its first, so
// a client that uses it should send every later frame of the generation
// here.
func (c *Client) PresentBlit(ctx context.Context, generation uint64, frame BlitFrame) (Result, error) {
	if generation == 0 {
		return Result{}, errors.New("invalid menu frame")
	}
	if len(frame.Ops) == 0 {
		// An END would submit a slot holding the frame before last.
		return Result{}, errors.New("empty menu blit frame")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var timing Timing
	began := time.Now()
	w, err := c.windowLocked(ctx, generation)
	if err != nil {
		return Result{}, err
	}
	if w.heap == nil {
		return Result{}, errors.New("menu grant has no blitter")
	}
	if err := frame.check(w.minor); err != nil {
		return Result{}, err
	}
	placed, err := w.heap.place(frame)
	if err != nil {
		return Result{}, err
	}
	// Uploads are resident only once a commit has written them back.
	ok := false
	defer func() {
		if ok {
			w.heap.finish(placed)
		} else {
			w.heap.reset()
		}
	}()
	shared := w.prepared
	conn, slot, err := c.begin(ctx, w, generation)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	if w.prepared != shared+1 {
		// Another peer began a frame since this client's last and may have
		// written the heap. The frame fit, so it fits the empty heap.
		w.heap.reset()
		if placed, err = w.heap.place(frame); err != nil {
			return Result{}, err
		}
	}
	staged := time.Now()
	timing.BeginUS = staged.Sub(began).Microseconds()
	count := len(frame.Ops)
	var spans []Span
	if err := w.guard(func() {
		list := w.mapping[ListOffset : ListOffset+count*CommandBytes]
		spans = writeFrame(w.mapping[HeapOffset:BlitWindowBytes], list, frame, placed)
	}); err != nil {
		w.held = [Slots]uint64{}
		return Result{}, err
	}
	committed := time.Now()
	timing.StageUS = committed.Sub(staged).Microseconds()
	timing.Commands = int64(count)
	commit := map[string]any{"protocol": 2, "operation": "menu_frame_commit", "generation": generation, "slot": slot,
		"list": map[string]int{"first": 0, "count": count}}
	if len(spans) > 0 {
		uploaded := make([][2]int, len(spans))
		for i, s := range spans {
			uploaded[i] = [2]int{s.Offset, s.Bytes}
			timing.UploadBytes += int64(s.Bytes)
		}
		commit["uploaded"] = uploaded
	}
	// The slot holds a blitter frame, which CPU damage cannot build on.
	w.held = [Slots]uint64{}
	w.damage = nil
	result, err := finishFrame(conn, commit, generation, slot, committed, &timing)
	if err != nil {
		return Result{}, err
	}
	ok = true
	return result, nil
}

// begin opens a frame: it asks the runtime for the slot to draw, and
// records the preparation in w.
func (c *Client) begin(ctx context.Context, w *window, generation uint64) (*net.UnixConn, int, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, 0, err
	}
	if err := send(conn, map[string]any{"protocol": 2, "operation": "menu_frame_begin", "expected_generation": generation}, -1); err != nil {
		conn.Close()
		return nil, 0, err
	}
	prepared, fds, err := receive(conn)
	for _, fd := range fds {
		unix.Close(fd)
	}
	if err != nil {
		conn.Close()
		return nil, 0, err
	}
	p := prepared.MenuFrame
	if len(fds) != 0 || p == nil || p.Generation != generation || p.Slot == nil || *p.Slot < 0 || *p.Slot >= Slots || p.Preparation == 0 || p.DisplayedSequence != nil {
		conn.Close()
		return nil, 0, errors.New("invalid menu preparation")
	}
	w.prepared = p.Preparation
	return conn, *p.Slot, nil
}

// finishFrame sends a commit on the frame's connection and checks its completion.
func finishFrame(conn *net.UnixConn, commit map[string]any, generation uint64, slot int, committed time.Time, timing *Timing) (Result, error) {
	if err := send(conn, commit, -1); err != nil {
		return Result{}, err
	}
	completed, extra, err := receive(conn)
	for _, other := range extra {
		unix.Close(other)
	}
	if err != nil {
		return Result{}, err
	}
	f := completed.MenuFrame
	if len(extra) != 0 || f == nil || f.Generation != generation || f.Slot == nil || *f.Slot != slot || f.DisplayedSequence == nil || f.Underflows > TransientUnderflowCap {
		return Result{}, errors.New("invalid menu frame completion")
	}
	timing.CommitUS = time.Since(committed).Microseconds()
	if t := f.Timing; t != nil {
		timing.RuntimeCleanUS, timing.RuntimeBlitUS, timing.RuntimeSubmitUS, timing.RuntimeScanoutWaitUS = t.CleanUS, t.BlitUS, t.SubmitUS, t.ScanoutWaitUS
	}
	return Result{Generation: generation, DisplayedSequence: *f.DisplayedSequence, Underflows: f.Underflows, Timing: *timing}, nil
}
