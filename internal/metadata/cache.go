package metadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
