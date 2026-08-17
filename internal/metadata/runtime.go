package metadata

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
)

type ConfigState string

const (
	StateUnconfigured ConfigState = "unconfigured"
	StateDisabled     ConfigState = "disabled"
	StateReady        ConfigState = "ready"

	platformPolicyKey = "platform-map-v1"
)

type RuntimeConfig struct {
	Root         string
	Configured   bool
	Enabled      bool
	ProviderName ProviderName
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
	Provider     Provider
	Now          func() time.Time
	Archive      string
}

func StateForConfig(config RuntimeConfig) ConfigState {
	if !config.Configured {
		return StateUnconfigured
	}
	if !config.Enabled {
		return StateDisabled
	}
	return StateReady
}

func Open(ctx context.Context, config RuntimeConfig) (Runtime, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !config.Configured || !config.Enabled {
		if err := purgeMetadataRoot(config.Root); err != nil {
			return nil, newOpError(ErrStorage, nil)
		}
		return nil, nil
	}
	if config.ProviderName == ProviderLaunchBox {
		if config.Provider != nil || strings.TrimSpace(config.ClientID) != "" || strings.TrimSpace(config.ClientSecret) != "" {
			return nil, newOpError(ErrPolicyBlocked, nil)
		}
		if archive := strings.TrimSpace(config.Archive); archive != "" {
			return OpenLaunchBoxArchiveWithCache(archive, config.HTTPClient, config.Root)
		}
		if config.HTTPClient != nil {
			return nil, newOpError(ErrPolicyBlocked, nil)
		}
		return openLaunchBoxRuntime(config)
	}
	if config.ProviderName != ProviderIGDB || config.Provider == nil && (strings.TrimSpace(config.ClientID) == "" || strings.TrimSpace(config.ClientSecret) == "") {
		return nil, newOpError(ErrUnconfigured, nil)
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	provider := config.Provider
	if provider == nil {
		var err error
		provider, err = NewIGDBProvider(IGDBConfig{ClientID: config.ClientID, ClientSecret: config.ClientSecret, HTTPClient: config.HTTPClient, Now: now})
		if err != nil {
			return nil, err
		}
	}
	if provider.Name() != ProviderIGDB {
		return nil, newOpError(ErrPolicyBlocked, nil)
	}
	cache, err := OpenCache(ctx, CacheConfig{Root: config.Root, CredentialScope: config.ClientID, Now: now})
	if err != nil {
		if closer, ok := provider.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		return nil, err
	}
	if attach, ok := provider.(interface{ AttachCache(*Cache) }); ok {
		attach.AttachCache(cache)
	}
	artwork, err := newArtworkFetcherFromCache(cache, ArtworkFetcherConfig{Root: config.Root, HTTPClient: config.HTTPClient})
	if err != nil {
		_ = cache.Close()
		if closer, ok := provider.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		return nil, err
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	return &metadataRuntime{
		rootCtx: rootCtx, cancel: cancel, provider: provider, cache: cache, artwork: artwork,
		now: now, platformIDs: make(map[protocol.System]platformEntry), inFlight: make(map[string]*lookupCall), keyPolicy: make(map[string]policyEntry), artworkUntil: make(map[string]time.Time), counters: make(map[ObservationKind]uint64),
	}, nil
}

type metadataRuntime struct {
	rootCtx  context.Context
	cancel   context.CancelFunc
	provider Provider
	cache    *Cache
	artwork  *ArtworkFetcher
	now      func() time.Time

	mu                   sync.Mutex
	publicationMu        sync.Mutex
	closed               bool
	platformIDs          map[protocol.System]platformEntry
	inFlight             map[string]*lookupCall
	keyPolicy            map[string]policyEntry
	artworkUntil         map[string]time.Time
	providerRateUntil    time.Time
	providerBackoffUntil time.Time
	providerBackoff      time.Duration
	latched              ErrorCode
	providerEpoch        uint64
	counters             map[ObservationKind]uint64
	leaders              sync.WaitGroup
	closeOnce            sync.Once
	closeErr             error
	reaper               *runtimeReaper
}

type runtimeReaper struct {
	done chan struct{}
	err  error
}

type platformEntry struct {
	ID        string
	ExpiresAt time.Time
}

type policyEntry struct {
	Until time.Time
	Code  ErrorCode
}

type lookupCall struct {
	done       chan struct{}
	leaderCtx  context.Context
	leaderStop context.CancelFunc
	result     Result
	err        error
}

type platformResolver interface {
	ResolvePlatform(context.Context, protocol.System) (string, error)
}

func (r *metadataRuntime) Lookup(parent context.Context, input LookupInput) (Result, error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return Result{}, mapContextError(err)
	}
	input = copyLookupInput(input)
	if err := protocol.ValidateSystem(input.System); err != nil {
		return Result{}, newOpError(ErrPolicyBlocked, nil)
	}
	normalized, err := NormalizeTitle(input.Title)
	if err != nil {
		return Result{}, newOpError(ErrPolicyBlocked, nil)
	}
	flightKeyBytes, err := BuildCacheKey(ProviderIGDB, "system:"+string(input.System), normalized, "")
	if err != nil {
		return Result{}, err
	}
	flightKey := string(flightKeyBytes)
	if err := r.policyBefore(flightKey); err != nil {
		return Result{}, err
	}
	return r.joinLookup(parent, flightKey, input, normalized)
}

func (r *metadataRuntime) joinLookup(parent context.Context, flightKey string, input LookupInput, normalized string) (Result, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return Result{}, newOpError(ErrCanceled, context.Canceled)
	}
	if existing := r.inFlight[flightKey]; existing != nil {
		done := existing.done
		r.mu.Unlock()
		select {
		case <-done:
			return existing.result, existing.err
		case <-parent.Done():
			return Result{}, mapContextError(parent.Err())
		}
	}
	if err := r.policyBeforeLocked(flightKey); err != nil {
		r.mu.Unlock()
		return Result{}, err
	}
	leaderCtx, leaderStop := context.WithCancel(r.rootCtx)
	call := &lookupCall{done: make(chan struct{}), leaderCtx: leaderCtx, leaderStop: leaderStop}
	r.inFlight[flightKey] = call
	r.leaders.Add(1)
	r.mu.Unlock()
	go r.runLookup(call, flightKey, input, normalized)
	select {
	case <-call.done:
		return call.result, call.err
	case <-parent.Done():
		return Result{}, mapContextError(parent.Err())
	}
}

