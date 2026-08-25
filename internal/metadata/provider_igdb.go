package metadata

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/systems"
	"github.com/DeanoC/FogCast-POC/protocol"
)

const (
	igdbTokenURL = "https://id.twitch.tv/oauth2/token"
	igdbAPIHost  = "api.igdb.com"
	igdbAPIBase  = "https://api.igdb.com/v4"
)

type IGDBConfig struct {
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
	Now          func() time.Time
}

type IGDBProvider struct {
	client       *http.Client
	clientID     string
	clientSecret string
	now          func() time.Time
	limiter      *requestLimiter

	tokenMu    sync.Mutex
	token      string
	tokenUntil time.Time

	platformMu         sync.Mutex
	platforms          map[string]resolvedPlatform
	platformsExpiresAt time.Time
	platformFlight     *platformResolutionFlight
	cache              *Cache
	closed             bool
	closeMu            sync.RWMutex
	rootCtx            context.Context
	rootCancel         context.CancelFunc
}

type resolvedPlatform struct {
	ID       string
	Checksum string
	Updated  int64
}

type platformResolutionFlight struct {
	done      chan struct{}
	platforms map[string]resolvedPlatform
	expiresAt time.Time
	err       error
}

func NewIGDBProvider(config IGDBConfig) (*IGDBProvider, error) {
	if strings.TrimSpace(config.ClientID) == "" || strings.TrimSpace(config.ClientSecret) == "" {
		return nil, newOpError(ErrUnconfigured, nil)
	}
	client := config.HTTPClient
	if client == nil {
		client = NewHardenedHTTPClient()
	} else {
		copy := *client
		copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		copy.Jar = nil
		copy.Timeout = 0
		client = &copy
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	rootCtx, rootCancel := context.WithCancel(context.Background())
	return &IGDBProvider{
		client:       client,
		clientID:     config.ClientID,
		clientSecret: config.ClientSecret,
		now:          now,
		limiter:      newRequestLimiter(),
		rootCtx:      rootCtx,
		rootCancel:   rootCancel,
	}, nil
}

func (p *IGDBProvider) Name() ProviderName { return ProviderIGDB }

func (p *IGDBProvider) AttachCache(cache *Cache) {
	p.platformMu.Lock()
	p.cache = cache
	p.platforms = nil
	p.platformsExpiresAt = time.Time{}
	p.platformMu.Unlock()
}

func (p *IGDBProvider) ResolvePlatform(ctx context.Context, system protocol.System) (string, error) {
	if err := p.isClosed(); err != nil {
		return "", err
	}
	spec, ok := platformSlugForSystem(system)
	if !ok {
		return "", newOpError(ErrPolicyBlocked, nil)
	}
	platforms, err := p.resolvePlatforms(ctx)
	if err != nil {
		return "", err
	}
	platform, ok := platforms[spec.slug]
	if !ok {
		return "", newOpError(ErrPolicyBlocked, nil)
	}
	return platform.ID, nil
}

func (p *IGDBProvider) ResetMetadataState() {
	p.platformMu.Lock()
	p.platforms = nil
	p.platformsExpiresAt = time.Time{}
	p.platformMu.Unlock()
}

func (p *IGDBProvider) Close() error {
	p.closeMu.Lock()
	p.closed = true
	if p.rootCancel != nil {
		p.rootCancel()
	}
	p.closeMu.Unlock()
	if transport, ok := p.client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
	return nil
}

func (p *IGDBProvider) Lookup(parent context.Context, query ProviderQuery) (ProviderResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, lookupTimeout)
	stopRoot := func() bool { return false }
	if p.rootCtx != nil {
		stopRoot = context.AfterFunc(p.rootCtx, cancel)
	}
	defer func() {
		_ = stopRoot()
		cancel()
	}()
	if err := p.isClosed(); err != nil {
		return ProviderResult{}, err
	}
	if strings.TrimSpace(query.NormalizedTitle) == "" {
		return ProviderResult{}, newOpError(ErrPolicyBlocked, nil)
	}
	spec, ok := platformSlugForSystem(query.System)
	if !ok {
		return ProviderResult{}, newOpError(ErrPolicyBlocked, nil)
	}
	platforms, err := p.resolvePlatforms(ctx)
	if err != nil {
		return ProviderResult{}, err
	}
	platform, ok := platforms[spec.slug]
	if !ok {
		return ProviderResult{}, newOpError(ErrPolicyBlocked, nil)
	}
	body := buildGamesQuery(query.NormalizedTitle, platform.ID, query.Region)
	payload, err := p.apiJSON(ctx, "/games", body)
	if err != nil {
		return ProviderResult{}, err
	}
	var games []igdbGame
	if err := json.Unmarshal(payload, &games); err != nil || games == nil {
		return ProviderResult{}, newOpError(ErrInvalidResponse, err)
	}
	if len(games) > 20 {
		return ProviderResult{}, newOpError(ErrInvalidResponse, nil)
	}
	candidates := make([]Candidate, 0, len(games))
	for _, game := range games {
		candidate, err := candidateFromWire(game)
		if err != nil {
			return ProviderResult{}, err
		}
		candidates = append(candidates, candidate)
	}
	return ProviderResult{PlatformID: platform.ID, Candidates: candidates}, nil
}

