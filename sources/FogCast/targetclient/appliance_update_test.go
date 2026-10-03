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
	var phases []string
	c := NewClient(u, "secret", srv.Client()).WithKitLease(lease).WithProgress(func(phase, detail string) {
		phases = append(phases, phase)
	})
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
	for _, want := range []string{"inspect/before", "upload start", "upload", "staged", "activation response lost", "new boot seen", "lease claimed", "confirm sent", "confirm result", "confirmed"} {
		found := false
		for _, phase := range phases {
			if phase == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing phase %s: %v", want, phases)
		}
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

func TestConfirmApplianceTrialStates(t *testing.T) {
	expected := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	cases := []struct {
		name      string
		states    []string
		wantError bool
		wantPosts int32
		deadline  time.Duration
	}{
		{name: "already confirmed", states: []string{"good"}},
		{name: "unreachable then trial", states: []string{"unreachable", "trial"}, wantPosts: 1},
		{name: "pending then trial", states: []string{"pending", "trial"}, wantPosts: 1},
		{name: "wrong image", states: []string{"wrong"}, wantError: true},
		{name: "trial waits for raw idle", states: []string{"not-ready", "trial"}, wantPosts: 1},
		{name: "conflict until deadline", states: []string{"conflict"}, wantError: true, deadline: 1200 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reads, posts atomic.Int32
			confirmed := atomic.Bool{}
			var phases []string
			state := func() string {
				n := int(reads.Load())
				if n >= len(tc.states) {
					n = len(tc.states) - 1
				}
				return tc.states[n]
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing bearer")
				}
				current := state()
				switch r.URL.Path {
				case "/v1/health":
					if current == "unreachable" {
						reads.Add(1)
						http.Error(w, "offline", 503)
						return
					}
					fmt.Fprint(w, `{"target_id":"kit","boot_id":"boot"}`)
				case "/v1/update":
					image, good, pending := expected, other, ""
					trial, ready := true, true
					switch current {
					case "good":
						trial = false
						good = expected
					case "pending":
						image = other
						pending = expected
						trial = false
					case "wrong":
						image = other
						trial = false
					case "not-ready":
						ready = false
					}
					if confirmed.Load() {
						trial = false
						good = expected
					}
					fmt.Fprintf(w, `{"boot_id":"boot","image_sha256":%q,"good":%q,"pending":%q,"trial":%t,"raw_idle_ready":%t}`, image, good, pending, trial, ready)
					reads.Add(1)
				case "/v1/kit/lease":
					fmt.Fprint(w, `{"state":"free"}`)
				case "/v1/kit/claim":
					fmt.Fprint(w, `{"token":"lease","status":{"state":"held","generation":"g","expires_in_ms":90000}}`)
				case "/v1/kit/release":
					fmt.Fprint(w, `{"state":"free"}`)
				case "/v1/update/confirm":
					posts.Add(1)
					if r.Header.Get(KitLeaseHeader) != "lease" || r.Header.Get("Content-Type") != "application/json" {
						t.Error("confirm headers differ from contract")
					}
					raw, _ := io.ReadAll(r.Body)
					if string(raw) != fmt.Sprintf(`{"boot_id":"boot","image_sha256":%q}`, expected) {
						t.Errorf("confirm body: %s", raw)
					}
					if current == "conflict" {
						w.WriteHeader(409)
						fmt.Fprint(w, `{"error":{"code":"UPDATE_CONFLICT","message":"retry"}}`)
						return
					}
					confirmed.Store(true)
					fmt.Fprint(w, `{"confirmed":true}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			u, _ := url.Parse(srv.URL)
			lease := NewKitLease(u, "secret", srv.Client(), "test", "confirm")
			c := NewClient(u, "secret", srv.Client()).WithKitLease(lease).WithProgress(func(phase, detail string) { phases = append(phases, phase) })
			defer lease.Close(context.Background())
			limit := tc.deadline
			if limit == 0 {
				limit = 3 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), limit)
			defer cancel()
			got, err := c.ConfirmApplianceTrial(ctx, "kit", expected, nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("status=%+v err=%v", got, err)
			}
			if tc.name == "conflict until deadline" && posts.Load() > 0 && posts.Load() <= 2 {
				// The one-second retry interval permits a second attempt near the deadline.
			} else if posts.Load() != tc.wantPosts {
				t.Errorf("posts=%d want=%d", posts.Load(), tc.wantPosts)
			}
			if !tc.wantError && (got.Trial || got.Good != expected) {
				t.Errorf("not durable: %+v", got)
			}
			if tc.name == "unreachable then trial" {
				for _, want := range []string{"old boot gone", "new boot seen", "confirm sent", "confirmed"} {
					found := false
					for _, phase := range phases {
						if phase == want {
							found = true
						}
					}
					if !found {
						t.Errorf("missing progress phase %s: %v", want, phases)
					}
				}
			}
		})
	}
}
