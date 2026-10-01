package fogcast

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestLibraryCacheReportsTargetInventoryWithoutLease(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	client := &fakeServiceClient{cacheIndex: func(context.Context) (protocol.CacheIndex, error) {
		return protocol.CacheIndex{
			UsedBytes: 8, MaxBytes: 64, FreeBytes: 56,
			Entries: []protocol.CacheIndexEntry{{System: protocol.SystemSNES, SHA256: digest, Size: 8, Extension: "sfc"}},
		}, nil
	}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	status, err := service.LibraryCache(context.Background())
	if err != nil || !status.ROM.Reachable || status.ROM.UsedBytes != 8 || status.ROM.MaxBytes != 64 || status.ROM.FreeBytes != 56 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	if client.cacheIndexCalls != 1 {
		t.Fatalf("calls=%d", client.cacheIndexCalls)
	}
	presence, known := service.ROMCachePresence(context.Background())
	if !known || !presence[string(protocol.SystemSNES)+"/"+digest] {
		t.Fatalf("presence=%v known=%v", presence, known)
	}
	if client.cacheIndexCalls != 1 {
		t.Fatalf("memo not used calls=%d", client.cacheIndexCalls)
	}
}

func TestPairedHealthAndLibraryCacheDoNotWaitForForegroundLaunchLock(t *testing.T) {
	clientB := &fakeServiceClient{
		healthResult: protocol.Health{Ready: true},
		cacheIndex: func(context.Context) (protocol.CacheIndex, error) {
			return protocol.CacheIndex{UsedBytes: 4, MaxBytes: 10, FreeBytes: 6}, nil
		},
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.targets = []TargetConfig{{Name: "kit-a", Enabled: true, TargetID: "a"}, {Name: "kit-b", Enabled: true, TargetID: "b"}}
	service.pairedTargetConfigs = append([]TargetConfig(nil), service.targets...)
	service.pairedTargetClients["kit-b"] = clientB

	// Launch holds targetMu for the full target operation. Model that critical
	// section while paired reads for another kit exercise the real Service.
	service.targetMu.RLock()
	defer service.targetMu.RUnlock()
	// A queued writer (for example a settings update) blocks new readers, so
	// paired reads must not touch targetMu at all.
	go func() { service.targetMu.Lock(); service.targetMu.Unlock() }()
	queueDeadline := time.Now().Add(time.Second)
	for service.targetMu.TryRLock() {
		service.targetMu.RUnlock()
		if time.Now().After(queueDeadline) {
			t.Fatal("writer did not queue")
		}
		runtime.Gosched()
	}
	ctx := WithPairedTarget(context.Background(), "b")
	results := make(chan error, 3)
	go func() {
		health, err := service.Health(ctx)
		if err == nil && !health.Ready {
			err = errors.New("kit B health was not ready")
		}
		results <- err
	}()
	go func() {
		cache, err := service.LibraryCache(ctx)
		if err == nil && (!cache.ROM.Reachable || cache.ROM.UsedBytes != 4) {
			err = errors.New("kit B cache response was incorrect")
		}
		results <- err
	}()
	go func() {
		_, known := service.ROMCachePresence(ctx)
		if !known {
			err := errors.New("kit B rom cache presence was unknown")
			results <- err
			return
		}
		results <- nil
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for range 3 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-deadline.C:
			t.Fatal("paired health or library cache waited behind the foreground target lock or a queued writer")
		}
	}
}

func TestLibraryCacheOmitsROMCachedWhenTargetUnreachable(t *testing.T) {
	client := &fakeServiceClient{}
	service := newTestService(&fakeServiceCatalog{games: []catalog.Game{serviceGame(catalog.Content{SHA256: strings.Repeat("cd", 32), Size: 3, Extension: "sfc"})}}, &fakeServicePreparer{}, client)
	status, err := service.LibraryCache(context.Background())
	if err != nil || status.ROM.Reachable {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	_, known := service.ROMCachePresence(context.Background())
	if known {
		t.Fatal("unreachable target invented rom_cached knowledge")
	}
}
