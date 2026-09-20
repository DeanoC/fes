package metadata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeProvider struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	result  ProviderResult
	err     error
}

func (p *fakeProvider) Name() ProviderName { return ProviderIGDB }
func (p *fakeProvider) Lookup(ctx context.Context, _ ProviderQuery) (ProviderResult, error) {
	p.mu.Lock()
	p.calls++
	if p.started != nil {
		select {
		case <-p.started:
		default:
			close(p.started)
		}
	}
	release := p.release
	p.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ProviderResult{}, ctx.Err()
		}
	}
	return p.result, p.err
}
func (p *fakeProvider) Close() error   { return nil }
func (p *fakeProvider) callCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.calls }

func TestRuntimeDisabledPurgesDerivedFilesWithoutCreatingCache(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	if err := os.MkdirAll(filepath.Join(root, "artwork"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cache.sqlite3"), []byte("derived"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: root, Configured: true, Enabled: false, ProviderName: ProviderIGDB})
	if err != nil || runtime != nil {
		t.Fatalf("Open disabled = runtime:%v err:%v", runtime, err)
	}
	if _, err := os.Stat(filepath.Join(root, "cache.sqlite3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache residue = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "artwork")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artwork residue = %v", err)
	}
}

func TestRuntimeSingleFlightKeepsWaiterCancellationIndependent(t *testing.T) {
	provider := &fakeProvider{
		started: make(chan struct{}), release: make(chan struct{}),
		result: ProviderResult{PlatformID: "58", Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}, Summary: "summary"}}},
	}
	root := filepath.Join(t.TempDir(), "metadata")
	runtime, err := Open(context.Background(), RuntimeConfig{Root: root, Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider, Now: func() time.Time { return time.Unix(1000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	leaderDone := make(chan struct{})
	go func() {
		_, _ = runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: "megadrive"})
		close(leaderDone)
	}()
	<-provider.started
	waiterCtx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := runtime.Lookup(waiterCtx, LookupInput{Title: "Sonic", System: "megadrive"})
		waiterDone <- err
	}()
	cancel()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter err = %v", err)
	}
	close(provider.release)
	<-leaderDone
	if got := provider.callCount(); got != 1 {
		t.Fatalf("provider calls = %d", got)
	}
}

func TestRuntimeProviderFailureIsNotNoMatch(t *testing.T) {
	provider := &fakeProvider{err: newOpError(ErrUpstreamUnavailable, errors.New("private upstream"))}
	runtime, err := Open(context.Background(), RuntimeConfig{Root: filepath.Join(t.TempDir(), "metadata"), Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	result, err := runtime.Lookup(context.Background(), LookupInput{Title: "Sonic", System: "megadrive"})
	if opCode(err) != ErrUpstreamUnavailable || result.Outcome == OutcomeNoMatch {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
