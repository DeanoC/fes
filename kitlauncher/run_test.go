package kitlauncher

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/remoteinput"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnavailableHostStillRendersAndExits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	draws := 0
	err := Run(ctx, NewClient(Config{API: server.URL}), func(Model) { draws++ }, func() (Pad, error) { return nil, errors.New("no pad") })
	if err != nil {
		t.Fatal(err)
	}
	if draws == 0 {
		t.Fatal("connecting screen never rendered")
	}
}

type pressPad struct{ sent bool }

func (p *pressPad) Poll() ([]remoteinput.Event, error) {
	if p.sent {
		return nil, nil
	}
	p.sent = true
	e, _ := remoteinput.NormalizeGamepad("a", true)
	return []remoteinput.Event{e}, nil
}
func (*pressPad) Close() error { return nil }

func TestLaunchPresentsLoadingBeforeDispatch(t *testing.T) {
	var loading atomic.Bool
	launched := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","launchable":true}]}`))
		case "/api/v1/session/launch":
			if !loading.Load() {
				t.Error("launch dispatched before loading frame")
			}
			close(launched)
			_, _ = w.Write([]byte(`{"state":"active","game_id":"pong"}`))
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ready := false
	_ = Run(ctx, NewClient(Config{API: server.URL}), func(m Model) {
		if m.Busy && m.Message == "Loading game" {
			loading.Store(true)
		}
		if len(m.Games) > 0 {
			ready = true
		}
	}, func() (Pad, error) {
		if !ready {
			return nil, errors.New("wait")
		}
		return &pressPad{}, nil
	})
	select {
	case <-launched:
	default:
		t.Fatal("launch not attempted")
	}
}
