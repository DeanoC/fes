//go:build sdl3

package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestSDLMouseFocusLossDispatch(t *testing.T) {
	delivered := make(chan remoteinput.Event, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Event remoteinput.Event }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		delivered <- body.Event
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.session = hostclient.SessionResult{State: "active", CoreMouse: true, Input: &hostclient.SessionInput{State: "attached", Ready: true}}
	app.playMice = map[int]*playMouse{1: {x: .8, y: -.4, buttons: 1}}
	if dispatchSyntheticSDL(app, int(evFocusLost), 0) {
		t.Fatal("focus loss quit the launcher")
	}
	app.mu.Lock()
	retained := len(app.playMice)
	tail := app.playHIDTail
	app.mu.Unlock()
	if retained != 0 {
		t.Fatal("focus loss retained physical mouse state")
	}
	select {
	case event := <-delivered:
		x, y, buttons, ok := remoteinput.MouseVector(event)
		if !ok || x != 0 || y != 0 || buttons != 0 {
			t.Fatalf("focus release=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("SDL focus loss did not release captured buttons")
	}
	<-tail
}