func (r *metadataRuntime) runLookup(call *lookupCall, flightKey string, input LookupInput, normalized string) {
	defer r.leaders.Done()
	ctx, cancel := context.WithTimeout(call.leaderCtx, lookupTimeout)
	result, err := r.lookupLeader(ctx, flightKey, input, normalized)
	cancel()
	call.result, call.err = result, err
	r.mu.Lock()
	delete(r.inFlight, flightKey)
	r.mu.Unlock()
	call.leaderStop()
	close(call.done)
}

func (r *metadataRuntime) lookupLeader(ctx context.Context, flightKey string, input LookupInput, normalized string) (Result, error) {
	platformID, epoch, err := r.resolvePlatform(ctx, input.System)
	if err != nil {
		return Result{}, err
	}
	if platformID == "unknown" {
		providerResult, providerEpoch, callErr := r.providerLookup(ctx, flightKey, ProviderQuery{NormalizedTitle: normalized, System: input.System})
		if callErr != nil {
			return Result{}, callErr
		}
		epoch = providerEpoch
		platformID = providerResult.PlatformID
		if strings.TrimSpace(platformID) == "" {
			return Result{}, newOpError(ErrInvalidResponse, nil)
		}
		r.mu.Lock()
		r.platformIDs[input.System] = platformEntry{ID: platformID, ExpiresAt: r.now().Add(positiveMetadataTTL)}
		r.mu.Unlock()
		key, keyErr := BuildCacheKey(ProviderIGDB, platformID, normalized, "")
		if keyErr != nil {
			return Result{}, keyErr
		}
		cached, state, cacheErr := r.cache.getState(key)
		if cacheErr != nil {
			return Result{}, cacheErr
		}
		if state == cacheFresh {
			r.incrementObservation(ObservationCacheHit)
			return r.retryMissingArtwork(ctx, key, cached)
		}
		r.incrementObservation(ObservationCacheMiss)
		var previous *cacheEntry
		if state == cacheExpiredPositive {
			previous = &cached
		}
		return r.finishProviderLookup(ctx, input, normalized, platformID, key, providerResult, epoch, previous)
	}

	key, keyErr := BuildCacheKey(ProviderIGDB, platformID, normalized, "")
	if keyErr != nil {
		return Result{}, keyErr
	}
	cached, state, cacheErr := r.cache.getState(key)
	if cacheErr != nil {
		return Result{}, cacheErr
	}
	if state == cacheFresh {
		r.incrementObservation(ObservationCacheHit)
		return r.retryMissingArtwork(ctx, key, cached)
	}
	if state == cacheMiss || state == cacheExpiredPositive {
		r.incrementObservation(ObservationCacheMiss)
	}
	providerResult, providerEpoch, callErr := r.providerLookup(ctx, string(key), ProviderQuery{NormalizedTitle: normalized, System: input.System})
	if callErr != nil {
		return Result{}, callErr
	}
	if providerResult.PlatformID != "" && providerResult.PlatformID != platformID {
		return Result{}, newOpError(ErrInvalidResponse, nil)
	}
	if providerEpoch != epoch {
		epoch = providerEpoch
	}
	if state == cacheExpiredPositive {
		return r.finishProviderLookup(ctx, input, normalized, platformID, key, providerResult, epoch, &cached)
	}
	return r.finishProviderLookup(ctx, input, normalized, platformID, key, providerResult, epoch, nil)
}

