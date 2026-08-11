package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCacheFailsClosedOnSymlinkResidue(t *testing.T) {
	root := t.TempDir()
	sentinel := filepath.Join(root, "sentinel")
	if err := os.WriteFile(sentinel, []byte("do not remove"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(root, "cache.sqlite3")); err != nil {
		t.Fatal(err)
	}
	configured := filepath.Join(root, "configured")
	if err := os.MkdirAll(configured, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(configured, "cache.sqlite3")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCache(context.Background(), CacheConfig{Root: configured, CredentialScope: "scope"}); err == nil {
		t.Fatal("expected symlink database to fail closed")
	}
	if content, err := os.ReadFile(sentinel); err != nil || string(content) != "do not remove" {
		t.Fatalf("sentinel changed: %q err=%v", content, err)
	}
	if info, err := os.Lstat(filepath.Join(configured, "cache.sqlite3")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink residue was removed: info=%v err=%v", info, err)
	}
}

func TestOpenCacheFailsClosedOnSymlinkSidecar(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "sidecar-sentinel")
	if err := os.WriteFile(sentinel, []byte("do not remove"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(root, "cache.sqlite3-wal")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"}); err == nil {
		t.Fatal("expected symlink sidecar to fail closed")
	}
	if content, err := os.ReadFile(sentinel); err != nil || string(content) != "do not remove" {
		t.Fatalf("sidecar sentinel changed: %q err=%v", content, err)
	}
}

func TestCacheArtworkExpiryIsClosedAtBoundary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	now := time.Unix(1000, 0)
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	key, _ := BuildCacheKey(ProviderIGDB, "58", "sonic", "")
	content := []byte("image")
	digestBytesValue := sha256.Sum256(content)
	digest := hex.EncodeToString(digestBytesValue[:])
	if err := cache.Put(key, Result{Outcome: OutcomeExact}, now.Add(time.Hour), []ArtworkCacheEntry{{Handle: digest, Role: ArtworkCover, ProviderImageID: "image", ContentDigest: digest, MIME: "image/png", ByteCount: int64(len(content)), Width: 1, Height: 1}}); err != nil {
		var operation *OpError
		if errors.As(err, &operation) {
			t.Fatalf("put: %v cause=%v", err, operation.cause)
		}
		t.Fatal(err)
	}
	if _, ok, err := cache.Artwork(digest); err != nil || !ok {
		t.Fatalf("artwork before expiry: ok=%v err=%v", ok, err)
	}
	now = now.Add(time.Hour)
	if _, ok, err := cache.Artwork(digest); err != nil || ok {
		t.Fatalf("artwork at expiry: ok=%v err=%v", ok, err)
	}
}

func TestCacheReplacementsGarbageCollectUnreferencedArtwork(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	key, _ := BuildCacheKey(ProviderIGDB, "58", "sonic", "")
	first := strings.Repeat("a", 64)
	second := strings.Repeat("b", 64)
	entry := func(handle string) ArtworkCacheEntry {
		return ArtworkCacheEntry{Handle: handle, Role: ArtworkCover, ProviderImageID: "image", ContentDigest: handle, MIME: "image/png", ByteCount: 1, Width: 1, Height: 1}
	}
	if err := cache.Put(key, Result{Outcome: OutcomeExact}, time.Now().Add(time.Hour), []ArtworkCacheEntry{entry(first)}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(key, Result{Outcome: OutcomeExact}, time.Now().Add(time.Hour), []ArtworkCacheEntry{entry(second)}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := cache.db.QueryRow(`SELECT COUNT(*) FROM artwork_objects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("artwork object count = %d, want one live object", count)
	}
}

func TestArtworkRootRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := NewArtworkFetcher(ArtworkFetcherConfig{Root: filepath.Join(alias, "metadata")}); err == nil {
		t.Fatal("expected symlinked parent to be rejected")
	}
}

func TestCacheMetadataRowsRetainProviderProvenance(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	key, _ := BuildCacheKey(ProviderIGDB, "58", "sonic", "europe")
	if err := cache.PutWithMetadata(key, CacheRecordMetadata{Provider: ProviderIGDB, PlatformID: "58", NormalizedTitle: "sonic", Region: "europe"}, Result{Outcome: OutcomeExact}, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	var provider, platformID, title, region string
	if err := cache.db.QueryRow(`SELECT provider, platform_id, normalized_title, region FROM metadata_records WHERE cache_key=?`, key).Scan(&provider, &platformID, &title, &region); err != nil {
		t.Fatal(err)
	}
	if provider != string(ProviderIGDB) || platformID != "58" || title != "sonic" || region != "europe" {
		t.Fatalf("provenance = provider=%q platform=%q title=%q region=%q", provider, platformID, title, region)
	}
}

func TestCacheBytePressureEvictsDeterministicLRUAndPhysicalFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	now := time.Unix(1000, 0)
	cache, err := OpenCache(context.Background(), CacheConfig{
		Root:            root,
		CredentialScope: "scope",
		Now:             func() time.Time { return now },
		maxArtworkBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	firstKey, _ := BuildCacheKey(ProviderIGDB, "58", "first", "")
	secondKey, _ := BuildCacheKey(ProviderIGDB, "58", "second", "")
	firstDigest := strings.Repeat("a", 64)
	secondDigest := strings.Repeat("b", 64)
	writeCachedArtwork(t, root, firstDigest, 4)
	if err := cache.Put(firstKey, Result{Outcome: OutcomeExact}, now.Add(time.Hour), []ArtworkCacheEntry{cachedArtwork(firstDigest, 4)}); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Second)
	writeCachedArtwork(t, root, secondDigest, 4)
	if err := cache.Put(secondKey, Result{Outcome: OutcomeExact}, now.Add(time.Hour), []ArtworkCacheEntry{cachedArtwork(secondDigest, 4)}); err != nil {
		t.Fatal(err)
	}

	var databaseBytes int64
	if err := cache.db.QueryRow(`SELECT COALESCE(SUM(byte_count), 0) FROM artwork_objects`).Scan(&databaseBytes); err != nil {
		t.Fatal(err)
	}
	if databaseBytes > 4 || artworkBytesOnDisk(t, root) > 4 {
		t.Fatalf("artwork exceeded injected cap: database=%d filesystem=%d", databaseBytes, artworkBytesOnDisk(t, root))
	}
	if _, ok, err := cache.Get(firstKey); err != nil || ok {
		t.Fatalf("least-recently-used record survived: ok=%v err=%v", ok, err)
	}
	if _, ok, err := cache.Get(secondKey); err != nil || !ok {
		t.Fatalf("most-recently-used record missing: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, "artwork", firstDigest)); !os.IsNotExist(err) {
		t.Fatalf("evicted artwork file still exists: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "artwork", secondDigest)); err != nil {
		t.Fatalf("retained artwork file missing: %v", err)
	}
}

func TestCacheBytePressureBreaksEqualLRUByCacheKey(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	now := time.Unix(1000, 0)
	cache, err := OpenCache(context.Background(), CacheConfig{
		Root:            root,
		CredentialScope: "scope",
		Now:             func() time.Time { return now },
		maxArtworkBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	firstKey, _ := BuildCacheKey(ProviderIGDB, "58", "first", "")
	secondKey, _ := BuildCacheKey(ProviderIGDB, "58", "second", "")
	firstDigest := strings.Repeat("c", 64)
	secondDigest := strings.Repeat("d", 64)
	writeCachedArtwork(t, root, firstDigest, 1)
	writeCachedArtwork(t, root, secondDigest, 1)
	if err := cache.Put(firstKey, Result{Outcome: OutcomeExact}, now.Add(time.Hour), []ArtworkCacheEntry{cachedArtwork(firstDigest, 1)}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(secondKey, Result{Outcome: OutcomeExact}, now.Add(time.Hour), []ArtworkCacheEntry{cachedArtwork(secondDigest, 1)}); err != nil {
		t.Fatal(err)
	}

	firstName := hex.EncodeToString(firstKey)
	secondName := hex.EncodeToString(secondKey)
	evictedKey, retainedKey := firstKey, secondKey
	if secondName < firstName {
		evictedKey, retainedKey = secondKey, firstKey
	}
	if _, ok, err := cache.Get(evictedKey); err != nil || ok {
		t.Fatalf("tie-broken record survived: key=%x ok=%v err=%v", evictedKey, ok, err)
	}
	if _, ok, err := cache.Get(retainedKey); err != nil || !ok {
		t.Fatalf("tie-broken record was not retained: key=%x ok=%v err=%v", retainedKey, ok, err)
	}
}

func TestCacheReplacementExpiryAndPurgeRemovePhysicalArtwork(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	now := time.Unix(1000, 0)
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	key, _ := BuildCacheKey(ProviderIGDB, "58", "replace", "")
	firstDigest := strings.Repeat("e", 64)
	secondDigest := strings.Repeat("f", 64)
	writeCachedArtwork(t, root, firstDigest, 2)
	if err := cache.Put(key, Result{Outcome: OutcomeExact}, now.Add(time.Hour), []ArtworkCacheEntry{cachedArtwork(firstDigest, 2)}); err != nil {
		t.Fatal(err)
	}
	writeCachedArtwork(t, root, secondDigest, 3)
	if err := cache.Put(key, Result{Outcome: OutcomeExact}, now.Add(2*time.Hour), []ArtworkCacheEntry{cachedArtwork(secondDigest, 3)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "artwork", firstDigest)); !os.IsNotExist(err) {
		t.Fatalf("replaced artwork file still exists: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "artwork", secondDigest)); err != nil {
		t.Fatalf("replacement artwork file missing: %v", err)
	}

	now = now.Add(2 * time.Hour)
	if _, ok, err := cache.Get(key); err != nil || ok {
		t.Fatalf("expired metadata was served: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, "artwork", secondDigest)); !os.IsNotExist(err) {
		t.Fatalf("expired artwork file still exists: err=%v", err)
	}

	purgeKey, _ := BuildCacheKey(ProviderIGDB, "58", "purge", "")
	purgeDigest := strings.Repeat("1", 64)
	orphanDigest := strings.Repeat("2", 64)
	writeCachedArtwork(t, root, purgeDigest, 2)
	writeCachedArtwork(t, root, orphanDigest, 3)
	if err := cache.Put(purgeKey, Result{Outcome: OutcomeExact}, now.Add(time.Hour), []ArtworkCacheEntry{cachedArtwork(purgeDigest, 2)}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Purge(); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{purgeDigest, orphanDigest} {
		if _, err := os.Stat(filepath.Join(root, "artwork", digest)); !os.IsNotExist(err) {
			t.Fatalf("purged artwork file %s still exists: err=%v", digest, err)
		}
	}
	var objects, records int
	if err := cache.db.QueryRow(`SELECT COUNT(*) FROM artwork_objects`).Scan(&objects); err != nil {
		t.Fatal(err)
	}
	if err := cache.db.QueryRow(`SELECT COUNT(*) FROM metadata_records`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if objects != 0 || records != 0 {
		t.Fatalf("purge left database rows: objects=%d records=%d", objects, records)
	}
}

func TestCacheArtworkDeletionFailuresFailClosedAndRecover(t *testing.T) {
	for _, kind := range []string{"symlink", "non-regular"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "metadata")
			cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
			if err != nil {
				t.Fatal(err)
			}
			defer cache.Close()

			key, _ := BuildCacheKey(ProviderIGDB, "58", kind, "")
			badDigest := strings.Repeat("3", 64)
			badPath := filepath.Join(root, "artwork", badDigest)
			var sentinel string
			switch kind {
			case "symlink":
				sentinel = filepath.Join(root, "sentinel")
				if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(sentinel, badPath); err != nil {
					t.Fatal(err)
				}
			case "non-regular":
				if err := os.Mkdir(badPath, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(badPath, "keep"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			goodDigest := strings.Repeat("4", 64)
			writeCachedArtwork(t, root, goodDigest, 1)
			if err := cache.Put(key, Result{Outcome: OutcomeExact}, time.Now().Add(time.Hour), []ArtworkCacheEntry{cachedArtwork(badDigest, 1)}); err != nil {
				t.Fatal(err)
			}
			if err := cache.Put(key, Result{Outcome: OutcomeExact}, time.Now().Add(time.Hour), []ArtworkCacheEntry{cachedArtwork(goodDigest, 1)}); opCode(err) != ErrStorage {
				t.Fatalf("deletion failure = %v code=%s", err, opCode(err))
			}
			var pending int
			if err := cache.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key='pending_record'`).Scan(&pending); err != nil {
				t.Fatal(err)
			}
			if pending != 0 {
				t.Fatalf("compensated publication left pending marker: %d", pending)
			}
			if kind == "symlink" {
				content, readErr := os.ReadFile(sentinel)
				if readErr != nil || string(content) != "keep" {
					t.Fatalf("symlink target changed: %q err=%v", content, readErr)
				}
			}

			if kind == "symlink" {
				if err := os.Remove(badPath); err != nil {
					t.Fatal(err)
				}
			} else if err := os.RemoveAll(badPath); err != nil {
				t.Fatal(err)
			}
			if err := cache.Purge(); err != nil {
				t.Fatalf("recovery purge failed: %v", err)
			}
			var objects int
			if err := cache.db.QueryRow(`SELECT COUNT(*) FROM artwork_objects`).Scan(&objects); err != nil {
				t.Fatal(err)
			}
			if objects != 0 {
				t.Fatalf("recovery left artwork rows: %d", objects)
			}
		})
	}
}

