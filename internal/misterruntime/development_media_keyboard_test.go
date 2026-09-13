package misterruntime_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type mediaKeyboardControl struct {
	mediaControl
	keysMu           sync.Mutex
	busy             bool
	matrix           uint64
	during           chan struct{}
	keyboardResponse *misterruntime.Protocol2Response
	failNeutral      bool
}

func (c *mediaKeyboardControl) SetKeyboard(_ context.Context, matrix uint64) (misterruntime.Protocol2Response, error) {
	c.keysMu.Lock()
	defer c.keysMu.Unlock()
	if c.failNeutral && matrix == zx81keys.Neutral {
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "busy", Phase: "input", Message: "neutralization failed"}}, nil
	}
	if c.busy {
		select {
		case c.during <- struct{}{}:
		default:
		}
		return misterruntime.Protocol2Response{OK: false, Error: &misterruntime.Protocol2Error{Code: "busy", Phase: "input", Message: "media in progress"}}, nil
	}
	c.matrix = matrix
	if c.keyboardResponse != nil {
		return *c.keyboardResponse, nil
	}
	return mediaResponse(), nil
}

func TestDevelopmentMediaDoesNotRestoreKeysFromPreviousGeneration(t *testing.T) {
	response := mediaResponse()
	response.Capabilities.ActiveInterfaces = []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}
	c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1), keyboardResponse: &response}
	r := misterruntime.NewRuntime(c, "", 0, 0)
	keys := input.NewKeyboardSink()
	keys.SetPoster(func(matrix uint64) error { return r.SetKeyboard(context.Background(), matrix) })
	if err := keys.Apply(protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Code: uint16(zx81keys.Letter('J')), Action: uint8(remoteinput.ActionPress)}); err != nil {
		t.Fatal(err)
	}
	c.failNeutral = true
	if err := keys.ReleaseAll(); err != nil {
		t.Fatal(err)
	} // Existing cleanup deliberately ignores posting failure.
	c.failNeutral = false
	*response.Generation = 10 // Successful replacement has a fresh target-local generation.
	c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
		c.matrix = zx81keys.Neutral
		return response, nil
	}
	err := r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 10})
	if err != nil {
		t.Fatal(err)
	}
	if c.matrix != zx81keys.Neutral {
		t.Fatalf("previous generation's held key restored: %x", c.matrix)
	}
}
func TestDevelopmentMediaSerializesKeyboardAndRestoresHeldMatrix(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "held key", true: "event during transfer"}[during], func(t *testing.T) {
			response := mediaResponse()
			response.Capabilities.ActiveInterfaces = []misterruntime.Protocol2Interface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.media.blob", Major: 1}}
			c := &mediaKeyboardControl{mediaControl: mediaControl{packageControl: packageControl{status2: &response}}, during: make(chan struct{}, 1)}
			entered, release := make(chan struct{}), make(chan struct{})
			c.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
				c.keysMu.Lock()
				c.busy = true
				c.matrix = zx81keys.Neutral
				c.keysMu.Unlock()
				close(entered)
				<-release
				c.keysMu.Lock()
				c.busy = false
				c.keysMu.Unlock()
				return response, nil
			}
			r := misterruntime.NewRuntime(c, "", 0, 0)
			keys := input.NewKeyboardSink()
			keys.SetPoster(func(matrix uint64) error { return r.SetKeyboard(context.Background(), matrix) })
			frame := protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Code: uint16(zx81keys.Letter('J')), Action: uint8(remoteinput.ActionPress)}
			if err := keys.Apply(frame); err != nil {
				t.Fatal(err)
			}
			c.keysMu.Lock()
			held := c.matrix
			c.keysMu.Unlock()
			done := make(chan *protocol.APIError, 1)
			go func() {
				done <- r.LoadDevelopmentMedia(context.Background(), 1, strings.NewReader("x"), protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9})
			}()
			<-entered
			var keyDone chan error
			if during {
				keyDone = make(chan error, 1)
				frame.Action = uint8(remoteinput.ActionRelease)
				go func() { keyDone <- keys.Apply(frame) }()
				select {
				case <-c.during:
					t.Error("keyboard reached busy runtime during media")
				case <-time.After(20 * time.Millisecond):
				}
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			want := held
			if during {
				if err := <-keyDone; err != nil {
					t.Fatalf("input stream would close: %v", err)
				}
				want = zx81keys.Neutral
			}
			c.keysMu.Lock()
			got := c.matrix
			c.keysMu.Unlock()
			if got != want {
				t.Fatalf("keyboard matrix=%x want=%x", got, want)
			}
		})
	}
}
