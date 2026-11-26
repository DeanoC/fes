package metadata

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

func (c *Cache) Get(key []byte) (Result, bool, error) {
	entry, state, err := c.getState(key)
	if err != nil {
		return Result{}, false, err
	}
	if state == cacheExpiredPositive {
		c.operationMu.Lock()
		defer c.operationMu.Unlock()
		release, beginErr := c.begin()
		if beginErr != nil {
			return Result{}, false, beginErr
		}
		defer release()
		if _, deleteErr := c.db.Exec(`DELETE FROM metadata_records WHERE cache_key=?`, key); deleteErr != nil {
			return Result{}, false, newOpError(ErrStorage, nil)
		}
		if gcErr := c.gcUnreferenced(); gcErr != nil {
			return Result{}, false, gcErr
		}
		return Result{}, false, nil
	}
	if state != cacheFresh {
		return Result{}, false, nil
	}
	return entry.result, true, nil
}

type CachedPresentation struct {
	PlatformID      string
	NormalizedTitle string
	Region          string
	Outcome         Outcome
	Genre           string
	Year            string
}

func (c *Cache) CachedPresentations(ctx context.Context) ([]CachedPresentation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return nil, err
	}
	defer release()
	now := c.now().UnixNano()
	rows, err := c.db.QueryContext(ctx, `
		SELECT platform_id, normalized_title, region, outcome, genre, year
		FROM metadata_records
		WHERE outcome IN ('exact', 'confident')
		  AND (genre <> '' OR year <> '')
		  AND expires_at > ?
		ORDER BY normalized_title, platform_id
		LIMIT ?`, now, maxMetadataRecords)
	if err != nil {
		return nil, newOpError(ErrStorage, err)
	}
	defer rows.Close()
	records := make([]CachedPresentation, 0)
	for rows.Next() {
		var record CachedPresentation
		if err := rows.Scan(&record.PlatformID, &record.NormalizedTitle, &record.Region, &record.Outcome, &record.Genre, &record.Year); err != nil {
			return nil, newOpError(ErrStorage, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, newOpError(ErrStorage, err)
	}
	return records, nil
}

func (c *Cache) getState(key []byte) (cacheEntry, cacheState, error) {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return cacheEntry{}, cacheMiss, err
	}
	defer release()
	if len(key) != 32 {
		return cacheEntry{}, cacheMiss, newOpError(ErrPolicyBlocked, nil)
	}
	pending, err := c.pendingRecord()
	if err != nil {
		return cacheEntry{}, cacheMiss, err
	}
	if pending == hex.EncodeToString(key) {
		return cacheEntry{}, cacheMiss, nil
	}
	var (
		provider, outcome, summary, year, genre, studio, players string
		coverHandle, backdropHandle                              string
		platformID, normalizedTitle, region                      string
		providerGameID, providerChecksum                         sql.NullString
		providerUpdatedAt                                        sql.NullInt64
		matchScore                                               sql.NullInt64
		retrievedAt, expiresAt                                   int64
	)
	err = c.db.QueryRow(`SELECT provider, platform_id, normalized_title, region, outcome, provider_game_id, match_score, summary, year, genre, studio, players, cover_handle, backdrop_handle, provider_updated_at, provider_checksum, retrieved_at, expires_at FROM metadata_records WHERE cache_key = ?`, key).Scan(
		&provider, &platformID, &normalizedTitle, &region, &outcome, &providerGameID, &matchScore,
		&summary, &year, &genre, &studio, &players, &coverHandle, &backdropHandle,
		&providerUpdatedAt, &providerChecksum, &retrievedAt, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return cacheEntry{}, cacheMiss, nil
	}
	if err != nil || provider != string(ProviderIGDB) {
		return cacheEntry{}, cacheMiss, newOpError(ErrStorage, err)
	}
	metadata := CacheRecordMetadata{Provider: ProviderIGDB, PlatformID: platformID, NormalizedTitle: normalizedTitle, Region: region}
	if matchScore.Valid {
		metadata.MatchScore = int(matchScore.Int64)
	}
	if providerGameID.Valid {
		metadata.ProviderGameID = providerGameID.String
	}
	if providerChecksum.Valid {
		metadata.ProviderChecksum = providerChecksum.String
	}
	if providerUpdatedAt.Valid && providerUpdatedAt.Int64 != 0 {
		metadata.ProviderUpdatedAt = time.Unix(0, providerUpdatedAt.Int64)
	}
	entry := cacheEntry{
		result: Result{Outcome: Outcome(outcome)}, metadata: metadata,
		retrievedAt: time.Unix(0, retrievedAt), expiresAt: time.Unix(0, expiresAt),
	}
	if entry.result.Outcome == OutcomeExact || entry.result.Outcome == OutcomeConfident {
		entry.result.Presentation = Presentation{Summary: summary, Year: year, Genre: genre, Studio: studio, Players: players, CoverArtworkID: coverHandle, BackdropArtworkID: backdropHandle}
		entry.result.Attribution = Attribution{Provider: ProviderIGDB, Label: "Data from IGDB.com"}
	}
	rows, err := c.db.Query(`SELECT r.handle, r.role, r.provider_image_id, r.transform, r.content_digest, COALESCE(o.mime,''), COALESCE(o.byte_count,0), COALESCE(o.width,0), COALESCE(o.height,0) FROM artwork_refs r LEFT JOIN artwork_objects o ON o.content_digest=r.content_digest WHERE r.cache_key=? ORDER BY r.role`, key)
	if err != nil {
		return cacheEntry{}, cacheMiss, newOpError(ErrStorage, nil)
	}
	for rows.Next() {
		var ref ArtworkCacheEntry
		var digest sql.NullString
		var role string
		if err := rows.Scan(&ref.Handle, &role, &ref.ProviderImageID, &ref.Transform, &digest, &ref.MIME, &ref.ByteCount, &ref.Width, &ref.Height); err != nil {
			_ = rows.Close()
			return cacheEntry{}, cacheMiss, newOpError(ErrStorage, nil)
		}
		ref.Role = ArtworkRole(role)
		if digest.Valid {
			ref.ContentDigest = digest.String
		}
		entry.artwork = append(entry.artwork, ref)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return cacheEntry{}, cacheMiss, newOpError(ErrStorage, nil)
	}
	if err := rows.Close(); err != nil {
		return cacheEntry{}, cacheMiss, newOpError(ErrStorage, nil)
	}
	now := c.now().UnixNano()
	if now >= expiresAt {
		if entry.result.Outcome == OutcomeNoMatch || entry.result.Outcome == OutcomeAmbiguous {
			if _, err := c.db.Exec(`DELETE FROM metadata_records WHERE cache_key = ?`, key); err != nil {
				return cacheEntry{}, cacheMiss, newOpError(ErrStorage, nil)
			}
			if err := c.gcUnreferenced(); err != nil {
				return cacheEntry{}, cacheMiss, err
			}
			return cacheEntry{}, cacheMiss, nil
		}
		return entry, cacheExpiredPositive, nil
	}
	if _, err := c.db.Exec(`UPDATE metadata_records SET last_accessed_at=? WHERE cache_key=? AND last_accessed_at <= ?`, now, key, now-int64(time.Hour)); err != nil {
		return cacheEntry{}, cacheMiss, newOpError(ErrStorage, nil)
	}
	return entry, cacheFresh, nil
}

