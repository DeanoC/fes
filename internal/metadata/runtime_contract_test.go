package metadata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type resolvingRuntimeProvider struct {
	*fakeProvider
	mu           sync.Mutex
	resolveCalls int
}

func (p *resolvingRuntimeProvider) ResolvePlatform(context.Context, protocol.System) (string, error) {
	p.mu.Lock()
	p.resolveCalls++
	p.mu.Unlock()
	return "58", nil
}

func (p *resolvingRuntimeProvider) resolutionCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resolveCalls
}

func TestRuntimePreservesWrappedProviderErrorCode(t *testing.T) {
	provider := &fakeProvider{err: fmt.Errorf("provider wrapper: %w", newOpError(ErrUnauthorized, errors.New("secret upstream")))}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, err = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: "megadrive"})
	if opCode(err) != ErrUnauthorized || errors.Is(err, context.DeadlineExceeded) || err.Error() != "metadata: unauthorized" {
		t.Fatalf("wrapped provider error = %v code=%s", err, opCode(err))
	}
}

func TestRuntimeRejectsLookupAfterCloseBeforeResolvingPlatform(t *testing.T) {
	provider := &resolvingRuntimeProvider{fakeProvider: &fakeProvider{result: ProviderResult{PlatformID: "58"}}}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: "megadrive"})
	if opCode(err) != ErrCanceled || provider.resolutionCount() != 0 || provider.callCount() != 0 {
		t.Fatalf("closed lookup err=%v resolve=%d lookup=%d", err, provider.resolutionCount(), provider.callCount())
	}
}

