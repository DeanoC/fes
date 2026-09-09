package tenfoot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectCoverHandlesUsesPresentationWhenCatalogCoverMissing(t *testing.T) {
	t.Parallel()
	catalog := strings.Repeat("aa", 32)
	meta := strings.Repeat("bb", 32)
	games := []Game{
		{ID: "pong", Cover: catalog},
		{ID: "sonic"},
		{ID: "missing"},
		{ID: "sonic-dup"},
	}
	pres := map[string]Presentation{
		"sonic":     {Presentation: &PresentationInfo{CoverArtworkID: meta}},
		"sonic-dup": {Presentation: &PresentationInfo{CoverArtworkID: meta}},
		"missing":   {Presentation: &PresentationInfo{CoverArtworkID: "nope"}},
	}
	got := CollectCoverHandles(games, 0, len(games), func(id string) Presentation { return pres[id] })
	if len(got) != 2 || got[0] != catalog || got[1] != meta {
		t.Fatalf("handles = %#v", got)
	}
	if got := CollectCoverHandles(games, 0, len(games), nil); len(got) != 1 || got[0] != catalog {
		t.Fatalf("catalog-only = %#v", got)
	}
}

func TestPageIDsSkipsEmpty(t *testing.T) {
	t.Parallel()
	games := []Game{{ID: "sonic"}, {ID: " "}, {ID: "mario"}}
	got := PageIDs(games, 0, len(games))
	if len(got) != 2 || got[0] != "sonic" || got[1] != "mario" {
		t.Fatalf("ids = %#v", got)
	}
}