func (r *metadataRuntime) resolvePlatform(ctx context.Context, system protocol.System) (string, uint64, error) {
	r.mu.Lock()
	if entry, ok := r.platformIDs[system]; ok && entry.ID != "" && r.now().Before(entry.ExpiresAt) {
		epoch := r.providerEpoch
		r.mu.Unlock()
		return entry.ID, epoch, nil
	}
	if r.closed {
		r.mu.Unlock()
		return "", 0, newOpError(ErrCanceled, context.Canceled)
	}
	resolver, ok := r.provider.(platformResolver)
	r.mu.Unlock()
	if !ok {
		r.mu.Lock()
		epoch := r.providerEpoch
		r.mu.Unlock()
		return "unknown", epoch, nil
	}
	if err := r.policyBefore(platformPolicyKey); err != nil {
		return "", 0, err
	}
	r.mu.Lock()
	epoch := r.providerEpoch
	r.mu.Unlock()
	id, callErr := resolver.ResolvePlatform(ctx, system)
	callErr = classifyProviderError(callErr)
	if policyErr := r.policyAfter(platformPolicyKey, callErr); policyErr != nil {
		return "", epoch, policyErr
	}
	if callErr != nil {
		return "", epoch, callErr
	}
	if strings.TrimSpace(id) == "" {
		invalid := newOpError(ErrInvalidResponse, nil)
		r.policyAfter(platformPolicyKey, invalid)
		return "", epoch, invalid
	}
	r.mu.Lock()
	if epoch != r.providerEpoch {
		r.mu.Unlock()
		return "", epoch, newOpError(ErrProviderRemoved, nil)
	}
	r.platformIDs[system] = platformEntry{ID: id, ExpiresAt: r.now().Add(positiveMetadataTTL)}
	r.mu.Unlock()
	return id, epoch, nil
}