func TestRuntimePurgeRejectsUnsafeParentComponentsWithoutTouchingRedirectedData(t *testing.T) {
	for _, config := range []RuntimeConfig{
		{Configured: false, Enabled: false},
		{Configured: true, Enabled: false},
	} {
		name := "unconfigured"
		if config.Configured {
			name = "disabled"
		}
		t.Run(name+" symlink parent", func(t *testing.T) {
			base := t.TempDir()
			redirect := t.TempDir()
			root := filepath.Join(base, "alias", "metadata")
			artwork := filepath.Join(redirect, "metadata", "artwork")
			if err := os.MkdirAll(artwork, 0o700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(artwork, "must-remain")
			if err := os.WriteFile(sentinel, []byte("redirected"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(redirect, filepath.Join(base, "alias")); err != nil {
				t.Fatal(err)
			}
			if runtime, err := Open(context.Background(), RuntimeConfig{Root: root, Configured: config.Configured, Enabled: config.Enabled, ProviderName: ProviderIGDB}); runtime != nil || opCode(err) != ErrStorage {
				t.Fatalf("unsafe parent purge = runtime:%v err:%v", runtime, err)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "redirected" {
				t.Fatalf("redirected artwork changed: data=%q err=%v", data, err)
			}
		})

		t.Run(name+" non-directory parent", func(t *testing.T) {
			base := t.TempDir()
			parent := filepath.Join(base, "parent-file")
			if err := os.WriteFile(parent, []byte("must-remain"), 0o600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(parent, "metadata")
			if runtime, err := Open(context.Background(), RuntimeConfig{Root: root, Configured: config.Configured, Enabled: config.Enabled, ProviderName: ProviderIGDB}); runtime != nil || opCode(err) != ErrStorage {
				t.Fatalf("non-directory parent purge = runtime:%v err:%v", runtime, err)
			}
			if data, err := os.ReadFile(parent); err != nil || string(data) != "must-remain" {
				t.Fatalf("non-directory parent changed: data=%q err=%v", data, err)
			}
		})
	}
}

func TestPresentationFromCandidatePreservesIndependentOptionalTextFields(t *testing.T) {
	for _, field := range []struct {
		name string
		set  func(*Candidate)
	}{
		{name: "summary", set: func(value *Candidate) { value.Summary = "" }},
		{name: "year", set: func(value *Candidate) { value.FirstReleaseYear = 0 }},
		{name: "genre", set: func(value *Candidate) { value.Genres = nil }},
		{name: "studio", set: func(value *Candidate) { value.Studios = nil }},
		{name: "players", set: func(value *Candidate) { value.Players = "" }},
		{name: "series", set: func(value *Candidate) { value.Series = "" }},
	} {
		t.Run(field.name, func(t *testing.T) {
			candidate := Candidate{
				Summary: "summary", FirstReleaseYear: 1991, Genres: []string{"genre"},
				Studios: []string{"studio"}, Players: "players", Series: "Sonic the Hedgehog",
			}
			field.set(&candidate)
			presentation := presentationFromCandidate(candidate)
			if field.name != "summary" && presentation.Summary != "summary" {
				t.Fatalf("summary was not preserved: %#v", presentation)
			}
			if field.name != "year" && presentation.Year != "1991" {
				t.Fatalf("year was not preserved: %#v", presentation)
			}
			if field.name != "genre" && presentation.Genre != "genre" {
				t.Fatalf("genre was not preserved: %#v", presentation)
			}
			if field.name != "studio" && presentation.Studio != "studio" {
				t.Fatalf("studio was not preserved: %#v", presentation)
			}
			if field.name != "players" && presentation.Players != "players" {
				t.Fatalf("players was not preserved: %#v", presentation)
			}
			if field.name != "series" && presentation.Series != "Sonic the Hedgehog" {
				t.Fatalf("series was not preserved: %#v", presentation)
			}
		})
	}
}

func TestRuntimeCachesNoMatchAndProviderFailureCooldown(t *testing.T) {
	clock := time.Unix(1000, 0)
	provider := &fakeProvider{result: ProviderResult{PlatformID: "58"}}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "Missing", System: "megadrive"})
	if err != nil || result.Outcome != OutcomeNoMatch {
		t.Fatalf("no match = %#v err=%v", result, err)
	}
	if _, err := runtime.Lookup(context.Background(), LookupInput{Title: "Missing", System: "megadrive"}); err != nil {
		t.Fatal(err)
	}
	if provider.callCount() != 1 {
		t.Fatalf("negative cache provider calls = %d", provider.callCount())
	}
	_ = runtime.Close()

	provider = &fakeProvider{err: newOpError(ErrUpstreamUnavailable, nil)}
	runtime, err = Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	for i := 0; i < 2; i++ {
		if _, err := runtime.Lookup(context.Background(), LookupInput{Title: "Offline", System: "megadrive"}); opCode(err) != ErrUpstreamUnavailable {
			t.Fatalf("failure lookup %d = %v", i, err)
		}
	}
	if provider.callCount() != 1 {
		t.Fatalf("failure cooldown provider calls = %d", provider.callCount())
	}
}

type contractErrorResolver struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (p *contractErrorResolver) Name() ProviderName { return ProviderIGDB }
func (p *contractErrorResolver) Lookup(context.Context, ProviderQuery) (ProviderResult, error) {
	return ProviderResult{}, errors.New("lookup must not be reached")
}
func (p *contractErrorResolver) ResolvePlatform(context.Context, protocol.System) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return "", p.err
}
func (p *contractErrorResolver) Close() error { return nil }
func (p *contractErrorResolver) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestContractPlatformResolutionErrorsUseRuntimeLatch(t *testing.T) {
	provider := &contractErrorResolver{err: newOpError(ErrUnauthorized, nil)}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Tails", System: protocol.SystemMegaDrive})
	if provider.callCount() != 1 {
		t.Fatalf("platform-resolution unauthorized error was not latched: calls=%d", provider.callCount())
	}
}

func TestContractRateLimitBackoffIsProviderGlobal(t *testing.T) {
	now := time.Unix(1000, 0)
	provider := &fakeProvider{err: &OpError{Code: ErrRateLimited, RetryAfter: 10 * time.Minute}}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	now = now.Add(31 * time.Second)
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Tails", System: protocol.SystemMegaDrive})
	if provider.callCount() != 1 {
		t.Fatalf("provider-global Retry-After was bypassed by another key: calls=%d", provider.callCount())
	}
}

func TestContractRuntimeClampsProviderRetryAfter(t *testing.T) {
	now := time.Unix(1000, 0)
	provider := &fakeProvider{err: &OpError{Code: ErrRateLimited, RetryAfter: 20 * time.Minute}}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	now = now.Add(16 * time.Minute)
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Tails", System: protocol.SystemMegaDrive})
	if provider.callCount() != 2 {
		t.Fatalf("Retry-After was not clamped to fifteen minutes: calls=%d", provider.callCount())
	}
}

func TestContractRuntimeClampsPositiveSubsecondRetryAfterToOneSecond(t *testing.T) {
	now := time.Unix(1000, 0)
	provider := &fakeProvider{err: &OpError{Code: ErrRateLimited, RetryAfter: 500 * time.Millisecond}}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	now = now.Add(1500 * time.Millisecond)
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Tails", System: protocol.SystemMegaDrive})
	if provider.callCount() != 2 {
		t.Fatalf("positive subsecond Retry-After was not clamped to one second: calls=%d", provider.callCount())
	}
}

func TestContractDeadlineUsesPerKeyAndProviderBackoff(t *testing.T) {
	provider := &fakeProvider{err: newOpError(ErrDeadline, context.DeadlineExceeded)}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	if provider.callCount() != 1 {
		t.Fatalf("deadline bypassed required cooldown/backoff: calls=%d", provider.callCount())
	}
}

func TestContractPlatformDeadlineCooldownUsesSingletonPolicyKey(t *testing.T) {
	now := time.Unix(1000, 0)
	provider := &contractErrorResolver{err: newOpError(ErrDeadline, context.DeadlineExceeded)}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	now = now.Add(2 * time.Second)
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Tails", System: protocol.SystemMegaDrive})
	if provider.callCount() != 1 {
		t.Fatalf("platform deadline cooldown was keyed by title: calls=%d", provider.callCount())
	}
}

type contractRoundTripper func(*http.Request) (*http.Response, error)

func (f contractRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestContractArtworkRejectionRetriesAfterFiveMinutes(t *testing.T) {
	now := time.Unix(1000, 0)
	var mu sync.Mutex
	artworkCalls := 0
	client := &http.Client{Transport: contractRoundTripper(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		artworkCalls++
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("missing")), Request: request}, nil
	})}
	provider := &fakeProvider{result: ProviderResult{
		PlatformID: "58",
		Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}, Artwork: []ArtworkRef{{Role: ArtworkCover, ID: "cover"}}}},
	}}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	first, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	if err != nil || first.Outcome != OutcomeExact {
		t.Fatalf("first lookup=%#v err=%v", first, err)
	}
	now = now.Add(6 * time.Minute)
	second, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	if err != nil || second.Outcome != OutcomeExact {
		t.Fatalf("second lookup=%#v err=%v", second, err)
	}
	mu.Lock()
	calls := artworkCalls
	mu.Unlock()
	if calls != 2 {
		t.Fatalf("artwork was cached missing for the positive TTL instead of retried: calls=%d", calls)
	}
}

