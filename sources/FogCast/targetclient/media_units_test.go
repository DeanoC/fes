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

func TestMediaUnitClientStreamsExactBindingUnderTheLease(t *testing.T) {
	id := strings.Repeat("a", 64)
	state := "ready"
	var paths []string
	var bodies []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/kit/claim" {
			json.NewEncoder(w).Encode(kitlease.Grant{Token: "secret", Status: kitlease.Status{State: "held", Generation: "gen", Owner: "test", Purpose: "test", ExpiresInMS: 60000, ExpiresAt: time.Now().Add(time.Minute)}})
			return
		}
		if r.URL.Path == "/v1/kit/release" {
			json.NewEncoder(w).Encode(kitlease.Status{State: "available"})
			return
		}
		data, _ := io.ReadAll(r.Body)
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, len(data))
		if r.Header.Get(targetclient.KitLeaseHeader) != "secret" || r.Header.Get("X-FogCast-Package-ID") != id || (r.Header.Get("X-FogCast-Core-Generation") != "5" && r.Header.Get("X-FogCast-Core-Generation") != "4") ||
			r.Header.Get(protocol.MediaUnitHeader) != "0" || r.Header.Get(protocol.HostTargetHeader) != "dev" {
			t.Errorf("headers %v", r.Header)
		}
		if r.URL.Path == "/v1/development/insert-media" && (r.Header.Get("Content-Type") != "application/octet-stream" || r.ContentLength != 143360) {
			t.Errorf("insert framing %v %d", r.Header, r.ContentLength)
		}
		status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 5, BuildID: strings.Repeat("b", 32),
			ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.media.apple2-floppy", Major: 1}},
			MediaUnits: []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.Apple2FloppyInterface(), MinBytes: 143360, MaxBytes: 143360, ChunkBytes: 512, State: state}}}}
		json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	lease := targetclient.NewKitLease(base, "bearer", server.Client(), "test", "test")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(base, "bearer", server.Client()).WithKitLease(lease)
	b := protocol.MediaUnitBinding{PackageID: id, Generation: 5, Unit: 0, Target: "dev"}
	disk := strings.Repeat("d", 143360)
	if _, err := client.InsertMedia(context.Background(), int64(len(disk)), strings.NewReader(disk), b); err == nil || len(paths) != 0 {
		t.Fatal("insert dispatched without the session lease")
	}
	request, _ := http.NewRequest("POST", server.URL+"/v1/development/core", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	got, err := client.InsertMedia(context.Background(), int64(len(disk)), strings.NewReader(disk), b)
	if err != nil || !b.Matches(got) || len(paths) != 1 || paths[0] != "/v1/development/insert-media" || bodies[0] != 143360 {
		t.Fatalf("insert %v %v %v", paths, bodies, err)
	}
	state = "empty"
	if _, err := client.InsertMedia(context.Background(), int64(len(disk)), strings.NewReader(disk), b); err == nil {
		t.Fatal("insert accepted a unit that is not ready")
	}
	got, err = client.EjectMedia(context.Background(), b)
	if err != nil || paths[len(paths)-1] != "/v1/development/eject-media" || bodies[len(bodies)-1] != 0 {
		t.Fatalf("eject %v %v", paths, err)
	}
	if _, err := client.InsertMedia(context.Background(), protocol.MaxComputerMediaBytes+1, strings.NewReader(disk), b); err == nil {
		t.Fatal("oversize insert accepted")
	}
	stale := b
	stale.Generation = 4
	before := len(paths)
	if _, err := client.EjectMedia(context.Background(), stale); err == nil || len(paths) != before+1 {
		t.Fatalf("stale response accepted: %v", err)
	}
}