func (r *metadataRuntime) providerLookup(ctx context.Context, key string, query ProviderQuery) (ProviderResult, uint64, error) {
	if err := r.policyBefore(key); err != nil {
		return ProviderResult{}, 0, err
	}
	r.mu.Lock()
	epoch := r.providerEpoch
	r.mu.Unlock()
	result, callErr := r.provider.Lookup(ctx, query)
	callErr = classifyProviderError(callErr)
	if policyErr := r.policyAfter(key, callErr); policyErr != nil {
		return ProviderResult{}, epoch, policyErr
	}
	if callErr != nil {
		return ProviderResult{}, epoch, callErr
	}
	return result, epoch, nil
}

func (r *metadataRuntime) policyBefore(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.policyBeforeLocked(key)
}

func (r *metadataRuntime) policyBeforeLocked(key string) error {
	if r.closed {
		return newOpError(ErrCanceled, context.Canceled)
	}
	now := r.now()
	if r.latched != "" {
		return newOpError(r.latched, nil)
	}
	if !r.providerRateUntil.IsZero() && now.Before(r.providerRateUntil) {
		err := newOpError(ErrRateLimited, nil)
		err.RetryAfter = r.providerRateUntil.Sub(now)
		return err
	}
	if !r.providerBackoffUntil.IsZero() && now.Before(r.providerBackoffUntil) {
		err := newOpError(ErrUpstreamUnavailable, nil)
		err.RetryAfter = r.providerBackoffUntil.Sub(now)
		return err
	}
	if entry, ok := r.keyPolicy[key]; ok && now.Before(entry.Until) {
		err := newOpError(entry.Code, nil)
		err.RetryAfter = entry.Until.Sub(now)
		return err
	}
	return nil
}

func (r *metadataRuntime) policyAfter(key string, err error) error {
	if err == nil {
		r.mu.Lock()
		r.providerBackoff, r.providerBackoffUntil = 0, time.Time{}
		r.mu.Unlock()
		return nil
	}
	code := opCode(err)
	r.incrementProviderError(code)
	now := r.now()
	purge := false
	r.mu.Lock()
	switch code {
	case ErrUnauthorized, ErrUnconfigured, ErrPolicyBlocked:
		if r.latched == "" {
			r.latched = code
		}
	case ErrProviderRemoved:
		r.providerEpoch++
		r.platformIDs = make(map[protocol.System]platformEntry)
		if r.latched == "" {
			r.latched = ErrProviderRemoved
		}
		purge = true
	case ErrRateLimited:
		delay := 30 * time.Second
		if operation, ok := err.(*OpError); ok && operation.RetryAfter > 0 {
			delay = clampRetryAfter(operation.RetryAfter)
		}
		r.providerRateUntil = now.Add(delay)
	case ErrDeadline, ErrUpstreamUnavailable:
		delay := time.Minute
		r.keyPolicy[key] = policyEntry{Until: now.Add(delay), Code: code}
		if r.providerBackoff == 0 {
			r.providerBackoff = time.Second
		} else {
			r.providerBackoff *= 2
			if r.providerBackoff > 15*time.Minute {
				r.providerBackoff = 15 * time.Minute
			}
		}
		r.providerBackoffUntil = now.Add(r.providerBackoff)
	case ErrInvalidResponse:
		r.keyPolicy[key] = policyEntry{Until: now.Add(5 * time.Minute), Code: code}
	case ErrStorage:
		r.keyPolicy[key] = policyEntry{Until: now.Add(5 * time.Second), Code: code}
	}
	r.mu.Unlock()
	if purge {
		r.publicationMu.Lock()
		defer r.publicationMu.Unlock()
		if resetter, ok := r.provider.(interface{ ResetMetadataState() }); ok {
			resetter.ResetMetadataState()
		}
		if err := r.cache.PurgeProvider(ProviderIGDB); err != nil {
			return newOpError(ErrStorage, nil)
		}
	}
	return nil
}