func TestContractArtworkStorageRejectionKeepsPositiveResult(t *testing.T) {
	now := time.Unix(1000, 0)
	var artworkCalls atomic.Int32
	secondStarted := make(chan struct{})
	release := make(chan struct{})
	var secondOnce sync.Once
	client := &http.Client{Transport: contractRoundTripper(func(request *http.Request) (*http.Response, error) {
		call := artworkCalls.Add(1)
		if call == 1 {
			return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("missing")), Request: request}, nil
		}
		secondOnce.Do(func() { close(secondStarted) })
		<-release
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(testPNG(t))), Request: request}, nil
	})}
	provider := &fakeProvider{result: ProviderResult{
		PlatformID: "58",
		Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}, Artwork: []ArtworkRef{{Role: ArtworkCover, ID: "cover"}}}},
	}}
	runtimeValue, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtimeValue.(*metadataRuntime)
	first, err := runtimeValue.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	if err != nil || first.Outcome != OutcomeExact {
		runtimeValue.Close()
		t.Fatalf("first lookup=%#v err=%v", first, err)
	}
	now = now.Add(6 * time.Minute)
	var second Result
	var secondErr error
	done := make(chan struct{})
	go func() {
		second, secondErr = runtimeValue.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
		close(done)
	}()
	<-secondStarted
	if err := runtime.cache.Close(); err != nil {
		t.Fatalf("close cache to inject artwork publication failure: %v", err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lookup did not finish after cache publication failure")
	}
	if secondErr != nil || second.Outcome != OutcomeExact || second.Presentation.CoverArtworkID != "" {
		t.Fatalf("artwork storage failure was not nonfatal: result=%#v err=%v", second, secondErr)
	}
	if got := runtime.ObservabilitySnapshot()["artwork_rejected"]; got != 2 {
		t.Fatalf("artwork storage rejection was not counted: %d", got)
	}
	if err := runtimeValue.Close(); err != nil {
		t.Fatal(err)
	}
}

