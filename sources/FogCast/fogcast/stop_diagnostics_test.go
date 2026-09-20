package fogcast

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestStopStagesSeparateAdmissionFromDispatchWithoutReplay(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	for _, fail := range []string{"admission_status", "admission_ownership", "target_stop"} {
		t.Run(fail, func(t *testing.T) {
			var stops atomic.Int32
			var recovered atomic.Bool
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/health":
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: id, Ready: true})
				case "/v1/kit/lease":
					if fail == "admission_ownership" && !recovered.Load() {
						http.Error(w, "secret token", 500)
						return
					}
					json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
				case "/v1/status":
					if fail == "admission_status" && !recovered.Load() {
						http.Error(w, "secret endpoint", 500)
						return
					}
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateActive, Development: true})
				case "/v1/stop":
					stops.Add(1)
					if fail == "target_stop" && !recovered.Load() {
						w.WriteHeader(500)
						json.NewEncoder(w).Encode(map[string]any{"error": &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "secret runtime path", Phase: "save"}})
						return
					}
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer peer.Close()
			base, _ := url.Parse(peer.URL)
			s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: peer.URL, Agent: "secret", TargetID: id}}, SelectedTarget: "kit", RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(base, "secret", peer.Client()))
			s.activeExecution, s.activeTarget = ExecutionFPGADevelopment, "kit"
			_, err := s.Stop(context.Background())
			if StopStage(err) != fail {
				t.Fatalf("stage=%q error=%v", StopStage(err), err)
			}
			want := int32(0)
			if fail == "target_stop" {
				want = 1
			}
			if stops.Load() != want || s.activeExecution != ExecutionFPGADevelopment {
				t.Fatalf("stops=%d execution=%s", stops.Load(), s.activeExecution)
			}
			if fail != "target_stop" {
				_, err = s.refreshTargetAdmission(context.Background())
				if StopStage(stopAdmissionError(err)) != "admission_backoff" || stops.Load() != 0 {
					t.Fatalf("backoff err=%v stage=%s stops=%d", err, StopStage(err), stops.Load())
				}
			}
			recovered.Store(true)
			// Explicit Stop must freshly admit even while polling remains backed off.
			status, err := s.Stop(context.Background())
			if err != nil || status.State != protocol.StateIdle || stops.Load() != want+1 {
				t.Fatalf("explicit retry status=%+v err=%v stops=%d", status, err, stops.Load())
			}
		})
	}
}

func TestExplicitStopFreshAdmissionDuringLookupBackoff(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	for _, tc := range []struct {
		name, version          string
		pending, wrongIdentity bool
	}{
		{"active", "v1", false, false},
		{"invalid-version", "v2", false, false},
		{"pending-rejection", "v1", true, false},
		{"wrong-identity", "v1", false, true},
		{"pending-wrong-identity", "v1", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var healths, ownerships, statuses, stops atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/health":
					healths.Add(1)
					peerID := id
					if tc.wrongIdentity {
						peerID = "another-kit"
					}
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: tc.version, TargetID: peerID, Ready: true})
				case "/v1/kit/lease":
					ownerships.Add(1)
					json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
				case "/v1/status":
					statuses.Add(1)
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateActive, Development: true})
				case "/v1/stop":
					stops.Add(1)
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer peer.Close()
			base, _ := url.Parse(peer.URL)
			s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: peer.URL, Agent: "secret", TargetID: id}}, SelectedTarget: "kit", RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(base, "secret", peer.Client()))
			s.activeExecution, s.activeTarget = ExecutionFPGADevelopment, "kit"
			s.resolveTarget = func(context.Context, string) ([]string, error) { return nil, nil }
			if tc.pending {
				s.packageRejection = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "recovery"}
			}
			// Same state produced by a failed background connection observation.
			_, _ = s.connectionFailed(TargetConnection{TargetID: id, Address: peer.URL}, context.Canceled)
			_, err := s.refreshTargetAdmission(context.Background())
			if StopStage(stopAdmissionError(err)) != "admission_backoff" || healths.Load() != 0 {
				t.Fatal("poll did not respect backoff")
			}
			if tc.pending {
				// Activation cleanup keeps ordinary admission, unlike explicit Stop.
				_, cleanupErr := s.stopRejectedCore(context.Background())
				if cleanupErr == nil || healths.Load() != 0 || stops.Load() != 0 {
					t.Fatal("activation cleanup bypassed backoff")
				}
			}
			status, err := s.Stop(context.Background())
			if healths.Load() != 1 {
				t.Fatalf("fresh health calls=%d error=%v", healths.Load(), err)
			}
			if tc.wrongIdentity {
				if err == nil || stops.Load() != 0 || ownerships.Load() != 0 || statuses.Load() != 0 {
					t.Fatalf("wrong identity admitted: err=%v stops=%d", err, stops.Load())
				}
			} else if tc.version == "v1" {
				wantStatuses := int32(1)
				if tc.pending {
					wantStatuses = 2
				}
				if err != nil || status.State != protocol.StateIdle || ownerships.Load() != 1 || statuses.Load() != wantStatuses || stops.Load() != 1 || s.packageRejection != nil {
					t.Fatalf("Stop=%+v err=%v ownership=%d status=%d stops=%d", status, err, ownerships.Load(), statuses.Load(), stops.Load())
				}
			} else {
				var api *protocol.APIError
				if !errors.As(err, &api) || api.Code != protocol.CodeVersionMismatch || stops.Load() != 0 || ownerships.Load() != 0 || statuses.Load() != 0 {
					t.Fatalf("invalid peer admitted: %v stops=%d", err, stops.Load())
				}
			}
		})
	}
}

func TestStopStagePreservesErrorIdentity(t *testing.T) {
	source := &protocol.APIError{Code: protocol.CodeSaveFailed, Phase: "save", Message: "private"}
	err := WithStopStage(errors.Join(source, context.Canceled), "target_stop")
	var got *protocol.APIError
	if !errors.As(err, &got) || got != source || !errors.Is(err, context.Canceled) {
		t.Fatal("error identity lost")
	}
	if StopStage(WithStopStage(err, "admission")) != "target_stop" {
		t.Fatal("stage overwritten")
	}
	if StopStage(WithStopStage(source, "secret/path")) != "" {
		t.Fatal("untrusted stage accepted")
	}
}
