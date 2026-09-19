// Package hostclient contains the UI-independent client for FogCast's public
// host API.
package hostclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAPIBase     = "http://127.0.0.1:8787"
	defaultPageLimit   = 200
	defaultMaxGames    = 10000
	maxResponseBytes   = 16 << 20
	maxArtworkBytes    = 8 << 20
	maxVideoBytes      = 128 << 20
	defaultHTTPTimeout = 15 * time.Second
	launchHTTPTimeout  = 60 * time.Second
	stopHTTPTimeout    = 60 * time.Second
	videoHTTPTimeout   = 120 * time.Second
)

// ClientStamp is a transport-neutral client wall/monotonic clock stamp.
// Sofa-specific flight bookkeeping remains in ui/tenfoot.
type ClientStamp struct {
	TsUTC    string
	MonoMS   int64
	FlightID string
}

var processStart = time.Now()

// ClientStampNow records UTC wall time and process-monotonic milliseconds.
func ClientStampNow() ClientStamp {
	return ClientStamp{
		TsUTC:  time.Now().UTC().Format(time.RFC3339Nano),
		MonoMS: time.Since(processStart).Milliseconds(),
	}
}

func applyClientStamp(req *http.Request, stamp ClientStamp) {
	if req == nil {
		return
	}
	if stamp.TsUTC == "" && stamp.MonoMS == 0 && stamp.FlightID == "" {
		stamp = ClientStampNow()
	}
	if stamp.TsUTC != "" {
		req.Header.Set("X-FogCast-Client-Ts-Utc", stamp.TsUTC)
	}
	req.Header.Set("X-FogCast-Client-Mono-Ms", strconv.FormatInt(stamp.MonoMS, 10))
	if id := strings.TrimSpace(stamp.FlightID); id != "" {
		req.Header.Set("X-FogCast-Flight-Id", id)
	}
}

// Client owns the public host transport and library/session requests. The
// specialized clients retain the historical per-operation deadlines.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	launchHTTP  *http.Client
	stopHTTP    *http.Client
	videoHTTP   *http.Client
	previewHTTP *http.Client
}

// NewClient builds a host API client. It preserves the caller's transport,
// redirect policy, cookie jar, and other http.Client settings.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultAPIBase
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	launchHTTP := *httpClient
	if launchHTTP.Timeout == 0 || launchHTTP.Timeout < launchHTTPTimeout {
		launchHTTP.Timeout = launchHTTPTimeout
	}
	stopHTTP := *httpClient
	if stopHTTP.Timeout == 0 || stopHTTP.Timeout < stopHTTPTimeout {
		stopHTTP.Timeout = stopHTTPTimeout
	}
	videoHTTP := *httpClient
	if videoHTTP.Timeout != 0 && videoHTTP.Timeout < videoHTTPTimeout {
		videoHTTP.Timeout = videoHTTPTimeout
	}
	previewHTTP := *httpClient
	previewHTTP.Timeout = 0
	return &Client{
		baseURL:     baseURL,
		httpClient:  httpClient,
		launchHTTP:  &launchHTTP,
		stopHTTP:    &stopHTTP,
		videoHTTP:   &videoHTTP,
		previewHTTP: &previewHTTP,
	}
}

// BaseURL returns the normalized host API base URL.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// HTTPClient returns the normal polling/client transport.
func (c *Client) HTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	return c.httpClient
}

// LaunchHTTPClient returns the long-deadline launch transport.
func (c *Client) LaunchHTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	return c.launchHTTP
}

// StopHTTPClient returns the long-deadline stop transport.
func (c *Client) StopHTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	return c.stopHTTP
}

// VideoHTTPClient returns the artwork/video transport.
func (c *Client) VideoHTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	return c.videoHTTP
}

// PreviewHTTPClient returns the no-client-timeout preview transport.
func (c *Client) PreviewHTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	return c.previewHTTP
}