type sequentialProvider struct {
	mu      sync.Mutex
	calls   int
	results []ProviderResult
}

func (p *sequentialProvider) Name() ProviderName { return ProviderIGDB }
func (p *sequentialProvider) Lookup(context.Context, ProviderQuery) (ProviderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	index := p.calls - 1
	if index >= len(p.results) {
		index = len(p.results) - 1
	}
	return p.results[index], nil
}
func (p *sequentialProvider) Close() error { return nil }

func TestContractExpiredPositiveRevalidationReusesUnchangedArtwork(t *testing.T) {
	now := time.Unix(1000, 0)
	var artworkCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		artworkCalls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(testPNG(t))), Request: request}, nil
	})}
	provider := &sequentialProvider{results: []ProviderResult{
		{PlatformID: "58", Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}, Artwork: []ArtworkRef{{Role: ArtworkCover, ID: "cover"}}, Checksum: "old", UpdatedAt: time.Unix(1, 0)}}},
		{PlatformID: "58", Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}, Artwork: []ArtworkRef{{Role: ArtworkCover, ID: "cover"}}, Checksum: "old", UpdatedAt: time.Unix(1, 0)}}},
	}}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	first, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	if err != nil || first.Outcome != OutcomeExact || first.Presentation.CoverArtworkID == "" {
		t.Fatalf("first lookup=%#v err=%v", first, err)
	}
	now = now.Add(31 * 24 * time.Hour)
	second, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
	if err != nil || second.Outcome != OutcomeExact || second.Presentation.CoverArtworkID != first.Presentation.CoverArtworkID {
		t.Fatalf("revalidation lookup=%#v err=%v", second, err)
	}
	if got := artworkCalls.Load(); got != 1 {
		t.Fatalf("unchanged artwork was rebuilt during revalidation: fetches=%d", got)
	}
}

func TestContractExpiredPositiveRevalidationRebuildsArtworkWhenProviderVersionChanges(t *testing.T) {
	now := time.Unix(1000, 0)
	var artworkCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		artworkCalls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(testPNG(t))), Request: request}, nil
	})}
	provider := &sequentialProvider{results: []ProviderResult{
		{PlatformID: "58", Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}, Artwork: []ArtworkRef{{Role: ArtworkCover, ID: "cover"}}, Checksum: "old", UpdatedAt: time.Unix(1, 0)}}},
		{PlatformID: "58", Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}, Artwork: []ArtworkRef{{Role: ArtworkCover, ID: "cover"}}, Checksum: "new", UpdatedAt: time.Unix(2, 0)}}},
	}}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, HTTPClient: client, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * 24 * time.Hour)
	if _, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive}); err != nil {
		t.Fatal(err)
	}
	if got := artworkCalls.Load(); got != 2 {
		t.Fatalf("changed provider version reused stale artwork: fetches=%d", got)
	}
}

func TestContractProvider404PurgesDerivedCache(t *testing.T) {
	provider := &fakeProvider{err: statusError(http.StatusNotFound, make(http.Header))}
	runtimeValue, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeValue.Close()
	runtime := runtimeValue.(*metadataRuntime)
	oldKey, _ := BuildCacheKey(ProviderIGDB, "58", "old", "")
	if err := runtime.cache.PutWithMetadata(oldKey, CacheRecordMetadata{Provider: ProviderIGDB, PlatformID: "58", NormalizedTitle: "old"}, Result{Outcome: OutcomeExact}, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "new", System: protocol.SystemMegaDrive})
	if _, ok, err := runtime.cache.Get(oldKey); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("upstream 404 did not purge provider-derived cache")
	}
}

