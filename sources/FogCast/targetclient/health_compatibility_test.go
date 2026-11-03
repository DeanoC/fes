package targetclient_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestHealthWireCompatibilityAndAdversarialIdentities(t *testing.T) {
	selected := strings.Repeat("a", 64)
	full := `{"version":1,"generation":0,"selected":"` + selected + `","receipts":{},"active_bundle":"` + selected + `","base_identity":"base"}`
	compact := `{"generation":0,"selected":"` + selected + `","active_bundle":"` + selected + `","base_identity":"base"}`
	for _, tc := range []struct {
		name, fields string
		want         bool
	}{
		{"legacy valid", `"software":` + full, true},
		{"legacy missing generation", `"software":` + strings.Replace(full, `"generation":0,`, "", 1), false},
		{"legacy duplicate selected", `"software":` + strings.Replace(full, `"selected":`, `"selected":"`+selected+`","selected":`, 1), false},
		{"legacy unicode alias", `"software":` + strings.Replace(full, `"selected":`, `"ſelected":"bad","selected":`, 1), false},
		{"legacy storage error", `"software":` + strings.TrimSuffix(full, "}") + `,"storage_error":"private"}`, false},
		{"compact old unmarked", `"software":` + compact, false},
		{"compact negotiated", `"software_identity_v1":` + compact, true},
		{"compact missing generation", `"software_identity_v1":` + strings.Replace(compact, `"generation":0,`, "", 1), false},
		{"compact null generation", `"software_identity_v1":` + strings.Replace(compact, `"generation":0`, `"generation":null`, 1), false},
		{"compact duplicate selected", `"software_identity_v1":` + strings.Replace(compact, `"selected":`, `"selected":"bad","selected":`, 1), false},
		{"compact unicode alias", `"software_identity_v1":` + strings.Replace(compact, `"selected":`, `"ſelected":"bad","selected":`, 1), false},
		{"compact storage error", `"software_identity_v1":` + strings.TrimSuffix(compact, "}") + `,"storage_error":"private"}`, false},
		{"compact null", `"software_identity_v1":null`, false},
		{"compact invalid selection", `"software_identity_v1":` + strings.Replace(compact, selected, "not-a-bundle-id", 1), false},
		{"compact duplicate generation", `"software_identity_v1":` + strings.Replace(compact, `"generation":0`, `"generation":1,"generation":0`, 1), false},
		{"unknown compact version", `"software_identity_v2":` + compact, false},
		{"both forms", `"software":` + full + `,"software_identity_v1":` + compact, false},
		{"duplicate outer", `"software_identity_v1":` + compact + `,"software_identity_v1":` + compact, false},
		{"outer unicode alias", `"ſoftware":` + full + `,"software":` + full, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get(protocol.SoftwareIdentityHeader) != protocol.SoftwareIdentityVersion {
					t.Error("missing negotiation")
				}
				io.WriteString(w, `{"api_version":"v1","ready":true,`+tc.fields+`}`)
			}))
			defer peer.Close()
			ep, _ := url.Parse(peer.URL)
			health, err := targetclient.NewClient(ep, "secret", peer.Client()).Health(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if health.Software != nil {
				got = health.Software.Selected
			}
			if health.SoftwareIdentityV1 != nil {
				got = health.SoftwareIdentityV1.Selected
			}
			if (tc.want && got != selected) || (!tc.want && got != "") {
				t.Fatalf("selected=%q, health=%+v", got, health)
			}
			if calls.Load() != 1 {
				t.Fatalf("unexpected number of requests: %d", calls.Load())
			}
		})
	}
}

func TestHealthTransportFailureDoesNotAdmitIdentity(t *testing.T) {
	selected := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"http failure", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"code":"UNAVAILABLE","message":"private"}}`)
		}},
		{"truncated transport", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "4096")
			io.WriteString(w, `{"api_version":"v1","ready":true,"software_identity_v1":{"selected":"`+selected+`"}}`)
		}},
		{"trailing JSON", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"api_version":"v1","ready":true} {}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer := httptest.NewServer(tc.handler)
			defer peer.Close()
			ep, _ := url.Parse(peer.URL)
			health, err := targetclient.NewClient(ep, "secret", peer.Client()).Health(context.Background())
			if err == nil || health.SoftwareIdentityV1 != nil || health.Software != nil {
				t.Fatalf("error=%v, health=%+v", err, health)
			}
		})
	}
}
