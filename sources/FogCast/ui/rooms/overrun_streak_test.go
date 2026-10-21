package rooms

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func softDraw(elapsed time.Duration) error {
	return fmt.Errorf("draw callback: %w", softOverrun{budget: 12 * time.Millisecond, timing: callTiming{elapsed: elapsed}})
}

// The baseline is the sample taken before the callback. A cycle that runs
// inside that callback still shows up at retirement; sampling afterwards
// would report zero.
func TestStreakBaselineIncludesGCInsideTheFirstCallback(t *testing.T) {
	r := newRoom(t, memPack(t, "gc-base", "function draw() end", nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	var cycles uint64
	r.frameStreak.sample = func() uint64 { return cycles }
	r.noteFrameBaseline()
	cycles = 1
	r.frameStreak.add(r, softDraw(20*time.Millisecond), 1)
	for i := 2; i <= maxConsecutiveOverruns; i++ {
		r.frameStreak.add(r, softDraw(time.Duration(20+i)*time.Millisecond), i)
	}
	msg := r.frameStreak.summary(r)
	if !strings.Contains(msg, "1 process-wide GC cycles") {
		t.Fatalf("first-callback GC was dropped: %s", msg)
	}
}

// Frame and input streaks sample independently. A cycle during one callback
// must not move the other streak's baseline.
func TestFrameAndInputStreaksKeepSeparateGCBaselines(t *testing.T) {
	r := newRoom(t, memPack(t, "gc-split", "function draw() end", nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	var frameN, inputN uint64
	r.frameStreak.sample = func() uint64 { return frameN }
	r.inputStreak.sample = func() uint64 { return inputN }

	r.noteFrameBaseline()
	frameN = 4
	r.frameStreak.add(r, softDraw(20*time.Millisecond), 1)
	frameMsg := r.frameStreak.summary(r)

	r.noteInputBaseline()
	inputN = 2
	r.inputStreak.add(r, fmt.Errorf("on_input callback: %w", softOverrun{budget: 8 * time.Millisecond, timing: callTiming{elapsed: 15 * time.Millisecond}}), 1)
	inputMsg := r.inputStreak.summary(r)

	if !strings.Contains(frameMsg, "4 process-wide GC cycles") {
		t.Fatalf("frame baseline: %s", frameMsg)
	}
	if !strings.Contains(inputMsg, "2 process-wide GC cycles") {
		t.Fatalf("input baseline: %s", inputMsg)
	}
	if again := r.frameStreak.summary(r); again != frameMsg {
		t.Fatalf("input sampling changed the frame streak: %s vs %s", again, frameMsg)
	}
}

func TestRetirementBeforeCoverDispatchReportsNoImages(t *testing.T) {
	r := newRoom(t, memPack(t, "cover-early", "function load() image.cover('game-a') end\nfunction draw() end", nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if n := r.imagesLoading(); n != 0 {
		t.Fatalf("unrequested cover counted as loading: %d", n)
	}
	for i := 0; i < maxConsecutiveOverruns; i++ {
		r.recordFrameOverrun(softDraw(time.Duration(13+i) * time.Millisecond))
	}
	msg := r.Err().Error()
	if !strings.Contains(msg, "up to 0 images loading") {
		t.Fatalf("retirement blamed an unrequested cover: %s", msg)
	}
	ids := r.TakeCoverRequests()
	if len(ids) != 1 || ids[0] != "game-a" {
		t.Fatalf("cover was dispatched before retirement: %v", ids)
	}
}

func TestRequestedCoverCountsAsLoading(t *testing.T) {
	r := newRoom(t, memPack(t, "cover-sent", "function load() image.cover('game-a') end\nfunction draw() end", nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	ids := r.TakeCoverRequests()
	if len(ids) != 1 || ids[0] != "game-a" {
		t.Fatalf("requests %v", ids)
	}
	if n := r.imagesLoading(); n != 1 {
		t.Fatalf("requested cover not loading: %d", n)
	}
	if again := r.TakeCoverRequests(); len(again) != 0 {
		t.Fatalf("cover requested twice: %v", again)
	}
}

// Step and Input sample GC before the callback that starts the streak, so a
// cycle inside that callback is in the retirement message.
func TestEngineSamplesGCBeforeTheOverrunningCallback(t *testing.T) {
	const (
		budget = 100 * time.Millisecond
		pause  = 150 * time.Millisecond
	)
	t.Run("draw", func(t *testing.T) {
		assertCallbackGC(t, budget, pause, true)
	})
	t.Run("input", func(t *testing.T) {
		assertCallbackGC(t, budget, pause, false)
	})
}

func assertCallbackGC(t *testing.T, budget, pause time.Duration, frame bool) {
	t.Helper()
	script := "function draw() slow() end"
	if !frame {
		script = "function on_input() slow() return true end\nfunction draw() end"
	}
	r := newRoom(t, memPack(t, "gc-live", script, nil), Options{Budget: Budget{
		Load: time.Second, Frame: budget, Input: budget,
	}})
	var cycles atomic.Uint64
	var once sync.Once
	var seq []string
	sample := func() uint64 {
		seq = append(seq, "sample")
		return cycles.Load()
	}
	if frame {
		r.frameStreak.sample = sample
	} else {
		r.inputStreak.sample = sample
	}
	r.L.SetGlobal("slow", r.L.NewFunction(func(L *lua.LState) int {
		seq = append(seq, "callback")
		once.Do(func() { cycles.Add(1) })
		time.Sleep(pause)
		return 0
	}))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000, 0)
	for i := 0; i < maxConsecutiveOverruns; i++ {
		now = now.Add(20 * time.Millisecond)
		if frame {
			r.Step(now)
		} else if !r.Input("a") && r.Err() == nil {
			t.Fatal("on_input was not consumed")
		}
		if r.Err() != nil {
			break
		}
	}
	err := r.Err()
	if err == nil {
		t.Fatal("soft overruns did not retire the room")
	}
	msg := err.Error()
	if !strings.Contains(msg, "1 process-wide GC cycles") {
		t.Fatalf("retirement missed the in-callback GC: %s", msg)
	}
	if len(seq) < 2 || seq[0] != "sample" || seq[1] != "callback" {
		t.Fatalf("GC was not sampled before the callback: %v", seq)
	}
}

func TestConcurrentRoomsSampleGCWithoutRacing(t *testing.T) {
	const n = 8
	opened := make([]*Instance, n)
	for i := range opened {
		opened[i] = newRoom(t, memPack(t, fmt.Sprintf("race-%d", i), "function draw() end", nil), Options{})
		if err := opened[i].Load(); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errc := make(chan error, n)
	for _, r := range opened {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < maxConsecutiveOverruns; i++ {
				r.noteFrameBaseline()
				r.recordFrameOverrun(softDraw(20 * time.Millisecond))
				_ = gcCycles()
			}
			if r.Err() == nil {
				errc <- fmt.Errorf("room did not retire")
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Error(err)
	}
}