func cachedArtwork(digest string, size int) ArtworkCacheEntry {
	return ArtworkCacheEntry{Handle: digest, Role: ArtworkCover, ProviderImageID: "image", ContentDigest: digest, MIME: "image/png", ByteCount: int64(size), Width: 1, Height: 1}
}

func writeCachedArtwork(t *testing.T, root, digest string, size int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "artwork", digest), []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func artworkBytesOnDisk(t *testing.T, root string) int64 {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "artwork"))
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(root, "artwork", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("unexpected artwork residue %s: %s", entry.Name(), info.Mode())
		}
		total += info.Size()
	}
	return total
}

func TestContractLRUThrottleSurvivesReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	now := time.Unix(1000, 0)
	open := func() *Cache {
		cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope", Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		return cache
	}
	cache := open()
	key, _ := BuildCacheKey(ProviderIGDB, "58", "sonic", "")
	if err := cache.Put(key, Result{Outcome: OutcomeExact}, now.Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := cache.db.QueryRow(`SELECT last_accessed_at FROM metadata_records WHERE cache_key=?`, key).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	cache = open()
	defer cache.Close()
	if _, ok, err := cache.Get(key); err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	var after int64
	if err := cache.db.QueryRow(`SELECT last_accessed_at FROM metadata_records WHERE cache_key=?`, key).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("LRU write was repeated within one hour across reopen: before=%d after=%d", before, after)
	}
}
