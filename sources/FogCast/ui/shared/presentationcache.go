package shared

import (
	"context"
	"strings"
	"sync"
)

const maxPresentationFetches = 3

// PresentationFetcher loads GET /api/v1/presentation/games/{id}.
type PresentationFetcher interface {
	GamePresentation(ctx context.Context, gameID string) (Presentation, error)
}

// PresentationCache holds presentation payloads by game ID. Request never
// waits on the network; Keep drops IDs that are no longer on the visible
// page or the following prefetch page.
type PresentationCache struct {
	mu       sync.Mutex
	byID     map[string]Presentation
	failed   map[string]struct{}
	inflight map[string]struct{}
	wanted   map[string]struct{}
	gen      uint64
}

// NewPresentationCache returns an empty game-ID presentation cache.
func NewPresentationCache() *PresentationCache {
	return &PresentationCache{
		byID:     map[string]Presentation{},
		failed:   map[string]struct{}{},
		inflight: map[string]struct{}{},
		wanted:   map[string]struct{}{},
	}
}

// Get returns a cached presentation, or a zero value when missing or failed.
func (c *PresentationCache) Get(id string) Presentation {
	if c == nil {
		return Presentation{}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Presentation{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byID[id]
}

// Generation changes when a fetch finishes so a renderer can repaint.
func (c *PresentationCache) Generation() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// ClearFailed drops fetch failures so the next Request can retry after the
// host becomes reachable again.
func (c *PresentationCache) ClearFailed() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.failed) == 0 {
		return
	}
	c.failed = map[string]struct{}{}
	c.gen++
}

// Request starts fetches for unknown IDs and returns immediately.
func (c *PresentationCache) Request(ctx context.Context, client PresentationFetcher, ids []string) {
	if c == nil || client == nil {
		return
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		c.mu.Lock()
		_, have := c.byID[id]
		_, failed := c.failed[id]
		_, busy := c.inflight[id]
		slots := len(c.inflight)
		if have || failed || busy || slots >= maxPresentationFetches {
			c.mu.Unlock()
			continue
		}
		c.inflight[id] = struct{}{}
		c.mu.Unlock()
		go c.fetch(ctx, client, id)
	}
}

// Keep retains only the supplied game IDs. In-flight fetches that finish for a
// dropped ID are discarded.
func (c *PresentationCache) Keep(ids []string) {
	if c == nil {
		return
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		wanted[id] = struct{}{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wanted = wanted
	for id := range c.byID {
		if _, ok := wanted[id]; ok {
			continue
		}
		if _, busy := c.inflight[id]; busy {
			continue
		}
		delete(c.byID, id)
	}
	for id := range c.failed {
		if _, ok := wanted[id]; ok {
			continue
		}
		if _, busy := c.inflight[id]; busy {
			continue
		}
		delete(c.failed, id)
	}
}

func (c *PresentationCache) fetch(ctx context.Context, client PresentationFetcher, id string) {
	pres, err := client.GamePresentation(ctx, id)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, id)
	c.gen++
	if _, ok := c.wanted[id]; !ok {
		return
	}
	if err != nil {
		c.failed[id] = struct{}{}
		delete(c.byID, id)
		return
	}
	c.byID[id] = pres
	delete(c.failed, id)
}
