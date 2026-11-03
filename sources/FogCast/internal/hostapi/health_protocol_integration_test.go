package hostapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/testsock"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

type healthProtocolController struct{}

func (healthProtocolController) HealthContext(context.Context, string) protocol.Health {
	return protocol.Health{APIVersion: "v1", Ready: true}
}
func (healthProtocolController) Status() protocol.Status { return protocol.Status{} }
func (healthProtocolController) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	return protocol.Status{}, nil
}

func TestHealthNegotiatedProtocolRealAgentHost(t *testing.T) {
	selected := strings.Repeat("a", 64)
	valid := `{"version":1,"generation":7,"selected":"` + selected + `","receipts":{},"active_bundle":"` + selected + `","base_identity":"base"}`
	for _, tc := range []struct {
		name, status string
		want         bool
	}{
		{"healthy", valid, true},
		{"missing generation", strings.Replace(valid, `"generation":7,`, "", 1), false},
		{"duplicate selection", strings.Replace(valid, `"selected":`, `"selected":"`+selected+`","selected":`, 1), false},
		{"unicode selection alias", strings.Replace(valid, `"selected":`, `"ſelected":"bad","selected":`, 1), false},
		{"storage error", strings.TrimSuffix(valid, "}") + `,"storage_error":"private"}`, false},
		{"trailing JSON", valid + ` {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			socket := testsock.Path(t, "supervisor.sock")
			ln, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, tc.status) })}
			go srv.Serve(ln)
			t.Cleanup(func() { srv.Close() })
			agent := httptest.NewServer(httpapi.New(healthProtocolController{}, "secret", "test", nil, httpapi.WithKitSoftware(socket)))
			defer agent.Close()
			ep, _ := url.Parse(agent.URL)
			service := &healthWireService{fakeService: &fakeService{}, client: targetclient.NewClient(ep, "secret", agent.Client())}
			response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/health")
			var result struct {
				Target struct {
					Software *struct {
						Selected   string `json:"selected"`
						Generation uint64 `json:"generation"`
					} `json:"software"`
				} `json:"target"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			got := ""
			if result.Target.Software != nil {
				got = result.Target.Software.Selected
			}
			if (tc.want && got != selected) || (!tc.want && got != "") {
				t.Fatalf("selected=%q; response=%s", got, response.Body.String())
			}
			if calls.Load() != 1 {
				t.Fatalf("supervisor calls=%d, want 1", calls.Load())
			}
			if tc.want && result.Target.Software.Generation != 7 {
				t.Fatalf("wrong generation: %+v", result.Target.Software)
			}
			if !tc.want && strings.Contains(response.Body.String(), "private") {
				t.Fatal("private supervisor error leaked")
			}
		})
	}
}
