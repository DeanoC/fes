package fogcast

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

const reprobeID = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"

type reprobeTransport struct {
	base            http.RoundTripper
	fail, health    atomic.Int32
	cancel          context.CancelFunc
	onFailure       func()
	blocked         chan struct{}
	blockAt         int32
	blackhole       string
	healthOrigins   chan string
	healthDeadlines chan time.Time
}

func (tr *reprobeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/v1/health" {
		number := tr.health.Add(1)
		if tr.healthOrigins != nil {
			tr.healthOrigins <- r.URL.Host
		}
		if tr.healthDeadlines != nil {
			deadline, _ := r.Context().Deadline()
			tr.healthDeadlines <- deadline
		}
		if (tr.blockAt != 0 && number == tr.blockAt) || (tr.blackhole != "" && r.URL.Host == tr.blackhole) {
			if tr.blocked != nil {
				close(tr.blocked)
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		for {
			n := tr.fail.Load()
			if n <= 0 {
				break
			}
			if tr.fail.CompareAndSwap(n, n-1) {
				if tr.cancel != nil {
					tr.cancel()
				}
				if tr.onFailure != nil {
					tr.onFailure()
				}
				return nil, &net.OpError{Op: "read", Net: "tcp", Err: errors.New("simulated transport interruption")}
			}
		}
	}
	return tr.base.RoundTrip(r)
}

type reprobeFixture struct {
	service                         *Service
	client                          *targetclient.Client
	transport                       *reprobeTransport
	endpoint, alternate             string
	resets, mutations, resolve      atomic.Int32
	boot, identity, leaseGeneration atomic.Value
	apiVersion                      atomic.Value
	healthStatus                    atomic.Int32
}

func newReprobeFixture(t *testing.T) *reprobeFixture {
	t.Helper()
	f := &reprobeFixture{}
	f.boot.Store("boot-1")
	f.identity.Store(reprobeID)
	f.leaseGeneration.Store("grant-1")
	f.apiVersion.Store("v1")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/health":
			if status := f.healthStatus.Load(); status != 0 {
				w.WriteHeader(int(status))
				_ = json.NewEncoder(w).Encode(protocol.ErrorEnvelope{Error: protocol.APIError{Code: protocol.CodeKitLeaseDenied}})
				return
			}
			_ = json.NewEncoder(w).Encode(protocol.Health{APIVersion: f.apiVersion.Load().(string), TargetID: f.identity.Load().(string), BootID: f.boot.Load().(string), Ready: true})
		case "/v1/kit/lease":
			_ = json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "held", Generation: f.leaseGeneration.Load().(string)})
		case "/v1/kit/claim":
			f.mutations.Add(1)
			_ = json.NewEncoder(w).Encode(kitlease.Grant{Token: "own-token", Status: kitlease.Status{State: "held", Generation: "grant-1", ExpiresInMS: 60000}})
		case "/v1/status":
			_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateActive})
		default:
			if r.Method != http.MethodGet {
				f.mutations.Add(1)
			}
			http.NotFound(w, r)
		}
	})
	old := httptest.NewServer(handler)
	t.Cleanup(old.Close)
	next := httptest.NewServer(handler)
	t.Cleanup(next.Close)
	f.endpoint, f.alternate = old.URL, next.URL
	f.transport = &reprobeTransport{base: http.DefaultTransport}
	hc := &http.Client{Transport: f.transport, Timeout: time.Second}
	base, _ := url.Parse(old.URL)
	lease := targetclient.NewKitLease(base, "secret", hc, "host", "test")
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	client := targetclient.NewClient(base, "secret", hc).WithKitLease(lease)
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, TargetID: reprobeID, Address: old.URL, Agent: "secret"}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	req, _ := http.NewRequest(http.MethodPost, old.URL, nil)
	if err := lease.Authorize(req, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mutations.Store(0)
	f.transport.health.Store(0)
	s.SetTargetReset(func(string) { f.resets.Add(1) })
	s.resolveTarget = func(context.Context, string) ([]string, error) { f.resolve.Add(1); return []string{next.URL}, nil }
	f.service = s
	f.client = client
	return f
}
func TestHealthReprobeKeepsLiveEndpointAfterOneTransportFailure(t *testing.T) {
	f := newReprobeFixture(t)
	f.transport.healthDeadlines = make(chan time.Time, 2)
	f.transport.fail.Store(1)
	if _, err := f.service.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.service.TargetConnection().Address; got != f.endpoint {
		t.Fatalf("endpoint changed to %s", got)
	}
	if f.transport.health.Load() != 2 || f.resolve.Load() != 0 || f.resets.Load() != 0 || f.mutations.Load() != 0 {
		t.Fatalf("health=%d resolve=%d resets=%d mutations=%d", f.transport.health.Load(), f.resolve.Load(), f.resets.Load(), f.mutations.Load())
	}
	if first, second := <-f.transport.healthDeadlines, <-f.transport.healthDeadlines; first.IsZero() || !first.Equal(second) {
		t.Fatalf("probes did not share a deadline: %s, %s", first, second)
	}
}
func TestHealthReprobeExhaustionPreservesAddressChangeInvalidation(t *testing.T) {
	f := newReprobeFixture(t)
	f.transport.fail.Store(2)
	if _, err := f.service.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.service.TargetConnection().Address; got != f.alternate {
		t.Fatalf("endpoint %s", got)
	}
	if f.resets.Load() != 1 || f.resolve.Load() != 1 || f.mutations.Load() != 0 {
		t.Fatalf("resets=%d resolve=%d mutations=%d", f.resets.Load(), f.resolve.Load(), f.mutations.Load())
	}
}
func TestHealthReprobeStillInvalidatesChangedBootOrLease(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*reprobeFixture)
	}{{"boot", func(f *reprobeFixture) { f.boot.Store("boot-2") }}, {"lease", func(f *reprobeFixture) { f.leaseGeneration.Store("foreign-grant") }}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReprobeFixture(t)
			f.transport.fail.Store(1)
			tc.change(f)
			if _, err := f.service.Health(context.Background()); err != nil {
				t.Fatal(err)
			}
			if f.resets.Load() != 1 || f.mutations.Load() != 0 {
				t.Fatalf("resets=%d mutations=%d", f.resets.Load(), f.mutations.Load())
			}
		})
	}
}
func TestHealthWrongIdentityDoesNotReprobe(t *testing.T) {
	f := newReprobeFixture(t)
	f.identity.Store("b54f2c65-cf14-4643-a042-d09825463d44")
	if _, err := f.service.Health(context.Background()); err == nil {
		t.Fatal("wrong identity accepted")
	}
	if f.transport.health.Load() != 2 || f.resolve.Load() != 1 {
		t.Fatalf("health=%d resolve=%d", f.transport.health.Load(), f.resolve.Load())
	}
}
func TestHealthCanceledContextDoesNotDiscoverOrRetry(t *testing.T) {
	f := newReprobeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.service.Health(ctx); err == nil {
		t.Fatal("canceled observation accepted")
	}
	if f.resolve.Load() != 0 || f.resets.Load() != 0 || f.mutations.Load() != 0 {
		t.Fatalf("resolve=%d resets=%d mutations=%d", f.resolve.Load(), f.resets.Load(), f.mutations.Load())
	}
}

