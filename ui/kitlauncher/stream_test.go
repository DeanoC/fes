package kitlauncher

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/remoteinput"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamDeliversEventAndCloses(t *testing.T) {
	got := make(chan remoteinput.Event, 1)
	ended := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(ended)
		if r.URL.Query().Get("session_id") != "abc" {
			t.Error("session missing")
		}
		_ = http.NewResponseController(w).EnableFullDuplex()
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		dec := json.NewDecoder(r.Body)
		for {
			var p struct {
				Event *remoteinput.Event `json:"event"`
			}
			if dec.Decode(&p) != nil {
				return
			}
			if p.Event != nil {
				got <- *p.Event
			}
		}
	}))
	defer server.Close()
	c := NewClient(Config{API: server.URL, Token: "secret", TargetID: "id"})
	s := c.OpenInput(context.Background(), "abc")
	defer s.Close()
	select {
	case <-s.Ready:
	case <-time.After(time.Second):
		t.Fatal("stream did not open")
	}
	e, _ := remoteinput.NormalizeGamepad("a", true)
	if !s.Send(e) {
		t.Fatal("send failed")
	}
	select {
	case actual := <-got:
		if actual != e {
			t.Fatal(actual)
		}
	case <-time.After(time.Second):
		t.Fatal("event lost")
	}
	s.Close()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("body not closed")
	}
}
