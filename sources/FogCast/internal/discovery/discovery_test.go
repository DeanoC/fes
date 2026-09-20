package discovery

import (
	"context"
	"errors"
	"net"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brutella/dnssd"
)

func TestIDsAreCanonicalUUIDs(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(id) {
		t.Fatalf("NewID() = %q, want canonical UUID", id)
	}
	for _, invalid := range []string{"", "01234567-89AB-cdef-0123-456789abcdef", "0123456789abcdef0123456789abcdef", "01234567-89ab-cdef-0123-456789abcdeg", " 01234567-89ab-cdef-0123-456789abcdef"} {
		if ValidID(invalid) {
			t.Errorf("ValidID(%q) = true", invalid)
		}
	}
}

func TestResolveFiltersIdentityDeduplicatesAndPreservesAmbiguity(t *testing.T) {
	old := lookupType
	t.Cleanup(func() { lookupType = old })
	wantID := "01234567-89ab-cdef-0123-456789abcdef"
	ctx, cancel := context.WithCancel(context.Background())
	lookupType = func(ctx context.Context, service string, add dnssd.AddFunc, _ dnssd.RmvFunc) error {
		if service != serviceFQDN {
			t.Fatalf("service = %q", service)
		}
		add(dnssd.BrowseEntry{Name: "kit-a", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("2001:db8::10")}, Text: map[string]string{"target_id": wantID, "protocol": protocolVersion}})
		add(dnssd.BrowseEntry{Name: "kit-a", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.10")}, Text: map[string]string{"target_id": wantID, "protocol": protocolVersion}})
		add(dnssd.BrowseEntry{Name: "kit-b", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.11")}, Text: map[string]string{"target_id": wantID, "protocol": protocolVersion}})
		add(dnssd.BrowseEntry{Name: "kit-c", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.12")}, Text: map[string]string{"target_id": "ffffffff-ffff-ffff-ffff-ffffffffffff", "protocol": protocolVersion}})
		cancel()
		return ctx.Err()
	}
	got, err := Resolve(ctx, wantID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://192.0.2.10:8182", "http://192.0.2.11:8182"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve = %#v, want %#v", got, want)
	}
}

func TestResolveCancellationWithoutMatches(t *testing.T) {
	old := lookupType
	t.Cleanup(func() { lookupType = old })
	lookupType = func(ctx context.Context, _ string, _ dnssd.AddFunc, _ dnssd.RmvFunc) error { return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, "01234567-89ab-cdef-0123-456789abcdef"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}
}

func TestRunUntilParentDoneCancelsInnerOnDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	innerErr := make(chan error, 1)
	stopped := make(chan struct{})
	err := runUntilParentDone(parent, func(ctx context.Context) error {
		go func() {
			for ctx.Err() != context.Canceled {
				runtime.Gosched()
			}
			close(stopped)
		}()
		<-ctx.Done()
		innerErr <- ctx.Err()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	select {
	case got := <-innerErr:
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("inner error = %v, want canceled", got)
		}
	default:
		t.Fatal("inner did not return")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("brutella-style reader kept spinning after deadline")
	}
}

func TestRunUntilParentDonePreservesParentCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	innerErr := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- runUntilParentDone(parent, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			innerErr <- ctx.Err()
			return ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("lookup did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runUntilParentDone did not return")
	}
	select {
	case got := <-innerErr:
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("inner error = %v, want canceled", got)
		}
	default:
		t.Fatal("inner did not return")
	}
}

func TestRunUntilParentDoneReturnsParentErrorWhenAlreadyDone(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	<-parent.Done()
	called := false
	err := runUntilParentDone(parent, func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if called {
		t.Fatal("lookup ran after parent was already done")
	}
}

func TestLookupTypeUntilParentDoneStopsOnDeadline(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	var lastErr error
	ok := 0
	for i := 0; i < 8; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		err := lookupTypeUntilParentDone(ctx, serviceFQDN, func(dnssd.BrowseEntry) {}, func(dnssd.BrowseEntry) {})
		cancel()
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			ok++
			continue
		}
		lastErr = err
	}
	if ok == 0 {
		t.Skipf("DNS-SD browse unavailable: %v", lastErr)
	}
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()
	if after-before > 8 {
		t.Fatalf("goroutines grew from %d to %d after timed-out browses", before, after)
	}
}

func TestResolvePreservesSameNamedClonesSeenOnDifferentInterfaces(t *testing.T) {
	old := lookupType
	t.Cleanup(func() { lookupType = old })
	wantID := "01234567-89ab-cdef-0123-456789abcdef"
	ctx, cancel := context.WithCancel(context.Background())
	lookupType = func(ctx context.Context, _ string, add dnssd.AddFunc, _ dnssd.RmvFunc) error {
		text := map[string]string{"target_id": wantID, "protocol": protocolVersion}
		add(dnssd.BrowseEntry{Name: "FogCast " + wantID, IfaceName: "eth0", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.10")}, Text: text})
		add(dnssd.BrowseEntry{Name: "FogCast " + wantID, IfaceName: "wlan0", Port: 8182, IPs: []net.IP{net.ParseIP("198.51.100.20")}, Text: text})
		cancel()
		return ctx.Err()
	}
	got, err := Resolve(ctx, wantID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://192.0.2.10:8182", "http://198.51.100.20:8182"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve = %#v, want %#v", got, want)
	}
}

func TestResolveRemovesWithdrawnServiceInstance(t *testing.T) {
	old := lookupType
	t.Cleanup(func() { lookupType = old })
	wantID := "01234567-89ab-cdef-0123-456789abcdef"
	ctx, cancel := context.WithCancel(context.Background())
	lookupType = func(ctx context.Context, _ string, add dnssd.AddFunc, remove dnssd.RmvFunc) error {
		entry := dnssd.BrowseEntry{Name: "kit", IfaceName: "eth0", Port: 8182, IPs: []net.IP{net.ParseIP("192.0.2.10")}, Text: map[string]string{"target_id": wantID, "protocol": protocolVersion}}
		add(entry)
		remove(entry)
		cancel()
		return ctx.Err()
	}
	got, err := Resolve(ctx, wantID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Resolve retained withdrawn endpoint: %#v", got)
	}
}

func TestAdvertiseContainsOnlyIdentityAndProtocolAndStopsWithContext(t *testing.T) {
	old := newAdvertiser
	oldFingerprint := networkFingerprint
	oldRandom := readRandom
	t.Cleanup(func() { newAdvertiser = old; networkFingerprint = oldFingerprint; readRandom = oldRandom })
	readRandom = func(bytes []byte) (int, error) {
		for i := range bytes {
			bytes[i] = 0xab
		}
		return len(bytes), nil
	}
	networkFingerprint = func() string { return "eth0|192.0.2.10/24" }
	wantID := "01234567-89ab-cdef-0123-456789abcdef"
	called := false
	ctx, cancel := context.WithCancel(context.Background())
	newAdvertiser = func(cfg advertisementConfig) (advertiser, error) {
		called = true
		if cfg.port != 8182 || cfg.name != "FogCast abababababab" || cfg.host != "fogcast-abababababab.local." {
			t.Fatalf("config = %#v", cfg)
		}
		wantText := []string{"protocol=" + protocolVersion, "target_id=" + wantID}
		if !reflect.DeepEqual(cfg.text, wantText) {
			t.Fatalf("TXT = %#v, want %#v", cfg.text, wantText)
		}
		return fakeAdvertiser{respond: func(ctx context.Context) error {
			cancel()
			return ctx.Err()
		}}, nil
	}
	if err := Advertise(ctx, wantID, 8182); !errors.Is(err, context.Canceled) {
		t.Fatalf("Advertise error = %v", err)
	}
	if !called {
		t.Fatal("advertiser was not created")
	}
}

func TestAdvertisementNamesDistinguishCopiedTargetIDs(t *testing.T) {
	oldRandom := readRandom
	t.Cleanup(func() { readRandom = oldRandom })
	fill := byte(0)
	readRandom = func(bytes []byte) (int, error) {
		fill++
		for i := range bytes {
			bytes[i] = fill
		}
		return len(bytes), nil
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	first, err := newAdvertisementConfig(id, 8182)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newAdvertisementConfig(id, 8182)
	if err != nil {
		t.Fatal(err)
	}
	if first.name == second.name || first.host == second.host {
		t.Fatalf("copied targets shared DNS identity: %#v %#v", first, second)
	}
	if !reflect.DeepEqual(first.text, second.text) || !reflect.DeepEqual(first.text, []string{"protocol=1", "target_id=" + id}) {
		t.Fatalf("TXT identity changed: %#v %#v", first.text, second.text)
	}
}

func TestAdvertiseWaitsForUsableNetworkThenRecreatesOnAddressChange(t *testing.T) {
	oldNew := newAdvertiser
	oldFingerprint := networkFingerprint
	oldPoll := advertisementPollInterval
	t.Cleanup(func() {
		newAdvertiser = oldNew
		networkFingerprint = oldFingerprint
		advertisementPollInterval = oldPoll
	})
	advertisementPollInterval = time.Millisecond
	var mu sync.Mutex
	fingerprint := ""
	networkFingerprint = func() string { mu.Lock(); defer mu.Unlock(); return fingerprint }
	started := make(chan int, 2)
	stopped := make(chan int, 2)
	var created atomic.Int32
	newAdvertiser = func(advertisementConfig) (advertiser, error) {
		index := int(created.Add(1))
		return fakeAdvertiser{respond: func(ctx context.Context) error {
			started <- index
			<-ctx.Done()
			stopped <- index
			return ctx.Err()
		}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Advertise(ctx, "01234567-89ab-cdef-0123-456789abcdef", 8182) }()
	time.Sleep(3 * time.Millisecond)
	if created.Load() != 0 {
		t.Fatalf("created %d responders without a usable network", created.Load())
	}
	mu.Lock()
	fingerprint = "eth0|192.0.2.10/24"
	mu.Unlock()
	if got := waitInt(t, started); got != 1 {
		t.Fatalf("first responder = %d", got)
	}
	mu.Lock()
	fingerprint = "eth0|192.0.2.20/24"
	mu.Unlock()
	if got := waitInt(t, stopped); got != 1 {
		t.Fatalf("stopped responder = %d", got)
	}
	if got := waitInt(t, started); got != 2 {
		t.Fatalf("replacement responder = %d", got)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Advertise error = %v", err)
	}
	if got := waitInt(t, stopped); got != 2 {
		t.Fatalf("final stopped responder = %d", got)
	}
}

func TestAdvertiseRetriesResponderStartupFailure(t *testing.T) {
	oldNew := newAdvertiser
	oldFingerprint := networkFingerprint
	oldRetry := advertisementRetryInitial
	t.Cleanup(func() {
		newAdvertiser = oldNew
		networkFingerprint = oldFingerprint
		advertisementRetryInitial = oldRetry
	})
	networkFingerprint = func() string { return "eth0|192.0.2.10/24" }
	advertisementRetryInitial = time.Millisecond
	var attempts atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	newAdvertiser = func(advertisementConfig) (advertiser, error) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			return nil, errors.New("join failed")
		}
		return fakeAdvertiser{respond: func(ctx context.Context) error { cancel(); <-ctx.Done(); return ctx.Err() }}, nil
	}
	if err := Advertise(ctx, "01234567-89ab-cdef-0123-456789abcdef", 8182); !errors.Is(err, context.Canceled) {
		t.Fatalf("Advertise error = %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("startup attempts = %d, want 2", attempts.Load())
	}
}

func TestAdvertiseBacksOffAcrossConsecutiveResponderFailures(t *testing.T) {
	oldNew := newAdvertiser
	oldFingerprint := networkFingerprint
	oldWait := advertisementWait
	t.Cleanup(func() { newAdvertiser = oldNew; networkFingerprint = oldFingerprint; advertisementWait = oldWait })
	networkFingerprint = func() string { return "eth0|192.0.2.10/24" }
	newAdvertiser = func(advertisementConfig) (advertiser, error) {
		return fakeAdvertiser{respond: func(context.Context) error { return errors.New("probe failed") }}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	var delays []time.Duration
	advertisementWait = func(ctx context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		if len(delays) == 2 {
			cancel()
			return ctx.Err()
		}
		return nil
	}
	if err := Advertise(ctx, "01234567-89ab-cdef-0123-456789abcdef", 8182); !errors.Is(err, context.Canceled) {
		t.Fatalf("Advertise error = %v", err)
	}
	want := []time.Duration{time.Second, 2 * time.Second}
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("retry delays = %v, want %v", delays, want)
	}
}

func waitInt(t *testing.T, ch <-chan int) int {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for advertiser lifecycle event")
		return 0
	}
}

type fakeAdvertiser struct{ respond func(context.Context) error }

func (a fakeAdvertiser) Respond(ctx context.Context) error { return a.respond(ctx) }
