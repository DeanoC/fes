package tenfoot

import (
	"context"
	"image"
	"sync"
)

const maxCoverFetches = 3

// ArtworkFetcher loads raw cover bytes for a 64-hex handle.
type ArtworkFetcher interface {
	Artwork(ctx context.Context, handle string) ([]byte, string, error)
}

// CoverCache holds decoded catalog covers by artwork handle. Request never
// waits on the network; Keep drops handles that are no longer on the visible
// page or the following prefetch page.
type CoverCache struct {
	mu       sync.Mutex
	images   map[string]*image.RGBA
	failed   map[string]struct{}
	inflight map[string]struct{}
	wanted   map[string]struct{}
	gen      uint64
}

// NewCoverCache returns an empty handle cache.
func NewCoverCache() *CoverCache {
	return &CoverCache{
		images:   map[string]*image.RGBA{},
		failed:   map[string]struct{}{},
		inflight: map[string]struct{}{},
		wanted:   map[string]struct{}{},
	}
}

// Image returns a decoded cover, or nil when missing, in flight, or failed.
func (c *CoverCache) Image(handle string) *image.RGBA {
	if c == nil {
		return nil
	}
	handle = normalizeHandle(handle)
	if handle == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.images[handle]
}

// Generation changes when a fetch finishes so a renderer can repaint.
func (c *CoverCache) Generation() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// PageHandles returns unique catalog cover handles in games[start:end].
func PageHandles(games []Game, start, end int) []string {
	if start < 0 {
		start = 0
	}
	if end > len(games) {
		end = len(games)
	}
	if start >= end {
		return nil
	}
	out := make([]string, 0, end-start)
	seen := make(map[string]struct{}, end-start)
	for _, game := range games[start:end] {
		handle := CoverHandle(game, Presentation{})
		if handle == "" {
			continue
		}
		if _, ok := seen[handle]; ok {
			continue
		}
		seen[handle] = struct{}{}
		out = append(out, handle)
	}
	return out
}

// Request starts fetches for unknown handles and returns immediately.
func (c *CoverCache) Request(ctx context.Context, client ArtworkFetcher, handles []string) {
	if c == nil || client == nil {
		return
	}
	for _, handle := range handles {
		handle = normalizeHandle(handle)
		if handle == "" {
			continue
		}
		c.mu.Lock()
		_, have := c.images[handle]
		_, failed := c.failed[handle]
		_, busy := c.inflight[handle]
		slots := len(c.inflight)
		if have || failed || busy || slots >= maxCoverFetches {
			c.mu.Unlock()
			continue
		}
		c.inflight[handle] = struct{}{}
		c.mu.Unlock()
		go c.fetch(ctx, client, handle)
	}
}

// Keep retains only the supplied handles. In-flight fetches that finish for a
// dropped handle are discarded.
func (c *CoverCache) Keep(handles []string) {
	if c == nil {
		return
	}
	wanted := make(map[string]struct{}, len(handles))
	for _, handle := range handles {
		handle = normalizeHandle(handle)
		if handle == "" {
			continue
		}
		wanted[handle] = struct{}{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wanted = wanted
	for handle := range c.images {
		if _, ok := wanted[handle]; ok {
			continue
		}
		if _, busy := c.inflight[handle]; busy {
			continue
		}
		delete(c.images, handle)
	}
	for handle := range c.failed {
		if _, ok := wanted[handle]; ok {
			continue
		}
		if _, busy := c.inflight[handle]; busy {
			continue
		}
		delete(c.failed, handle)
	}
}

func (c *CoverCache) fetch(ctx context.Context, client ArtworkFetcher, handle string) {
	data, _, err := client.Artwork(ctx, handle)
	var img *image.RGBA
	if err == nil {
		img, err = DecodeCover(data)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, handle)
	c.gen++
	if _, ok := c.wanted[handle]; !ok {
		return
	}
	if err != nil || img == nil {
		c.failed[handle] = struct{}{}
		delete(c.images, handle)
		return
	}
	c.images[handle] = img
	delete(c.failed, handle)
}
