package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheKeyIsVersionedAndDetachedFromCatalogIdentity(t *testing.T) {
	left, err := BuildCacheKey(ProviderIGDB, "58", "sonic", "")
	if err != nil {
		t.Fatal(err)
	}
	right, err := BuildCacheKey(ProviderIGDB, "58", "sonic", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) || len(left) != 32 {
		t.Fatalf("keys are not deterministic SHA-256 values: %x %x", left, right)
	}
	for _, input := range [][4]string{{"igdb", "59", "sonic", ""}, {"igdb", "58", "sonic 2", ""}, {"igdb", "58", "sonic", "europe"}} {
		other, err := BuildCacheKey(ProviderName(input[0]), input[1], input[2], input[3])
		if err != nil {
			t.Fatal(err)
		}
		if string(other) == string(left) {
			t.Fatalf("key collision for %#v", input)
		}
	}
}

func TestCachePersistsPositiveAndNegativeTTLs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	now := time.Unix(1000, 0)
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "client-id", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := BuildCacheKey(ProviderIGDB, "58", "sonic", "")
	positive := Result{Outcome: OutcomeExact, Presentation: Presentation{Summary: "summary"}, Attribution: Attribution{Provider: ProviderIGDB, Label: "Data from IGDB.com"}}
	if err := cache.Put(key, positive, now.Add(30*24*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	got, ok, err := cache.Get(key)
	if err != nil || !ok || got.Presentation.Summary != "summary" {
		t.Fatalf("positive get = %#v %v %v", got, ok, err)
	}
	negativeKey, _ := BuildCacheKey(ProviderIGDB, "58", "missing", "")
	if err := cache.Put(negativeKey, Result{Outcome: OutcomeNoMatch}, now.Add(24*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24*time.Hour + time.Second)
	if _, ok, err := cache.Get(negativeKey); err != nil || ok {
		t.Fatalf("expired negative get = ok:%v err:%v", ok, err)
	}
	now = now.Add(30*24*time.Hour + time.Second)
	if _, ok, err := cache.Get(key); err != nil || ok {
		t.Fatalf("expired positive was served: ok:%v err:%v", ok, err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cache.sqlite3")); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, filepath.Join(root, "cache.sqlite3")); mode != 0o600 {
		t.Fatalf("cache mode = %o", mode)
	}
	cache, err = OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "client-id", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	if _, ok, err := cache.Get(key); err != nil || ok {
		t.Fatalf("expired positive survived re-open as served: ok:%v err:%v", ok, err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