func TestHealthCancellationDuringTransportFailureDoesNotDiscoverOrRetry(t *testing.T) {
	f := newReprobeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.transport.cancel = cancel
	f.transport.fail.Store(1)
	if _, err := f.service.Health(ctx); err == nil {
		t.Fatal("canceled observation accepted")
	}
	if f.transport.health.Load() != 1 || f.resolve.Load() != 0 || f.resets.Load() != 0 || f.mutations.Load() != 0 {
		t.Fatalf("health=%d resolve=%d resets=%d mutations=%d", f.transport.health.Load(), f.resolve.Load(), f.resets.Load(), f.mutations.Load())
	}
}

func TestHealthReprobeBlockedRespectsCallerCancelAndDeadline(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			f := newReprobeFixture(t)
			f.transport.fail.Store(1)
			f.transport.blockAt = 2
			f.transport.blocked = make(chan struct{})
			var ctx context.Context
			var cancel context.CancelFunc
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := f.service.Health(ctx); done <- err }()
			select {
			case <-f.transport.blocked:
			case <-time.After(time.Second):
				t.Fatal("retry did not start")
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("interrupted health succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("health ignored caller cancellation")
			}
			if f.transport.health.Load() != 2 || f.resolve.Load() != 0 || f.resets.Load() != 0 || f.mutations.Load() != 0 {
				t.Fatalf("health=%d resolve=%d resets=%d mutations=%d", f.transport.health.Load(), f.resolve.Load(), f.resets.Load(), f.mutations.Load())
			}
		})
	}
}

func TestHealthRefusalsAndProtocolMismatchDoNotReprobe(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newReprobeFixture(t)
			f.healthStatus.Store(int32(status))
			if _, err := f.service.Health(context.Background()); err == nil {
				t.Fatal("refusal accepted")
			}
			// One original request and one to the discovered candidate, no re-probe.
			if f.transport.health.Load() != 2 || f.resolve.Load() != 1 || f.resets.Load() != 0 || f.mutations.Load() != 0 {
				t.Fatalf("health=%d resolve=%d resets=%d mutations=%d", f.transport.health.Load(), f.resolve.Load(), f.resets.Load(), f.mutations.Load())
			}
		})
	}
	t.Run("protocol", func(t *testing.T) {
		f := newReprobeFixture(t)
		f.apiVersion.Store("v999")
		if _, err := f.service.Health(context.Background()); err == nil {
			t.Fatal("incompatible protocol accepted")
		}
		if f.transport.health.Load() != 1 || f.resolve.Load() != 0 || f.resets.Load() != 0 {
			t.Fatalf("health=%d resolve=%d resets=%d", f.transport.health.Load(), f.resolve.Load(), f.resets.Load())
		}
	})
}