func TestContractProviderRemovalLatchesAndStopsLaterProviderCalls(t *testing.T) {
	provider := &fakeProvider{err: statusError(http.StatusNotFound, make(http.Header))}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive}); opCode(err) != ErrProviderRemoved {
		t.Fatalf("first provider-removal error = %v", err)
	}
	if _, err := runtime.Lookup(context.Background(), LookupInput{Title: "Tails", System: protocol.SystemMegaDrive}); opCode(err) != ErrProviderRemoved {
		t.Fatalf("latched provider-removal error = %v", err)
	}
	if provider.callCount() != 1 {
		t.Fatalf("provider removal was not latched: calls=%d", provider.callCount())
	}
}

type closeUnblocksProvider struct {
	started     chan struct{}
	closeCalled chan struct{}
	once        sync.Once
}

func (p *closeUnblocksProvider) Name() ProviderName { return ProviderIGDB }
func (p *closeUnblocksProvider) Lookup(ctx context.Context, _ ProviderQuery) (ProviderResult, error) {
	p.once.Do(func() { close(p.started) })
	<-p.closeCalled
	return ProviderResult{}, newOpError(ErrCanceled, context.Canceled)
}
func (p *closeUnblocksProvider) Close() error {
	select {
	case <-p.closeCalled:
	default:
		close(p.closeCalled)
	}
	return nil
}

func TestContractCloseClosesProviderBeforeWaitingForLeaders(t *testing.T) {
	provider := &closeUnblocksProvider{started: make(chan struct{}), closeCalled: make(chan struct{})}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	lookupDone := make(chan struct{})
	go func() {
		_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
		close(lookupDone)
	}()
	<-provider.started
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close() }()
	select {
	case <-provider.closeCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close did not close the provider before waiting for the leader")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after provider transport shutdown")
	}
	select {
	case <-lookupDone:
	case <-time.After(time.Second):
		t.Fatal("leader did not exit after Close")
	}
}

type contractCancelableResolver struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *contractCancelableResolver) Name() ProviderName { return ProviderIGDB }
func (p *contractCancelableResolver) Lookup(context.Context, ProviderQuery) (ProviderResult, error) {
	return ProviderResult{}, errors.New("lookup must not be reached")
}
func (p *contractCancelableResolver) ResolvePlatform(ctx context.Context, _ protocol.System) (string, error) {
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
		return "58", nil
	case <-ctx.Done():
		return "", mapContextError(ctx.Err())
	}
}
func (p *contractCancelableResolver) Close() error { return nil }

func TestContractPlatformResolutionWaiterCancellationDetaches(t *testing.T) {
	provider := &contractCancelableResolver{started: make(chan struct{}), release: make(chan struct{})}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runtime.Lookup(ctx, LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
		done <- err
	}()
	<-provider.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter err=%v", err)
		}
	case <-time.After(200 * time.Millisecond):
		close(provider.release)
		<-done
		_ = runtime.Close()
		t.Fatal("canceled waiter remained blocked in platform resolution")
	}
	close(provider.release)
	_ = runtime.Close()
}

func TestContractObservabilityCountsProviderOutcome(t *testing.T) {
	provider := &fakeProvider{result: ProviderResult{PlatformID: "58"}}
	runtimeValue, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeValue.Close()
	result, err := runtimeValue.Lookup(context.Background(), LookupInput{Title: "Missing", System: protocol.SystemMegaDrive})
	if err != nil || result.Outcome != OutcomeNoMatch {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	snapshot := runtimeValue.(*metadataRuntime).ObservabilitySnapshot()
	if snapshot["no_match"] != 1 {
		t.Fatalf("counter-only observability omitted provider outcome: %#v", snapshot)
	}
}

func TestContractObservabilityCountsCachedPositiveOutcome(t *testing.T) {
	provider := &fakeProvider{result: ProviderResult{PlatformID: "58", Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}}}}}
	runtimeValue, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeValue.Close()
	for i := 0; i < 2; i++ {
		result, err := runtimeValue.Lookup(context.Background(), LookupInput{Title: "Sonic", System: protocol.SystemMegaDrive})
		if err != nil || result.Outcome != OutcomeExact {
			t.Fatalf("lookup %d result=%#v err=%v", i+1, result, err)
		}
	}
	snapshot := runtimeValue.(*metadataRuntime).ObservabilitySnapshot()
	if snapshot["exact"] != 2 || snapshot["cache_hit"] != 1 {
		t.Fatalf("cached positive outcome counters = %#v", snapshot)
	}
}

