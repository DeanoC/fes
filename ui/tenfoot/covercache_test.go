package tenfoot

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPageHandlesSkipsMissingAndDuplicates(t *testing.T) {
	t.Parallel()
	a := strings.Repeat("aa", 32)
	b := strings.Repeat("bb", 32)
	games := []Game{
		{ID: "1", Cover: a},
		{ID: "2"},
		{ID: "3", Cover: "nope"},
		{ID: "4", Cover: a},
		{ID: "5", Cover: b},
	}
	got := PageHandles(games, 0, len(games))
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("handles = %#v", got)
	}
	if got := PageHandles(games, 1, 3); len(got) != 0 {
		t.Fatalf("expected no valid handles, got %#v", got)
	}
}

func TestCoverCacheFetchesDecodesAndEvicts(t *testing.T) {
	t.Parallel()
	keep := strings.Repeat("ab", 32)
	drop := strings.Repeat("cd", 32)
	bad := strings.Repeat("ef", 32)
	var gets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		switch r.URL.Path {
		case "/api/v1/presentation/artwork/" + keep:
			writeSolidPNG(t, w, color.RGBA{R: 10, G: 200, B: 30, A: 255})
		case "/api/v1/presentation/artwork/" + drop:
			writeSolidPNG(t, w, color.RGBA{R: 200, G: 10, B: 30, A: 255})
		case "/api/v1/presentation/artwork/" + bad:
			_, _ = w.Write([]byte("not-an-image"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	cache := NewCoverCache()
	ctx := context.Background()
	cache.Keep([]string{keep, drop, bad})
	cache.Request(ctx, client, []string{keep, drop, bad})
	waitCover(t, cache, keep)
	waitCover(t, cache, drop)
	waitGeneration(t, cache, func(c *CoverCache) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, failed := c.failed[bad]
		return failed && c.images[bad] == nil
	})
	img := cache.Image(keep)
	if img == nil {
		t.Fatal("keep cover missing")
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r>>8 > 40 || g>>8 < 150 || b>>8 > 50 || a>>8 != 255 {
		t.Fatalf("decoded pixel = %d %d %d %d", r, g, b, a)
	}
	before := gets.Load()
	cache.Request(ctx, client, []string{keep, bad})
	time.Sleep(20 * time.Millisecond)
	if gets.Load() != before {
		t.Fatalf("retried ready/failed handles, gets=%d", gets.Load())
	}
	cache.Keep([]string{keep})
	if cache.Image(drop) != nil {
		t.Fatal("evicted cover still cached")
	}
	if cache.Image(keep) == nil {
		t.Fatal("visible cover evicted")
	}
}

func TestCoverCacheStatusReportsReadyLoadingFailed(t *testing.T) {
	t.Parallel()
	ready := strings.Repeat("aa", 32)
	bad := strings.Repeat("bb", 32)
	block := strings.Repeat("cc", 32)
	hold := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/presentation/artwork/" + ready:
			writeSolidPNG(t, w, color.RGBA{R: 10, G: 200, B: 30, A: 255})
		case "/api/v1/presentation/artwork/" + bad:
			_, _ = w.Write([]byte("not-an-image"))
		case "/api/v1/presentation/artwork/" + block:
			<-hold
			writeSolidPNG(t, w, color.RGBA{R: 1, G: 2, B: 3, A: 255})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		close(hold)
		server.Close()
	})
	client := NewClient(server.URL, server.Client())
	cache := NewCoverCache()
	if cache.Status("") != CoverAbsent || cache.Status("nope") != CoverAbsent {
		t.Fatal("empty/invalid handle should be absent")
	}
	cache.Keep([]string{ready, bad, block})
	cache.Request(context.Background(), client, []string{block})
	waitGeneration(t, cache, func(c *CoverCache) bool {
		return c.Status(block) == CoverLoading
	})
	if cache.Image(block) != nil {
		t.Fatal("loading cover appeared early")
	}
	cache.Request(context.Background(), client, []string{ready, bad})
	waitCover(t, cache, ready)
	waitGeneration(t, cache, func(c *CoverCache) bool {
		return c.Status(bad) == CoverFailed
	})
	if cache.Status(ready) != CoverReady {
		t.Fatalf("ready status %d", cache.Status(ready))
	}
	if cache.Status(bad) != CoverFailed {
		t.Fatalf("failed status %d", cache.Status(bad))
	}
	if (*CoverCache)(nil).Status(ready) != CoverAbsent {
		t.Fatal("nil cache should be absent")
	}
}

func TestCoverCacheRequestDoesNotBlock(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("11", 32)
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		writeSolidPNG(t, w, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	}))
	t.Cleanup(func() {
		close(block)
		server.Close()
	})
	client := NewClient(server.URL, server.Client())
	cache := NewCoverCache()
	cache.Keep([]string{handle})
	done := make(chan struct{})
	go func() {
		cache.Request(context.Background(), client, []string{handle})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Request blocked on artwork GET")
	}
	if cache.Image(handle) != nil {
		t.Fatal("cover appeared before GET finished")
	}
}

