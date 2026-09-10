package fogcast

import (
	"context"
	"strings"
	"testing"

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