func (p *IGDBProvider) isClosed() error {
	p.closeMu.RLock()
	defer p.closeMu.RUnlock()
	if p.closed {
		return newOpError(ErrCanceled, context.Canceled)
	}
	return nil
}

func (p *IGDBProvider) resolvePlatforms(ctx context.Context) (map[string]resolvedPlatform, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := p.isClosed(); err != nil {
		return nil, err
	}
	p.platformMu.Lock()
	if len(p.platforms) == len(igdbPlatformSpecs()) && p.now().Before(p.platformsExpiresAt) {
		platforms := clonePlatforms(p.platforms)
		p.platformMu.Unlock()
		return platforms, nil
	}
	if flight := p.platformFlight; flight != nil {
		done := flight.done
		p.platformMu.Unlock()
		select {
		case <-done:
			if err := p.isClosed(); err != nil {
				return nil, err
			}
			return clonePlatforms(flight.platforms), flight.err
		case <-ctx.Done():
			return nil, mapContextError(ctx.Err())
		}
	}
	flight := &platformResolutionFlight{done: make(chan struct{})}
	p.platformFlight = flight
	p.platformMu.Unlock()
	go p.runPlatformFlight(flight)
	return p.waitPlatformFlight(ctx, flight)
}

func (p *IGDBProvider) waitPlatformFlight(ctx context.Context, flight *platformResolutionFlight) (map[string]resolvedPlatform, error) {
	select {
	case <-flight.done:
		if err := p.isClosed(); err != nil {
			return nil, err
		}
		return clonePlatforms(flight.platforms), flight.err
	case <-ctx.Done():
		return nil, mapContextError(ctx.Err())
	}
}

func (p *IGDBProvider) runPlatformFlight(flight *platformResolutionFlight) {
	flightContext := p.rootCtx
	if flightContext == nil {
		flightContext = context.Background()
	}
	platforms, expiresAt, err := p.resolvePlatformsLeader(flightContext)
	if closeErr := p.isClosed(); closeErr != nil {
		platforms, expiresAt, err = nil, time.Time{}, closeErr
	}
	p.platformMu.Lock()
	if err == nil {
		p.platforms = clonePlatforms(platforms)
		p.platformsExpiresAt = expiresAt
	}
	flight.platforms = clonePlatforms(platforms)
	flight.expiresAt = expiresAt
	flight.err = err
	p.platformFlight = nil
	close(flight.done)
	p.platformMu.Unlock()
}

