// Package tenfoot is the native SDL3 10-foot FogCast launcher.
//
// It talks to the public host API over HTTP. Catalog, content transfer, and
// the MiSTer command path stay in the existing host and target processes.
package tenfoot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	maxAPIResponse     = 16 << 20
	maxArtworkBytes    = 8 << 20
	maxVideoBytes      = 128 << 20 // matches librarymedia.MaxVideoBytes
	artworkHandleLen   = 64
	defaultHTTPTimeout = 15 * time.Second
	launchHTTPTimeout  = 60 * time.Second
	stopHTTPTimeout    = 60 * time.Second
	videoHTTPTimeout   = 120 * time.Second
)

// Game is one catalog row from GET /api/v1/games.
type Game struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	System      string   `json:"system"`
	Cover       string   `json:"cover,omitempty"`
	Genre       string   `json:"genre,omitempty"`
	Year        string   `json:"year,omitempty"`
	Region      string   `json:"region,omitempty"`
	State       string   `json:"state"`
	RootOnline  bool     `json:"root_online"`
	Launchable  bool     `json:"launchable"`
	Favorite    bool     `json:"favorite,omitempty"`
	Collections []string `json:"collections,omitempty"`
	Variants    []Game   `json:"variants,omitempty"`
}

// Presentation is GET /api/v1/presentation/games/{id}.
type Presentation struct {
	GameID       string                   `json:"game_id"`
	State        string                   `json:"state"`
	Presentation *PresentationInfo        `json:"presentation"`
	Attribution  *PresentationAttribution `json:"attribution,omitempty"`
}

// PresentationInfo is the nested presentation object on a games/{id} payload.
type PresentationInfo struct {
	CoverArtworkID    string   `json:"cover_artwork_id"`
	BackdropArtworkID string   `json:"backdrop_artwork_id,omitempty"`
	LogoID            string   `json:"logo_id,omitempty"`
	Summary           string   `json:"summary"`
	Year              string   `json:"year"`
	Genre             string   `json:"genre"`
	Studio            string   `json:"studio"`
	Players           string   `json:"players"`
	ScreenshotIDs     []string `json:"screenshot_ids,omitempty"`
}

// PresentationAttribution is the provider label the public API returns with ready metadata.
type PresentationAttribution struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

// AttributionLabel returns the validated IGDB or LaunchBox label, or empty.
func (p Presentation) AttributionLabel() string {
	if p.Attribution == nil {
		return ""
	}
	provider := strings.TrimSpace(p.Attribution.Provider)
	label := strings.TrimSpace(p.Attribution.Label)
	switch {
	case provider == "igdb" && label == "Data from IGDB.com":
		return label
	case provider == "launchbox" && label == "Data from LaunchBox Games Database":
		return label
	default:
		return ""
	}
}

// Platform is one row from GET /api/v1/platforms.
type Platform struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	GameCount  int    `json:"game_count"`
	Online     bool   `json:"online"`
	Launchable bool   `json:"launchable"`
}

// Collection is one custom shelf from GET /api/v1/library/collections.
type Collection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

const (
	defaultAttractLimit       = 24
	defaultAttractIdleSeconds = 60
)

// AttractItem is one row from GET /api/v1/library/attract.
type AttractItem struct {
	GameID     string `json:"game_id"`
	Title      string `json:"title"`
	Platform   string `json:"platform"`
	Video      string `json:"video,omitempty"`
	Cover      string `json:"cover,omitempty"`
	Backdrop   string `json:"backdrop,omitempty"`
	Marquee    string `json:"marquee,omitempty"`
	Launchable bool   `json:"launchable"`
}

// AttractPlaylist is the attract response, including host idle_seconds.
type AttractPlaylist struct {
	Items       []AttractItem `json:"items"`
	IdleSeconds int           `json:"idle_seconds"`
}

// StillHandle prefers backdrop, then cover, then marquee. Video is ignored.
func (item AttractItem) StillHandle() string {
	handles := item.StillHandles()
	if len(handles) == 0 {
		return ""
	}
	return handles[0]
}