func clampRetryAfter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 30 * time.Second
	}
	if delay < time.Second {
		return time.Second
	}
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}

func (r *metadataRuntime) incrementProviderError(code ErrorCode) {
	switch code {
	case ErrUnauthorized:
		r.incrementObservation(ObservationUnauthorized)
	case ErrUnconfigured, ErrPolicyBlocked:
		r.incrementObservation(ObservationOffline)
	case ErrRateLimited:
		r.incrementObservation(ObservationRateLimited)
	case ErrInvalidResponse:
		r.incrementObservation(ObservationInvalidResponse)
	case ErrUpstreamUnavailable, ErrDeadline, ErrStorage, ErrProviderRemoved:
		r.incrementObservation(ObservationOffline)
	}
}

func (r *metadataRuntime) incrementObservation(kind ObservationKind) {
	r.mu.Lock()
	r.counters[kind]++
	r.mu.Unlock()
}

// CounterSnapshot returns the closed typed observability projection.
func (r *metadataRuntime) CounterSnapshot() CounterSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	counters := make(map[ObservationKind]uint64, len(observationKinds))
	for _, kind := range observationKinds {
		counters[kind] = r.counters[kind]
	}
	return CounterSnapshot{Provider: ProviderIGDB, Counters: counters}
}

// ObservabilitySnapshot returns only coarse counter names and values.
func (r *metadataRuntime) ObservabilitySnapshot() map[string]uint64 {
	snapshot := r.CounterSnapshot()
	out := make(map[string]uint64, len(snapshot.Counters))
	for key, value := range snapshot.Counters {
		out[string(key)] = value
	}
	return out
}

var observationKinds = []ObservationKind{
	ObservationExact, ObservationConfident, ObservationAmbiguous, ObservationNoMatch,
	ObservationUnauthorized, ObservationRateLimited, ObservationOffline,
	ObservationInvalidResponse, ObservationArtworkRejected, ObservationCacheHit,
	ObservationCacheMiss,
}

func (r *metadataRuntime) finishProviderLookup(ctx context.Context, input LookupInput, normalized, platformID string, key []byte, providerResult ProviderResult, epoch uint64, previous *cacheEntry) (Result, error) {
	decision, err := MatchCandidates(input, platformID, providerResult.Candidates)
	if err != nil {
		return Result{}, err
	}
	result := Result{Outcome: decision.Outcome}
	var artwork []ArtworkCacheEntry
	if decision.Outcome == OutcomeExact || decision.Outcome == OutcomeConfident {
		result.Presentation = presentationFromCandidate(decision.Candidate)
		result.Attribution = Attribution{Provider: ProviderIGDB, Label: "Data from IGDB.com"}
		for _, reference := range decision.Candidate.Artwork {
			transform := artworkTransformCover
			if reference.Role == ArtworkBackdrop {
				transform = artworkTransformBackdrop
			}
			provenanceUnchanged := previous != nil &&
				previous.metadata.ProviderGameID == decision.Candidate.ProviderID &&
				previous.metadata.ProviderChecksum == decision.Candidate.Checksum &&
				previous.metadata.ProviderUpdatedAt.Equal(decision.Candidate.UpdatedAt)
			if provenanceUnchanged {
				for _, old := range previous.artwork {
					if old.Role == reference.Role && old.ProviderImageID == reference.ID && old.Transform == transform && old.ContentDigest != "" {
						artwork = append(artwork, old)
						if reference.Role == ArtworkCover {
							result.Presentation.CoverArtworkID = old.Handle
						} else {
							result.Presentation.BackdropArtworkID = old.Handle
						}
						goto nextArtwork
					}
				}
			}
			artwork = append(artwork, r.fetchArtworkReference(ctx, key, reference, transform, &result.Presentation))
		nextArtwork:
		}
	}
	ttl := negativeMetadataTTL
	if result.Outcome == OutcomeExact || result.Outcome == OutcomeConfident {
		ttl = positiveMetadataTTL
	}
	if err := r.publishCache(epoch, key, CacheRecordMetadata{
		Provider:          ProviderIGDB,
		PlatformID:        platformID,
		NormalizedTitle:   normalized,
		ProviderGameID:    decision.Candidate.ProviderID,
		MatchScore:        decision.Score,
		ProviderUpdatedAt: decision.Candidate.UpdatedAt,
		ProviderChecksum:  decision.Candidate.Checksum,
	}, result, r.now().Add(ttl), artwork); err != nil {
		return Result{}, err
	}
	switch result.Outcome {
	case OutcomeExact:
		r.incrementObservation(ObservationExact)
	case OutcomeConfident:
		r.incrementObservation(ObservationConfident)
	case OutcomeAmbiguous:
		r.incrementObservation(ObservationAmbiguous)
	case OutcomeNoMatch:
		r.incrementObservation(ObservationNoMatch)
	}
	return result, nil
}

