package targetclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance"
)

func updateManifest(body string) appliance.Manifest {
	return appliance.Manifest{Format: 1, Board: "de10-nano", BootABI: "fes-bootstrap-v1", Version: "test", ImageSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), ImageSize: int64(len(body)), KernelSHA256: strings.Repeat("a", 64), FESRevision: strings.Repeat("b", 40), FogCastRevision: strings.Repeat("c", 40), RuntimeRevision: strings.Repeat("d", 40)}
}

func TestUpdateObservesLostActivationAndConfirmationRepliesUsingFreshLease(t *testing.T) {
	body := strings.Repeat("x", 4096)
	m := updateManifest(body)
	var claims, activations, confirms, ownershipReads, postConfirmStatusReads atomic.Int32
	var rebooted, confirmed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing authentication")
		}
		boot := "old"
		if rebooted.Load() {
			boot = "new"
		}
		switch r.URL.Path {
		case "/v1/health":
			fmt.Fprintf(w, `{"target_id":"kit","boot_id":%q}`, boot)
		case "/v1/kit/lease":
			if ownershipReads.Add(1) == 1 {
				fmt.Fprint(w, `{"state":"revoking"}`)
				return
			}
			fmt.Fprint(w, `{"state":"free"}`)
		case "/v1/kit/claim":
			n := claims.Add(1)
			fmt.Fprintf(w, `{"token":"lease-%d","status":{"state":"held","generation":"g%d","expires_in_ms":90000}}`, n, n)
		case "/v1/update":
			image := strings.Repeat("f", 64)
			if rebooted.Load() {
				image = m.ImageSHA256
			}
			if confirmed.Load() && postConfirmStatusReads.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"error":{"code":"TEMPORARY","message":"retry"}}`)
				return
			}
			fmt.Fprintf(w, `{"boot_id":%q,"image_sha256":%q,"trial":%t,"raw_idle_ready":true,"good":%q}`, boot, image, rebooted.Load() && !confirmed.Load(), image)
		case "/v1/update/stage":
			if r.Header.Get(KitLeaseHeader) != "lease-1" {
				t.Error("stage lacks current lease")
			}
			raw, err := base64.StdEncoding.DecodeString(r.Header.Get("X-FogCast-Release-Manifest"))
			if err != nil {
				t.Fatal(err)
			}
			var got appliance.Manifest
			if json.Unmarshal(raw, &got) != nil || got != m {
				t.Error("manifest changed")
			}
			bytes, _ := io.ReadAll(r.Body)
			if string(bytes) != body || r.ContentLength != m.ImageSize {
				t.Error("image changed")
			}
			fmt.Fprint(w, `{}`)
		case "/v1/update/activate":
			activations.Add(1)
			rebooted.Store(true)
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		case "/v1/update/confirm":
			if ownershipReads.Load() < 2 {
				t.Error("confirmation raced startup lease cleanup")
			}
			confirms.Add(1)
			if r.Header.Get(KitLeaseHeader) != "lease-2" {
				t.Error("old lease survived reboot")
			}
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			if req["boot_id"] != "new" || req["image_sha256"] != m.ImageSHA256 {
				t.Error("wrong confirmation identity")
			}
			confirmed.Store(true)
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	lease := NewKitLease(u, "secret", srv.Client(), "test", "update")
	c := NewClient(u, "secret", srv.Client()).WithKitLease(lease)
	defer c.InvalidateKitSession()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := c.UpdateAppliance(ctx, "kit", m, strings.NewReader(body), nil)
	if err != nil || got.Trial || got.ImageSHA256 != m.ImageSHA256 {
		t.Fatalf("result %+v, %v", got, err)
	}
	if claims.Load() != 2 || activations.Load() != 1 || confirms.Load() != 1 {
		t.Fatalf("claims=%d activate=%d confirm=%d", claims.Load(), activations.Load(), confirms.Load())
	}
}

func TestUpdateAcceptsAlreadyConfirmedObservationWithoutRepeatingConfirmation(t *testing.T) {
	body := strings.Repeat("x", 4096)
	m := updateManifest(body)
	var rebooted, confirmed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing authentication")
		}
		boot := "old"
		image := strings.Repeat("f", 64)
		if rebooted.Load() {
			boot = "new"
			image = m.ImageSHA256
		}
		switch r.URL.Path {
		case "/v1/health":
			fmt.Fprintf(w, `{"target_id":"kit","boot_id":%q}`, boot)
		case "/v1/update":
			fmt.Fprintf(w, `{"boot_id":%q,"image_sha256":%q,"trial":false,"raw_idle_ready":true,"good":%q}`, boot, image, image)
		case "/v1/kit/claim":
			fmt.Fprint(w, `{"token":"lease","status":{"state":"held","generation":"g","expires_in_ms":90000}}`)
		case "/v1/update/stage":
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{}`)
		case "/v1/update/activate":
			rebooted.Store(true)
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		case "/v1/update/confirm":
			confirmed.Store(true)
			t.Error("already confirmed boot must not be confirmed again")
			fmt.Fprint(w, `{"confirmed":true}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	lease := NewKitLease(u, "secret", srv.Client(), "test", "update")
	c := NewClient(u, "secret", srv.Client()).WithKitLease(lease)
	defer c.InvalidateKitSession()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := c.UpdateAppliance(ctx, "kit", m, strings.NewReader(body), nil)
	if err != nil || got.Trial || got.ImageSHA256 != m.ImageSHA256 || got.Good != m.ImageSHA256 || confirmed.Load() {
		t.Fatalf("result %+v, err=%v, confirmed=%v", got, err, confirmed.Load())
	}
}

func TestUpdateRejectsWrongTargetBeforeClaimOrUpload(t *testing.T) {
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			mutations++
		}
		fmt.Fprint(w, `{"target_id":"someone-else","boot_id":"old"}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := NewClient(u, "secret", srv.Client())
	_, err := c.UpdateAppliance(context.Background(), "kit", updateManifest(strings.Repeat("x", 4096)), strings.NewReader("x"), nil)
	if err == nil || mutations != 0 {
		t.Fatalf("err=%v mutations=%d", err, mutations)
	}
}

func TestInspectApplianceRediscoversRecordedIdentityWithoutMutation(t *testing.T) {
	old := httptest.NewServer(http.NotFoundHandler())
	defer old.Close()
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			mutations++
		}
		switch r.URL.Path {
		case "/v1/health":
			fmt.Fprint(w, `{"target_id":"kit","boot_id":"new"}`)
		case "/v1/update":
			fmt.Fprintf(w, `{"boot_id":"new","image_sha256":%q,"trial":true}`, strings.Repeat("a", 64))
		case "/v1/kit/lease":
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(old.URL)
	c := NewClient(u, "secret", srv.Client())
	status, err := c.InspectAppliance(context.Background(), "kit", func(context.Context, string) ([]string, error) { return []string{srv.URL}, nil })
	if err != nil || !status.Trial || c.EndpointURL().String() != srv.URL || mutations != 0 {
		t.Fatalf("status=%+v err=%v mutations=%d", status, err, mutations)
	}
}
