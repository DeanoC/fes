package hostclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type sessionStopTransport func(*http.Request) (*http.Response, error)

func (f sessionStopTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSessionStopDurableBudgetAndSingleMutation(t *testing.T) {
	for _, mode := range []string{"expected-bound", "explicit-bound", "retain-bound", "expected-volatile", "explicit-volatile", "explicit-unknown", "short-parent", "lost-response"} {
		t.Run(mode, func(t *testing.T) {
			p := &SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: SessionCoreABI{ID: "fes.computer", Major: 1}, ActiveInterfaces: []SessionCoreInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}}
			if strings.Contains(mode, "volatile") {
				p.MediaUnits[0].Persistence = nil
			}
			prior := SessionResult{ID: "session", State: "active", GameID: "st-desktop", Target: "kit", CorePackage: p}
			raw, _ := json.Marshal(map[string]any{"id": prior.ID, "state": "active", "game_id": prior.GameID, "target": prior.Target, "core_package": p})
			gets, posts := 0, 0
			var budget time.Duration
			original := &http.Client{Timeout: 5 * time.Second, Transport: sessionStopTransport(func(r *http.Request) (*http.Response, error) {
				body := raw
				if r.Method == "GET" {
					gets++
				} else {
					posts++
					d, _ := r.Context().Deadline()
					budget = time.Until(d)
					if r.URL.Path != "/api/v1/session/stop" {
						t.Fatal(r.URL)
					}
					if mode == "lost-response" {
						return nil, errors.New("lost Stop response")
					}
					body = []byte(`{"id":"session","state":"idle"}`)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			})}
			c := NewClient("http://host", original)
			// A completed normal poll supplies a deadline hint; Stop itself
			// must issue no GET, including when another poll is outstanding.
			if (strings.HasPrefix(mode, "explicit") && mode != "explicit-unknown") || mode == "retain-bound" {
				if _, err := c.Session(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			cancel := func() {}
			if mode == "short-parent" {
				ctx, cancel = context.WithTimeout(ctx, time.Second)
			}
			defer cancel()
			var err error
			if strings.HasPrefix(mode, "explicit") {
				_, err = c.Stop(ctx)
			} else if mode == "retain-bound" {
				_, err = c.StopRetainLease(ctx, ClientStampNow())
			} else {
				_, err = c.StopExpectedStamped(ctx, prior, ClientStampNow())
			}
			if mode == "lost-response" {
				if err == nil {
					t.Fatal("lost reply accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if posts != 1 {
				t.Fatal("Stop replayed", posts)
			}
			wantGets := 0
			if (strings.HasPrefix(mode, "explicit") && mode != "explicit-unknown") || mode == "retain-bound" {
				wantGets = 1
			}
			if gets != wantGets {
				t.Fatal(gets, wantGets)
			}
			if mode == "short-parent" {
				if budget > time.Second {
					t.Fatal(budget)
				}
			} else if strings.Contains(mode, "volatile") || mode == "explicit-unknown" {
				if budget < 59*time.Second || budget > 60*time.Second {
					t.Fatal(budget)
				}
			} else if budget < 149*time.Second || budget > 150*time.Second {
				t.Fatal(budget)
			}
			if original.Timeout != 5*time.Second || c.stopHTTP.Timeout != 60*time.Second {
				t.Fatal("shared HTTP budget modified")
			}
		})
	}
}

func TestExplicitStopDoesNotWaitForPollOrAdoptItsLateBudget(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	budgets := make(chan time.Duration, 4)
	p := &SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: SessionCoreABI{ID: "fes.computer", Major: 1}, ActiveInterfaces: []SessionCoreInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}}
	raw, _ := json.Marshal(map[string]any{"id": "session", "state": "active", "game_id": "st-desktop", "target": "kit", "core_package": p})
	transport := sessionStopTransport(func(r *http.Request) (*http.Response, error) {
		body := []byte(`{"id":"session","state":"idle"}`)
		if r.Method == "GET" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			body = raw
		} else {
			d, _ := r.Context().Deadline()
			budgets <- time.Until(d)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	c := NewClient("http://host", &http.Client{Transport: transport, Timeout: 5 * time.Second})
	polled := make(chan error, 1)
	go func() { _, err := c.Session(context.Background()); polled <- err }()
	<-started
	stopped := make(chan error, 1)
	go func() { _, err := c.Stop(context.Background()); stopped <- err }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("Stop waited for a poll")
	}
	close(release)
	if err := <-polled; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		b := <-budgets
		if b < 59*time.Second || b > 60*time.Second {
			t.Fatal("late stale poll replaced idle deadline", b)
		}
	}
}

func TestStopReplyFencesPollStartedDuringMutation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	budgets := make(chan time.Duration, 2)
	p := &SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: SessionCoreABI{ID: "fes.computer", Major: 1}, ActiveInterfaces: []SessionCoreInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}}}
	raw, _ := json.Marshal(map[string]any{"id": "session", "state": "active", "game_id": "st-desktop", "target": "kit", "core_package": p})
	posts := 0
	c := NewClient("http://host", &http.Client{Timeout: 5 * time.Second, Transport: sessionStopTransport(func(r *http.Request) (*http.Response, error) {
		body := raw
		if r.Method == "POST" {
			posts++
			d, _ := r.Context().Deadline()
			budgets <- time.Until(d)
			if posts == 1 {
				close(started)
				<-release
			}
			body = []byte(`{"id":"session","state":"idle"}`)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})})
	stopped := make(chan error, 1)
	go func() { _, err := c.Stop(context.Background()); stopped <- err }()
	<-started
	if _, err := c.Session(context.Background()); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		budget := <-budgets
		if budget < 59*time.Second || budget > 60*time.Second {
			t.Fatal("inflight poll survived completed idle", budget)
		}
	}
}