// NewRequest builds a request against the configured host API. The caller
// owns the returned request and response lifecycle.
func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if c == nil {
		return nil, fmt.Errorf("host client is nil")
	}
	return http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
}

// WithAPIHost returns a client that sends Host: host while keeping the
// connection URL and all caller transport/client settings unchanged.
func (c *Client) WithAPIHost(host string) *Client {
	host = strings.TrimSpace(host)
	if c == nil || host == "" {
		return c
	}
	wrap := func(src *http.Client) *http.Client {
		if src == nil {
			src = &http.Client{Timeout: defaultHTTPTimeout}
		}
		clone := *src
		base := clone.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		clone.Transport = apiHostTransport{base: base, host: host}
		return &clone
	}
	out := *c
	out.httpClient = wrap(c.httpClient)
	out.launchHTTP = wrap(c.launchHTTP)
	out.stopHTTP = wrap(c.stopHTTP)
	out.videoHTTP = wrap(c.videoHTTP)
	out.previewHTTP = wrap(c.previewHTTP)
	return &out
}

type apiHostTransport struct {
	base http.RoundTripper
	host string
}

func (t apiHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Host = t.host
	return t.base.RoundTrip(clone)
}

// ListGames fetches one catalog page. grouped=1 and availability=ready stay the default.
func (c *Client) ListGames(ctx context.Context, query GameListQuery) ([]Game, string, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	}
	values := url.Values{}
	values.Set("grouped", "1")
	values.Set("availability", "ready")
	values.Set("limit", strconv.Itoa(limit))
	if platform := strings.TrimSpace(query.Platform); platform != "" {
		values.Set("platform", platform)
	}
	if sort := catalogSortParam(query.Sort); sort != "" {
		values.Set("sort", sort)
	}
	if q := strings.TrimSpace(query.Q); q != "" {
		values.Set("q", q)
	}
	if collection := strings.TrimSpace(query.Collection); collection != "" {
		values.Set("collection", collection)
	}
	if genre := strings.TrimSpace(query.Genre); genre != "" {
		values.Set("genre", genre)
	}
	if year := strings.TrimSpace(query.Year); year != "" {
		values.Set("year", year)
	}
	if region := strings.TrimSpace(query.Region); region != "" {
		values.Set("region", region)
	}
	if query.HidePrerelease {
		values.Set("hide_prerelease", "1")
	}
	if query.HideHacks {
		values.Set("hide_hacks", "1")
	}
	if strings.TrimSpace(query.Cursor) != "" {
		values.Set("cursor", query.Cursor)
	}
	var page struct {
		Games      []Game `json:"games"`
		NextCursor string `json:"next_cursor"`
	}
	if err := c.getJSON(ctx, "/api/v1/games?"+values.Encode(), &page); err != nil {
		return nil, "", err
	}
	if page.Games == nil {
		page.Games = []Game{}
	}
	for i, game := range page.Games {
		page.Games[i] = preferLaunchable(game)
	}
	return page.Games, page.NextCursor, nil
}

// Game loads one catalog row from GET /api/v1/games/{id}.
func (c *Client) Game(ctx context.Context, id string) (Game, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Game{}, fmt.Errorf("hostclient: game id is required")
	}
	var game Game
	if err := c.getJSON(ctx, "/api/v1/games/"+url.PathEscape(id), &game); err != nil {
		return Game{}, err
	}
	return preferLaunchable(game), nil
}

