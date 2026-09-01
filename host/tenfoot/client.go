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
	artworkHandleLen   = 64
	defaultHTTPTimeout = 15 * time.Second
	launchHTTPTimeout  = 60 * time.Second
)

// Game is one catalog row from GET /api/v1/games.
type Game struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	System     string `json:"system"`
	Cover      string `json:"cover,omitempty"`
	Genre      string `json:"genre,omitempty"`
	Year       string `json:"year,omitempty"`
	State      string `json:"state"`
	RootOnline bool   `json:"root_online"`
	Launchable bool   `json:"launchable"`
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
}

// LaunchResult is the host response to POST /api/v1/session/launch.
type LaunchResult struct {
	HTTPStatus   int
	State        string
	GameID       string
	ErrorCode    string
	ErrorMessage string
}

// Client calls the FogCast public host API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	launchHTTP *http.Client
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
	return &Client{baseURL: baseURL, httpClient: httpClient, launchHTTP: &launchHTTP}
}

// ListGames fetches one catalog page.
func (c *Client) ListGames(ctx context.Context, cursor string, limit int) ([]Game, string, error) {
	if limit <= 0 {
		limit = defaultPageLimit
	}
	values := url.Values{}
	values.Set("grouped", "1")
	values.Set("limit", strconv.Itoa(limit))
	if strings.TrimSpace(cursor) != "" {
		values.Set("cursor", cursor)
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
	return page.Games, page.NextCursor, nil
}

// FetchLibrary walks catalog pages until maxGames or the cursor ends.
func (c *Client) FetchLibrary(ctx context.Context, pageLimit, maxGames int) ([]Game, error) {
	if pageLimit <= 0 {
		pageLimit = defaultPageLimit
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
		page, next, err := c.ListGames(ctx, cursor, pageLimit)
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
	result := LaunchResult{HTTPStatus: resp.StatusCode}
	var wire struct {
		State  string `json:"state"`
		GameID string `json:"game_id"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &wire); err != nil {
			return result, fmt.Errorf("launch response: %w", err)
		}
	}
	result.State = wire.State
	result.GameID = wire.GameID
	if wire.Error != nil {
		result.ErrorCode = wire.Error.Code
		result.ErrorMessage = wire.Error.Message
	}
	return result, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
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
