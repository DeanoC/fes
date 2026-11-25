package metadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	CacheSchemaVersion     = 1
	ProviderAdapterVersion = 2
	PlatformMapVersion     = 1
	maxMetadataRecords     = 10_000
	maxArtworkBytes        = int64(512 << 20)
	positiveMetadataTTL    = 30 * 24 * time.Hour
	negativeMetadataTTL    = 24 * time.Hour
)

type CacheConfig struct {
	Root            string
	CredentialScope string
	Now             func() time.Time
	// maxArtworkBytes is intentionally unexported: tests can inject a small
	// retention limit without making the production policy configurable.
	maxArtworkBytes int64
}

type ArtworkCacheEntry struct {
	Handle          string
	Role            ArtworkRole
	ProviderImageID string
	Transform       string
	ContentDigest   string
	MIME            string
	ByteCount       int64
	Width           int
	Height          int
}

type ArtworkLookup struct {
	Handle        string
	ContentDigest string
	MIME          string
	ByteCount     int64
	Width         int
	Height        int
	ExpiresAt     time.Time
}

type cacheState uint8

const (
	cacheMiss cacheState = iota
	cacheFresh
	cacheExpiredPositive
)

type cacheEntry struct {
	result      Result
	metadata    CacheRecordMetadata
	retrievedAt time.Time
	expiresAt   time.Time
	artwork     []ArtworkCacheEntry
}

type CacheRecordMetadata struct {
	Provider          ProviderName
	PlatformID        string
	NormalizedTitle   string
	Region            string
	ProviderGameID    string
	MatchScore        int
	ProviderUpdatedAt time.Time
	ProviderChecksum  string
}

type Cache struct {
	db              *sql.DB
	root            string
	artwork         string
	owner           *privateRoot
	sqliteRoot      *os.Root
	sqliteVFS       *rootedSQLiteVFS
	sqliteVFSName   string
	rootLease       *cacheRootLease
	artworkRoot     *artworkDirectory
	now             func() time.Time
	maxArtworkBytes int64
	operationMu     sync.Mutex
	closeMu         sync.RWMutex
	closed          bool
	closeErr        error
	retirement      *sqliteRetirement
	resourceOnce    sync.Once
	resourceErr     error
}

func BuildCacheKey(provider ProviderName, platformID, normalizedTitle, region string) ([]byte, error) {
	if provider == "" || strings.TrimSpace(platformID) == "" || strings.TrimSpace(normalizedTitle) == "" {
		return nil, newOpError(ErrPolicyBlocked, nil)
	}
	parts := []string{
		string(provider),
		strconvItoa(ProviderAdapterVersion),
		strconvItoa(CacheSchemaVersion),
		strconvItoa(NormalizerVersion),
		strconvItoa(PlatformMapVersion),
		platformID,
		normalizedTitle,
		region,
	}
	var encoded []byte
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		encoded = append(encoded, length[:]...)
		encoded = append(encoded, part...)
	}
	digest := sha256.Sum256(encoded)
	return digest[:], nil
}

