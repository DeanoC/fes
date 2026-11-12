package rooms

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

// homeWorkOptions is a kit Home showing all cores: eight kit cores, their
// admitted computers, library titles and three recent titles.
func homeWorkOptions(storePath string) Options {
	ids := []string{"fes.zx81", "fes.spectrum", "fes.atari-st", "fes.c64", "fes.apple2", "fes.coleco", "fes.sms", "fes.pong"}
	var cores []localcores.Core
	var computers []hostclient.ShelfComputer
	var games []hostclient.Game
	for i, id := range ids {
		cores = append(cores, localcores.Core{PackageID: coreZXID, CoreID: id, Name: id, Needs: "none", Launchable: true})
		reason := ""
		if i%3 == 2 {
			reason = "Its library is offline"
		}
		computers = append(computers, computerEntry(id, id, reason))
		games = append(games, machineEntry(id, id, ""), titleEntry(id+"-a", id), titleEntry(id+"-b", id))
	}
	svc := &homeServices{
		fakeServices: fakeServices{games: games},
		computers:    computers,
		recents:      []hostclient.Game{titleEntry("lunar", "fes.spectrum"), titleEntry("bits", "fes.zx81"), titleEntry("disk", "fes.c64")},
	}
	return Options{Services: svc, Local: &fakeLocalCores{cores: cores}, StorePath: storePath, Width: 1280, Height: 720}
}

