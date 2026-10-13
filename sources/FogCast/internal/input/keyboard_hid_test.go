package input

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type hidPost struct {
	id         string
	generation uint64
	rows       hidkeys.Rows
}

func newHIDPortsFixture(t *testing.T, ports bool) (*controllerPortsSink, *[]hidPost, *[]portWrite) {
	t.Helper()
	var mu sync.Mutex
	var posts []hidPost
	var writes []portWrite
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, id string, generation uint64, rows hidkeys.Rows) error {
		mu.Lock()
		posts = append(posts, hidPost{id, generation, rows})
		mu.Unlock()
		return nil
	})
	sink := &controllerPortsSink{fallback: &recordingSink{}, hid: hid, poster: func(_ context.Context, id string, generation uint64, port, buttons uint8, keypad uint16) error {
		mu.Lock()
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		mu.Unlock()
		return nil
	}}
	obs := CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "apple2", Generation: 7}}
	if ports {
		obs.Binding = &ControllerBinding{PackageID: "apple2", Generation: 7}
	}
	sink.setObservation(obs)
	if err := sink.bind(obs.Binding); err != nil {
		t.Fatal(err)
	}
	return sink, &posts, &writes
}

func TestHIDKeyboardFramesPostUsageRowsForTheGeneration(t *testing.T) {
	sink, posts, writes := newHIDPortsFixture(t, true)
	escape, _ := hidkeys.Code(0x29)
	shift, _ := hidkeys.Code(0xe1)
	a, _ := hidkeys.Code(0x04)
	for _, step := range []struct {
		source inputSource
		code   remoteinput.Code
		action remoteinput.Action
	}{
		{sourceLocal, shift, remoteinput.ActionPress},
		{sourceRemote, a, remoteinput.ActionPress},
		{sourceLocal, escape, remoteinput.ActionPress},
		{sourceLocal, escape, remoteinput.ActionPress}, // repeat of a held key posts nothing
		{sourceRemote, shift, remoteinput.ActionRelease},
	} {
		if err := sink.applyContext(context.Background(), step.source, keyboardFrameFor(step.code, step.action)); err != nil {
			t.Fatal(err)
		}
	}
	if len(*posts) != 3 || len(*writes) != 0 {
		t.Fatalf("posts=%+v writes=%+v", *posts, *writes)
	}
	last := (*posts)[2]
	if last.id != "apple2" || last.generation != 7 || last.rows[0] != 1<<4 || last.rows[2] != 1<<(0x29-32) || last.rows[8] != 1<<1 {
		t.Fatalf("rows %+v", last)
	}
	// Controller frames keep using set_controller beside the keyboard.
	if err := sink.applyContext(context.Background(), sourceRemote, gamepad(1, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].port != 1 || (*writes)[0].buttons != 1<<4 || (*writes)[0].keypad != 0 {
		t.Fatalf("controller %+v", *writes)
	}
	// Closing the kit socket releases only the local keys.
	if err := sink.releaseSource(sourceLocal); err != nil {
		t.Fatal(err)
	}
	after := (*posts)[len(*posts)-1]
	if after.rows[8] != 0 || after.rows[2] != 0 || after.rows[0] != 1<<4 {
		t.Fatalf("local release %+v", after)
	}
	if err := sink.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	neutral := (*posts)[len(*posts)-1]
	if neutral.rows != (hidkeys.Rows{}) || sink.hid.bound() {
		t.Fatalf("release all %+v bound=%v", neutral, sink.hid.bound())
	}
	count := len(*posts)
	if err := sink.ReleaseAll(); err != nil || len(*posts) != count {
		t.Fatal("clean release posted again")
	}
}

func TestTransientProbeKeepsHIDHoldUntilRelease(t *testing.T) {
	sink, posts, _ := newHIDPortsFixture(t, false)
	code, _ := hidkeys.Code(0x04)
	press := keyboardFrameFor(code, remoteinput.ActionPress)
	release := keyboardFrameFor(code, remoteinput.ActionRelease)
	if err := sink.applyContext(context.Background(), sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	if err := sink.invalidateObservationContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*posts) != 1 || !sink.keyboardHID || !sink.hid.bound() {
		t.Fatalf("failed probe dropped the hold: posts=%d hid=%v bound=%v", len(*posts), sink.keyboardHID, sink.hid.bound())
	}
	obs := CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "apple2", Generation: 7}}
	if err := sink.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	if err := sink.applyContext(context.Background(), sourceLocal, release); err != nil {
		t.Fatal(err)
	}
	if len(*posts) != 2 {
		t.Fatalf("posts %+v", *posts)
	}
	last := (*posts)[1]
	if last.id != "apple2" || last.generation != 7 || last.rows != (hidkeys.Rows{}) {
		t.Fatalf("release after recovered probe: %+v", last)
	}
}