func (r *metadataRuntime) publishCache(epoch uint64, key []byte, metadata CacheRecordMetadata, result Result, expiresAt time.Time, artwork []ArtworkCacheEntry) error {
	if err := r.lockPublication(r.rootCtx); err != nil {
		return err
	}
	defer r.publicationMu.Unlock()
	r.mu.Lock()
	err := r.ensureEpochLocked(epoch)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if err := r.rootCtx.Err(); err != nil {
		return mapContextError(err)
	}
	if err := r.cache.PutWithMetadataContext(r.rootCtx, key, metadata, result, expiresAt, artwork); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ensureEpochLocked(epoch)
}

func (r *metadataRuntime) lockPublication(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if r.publicationMu.TryLock() {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return mapContextError(ctx.Err())
		case <-timer.C:
		}
	}
}

func (r *metadataRuntime) fetchArtworkReference(ctx context.Context, key []byte, reference ArtworkRef, transform string, presentation *Presentation) ArtworkCacheEntry {
	suppressionKey := artworkSuppressionKey(reference.Role, reference.ID, transform)
	r.mu.Lock()
	suppressed := r.now().Before(r.artworkUntil[suppressionKey])
	r.mu.Unlock()
	if suppressed {
		return ArtworkCacheEntry{Handle: artworkHandle(key, reference.Role, reference.ID), Role: reference.Role, ProviderImageID: reference.ID, Transform: transform}
	}
	object, fetchErr := r.artwork.Fetch(ctx, key, reference)
	if fetchErr != nil {
		r.mu.Lock()
		r.artworkUntil[suppressionKey] = r.now().Add(5 * time.Minute)
		r.mu.Unlock()
		r.incrementObservation(ObservationArtworkRejected)
		return ArtworkCacheEntry{Handle: artworkHandle(key, reference.Role, reference.ID), Role: reference.Role, ProviderImageID: reference.ID, Transform: transform}
	}
	entry := ArtworkCacheEntry{Handle: object.Handle, Role: reference.Role, ProviderImageID: reference.ID, Transform: transform, ContentDigest: object.Digest, MIME: object.MIME, ByteCount: object.Size, Width: object.Width, Height: object.Height}
	if reference.Role == ArtworkCover {
		presentation.CoverArtworkID = object.Handle
	} else {
		presentation.BackdropArtworkID = object.Handle
	}
	return entry
}

func artworkSuppressionKey(role ArtworkRole, imageID, transform string) string {
	return string(ProviderIGDB) + "\x00" + string(role) + "\x00" + imageID + "\x00" + transform
}

