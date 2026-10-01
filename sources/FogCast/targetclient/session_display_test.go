package targetclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestDisplayClientRequiresExistingLeaseAndRejectsReplacedResponse(t *testing.T) {
	claims, calls := 0, 0
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: b.PackageID, Generation: b.Generation, BuildID: strings.Repeat("b", 32), ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			claims++
			_ = json.NewEncoder(w).Encode(kitlease.Grant{Token: "grant", Status: kitlease.Status{State: "held", Generation: "lease-generation", Owner: "test", Purpose: "test", ExpiresInMS: 60000, ExpiresAt: time.Now().Add(time.Minute)}})
		case "/v1/kit/release":
			_ = json.NewEncoder(w).Encode(kitlease.Status{State: "free"})
		case "/v1/session/display":
			calls++
			got, valid := protocol.DevelopmentMediaHeaders(r.Header)
			var request protocol.SessionDisplayRequest
			if !valid || got != b || r.Header.Get(targetclient.KitLeaseHeader) != "grant" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Visible == nil || !*request.Visible {
				t.Error("display request lost its captured binding or explicit visibility")
			}
			_ = json.NewEncoder(w).Encode(status)
		default:
			t.Errorf("unexpected operation %s", r.URL.Path)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	lease := targetclient.NewKitLease(base, "bearer", server.Client(), "test", "test")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(base, "bearer", server.Client()).WithKitLease(lease)
	if _, err := client.SetSessionDisplay(context.Background(), true, b); err == nil || claims != 0 || calls != 0 {
		t.Fatal("display acquired ownership or dispatched unowned")
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/development/core", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	if got, err := client.SetSessionDisplay(context.Background(), true, b); err != nil || !b.MatchesSessionDisplay(got) || calls != 1 {
		t.Fatalf("display err=%v status=%+v", err, got)
	}
	status.CorePackage.Generation++
	if _, err := client.SetSessionDisplay(context.Background(), true, b); err == nil || calls != 2 || claims != 1 {
		t.Fatal("replacement response accepted or operation replayed")
	}
}