func TestHIDObservationRebindsSameIdentityAfterReleaseAll(t *testing.T) {
	sink, posts, _ := newHIDPortsFixture(t, false)
	if err := sink.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	sink.setObservation(CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "apple2", Generation: 7}})
	code, _ := hidkeys.Code(0x04)
	if err := sink.Apply(keyboardFrameFor(code, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if !sink.hid.bound() || len(*posts) != 1 || (*posts)[0].rows[0] != 1<<4 {
		t.Fatalf("same identity after cleanup: bound=%v posts=%+v", sink.hid.bound(), *posts)
	}
}

func TestHIDKeyboardWithoutControllerPorts(t *testing.T) {
	sink, posts, _ := newHIDPortsFixture(t, false)
	backspace, _ := hidkeys.Code(0x2a)
	if err := sink.applyContext(context.Background(), sourceLocal, keyboardFrameFor(backspace, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if len(*posts) != 1 || (*posts)[0].rows[2] != 1<<(0x2a-32) {
		t.Fatalf("posts %+v", *posts)
	}
	// Legacy ZX81 codes reach an HID core as their physical keys.
	if err := sink.applyContext(context.Background(), sourceRemote, keyboardFrameFor(zx81keys.Letter('B'), remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if last := (*posts)[len(*posts)-1]; last.rows[0] != 1<<5 {
		t.Fatalf("legacy B rows %+v", last)
	}
}

func TestMatrixCoreStillReceivesZX81CodesFromHIDKitFrames(t *testing.T) {
	var matrices []uint64
	keys := NewKeyboardSink()
	keys.SetPoster(func(_ context.Context, matrix uint64) error { matrices = append(matrices, matrix); return nil })
	sink := &controllerPortsSink{fallback: &recordingSink{}, keys: keys, hid: &keyboardHIDSink{}}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true})
	a, _ := hidkeys.Code(0x04)
	if err := sink.applyContext(context.Background(), sourceLocal, keyboardFrameFor(a, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	want := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('A'): true})
	if len(matrices) != 1 || matrices[0] != want {
		t.Fatalf("matrix %x want %x", matrices, want)
	}
	escape, _ := hidkeys.Code(0x29)
	if err := sink.applyContext(context.Background(), sourceLocal, keyboardFrameFor(escape, remoteinput.ActionPress)); err == nil || len(matrices) != 1 {
		t.Fatalf("escape reached the ZX81 matrix: %v", err)
	}
}

func TestHIDPostFailureKeepsNeutralReleasePending(t *testing.T) {
	hid := &keyboardHIDSink{}
	calls := 0
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error {
		calls++
		if calls == 1 {
			return errors.New("lost")
		}
		return nil
	})
	hid.bind(&KeyboardHIDBinding{PackageID: "apple2", Generation: 3})
	code, _ := hidkeys.Code(0x04)
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(code, remoteinput.ActionPress)); err == nil {
		t.Fatal("post failure hidden")
	}
	hid.releaseAll()
	if calls != 2 {
		t.Fatalf("release after a failed post did not neutralize: %d", calls)
	}
}

// A retried press or release after a failed post reposts the state the
// runtime may not have applied; once confirmed, a duplicate posts nothing.
func TestHIDRetryAfterFailedPostRepostsState(t *testing.T) {
	hid := &keyboardHIDSink{}
	var posted []hidkeys.Rows
	fail := false
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
		posted = append(posted, rows)
		if fail {
			return errors.New("busy")
		}
		return nil
	})
	hid.bind(&KeyboardHIDBinding{PackageID: "apple2", Generation: 3})
	code, _ := hidkeys.Code(0x04)
	press := keyboardFrameFor(code, remoteinput.ActionPress)
	release := keyboardFrameFor(code, remoteinput.ActionRelease)
	held := hidkeys.Encode(map[uint8]bool{0x04: true})
	neutral := hidkeys.Encode(map[uint8]bool{})
	apply := func(f protocol.InputFrame) error { return hid.applyFrom(context.Background(), sourceRemote, f) }

	// Press fails, then its retry reposts the held state.
	fail = true
	if err := apply(press); err == nil {
		t.Fatal("press failure hidden")
	}
	fail = false
	if err := apply(press); err != nil || len(posted) != 2 || posted[1] != held {
		t.Fatalf("press retry: err=%v posts=%d", err, len(posted))
	}
	// Release fails, then its retry reposts neutral instead of succeeding silently.
	fail = true
	if err := apply(release); err == nil {
		t.Fatal("release failure hidden")
	}
	fail = false
	if err := apply(release); err != nil || len(posted) != 4 || posted[3] != neutral {
		t.Fatalf("release retry: err=%v posts=%d", err, len(posted))
	}
	// Confirmed: duplicates and an empty source release post nothing.
	if err := apply(release); err != nil || len(posted) != 4 {
		t.Fatalf("confirmed duplicate reposted: err=%v posts=%d", err, len(posted))
	}
	if err := hid.releaseSource(sourceRemote); err != nil || len(posted) != 4 {
		t.Fatalf("confirmed source release reposted: err=%v posts=%d", err, len(posted))
	}
	// A failed release is also repaired by the source's release.
	if err := apply(press); err != nil {
		t.Fatal(err)
	}
	fail = true
	_ = apply(release)
	fail = false
	if err := hid.releaseSource(sourceRemote); err != nil || len(posted) != 7 || posted[6] != neutral {
		t.Fatalf("source release after failure: err=%v posts=%d", err, len(posted))
	}
}

