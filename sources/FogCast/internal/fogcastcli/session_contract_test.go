package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
)

type sessionDeadlineTransport func(*http.Request) (*http.Response, error)

func (f sessionDeadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSessionMutationsPreserveCallerDeadline(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, command := range [][]string{{"launch", "fixture"}, {"stop"}} {
		t.Run(command[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			want, _ := ctx.Deadline()
			calls := 0
			http.DefaultTransport = sessionDeadlineTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				got, ok := r.Context().Deadline()
				if !ok || !got.Equal(want) {
					t.Errorf("request deadline %v, want caller deadline %v", got, want)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fixtureSessionJSON))}, nil
			})
			var out, errOut bytes.Buffer
			exit := Run(ctx, append([]string{"--json", "--api", "http://127.0.0.1:8787"}, command...), &out, &errOut, failOpen(t))
			if exit != 0 || calls != 1 {
				t.Fatalf("exit=%d calls=%d stderr=%s", exit, calls, &errOut)
			}
		})
	}
}

func TestSessionJSONPreservesPublicInputStatus(t *testing.T) {
	input := host.RemoteInputStatus{SessionID: "input-session", Source: "usb", State: host.RemoteInputAttached, Ready: true,
		Metrics: host.RemoteInputMetrics{FramesSent: 9007199254740993, StateResyncs: 2, SequenceGaps: 3, Releases: 4, CaptureToBridgeP95MS: 1.25, BridgeToUInputP95MS: 2.5, RTTMS: 3.75, BridgeToUInputMeasurable: true, ShutdownReason: "session_stop"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "sess-1", "state": "active", "input": input})
	}))
	defer server.Close()
	var out, errOut bytes.Buffer
	exit := Run(context.Background(), []string{"--json", "--api", server.URL, "status"}, &out, &errOut, failOpen(t))
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, &errOut)
	}
	var got struct {
		Input host.RemoteInputStatus `json:"input"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Input, input) {
		t.Fatalf("input=%+v want=%+v", got.Input, input)
	}
}

func TestSessionMutationCancellationAfterRequestStarts(t *testing.T) {
	for _, command := range [][]string{{"launch", "fixture"}, {"stop"}} {
		t.Run(command[0], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				cancel()
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
					t.Error("request did not propagate caller cancellation")
				}
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			exit := Run(ctx, append([]string{"--json", "--api", server.URL}, command...), &out, &errOut, failOpen(t))
			if exit != 1 || calls.Load() != 1 || !strings.Contains(out.String(), `"code":"CANCELED"`) {
				t.Fatalf("exit=%d calls=%d stdout=%s stderr=%s", exit, calls.Load(), &out, &errOut)
			}
		})
	}
}
