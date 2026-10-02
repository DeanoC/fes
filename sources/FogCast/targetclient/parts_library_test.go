package targetclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
	"github.com/DeanoC/misteross/expansion"
)

func libraryPartsStatus() protocol.Status {
	id := strings.Repeat("a", 64)
	c := expansion.PartsComposition{PackageID: id, Layout: expansion.ColecoVideoLayout, Parts: []expansion.PartSelection{{Role: "video", PartID: strings.Repeat("c", 64)}}, ShellSHA256: strings.Repeat("d", 64), PayloadSHA256: strings.Repeat("e", 64), PayloadSize: 40408}
	c.ID, _ = expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
	return protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 7, ABI: protocol.RuntimeContract{ID: "fes.application", Major: 1}, BuildID: strings.Repeat("b", 32), PersistenceMode: "volatile", ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.video.fixed-720p60", Major: 1}}, PartsComposition: &c}}
}

func TestLibraryPartsClientCarriesExactIdentityAndRejectsOtherReceipts(t *testing.T) {
	for _, name := range []string{"library", "wrong package", "missing parts", "wrong digest", "mixed receipt", "persistence", "lost reply", "ordinary load"} {
		t.Run(name, func(t *testing.T) {
			status := libraryPartsStatus()
			id := status.CorePackage.PackageID
			switch name {
			case "wrong package":
				status.CorePackage.PackageID = strings.Repeat("f", 64)
			case "missing parts":
				status.CorePackage.PartsComposition = nil
			case "wrong digest":
				status.CorePackage.PartsComposition.PayloadSHA256 = strings.Repeat("f", 64)
			case "mixed receipt":
				status.CorePackage.ROMLink = &corepackage.ROMLinkIdentity{}
			case "persistence":
				status.CorePackage.PersistenceMode = "persistent"
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				path := "/v1/library/core/parts"
				if name == "ordinary load" {
					path = "/v1/development/core"
				}
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path != path || r.URL.RawQuery != "" || r.ContentLength != 5 || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Content-Type") != "application/octet-stream" || string(body) != "parts" {
					t.Errorf("request %s headers=%v body=%s", r.URL, r.Header, body)
				}
				if name != "ordinary load" && r.Header.Get("X-FogCast-Package-ID") != id {
					t.Error("missing exact package")
				}
				if name == "lost reply" {
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
					return
				}
				json.NewEncoder(w).Encode(status)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := targetclient.NewClient(base, "secret", server.Client())
			var err error
			if name == "ordinary load" {
				_, err = client.LoadCore(context.Background(), 5, strings.NewReader("parts"))
			} else {
				_, err = client.LoadLibraryPartsCore(context.Background(), 5, strings.NewReader("parts"), id)
			}
			if (err == nil) != (name == "library") || calls.Load() != 1 {
				t.Fatalf("error=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestLibraryPartsClientUsesSharedKitLease(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			io.WriteString(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"parts-owner"}`)
		case "/v1/kit/release":
			io.WriteString(w, `{"state":"free"}`)
		case "/v1/library/core/parts":
			calls.Add(1)
			if r.Header.Get(targetclient.KitLeaseHeader) != "parts-owner" {
				t.Error("missing lease")
			}
			json.NewEncoder(w).Encode(libraryPartsStatus())
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	lease := targetclient.NewKitLease(base, "secret", server.Client(), "test", "video")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(base, "secret", server.Client()).WithKitLease(lease)
	_, err := client.LoadLibraryPartsCore(context.Background(), 5, strings.NewReader("parts"), strings.Repeat("a", 64))
	if err != nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}