func OpenCache(ctx context.Context, config CacheConfig) (*Cache, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	artworkLimit := maxArtworkBytes
	if config.maxArtworkBytes != 0 {
		if config.maxArtworkBytes < 1 {
			return nil, newOpError(ErrPolicyBlocked, nil)
		}
		artworkLimit = config.maxArtworkBytes
	}
	owner, err := acquirePrivateRoot(config.Root, true)
	if err != nil {
		return nil, newOpError(ErrStorage, err)
	}
	lease, err := acquireCacheRootLease(owner.info)
	if err != nil {
		_ = owner.abort(true)
		return nil, newOpError(ErrStorage, err)
	}
	artworkRoot, err := openArtworkDirectory(owner, true)
	if err != nil {
		_ = owner.abort(true)
		lease.release()
		return nil, newOpError(ErrStorage, err)
	}
	releaseOpenResources := func(removeCreated bool) {
		if removeCreated {
			_ = artworkRoot.removeCreated()
		}
		_ = artworkRoot.close()
		_ = owner.abort(removeCreated)
		lease.release()
	}
	if err := preflightSQLiteResidue(owner.root); err != nil {
		releaseOpenResources(true)
		return nil, newOpError(ErrStorage, err)
	}
	if err := removeLegacySQLiteSHM(owner.root); err != nil {
		releaseOpenResources(true)
		return nil, newOpError(ErrStorage, err)
	}
	cache, err := openCacheOnce(ctx, owner, artworkRoot, owner.path, filepath.Join(owner.path, "artwork"), lease, config.CredentialScope, now, artworkLimit)
	var retainedErr *rootedSQLiteVFSRetainedError
	if errors.As(err, &retainedErr) {
		enqueueSQLiteRetirement(newSQLiteRetirementWithSQL(owner, artworkRoot, lease, retainedErr.vfs, retainedErr.conn, retainedErr.db, true, true))
		return nil, newOpError(ErrStorage, nil)
	}
	if err == nil {
		if err := cache.reconcile(); err != nil {
			if closeErr := cache.closeForInitializationFailure(); closeErr != nil {
				return nil, closeErr
			}
			return nil, err
		}
		if err := owner.commit(); err != nil {
			if closeErr := cache.closeForInitializationFailure(); closeErr != nil {
				return nil, closeErr
			}
			return nil, newOpError(ErrStorage, nil)
		}
		return cache, nil
	}
	// All cache content is derived. A corrupt/incompatible database is safely
	// disposable, but a symlink or unsafe root is never followed or removed.
	if purgeErr := purgeDerivedFilesOwned(owner, artworkRoot); purgeErr != nil {
		releaseOpenResources(true)
		return nil, newOpError(ErrStorage, nil)
	}
	cache, retryErr := openCacheOnce(ctx, owner, artworkRoot, owner.path, filepath.Join(owner.path, "artwork"), lease, config.CredentialScope, now, artworkLimit)
	if errors.As(retryErr, &retainedErr) {
		enqueueSQLiteRetirement(newSQLiteRetirementWithSQL(owner, artworkRoot, lease, retainedErr.vfs, retainedErr.conn, retainedErr.db, true, true))
		return nil, newOpError(ErrStorage, nil)
	}
	if retryErr != nil {
		releaseOpenResources(true)
		return nil, newOpError(ErrStorage, retryErr)
	}
	if reconcileErr := cache.reconcile(); reconcileErr != nil {
		if closeErr := cache.closeForInitializationFailure(); closeErr != nil {
			return nil, closeErr
		}
		return nil, reconcileErr
	}
	if err := owner.commit(); err != nil {
		if closeErr := cache.closeForInitializationFailure(); closeErr != nil {
			return nil, closeErr
		}
		return nil, newOpError(ErrStorage, nil)
	}
	return cache, nil
}

