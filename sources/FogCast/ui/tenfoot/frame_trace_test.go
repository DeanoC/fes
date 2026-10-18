package tenfoot

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestFrameTraceRingWrapsAndDumpsJSONLines(t *testing.T) {
	f := newFrameTrace()
	start := time.Unix(100, 0)
	for i := 0; i < frameTraceCap+5; i++ {
		f.add(frameTraceRecord{T: start.Add(time.Duration(i) * time.Millisecond).UnixMicro(), Kind: "tick", TickUS: int64(i)})
	}
	f.input(CmdSelect, start)
	f.present(gfx.MenuPresentTrace{Seq: 9, Queued: start, Presented: true, PresentUS: 70000,
		Timing: menudisplay.Timing{RuntimeCopyUS: 41000, RuntimeScanoutWaitUS: 9000}})
	path, err := f.dump(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines []frameTraceRecord
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		var r frameTraceRecord
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, r)
	}
	if len(lines) != frameTraceCap {
		t.Fatalf("dumped %d records, want the ring's %d", len(lines), frameTraceCap)
	}
	// Oldest first: the first 7 ticks were overwritten (5 extra ticks + input + present).
	if lines[0].TickUS != 7 {
		t.Fatalf("oldest record %+v", lines[0])
	}
	last, input := lines[len(lines)-1], lines[len(lines)-2]
	if input.Kind != "input" || input.Cmd != CmdSelect.String() {
		t.Fatalf("input record %+v", input)
	}
	if last.Kind != "present" || last.Seq != 9 || last.RTCopyUS != 41000 || last.RTWaitUS != 9000 || !last.Presented {
		t.Fatalf("present record %+v", last)
	}
}

func TestFrameTraceDumpsDoNotOverwrite(t *testing.T) {
	f := newFrameTrace()
	dir := t.TempDir()
	first, err := f.dump(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.add(frameTraceRecord{T: 1, Kind: "tick", View: "library"})
	second, err := f.dump(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("second dump reused %s", first)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("dump files = %d, want 2", len(entries))
	}
	kept, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 0 {
		t.Fatalf("first dump changed after the second write: %q", kept)
	}
}

func TestTraceViewFollowsDrawnFrame(t *testing.T) {
	room := Snapshot{Room: RoomSnapshot{Open: true, ID: "example.fes-cores"}}
	cases := []struct {
		name string
		snap Snapshot
		want string
	}{
		{"attract over a room", Snapshot{Attract: AttractSnapshot{Active: true}, Room: room.Room}, "attract"},
		{"now playing", Snapshot{GPUParked: true, Room: room.Room}, "now-playing"},
		{"open room", room, "room:example.fes-cores"},
		{"library", Snapshot{}, "library"},
	}
	for _, tc := range cases {
		if got := traceView(tc.snap); got != tc.want {
			t.Fatalf("%s view = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTickClearsLuaTimingWhenRoomDoesNotStep(t *testing.T) {
	app := NewApp(nil, 320, 240, 4)
	app.mu.Lock()
	app.frameLua = rooms.StepTiming{Async: time.Millisecond, Update: 2 * time.Millisecond, Draw: 5 * time.Millisecond, Total: 8 * time.Millisecond}
	app.attractActive = true
	app.mu.Unlock()
	app.Tick(time.Now())
	async, update, draw, total := app.frameLuaTiming()
	if async != 0 || update != 0 || draw != 0 || total != 0 {
		t.Fatalf("unstepped tick kept lua async=%s update=%s draw=%s total=%s", async, update, draw, total)
	}
	if got := traceView(app.Snapshot()); got != "attract" {
		t.Fatalf("view = %q, want attract", got)
	}
}
