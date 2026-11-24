package input

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestLocalPadMouseCannotReplayUncertainV2Keyboard(t *testing.T) {
	f := newV2BackendFixture(t)
	var padPosts, mousePosts, keyPosts int
	f.c.ports.poster = func(context.Context, string, uint64, uint8, uint8, uint16) error { padPosts++; return nil }
	if err := f.c.ports.bind(&ControllerBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	f.c.ports.mouse = &mouseSink{poster: func(context.Context, string, uint64, int16, int16, uint8) error { mousePosts++; return nil }}
	if err := f.c.ports.mouse.bindContext(context.Background(), &MouseBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	hid := f.c.ports.hid
	backend := delegatedV2Backend{f.c}
	witness, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	fail := true
	hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error {
		keyPosts++
		if fail {
			return errors.New("ambiguous input")
		}
		return nil
	})
	if err := backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("ambiguous keyboard edge succeeded")
	}
	fail = false
	pad := protocol.InputFrame{Device: uint8(remoteinput.DeviceGamepad), Kind: uint8(remoteinput.KindButton), Action: uint8(remoteinput.ActionPress), Code: uint16(remoteinput.ButtonA)}
	if err := f.c.deliverLocal(context.Background(), pad); err != nil {
		t.Fatal(err)
	}
	mouse := protocol.InputFrame{Device: uint8(remoteinput.DeviceMouse), Kind: uint8(remoteinput.KindRelative), Action: uint8(remoteinput.ActionRelative), Value: 1}
	if err := f.c.deliverLocal(context.Background(), mouse); err != nil {
		t.Fatal(err)
	}
	if padPosts != 1 || mousePosts != 1 {
		t.Fatalf("pad=%d mouse=%d; unrelated input did not pass", padPosts, mousePosts)
	}
	if keyPosts != 1 {
		t.Fatalf("pad/mouse replayed ambiguous keyboard: %d posts", keyPosts)
	}
	if status, err := backend.Neutral(context.Background(), witness); err != nil || status != "neutral-confirmed" {
		t.Fatalf("neutral = %q, %v", status, err)
	}
	if keyPosts != 2 {
		t.Fatalf("neutral was not the next keyboard post: %d", keyPosts)
	}
}

func TestLocalPadCannotReplayUncertainV2Matrix(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	backend := delegatedV2Backend{f.c}
	witness, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	var posts int
	fail := true
	f.c.keyboard.SetPoster(func(ctx context.Context, post localhid.MatrixPost, _ uint64) error {
		if err := post.Validate(ctx); err != nil {
			return err
		}
		posts++
		if fail {
			return errors.New("ambiguous input")
		}
		return nil
	})
	if err := backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("ambiguous keyboard edge succeeded")
	}
	fail = false
	pad := protocol.InputFrame{Device: uint8(remoteinput.DeviceGamepad), Kind: uint8(remoteinput.KindButton), Action: uint8(remoteinput.ActionPress), Code: uint16(remoteinput.ButtonA)}
	if err := f.c.deliverLocal(context.Background(), pad); err == nil {
		t.Fatal("pad-to-matrix write crossed keyboard uncertainty")
	}
	if posts != 1 {
		t.Fatalf("pad replayed ambiguous keyboard: %d posts", posts)
	}
	if status, err := backend.Neutral(context.Background(), witness); err != nil || status != "neutral-confirmed" {
		t.Fatalf("neutral = %q, %v", status, err)
	}
	if posts != 2 {
		t.Fatalf("neutral was not the next matrix post: %d", posts)
	}
}
