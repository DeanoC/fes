package input

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func chordFrame(key uint16) protocol.InputFrame {
	f := tapFrame(key, 7)
	f.Header.Type = protocol.InputTypeTapChord
	f.Reserved = 2
	return f
}

func pausedChord(t *testing.T, hid *keyboardHIDSink, key uint16) (chan struct{}, <-chan error) {
	t.Helper()
	started, resume, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { done <- hid.tap(context.Background(), chordFrame(key), func() { close(started); <-resume }) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("chord did not start")
	}
	return resume, done
}

func awaitChord(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("chord did not finish")
		return nil
	}
}

func TestChordFocusRetiresKeyAndModifiers(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	posts := make(chan hidkeys.Rows, 16)
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { posts <- rows; return nil })
	controller := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	resume, done := pausedChord(t, ports.hid, 4)
	if got := <-posts; got[0] != 1<<4 || got[8] != 2 {
		t.Fatalf("chord press: %v", got)
	}
	finish, err := controller.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	close(resume)
	if err := awaitChord(t, done); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("retired chord: %v", err)
	}
	finish, err = controller.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	for len(posts) > 0 {
		<-posts
	}
	key, _ := hidkeys.Code(5)
	if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(key, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if got := <-posts; got[0] != 1<<5 || got[8] != 0 {
		t.Fatalf("retired chord leaked: %v", got)
	}
}

func TestChordDisconnectPreservesLocalModifier(t *testing.T) {
	posts := make(chan hidkeys.Rows, 16)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { posts <- rows; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	shift, _ := hidkeys.Code(0xe1)
	if err := hid.applyFrom(context.Background(), sourceLocal, keyboardFrameFor(shift, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	<-posts
	resume, done := pausedChord(t, hid, 4)
	if got := <-posts; got[0] != 1<<4 || got[8] != 2 {
		t.Fatalf("chord press: %v", got)
	}
	if err := hid.releaseSource(sourceRemote); err != nil {
		t.Fatal(err)
	}
	if got := <-posts; got[0] != 0 || got[8] != 2 {
		t.Fatalf("disconnect released local shift: %v", got)
	}
	close(resume)
	if err := awaitChord(t, done); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("retired chord: %v", err)
	}
	select {
	case got := <-posts:
		t.Fatalf("retired chord posted again: %v", got)
	default:
	}
}

func TestChordPreservesRemoteModifierPressedDuringTap(t *testing.T) {
	posts := make(chan hidkeys.Rows, 16)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { posts <- rows; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	resume, done := pausedChord(t, hid, 4)
	<-posts
	shift, _ := hidkeys.Code(0xe1)
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(shift, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	<-posts
	close(resume)
	if err := awaitChord(t, done); err != nil {
		t.Fatal(err)
	}
	if got := <-posts; got[0] != 0 || got[8] != 2 {
		t.Fatalf("remote physical hold lost: %v", got)
	}
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(shift, remoteinput.ActionRelease)); err != nil {
		t.Fatal(err)
	}
	if got := <-posts; got != (hidkeys.Rows{}) {
		t.Fatalf("shift release: %v", got)
	}
}

func TestChordRejectsOverlappingModifierWithoutChangingFirst(t *testing.T) {
	posts := make(chan hidkeys.Rows, 16)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { posts <- rows; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	resume, done := pausedChord(t, hid, 4)
	<-posts
	if err := hid.tap(context.Background(), chordFrame(5), nil); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("overlapping chord: %v", err)
	}
	select {
	case got := <-posts:
		t.Fatalf("overlapping chord posted: %v", got)
	default:
	}
	close(resume)
	if err := awaitChord(t, done); err != nil {
		t.Fatal(err)
	}
	if got := <-posts; got != (hidkeys.Rows{}) {
		t.Fatalf("first chord did not release: %v", got)
	}
}

func TestChordRespectsFocusSuppressedRemoteModifier(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	posts := make(chan hidkeys.Rows, 16)
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { posts <- rows; return nil })
	controller := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	finish, err := controller.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	shift, _ := hidkeys.Code(0xe1)
	if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(shift, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	finish, err = controller.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	for len(posts) > 0 {
		<-posts
	}
	if err := ports.hid.tap(context.Background(), chordFrame(4), nil); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("suppressed modifier accepted: %v", err)
	}
	select {
	case got := <-posts:
		t.Fatalf("suppressed chord posted: %v", got)
	default:
	}
	if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(shift, remoteinput.ActionRelease)); err != nil {
		t.Fatal(err)
	}
	if err := ports.hid.tap(context.Background(), chordFrame(4), nil); err != nil {
		t.Fatalf("fresh chord refused after release: %v", err)
	}
}

func TestChordRejectsModifierTapInFlight(t *testing.T) {
	posts := make(chan hidkeys.Rows, 16)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { posts <- rows; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	started, resume, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { done <- hid.tap(context.Background(), tapFrame(0xe1, 7), func() { close(started); <-resume }) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("modifier tap did not start")
	}
	<-posts
	if err := hid.tap(context.Background(), chordFrame(4), nil); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("chord overlapped modifier tap: %v", err)
	}
	select {
	case got := <-posts:
		t.Fatalf("overlapping chord posted: %v", got)
	default:
	}
	close(resume)
	if err := awaitChord(t, done); err != nil {
		t.Fatal(err)
	}
	if got := <-posts; got != (hidkeys.Rows{}) {
		t.Fatalf("modifier tap did not release: %v", got)
	}
}

func TestRetiredChordCannotReleaseReplacementWithSameBinding(t *testing.T) {
	posts := make(chan hidkeys.Rows, 16)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { posts <- rows; return nil })
	binding := &KeyboardHIDBinding{PackageID: "p", Generation: 7}
	hid.bind(binding)
	oldResume, oldDone := pausedChord(t, hid, 4)
	<-posts
	hid.bind(nil)
	hid.bind(binding)
	newResume, newDone := pausedChord(t, hid, 4)
	<-posts
	close(oldResume)
	if err := awaitChord(t, oldDone); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("old chord: %v", err)
	}
	select {
	case got := <-posts:
		t.Fatalf("old chord changed replacement: %v", got)
	default:
	}
	close(newResume)
	if err := awaitChord(t, newDone); err != nil {
		t.Fatalf("replacement chord: %v", err)
	}
	if got := <-posts; got != (hidkeys.Rows{}) {
		t.Fatalf("replacement did not release: %v", got)
	}
}