func (p *IGDBProvider) resolvePlatformsLeader(ctx context.Context) (map[string]resolvedPlatform, time.Time, error) {
	p.platformMu.Lock()
	cache := p.cache
	p.platformMu.Unlock()
	var (
		cached        map[string]resolvedPlatform
		cachedOK      bool
		cachedExpires time.Time
	)
	if cache != nil {
		var err error
		cached, _, cachedExpires, cachedOK, err = cache.PlatformMapping()
		if err != nil {
			return nil, time.Time{}, err
		}
	}
	if cachedOK && validResolvedPlatforms(cached) && p.now().Before(cachedExpires) {
		return clonePlatforms(cached), cachedExpires, nil
	}
	expected := igdbPlatformSpecs()
	slugs := make([]string, 0, len(expected))
	for slug := range expected {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	quoted := make([]string, len(slugs))
	for index, slug := range slugs {
		quoted[index] = strconv.Quote(slug)
	}
	body := `fields id,name,slug,checksum,updated_at; where slug = (` + strings.Join(quoted, ",") + `); limit ` + strconv.Itoa(len(slugs)) + `;`
	payload, err := p.apiJSON(ctx, "/platforms", body)
	if err != nil {
		return nil, time.Time{}, err
	}
	var records []igdbPlatform
	if err := json.Unmarshal(payload, &records); err != nil || records == nil || len(records) != len(expected) {
		return nil, time.Time{}, newOpError(ErrInvalidResponse, err)
	}
	resolved := make(map[string]resolvedPlatform, len(expected))
	seenIDs := make(map[string]struct{}, len(expected))
	for _, record := range records {
		name, ok := expected[record.Slug]
		if !ok || record.Name != name || record.ID <= 0 || strings.TrimSpace(record.Checksum) == "" {
			return nil, time.Time{}, newOpError(ErrPolicyBlocked, nil)
		}
		id := strconv.FormatInt(record.ID, 10)
		if _, exists := seenIDs[id]; exists {
			return nil, time.Time{}, newOpError(ErrPolicyBlocked, nil)
		}
		seenIDs[id] = struct{}{}
		resolved[record.Slug] = resolvedPlatform{ID: id, Checksum: record.Checksum, Updated: record.UpdatedAt}
	}
	if !hasExactPlatformSlugs(resolved, expected) {
		return nil, time.Time{}, newOpError(ErrPolicyBlocked, nil)
	}
	if cache != nil {
		changed := cachedOK && !samePlatformIdentity(cached, resolved)
		if changed {
			if err := cache.PurgeProvider(ProviderIGDB); err != nil {
				return nil, time.Time{}, err
			}
		}
		expiresAt := p.now().Add(positiveMetadataTTL)
		if err := cache.SavePlatformMapping(resolved, p.now(), expiresAt); err != nil {
			return nil, time.Time{}, err
		}
		return clonePlatforms(resolved), expiresAt, nil
	}
	return clonePlatforms(resolved), p.now().Add(positiveMetadataTTL), nil
}

func samePlatformIdentity(left, right map[string]resolvedPlatform) bool {
	if len(left) != len(right) {
		return false
	}
	for slug, current := range right {
		previous, ok := left[slug]
		if !ok || previous.ID != current.ID || previous.Checksum != current.Checksum {
			return false
		}
	}
	return true
}

func validResolvedPlatforms(platforms map[string]resolvedPlatform) bool {
	expected := igdbPlatformSpecs()
	if !hasExactPlatformSlugs(platforms, expected) {
		return false
	}
	seenIDs := make(map[string]struct{}, len(platforms))
	for slug, platform := range platforms {
		if _, ok := expected[slug]; !ok || strings.TrimSpace(platform.Checksum) == "" || platform.Updated < 0 {
			return false
		}
		id, err := strconv.ParseInt(platform.ID, 10, 64)
		if err != nil || id <= 0 {
			return false
		}
		if _, exists := seenIDs[platform.ID]; exists {
			return false
		}
		seenIDs[platform.ID] = struct{}{}
	}
	return true
}

func hasExactPlatformSlugs(platforms map[string]resolvedPlatform, expected map[string]string) bool {
	if len(platforms) != len(expected) {
		return false
	}
	for slug := range expected {
		if _, ok := platforms[slug]; !ok {
			return false
		}
	}
	return true
}

func clonePlatforms(platforms map[string]resolvedPlatform) map[string]resolvedPlatform {
	copy := make(map[string]resolvedPlatform, len(platforms))
	for slug, platform := range platforms {
		copy[slug] = platform
	}
	return copy
}

type platformSpec struct{ slug string }

func platformSlugForSystem(system protocol.System) (platformSpec, bool) {
	row, ok := systems.Lookup(system)
	if !ok {
		return platformSpec{}, false
	}
	cover, ok := row.CoverSlugs[systems.CoverProviderIGDB]
	return platformSpec{slug: cover.Slug}, ok && cover.Slug != ""
}

func igdbPlatformSpecs() map[string]string {
	expected := make(map[string]string)
	for _, row := range systems.Rows() {
		if cover, ok := row.CoverSlugs[systems.CoverProviderIGDB]; ok && cover.Slug != "" {
			expected[cover.Slug] = cover.Name
		}
	}
	return expected
}

func buildGamesQuery(title, platformID, region string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(title)
	query := `fields id,name,alternative_names.name,platforms,summary,first_release_date,genres.name,involved_companies.company.name,involved_companies.developer,involved_companies.publisher,cover.image_id,artworks.image_id,checksum,updated_at; where platforms = ` + platformID + `; search "` + escaped + `"; limit 20;`
	if strings.TrimSpace(region) != "" {
		// Region is an internal cache/query discriminator. IGDB's game search
		// endpoint does not accept a free-form region predicate, so it is never
		// copied onto the provider wire request.
		_ = region
	}
	return query
}

func (p *IGDBProvider) apiJSON(ctx context.Context, path, body string) ([]byte, error) {
	token, cached, err := p.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	payload, status, opErr := p.doJSON(ctx, http.MethodPost, igdbAPIBase+path, []byte(body), map[string]string{
		"Client-ID":     p.clientID,
		"Authorization": "Bearer " + token,
		"Content-Type":  "text/plain; charset=utf-8",
	}, maxProviderBody)
	if status == http.StatusUnauthorized && cached {
		p.invalidateToken(token)
		fresh, _, refreshErr := p.accessToken(ctx)
		if refreshErr != nil {
			return nil, refreshErr
		}
		payload, _, opErr = p.doJSON(ctx, http.MethodPost, igdbAPIBase+path, []byte(body), map[string]string{
			"Client-ID":     p.clientID,
			"Authorization": "Bearer " + fresh,
			"Content-Type":  "text/plain; charset=utf-8",
		}, maxProviderBody)
	}
	if opErr != nil {
		return nil, opErr
	}
	return payload, nil
}

func (p *IGDBProvider) accessToken(ctx context.Context) (string, bool, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if p.token != "" && p.now().Before(p.tokenUntil.Add(-30*time.Second)) {
		return p.token, true, nil
	}
	values := url.Values{}
	values.Set("client_id", p.clientID)
	values.Set("client_secret", p.clientSecret)
	values.Set("grant_type", "client_credentials")
	payload, _, err := p.doJSON(ctx, http.MethodPost, igdbTokenURL, []byte(values.Encode()), map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/x-www-form-urlencoded",
	}, maxTokenBody)
	if err != nil {
		return "", false, err
	}
	var token igdbToken
	if json.Unmarshal(payload, &token) != nil || strings.TrimSpace(token.AccessToken) == "" || token.ExpiresIn <= 0 {
		return "", false, newOpError(ErrInvalidResponse, nil)
	}
	p.token = token.AccessToken
	p.tokenUntil = p.now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return p.token, false, nil
}