func TestHealthReprobePinsEndpointAcrossConcurrentAdoption(t *testing.T) {
	f := newReprobeFixture(t)
	f.transport.healthOrigins = make(chan string, 2)
	var adoptionErr error
	f.transport.onFailure = func() {
		done := make(chan error, 1)
		go func() {
			base, _ := url.Parse(f.alternate)
			_, err := f.client.AdoptEndpoint(context.Background(), base, false)
			done <- err
		}()
		adoptionErr = <-done
	}
	f.transport.fail.Store(1)
	health, address, err := f.service.probeTarget(context.Background(), f.client, TargetConfig{TargetID: reprobeID})
	if adoptionErr != nil || err != nil {
		t.Fatalf("adoption=%v probe=%v", adoptionErr, err)
	}
	if address != f.endpoint || health.TargetID != reprobeID || f.client.EndpointURL().String() != f.alternate || f.transport.health.Load() != 2 {
		t.Fatalf("probe endpoint=%s client endpoint=%s health=%d", address, f.client.EndpointURL(), f.transport.health.Load())
	}
	original, _ := url.Parse(f.endpoint)
	if a, b := <-f.transport.healthOrigins, <-f.transport.healthOrigins; a != original.Host || b != original.Host {
		t.Fatalf("health requests moved: %s then %s", a, b)
	}
}

func TestHealthSlowOldEndpointKeepsDiscoveryBudget(t *testing.T) {
	f := newReprobeFixture(t)
	old, _ := url.Parse(f.endpoint)
	f.transport.blackhole = old.Host
	started := time.Now()
	if _, err := f.service.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("reconciliation took %s", elapsed)
	}
	if got := f.service.TargetConnection().Address; got != f.alternate {
		t.Fatalf("endpoint=%s", got)
	}
	if f.transport.health.Load() != 2 || f.resolve.Load() != 1 || f.resets.Load() != 1 || f.mutations.Load() != 0 {
		t.Fatalf("health=%d resolve=%d resets=%d mutations=%d", f.transport.health.Load(), f.resolve.Load(), f.resets.Load(), f.mutations.Load())
	}
}

func TestHealthBlockedReprobeLeavesDiscoveryBudget(t *testing.T) {
	f := newReprobeFixture(t)
	f.transport.fail.Store(1)
	f.transport.blockAt = 2
	f.transport.blocked = make(chan struct{})
	var discoveryHadTime atomic.Bool
	f.service.resolveTarget = func(ctx context.Context, _ string) ([]string, error) {
		f.resolve.Add(1)
		if ctx.Err() == nil {
			discoveryHadTime.Store(true)
		}
		return []string{f.alternate}, nil
	}
	if _, err := f.service.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.service.TargetConnection().Address; got != f.alternate {
		t.Fatalf("endpoint=%s", got)
	}
	if f.transport.health.Load() != 3 || f.resolve.Load() != 1 || !discoveryHadTime.Load() || f.resets.Load() != 1 || f.mutations.Load() != 0 {
		t.Fatalf("health=%d resolve=%d discovery live=%v resets=%d mutations=%d", f.transport.health.Load(), f.resolve.Load(), discoveryHadTime.Load(), f.resets.Load(), f.mutations.Load())
	}
}

func TestTransientHealthTransportRejectsApplicationFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"network", &url.Error{Op: "Get", Err: &net.OpError{Op: "read", Err: errors.New("reset")}}, true},
		{"timeout", &url.Error{Op: "Get", Err: context.DeadlineExceeded}, true},
		{"eof", &url.Error{Op: "Get", Err: io.EOF}, true},
		{"temporary dns", &url.Error{Op: "Get", Err: &net.DNSError{IsTemporary: true}}, true},
		{"missing dns", &url.Error{Op: "Get", Err: &net.OpError{Op: "dial", Err: &net.DNSError{IsNotFound: true}}}, false},
		{"cancel", &url.Error{Op: "Get", Err: context.Canceled}, false},
		{"authentication", &protocol.APIError{Code: protocol.CodeKitLeaseDenied}, false},
		{"malformed", errors.New("decode HTTP 200 response"), false},
		{"redirect", &url.Error{Op: "Get", Err: http.ErrUseLastResponse}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := transientHealthTransport(tc.err); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