// newLocalHIDFeed is a kit-local controller whose runtime observation and
// set_keyboard_hid poster are supplied by the test.
func newLocalHIDFeed(t *testing.T, poster KeyboardHIDPoster, observe func(context.Context) (CoreObservation, error)) (*TargetController, *controllerPortsSink) {
	t.Helper()
	sink := &controllerPortsSink{hid: &keyboardHIDSink{}}
	sink.hid.setPoster(poster)
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.ObserveCore(observe)
	return controller, sink
}

func hidBinding(generation uint64) CoreObservation {
	return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "apple2", Generation: generation}}
}

// The release that discovers a failed status read is the only release the
// socket will deliver. It must post neutral for the generation still bound,
// and a press in that same window must not.
func TestLocalHIDReleaseSurvivesFailedProbe(t *testing.T) {
	var mu sync.Mutex
	var posts []hidPost
	observations := 0
	fail := false
	controller, sink := newLocalHIDFeed(t, func(_ context.Context, id string, generation uint64, rows hidkeys.Rows) error {
		mu.Lock()
		posts = append(posts, hidPost{id, generation, rows})
		mu.Unlock()
		return nil
	}, func(context.Context) (CoreObservation, error) {
		observations++
		if fail {
			return CoreObservation{}, errors.New("status unread")
		}
		return hidBinding(7), nil
	})
	a, _ := hidkeys.Code(0x04)
	b, _ := hidkeys.Code(0x05)
	pressA := keyboardFrameFor(a, remoteinput.ActionPress)
	pressB := keyboardFrameFor(b, remoteinput.ActionPress)
	releaseA := keyboardFrameFor(a, remoteinput.ActionRelease)
	if err := controller.deliverLocal(context.Background(), pressA); err != nil {
		t.Fatal(err)
	}
	fail = true
	expireLocalObservation(sink)
	if err := controller.deliverLocal(context.Background(), pressB); !errors.Is(err, errNoCore) {
		t.Fatalf("press during failed probe: %v", err)
	}
	probes := observations
	if err := controller.deliverLocal(context.Background(), releaseA); err != nil {
		t.Fatal(err)
	}
	if observations != probes {
		t.Fatalf("cached failure re-read status: observations %d want %d", observations, probes)
	}
	mu.Lock()
	got := append([]hidPost(nil), posts...)
	mu.Unlock()
	if len(got) != 2 || got[1].id != "apple2" || got[1].generation != 7 || got[1].rows != (hidkeys.Rows{}) {
		t.Fatalf("release consumed by the failed probe: %+v", got)
	}
	if controller.LocalInputDrops() != 1 {
		t.Fatalf("drops=%d, want the gated press only", controller.LocalInputDrops())
	}
	fail = false
	expireLocalObservation(sink)
	if err := controller.deliverLocal(context.Background(), pressB); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	last := posts[len(posts)-1]
	mu.Unlock()
	if last.generation != 7 || last.rows != (hidkeys.Rows{1 << 5}) {
		t.Fatalf("recovered generation restored A or dropped B: %+v", last)
	}
}