func openCacheOnce(ctx context.Context, owner *privateRoot, artworkRoot *artworkDirectory, root, artwork string, lease *cacheRootLease, credentialScope string, now func() time.Time, artworkLimit int64) (*Cache, error) {
	privateRoot := owner.root
	sqliteVFS := newRootedSQLiteVFS(privateRoot)
	if err := registerRootedSQLiteVFS(sqliteVFS); err != nil {
		return nil, err
	}
	registered := true
	unregister := func() error {
		if !registered {
			return nil
		}
		err := unregisterRootedSQLiteVFS(sqliteVFS)
		if err == nil {
			registered = false
		}
		return err
	}
	dsn := "file:" + sqliteMainLeaf + "?vfs=" + url.QueryEscape(sqliteVFS.logicalPrefix)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		if unregisterErr := unregister(); unregisterErr != nil {
			return nil, &rootedSQLiteVFSRetainedError{vfs: sqliteVFS, cause: errors.Join(err, unregisterErr)}
		}
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var connection *sql.Conn
	closeDB := func(cause error) (*Cache, error) {
		var closeErrs []error
		if connection != nil {
			if err := connection.Close(); err != nil {
				closeErrs = append(closeErrs, err)
			} else {
				connection = nil
			}
		}
		if db != nil {
			if err := db.Close(); err != nil {
				closeErrs = append(closeErrs, err)
			} else {
				db = nil
			}
		}
		sqliteVFS.markRetiring()
		if err := unregister(); err != nil {
			if rootedSQLiteVFSRegistered(sqliteVFS) {
				return nil, &rootedSQLiteVFSRetainedError{vfs: sqliteVFS, conn: connection, db: db, cause: errors.Join(cause, err, errors.Join(closeErrs...))}
			}
			closeErrs = append(closeErrs, err)
		}
		cleanupErr := errors.Join(closeErrs...)
		if cleanupErr != nil || connection != nil || db != nil {
			return nil, &rootedSQLiteVFSRetainedError{vfs: sqliteVFS, conn: connection, db: db, cause: errors.Join(cause, cleanupErr)}
		}
		return nil, cause
	}
	connection, err = db.Conn(ctx)
	if err != nil {
		return closeDB(err)
	}
	for _, statement := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000", "PRAGMA temp_store = MEMORY"} {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			return closeDB(err)
		}
	}
	var journalMode string
	if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil || !strings.EqualFold(journalMode, "wal") {
		return closeDB(errors.New("metadata WAL mode unavailable"))
	}
	var version int
	if err := connection.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version > CacheSchemaVersion {
		return closeDB(errors.New("metadata schema version is unsupported"))
	}
	if version == 0 {
		if _, err := connection.ExecContext(ctx, cacheSchemaV1); err != nil {
			return closeDB(err)
		}
	}
	scope := scopeDigest(credentialScope)
	settings := map[string]string{
		"schema_version":       strconvItoa(CacheSchemaVersion),
		"adapter_version":      strconvItoa(ProviderAdapterVersion),
		"normalizer_version":   strconvItoa(NormalizerVersion),
		"platform_map_version": strconvItoa(PlatformMapVersion),
		"credential_scope":     scope,
	}
	for key, expected := range settings {
		var actual string
		err := connection.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&actual)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := connection.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES(?,?)", key, expected); err != nil {
				return closeDB(err)
			}
			continue
		}
		if err != nil || actual != expected {
			return closeDB(errors.New("metadata settings mismatch"))
		}
	}
	if err := cleanupArtworkTemps(artworkRoot); err != nil {
		return closeDB(err)
	}
	if err := connection.Close(); err != nil {
		return closeDB(err)
	}
	connection = nil
	return &Cache{db: db, root: root, artwork: artwork, owner: owner, sqliteRoot: privateRoot, sqliteVFS: sqliteVFS, sqliteVFSName: sqliteVFS.logicalPrefix, rootLease: lease, artworkRoot: artworkRoot, now: now, maxArtworkBytes: artworkLimit}, nil
}

const cacheSchemaV1 = `
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS metadata_records (
  cache_key BLOB PRIMARY KEY CHECK(length(cache_key)=32),
  provider TEXT NOT NULL,
  adapter_version INTEGER NOT NULL,
  normalizer_version INTEGER NOT NULL,
  platform_map_version INTEGER NOT NULL,
  platform_id TEXT NOT NULL,
  normalized_title TEXT NOT NULL,
  region TEXT NOT NULL,
  outcome TEXT NOT NULL CHECK(outcome IN ('exact','confident','ambiguous','no_match')),
  provider_game_id TEXT,
  match_score INTEGER,
  summary TEXT, year TEXT, genre TEXT, studio TEXT, players TEXT,
  cover_handle TEXT, backdrop_handle TEXT,
  provider_updated_at INTEGER,
  provider_checksum TEXT,
  retrieved_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  last_accessed_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS artwork_refs (
  handle TEXT PRIMARY KEY,
  cache_key BLOB NOT NULL REFERENCES metadata_records(cache_key) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  role TEXT NOT NULL CHECK(role IN ('cover','backdrop')),
  provider_image_id TEXT NOT NULL,
  transform TEXT NOT NULL,
  content_digest TEXT,
  UNIQUE(cache_key, role)
);
CREATE TABLE IF NOT EXISTS artwork_objects (
  content_digest TEXT PRIMARY KEY,
  mime TEXT NOT NULL CHECK(mime IN ('image/jpeg','image/png')),
  byte_count INTEGER NOT NULL,
  width INTEGER NOT NULL,
  height INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  last_accessed_at INTEGER NOT NULL
);
PRAGMA user_version = 1;
`

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