// StillHandles is backdrop, then cover, then marquee, de-duplicated. Video is ignored.
func (item AttractItem) StillHandles() []string {
	return item.stillHandles()
}

func (item AttractItem) stillHandles() []string {
	out := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, handle := range []string{item.Backdrop, item.Cover, item.Marquee} {
		got := normalizeHandle(handle)
		if got == "" || seen[got] {
			continue
		}
		seen[got] = true
		out = append(out, got)
	}
	return out
}

// GameListQuery is GET /api/v1/games with the web UI's catalog params.
type GameListQuery struct {
	Cursor         string
	Limit          int
	Platform       string
	Sort           string
	Q              string
	Collection     string
	Genre          string
	Year           string
	Region         string
	HidePrerelease bool
	HideHacks      bool
}

// FacetValues is GET /api/v1/library/facets. The host does not return regions.
type FacetValues struct {
	Genres []string `json:"genres"`
	Years  []string `json:"years"`
}

// SessionProgress is the optional progress object on sessionResult.
type SessionProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// SessionInput is the read-only remote-input object on sessionResult
// and GET /api/v1/session/input.
type SessionInput struct {
	State string `json:"state"`
	Ready bool   `json:"ready"`
}

// HealthResult is GET /api/v1/health. HTTP 200 while the host process is up.
type HealthResult struct {
	Ready           bool
	TargetReachable bool
	TargetReady     bool
	Connection      TargetConnection
}

// TargetConnection is discovery and reconciliation state, independent of game state.
type TargetConnection struct {
	State    string `json:"state"`
	Message  string `json:"message,omitempty"`
	Address  string `json:"address,omitempty"`
	TargetID string `json:"target_id,omitempty"`
	BootID   string `json:"boot_id,omitempty"`
	Owner    string `json:"owner,omitempty"`
}

// TargetStatus is GET /api/v1/status. HTTP 503 TARGET_UNAVAILABLE means the kit
// did not answer; that is not a host-process failure.
type TargetStatus struct {
	HTTPStatus   int
	State        string
	GameID       string
	System       string
	Core         string
	ErrorCode    string
	ErrorMessage string
	Unavailable  bool
	Connection   TargetConnection
}

// SessionResult is sessionResult from GET /api/v1/session, POST launch, POST stop,
// and POST /api/v1/session/development-rbf.
type SessionResult struct {
	HTTPStatus              int
	State                   string
	GameID                  string
	System                  string
	Execution               string
	Media                   string
	Progress                *SessionProgress
	Input                   *SessionInput
	Development             bool
	DevelopmentSessionState string
	ErrorCode               string
	ErrorMessage            string
}

// SessionEvent is one row from GET /api/v1/session/events.
type SessionEvent struct {
	Sequence uint64           `json:"sequence"`
	Event    string           `json:"event"`
	State    string           `json:"state"`
	GameID   string           `json:"game_id,omitempty"`
	System   string           `json:"system,omitempty"`
	Media    string           `json:"media,omitempty"`
	Progress *SessionProgress `json:"progress,omitempty"`
	Input    *SessionInput    `json:"input,omitempty"`
}

// KitLeaseStatus is GET /v1/kit/lease on the selected target (status-only).
type KitLeaseStatus struct {
	HTTPStatus   int
	State        string
	Owner        string
	Purpose      string
	Generation   string
	ExpiresAt    string
	ExpiresInMS  int64
	Reason       string
	ErrorCode    string
	ErrorMessage string
	Unavailable  bool
}

// LaunchResult is the host response to POST /api/v1/session/launch.
type LaunchResult = SessionResult

// Client calls the FogCast public host API.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	launchHTTP  *http.Client
	stopHTTP    *http.Client
	videoHTTP   *http.Client
	previewHTTP *http.Client
}

// NewClient builds a host API client. baseURL defaults to DefaultAPIBase.
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
	return &Client{baseURL: baseURL, httpClient: httpClient, launchHTTP: &launchHTTP, stopHTTP: &stopHTTP, videoHTTP: &videoHTTP, previewHTTP: &previewHTTP}
}