// CoreLibrary loads the selected core entries and the installed package
// inventory. Both reads are required for a truthful selected-package status.
func (c *Client) CoreLibrary(ctx context.Context) (CoreLibrary, error) {
	var entries struct {
		Entries []CoreEntry `json:"entries"`
	}
	if err := c.getJSON(ctx, "/api/v1/library/core-entries", &entries); err != nil {
		return CoreLibrary{}, err
	}
	var packages struct {
		Packages []corePackageWire `json:"packages"`
	}
	if err := c.getJSON(ctx, "/api/v1/core-packages", &packages); err != nil {
		return CoreLibrary{}, err
	}
	result := CoreLibrary{
		Entries:  entries.Entries,
		Packages: make([]CorePackage, 0, len(packages.Packages)),
	}
	if result.Entries == nil {
		result.Entries = []CoreEntry{}
	}
	for _, packageInfo := range packages.Packages {
		result.Packages = append(result.Packages, CorePackage{
			PackageID:     strings.TrimSpace(packageInfo.PackageID),
			CoreID:        strings.TrimSpace(packageInfo.Descriptor.Core.ID),
			Name:          strings.TrimSpace(packageInfo.Descriptor.Core.Name),
			Version:       strings.TrimSpace(packageInfo.Descriptor.Core.Version),
			Compatibility: strings.TrimSpace(packageInfo.Compatibility),
		})
	}
	return result, nil
}

func catalogSortParam(sort string) string {
	switch strings.TrimSpace(sort) {
	case "title":
		return "title"
	case "recently_added":
		return "recently_added"
	case "platform", "system":
		return "platform"
	default:
		return ""
	}
}

// preferLaunchable keeps a grouped row launchable when the region-picked
// representative is offline or otherwise blocked but a sibling variant is not.
func preferLaunchable(game Game) Game {
	if game.LaunchEligible() {
		game.Variants = nil
		return game
	}
	for _, variant := range game.Variants {
		variant.Variants = nil
		if variant.LaunchEligible() {
			if !variant.Favorite {
				variant.Favorite = game.Favorite
			}
			if len(variant.Collections) == 0 {
				variant.Collections = game.Collections
			}
			return variant
		}
	}
	game.Variants = nil
	return game
}

// Collections loads GET /api/v1/library/collections.
func (c *Client) Collections(ctx context.Context) ([]Collection, error) {
	var page struct {
		Collections []Collection `json:"collections"`
	}
	if err := c.getJSON(ctx, "/api/v1/library/collections", &page); err != nil {
		return nil, err
	}
	if page.Collections == nil {
		page.Collections = []Collection{}
	}
	return page.Collections, nil
}

// SetCollectionMember calls PUT or DELETE /api/v1/library/collections/{id}/{gameId}.
func (c *Client) SetCollectionMember(ctx context.Context, collectionID, gameID string, member bool) error {
	collectionID = strings.TrimSpace(collectionID)
	gameID = strings.TrimSpace(gameID)
	if collectionID == "" {
		return fmt.Errorf("collection id is empty")
	}
	if gameID == "" {
		return fmt.Errorf("game id is empty")
	}
	method := http.MethodPut
	if !member {
		method = http.MethodDelete
	}
	path := "/api/v1/library/collections/" + url.PathEscape(collectionID) + "/" + url.PathEscape(gameID)
	var result struct {
		ID         string `json:"id"`
		Collection string `json:"collection"`
		Member     bool   `json:"member"`
	}
	if err := c.mutateJSON(ctx, method, path, &result); err != nil {
		return err
	}
	if result.Member != member {
		return fmt.Errorf("host API member = %v, want %v", result.Member, member)
	}
	return nil
}

// UpsertCollection calls PUT /api/v1/library/collections/{id}?name=... with an empty body.
func (c *Client) UpsertCollection(ctx context.Context, id, name string) (Collection, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return Collection{}, fmt.Errorf("collection id is empty")
	}
	if name == "" {
		return Collection{}, fmt.Errorf("collection name is empty")
	}
	path := "/api/v1/library/collections/" + url.PathEscape(id) + "?name=" + url.QueryEscape(name)
	var result Collection
	if err := c.mutateJSON(ctx, http.MethodPut, path, &result); err != nil {
		return Collection{}, err
	}
	if strings.TrimSpace(result.ID) == "" {
		result.ID = id
	}
	if strings.TrimSpace(result.Name) == "" {
		result.Name = name
	}
	return result, nil
}