// A release post that does not land is sent again when the same generation
// is confirmed, without requiring another copy of the consumed release.
func TestLocalHIDUnconfirmedReleaseReplaysForSameGeneration(t *testing.T) {
	var mu sync.Mutex
	var posts []hidPost
	failPost := false
	failProbe := false
	controller, sink := newLocalHIDFeed(t, func(_ context.Context, id string, generation uint64, rows hidkeys.Rows) error {
		mu.Lock()
		posts = append(posts, hidPost{id, generation, rows})
		mu.Unlock()
		if failPost {
			return errors.New("hid post lost")
		}
		return nil
	}, func(context.Context) (CoreObservation, error) {
		if failProbe {
			return CoreObservation{}, errors.New("status unread")
		}
		return hidBinding(7), nil
	})
	a, _ := hidkeys.Code(0x04)
	if err := controller.deliverLocal(context.Background(), keyboardFrameFor(a, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	failPost, failProbe = true, true
	expireLocalObservation(sink)
	if err := controller.deliverLocal(context.Background(), keyboardFrameFor(a, remoteinput.ActionRelease)); err == nil {
		t.Fatal("lost release post reported success")
	}
	failPost, failProbe = false, false
	expireLocalObservation(sink)
	if err := controller.ensureLocalCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 3 || posts[2].generation != 7 || posts[2].rows != (hidkeys.Rows{}) {
		t.Fatalf("same generation did not replay the release: %+v", posts)
	}
}

// A confirmed replacement must not receive keys the failed probe still held,
// and a confirmed idle result must not post the release to the retired core.
func TestLocalHIDReleaseDoesNotReplayOntoReplacement(t *testing.T) {
	var mu sync.Mutex
	var posts []hidPost
	live := uint64(7)
	failProbe := false
	idle := false
	controller, sink := newLocalHIDFeed(t, func(_ context.Context, id string, generation uint64, rows hidkeys.Rows) error {
		mu.Lock()
		posts = append(posts, hidPost{id, generation, rows})
		rejected := generation != live
		mu.Unlock()
		if rejected {
			return errors.New("generation mismatch")
		}
		return nil
	}, func(context.Context) (CoreObservation, error) {
		if failProbe {
			return CoreObservation{}, errors.New("status unread")
		}
		if idle {
			return CoreObservation{}, nil
		}
		return hidBinding(live), nil
	})
	a, _ := hidkeys.Code(0x04)
	b, _ := hidkeys.Code(0x05)
	if err := controller.deliverLocal(context.Background(), keyboardFrameFor(a, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	live = 8
	failProbe = true
	expireLocalObservation(sink)
	if err := controller.deliverLocal(context.Background(), keyboardFrameFor(a, remoteinput.ActionRelease)); err == nil {
		t.Fatal("release posted to the replaced generation")
	}
	failProbe = false
	expireLocalObservation(sink)
	if err := controller.deliverLocal(context.Background(), keyboardFrameFor(b, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]hidPost(nil), posts...)
	mu.Unlock()
	var gen8 []hidPost
	for _, post := range got {
		if post.generation == 8 {
			gen8 = append(gen8, post)
		}
	}
	if len(gen8) != 1 || gen8[0].rows != (hidkeys.Rows{1 << 5}) {
		t.Fatalf("replacement replayed the old key: %+v", got)
	}

	idle = true
	expireLocalObservation(sink)
	before := len(got)
	if err := controller.deliverLocal(context.Background(), keyboardFrameFor(b, remoteinput.ActionRelease)); !errors.Is(err, errNoCore) {
		t.Fatalf("confirmed idle release: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != before {
		t.Fatalf("confirmed idle posted the retired release: %+v", posts[before:])
	}
}