func (p *IGDBProvider) invalidateToken(token string) {
	p.tokenMu.Lock()
	if p.token == token {
		p.token = ""
		p.tokenUntil = time.Time{}
	}
	p.tokenMu.Unlock()
}

func (p *IGDBProvider) doJSON(ctx context.Context, method, rawURL string, body []byte, headers map[string]string, maxBytes int64) ([]byte, int, *OpError) {
	var last *OpError
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.limiter.acquire(ctx); err != nil {
			return nil, 0, mapContextError(err)
		}
		requestCtx, cancel := context.WithTimeout(ctx, providerRequestTimeout)
		if strings.HasSuffix(rawURL, "/oauth2/token") || strings.Contains(rawURL, "/oauth2/token?") {
			cancel()
			requestCtx, cancel = context.WithTimeout(ctx, tokenRequestTimeout)
		}
		request, err := http.NewRequestWithContext(requestCtx, method, rawURL, bytes.NewReader(body))
		if err != nil {
			cancel()
			p.limiter.release()
			return nil, 0, newOpError(ErrPolicyBlocked, nil)
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, requestErr := p.client.Do(request)
		if requestErr != nil {
			requestTimedOut := errors.Is(requestCtx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(requestErr, context.DeadlineExceeded)
			requestCanceled := errors.Is(requestCtx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.Canceled) || errors.Is(requestErr, context.Canceled)
			cancel()
			p.limiter.release()
			if requestTimedOut {
				return nil, 0, newOpError(ErrDeadline, context.DeadlineExceeded)
			}
			if requestCanceled || ctx.Err() != nil {
				return nil, 0, mapContextError(ctx.Err())
			}
			last = newOpError(ErrUpstreamUnavailable, nil)
			if !isTransientConnectionError(requestErr) || attempt != 0 {
				return nil, 0, last
			}
			continue
		}
		if response.Body == nil {
			cancel()
			p.limiter.release()
			return nil, response.StatusCode, newOpError(ErrInvalidResponse, nil)
		}
		payload, readErr := readLimited(response.Body, maxBytes)
		responseTimedOut := errors.Is(requestCtx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)
		responseCanceled := errors.Is(requestCtx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.Canceled)
		response.Body.Close()
		status := response.StatusCode
		cancel()
		p.limiter.release()
		if readErr != nil {
			if responseTimedOut {
				return nil, status, newOpError(ErrDeadline, context.DeadlineExceeded)
			}
			if responseCanceled {
				return nil, status, newOpError(ErrCanceled, context.Canceled)
			}
			return nil, status, newOpError(ErrInvalidResponse, readErr)
		}
		if status < 200 || status >= 300 {
			opErr := statusError(status, response.Header)
			if (status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout) && attempt == 0 {
				last = opErr
				continue
			}
			return nil, status, opErr
		}
		if !validJSONContentType(response.Header.Get("Content-Type")) {
			return nil, status, newOpError(ErrInvalidResponse, nil)
		}
		return payload, status, nil
	}
	return nil, 0, last
}

func isTransientConnectionError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && !netErr.Timeout()
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("invalid response limit")
	}
	limited := io.LimitReader(reader, limit+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, errors.New("response body exceeds policy limit")
	}
	return payload, nil
}