func (r *metadataRuntime) retryMissingArtwork(ctx context.Context, key []byte, cached cacheEntry) (Result, error) {
	r.recordCachedOutcome(cached.result.Outcome)
	if cached.result.Outcome != OutcomeExact && cached.result.Outcome != OutcomeConfident {
		return cached.result, nil
	}
	result := cached.result
	for index, ref := range cached.artwork {
		if ref.ContentDigest != "" {
			continue
		}
		transform := ref.Transform
		suppressionKey := artworkSuppressionKey(ref.Role, ref.ProviderImageID, transform)
		r.mu.Lock()
		suppressed := r.now().Before(r.artworkUntil[suppressionKey])
		r.mu.Unlock()
		if suppressed {
			continue
		}
		object, fetchErr := r.artwork.Fetch(ctx, key, ArtworkRef{Role: ref.Role, ID: ref.ProviderImageID})
		if fetchErr != nil {
			r.mu.Lock()
			r.artworkUntil[suppressionKey] = r.now().Add(5 * time.Minute)
			r.mu.Unlock()
			r.incrementObservation(ObservationArtworkRejected)
			continue
		}
		attached := ArtworkCacheEntry{Handle: object.Handle, Role: ref.Role, ProviderImageID: ref.ProviderImageID, Transform: transform, ContentDigest: object.Digest, MIME: object.MIME, ByteCount: object.Size, Width: object.Width, Height: object.Height}
		if err := r.cache.AttachArtwork(key, attached); err != nil {
			r.mu.Lock()
			r.artworkUntil[suppressionKey] = r.now().Add(5 * time.Minute)
			r.mu.Unlock()
			r.incrementObservation(ObservationArtworkRejected)
			continue
		}
		cached.artwork[index] = attached
		if ref.Role == ArtworkCover {
			result.Presentation.CoverArtworkID = object.Handle
		} else {
			result.Presentation.BackdropArtworkID = object.Handle
		}
	}
	return result, nil
}

func (r *metadataRuntime) recordCachedOutcome(outcome Outcome) {
	switch outcome {
	case OutcomeExact:
		r.incrementObservation(ObservationExact)
	case OutcomeConfident:
		r.incrementObservation(ObservationConfident)
	case OutcomeAmbiguous:
		r.incrementObservation(ObservationAmbiguous)
	case OutcomeNoMatch:
		r.incrementObservation(ObservationNoMatch)
	}
}

func (r *metadataRuntime) ensureEpoch(epoch uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ensureEpochLocked(epoch)
}

func (r *metadataRuntime) ensureEpochLocked(epoch uint64) error {
	if r.closed {
		return newOpError(ErrCanceled, context.Canceled)
	}
	if r.providerEpoch != epoch {
		return newOpError(ErrProviderRemoved, nil)
	}
	if r.latched != "" {
		return newOpError(r.latched, nil)
	}
	return nil
}

func presentationFromCandidate(candidate Candidate) Presentation {
	return Presentation{
		Summary: boundedText(candidate.Summary, 240),
		Year:    yearText(candidate.FirstReleaseYear),
		Genre:   boundedText(strings.Join(candidate.Genres, ", "), 40),
		Studio:  boundedText(strings.Join(candidate.Studios, ", "), 60),
		Players: boundedText(candidate.Players, 40),
	}
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func yearText(year int) string {
	if year <= 0 {
		return ""
	}
	return boundedText(strconvItoa(year), 20)
}

func classifyProviderError(err error) error {
	if err == nil {
		return nil
	}
	var operation *OpError
	if errors.As(err, &operation) && operation != nil {
		return operation
	}
	return mapContextError(err)
}

func (r *metadataRuntime) OpenArtwork(ctx context.Context, handle string) (Artwork, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Artwork{}, mapContextError(err)
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return Artwork{}, newOpError(ErrCanceled, context.Canceled)
	}
	lookup, ok, err := r.cache.Artwork(handle)
	if err != nil {
		return Artwork{}, err
	}
	if !ok {
		return Artwork{}, newOpError(ErrStorage, nil)
	}
	artwork, err := r.artwork.OpenDigest(lookup.ContentDigest)
	if err != nil {
		return Artwork{}, err
	}
	if artwork.MIME != lookup.MIME || artwork.Size != lookup.ByteCount {
		_ = artwork.Reader.Close()
		return Artwork{}, newOpError(ErrStorage, nil)
	}
	return artwork, nil
}