// homeSettledFully waits for settled data and picture, then lets any
// remaining family batches and their repaints finish.
func homeSettledFully(t testing.TB, r *Instance) {
	t.Helper()
	stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	for i := 0; i < 30; i++ {
		r.Step(time.Now())
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
}

// homeTour drives settled Homes through the callbacks that redraw them:
// focus moves across every tile and row, the header routes, pointer focus,
// a compact resize and back. Each callback goes to every room, then step
// runs.
func homeTour(rs []*Instance, step func(what string)) {
	each := func(what string, fn func(r *Instance)) {
		for _, r := range rs {
			fn(r)
		}
		step(what)
	}
	input := func(cmd string) { each("input "+cmd, func(r *Instance) { r.Input(cmd) }) }
	for i := 0; i < 3; i++ {
		input("right")
	}
	input("down")
	for i := 0; i < 12; i++ {
		input("right")
	}
	for i := 0; i < 12; i++ {
		input("left")
	}
	input("up")
	input("left")
	input("header")
	input("left")
	input("left")
	input("right")
	input("down")
	input("down")
	for _, id := range []string{"family:2", "family:3", "recent:2", "menu:1", "native:actions", "native:shelf", "family:1"} {
		each("hover "+id, func(r *Instance) { r.Hover(id) })
	}
	each("activate family:4", func(r *Instance) { r.Activate("family:4") })
	each("resize compact", func(r *Instance) { r.Resize(470, 350) })
	input("down")
	input("down")
	input("up")
	each("resize full", func(r *Instance) { r.Resize(1280, 720) })
	for i := 0; i < 6; i++ {
		step("settle")
	}
	input("right")
	input("up")
	input("right")
}

// TestHomeCallbackWorkCeiling holds Home's worst callbacks under fixed
// ceilings of Lua instructions and recorded draw ops (#1770). The counts do
// not depend on the machine, so this guards the kit's frame budget without
// timing anything. Every kind but update is exact on each run. The family
// answers arrive on goroutines in real time and update processes whichever
// became pending first, so the worst update varies with arrival order
// within a bound (470-547 instructions seen); its ceiling covers that
// bound. Fixing the order would need hooks in the family request code. Draws are split by what caused them: the staged first
// picture and data changes (draw:paint), focus moves (draw:focus) and a
// resize, which redraws everything at once (draw:resize). Ceilings are about
// 1.25x the worst case measured with gfx.segment replays in Home.
func TestHomeCallbackWorkCeiling(t *testing.T) {
	// Measured after the segment replays (#1770); before them a focus move
	// ran 9083 instructions and recorded 354 ops.
	ceilings := map[string]struct {
		instructions uint64
		ops          int
	}{
		"main":        {2870, 0},   // 2299
		"load":        {1800, 0},   // 1443
		"async":       {760, 0},    // 608
		"update":      {700, 0},    // 470-547, by family answer order
		"draw:paint":  {3820, 100}, // 3054, 80
		"draw:focus":  {7130, 344}, // 5721, 275
		"draw:resize": {5060, 443}, // 4088, 354
		"on_input":    {800, 0},    // 641
		"on_hover":    {790, 0},    // 631
		"on_activate": {480, 0},    // 381
		"on_resize":   {420, 0},    // 335
		"on_resume":   {880, 0},    // 701
	}
	// An unchanged frame keeps the presented picture: 47 instructions.
	const idleInstructions = 60

	storePath := filepath.Join(t.TempDir(), "home.state.json")
	w := &workMeter{}
	var r *Instance
	open := func() {
		r = newRoom(t, examplePack(t, "example.fes-cores"), homeWorkOptions(storePath))
		r.work = w
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
	}
	worst := map[string]callWork{}
	phase := "paint"
	take := func() {
		for _, c := range w.calls {
			kind := c.kind
			if kind == "draw" {
				kind += ":" + phase
			}
			m := worst[kind]
			m.instructions = max(m.instructions, c.instructions)
			m.ops = max(m.ops, c.ops)
			m.replayed = max(m.replayed, c.replayed)
			worst[kind] = m
		}
		w.calls = w.calls[:0]
	}
	settle := func() {
		phase = "paint"
		homeSettledFully(t, r)
		take()
	}
	idle := func(what string) {
		t.Helper()
		for i := 0; i < 60; i++ {
			r.Step(time.Now())
			for _, c := range w.calls {
				if c.kind == "draw" && (c.instructions > idleInstructions || c.ops != 0) {
					t.Fatalf("%s: unchanged frame %d ran %d instructions and recorded %d ops", what, i, c.instructions, c.ops)
				}
			}
			take()
		}
	}

	open()
	settle()
	idle("first paint")
	homeTour([]*Instance{r}, func(what string) {
		switch {
		case strings.HasPrefix(what, "resize"):
			phase = "resize"
		case what == "settle":
			phase = "paint"
		default:
			phase = "focus"
		}
		r.Step(time.Now())
		take()
	})
	settle()
	idle("tour")
	r.Close()
	take()
	open()
	settle()
	idle("reopen")
	r.Resume()
	take()
	settle()
	idle("resume")
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}

	kinds := make([]string, 0, len(worst))
	for kind := range worst {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	var report strings.Builder
	for _, kind := range kinds {
		m := worst[kind]
		fmt.Fprintf(&report, "%-12s %6d instructions %4d ops %4d replayed\n", kind, m.instructions, m.ops, m.replayed)
	}
	t.Logf("worst callbacks:\n%s", report.String())
	for _, kind := range kinds {
		m := worst[kind]
		c, ok := ceilings[kind]
		if !ok {
			t.Errorf("no ceiling for %s callbacks", kind)
			continue
		}
		if m.instructions > c.instructions || m.ops > c.ops {
			t.Errorf("%s: %d instructions, %d ops; ceiling %d, %d", kind, m.instructions, m.ops, c.instructions, c.ops)
		}
	}
}

// TestHomeSegmentReplayMatchesFullDraw draws the same Home tour twice, once
// replaying gfx.segment runs and once running every segment's function, and
// requires identical frames and hit regions after every callback.
func TestHomeSegmentReplayMatchesFullDraw(t *testing.T) {
	replay := loadHome(t, homeWorkOptions(""))
	full := loadHome(t, homeWorkOptions(""))
	full.seg.noReplay = true
	homeSettledFully(t, replay)
	homeSettledFully(t, full)
	replayed := 0
	compare := func(what string) {
		t.Helper()
		a, b := replay.Step(time.Now()), full.Step(time.Now())
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%s: replayed frame differs from the full draw (%d/%d ops, %d/%d hits)", what, len(a.Ops), len(b.Ops), len(a.Hits), len(b.Hits))
		}
		if replay.Destination().Label != full.Destination().Label {
			t.Fatalf("%s: destinations %q and %q", what, replay.Destination().Label, full.Destination().Label)
		}
		replayed += replay.seg.replayed
		if full.seg.replayed != 0 {
			t.Fatalf("%s: full draw replayed %d ops", what, full.seg.replayed)
		}
	}
	compare("settled")
	homeTour([]*Instance{replay, full}, compare)
	if replayed == 0 {
		t.Fatal("the tour never replayed a segment")
	}
	for _, r := range []*Instance{replay, full} {
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

// BenchmarkHomeFocusChange is one focus move on a settled Home and the draw
// it causes, the routine callback pair the kit found slowest (#1770).
func BenchmarkHomeFocusChange(b *testing.B) {
	r := newRoom(b, examplePack(b, "example.fes-cores"), homeWorkOptions(""))
	if err := r.Load(); err != nil {
		b.Fatal(err)
	}
	homeSettledFully(b, r)
	r.Input("down")
	r.Step(time.Now())
	cmds := []string{"right", "right", "right", "left", "left", "left"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Input(cmds[i%len(cmds)])
		r.Step(time.Now())
	}
	b.StopTimer()
	if err := r.Err(); err != nil {
		b.Fatal(err)
	}
}

// homeTourFrames is the sha256 (first 16 hex digits) of each frame and
// destination the Home tour gives, recorded from the Home script before
// #1770 drew keyboards, moons and hills from cached gfx.rects lists,
// remembered gfx.measure results and copied segments. The cheaper script
// must paint the same pixels and hit regions. A deliberate visual change to
// Home records them again from this test's failure output.
var homeTourFrames = []string{
	"4f0c06e40a1b2d47", "8d94681b59611559", "038def75933ffbb4", "038def75933ffbb4",
	"1edd9f1fb5c2a1ff", "576e3ed68a81f2f6", "0577bf99f6f2f513", "65d7c78c0164398d",
	"3fe70f906c5116d4", "7cf48f193939a22a", "aaa50709bc7a242f", "e5858b042ba13bc4",
	"37668531d81dc5aa", "9c5469c036f0fa61", "f71922d3f00c03d8", "f71922d3f00c03d8",
	"f71922d3f00c03d8", "9c5469c036f0fa61", "37668531d81dc5aa", "e5858b042ba13bc4",
	"aaa50709bc7a242f", "7cf48f193939a22a", "3fe70f906c5116d4", "65d7c78c0164398d",
	"0577bf99f6f2f513", "576e3ed68a81f2f6", "1edd9f1fb5c2a1ff", "1edd9f1fb5c2a1ff",
	"1edd9f1fb5c2a1ff", "038def75933ffbb4", "8d94681b59611559", "97f57277d57cc897",
	"97f57277d57cc897", "97f57277d57cc897", "cb719c7b3897fc45", "8d94681b59611559",
	"1edd9f1fb5c2a1ff", "576e3ed68a81f2f6", "0577bf99f6f2f513", "b66694787ea48943",
	"8b650fa075063de8", "659a5fd8a967b30c", "b66694787ea48943", "1edd9f1fb5c2a1ff",
	"65d7c78c0164398d", "a2d308b6e73d513e", "96dbe0bbe43e4c3d", "b10990f7d18bb3eb",
	"96dbe0bbe43e4c3d", "3fe70f906c5116d4", "3fe70f906c5116d4", "3fe70f906c5116d4",
	"3fe70f906c5116d4", "3fe70f906c5116d4", "3fe70f906c5116d4", "3fe70f906c5116d4",
	"7cf48f193939a22a", "a4ed722a5f274bea", "193c8e7aad3c7a56",
}

// TestHomeTourFramesMatchRecorded holds Home's settled picture and every
// picture of the tour to the recorded hashes.
func TestHomeTourFramesMatchRecorded(t *testing.T) {
	r := loadHome(t, homeWorkOptions(""))
	homeSettledFully(t, r)
	var got []string
	record := func(what string) {
		f := r.Step(time.Now())
		b, err := json.Marshal(struct {
			Frame       Frame
			Destination Destination
		}{f, r.Destination()})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		got = append(got, hex.EncodeToString(sum[:8]))
	}
	record("settled")
	homeTour([]*Instance{r}, record)
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, homeTourFrames) {
		first := 0
		for first < len(got) && first < len(homeTourFrames) && got[first] == homeTourFrames[first] {
			first++
		}
		t.Fatalf("Home tour frames differ from the recorded ones from picture %d (%d pictures, %d recorded); got\n%#v", first, len(got), len(homeTourFrames), got)
	}
}