// withAPIHost returns a client that sends Host: host on every request.
// The connection URL is unchanged. Empty host is a no-op.
func (c *Client) withAPIHost(host string) *Client {
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

func apiURLIsLoopback(baseURL string) bool {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackAPIHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	port := "8787"
	if err == nil {
		if p := u.Port(); p != "" {
			port = p
		} else if u.Scheme == "https" {
			port = "443"
		} else if u.Scheme == "http" {
			port = "80"
		}
	}
	return net.JoinHostPort("127.0.0.1", port)
}

// smokeAPIHost is the Host header for -smoke against a non-loopback API URL.
// The host API allowlist is loopback; Docker/host-gateway URLs still connect
// but send Host: host.docker.internal and get 403 HOST_NOT_ALLOWED.
func smokeAPIHost(baseURL, explicit string) string {
	if host := strings.TrimSpace(explicit); host != "" {
		return host
	}
	if apiURLIsLoopback(baseURL) {
		return ""
	}
	return loopbackAPIHost(baseURL)
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
	if launchBlockReason(game) == "" {
		game.Variants = nil
		return game
	}
	for _, variant := range game.Variants {
		variant.Variants = nil
		if launchBlockReason(variant) == "" {
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

// GamePresentation loads presentation metadata, including cover artwork IDs.
func (c *Client) GamePresentation(ctx context.Context, gameID string) (Presentation, error) {
	var result Presentation
	path := "/api/v1/presentation/games/" + url.PathEscape(strings.TrimSpace(gameID))
	err := c.getJSON(ctx, path, &result)
	return result, err
}

// CoverHandle returns a 64-hex artwork handle from a list cover or presentation.
func CoverHandle(game Game, presentation Presentation) string {
	if handle := normalizeHandle(game.Cover); handle != "" {
		return handle
	}
	if presentation.Presentation == nil {
		return ""
	}
	return normalizeHandle(presentation.Presentation.CoverArtworkID)
}

// LogoHandle returns a 64-hex clear-logo handle from presentation.
func LogoHandle(presentation Presentation) string {
	if presentation.Presentation == nil {
		return ""
	}
	return normalizeHandle(presentation.Presentation.LogoID)
}

// BackdropHandle returns a 64-hex fanart/backdrop handle from presentation.
func BackdropHandle(presentation Presentation) string {
	if presentation.Presentation == nil {
		return ""
	}
	return normalizeHandle(presentation.Presentation.BackdropArtworkID)
}

// LibraryTarget is one target row from GET /api/v1/library/settings.
// The host never returns the agent secret; AgentConfigured is the only
// secret-related field the client stores.
type LibraryTarget struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	Enabled         bool   `json:"enabled"`
	AgentConfigured bool   `json:"agent_configured"`
	TargetID        string `json:"target_id,omitempty"`
}

// LibraryTargetWrite is one target in a PATCH /api/v1/library/settings body.
// Agent is omitted when nil (untouched). A pointer to "" clears the stored agent.
type LibraryTargetWrite struct {
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name,omitempty"`
	Address      string  `json:"address"`
	Enabled      bool    `json:"enabled"`
	Agent        *string `json:"agent,omitempty"`
}

// LibraryRoot is one library path row from GET /api/v1/library/settings.
type LibraryRoot struct {
	ID     string `json:"id"`
	System string `json:"system"`
	Root   string `json:"root"`
}

// LibrarySystem is one platform label from GET /api/v1/library/settings.
type LibrarySystem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// LibrarySettings is GET /api/v1/library/settings.
type LibrarySettings struct {
	AttractIdleSeconds int             `json:"attract_idle_seconds"`
	PreferredRegions   []string        `json:"preferred_regions"`
	SelectedTarget     string          `json:"selected_target"`
	Targets            []LibraryTarget `json:"targets"`
	Libraries          []LibraryRoot   `json:"libraries"`
	Systems            []LibrarySystem `json:"systems"`
}

// LibrarySettingsPatch is a partial PATCH /api/v1/library/settings body.
// Nil fields are omitted. Libraries and Targets are full-array replaces when
// non-nil, including an empty list.
type LibrarySettingsPatch struct {
	AttractIdleSeconds *int                  `json:"attract_idle_seconds,omitempty"`
	PreferredRegions   *[]string             `json:"preferred_regions,omitempty"`
	SelectedTarget     *string               `json:"selected_target,omitempty"`
	Targets            *[]LibraryTargetWrite `json:"targets,omitempty"`
	Libraries          *[]LibraryRoot        `json:"libraries,omitempty"`
	PrepareTarget      *string               `json:"prepare_target,omitempty"`
}

func (p LibrarySettingsPatch) payload() (map[string]any, error) {
	raw := map[string]any{}
	if p.AttractIdleSeconds != nil {
		raw["attract_idle_seconds"] = *p.AttractIdleSeconds
	}
	if p.PreferredRegions != nil {
		regions := append([]string(nil), *p.PreferredRegions...)
		if regions == nil {
			regions = []string{}
		}
		raw["preferred_regions"] = regions
	}
	if p.SelectedTarget != nil {
		raw["selected_target"] = *p.SelectedTarget
	}
	if p.Targets != nil {
		targets := append([]LibraryTargetWrite(nil), *p.Targets...)
		if targets == nil {
			targets = []LibraryTargetWrite{}
		}
		raw["targets"] = targets
	}
	if p.Libraries != nil {
		libraries := append([]LibraryRoot(nil), *p.Libraries...)
		if libraries == nil {
			libraries = []LibraryRoot{}
		}
		raw["libraries"] = libraries
	}
	if p.PrepareTarget != nil {
		raw["prepare_target"] = *p.PrepareTarget
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("settings patch is empty")
	}
	return raw, nil
}

// LibrarySettings loads GET /api/v1/library/settings.
func (c *Client) LibrarySettings(ctx context.Context) (LibrarySettings, error) {
	var result LibrarySettings
	if err := c.getJSON(ctx, "/api/v1/library/settings", &result); err != nil {
		return LibrarySettings{}, err
	}
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
	return result, nil
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
	return result, nil
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
	handle = normalizeHandle(handle)
	if handle == "" {
		return nil, "", fmt.Errorf("artwork handle is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/presentation/artwork/"+handle, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArtworkBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxArtworkBytes {
		return nil, "", fmt.Errorf("artwork exceeds %d bytes", maxArtworkBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", apiStatusError(resp.StatusCode, body)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// FetchVideoFile streams GET /api/v1/presentation/artwork/{handle} to a temp file.
// The caller must remove the file. Accept is video/*; the still Artwork path is unchanged.
func (c *Client) FetchVideoFile(ctx context.Context, handle string) (string, error) {
	handle = normalizeHandle(handle)
	if handle == "" {
		return "", fmt.Errorf("artwork handle is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/presentation/artwork/"+handle, nil)
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
		return "", apiStatusError(resp.StatusCode, body)
	}
	ct := resp.Header.Get("Content-Type")
	if imageContentType(ct) {
		return "", fmt.Errorf("artwork is not video (%s)", ct)
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
	if !videoContentType(ct) && sniffVideoMIME(header[:]) == "" {
		return "", fmt.Errorf("artwork is not video (%s)", ct)
	}
	ok = true
	return path, nil
}

func imageContentType(value string) bool {
	return strings.HasPrefix(contentTypeMain(value), "image/")
}

func videoContentType(value string) bool {
	switch contentTypeMain(value) {
	case "video/mp4", "video/webm", "video/x-m4v", "video/quicktime":
		return true
	default:
		return false
	}
}

func contentTypeMain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if i := strings.Index(value, ";"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return value
}

func sniffVideoMIME(header []byte) string {
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		return "video/mp4"
	}
	if len(header) >= 4 && header[0] == 0x1A && header[1] == 0x45 && header[2] == 0xDF && header[3] == 0xA3 {
		return "video/webm"
	}
	return ""
}

// Launch posts {game_id} to POST /api/v1/session/launch.
func (c *Client) Launch(ctx context.Context, gameID string) (LaunchResult, error) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return LaunchResult{}, fmt.Errorf("game id is empty")
	}
	payload, err := json.Marshal(struct {
		GameID string `json:"game_id"`
	}{GameID: gameID})
	if err != nil {
		return LaunchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/session/launch", bytes.NewReader(payload))
	if err != nil {
		return LaunchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.launchHTTP.Do(req)
	if err != nil {
		return LaunchResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return LaunchResult{}, err
	}
	result, err := decodeSessionBody(resp.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("launch response: %w", err)
	}
	if result.ErrorCode != "" {
		return result, nil
	}
	if result.State != "active" {
		return result, fmt.Errorf("launch response: expected active session, got %q", result.State)
	}
	return result, nil
}

// SessionEvents loads GET /api/v1/session/events?after=N (JSON poll, not SSE).
func (c *Client) SessionEvents(ctx context.Context, after uint64) ([]SessionEvent, error) {
	path := "/api/v1/session/events?after=" + strconv.FormatUint(after, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiStatusError(resp.StatusCode, body)
	}
	var wire struct {
		Events []struct {
			Sequence uint64           `json:"sequence"`
			Event    string           `json:"event"`
			State    string           `json:"state"`
			GameID   *string          `json:"game_id"`
			System   *string          `json:"system"`
			Media    string           `json:"media"`
			Progress *SessionProgress `json:"progress"`
			Input    *SessionInput    `json:"input"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("session events: %w", err)
	}
	out := make([]SessionEvent, 0, len(wire.Events))
	for _, row := range wire.Events {
		ev := SessionEvent{
			Sequence: row.Sequence,
			Event:    strings.TrimSpace(row.Event),
			State:    strings.TrimSpace(row.State),
			Media:    strings.TrimSpace(row.Media),
			Progress: row.Progress,
			Input:    row.Input,
		}
		if row.GameID != nil {
			ev.GameID = strings.TrimSpace(*row.GameID)
		}
		if row.System != nil {
			ev.System = strings.TrimSpace(*row.System)
		}
		out = append(out, ev)
	}
	return out, nil
}

// KitLease loads GET /v1/kit/lease on the selected target agent.
// This is a status-only read of the target kit API, not a host proxy.
func (c *Client) KitLease(ctx context.Context, targetBase string) (KitLeaseStatus, error) {
	targetBase = strings.TrimRight(strings.TrimSpace(targetBase), "/")
	if targetBase == "" {
		return KitLeaseStatus{}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetBase+"/v1/kit/lease", http.NoBody)
	if err != nil {
		return KitLeaseStatus{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isHostTransportError(err) {
			return KitLeaseStatus{Unavailable: true, ErrorMessage: "kit unreachable"}, nil
		}
		return KitLeaseStatus{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return KitLeaseStatus{}, err
	}
	result := decodeKitLeaseBody(resp.StatusCode, body)
	if resp.StatusCode == http.StatusOK {
		return result, nil
	}
	if result.Unavailable || result.ErrorCode != "" {
		return result, nil
	}
	return result, apiStatusError(resp.StatusCode, body)
}

func decodeKitLeaseBody(status int, body []byte) KitLeaseStatus {
	result := KitLeaseStatus{HTTPStatus: status}
	var wire struct {
		State       string `json:"state"`
		Owner       string `json:"owner"`
		Purpose     string `json:"purpose"`
		Generation  string `json:"generation"`
		ExpiresAt   string `json:"expires_at"`
		ExpiresInMS int64  `json:"expires_in_ms"`
		Reason      string `json:"reason"`
		Error       *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Status *struct {
			State       string `json:"state"`
			Owner       string `json:"owner"`
			Purpose     string `json:"purpose"`
			Generation  string `json:"generation"`
			ExpiresAt   string `json:"expires_at"`
			ExpiresInMS int64  `json:"expires_in_ms"`
			Reason      string `json:"reason"`
		} `json:"status"`
	}
	_ = json.Unmarshal(body, &wire)
	if wire.Status != nil {
		if wire.State == "" {
			wire.State = wire.Status.State
		}
		if wire.Owner == "" {
			wire.Owner = wire.Status.Owner
		}
		if wire.Purpose == "" {
			wire.Purpose = wire.Status.Purpose
		}
		if wire.Generation == "" {
			wire.Generation = wire.Status.Generation
		}
		if wire.ExpiresAt == "" {
			wire.ExpiresAt = wire.Status.ExpiresAt
		}
		if wire.ExpiresInMS == 0 {
			wire.ExpiresInMS = wire.Status.ExpiresInMS
		}
		if wire.Reason == "" {
			wire.Reason = wire.Status.Reason
		}
	}
	result.State = strings.TrimSpace(wire.State)
	result.Owner = strings.TrimSpace(wire.Owner)
	result.Purpose = strings.TrimSpace(wire.Purpose)
	result.Generation = strings.TrimSpace(wire.Generation)
	result.ExpiresAt = strings.TrimSpace(wire.ExpiresAt)
	result.ExpiresInMS = wire.ExpiresInMS
	result.Reason = strings.TrimSpace(wire.Reason)
	if wire.Error != nil {
		result.ErrorCode = strings.TrimSpace(wire.Error.Code)
		result.ErrorMessage = strings.TrimSpace(wire.Error.Message)
	}
	switch {
	case status == http.StatusServiceUnavailable && (result.ErrorCode == "TARGET_UNAVAILABLE" || result.ErrorCode == "KIT_LEASE_BLOCKED" || result.ErrorCode == "MISTER_UNAVAILABLE"):
		result.Unavailable = result.ErrorCode != "KIT_LEASE_BLOCKED"
		if result.ErrorCode == "KIT_LEASE_BLOCKED" && result.State == "" {
			result.State = "blocked"
		}
		if result.Reason == "" {
			result.Reason = result.ErrorMessage
		}
	case status == 0 || status >= 500:
		result.Unavailable = true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		if result.ErrorCode == "" {
			result.ErrorCode = "UNAUTHORIZED"
		}
		if result.ErrorMessage == "" {
			result.ErrorMessage = "kit lease unavailable"
		}
	}
	return result
}

// OpenSessionPreview starts GET /api/v1/session/preview and returns an MJPEG
// reader. 404 (no decoder route), 503 inactive, and transport failures are
// PreviewUnavailable. The caller must Close the stream.
func (c *Client) OpenSessionPreview(ctx context.Context) (*MJPEGStream, error) {
	if c == nil {
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	httpClient := c.previewHTTP
	if httpClient == nil {
		httpClient = c.httpClient
	}
	if httpClient == nil {
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/session/preview", http.NoBody)
	if err != nil {
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	req.Header.Set("Accept", previewContentType)
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = "session preview is unavailable"
		}
		return nil, PreviewUnavailable{Status: resp.StatusCode, Message: msg}
	}
	stream, err := NewMJPEGStream(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		_ = resp.Body.Close()
		if IsPreviewUnavailable(err) {
			return nil, err
		}
		return nil, PreviewUnavailable{Message: "session preview is unavailable"}
	}
	return stream, nil
}

// Session loads GET /api/v1/session.
func (c *Client) Session(ctx context.Context) (SessionResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/session", http.NoBody)
	if err != nil {
		return SessionResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return SessionResult{}, err
	}
	result, err := decodeSessionBody(resp.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("session response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if result.ErrorCode != "" {
			return result, fmt.Errorf("host API %d %s: %s", resp.StatusCode, result.ErrorCode, result.ErrorMessage)
		}
		return result, apiStatusError(resp.StatusCode, body)
	}
	return result, nil
}

// LoadDevelopmentRBF posts a bounded application/octet-stream body to
// POST /api/v1/session/development-rbf. Content-Length is required. Empty and
// >32MiB payloads are rejected before the request.
func (c *Client) LoadDevelopmentRBF(ctx context.Context, size int64, content io.Reader) (SessionResult, error) {
	if err := developmentRBFSizeError(size); err != nil {
		return SessionResult{}, err
	}
	if content == nil {
		return SessionResult{}, fmt.Errorf("development RBF input is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/session/development-rbf", content)
	if err != nil {
		return SessionResult{}, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	resp, err := c.launchHTTP.Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return SessionResult{}, err
	}
	result, err := decodeSessionBody(resp.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("development RBF response: %w", err)
	}
	if result.ErrorCode != "" {
		return result, nil
	}
	if result.State != "active" {
		return result, fmt.Errorf("development RBF response: expected active session, got %q", result.State)
	}
	return result, nil
}

// Stop posts an empty body to POST /api/v1/session/stop.
func (c *Client) Stop(ctx context.Context) (SessionResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/session/stop", http.NoBody)
	if err != nil {
		return SessionResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.stopHTTP.Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return SessionResult{}, err
	}
	result, err := decodeSessionBody(resp.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("stop response: %w", err)
	}
	if result.ErrorCode != "" {
		return result, nil
	}
	if result.State != "idle" {
		return result, fmt.Errorf("stop response: expected idle session, got %q", result.State)
	}
	return result, nil
}

// Health loads GET /api/v1/health. A transport failure means the host process
// is unreachable; HTTP 200 with target.reachable=false is a kit-down signal.
func (c *Client) Health(ctx context.Context) (HealthResult, error) {
	var wire struct {
		Ready  bool `json:"ready"`
		Target struct {
			Reachable  bool             `json:"reachable"`
			Ready      bool             `json:"ready"`
			Connection TargetConnection `json:"connection"`
		} `json:"target"`
	}
	if err := c.getJSON(ctx, "/api/v1/health", &wire); err != nil {
		return HealthResult{}, err
	}
	return HealthResult{
		Ready:           wire.Ready,
		TargetReachable: wire.Target.Reachable,
		TargetReady:     wire.Target.Ready,
		Connection:      wire.Target.Connection,
	}, nil
}

// Status loads GET /api/v1/status. HTTP 503 TARGET_UNAVAILABLE is kit-down,
// not a decode error.
func (c *Client) Status(ctx context.Context) (TargetStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/status", http.NoBody)
	if err != nil {
		return TargetStatus{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TargetStatus{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return TargetStatus{}, err
	}
	result := TargetStatus{HTTPStatus: resp.StatusCode}
	var wire struct {
		State  string  `json:"state"`
		GameID *string `json:"game_id"`
		System *string `json:"system"`
		Core   *string `json:"core"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Connection TargetConnection `json:"connection"`
	}
	_ = json.Unmarshal(body, &wire)
	result.State = strings.TrimSpace(wire.State)
	result.Connection = wire.Connection
	if wire.GameID != nil {
		result.GameID = strings.TrimSpace(*wire.GameID)
	}
	if wire.System != nil {
		result.System = strings.TrimSpace(*wire.System)
	}
	if wire.Core != nil {
		result.Core = strings.TrimSpace(*wire.Core)
	}
	if wire.Error != nil {
		result.ErrorCode = strings.TrimSpace(wire.Error.Code)
		result.ErrorMessage = strings.TrimSpace(wire.Error.Message)
	}
	if resp.StatusCode == http.StatusServiceUnavailable && result.ErrorCode == "TARGET_UNAVAILABLE" {
		result.Unavailable = true
		return result, nil
	}
	if resp.StatusCode != http.StatusOK {
		if result.ErrorCode != "" {
			return result, fmt.Errorf("host API %d %s: %s", resp.StatusCode, result.ErrorCode, result.ErrorMessage)
		}
		return result, apiStatusError(resp.StatusCode, body)
	}
	return result, nil
}

// SessionInput loads GET /api/v1/session/input.
func (c *Client) SessionInput(ctx context.Context) (SessionInput, error) {
	var result SessionInput
	if err := c.getJSON(ctx, "/api/v1/session/input", &result); err != nil {
		return SessionInput{}, err
	}
	return result, nil
}

// AttachInput posts an empty body to POST /api/v1/session/input/attach.
func (c *Client) AttachInput(ctx context.Context) (SessionResult, error) {
	return c.postSessionInput(ctx, "/api/v1/session/input/attach")
}

// DetachInput posts an empty body to POST /api/v1/session/input/detach.
func (c *Client) DetachInput(ctx context.Context) (SessionResult, error) {
	return c.postSessionInput(ctx, "/api/v1/session/input/detach")
}

func (c *Client) postSessionInput(ctx context.Context, path string) (SessionResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, http.NoBody)
	if err != nil {
		return SessionResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return SessionResult{}, err
	}
	result, err := decodeSessionBody(resp.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("input response: %w", err)
	}
	if result.ErrorCode != "" {
		return result, nil
	}
	if resp.StatusCode != http.StatusOK {
		return result, apiStatusError(resp.StatusCode, body)
	}
	if !validSessionState(result.State) {
		return result, fmt.Errorf("input response: invalid session state %q", result.State)
	}
	return result, nil
}

func isHostTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func validSessionState(state string) bool {
	switch state {
	case "idle", "launching", "active", "stopping", "failed":
		return true
	default:
		return false
	}
}

func decodeSessionBody(status int, body []byte) (SessionResult, error) {
	result := SessionResult{HTTPStatus: status}
	if len(bytes.TrimSpace(body)) == 0 {
		return result, fmt.Errorf("empty body")
	}
	var wire struct {
		State                   string           `json:"state"`
		GameID                  *string          `json:"game_id"`
		System                  *string          `json:"system"`
		Execution               string           `json:"execution"`
		Media                   string           `json:"media"`
		Progress                *SessionProgress `json:"progress"`
		Input                   *SessionInput    `json:"input"`
		Development             *bool            `json:"development"`
		DevelopmentActive       *bool            `json:"development_active"`
		DevelopmentSessionState string           `json:"development_session_state"`
		Error                   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return result, err
	}
	result.State = wire.State
	if wire.GameID != nil {
		result.GameID = strings.TrimSpace(*wire.GameID)
	}
	if wire.System != nil {
		result.System = strings.TrimSpace(*wire.System)
	}
	result.Execution = strings.TrimSpace(wire.Execution)
	result.Media = strings.TrimSpace(wire.Media)
	result.Progress = wire.Progress
	result.Input = wire.Input
	result.DevelopmentSessionState = strings.TrimSpace(wire.DevelopmentSessionState)
	switch {
	case wire.DevelopmentActive != nil:
		result.Development = *wire.DevelopmentActive
	case wire.Development != nil:
		result.Development = *wire.Development
	default:
		result.Development = result.Execution == "fpga_development"
	}
	if result.Development && result.Execution == "" {
		switch result.State {
		case "active", "launching":
			result.Execution = "fpga_development"
		}
	}
	if wire.Error != nil {
		result.ErrorCode = wire.Error.Code
		result.ErrorMessage = wire.Error.Message
		return result, nil
	}
	if !validSessionState(wire.State) {
		return result, fmt.Errorf("invalid session state %q", wire.State)
	}
	return result, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, dest)
}

func (c *Client) mutateJSON(ctx context.Context, method, path string, dest any) error {
	return c.doJSON(ctx, method, path, nil, dest)
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, dest any) error {
	var bodyReader io.Reader = http.NoBody
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return apiStatusError(resp.StatusCode, body)
	}
	if dest == nil {
		return nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func apiStatusError(status int, body []byte) error {
	var wire struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error.Code != "" {
		return fmt.Errorf("host API %d %s: %s", status, wire.Error.Code, wire.Error.Message)
	}
	return fmt.Errorf("host API status %d", status)
}

func normalizeHandle(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) != artworkHandleLen {
		return ""
	}
	for _, r := range value {
		if r < '0' || r > '9' && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return value
}

const maxScreenshotHandles = 8

// screenshotHandles normalizes presentation screenshot_ids, drops invalid
// entries, de-duplicates, and caps at the web limit of 8.
func screenshotHandles(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, maxScreenshotHandles)
	seen := map[string]bool{}
	for _, id := range ids {
		handle := normalizeHandle(id)
		if handle == "" || seen[handle] {
			continue
		}
		seen[handle] = true
		out = append(out, handle)
		if len(out) >= maxScreenshotHandles {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