func validJSONContentType(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "application/json" || strings.HasPrefix(value, "application/json;")
}

func statusError(status int, headers http.Header) *OpError {
	code := ErrUpstreamUnavailable
	switch status {
	case http.StatusBadRequest:
		code = ErrInvalidResponse
	case http.StatusUnauthorized:
		code = ErrUnauthorized
	case http.StatusForbidden:
		code = ErrPolicyBlocked
	case http.StatusNotFound:
		code = ErrProviderRemoved
	case http.StatusTooManyRequests:
		code = ErrRateLimited
	}
	err := newOpError(code, nil)
	if status == http.StatusTooManyRequests {
		err.RetryAfter = retryAfter(headers.Get("Retry-After"))
	}
	return err
}

func retryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	seconds, err := strconv.Atoi(value)
	if err != nil {
		if retryAt, parseErr := http.ParseTime(value); parseErr == nil {
			duration := time.Until(retryAt)
			if duration < time.Second {
				duration = time.Second
			}
			if duration > 15*time.Minute {
				return 15 * time.Minute
			}
			return duration
		}
		return 30 * time.Second
	}
	if seconds < 1 {
		return 30 * time.Second
	}
	duration := time.Duration(seconds) * time.Second
	if duration > 15*time.Minute {
		return 15 * time.Minute
	}
	return duration
}

type igdbToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

type igdbPlatform struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Checksum  string `json:"checksum"`
	UpdatedAt int64  `json:"updated_at"`
}

type igdbName struct {
	Name string `json:"name"`
}

type igdbCompany struct {
	Developer bool     `json:"developer"`
	Publisher bool     `json:"publisher"`
	Company   igdbName `json:"company"`
}

type igdbImage struct {
	ImageID string `json:"image_id"`
}

type igdbGame struct {
	ID               int64         `json:"id"`
	Name             string        `json:"name"`
	AlternativeNames []igdbName    `json:"alternative_names"`
	Platforms        []int64       `json:"platforms"`
	Summary          string        `json:"summary"`
	FirstReleaseDate int64         `json:"first_release_date"`
	Genres           []igdbName    `json:"genres"`
	Companies        []igdbCompany `json:"involved_companies"`
	Cover            *igdbImage    `json:"cover"`
	Artworks         []igdbImage   `json:"artworks"`
	Checksum         string        `json:"checksum"`
	UpdatedAt        int64         `json:"updated_at"`
}

func candidateFromWire(game igdbGame) (Candidate, error) {
	if game.ID <= 0 || strings.TrimSpace(game.Name) == "" {
		return Candidate{}, newOpError(ErrInvalidResponse, nil)
	}
	candidate := Candidate{ProviderID: strconv.FormatInt(game.ID, 10), Name: game.Name, Summary: game.Summary, Checksum: game.Checksum}
	for _, alternate := range game.AlternativeNames {
		if strings.TrimSpace(alternate.Name) != "" {
			candidate.AlternativeNames = append(candidate.AlternativeNames, alternate.Name)
		}
	}
	for _, platform := range game.Platforms {
		if platform <= 0 {
			return Candidate{}, newOpError(ErrInvalidResponse, nil)
		}
		candidate.PlatformIDs = append(candidate.PlatformIDs, strconv.FormatInt(platform, 10))
	}
	sort.Strings(candidate.PlatformIDs)
	for _, genre := range game.Genres {
		if genre.Name != "" {
			candidate.Genres = append(candidate.Genres, genre.Name)
		}
	}
	for _, company := range game.Companies {
		if (company.Developer || company.Publisher) && company.Company.Name != "" {
			candidate.Studios = append(candidate.Studios, company.Company.Name)
		}
	}
	if game.FirstReleaseDate > 0 {
		candidate.FirstReleaseYear = time.Unix(game.FirstReleaseDate, 0).UTC().Year()
	}
	if len(game.Artworks) > 0 && game.Artworks[0].ImageID != "" {
		candidate.Artwork = append(candidate.Artwork, ArtworkRef{Role: ArtworkBackdrop, ID: game.Artworks[0].ImageID})
	}
	if game.Cover != nil && game.Cover.ImageID != "" {
		candidate.Artwork = append(candidate.Artwork, ArtworkRef{Role: ArtworkCover, ID: game.Cover.ImageID})
	}
	if game.UpdatedAt > 0 {
		candidate.UpdatedAt = time.Unix(game.UpdatedAt, 0).UTC()
	}
	return candidate, nil
}

func (p *IGDBProvider) String() string { return fmt.Sprintf("%s", p.Name()) }