func TestCoverHandlePrefersCatalogThenPresentation(t *testing.T) {
	t.Parallel()
	catalog := strings.Repeat("11", 32)
	meta := strings.Repeat("22", 32)
	pres := Presentation{Presentation: &PresentationInfo{CoverArtworkID: meta}}
	if got := CoverHandle(Game{Cover: catalog}, pres); got != catalog {
		t.Fatalf("catalog preferred = %q", got)
	}
	if got := CoverHandle(Game{}, pres); got != meta {
		t.Fatalf("presentation = %q", got)
	}
	if got := CoverHandle(Game{}, Presentation{}); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestLogoHandleAndCollectLogoHandles(t *testing.T) {
	t.Parallel()
	logo := strings.Repeat("ab", 32)
	dup := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	games := []Game{{ID: "sonic"}, {ID: "pong"}, {ID: "mario"}, {ID: "sonic-dup"}}
	pres := map[string]Presentation{
		"sonic":     {Presentation: &PresentationInfo{LogoID: logo, CoverArtworkID: strings.Repeat("11", 32)}},
		"pong":      {Presentation: &PresentationInfo{CoverArtworkID: strings.Repeat("22", 32)}},
		"mario":     {Presentation: &PresentationInfo{LogoID: other}},
		"sonic-dup": {Presentation: &PresentationInfo{LogoID: dup}},
	}
	if got := LogoHandle(pres["sonic"]); got != logo {
		t.Fatalf("logo = %q", got)
	}
	if got := LogoHandle(pres["pong"]); got != "" {
		t.Fatalf("cover-only logo = %q", got)
	}
	if got := LogoHandle(Presentation{}); got != "" {
		t.Fatalf("empty = %q", got)
	}
	got := CollectLogoHandles(games, 0, len(games), func(id string) Presentation { return pres[id] })
	if len(got) != 2 || got[0] != logo || got[1] != other {
		t.Fatalf("handles = %#v", got)
	}
	if got := CollectLogoHandles(games, 0, len(games), nil); got != nil {
		t.Fatalf("nil presentation = %#v", got)
	}
	backdrop := strings.Repeat("ef", 32)
	if got := BackdropHandle(Presentation{Presentation: &PresentationInfo{BackdropArtworkID: backdrop}}); got != backdrop {
		t.Fatalf("backdrop = %q", got)
	}
	if got := BackdropHandle(Presentation{Presentation: &PresentationInfo{CoverArtworkID: strings.Repeat("11", 32)}}); got != "" {
		t.Fatalf("cover-only backdrop = %q", got)
	}
	got = CollectBackdropHandles(games, 0, len(games), func(id string) Presentation {
		if id == "sonic" {
			return Presentation{Presentation: &PresentationInfo{BackdropArtworkID: backdrop}}
		}
		if id == "mario" {
			return Presentation{Presentation: &PresentationInfo{BackdropArtworkID: other}}
		}
		return pres[id]
	})
	if len(got) != 2 || got[0] != backdrop || got[1] != other {
		t.Fatalf("backdrop handles = %#v", got)
	}
	if got := CollectBackdropHandles(games, 0, len(games), nil); got != nil {
		t.Fatalf("nil presentation backdrop = %#v", got)
	}
	marquee := strings.Repeat("99", 32)
	if got := MarqueeHandle(Presentation{Presentation: &PresentationInfo{MarqueeID: marquee}}); got != marquee {
		t.Fatalf("marquee = %q", got)
	}
	if got := MarqueeHandle(Presentation{Presentation: &PresentationInfo{LogoID: logo}}); got != "" {
		t.Fatalf("logo-only marquee = %q", got)
	}
}

func TestPresentationCacheFetchesEvictsAndSkipsRetry(t *testing.T) {
	t.Parallel()
	keep := strings.Repeat("ab", 32)
	drop := strings.Repeat("cd", 32)
	var gets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/presentation/games/")
		handle := keep
		if id == "drop" {
			handle = drop
		}
		if id == "bad" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Presentation{
			GameID:       id,
			State:        "ready",
			Presentation: &PresentationInfo{CoverArtworkID: handle},
		})
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	cache := NewPresentationCache()
	ctx := context.Background()
	cache.Keep([]string{"keep", "drop", "bad"})
	cache.Request(ctx, client, []string{"keep", "drop", "bad"})
	waitPresentation(t, cache, "keep")
	waitPresentation(t, cache, "drop")
	waitPresentationGeneration(t, cache, func(c *PresentationCache) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, failed := c.failed["bad"]
		_, have := c.byID["bad"]
		return failed && !have
	})
	if CoverHandle(Game{ID: "keep"}, cache.Get("keep")) != keep {
		t.Fatalf("keep handle = %q", CoverHandle(Game{ID: "keep"}, cache.Get("keep")))
	}
	before := gets.Load()
	cache.Request(ctx, client, []string{"keep", "bad"})
	time.Sleep(20 * time.Millisecond)
	if gets.Load() != before {
		t.Fatalf("retried ready/failed presentations, gets=%d", gets.Load())
	}
	cache.Keep([]string{"keep"})
	if CoverHandle(Game{ID: "drop"}, cache.Get("drop")) != "" {
		t.Fatal("evicted presentation still cached")
	}
	if CoverHandle(Game{ID: "keep"}, cache.Get("keep")) != keep {
		t.Fatal("visible presentation evicted")
	}
}

func TestPresentationCacheRequestDoesNotBlock(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		_ = json.NewEncoder(w).Encode(Presentation{GameID: "sonic", State: "ready"})
	}))
	t.Cleanup(func() {
		close(block)
		server.Close()
	})
	client := NewClient(server.URL, server.Client())
	cache := NewPresentationCache()
	cache.Keep([]string{"sonic"})
	done := make(chan struct{})
	go func() {
		cache.Request(context.Background(), client, []string{"sonic"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Request blocked on presentation GET")
	}
	if cache.Get("sonic").GameID != "" {
		t.Fatal("presentation appeared before GET finished")
	}
}

func waitPresentation(t *testing.T, cache *PresentationCache, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cache.Get(id).GameID == id || (cache.Get(id).Presentation != nil) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("presentation %s did not arrive", id)
}

func waitPresentationGeneration(t *testing.T, cache *PresentationCache, ok func(*PresentationCache) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok(cache) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("presentation cache did not reach expected state")
}
