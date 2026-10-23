package tenfoot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// frameTraceCap bounds the ring. At the 30 Hz loop it holds several
// minutes of ticks plus their inputs and presents.
const frameTraceCap = 16384

// frameTraceRecord is one line of a frame-trace dump. Kind selects the
// fields that apply: "tick" for one loop iteration, "input" for a command,
// "present" for a menu frame the display worker handled. Times are
// microseconds; T is wall-clock Unix microseconds.
type frameTraceRecord struct {
	T    int64  `json:"t_us"`
	Kind string `json:"kind"`

	// tick
	PollUS      int64  `json:"poll_us,omitempty"`
	TickUS      int64  `json:"tick_us,omitempty"`
	LuaAsyncUS  int64  `json:"lua_async_us,omitempty"`
	LuaUpdateUS int64  `json:"lua_update_us,omitempty"`
	LuaDrawUS   int64  `json:"lua_draw_us,omitempty"`
	LuaStepUS   int64  `json:"lua_step_us,omitempty"`
	SnapshotUS  int64  `json:"snapshot_us,omitempty"`
	RenderUS    int64  `json:"render_us,omitempty"`
	Queued      bool   `json:"queued,omitempty"`
	View        string `json:"view,omitempty"`

	// input
	Cmd string `json:"cmd,omitempty"`

	// present (Seq also marks the tick that queued it)
	Seq         uint64 `json:"seq,omitempty"`
	QueuedT     int64  `json:"queued_t_us,omitempty"`
	FrameCopyUS int64  `json:"frame_copy_us,omitempty"`
	QueueWaitUS int64  `json:"queue_wait_us,omitempty"`
	StatusUS    int64  `json:"status_us,omitempty"`
	PresentUS   int64  `json:"present_us,omitempty"`
	BeginUS     int64  `json:"begin_us,omitempty"`
	StageUS     int64  `json:"stage_us,omitempty"`
	CommitUS    int64  `json:"commit_us,omitempty"`
	RTCleanUS   int64  `json:"runtime_clean_us,omitempty"`
	RTBlitUS    int64  `json:"runtime_blit_us,omitempty"`
	RTSubmitUS  int64  `json:"runtime_submit_us,omitempty"`
	RTWaitUS    int64  `json:"runtime_scanout_wait_us,omitempty"`
	// Pixels the client wrote into the granted slot, and the shortcuts the
	// worker took (docs/frame-trace.md).
	StagedPixels int64 `json:"staged_pixels,omitempty"`
	// A frame the blitter drew: its commands and the heap bytes uploaded.
	Blitted       bool   `json:"blitted,omitempty"`
	Commands      int64  `json:"commands,omitempty"`
	UploadBytes   int64  `json:"upload_bytes,omitempty"`
	DamageRects   int    `json:"damage_rects,omitempty"`
	StatusSkipped bool   `json:"status_skipped,omitempty"`
	Retried       bool   `json:"retried,omitempty"`
	Presented     bool   `json:"presented,omitempty"`
	Err           string `json:"err,omitempty"`
}

// frameTrace is an always-on ring of recent frame timings for the kit
// display loop. Recording is a few clock reads per tick; nothing is written
// until a dump is requested (SIGUSR1), so it needs no configuration change
// on a managed kit and no budget or behaviour depends on it.
type frameTrace struct {
	mu    sync.Mutex
	ring  []frameTraceRecord
	next  int
	full  bool
	dumps int
}

func newFrameTrace() *frameTrace {
	return &frameTrace{ring: make([]frameTraceRecord, frameTraceCap)}
}

func (f *frameTrace) add(r frameTraceRecord) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.ring[f.next] = r
	f.next++
	if f.next == len(f.ring) {
		f.next = 0
		f.full = true
	}
	f.mu.Unlock()
}

func (f *frameTrace) input(cmd Command, now time.Time) {
	f.add(frameTraceRecord{T: now.UnixMicro(), Kind: "input", Cmd: cmd.String()})
}

// present is the gfx.MenuDisplay trace sink.
func (f *frameTrace) present(p gfx.MenuPresentTrace) {
	f.add(frameTraceRecord{
		T: time.Now().UnixMicro(), Kind: "present", Seq: p.Seq, QueuedT: p.Queued.UnixMicro(),
		FrameCopyUS: p.FrameCopyUS, QueueWaitUS: p.QueueWaitUS, StatusUS: p.StatusUS, PresentUS: p.PresentUS,
		BeginUS: p.Timing.BeginUS, StageUS: p.Timing.StageUS, CommitUS: p.Timing.CommitUS,
		RTCleanUS: p.Timing.RuntimeCleanUS, RTBlitUS: p.Timing.RuntimeBlitUS, RTSubmitUS: p.Timing.RuntimeSubmitUS,
		RTWaitUS: p.Timing.RuntimeScanoutWaitUS, StagedPixels: p.Timing.StagedPixels,
		Blitted: p.Blitted, Commands: p.Timing.Commands, UploadBytes: p.Timing.UploadBytes,
		DamageRects: p.DamageRects, StatusSkipped: p.StatusSkipped, Retried: p.Retried,
		Presented: p.Presented, Err: p.Err,
	})
}

func (f *frameTrace) records() []frameTraceRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshotLocked()
}

func (f *frameTrace) snapshotLocked() []frameTraceRecord {
	if !f.full {
		return append([]frameTraceRecord(nil), f.ring[:f.next]...)
	}
	return append(append([]frameTraceRecord(nil), f.ring[f.next:]...), f.ring[:f.next]...)
}

// dump writes the ring as JSON Lines to
// dir/fogcast-frame-trace-<pid>-<n>-<utc>.jsonl and returns the path.
// Each call uses a new file, so earlier dumps stay on disk. The ring is kept.
func (f *frameTrace) dump(dir string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	f.mu.Lock()
	f.dumps++
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	path := filepath.Join(dir, fmt.Sprintf("fogcast-frame-trace-%d-%d-%s.jsonl", os.Getpid(), f.dumps, stamp))
	recs := f.snapshotLocked()
	f.mu.Unlock()
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(file)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			_ = file.Close()
			return "", err
		}
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

// traceInputLocked records a command when the kit loop traces frames.
func (a *App) traceInputLocked(cmd Command, now time.Time) {
	if a.frameTrace != nil {
		a.frameTrace.input(cmd, now)
	}
}

// frameLuaTiming is the room Step that ran during the current Tick.
// Tick clears it, and a Tick that does not step the room leaves it zero.
func (a *App) frameLuaTiming() (async, update, draw, total time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.frameLua
	return s.Async, s.Update, s.Draw, s.Total
}

// traceView names the frame presentFrame drew from snap.
func traceView(snap Snapshot) string {
	if snap.Attract.Active {
		return "attract"
	}
	if snap.GPUParked {
		return "now-playing"
	}
	if snap.Room.Open && snap.Room.ID != "" {
		return "room:" + snap.Room.ID
	}
	return "library"
}