func (c *Cache) Put(key []byte, result Result, expiresAt time.Time, artwork []ArtworkCacheEntry) error {
	return c.PutWithMetadata(key, CacheRecordMetadata{Provider: ProviderIGDB}, result, expiresAt, artwork)
}

func (c *Cache) PutWithMetadata(key []byte, metadata CacheRecordMetadata, result Result, expiresAt time.Time, artwork []ArtworkCacheEntry) error {
	return c.PutWithMetadataContext(context.Background(), key, metadata, result, expiresAt, artwork)
}

func (c *Cache) PutWithMetadataContext(ctx context.Context, key []byte, metadata CacheRecordMetadata, result Result, expiresAt time.Time, artwork []ArtworkCacheEntry) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return mapContextError(err)
	}
	if err := c.lockOperation(ctx); err != nil {
		return err
	}
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return mapContextError(err)
	}
	if len(key) != 32 || !validOutcome(result.Outcome) || expiresAt.IsZero() {
		return newOpError(ErrPolicyBlocked, nil)
	}
	if metadata.Provider == "" {
		metadata.Provider = ProviderIGDB
	}
	if metadata.Provider != ProviderIGDB {
		return newOpError(ErrPolicyBlocked, nil)
	}
	cover, backdrop := "", ""
	seenRoles := make(map[ArtworkRole]struct{}, 2)
	for _, entry := range artwork {
		if entry.Role != ArtworkCover && entry.Role != ArtworkBackdrop {
			return newOpError(ErrPolicyBlocked, nil)
		}
		if _, exists := seenRoles[entry.Role]; exists {
			return newOpError(ErrPolicyBlocked, nil)
		}
		seenRoles[entry.Role] = struct{}{}
		if !digestPattern.MatchString(entry.Handle) || !imageIDPattern.MatchString(entry.ProviderImageID) || (entry.ContentDigest != "" && !digestPattern.MatchString(entry.ContentDigest)) {
			return newOpError(ErrPolicyBlocked, nil)
		}
		if entry.ContentDigest != "" {
			if (entry.MIME != "image/jpeg" && entry.MIME != "image/png") || entry.ByteCount < 1 || entry.ByteCount > maxArtworkCompressed || entry.Width < 1 || entry.Height < 1 || entry.Width > 4096 || entry.Height > 4096 || int64(entry.Width)*int64(entry.Height) > 16_777_216 {
				return newOpError(ErrPolicyBlocked, nil)
			}
		}
		expectedTransform := artworkTransformCover
		if entry.Role == ArtworkBackdrop {
			expectedTransform = artworkTransformBackdrop
		}
		if entry.Transform != "" && entry.Transform != expectedTransform {
			return newOpError(ErrPolicyBlocked, nil)
		}
		if entry.ContentDigest != "" {
			if entry.Role == ArtworkCover {
				cover = entry.Handle
			} else {
				backdrop = entry.Handle
			}
		}
	}
	if result.Outcome != OutcomeExact && result.Outcome != OutcomeConfident {
		cover, backdrop = "", ""
		artwork = nil
	}
	now := c.now().UnixNano()
	pending := hex.EncodeToString(key)
	tx, err := c.db.Begin()
	if err != nil {
		return newOpError(ErrStorage, nil)
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		if err := ctx.Err(); err != nil {
			return mapContextError(err)
		}
		return newOpError(ErrStorage, cause)
	}
	var providerUpdatedAt any
	if !metadata.ProviderUpdatedAt.IsZero() {
		providerUpdatedAt = metadata.ProviderUpdatedAt.UnixNano()
	}
	var providerGameID any
	if metadata.ProviderGameID != "" {
		providerGameID = metadata.ProviderGameID
	}
	var matchScore any
	if metadata.MatchScore != 0 {
		matchScore = metadata.MatchScore
	}
	_, err = tx.Exec(`INSERT INTO metadata_records(cache_key,provider,adapter_version,normalizer_version,platform_map_version,platform_id,normalized_title,region,outcome,provider_game_id,match_score,summary,year,genre,studio,players,cover_handle,backdrop_handle,provider_updated_at,provider_checksum,retrieved_at,expires_at,last_accessed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(cache_key) DO UPDATE SET provider=excluded.provider,adapter_version=excluded.adapter_version,normalizer_version=excluded.normalizer_version,platform_map_version=excluded.platform_map_version,platform_id=excluded.platform_id,normalized_title=excluded.normalized_title,region=excluded.region,outcome=excluded.outcome,provider_game_id=excluded.provider_game_id,match_score=excluded.match_score,summary=excluded.summary,year=excluded.year,genre=excluded.genre,studio=excluded.studio,players=excluded.players,cover_handle=excluded.cover_handle,backdrop_handle=excluded.backdrop_handle,provider_updated_at=excluded.provider_updated_at,provider_checksum=excluded.provider_checksum,retrieved_at=excluded.retrieved_at,expires_at=excluded.expires_at,last_accessed_at=excluded.last_accessed_at`,
		key, string(metadata.Provider), ProviderAdapterVersion, NormalizerVersion, PlatformMapVersion, metadata.PlatformID, metadata.NormalizedTitle, metadata.Region, result.Outcome, providerGameID, matchScore,
		result.Presentation.Summary, result.Presentation.Year, result.Presentation.Genre, result.Presentation.Studio, result.Presentation.Players,
		cover, backdrop, providerUpdatedAt, metadata.ProviderChecksum, now, expiresAt.UnixNano(), now)
	if err != nil {
		return rollback(err)
	}
	if _, err := tx.Exec(`DELETE FROM artwork_refs WHERE cache_key = ?`, key); err != nil {
		return rollback(err)
	}
	for _, entry := range artwork {
		transform := entry.Transform
		if transform == "" {
			if entry.Role == ArtworkCover {
				transform = artworkTransformCover
			} else {
				transform = artworkTransformBackdrop
			}
		}
		if entry.ContentDigest != "" {
			if _, err := tx.Exec(`INSERT INTO artwork_objects(content_digest,mime,byte_count,width,height,created_at,last_accessed_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(content_digest) DO UPDATE SET mime=excluded.mime,byte_count=excluded.byte_count,width=excluded.width,height=excluded.height,last_accessed_at=CASE WHEN artwork_objects.last_accessed_at <= ? THEN excluded.last_accessed_at ELSE artwork_objects.last_accessed_at END`, entry.ContentDigest, entry.MIME, entry.ByteCount, entry.Width, entry.Height, now, now, now-int64(time.Hour)); err != nil {
				return rollback(err)
			}
		}
		var digest any
		if entry.ContentDigest != "" {
			digest = entry.ContentDigest
		}
		if _, err := tx.Exec(`INSERT INTO artwork_refs(handle,cache_key,provider,role,provider_image_id,transform,content_digest) VALUES(?,?,?,?,?,?,?)`, entry.Handle, key, string(metadata.Provider), entry.Role, entry.ProviderImageID, transform, digest); err != nil {
			return rollback(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES('pending_record',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, pending); err != nil {
		return rollback(err)
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return newOpError(ErrStorage, nil)
	}
	if err := ctx.Err(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	if err := c.gcUnreferenced(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	if err := ctx.Err(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	if err := c.evictUnderLock(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	if err := ctx.Err(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	finalize, err := c.db.Begin()
	if err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	var present int
	if err := finalize.QueryRow(`SELECT EXISTS(SELECT 1 FROM metadata_records WHERE cache_key=?)`, key).Scan(&present); err != nil || present == 0 {
		_ = finalize.Rollback()
		return c.rollbackPendingLocked(key, err)
	}
	if _, err := finalize.Exec(`DELETE FROM settings WHERE key='pending_record' AND value=?`, pending); err != nil {
		_ = finalize.Rollback()
		return c.rollbackPendingLocked(key, err)
	}
	if err := finalize.Commit(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	return nil
}

func (c *Cache) rollbackPendingLocked(key []byte, cause error) error {
	pending := hex.EncodeToString(key)
	result := func() *OpError {
		switch {
		case errors.Is(cause, context.Canceled):
			return newOpError(ErrCanceled, context.Canceled)
		case errors.Is(cause, context.DeadlineExceeded):
			return newOpError(ErrDeadline, context.DeadlineExceeded)
		default:
			return newOpError(ErrStorage, cause)
		}
	}
	tx, err := c.db.Begin()
	if err != nil {
		return result()
	}
	if _, err := tx.Exec(`DELETE FROM metadata_records WHERE cache_key=?`, key); err != nil {
		_ = tx.Rollback()
		return result()
	}
	if _, err := tx.Exec(`DELETE FROM settings WHERE key='pending_record' AND value=?`, pending); err != nil {
		_ = tx.Rollback()
		return result()
	}
	if err := tx.Commit(); err != nil {
		return result()
	}
	return result()
}

func (c *Cache) Artwork(handle string) (ArtworkLookup, bool, error) {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return ArtworkLookup{}, false, err
	}
	defer release()
	if !digestPattern.MatchString(handle) {
		return ArtworkLookup{}, false, newOpError(ErrPolicyBlocked, nil)
	}
	var lookup ArtworkLookup
	var expires int64
	var cacheKey []byte
	var digest string
	err = c.db.QueryRow(`SELECT r.content_digest,o.mime,o.byte_count,o.width,o.height,m.expires_at,r.cache_key FROM artwork_refs r JOIN artwork_objects o ON o.content_digest=r.content_digest JOIN metadata_records m ON m.cache_key=r.cache_key WHERE r.handle=? AND NOT EXISTS (SELECT 1 FROM settings WHERE key='pending_record' AND value=lower(hex(r.cache_key)))`, handle).Scan(&digest, &lookup.MIME, &lookup.ByteCount, &lookup.Width, &lookup.Height, &expires, &cacheKey)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtworkLookup{}, false, nil
	}
	if err != nil {
		return ArtworkLookup{}, false, newOpError(ErrStorage, nil)
	}
	lookup.Handle, lookup.ContentDigest, lookup.ExpiresAt = handle, digest, time.Unix(0, expires)
	if !c.now().Before(lookup.ExpiresAt) {
		return ArtworkLookup{}, false, nil
	}
	if _, err := c.db.Exec(`UPDATE artwork_objects SET last_accessed_at=? WHERE content_digest=? AND last_accessed_at <= ?`, c.now().UnixNano(), lookup.ContentDigest, c.now().UnixNano()-int64(time.Hour)); err != nil {
		return ArtworkLookup{}, false, newOpError(ErrStorage, nil)
	}
	return lookup, true, nil
}

func (c *Cache) pendingRecord() (string, error) {
	var value string
	err := c.db.QueryRow(`SELECT value FROM settings WHERE key='pending_record'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", newOpError(ErrStorage, nil)
	}
	if !digestPattern.MatchString(value) {
		return "", newOpError(ErrStorage, nil)
	}
	return value, nil
}

func (c *Cache) AttachArtwork(key []byte, entry ArtworkCacheEntry) error {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return err
	}
	defer release()
	if len(key) != 32 || entry.ContentDigest == "" || !digestPattern.MatchString(entry.Handle) || !digestPattern.MatchString(entry.ContentDigest) || !imageIDPattern.MatchString(entry.ProviderImageID) {
		return newOpError(ErrPolicyBlocked, nil)
	}
	if (entry.MIME != "image/jpeg" && entry.MIME != "image/png") || entry.ByteCount < 1 || entry.Width < 1 || entry.Height < 1 || entry.Width > 4096 || entry.Height > 4096 || int64(entry.Width)*int64(entry.Height) > 16_777_216 {
		return newOpError(ErrPolicyBlocked, nil)
	}
	now := c.now().UnixNano()
	pending := hex.EncodeToString(key)
	tx, err := c.db.Begin()
	if err != nil {
		return newOpError(ErrStorage, nil)
	}
	transform := entry.Transform
	if transform == "" {
		if entry.Role == ArtworkCover {
			transform = artworkTransformCover
		} else {
			transform = artworkTransformBackdrop
		}
	}
	if _, err := tx.Exec(`INSERT INTO artwork_objects(content_digest,mime,byte_count,width,height,created_at,last_accessed_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(content_digest) DO UPDATE SET mime=excluded.mime,byte_count=excluded.byte_count,width=excluded.width,height=excluded.height,last_accessed_at=CASE WHEN artwork_objects.last_accessed_at <= ? THEN excluded.last_accessed_at ELSE artwork_objects.last_accessed_at END`, entry.ContentDigest, entry.MIME, entry.ByteCount, entry.Width, entry.Height, now, now, now-int64(time.Hour)); err != nil {
		_ = tx.Rollback()
		return newOpError(ErrStorage, nil)
	}
	result, err := tx.Exec(`UPDATE artwork_refs SET content_digest=?,transform=? WHERE cache_key=? AND role=? AND provider_image_id=?`, entry.ContentDigest, transform, key, entry.Role, entry.ProviderImageID)
	if err != nil {
		_ = tx.Rollback()
		return newOpError(ErrStorage, nil)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		if _, err := tx.Exec(`INSERT INTO artwork_refs(handle,cache_key,provider,role,provider_image_id,transform,content_digest) VALUES(?,?,?,?,?,?,?)`, entry.Handle, key, string(ProviderIGDB), entry.Role, entry.ProviderImageID, transform, entry.ContentDigest); err != nil {
			_ = tx.Rollback()
			return newOpError(ErrStorage, nil)
		}
	}
	column := "cover_handle"
	if entry.Role == ArtworkBackdrop {
		column = "backdrop_handle"
	}
	if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES('pending_record',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, pending); err != nil {
		_ = tx.Rollback()
		return newOpError(ErrStorage, nil)
	}
	if _, err := tx.Exec(`UPDATE metadata_records SET `+column+`=? WHERE cache_key=?`, entry.Handle, key); err != nil {
		_ = tx.Rollback()
		return newOpError(ErrStorage, nil)
	}
	if err := tx.Commit(); err != nil {
		return newOpError(ErrStorage, nil)
	}
	if err := c.gcUnreferenced(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	if err := c.evictUnderLock(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	finalize, err := c.db.Begin()
	if err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	if _, err := finalize.Exec(`DELETE FROM settings WHERE key='pending_record' AND value=?`, pending); err != nil {
		_ = finalize.Rollback()
		return c.rollbackPendingLocked(key, err)
	}
	if err := finalize.Commit(); err != nil {
		return c.rollbackPendingLocked(key, err)
	}
	return nil
}
