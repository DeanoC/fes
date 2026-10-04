package tenfoot

import (
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestPlayMouseFractionsButtonsAndCapability(t *testing.T) {
	var mu sync.Mutex
	var events []remoteinput.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Event remoteinput.Event `json:"event"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		events = append(events, body.Event)
		mu.Unlock()
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	a.session = hostclient.SessionResult{State: "active", CoreMouse: true, Input: &hostclient.SessionInput{State: "attached", Ready: true}}
	if !a.HandlePlayMouseMotion(1, .4, -.2) || !a.HandlePlayMouseMotion(1, .7, -.9) {
		t.Fatal("capture")
	}
	a.HandlePlayMouseButton(1, 1, true)
	a.HandlePlayMouseButton(2, 3, true)
	a.ReleasePlayMouse(1)
	a.ReleaseAllPlayMouse()
	a.mu.Lock()
	tail := a.playHIDTail
	a.mu.Unlock()
	select {
	case <-tail:
	case <-time.After(time.Second):
		t.Fatal("queue")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 5 {
		t.Fatal(events)
	}
	x, y, b, ok := remoteinput.MouseVector(events[0])
	if !ok || x != 1 || y != -1 || b != 0 {
		t.Fatal(events)
	}
	_, _, b, _ = remoteinput.MouseVector(events[2])
	if b != 3 {
		t.Fatal(events)
	}
	x, y, b, _ = remoteinput.MouseVector(events[3])
	if x != 0 || y != 0 || b != 2 {
		t.Fatal(events)
	}
	x, y, b, _ = remoteinput.MouseVector(events[4])
	if x != 0 || y != 0 || b != 0 {
		t.Fatal("focus loss must release without replaying motion", events)
	}
	a.mu.Lock()
	if len(a.playMice) != 0 {
		t.Fatal("focus loss retained physical mouse state")
	}
	a.session.CoreMouse = false
	a.mu.Unlock()
	if a.HandlePlayMouseMotion(1, 1, 1) {
		t.Fatal("undeclared mouse")
	}
}
