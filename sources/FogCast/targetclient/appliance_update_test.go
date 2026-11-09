package targetclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/protocol"
)

func updateManifest(body string) appliance.Manifest {
	return appliance.Manifest{Format: 1, Board: "de10-nano", BootABI: "fes-bootstrap-v1", Version: "test", ImageSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), ImageSize: int64(len(body)), KernelSHA256: strings.Repeat("a", 64), FESRevision: strings.Repeat("b", 40), FogCastRevision: strings.Repeat("c", 40), RuntimeRevision: strings.Repeat("d", 40)}
}

func TestUpdateObservesLostActivationAndConfirmationRepliesUsingFreshLease(t *testing.T) {
	for _, board := range []string{appliance.Board, appliance.BoardDE25} {
		t.Run(board, func(t *testing.T) { updateLostRepliesForBoard(t, board) })
	}
}

func updateLostRepliesForBoard(t *testing.T, board string) {
	body := strings.Repeat("x", 4096)
	m := updateManifest(body)
	m.Board = board
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
			fmt.Fprintf(w, `{"board":%q,"boot_id":%q,"image_sha256":%q,"trial":%t,"raw_idle_ready":true,"good":%q}`, board, boot, image, rebooted.Load() && !confirmed.Load(), image)
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
	var got ApplianceStatus
	var err error
	if board == appliance.Board {
		got, err = c.UpdateAppliance(ctx, "kit", m, strings.NewReader(body), nil)
	} else {
		got, err = c.UpdateApplianceForBoard(ctx, "kit", m, strings.NewReader(body), nil, board)
	}
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

func TestRollbackJSONBusyRejectionDoesNotWaitForBoot(t *testing.T) {
	var healthReads, updateReads, maintenanceReads, claims, rollbacks atomic.Int32
	current, previous := strings.Repeat("a", 64), strings.Repeat("b", 64)
	transport := rollbackTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing authentication")
		}
		status := http.StatusOK
		body := "{}"
		switch r.URL.Path {
		case "/v1/health":
			healthReads.Add(1)
			body = `{"target_id":"kit","boot_id":"boot-a"}`
		case "/v1/update":
			updateReads.Add(1)
			body = fmt.Sprintf(`{"boot_id":"boot-a","image_sha256":%q,"trial":false,"raw_idle_ready":true,"good":%q,"previous":%q}`, current, current, previous)
		case "/v1/kit/maintenance":
			maintenanceReads.Add(1)
			body = `{"state":"blocked","operation_id":"failed-software-activation"}`
		case "/v1/kit/claim":
			claims.Add(1)
			t.Errorf("rollback tried to claim a lease held by blocked maintenance")
			status = http.StatusConflict
			body = `{"code":"KIT_LEASE_BUSY","message":"maintenance is blocked"}`
		case "/v1/update/rollback":
			rollbacks.Add(1)
			status = http.StatusServiceUnavailable
			encoded, _ := json.Marshal(protocol.ErrorEnvelope{Error: protocol.APIError{Code: protocol.CodeBusy, Message: "kit software activation blocks mutations"}})
			body = string(encoded)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			status = http.StatusNotFound
		}
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	u, _ := url.Parse("http://kit.invalid")
	httpClient := &http.Client{Transport: transport}
	lease := NewKitLease(u, "secret", httpClient, "test", "update")
	c := NewClient(u, "secret", httpClient).WithKitLease(lease)
	defer c.InvalidateKitSession()
	var phases []string
	c.WithProgress(func(phase, detail string) { phases = append(phases, phase) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := c.RollbackAppliance(ctx, "kit", nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBusy {
		t.Fatalf("rollback error = %v, want typed BUSY response", err)
	}
	if status.BootID != "boot-a" || status.ImageSHA256 != current || healthReads.Load() != 1 || updateReads.Load() != 1 || maintenanceReads.Load() != 1 || claims.Load() != 0 || rollbacks.Load() != 1 {
		t.Fatalf("status=%+v health=%d update=%d maintenance=%d claims=%d rollback=%d", status, healthReads.Load(), updateReads.Load(), maintenanceReads.Load(), claims.Load(), rollbacks.Load())
	}
	for _, phase := range phases {
		if strings.HasPrefix(phase, "waiting") || phase == "activation response lost" {
			t.Fatalf("definite rejection entered reboot observation: %v", phases)
		}
	}
}

func TestRollbackAcceptedJSON503IsReconciledAsAmbiguous(t *testing.T) {
	var healthReads, updateReads, claims, rollbacks atomic.Int32
	var rebooted atomic.Bool
	current, previous := strings.Repeat("a", 64), strings.Repeat("b", 64)
	transport := rollbackTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing authentication")
		}
		status := http.StatusOK
		body := "{}"
		switch r.URL.Path {
		case "/v1/health":
			healthReads.Add(1)
			boot := "boot-a"
			if rebooted.Load() {
				boot = "boot-b"
			}
			body = fmt.Sprintf(`{"target_id":"kit","boot_id":%q}`, boot)
		case "/v1/update":
			updateReads.Add(1)
			boot, image, old := "boot-a", current, previous
			if rebooted.Load() {
				boot, image = "boot-b", previous
				old = current
			}
			body = fmt.Sprintf(`{"boot_id":%q,"image_sha256":%q,"trial":false,"raw_idle_ready":true,"good":%q,"previous":%q}`, boot, image, image, old)
		case "/v1/kit/maintenance":
			body = `{"state":"none"}`
		case "/v1/kit/claim":
			claims.Add(1)
			body = `{"token":"rollback-lease","status":{"state":"held","generation":"rollback-generation","expires_in_ms":90000}}`
		case "/v1/update/rollback":
			rollbacks.Add(1)
			if r.Header.Get(KitLeaseHeader) != "rollback-lease" {
				t.Error("rollback lacked its current execution lease")
			}
			// The server may have selected the previous image before its
			// structured 503 response was produced. The client must inspect the
			// boot rather than classify this as a definite rejection.
			rebooted.Store(true)
			status = http.StatusServiceUnavailable
			encoded, _ := json.Marshal(protocol.ErrorEnvelope{Error: protocol.APIError{Code: "ROLLBACK_ACCEPTED", Message: "rollback accepted; rebooting"}})
			body = string(encoded)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			status = http.StatusNotFound
		}
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	u, _ := url.Parse("http://kit.invalid")
	httpClient := &http.Client{Transport: transport}
	lease := NewKitLease(u, "secret", httpClient, "test", "update")
	c := NewClient(u, "secret", httpClient).WithKitLease(lease)
	defer c.InvalidateKitSession()
	var phases []string
	c.WithProgress(func(phase, detail string) { phases = append(phases, phase) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := c.RollbackAppliance(ctx, "kit", nil)
	if err != nil || status.BootID != "boot-b" || status.ImageSHA256 != previous || status.Good != previous {
		t.Fatalf("accepted 503 outcome status=%+v err=%v", status, err)
	}
	if healthReads.Load() < 2 || updateReads.Load() < 2 || claims.Load() != 1 || rollbacks.Load() != 1 {
		t.Fatalf("health=%d update=%d claims=%d rollback=%d", healthReads.Load(), updateReads.Load(), claims.Load(), rollbacks.Load())
	}
	for _, phase := range phases {
		if phase == "activation rejected" {
			t.Fatalf("accepted 503 was treated as rejection: %v", phases)
		}
	}
	if !slices.Contains(phases, "activation outcome uncertain") || !slices.Contains(phases, "confirmed") {
		t.Fatalf("accepted 503 was not reconciled to the new boot: %v", phases)
	}
}

func TestRollbackDoesNotBypassActiveSoftwareMaintenance(t *testing.T) {
	var claims, rollbacks atomic.Int32
	current, previous := strings.Repeat("a", 64), strings.Repeat("b", 64)
	transport := rollbackTransport(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := `{}`
		switch r.URL.Path {
		case "/v1/health":
			body = `{"target_id":"kit","boot_id":"boot-a"}`
		case "/v1/update":
			body = fmt.Sprintf(`{"boot_id":"boot-a","image_sha256":%q,"trial":false,"raw_idle_ready":true,"good":%q,"previous":%q}`, current, current, previous)
		case "/v1/kit/maintenance":
			body = `{"state":"ready"}`
		case "/v1/kit/claim":
			claims.Add(1)
			t.Errorf("active maintenance attempted an execution lease claim")
		case "/v1/update/rollback":
			rollbacks.Add(1)
			t.Errorf("rollback ran while software maintenance remained active")
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			status = http.StatusNotFound
		}
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	u, _ := url.Parse("http://kit.invalid")
	httpClient := &http.Client{Transport: transport}
	lease := NewKitLease(u, "secret", httpClient, "test", "update")
	c := NewClient(u, "secret", httpClient).WithKitLease(lease)
	defer c.InvalidateKitSession()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.RollbackAppliance(ctx, "kit", nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBusy || claims.Load() != 0 || rollbacks.Load() != 0 {
		t.Fatalf("active maintenance result = %v, claims=%d rollbacks=%d", err, claims.Load(), rollbacks.Load())
	}
}

type rollbackTransport func(*http.Request) (*http.Response, error)

func (f rollbackTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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

func TestRollbackRetriesMalformed503AfterStatusProvesNoActivation(t *testing.T) {
	oldImage := strings.Repeat("a", 64)
	previous := strings.Repeat("b", 64)
	var rollbacks, statusReads atomic.Int32
	var rebooted atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		boot, image, good, prior, pending := "old", oldImage, oldImage, previous, ""
		if rebooted.Load() {
			boot, image, good, prior = "new", previous, previous, oldImage
		}
		switch r.URL.Path {
		case "/v1/health":
			fmt.Fprintf(w, `{"target_id":"kit","boot_id":%q}`, boot)
		case "/v1/update":
			statusReads.Add(1)
			fmt.Fprintf(w, `{"boot_id":%q,"image_sha256":%q,"trial":false,"raw_idle_ready":true,"good":%q,"previous":%q,"pending":%q}`, boot, image, good, prior, pending)
		case "/v1/update/rollback":
			n := rollbacks.Add(1)
			if n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprint(w, "kit restarting")
				return
			}
			rebooted.Store(true)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := NewClient(u, "secret", srv.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := c.RollbackAppliance(ctx, "kit", nil)
	if err != nil || got.BootID != "new" || got.ImageSHA256 != previous {
		t.Fatalf("rollback result %+v, err=%v", got, err)
	}
	if rollbacks.Load() != 2 {
		t.Fatalf("rollback requests=%d, want 2", rollbacks.Load())
	}
	if statusReads.Load() < 3 {
		t.Fatalf("status reads=%d, want initial read plus recovery observations", statusReads.Load())
	}
}

func TestRollbackDoesNotRetryWhen503MayHaveCommitted(t *testing.T) {
	for _, tc := range []struct {
		name         string
		responseBody string
	}{
		{name: "valid JSON envelope", responseBody: `{"error":{"code":"TEMPORARY","message":"rollback response unavailable"}}`},
		{name: "malformed body", responseBody: "kit restarting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldImage := strings.Repeat("c", 64)
			previous := strings.Repeat("d", 64)
			var rollbacks atomic.Int32
			var rebooted atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				boot, image, good, prior, pending := "old", oldImage, oldImage, previous, ""
				if rebooted.Load() {
					boot, image, good, prior = "new", previous, previous, oldImage
				}
				switch r.URL.Path {
				case "/v1/health":
					fmt.Fprintf(w, `{"target_id":"kit","boot_id":%q}`, boot)
				case "/v1/update":
					fmt.Fprintf(w, `{"boot_id":%q,"image_sha256":%q,"trial":false,"raw_idle_ready":true,"good":%q,"previous":%q,"pending":%q}`, boot, image, good, prior, pending)
				case "/v1/update/rollback":
					rollbacks.Add(1)
					rebooted.Store(true)
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = fmt.Fprint(w, tc.responseBody)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			u, _ := url.Parse(srv.URL)
			c := NewClient(u, "secret", srv.Client())
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			got, err := c.RollbackAppliance(ctx, "kit", nil)
			if err != nil || got.BootID != "new" || got.ImageSHA256 != previous {
				t.Fatalf("rollback result %+v, err=%v", got, err)
			}
			if rollbacks.Load() != 1 {
				t.Fatalf("rollback requests=%d, want no duplicate after changed boot", rollbacks.Load())
			}
		})
	}
}

func TestRollbackPreservesExplicitRejection(t *testing.T) {
	oldImage := strings.Repeat("e", 64)
	previous := strings.Repeat("f", 64)
	var rollbacks, statusReads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			fmt.Fprint(w, `{"target_id":"kit","boot_id":"old"}`)
		case "/v1/update":
			statusReads.Add(1)
			fmt.Fprintf(w, `{"boot_id":"old","image_sha256":%q,"trial":false,"raw_idle_ready":true,"good":%q,"previous":%q}`, oldImage, oldImage, previous)
		case "/v1/update/rollback":
			rollbacks.Add(1)
			w.WriteHeader(http.StatusConflict)
			_, _ = fmt.Fprint(w, `{"error":{"code":"UPDATE_CONFLICT","message":"rollback rejected"}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := NewClient(u, "secret", srv.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := c.RollbackAppliance(ctx, "kit", nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "UPDATE_CONFLICT" {
		t.Fatalf("rollback result %+v, err=%v, want typed UPDATE_CONFLICT rejection", got, err)
	}
	if rollbacks.Load() != 1 {
		t.Fatalf("rollback requests=%d, want 1 for explicit rejection", rollbacks.Load())
	}
	if statusReads.Load() != 1 {
		t.Fatalf("status reads=%d, want only the initial read after explicit rejection", statusReads.Load())
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

// gatedUpload returns one chunk, then blocks until released, so the
// transport is still inside Read when an early rejection returns from Do.
type gatedUpload struct {
	first   bool
	entered chan struct{}
	release chan struct{}
}

func (r *gatedUpload) Read(b []byte) (int, error) {
	if !r.first {
		r.first = true
		n := min(len(b), 4096)
		for i := range n {
			b[i] = 'x'
		}
		return n, nil
	}
	close(r.entered)
	<-r.release
	n := min(len(b), 4096)
	for i := range n {
		b[i] = 'x'
	}
	return n, nil
}

func TestStageProgressStopsWhenEarlyRejectionReturns(t *testing.T) {
	// The released second chunk completes the body, which would report 100%.
	body := strings.Repeat("x", 8192)
	m := updateManifest(body)
	upload := &gatedUpload{entered: make(chan struct{}), release: make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-upload.entered
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"BAD_REQUEST","message":"rejected early"}}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	var mu sync.Mutex
	var phases []string
	returned := false
	c := NewClient(u, "secret", srv.Client()).WithProgress(func(phase, detail string) {
		mu.Lock()
		defer mu.Unlock()
		if returned {
			t.Errorf("progress %q reported after StageAppliance returned", phase)
		}
		phases = append(phases, phase)
	})
	if err := c.StageAppliance(context.Background(), m, upload); err == nil {
		t.Fatal("expected early rejection")
	}
	mu.Lock()
	returned = true
	got := append([]string(nil), phases...)
	mu.Unlock()
	upload.release <- struct{}{}
	time.Sleep(100 * time.Millisecond)
	if len(got) == 0 || got[0] != "upload start" {
		t.Fatalf("phases = %v", got)
	}
}
