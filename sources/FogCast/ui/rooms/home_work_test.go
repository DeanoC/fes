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
// resize, which restarts the staged picture (draw:resize). Ceilings are about
// 1.25x the worst case measured with gfx.segment replays in Home.
func TestHomeCallbackWorkCeiling(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		withRecent bool
	}{
		{name: "with-recents", withRecent: true},
		{name: "no-recents"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			homeCallbackWorkCeiling(t, fixture.withRecent)
		})
	}
}

func homeCallbackWorkCeiling(t *testing.T, withRecent bool) {
	// Measured after the segment replays (#1770); before them a focus move
	// ran 9083 instructions and recorded 354 ops.
	ceilings := map[string]struct {
		instructions uint64
		ops          int
	}{
		"main":        {2870, 0},   // 2778
		"load":        {1800, 0},   // 1549
		"async":       {760, 0},    // 617
		"update":      {700, 0},    // 470-547, by family answer order
		"draw:paint":  {3820, 100}, // 3097, 80
		"draw:focus":  {7130, 344}, // 3818, 133
		"draw:resize": {5060, 443}, // 934, 72
		"on_input":    {800, 0},    // 649
		"on_hover":    {790, 0},    // 639
		"on_activate": {480, 0},    // 389
		"on_resize":   {420, 0},    // 346
		"on_resume":   {880, 0},    // 797
	}
	// An unchanged frame keeps the presented picture: 47 instructions.
	const idleInstructions = 60

	storePath := filepath.Join(t.TempDir(), "home.state.json")
	w := &workMeter{}
	var r *Instance
	open := func() {
		options := homeWorkOptions(storePath)
		if !withRecent {
			options.Services.(*homeServices).setRecents()
		}
		r = newRoom(t, examplePack(t, "example.fes-cores"), options)
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
	if !withRecent {
		// Confirm this fixture exercises the six tall family tiles, not the
		// compact row used when recently played titles are present.
		frame := r.Step(time.Now())
		families := map[string]Hit{}
		for _, hit := range frame.Hits {
			if strings.HasPrefix(hit.ID, "recent:") {
				t.Fatalf("no-recents fixture has recent hit %q", hit.ID)
			}
			if strings.HasPrefix(hit.ID, "family:") {
				families[hit.ID] = hit
			}
		}
		if len(families) != 6 {
			t.Fatalf("no-recents fixture has %d family tiles, want 6: %+v", len(families), families)
		}
		for i := 1; i <= 6; i++ {
			hit, ok := families[fmt.Sprintf("family:%d", i)]
			if !ok || hit.H < 140 {
				t.Fatalf("no-recents tile %d is missing or not tall: %+v", i, hit)
			}
		}
		take()
		// Exercise every tall family tile using the same direction inputs as
		// the launcher, checking both focus and the work of each redraw.
		phase = "focus"
		r.Hover("family:1")
		r.Step(time.Now())
		take()
		for _, label := range []string{"ZX Spectrum", "Atari ST", "Commodore 64", "PC/XT", "PC 386"} {
			r.Input("right")
			r.Step(time.Now())
			take()
			if got := r.Destination().Label; got != label {
				t.Fatalf("tall tile focus = %q, want %q", got, label)
			}
		}
	}
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
		if !withRecent {
			// With no recent title, Home publishes the selected family's richer
			// destination when data settles or reload starts. These separate
			// instruction ceilings bound that path; all existing fixture
			// ceilings and every draw-op ceiling stay unchanged. Observed maxima
			// were 1196 (update, arrival-order dependent) and 989 (on_resume).
			switch kind {
			case "update":
				c.instructions = 1500
			case "on_resume":
				c.instructions = 1250
			}
		}
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
	"10285482ff1f2418", "b0eafd68538cee03", "2ae79d0aa615a35d", "2ae79d0aa615a35d",
	"c265350e9c92e864", "8ef62f97eb2dfab9", "8df5e5b37a165506", "dc968cd51e036ec5",
	"3988e36e2bbac26d", "6c33cf06318c7a75", "38f8ccc66a35a9de", "153e5cc6b70c3863",
	"3c32399c22889742", "9f630bfb4fac5f57", "55149c1bae015613", "8e70f5a15fe57e07",
	"72632a2a56e6a70a", "8e70f5a15fe57e07", "55149c1bae015613", "9f630bfb4fac5f57",
	"3c32399c22889742", "153e5cc6b70c3863", "38f8ccc66a35a9de", "6c33cf06318c7a75",
	"3988e36e2bbac26d", "dc968cd51e036ec5", "8df5e5b37a165506", "8ef62f97eb2dfab9",
	"c265350e9c92e864", "2ae79d0aa615a35d", "b0eafd68538cee03", "4843f9e5628e611c",
	"4843f9e5628e611c", "4843f9e5628e611c", "9096d8041f39f99d", "b0eafd68538cee03",
	"c265350e9c92e864", "8ef62f97eb2dfab9", "8df5e5b37a165506", "4142ba012274c34d",
	"8a5734fc5bce1e17", "c5c3a1e4975ed179", "4142ba012274c34d", "c265350e9c92e864",
	"dc968cd51e036ec5", "96b5e80c73fb0ca5", "8ecb81d06867a996", "6b511d11c5b0ff86",
	"8ecb81d06867a996", "392308fd9c1ac552", "a92cd4d9fabb6285", "a0bc045e70225548",
	"2039d045bf824319", "20aa471274d9b993", "d37bbc0f8c73accf", "590832583f518879",
	"4219931f7bc2e5fc", "b4086fe5712ee1dc", "2935edea693ba764",
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