// DeleteCollection calls DELETE /api/v1/library/collections/{id}.
func (c *Client) DeleteCollection(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("collection id is empty")
	}
	var result struct {
		ID string `json:"id"`
	}
	return c.mutateJSON(ctx, http.MethodDelete, "/api/v1/library/collections/"+url.PathEscape(id), &result)
}

// SetFavorite calls PUT or DELETE /api/v1/library/favorites/{id}.
func (c *Client) SetFavorite(ctx context.Context, gameID string, favorite bool) error {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return fmt.Errorf("game id is empty")
	}
	method := http.MethodPut
	if !favorite {
		method = http.MethodDelete
	}
	path := "/api/v1/library/favorites/" + url.PathEscape(gameID)
	var result struct {
		ID       string `json:"id"`
		Favorite bool   `json:"favorite"`
	}
	if err := c.mutateJSON(ctx, method, path, &result); err != nil {
		return err
	}
	if result.Favorite != favorite {
		return fmt.Errorf("host API favorite = %v, want %v", result.Favorite, favorite)
	}
	return nil
}

// Facets loads GET /api/v1/library/facets. Nil genre/year arrays become empty.
func (c *Client) Facets(ctx context.Context) (FacetValues, error) {
	var values FacetValues
	if err := c.getJSON(ctx, "/api/v1/library/facets", &values); err != nil {
		return FacetValues{Genres: []string{}, Years: []string{}}, err
	}
	return normalizeFacets(values), nil
}

func normalizeFacets(values FacetValues) FacetValues {
	if values.Genres == nil {
		values.Genres = []string{}
	}
	if values.Years == nil {
		values.Years = []string{}
	}
	return values
}

// Platforms loads GET /api/v1/platforms.
func (c *Client) Platforms(ctx context.Context) ([]Platform, error) {
	var page struct {
		Platforms []Platform `json:"platforms"`
	}
	if err := c.getJSON(ctx, "/api/v1/platforms", &page); err != nil {
		return nil, err
	}
	if page.Platforms == nil {
		page.Platforms = []Platform{}
	}
	return page.Platforms, nil
}

// FetchLibrary walks catalog pages until maxGames or the cursor ends.
func (c *Client) FetchLibrary(ctx context.Context, query GameListQuery, maxGames int) ([]Game, error) {
	if query.Limit <= 0 {
		query.Limit = defaultPageLimit
	}
	if maxGames <= 0 {
		maxGames = defaultMaxGames
	}
	var (
		all    []Game
		cursor string
	)
	for {
		if err := ctx.Err(); err != nil {
			return all, err
		}
		query.Cursor = cursor
		page, next, err := c.ListGames(ctx, query)
		if err != nil {
			return all, err
		}
		remain := maxGames - len(all)
		if remain <= 0 {
			break
		}
		if len(page) > remain {
			page = page[:remain]
		}
		all = append(all, page...)
		if next == "" || len(all) >= maxGames {
			break
		}
		cursor = next
	}
	return all, nil
}

// LibraryCache loads GET /api/v1/library/cache (ROM used/free from the target).
func (c *Client) LibraryCache(ctx context.Context) (LibraryCache, error) {
	var result LibraryCache
	err := c.getJSON(ctx, "/api/v1/library/cache", &result)
	return result, err
}

// GamePresentation loads presentation metadata, including cover artwork IDs.
func (c *Client) GamePresentation(ctx context.Context, gameID string) (Presentation, error) {
	var result Presentation
	path := "/api/v1/presentation/games/" + url.PathEscape(strings.TrimSpace(gameID))
	err := c.getJSON(ctx, path, &result)
	return result, err
}

// LibrarySettings loads GET /api/v1/library/settings.
func (c *Client) LibrarySettings(ctx context.Context) (LibrarySettings, error) {
	var result LibrarySettings
	if err := c.getJSON(ctx, "/api/v1/library/settings", &result); err != nil {
		return LibrarySettings{}, err
	}
	return normalizeLibrarySettings(result), nil
}

