package targetclient_test

import (
	"context"
	"encoding/json"
	"io"
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

func TestDevelopmentMediaClientRequiresExistingLeaseAndSendsBinding(t *testing.T) {
	calls, claims := 0, 0
	wantPath, payload := "/v1/development/media", "raw"
	redirect := false
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, BuildID: strings.Repeat("b", 32), ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/kit/claim" {
			claims++
			json.NewEncoder(w).Encode(kitlease.Grant{Token: "secret", Status: kitlease.Status{State: "held", Generation: "gen", Owner: "test", Purpose: "test", ExpiresInMS: 60000, ExpiresAt: time.Now().Add(time.Minute)}})
			return
		}
		if r.URL.Path == "/v1/kit/release" {
			json.NewEncoder(w).Encode(kitlease.Status{State: "available"})
			return
		}
		calls++
		data, _ := io.ReadAll(r.Body)
		if r.URL.Path != wantPath || r.Header.Get(targetclient.KitLeaseHeader) != "secret" || r.Header.Get("X-FogCast-Package-ID") != strings.Repeat("a", 64) || r.Header.Get("X-FogCast-Core-Generation") != "9" || string(data) != payload || r.ContentLength != int64(len(payload)) || len(r.TransferEncoding) != 0 {
			t.Errorf("invalid upload: %s headers=%v data=%q", r.URL.Path, r.Header, data)
		}
		if redirect {
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	lease := targetclient.NewKitLease(base, "bearer", server.Client(), "test", "test")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(base, "bearer", server.Client()).WithKitLease(lease)
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}
	if _, err := client.LoadDevelopmentMedia(context.Background(), 3, strings.NewReader("raw"), b); err == nil || claims != 0 || calls != 0 {
		t.Fatal("media acquired a new lease or dispatched unowned")
	}
	request, _ := http.NewRequest("POST", server.URL+"/v1/development/core", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	got, err := client.LoadDevelopmentMedia(context.Background(), 3, strings.NewReader("raw"), b)
	if err != nil || !b.Matches(got) || calls != 1 || claims != 1 {
		t.Fatalf("status=%+v calls=%d claims=%d error=%v", got, calls, claims, err)
	}
	wantPath, payload = "/v1/development/media-stream", strings.Repeat("x", 32768)
	b.Stream = true
	status.CorePackage.ActiveInterfaces = append(status.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: protocol.MediaStreamInterface().ID, Major: 1})
	status.CorePackage.MediaStream = &protocol.MediaStreamCapability{Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512}
	got, err = client.LoadDevelopmentMedia(context.Background(), int64(len(payload)), strings.NewReader(payload), b)
	if err != nil || !b.Matches(got) || calls != 2 {
		t.Fatalf("stream calls=%d err=%v", calls, err)
	}
	if _, err = client.LoadDevelopmentMedia(context.Background(), 32769, strings.NewReader(payload), b); err == nil || calls != 2 {
		t.Fatal("oversize dispatched")
	}
	redirect = true
	if _, err = client.LoadDevelopmentMedia(context.Background(), int64(len(payload)), strings.NewReader(payload), b); err == nil || calls != 3 {
		t.Fatalf("redirect replayed: calls=%d err=%v", calls, err)
	}
}
