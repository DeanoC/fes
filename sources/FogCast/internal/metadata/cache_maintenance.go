package metadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type platformCache struct {
	Platforms   map[string]resolvedPlatform `json:"platforms"`
	RetrievedAt time.Time                   `json:"retrieved_at"`
	ExpiresAt   time.Time                   `json:"expires_at"`
}

func (c *Cache) platformMapping() (platformCache, bool, error) {
	var raw string
	err := c.db.QueryRow(`SELECT value FROM settings WHERE key='platform_mapping'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return platformCache{}, false, nil
	}
	if err != nil {
		return platformCache{}, false, newOpError(ErrStorage, nil)
	}
	var value platformCache
	if json.Unmarshal([]byte(raw), &value) != nil {
		return platformCache{}, false, newOpError(ErrStorage, nil)
	}
	return value, true, nil
}

func (c *Cache) PlatformMapping() (map[string]resolvedPlatform, time.Time, time.Time, bool, error) {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return nil, time.Time{}, time.Time{}, false, err
	}
	defer release()
	value, ok, err := c.platformMapping()
	if err != nil || !ok {
		return nil, time.Time{}, time.Time{}, ok, err
	}
	return clonePlatforms(value.Platforms), value.RetrievedAt, value.ExpiresAt, true, nil
}

func (c *Cache) SavePlatformMapping(platforms map[string]resolvedPlatform, retrievedAt, expiresAt time.Time) error {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return err
	}
	defer release()
	raw, err := json.Marshal(platformCache{Platforms: platforms, RetrievedAt: retrievedAt, ExpiresAt: expiresAt})
	if err != nil {
		return newOpError(ErrStorage, nil)
	}
	if _, err = c.db.Exec(`INSERT INTO settings(key,value) VALUES('platform_mapping',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, raw); err != nil {
		return newOpError(ErrStorage, nil)
	}
	return nil
}

func (c *Cache) InvalidatePlatform(id, checksum string) error {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return err
	}
	defer release()
	return c.purgeProviderLocked(ProviderIGDB)
}

func (c *Cache) PurgeProvider(provider ProviderName) error {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return err
	}
	defer release()
	return c.purgeProviderLocked(provider)
}

func (c *Cache) purgeProviderLocked(provider ProviderName) error {
	tx, err := c.db.Begin()
	if err != nil {
		return newOpError(ErrStorage, nil)
	}
	if _, err := tx.Exec(`DELETE FROM metadata_records WHERE provider=?`, string(provider)); err != nil {
		_ = tx.Rollback()
		return newOpError(ErrStorage, nil)
	}
	if provider == ProviderIGDB {
		if _, err := tx.Exec(`DELETE FROM settings WHERE key IN ('platform_mapping','pending_record')`); err != nil {
			_ = tx.Rollback()
			return newOpError(ErrStorage, nil)
		}
	}
	if err := tx.Commit(); err != nil {
		return newOpError(ErrStorage, nil)
	}
	return c.gcUnreferenced()
}

func (c *Cache) Purge() error {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return err
	}
	defer release()
	return c.purgeProviderLocked(ProviderIGDB)
}

func (c *Cache) gcUnreferenced() error {
	rows, err := c.db.Query(`SELECT o.content_digest FROM artwork_objects o LEFT JOIN artwork_refs r ON r.content_digest=o.content_digest WHERE r.content_digest IS NULL ORDER BY o.last_accessed_at ASC, o.content_digest ASC`)
	if err != nil {
		return newOpError(ErrStorage, nil)
	}
	var digests []string
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			_ = rows.Close()
			return newOpError(ErrStorage, nil)
		}
		digests = append(digests, digest)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return newOpError(ErrStorage, nil)
	}
	if err := rows.Close(); err != nil {
		return newOpError(ErrStorage, nil)
	}
	for _, digest := range digests {
		if err := c.removeArtworkDigest(digest); err != nil {
			return newOpError(ErrStorage, err)
		}
		if _, err := c.db.Exec(`DELETE FROM artwork_objects WHERE content_digest=? AND NOT EXISTS (SELECT 1 FROM artwork_refs WHERE content_digest=?)`, digest, digest); err != nil {
			return newOpError(ErrStorage, nil)
		}
	}
	return c.gcOrphanArtworkFiles()
}

