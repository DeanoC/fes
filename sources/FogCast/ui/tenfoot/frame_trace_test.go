package tenfoot

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
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