func (r *metadataRuntime) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		reaper := &runtimeReaper{done: make(chan struct{})}
		r.reaper = reaper
		r.mu.Unlock()
		r.cancel()
		go r.runReaper(reaper)
		select {
		case <-reaper.done:
			if reaper.err != nil {
				r.closeErr = newOpError(ErrStorage, reaper.err)
			}
		case <-time.After(2 * time.Second):
			r.closeErr = newOpError(ErrStorage, errors.New("metadata shutdown retained by reaper"))
		}
	})
	if r.closeErr != nil {
		return r.closeErr
	}
	return nil
}

func (r *metadataRuntime) runReaper(reaper *runtimeReaper) {
	defer close(reaper.done)
	var errs []error
	if closer, ok := r.provider.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	r.leaders.Wait()
	if err := r.artwork.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := r.cache.Close(); err != nil {
		errs = append(errs, err)
		if retirement := r.cache.retirementRecord(); retirement != nil {
			<-retirement.done
			if err := retirement.error(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	reaper.err = errors.Join(errs...)
}

var derivedArtifactHookState struct {
	sync.Mutex
	beforeDelete func()
}

// setDerivedArtifactBeforeUseHookForTest is a deterministic adversarial-test
// seam. Production callers never install a hook; the hook is invoked after a
// held derived-artifact root has been validated and before its final use.
func setDerivedArtifactBeforeUseHookForTest(hook func()) func() {
	derivedArtifactHookState.Lock()
	previous := derivedArtifactHookState.beforeDelete
	derivedArtifactHookState.beforeDelete = hook
	derivedArtifactHookState.Unlock()
	return func() {
		derivedArtifactHookState.Lock()
		derivedArtifactHookState.beforeDelete = previous
		derivedArtifactHookState.Unlock()
	}
}

// setPurgeBeforeDeleteHookForTest is retained for the existing purge
// contracts; all derived-artifact operations share the same seam.
func setPurgeBeforeDeleteHookForTest(hook func()) func() {
	return setDerivedArtifactBeforeUseHookForTest(hook)
}

func derivedArtifactBeforeUseHook() func() {
	derivedArtifactHookState.Lock()
	defer derivedArtifactHookState.Unlock()
	return derivedArtifactHookState.beforeDelete
}

func purgeBeforeDeleteHook() func() {
	return derivedArtifactBeforeUseHook()
}

func purgeMetadataRoot(root string) error {
	if root == "" {
		return nil
	}
	owner, err := acquirePrivateRoot(root, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lease, err := acquireCacheRootLease(owner.info)
	if err != nil {
		_ = owner.abort(false)
		return err
	}
	artwork, err := openArtworkDirectory(owner, false)
	if errors.Is(err, os.ErrNotExist) {
		artwork = nil
	} else if err != nil {
		_ = owner.abort(false)
		lease.release()
		return err
	}
	cleanup := func(cause error) error {
		if artwork != nil {
			cause = errors.Join(cause, artwork.close())
		}
		// Keep the physical-root lease until every descriptor is closed and the
		// owner has released its root. A future opener must never race cleanup.
		cause = errors.Join(cause, owner.abort(false))
		lease.release()
		return cause
	}
	if err := purgeDerivedFilesOwned(owner, artwork); err != nil {
		return cleanup(err)
	}
	if artwork != nil {
		if err := artwork.removeIfEmpty(); err != nil {
			return cleanup(err)
		}
	}
	return cleanup(nil)
}
