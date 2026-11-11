package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DeanoC/FogCast/protocol"
)

// RemoteCacheObject is a verified remote-only object's content identity and size.
type RemoteCacheObject struct {
	MediaID string `json:"media_id"`
	Size    int64  `json:"size"`
}

// RemoteCacheUsage reports logical content bytes, not filesystem free space.
// Old objects with unknown provenance are intentionally excluded.
type RemoteCacheUsage struct {
	TrackedObjects     int64               `json:"tracked_objects"`
	TrackedBytes       int64               `json:"tracked_bytes"`
	ReclaimableObjects int64               `json:"reclaimable_objects"`
	ReclaimableBytes   int64               `json:"reclaimable_bytes"`
	Candidates         []RemoteCacheObject `json:"candidates"`
	NextAfter          string              `json:"next_after,omitempty"`
}

// A remote origin remains a reference even if the source is removed or the
// pending selection was consumed. Protect retained SAVE rows explicitly:
// their FK cascades on deletion and is not a protection against data loss.
const remoteCacheUnreferenced = `
 NOT EXISTS (SELECT 1 FROM core_entries e WHERE e.media_id = m.media_id)
 AND NOT EXISTS (SELECT 1 FROM remote_core_entries r WHERE r.media_id = m.media_id)
 AND NOT EXISTS (SELECT 1 FROM core_entry_roms r WHERE r.media_id = m.media_id)
 AND NOT EXISTS (SELECT 1 FROM core_expansions e WHERE e.media_id = m.media_id)
 AND NOT EXISTS (SELECT 1 FROM core_video_parts p WHERE p.media_id = m.media_id)
 AND NOT EXISTS (SELECT 1 FROM zx81_saves z WHERE z.media_id = m.media_id)`

// 200 digests fit within the host API's existing 16 KiB JSON body limit.
const RemoteCachePageLimit = 200

// RemoteCassetteCache returns one consistent, read-only preview. after is the
// last digest in the previous page; each returned page is a new snapshot.
func (s *Store) RemoteCassetteCache(ctx context.Context, after string, limit int) (usage RemoteCacheUsage, err error) {
	usage.Candidates = []RemoteCacheObject{}
	if (after != "" && protocol.ValidateDigest(after) != nil) || limit < 1 || limit > RemoteCachePageLimit {
		return usage, ErrInvalidCoreMedia
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return usage, err
	}
	defer conn.Close()
	// Use a deferred read transaction rather than Store's immediate-writer
	// default so inspection neither reserves the writer nor changes content.
	if _, err = conn.ExecContext(ctx, "BEGIN"); err != nil {
		return usage, err
	}
	defer func() {
		if err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	const base = ` FROM core_media m JOIN remote_cached_media c ON c.media_id = m.media_id `
	if err = conn.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(m.size),0)`+base).Scan(&usage.TrackedObjects, &usage.TrackedBytes); err != nil {
		return usage, err
	}
	if err = conn.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(m.size),0)`+base+` WHERE `+remoteCacheUnreferenced).Scan(&usage.ReclaimableObjects, &usage.ReclaimableBytes); err != nil {
		return usage, err
	}
	rows, err := conn.QueryContext(ctx, `SELECT m.media_id, m.size`+base+` WHERE m.media_id > ? AND `+remoteCacheUnreferenced+` ORDER BY m.media_id LIMIT ?`, after, limit+1)
	if err != nil {
		return usage, err
	}
	for rows.Next() {
		var item RemoteCacheObject
		if err = rows.Scan(&item.MediaID, &item.Size); err != nil {
			break
		}
		usage.Candidates = append(usage.Candidates, item)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return usage, err
	}
	if len(usage.Candidates) > limit {
		usage.Candidates = usage.Candidates[:limit]
		usage.NextAfter = usage.Candidates[limit-1].MediaID
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return usage, err
}

// RemoteCacheCleanup records only objects actually deleted after rechecking
// every reference under the catalog's immediate writer transaction.
type RemoteCacheCleanup struct {
	Deleted      []RemoteCacheObject `json:"deleted"`
	Skipped      []string            `json:"skipped"`
	LogicalBytes int64               `json:"logical_bytes"`
}

// RemoveRemoteCassetteCache removes only explicitly requested, still eligible
// objects. SQLite may reuse freed pages; this does not compact the database or
// promise a corresponding increase in filesystem free space.
func (s *Store) RemoveRemoteCassetteCache(ctx context.Context, ids []string) (result RemoteCacheCleanup, err error) {
	result.Deleted, result.Skipped = []RemoteCacheObject{}, []string{}
	defer func() {
		if err != nil {
			result = RemoteCacheCleanup{}
		}
	}()
	if len(ids) == 0 || len(ids) > RemoteCachePageLimit {
		return result, ErrInvalidCoreMedia
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if protocol.ValidateDigest(id) != nil || seen[id] {
			return result, ErrInvalidCoreMedia
		}
		seen[id] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var size int64
		query := `SELECT m.size FROM core_media m JOIN remote_cached_media c ON c.media_id = m.media_id WHERE m.media_id = ? AND ` + remoteCacheUnreferenced
		err = tx.QueryRowContext(ctx, query, id).Scan(&size)
		if errors.Is(err, sql.ErrNoRows) {
			result.Skipped = append(result.Skipped, id)
			err = nil
			continue
		}
		if err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM core_media WHERE media_id = ?`, id); err != nil {
			return result, err
		}
		result.Deleted = append(result.Deleted, RemoteCacheObject{MediaID: id, Size: size})
		result.LogicalBytes += size
	}
	if err = tx.Commit(); err != nil {
		return RemoteCacheCleanup{}, fmt.Errorf("commit remote cache cleanup: %w", err)
	}
	return result, nil
}
