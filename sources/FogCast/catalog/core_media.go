package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	MaxCoreMediaBytes    = protocol.MaxContentBytes
	CoreMediaChunkBytes  = 64 << 10
	legacyCoreMediaBytes = 16 << 10
)

var (
	ErrInvalidCoreMedia  = errors.New("catalog core media is invalid")
	ErrCoreMediaNotFound = errors.New("catalog core media not found")
)

// CoreMedia describes immutable, content-addressed bytes. It exposes no path.
type CoreMedia struct {
	MediaID string `json:"media_id"`
	Size    int64  `json:"size"`
}

// coreMediaIdentity is used only for the immutable schema 7 seed.
func coreMediaIdentity(data []byte) (CoreMedia, error) {
	if len(data) < 1 || len(data) > legacyCoreMediaBytes {
		return CoreMedia{}, ErrInvalidCoreMedia
	}
	return CoreMedia{MediaID: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data))}, nil
}

// ImportCoreMedia is the allocating compatibility helper.
func (s *Store) ImportCoreMedia(ctx context.Context, data []byte) (CoreMedia, bool, error) {
	if len(data) < 1 || int64(len(data)) > MaxCoreMediaBytes {
		return CoreMedia{}, false, ErrInvalidCoreMedia
	}
	return s.ImportCoreMediaStream(ctx, int64(len(data)), bytes.NewReader(bytes.Clone(data)))
}

type coreMediaSnapshot struct {
	file     *os.File
	once     sync.Once
	closeErr error
}

func newCoreMediaSnapshot() (*coreMediaSnapshot, error) {
	file, err := os.CreateTemp("", "fogcast-core-media-*")
	if err != nil {
		return nil, err
	}
	return &coreMediaSnapshot{file: file}, nil
}
func (s *coreMediaSnapshot) Read(p []byte) (int, error) { return s.file.Read(p) }
func (s *coreMediaSnapshot) Close() error {
	s.once.Do(func() { s.closeErr = errors.Join(s.file.Close(), os.Remove(s.file.Name())) })
	return s.closeErr
}

type coreMediaContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r coreMediaContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

// ImportCoreMediaStream snapshots and hashes the exact body before reserving a
// database writer. Memory usage is bounded by the chunk size.
func (s *Store) ImportCoreMediaStream(ctx context.Context, size int64, body io.Reader) (resultMedia CoreMedia, created bool, err error) {
	if size < 1 || size > MaxCoreMediaBytes || body == nil {
		return CoreMedia{}, false, ErrInvalidCoreMedia
	}
	if err := ctx.Err(); err != nil {
		return CoreMedia{}, false, err
	}
	snapshot, err := newCoreMediaSnapshot()
	if err != nil {
		return CoreMedia{}, false, err
	}
	defer func() { err = errors.Join(err, snapshot.Close()) }()
	hash := sha256.New()
	buffer := make([]byte, CoreMediaChunkBytes)
	n, err := io.CopyBuffer(io.MultiWriter(snapshot.file, hash), io.LimitReader(coreMediaContextReader{ctx, body}, size+1), buffer)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CoreMedia{}, false, err
		}
		return CoreMedia{}, false, errors.Join(ErrInvalidCoreMedia, err)
	}
	if n != size {
		return CoreMedia{}, false, fmt.Errorf("%w: body length differs from declared size", ErrInvalidCoreMedia)
	}
	if err := ctx.Err(); err != nil {
		return CoreMedia{}, false, err
	}
	media := CoreMedia{MediaID: fmt.Sprintf("%x", hash.Sum(nil)), Size: size}
	if _, err := snapshot.file.Seek(0, io.SeekStart); err != nil {
		return CoreMedia{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreMedia{}, false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO core_media(media_id, size, data) VALUES (?, ?, X'') ON CONFLICT(media_id) DO NOTHING`, media.MediaID, size)
	if err != nil {
		return CoreMedia{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return CoreMedia{}, false, err
	}
	if changed == 0 {
		if _, err := verifyCoreMedia(ctx, tx, media.MediaID, io.Discard); err != nil {
			return CoreMedia{}, false, err
		}
	} else {
		for index, remaining := 0, size; remaining > 0; index++ {
			if err := ctx.Err(); err != nil {
				return CoreMedia{}, false, err
			}
			count := min(int64(len(buffer)), remaining)
			if _, err := io.ReadFull(snapshot.file, buffer[:count]); err != nil {
				return CoreMedia{}, false, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO core_media_chunks(media_id, chunk_index, data) VALUES (?, ?, ?)`, media.MediaID, index, buffer[:count]); err != nil {
				return CoreMedia{}, false, err
			}
			remaining -= count
		}
	}
	if err := tx.Commit(); err != nil {
		return CoreMedia{}, false, err
	}
	return media, changed == 1, nil
}

type coreMediaQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// verifyCoreMedia writes only to private sinks. Callers must hold a transaction.
// Every payload expression is bounded in SQL before the driver materializes it.
func verifyCoreMedia(ctx context.Context, q coreMediaQuerier, id string, dst io.Writer) (CoreMedia, error) {
	if protocol.ValidateDigest(id) != nil {
		return CoreMedia{}, ErrInvalidCoreMedia
	}
	var size, legacySize int64
	var legacy []byte
	err := q.QueryRowContext(ctx, `SELECT size,
  CASE WHEN typeof(data) = 'blob' THEN length(data) ELSE -1 END,
  CASE WHEN typeof(data) = 'blob' AND length(data) BETWEEN 1 AND ? AND length(data) = size THEN data ELSE NULL END
  FROM core_media WHERE media_id = ?`, legacyCoreMediaBytes, id).Scan(&size, &legacySize, &legacy)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreMedia{}, ErrCoreMediaNotFound
	}
	if err != nil {
		return CoreMedia{}, err
	}
	if size < 1 || size > MaxCoreMediaBytes || legacySize < 0 || legacySize > legacyCoreMediaBytes {
		return CoreMedia{}, ErrInvalidCoreMedia
	}
	hash := sha256.New()
	sink := io.MultiWriter(dst, hash)
	var total int64
	if legacySize > 0 {
		if int64(len(legacy)) != size {
			return CoreMedia{}, ErrInvalidCoreMedia
		}
		if _, err := sink.Write(legacy); err != nil {
			return CoreMedia{}, err
		}
		total = int64(len(legacy))
	}
	rows, err := q.QueryContext(ctx, `SELECT chunk_index,
  CASE WHEN typeof(data) = 'blob' AND length(data) BETWEEN 1 AND ? THEN data ELSE NULL END
  FROM core_media_chunks WHERE media_id = ? ORDER BY chunk_index`, CoreMediaChunkBytes, id)
	if err != nil {
		return CoreMedia{}, err
	}
	defer rows.Close()
	var index int64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return CoreMedia{}, err
		}
		var actual int64
		var data []byte
		if err := rows.Scan(&actual, &data); err != nil {
			return CoreMedia{}, err
		}
		expected := min(int64(CoreMediaChunkBytes), size-total)
		if legacySize != 0 || actual != index || len(data) == 0 || int64(len(data)) != expected {
			return CoreMedia{}, ErrInvalidCoreMedia
		}
		if _, err := sink.Write(data); err != nil {
			return CoreMedia{}, err
		}
		total += int64(len(data))
		index++
	}
	if err := rows.Err(); err != nil {
		return CoreMedia{}, err
	}
	if err := ctx.Err(); err != nil {
		return CoreMedia{}, err
	}
	if total != size || fmt.Sprintf("%x", hash.Sum(nil)) != id {
		return CoreMedia{}, ErrInvalidCoreMedia
	}
	return CoreMedia{MediaID: id, Size: size}, nil
}

