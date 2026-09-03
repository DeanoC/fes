// Package tenfoot is the native SDL3 10-foot FogCast launcher.
//
// It talks to the public host API over HTTP. Catalog, content transfer, and
// the MiSTer command path stay in the existing host and target processes.
package tenfoot

import (
	"bytes"
	"context"
	"encoding/json"
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
	State       string   `json:"state"`
	RootOnline  bool     `json:"root_online"`
	Launchable  bool     `json:"launchable"`
	Favorite    bool     `json:"favorite,omitempty"`
	Collections []string `json:"collections,omitempty"`
	Variants    []Game   `json:"variants,omitempty"`
}

// Presentation is GET /api/v1/presentation/games/{id}.
type Presentation struct {
	GameID       string `json:"game_id"`
	State        string `json:"state"`
	Presentation *struct {
		CoverArtworkID string `json:"cover_artwork_id"`
		Summary        string `json:"summary"`
		Year           string `json:"year"`
		Genre          string `json:"genre"`
		Studio         string `json:"studio"`
	} `json:"presentation"`
	Attribution *PresentationAttribution `json:"attribution,omitempty"`
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
	handles := item.stillHandles()
	if len(handles) == 0 {
		return ""
	}
	return handles[0]
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
	Cursor     string
	Limit      int
	Platform   string
	Sort       string
	Q          string
	Collection string
}

// SessionProgress is the optional progress object on sessionResult.
type SessionProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// SessionInput is the read-only remote-input object on sessionResult.
type SessionInput struct {
	State string `json:"state"`
	Ready bool   `json:"ready"`
}

// SessionResult is sessionResult from GET /api/v1/session, POST launch, and POST stop.
type SessionResult struct {
	HTTPStatus   int
	State        string
	GameID       string
	System       string
	Execution    string
	Media        string
	Progress     *SessionProgress
	Input        *SessionInput
	ErrorCode    string
	ErrorMessage string
}

// LaunchResult is the host response to POST /api/v1/session/launch.
type LaunchResult = SessionResult

// Client calls the FogCast public host API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	launchHTTP *http.Client
	stopHTTP   *http.Client
	videoHTTP  *http.Client
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
	return &Client{baseURL: baseURL, httpClient: httpClient, launchHTTP: &launchHTTP, stopHTTP: &stopHTTP, videoHTTP: &videoHTTP}
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

// LibraryTarget is one read-only target row from GET /api/v1/library/settings.
type LibraryTarget struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	Enabled         bool   `json:"enabled"`
	AgentConfigured bool   `json:"agent_configured"`
}

// LibraryRoot is one library path row. Tenfoot does not edit these.
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
// Nil fields are omitted.
type LibrarySettingsPatch struct {
	AttractIdleSeconds *int      `json:"attract_idle_seconds,omitempty"`
	PreferredRegions   *[]string `json:"preferred_regions,omitempty"`
	SelectedTarget     *string   `json:"selected_target,omitempty"`
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
		State     string           `json:"state"`
		GameID    *string          `json:"game_id"`
		System    *string          `json:"system"`
		Execution string           `json:"execution"`
		Media     string           `json:"media"`
		Progress  *SessionProgress `json:"progress"`
		Input     *SessionInput    `json:"input"`
		Error     *struct {
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
