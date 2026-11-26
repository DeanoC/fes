//go:build linux

package tenfoot

import (
	"context"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// The production loop renders through FrameCache before MenuDisplay. Exercise
// that path so a trace attached to the wrong layer cannot pass this test.
func TestFrameTraceObservesProductionFrameCache(t *testing.T) {
	display, err := gfx.NewMenuDisplayWithPresenter(&recordingMenu{})
	if err != nil {
		t.Fatal(err)
	}
	defer display.Close()
	display.SetChangeDriven(true)
	cache := gfx.NewFrameCache(display)
	app := NewApp(nil, 1280, 720, 20)
	trace, stop := startFrameTrace(context.Background(), app, display, cache, "test")
	paint := func(color gfx.Color) {
		cache.BeginFrame()
		cache.Clear(gfx.RGB(0, 0, 0))
		cache.FillRect(gfx.Rect{W: 10, H: 10}, color)
		cache.Present()
	}
	paint(gfx.RGB(255, 0, 0)) // No previous complete scene.
	paint(gfx.RGB(255, 0, 0)) // Actual equal comparison.
	paint(gfx.RGB(0, 255, 0)) // Actual changed comparison.
	comparisons := func() []frameTraceRecord {
		var result []frameTraceRecord
		for _, record := range trace.trace.records() {
			if record.Kind == "list-compare" {
				result = append(result, record)
			}
		}
		return result
	}
	records := comparisons()
	if len(records) != 2 || records[0].Kind != "list-compare" || records[0].CompareResult != "equal" ||
		records[1].Kind != "list-compare" || records[1].CompareResult != "changed" ||
		records[0].CompareCommands == 0 || records[1].CompareCommands == 0 {
		t.Fatalf("actual FrameCache comparison trace: %+v", records)
	}
	stop()
	paint(gfx.RGB(0, 255, 0))
	if len(comparisons()) != 2 {
		t.Fatal("comparison trace sink was not detached")
	}
}