func TestContractStorageFailureDoesNotPublishProviderResult(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	key, _ := BuildCacheKey(ProviderIGDB, "58", "sonic", "")
	badDigest := strings.Repeat("3", 64)
	goodDigest := strings.Repeat("4", 64)
	sentinel := filepath.Join(root, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(root, "artwork", badDigest)); err != nil {
		t.Fatal(err)
	}
	bad := ArtworkCacheEntry{Handle: badDigest, Role: ArtworkCover, ProviderImageID: "bad", ContentDigest: badDigest, MIME: "image/png", ByteCount: 1, Width: 1, Height: 1}
	if err := cache.Put(key, Result{Outcome: OutcomeExact, Presentation: Presentation{Summary: "old"}}, time.Now().Add(time.Hour), []ArtworkCacheEntry{bad}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artwork", goodDigest), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := ArtworkCacheEntry{Handle: goodDigest, Role: ArtworkCover, ProviderImageID: "good", ContentDigest: goodDigest, MIME: "image/png", ByteCount: 1, Width: 1, Height: 1}
	if err := cache.Put(key, Result{Outcome: OutcomeExact, Presentation: Presentation{Summary: "new"}}, time.Now().Add(time.Hour), []ArtworkCacheEntry{good}); opCode(err) != ErrStorage {
		t.Fatalf("expected storage failure, got %v", err)
	}
	if _, ok, err := cache.Get(key); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("failed write remained serveable")
	}
}

func TestContractDisabledPurgeRemovesAllSQLiteDerivedLeaves(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	if err := os.MkdirAll(filepath.Join(root, "artwork", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"cache.sqlite3",
		"cache.sqlite3-wal",
		"cache.sqlite3-journal",
		"cache.sqlite3-shm",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "artwork", "nested", "derived"), []byte("artwork"), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimeValue, err := Open(context.Background(), RuntimeConfig{Root: root, Configured: false, Enabled: false})
	if runtimeValue != nil || err != nil {
		t.Fatalf("disabled purge = runtime:%v err:%v", runtimeValue, err)
	}
	for _, name := range []string{
		"cache.sqlite3",
		"cache.sqlite3-wal",
		"cache.sqlite3-journal",
		"cache.sqlite3-shm",
	} {
		if _, statErr := os.Lstat(filepath.Join(root, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("derived leaf %q remains: %v", name, statErr)
		}
	}
	if _, statErr := os.Lstat(filepath.Join(root, "artwork")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("artwork residue remains: %v", statErr)
	}
}

func TestContractPurgePreflightsEverySQLiteLeafBeforeRemoval(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "unconfigured"
		if configured {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "metadata")
			if err := os.MkdirAll(filepath.Join(root, "artwork"), 0o700); err != nil {
				t.Fatal(err)
			}
			regularLeaves := []string{"cache.sqlite3", "cache.sqlite3-wal", "cache.sqlite3-shm"}
			for _, leaf := range regularLeaves {
				if err := os.WriteFile(filepath.Join(root, leaf), []byte(leaf), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			external := filepath.Join(t.TempDir(), "external-journal")
			if err := os.WriteFile(external, []byte("must remain"), 0o600); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(root, sqliteJournalLeaf)
			if err := os.Symlink(external, journal); err != nil {
				t.Fatal(err)
			}

			runtimeValue, err := Open(context.Background(), RuntimeConfig{Root: root, Configured: configured, Enabled: false, ProviderName: ProviderIGDB})
			if runtimeValue != nil || opCode(err) != ErrStorage {
				t.Fatalf("unsafe %s purge = runtime:%v err:%v", name, runtimeValue, err)
			}
			for _, leaf := range regularLeaves {
				if data, readErr := os.ReadFile(filepath.Join(root, leaf)); readErr != nil || string(data) != leaf {
					t.Fatalf("preflight removed or changed %q: data=%q err=%v", leaf, data, readErr)
				}
			}
			if data, readErr := os.ReadFile(external); readErr != nil || string(data) != "must remain" {
				t.Fatalf("external journal target changed: data=%q err=%v", data, readErr)
			}
		})
	}
}