func TestCoverCacheLoadsFromStoreWithoutHTTP(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	store := &memArtworkStore{blobs: map[string][]byte{handle: solidPNG(t, color.RGBA{R: 10, G: 200, B: 30, A: 255})}}
	cache := NewCoverCache()
	cache.SetStore(store)
	cache.Keep([]string{handle})
	cache.Request(context.Background(), nil, []string{handle})
	waitCover(t, cache, handle)
	img := cache.Image(handle)
	if img == nil {
		t.Fatal("disk cover missing")
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r>>8 > 40 || g>>8 < 150 || b>>8 > 50 || a>>8 != 255 {
		t.Fatalf("decoded pixel = %d %d %d %d", r, g, b, a)
	}
	if store.loads.Load() != 1 {
		t.Fatalf("loads = %d", store.loads.Load())
	}
	if store.saves.Load() != 0 {
		t.Fatalf("disk hit rewrote cover saves=%d", store.saves.Load())
	}
}

func TestCoverCachePersistsFetchedArtwork(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("cd", 32)
	var gets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		writeSolidPNG(t, w, color.RGBA{R: 200, G: 10, B: 30, A: 255})
	}))
	t.Cleanup(server.Close)
	store := &memArtworkStore{blobs: map[string][]byte{}}
	cache := NewCoverCache()
	cache.SetStore(store)
	cache.Keep([]string{handle})
	cache.Request(context.Background(), NewClient(server.URL, server.Client()), []string{handle})
	waitCover(t, cache, handle)
	if gets.Load() != 1 {
		t.Fatalf("gets = %d", gets.Load())
	}
	if store.saves.Load() != 1 {
		t.Fatalf("saves = %d", store.saves.Load())
	}
	if _, ok := store.LoadArtwork(handle); !ok {
		t.Fatal("fetched cover was not stored")
	}
	cache.Keep([]string{})
	if cache.Image(handle) != nil {
		t.Fatal("RAM cover should evict")
	}
	cache.Keep([]string{handle})
	cache.Request(context.Background(), NewClient(server.URL, server.Client()), []string{handle})
	waitCover(t, cache, handle)
	if gets.Load() != 1 {
		t.Fatalf("disk hit used HTTP gets=%d", gets.Load())
	}
}

func TestCoverCacheClearFailedRetries(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ef", 32)
	var gets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := gets.Add(1)
		if n == 1 {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		writeSolidPNG(t, w, color.RGBA{R: 10, G: 20, B: 200, A: 255})
	}))
	t.Cleanup(server.Close)
	cache := NewCoverCache()
	cache.Keep([]string{handle})
	cache.Request(context.Background(), NewClient(server.URL, server.Client()), []string{handle})
	waitGeneration(t, cache, func(c *CoverCache) bool { return c.Status(handle) == CoverFailed })
	cache.Request(context.Background(), NewClient(server.URL, server.Client()), []string{handle})
	time.Sleep(20 * time.Millisecond)
	if gets.Load() != 1 {
		t.Fatalf("retried failed handle gets=%d", gets.Load())
	}
	cache.ClearFailed()
	cache.Request(context.Background(), NewClient(server.URL, server.Client()), []string{handle})
	waitCover(t, cache, handle)
	if gets.Load() != 2 {
		t.Fatalf("clear failed gets=%d", gets.Load())
	}
}

func TestStillCacheDecodesToStillStage(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	src := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			src.Set(x, y, color.RGBA{R: 200, G: 20, B: 20, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	pngBytes := buf.Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	covers := NewCoverCache()
	stills := NewStillCache()
	covers.Keep([]string{handle})
	stills.Keep([]string{handle})
	covers.Request(context.Background(), client, []string{handle})
	stills.Request(context.Background(), client, []string{handle})
	waitCover(t, covers, handle)
	waitCover(t, stills, handle)
	cover := covers.Image(handle)
	still := stills.Image(handle)
	if cover == nil || still == nil {
		t.Fatal("missing decode")
	}
	if cover.Bounds().Dx() > coverMaxW || cover.Bounds().Dy() > coverMaxH {
		t.Fatalf("cover stayed large %s", cover.Bounds())
	}
	if still.Bounds().Dx() != 400 || still.Bounds().Dy() != 400 {
		t.Fatalf("still scaled %s want 400x400", still.Bounds())
	}
}

type memArtworkStore struct {
	mu    sync.Mutex
	blobs map[string][]byte
	loads atomic.Int64
	saves atomic.Int64
}

func (s *memArtworkStore) LoadArtwork(handle string) ([]byte, bool) {
	s.loads.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.blobs[handle]
	if !ok {
		return nil, false
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, true
}

func (s *memArtworkStore) SaveArtwork(handle string, data []byte) error {
	s.saves.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blobs == nil {
		s.blobs = map[string][]byte{}
	}
	out := make([]byte, len(data))
	copy(out, data)
	s.blobs[handle] = out
	return nil
}

func writeSolidPNG(t *testing.T, w http.ResponseWriter, c color.RGBA) {
	t.Helper()
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(solidPNG(t, c))
}

func solidPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func waitCover(t *testing.T, cache *CoverCache, handle string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cache.Image(handle) != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("cover %s did not arrive", handle)
}

func waitGeneration(t *testing.T, cache *CoverCache, ok func(*CoverCache) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok(cache) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("cache did not reach expected state")
}
