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
	for _, mode := range []string{"expected-bound", "explicit-bound", "retain-bound", "expected-volatile", "explicit-volatile", "short-parent", "lost-response"} {
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
			if strings.HasPrefix(mode, "explicit") || mode == "retain-bound" {
				wantGets = 1
			}
			if gets != wantGets {
				t.Fatal(gets, wantGets)
			}
			if mode == "short-parent" {
				if budget > time.Second {
					t.Fatal(budget)
				}
			} else if strings.Contains(mode, "volatile") {
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
