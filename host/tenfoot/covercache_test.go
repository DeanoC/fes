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

func writeSolidPNG(t *testing.T, w http.ResponseWriter, c color.RGBA) {
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
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
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