// PatchLibrarySettings sends PATCH /api/v1/library/settings with only set fields.
func (c *Client) PatchLibrarySettings(ctx context.Context, patch LibrarySettingsPatch) (LibrarySettings, error) {
	payload, err := patch.payload()
	if err != nil {
		return LibrarySettings{}, err
	}
	var result LibrarySettings
	if err := c.doJSON(ctx, http.MethodPatch, "/api/v1/library/settings", payload, &result); err != nil {
		return LibrarySettings{}, err
	}
	return normalizeLibrarySettings(result), nil
}

func normalizeLibrarySettings(result LibrarySettings) LibrarySettings {
	if result.PreferredRegions == nil {
		result.PreferredRegions = []string{}
	}
	if result.Targets == nil {
		result.Targets = []LibraryTarget{}
	}
	if result.Libraries == nil {
		result.Libraries = []LibraryRoot{}
	}
	if result.Systems == nil {
		result.Systems = []LibrarySystem{}
	}
	if result.AttractIdleSeconds <= 0 {
		result.AttractIdleSeconds = defaultAttractIdleSeconds
	}
	return result
}

// Attract loads GET /api/v1/library/attract?limit=N.
func (c *Client) Attract(ctx context.Context, limit int) (AttractPlaylist, error) {
	if limit <= 0 {
		limit = defaultAttractLimit
	}
	var page AttractPlaylist
	if err := c.getJSON(ctx, "/api/v1/library/attract?limit="+strconv.Itoa(limit), &page); err != nil {
		return AttractPlaylist{}, err
	}
	if page.Items == nil {
		page.Items = []AttractItem{}
	}
	if page.IdleSeconds <= 0 {
		page.IdleSeconds = defaultAttractIdleSeconds
	}
	return page, nil
}

// Artwork fetches raw cover bytes from GET /api/v1/presentation/artwork/{handle}.
func (c *Client) Artwork(ctx context.Context, handle string) ([]byte, string, error) {
	handle = NormalizeHandle(handle)
	if handle == "" {
		return nil, "", fmt.Errorf("artwork handle is invalid")
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/v1/presentation/artwork/"+handle, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxArtworkBytes)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", APIStatusError(resp.StatusCode, body)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// FetchVideoFile streams GET /api/v1/presentation/artwork/{handle} to a temp file.
// The caller must remove the file. Accept is video/*; the still Artwork path is unchanged.
func (c *Client) FetchVideoFile(ctx context.Context, handle string) (string, error) {
	handle = NormalizeHandle(handle)
	if handle == "" {
		return "", fmt.Errorf("artwork handle is invalid")
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/v1/presentation/artwork/"+handle, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "video/*")
	resp, err := c.videoHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil && len(body) == 0 {
			return "", readErr
		}
		return "", APIStatusError(resp.StatusCode, body)
	}
	contentType := resp.Header.Get("Content-Type")
	if imageContentType(contentType) {
		return "", fmt.Errorf("artwork is not video (%s)", contentType)
	}
	if resp.ContentLength > maxVideoBytes {
		return "", fmt.Errorf("video exceeds %d bytes", maxVideoBytes)
	}
	file, err := os.CreateTemp("", "fogcast-attract-*.bin")
	if err != nil {
		return "", err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	n, err := io.Copy(file, io.LimitReader(resp.Body, maxVideoBytes+1))
	if err != nil {
		return "", err
	}
	if n > maxVideoBytes {
		return "", fmt.Errorf("video exceeds %d bytes", maxVideoBytes)
	}
	if resp.ContentLength >= 0 && n < resp.ContentLength {
		return "", fmt.Errorf("%w: read %d of %d bytes", ErrResponseTruncated, n, resp.ContentLength)
	}
	if n < 16 {
		return "", fmt.Errorf("video is too small")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	var header [16]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return "", err
	}
	if !videoContentType(contentType) && sniffVideoMIME(header[:]) == "" {
		return "", fmt.Errorf("artwork is not video (%s)", contentType)
	}
	ok = true
	return path, nil
}
