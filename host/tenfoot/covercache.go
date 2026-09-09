package tenfoot

import (
	"context"
	"errors"
	"image"
	"strings"
	"sync"
)

var errNoArtwork = errors.New("artwork is unavailable")

const maxCoverFetches = 3

// ArtworkFetcher loads raw cover bytes for a 64-hex handle.
type ArtworkFetcher interface {
	Artwork(ctx context.Context, handle string) ([]byte, string, error)
}

// ArtworkStore is an optional durable cache of raw artwork bytes keyed by
// 64-hex handle. Request reads it before HTTP and writes successful fetches.
type ArtworkStore interface {
	LoadArtwork(handle string) ([]byte, bool)
	SaveArtwork(handle string, data []byte) error
}

// CoverCache holds decoded catalog covers by artwork handle. Request never
// waits on the network; Keep drops handles that are no longer on the visible
// page or the following prefetch page. Disk hits stay in RAM until Keep drops
// them the same way network hits do.
type CoverCache struct {
	mu       sync.Mutex
	images   map[string]*image.RGBA
	failed   map[string]struct{}
	inflight map[string]struct{}
	wanted   map[string]struct{}
	gen      uint64
	decode   func([]byte) (*image.RGBA, error)
	store    ArtworkStore
}

// NewCoverCache returns an empty handle cache that decodes catalog covers.
func NewCoverCache() *CoverCache {
	return newArtworkCache(DecodeCover)
}

// NewStillCache returns an empty handle cache that decodes attract stills.
func NewStillCache() *CoverCache {
	return newArtworkCache(DecodeStill)
}

func newArtworkCache(decode func([]byte) (*image.RGBA, error)) *CoverCache {
	if decode == nil {
		decode = DecodeCover
	}
	return &CoverCache{
		images:   map[string]*image.RGBA{},
		failed:   map[string]struct{}{},
		inflight: map[string]struct{}{},
		wanted:   map[string]struct{}{},
		decode:   decode,
	}
}

// CoverStatus is the cheap fetch state for one artwork handle.
type CoverStatus uint8

const (
	// CoverAbsent is an empty handle, or a handle not yet requested.
	CoverAbsent CoverStatus = iota
	// CoverReady is a decoded image in the cache.
	CoverReady
	// CoverLoading is an in-flight GET/decode.
	CoverLoading
	// CoverFailed is a fetch or decode that will not be retried until Keep drops it.
	CoverFailed
)

// Status reports whether handle is ready, in flight, failed, or absent.
// It does not wait on the network.
func (c *CoverCache) Status(handle string) CoverStatus {
	if c == nil {
		return CoverAbsent
	}
	handle = normalizeHandle(handle)
	if handle == "" {
		return CoverAbsent
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.images[handle]; ok {
		return CoverReady
	}
	if _, ok := c.failed[handle]; ok {
		return CoverFailed
	}
	if _, ok := c.inflight[handle]; ok {
		return CoverLoading
	}
	return CoverAbsent
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

// SetStore attaches a durable artwork blob store. Request reads it before
// HTTP. A nil store returns CoverCache to RAM-only fetches.
func (c *CoverCache) SetStore(store ArtworkStore) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.store = store
	c.mu.Unlock()
}

// ClearFailed drops fetch failures so the next Request can retry, including
// after the host becomes reachable again.
func (c *CoverCache) ClearFailed() {
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

// PageHandles returns unique catalog cover handles in games[start:end].
func PageHandles(games []Game, start, end int) []string {
	return CollectCoverHandles(games, start, end, nil)
}

// PageIDs returns game IDs in games[start:end] for presentation prefetch.
func PageIDs(games []Game, start, end int) []string {
	start, end = clampPage(games, start, end)
	if start >= end {
		return nil
	}
	out := make([]string, 0, end-start)
	for _, game := range games[start:end] {
		id := strings.TrimSpace(game.ID)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out
}

// CollectCoverHandles returns unique 64-hex cover handles from catalog rows
// and optional presentation lookups. presentation may be nil.
func CollectCoverHandles(games []Game, start, end int, presentation func(string) Presentation) []string {
	start, end = clampPage(games, start, end)
	if start >= end {
		return nil
	}
	out := make([]string, 0, end-start)
	seen := make(map[string]struct{}, end-start)
	for _, game := range games[start:end] {
		var pres Presentation
		if presentation != nil {
			pres = presentation(game.ID)
		}
		handle := CoverHandle(game, pres)
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

// CollectLogoHandles returns unique 64-hex clear-logo handles from optional
// presentation lookups. presentation may be nil.
func CollectLogoHandles(games []Game, start, end int, presentation func(string) Presentation) []string {
	start, end = clampPage(games, start, end)
	if start >= end || presentation == nil {
		return nil
	}
	out := make([]string, 0, end-start)
	seen := make(map[string]struct{}, end-start)
	for _, game := range games[start:end] {
		handle := LogoHandle(presentation(game.ID))
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

// CollectBox3DHandles returns unique 64-hex 3D box/cart handles from optional
// presentation lookups. presentation may be nil.
func CollectBox3DHandles(games []Game, start, end int, presentation func(string) Presentation) []string {
	start, end = clampPage(games, start, end)
	if start >= end || presentation == nil {
		return nil
	}
	out := make([]string, 0, end-start)
	seen := make(map[string]struct{}, end-start)
	for _, game := range games[start:end] {
		handle := Box3DHandle(presentation(game.ID))
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

// CollectBackdropHandles returns unique 64-hex fanart/backdrop handles from
// optional presentation lookups. presentation may be nil.
func CollectBackdropHandles(games []Game, start, end int, presentation func(string) Presentation) []string {
	start, end = clampPage(games, start, end)
	if start >= end || presentation == nil {
		return nil
	}
	out := make([]string, 0, end-start)
	seen := make(map[string]struct{}, end-start)
	for _, game := range games[start:end] {
		handle := BackdropHandle(presentation(game.ID))
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

func clampPage(games []Game, start, end int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end > len(games) {
		end = len(games)
	}
	return start, end
}

// Request starts fetches for unknown handles and returns immediately.
// A nil client is allowed when a store can still satisfy disk hits.
func (c *CoverCache) Request(ctx context.Context, client ArtworkFetcher, handles []string) {
	if c == nil {
		return
	}
	for _, handle := range handles {
		handle = normalizeHandle(handle)
		if handle == "" {
			continue
		}
		c.mu.Lock()
		store := c.store
		_, have := c.images[handle]
		_, failed := c.failed[handle]
		_, busy := c.inflight[handle]
		slots := len(c.inflight)
		if have || failed || busy || slots >= maxCoverFetches || (client == nil && store == nil) {
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
	c.mu.Lock()
	store := c.store
	decode := c.decode
	c.mu.Unlock()
	if decode == nil {
		decode = DecodeCover
	}

	var (
		data []byte
		img  *image.RGBA
		err  error
	)
	if store != nil {
		if blob, ok := store.LoadArtwork(handle); ok {
			img, err = decode(blob)
			if err == nil && img != nil {
				c.finishFetch(handle, img, nil)
				return
			}
			err = nil
			img = nil
		}
	}
	if client == nil {
		c.finishFetch(handle, nil, errNoArtwork)
		return
	}
	data, _, err = client.Artwork(ctx, handle)
	if err == nil {
		img, err = decode(data)
	}
	if err == nil && img != nil && store != nil && len(data) > 0 {
		_ = store.SaveArtwork(handle, data)
	}
	c.finishFetch(handle, img, err)
}

func (c *CoverCache) finishFetch(handle string, img *image.RGBA, err error) {
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