// readCoreMediaSnapshot uses explicit deferred BEGIN: the Store's default
// BeginTx reserves the writer, even for read-only TxOptions with this driver.
func (s *Store) readCoreMediaSnapshot(ctx context.Context, id string, dst io.Writer) (CoreMedia, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return CoreMedia{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return CoreMedia{}, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	media, err := verifyCoreMedia(ctx, conn, id, dst)
	if err != nil {
		return CoreMedia{}, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return CoreMedia{}, err
	}
	return media, nil
}

// CoreMediaInfo verifies all stored bytes without retaining them.
func (s *Store) CoreMediaInfo(ctx context.Context, id string) (CoreMedia, error) {
	return s.readCoreMediaSnapshot(ctx, id, io.Discard)
}

// OpenCoreMedia returns a verified private snapshot positioned at byte zero.
// Close releases the file and removes it. No database cursor escapes.
func (s *Store) OpenCoreMedia(ctx context.Context, id string) (CoreMedia, io.ReadCloser, error) {
	if protocol.ValidateDigest(id) != nil {
		return CoreMedia{}, nil, ErrInvalidCoreMedia
	}
	if err := ctx.Err(); err != nil {
		return CoreMedia{}, nil, err
	}
	snapshot, err := newCoreMediaSnapshot()
	if err != nil {
		return CoreMedia{}, nil, err
	}
	media, err := s.readCoreMediaSnapshot(ctx, id, snapshot.file)
	if err == nil {
		_, err = snapshot.file.Seek(0, io.SeekStart)
	}
	if err != nil {
		return CoreMedia{}, nil, errors.Join(err, snapshot.Close())
	}
	return media, snapshot, nil
}

// CoreMedia is the allocating compatibility helper.
func (s *Store) CoreMedia(ctx context.Context, id string) (CoreMedia, []byte, error) {
	media, reader, err := s.OpenCoreMedia(ctx, id)
	if err != nil {
		return CoreMedia{}, nil, err
	}
	data, readErr := io.ReadAll(reader)
	if err := errors.Join(readErr, reader.Close()); err != nil {
		return CoreMedia{}, nil, err
	}
	return media, data, nil
}

func validateCoreMediaSelection(role, id string) error {
	if role == "" && id == "" {
		return nil
	}
	if role != "blob" || protocol.ValidateDigest(id) != nil {
		return fmt.Errorf("%w: expected blob role and media digest, or both empty", ErrInvalidCoreMedia)
	}
	return nil
}

// SelectCoreEntryMedia changes only media, conditional on the package and media
// IDs observed by the caller. Empty role and ID clear the selection.
func (s *Store) SelectCoreEntryMedia(ctx context.Context, gameID, expectedPackageID, expectedMediaID, mediaRole, mediaID string) (CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(expectedPackageID) != nil {
		return CoreEntry{}, fmt.Errorf("%w: invalid selection identity", ErrInvalidCoreEntry)
	}
	if expectedMediaID != "" && protocol.ValidateDigest(expectedMediaID) != nil {
		return CoreEntry{}, fmt.Errorf("%w: invalid expected media ID", ErrInvalidCoreMedia)
	}
	if err := validateCoreMediaSelection(mediaRole, mediaID); err != nil {
		return CoreEntry{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("begin core entry media selection: %w", err)
	}
	defer tx.Rollback()
	var entry CoreEntry
	err = tx.QueryRowContext(ctx, `
  SELECT e.game_id, g.title, e.core_id, e.package_id, e.media_role, e.media_id
  FROM core_entries AS e JOIN games AS g ON g.game_id = e.game_id WHERE e.game_id = ?`, gameID).
		Scan(&entry.GameID, &entry.Title, &entry.CoreID, &entry.PackageID, &entry.MediaRole, &entry.MediaID)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreEntry{}, ErrCoreEntryNotFound
	}
	if err != nil {
		return CoreEntry{}, fmt.Errorf("read core entry media selection: %w", err)
	}
	if entry.PackageID != expectedPackageID || entry.MediaID != expectedMediaID {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	if mediaID != "" {
		if _, err := verifyCoreMedia(ctx, tx, mediaID, io.Discard); err != nil {
			return CoreEntry{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE core_entries SET media_role = ?, media_id = ? WHERE game_id = ? AND package_id = ? AND media_id = ?`,
		mediaRole, mediaID, gameID, expectedPackageID, expectedMediaID)
	if err != nil {
		return CoreEntry{}, fmt.Errorf("update core entry media selection: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return CoreEntry{}, fmt.Errorf("count core entry media selection: %w", err)
	}
	if changed != 1 {
		return CoreEntry{}, ErrCoreEntryConflict
	}
	if err := tx.Commit(); err != nil {
		return CoreEntry{}, fmt.Errorf("commit core entry media selection: %w", err)
	}
	entry.MediaRole, entry.MediaID = mediaRole, mediaID
	return entry, nil
}