func (c *Cache) evictUnderLock() error {
	if _, err := c.db.Exec(`DELETE FROM metadata_records WHERE outcome IN ('no_match','ambiguous') AND expires_at <= ? AND NOT EXISTS (SELECT 1 FROM settings WHERE key='pending_record' AND value=lower(hex(metadata_records.cache_key)))`, c.now().UnixNano()); err != nil {
		return newOpError(ErrStorage, nil)
	}
	if err := c.gcUnreferenced(); err != nil {
		return err
	}
	for {
		var count int
		if err := c.db.QueryRow(`SELECT COUNT(*) FROM metadata_records`).Scan(&count); err != nil {
			return newOpError(ErrStorage, nil)
		}
		if count <= maxMetadataRecords {
			break
		}
		if _, err := c.db.Exec(`DELETE FROM metadata_records WHERE cache_key=(SELECT cache_key FROM metadata_records WHERE NOT EXISTS (SELECT 1 FROM settings WHERE key='pending_record' AND value=lower(hex(metadata_records.cache_key))) ORDER BY last_accessed_at ASC, hex(cache_key) ASC LIMIT 1)`); err != nil {
			return newOpError(ErrStorage, nil)
		}
		if err := c.gcUnreferenced(); err != nil {
			return err
		}
	}
	limit := c.maxArtworkBytes
	if limit < 1 {
		limit = maxArtworkBytes
	}
	for {
		var bytes int64
		if err := c.db.QueryRow(`SELECT COALESCE(SUM(byte_count),0) FROM artwork_objects`).Scan(&bytes); err != nil {
			return newOpError(ErrStorage, nil)
		}
		if bytes <= limit {
			break
		}
		var key []byte
		err := c.db.QueryRow(`SELECT m.cache_key FROM metadata_records m WHERE EXISTS (SELECT 1 FROM artwork_refs r WHERE r.cache_key=m.cache_key) AND NOT EXISTS (SELECT 1 FROM settings WHERE key='pending_record' AND value=lower(hex(m.cache_key))) ORDER BY m.last_accessed_at ASC, hex(m.cache_key) ASC LIMIT 1`).Scan(&key)
		if errors.Is(err, sql.ErrNoRows) {
			return newOpError(ErrStorage, nil)
		}
		if err != nil {
			return newOpError(ErrStorage, nil)
		}
		if _, err := c.db.Exec(`DELETE FROM metadata_records WHERE cache_key=?`, key); err != nil {
			return newOpError(ErrStorage, nil)
		}
		if err := c.gcUnreferenced(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) reconcile() error {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	release, err := c.begin()
	if err != nil {
		return err
	}
	defer release()
	pending, err := c.pendingRecord()
	if err != nil {
		return err
	}
	if pending != "" {
		key, err := hex.DecodeString(pending)
		if err != nil || len(key) != 32 {
			return newOpError(ErrStorage, err)
		}
		tx, err := c.db.Begin()
		if err != nil {
			return newOpError(ErrStorage, nil)
		}
		if _, err := tx.Exec(`DELETE FROM metadata_records WHERE cache_key=?`, key); err != nil {
			_ = tx.Rollback()
			return newOpError(ErrStorage, nil)
		}
		if _, err := tx.Exec(`DELETE FROM settings WHERE key='pending_record' AND value=?`, pending); err != nil {
			_ = tx.Rollback()
			return newOpError(ErrStorage, nil)
		}
		if err := tx.Commit(); err != nil {
			return newOpError(ErrStorage, nil)
		}
	}
	return c.evictUnderLock()
}

func (c *Cache) gcOrphanArtworkFiles() error {
	entries, err := c.artworkRoot.readDir()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return newOpError(ErrStorage, nil)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".tmp-") {
			continue
		}
		if !digestPattern.MatchString(name) {
			return newOpError(ErrStorage, nil)
		}
		var present int
		if err := c.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM artwork_objects WHERE content_digest=?)`, name).Scan(&present); err != nil {
			return newOpError(ErrStorage, nil)
		}
		if present != 0 {
			continue
		}
		if err := c.removeArtworkDigest(name); err != nil {
			return newOpError(ErrStorage, err)
		}
	}
	return nil
}

func (c *Cache) removeArtworkDigest(digest string) error {
	if !digestPattern.MatchString(digest) {
		return errors.New("artwork digest is unsafe")
	}
	if err := c.artworkRoot.intact(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	info, err := c.artworkRoot.lstat(digest)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("artwork digest is not a regular file")
	}
	if err := c.artworkRoot.beforeUse(); err != nil {
		return err
	}
	if err := c.artworkRoot.remove(digest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (c *Cache) closeForInitializationFailure() error {
	return c.closeInternal(true)
}

func (c *Cache) Close() error {
	return c.closeInternal(false)
}

func (c *Cache) closeInternal(removeCreated bool) error {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return c.closeErr
	}
	c.closed = true
	dbErr := c.db.Close()
	c.sqliteVFS.markRetiring()
	vfsErr := unregisterRootedSQLiteVFS(c.sqliteVFS)
	if dbErr != nil || vfsErr != nil || rootedSQLiteVFSRegistered(c.sqliteVFS) {
		var database *sql.DB
		if dbErr != nil {
			database = c.db
		}
		record := newSQLiteRetirementWithSQL(c.owner, c.artworkRoot, c.rootLease, c.sqliteVFS, nil, database, removeCreated, removeCreated)
		c.retirement = record
		c.closeErr = newOpError(ErrStorage, nil)
		enqueueSQLiteRetirement(record)
		return c.closeErr
	}
	resourceErr := c.releaseResources(removeCreated)
	if cleanupErr := errors.Join(dbErr, vfsErr, resourceErr); cleanupErr != nil {
		c.closeErr = newOpError(ErrStorage, nil)
		return newOpError(ErrStorage, nil)
	}
	return nil
}

func (c *Cache) releaseResources(removeCreated bool) error {
	c.resourceOnce.Do(func() {
		var errs []error
		if c.artworkRoot != nil {
			if removeCreated {
				errs = append(errs, c.artworkRoot.removeCreated())
			}
			errs = append(errs, c.artworkRoot.close())
		}
		if c.owner != nil {
			errs = append(errs, c.owner.abort(removeCreated))
		} else if c.sqliteRoot != nil {
			errs = append(errs, c.sqliteRoot.Close())
		}
		if c.rootLease != nil {
			c.rootLease.release()
		}
		c.resourceErr = errors.Join(errs...)
	})
	return c.resourceErr
}

func (c *Cache) retirementRecord() *sqliteRetirement {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	return c.retirement
}

func (c *Cache) begin() (func(), error) {
	c.closeMu.RLock()
	if c.closed {
		c.closeMu.RUnlock()
		return nil, newOpError(ErrCanceled, context.Canceled)
	}
	return c.closeMu.RUnlock, nil
}

func (c *Cache) lockOperation(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if c.operationMu.TryLock() {
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

func validOutcome(outcome Outcome) bool {
	return outcome == OutcomeExact || outcome == OutcomeConfident || outcome == OutcomeAmbiguous || outcome == OutcomeNoMatch
}

func scopeDigest(scope string) string {
	digest := sha256.Sum256(append([]byte("igdb\x00"), []byte(scope)...))
	return hex.EncodeToString(digest[:])
}

func preflightSQLiteResidue(privateRoot *os.Root) error {
	for _, name := range sqliteDerivedLeaves {
		info, err := privateRoot.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("metadata database residue is unsafe")
		}
	}
	return nil
}

func removeLegacySQLiteSHM(privateRoot *os.Root) error {
	info, err := privateRoot.Lstat(sqliteSHMLeaf)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("metadata legacy SHM residue is unsafe")
	}
	return privateRoot.Remove(sqliteSHMLeaf)
}

func purgeDerivedFilesOwned(owner *privateRoot, artworkRoot *artworkDirectory) error {
	if owner == nil || owner.root == nil {
		return errors.New("metadata root is unavailable")
	}
	if err := owner.verifyInitChain(); err != nil {
		return err
	}
	if artworkRoot != nil {
		if err := artworkRoot.intact(); err != nil {
			return err
		}
	}
	if hook := purgeBeforeDeleteHook(); hook != nil {
		hook()
	}
	if err := owner.verifyInitChain(); err != nil {
		return err
	}
	if artworkRoot != nil {
		if err := artworkRoot.intact(); err != nil {
			return err
		}
	}
	if err := preflightSQLiteResidue(owner.root); err != nil {
		return err
	}
	if err := owner.verifyInitChain(); err != nil {
		return err
	}
	for _, name := range sqliteDerivedLeaves {
		if err := owner.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if artworkRoot == nil {
		return nil
	}
	entries, err := artworkRoot.readDir()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := artworkRoot.root.RemoveAll(entry.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func cleanupArtworkTemps(artwork *artworkDirectory) error {
	entries, err := artwork.readDir()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		info, err := artwork.lstat(entry.Name())
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("unsafe artwork temporary file")
		}
		if err := artwork.beforeUse(); err != nil {
			return err
		}
		if err := artwork.remove(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", value)
}
