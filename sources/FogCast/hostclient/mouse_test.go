package hostclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestMousePostPackedOnceAndAdmission(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		if r.URL.Path != "/api/v1/session/input/event" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body struct{ Event remoteinput.Event }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		x, y, buttons, ok := remoteinput.MouseVector(body.Event)
		if !ok || x != -32768 || y != 32767 || buttons != 3 {
			t.Errorf("mouse report=%+v", body.Event)
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	session := SessionResult{State: "active", CoreMouse: true, Input: &SessionInput{State: "attached", Ready: true}, CorePackage: &SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 7, ABI: SessionCoreABI{ID: "fes.computer", Major: 1}, ActiveInterfaces: []SessionCoreInterface{{ID: "fes.mouse.relative", Major: 1}}}}
	if err := client.SendMouseRelativeForSession(context.Background(), session, -32768, 32767, 3); err != nil || posts != 1 {
		t.Fatalf("post=%d err=%v", posts, err)
	}
	for _, invalid := range []SessionResult{{}, {State: "active", Input: session.Input}, {State: "active", CoreMouse: true}} {
		if err := client.SendMouseRelativeForSession(context.Background(), invalid, 1, 1, 0); err == nil {
			t.Fatal("invalid session posted mouse")
		}
	}
	if err := client.SendMouseRelativeForSession(context.Background(), session, 0, 0, 4); err == nil || posts != 1 {
		t.Fatal("invalid button report posted")
	}
	lost := 0
	client = NewClient("http://not-used", &http.Client{Transport: mouseLostResponse(func(*http.Request) (*http.Response, error) {
		lost++
		return nil, io.ErrUnexpectedEOF
	})})
	if err := client.SendMouseRelativeForSession(context.Background(), session, 1, 1, 1); err == nil || lost != 1 {
		t.Fatalf("ambiguous report was replayed: posts=%d err=%v", lost, err)
	}
}

type mouseLostResponse func(*http.Request) (*http.Response, error)

func (f mouseLostResponse) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
