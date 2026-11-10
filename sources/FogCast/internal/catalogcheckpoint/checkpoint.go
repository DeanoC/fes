// Package catalogcheckpoint saves and restores one stopped kit-host catalog.
// The software supervisor owns quiescence and operation/bundle identity. These
// functions must only run after every host process has been proved stopped.
package catalogcheckpoint

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const databaseName = "catalog.sqlite3"
const metadataName = "metadata.json"
const targetName = "restore-target.json"

// Metadata describes a standalone SQLite file, or the absence of a catalog.
type Metadata struct {
	Present bool   `json:"present"`
	Schema  int    `json:"schema"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

// Inspect checks integrity without applying application migrations, drains any
// committed WAL into the main file, closes SQLite, and hashes the result.
func Inspect(ctx context.Context, database string) (Metadata, error) {
	if err := absolutePath(database); err != nil {
		return Metadata{}, err
	}
	if err := regularFile(database); errors.Is(err, os.ErrNotExist) {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if _, err := os.Lstat(database + suffix); !errors.Is(err, os.ErrNotExist) {
				return Metadata{}, fmt.Errorf("catalog is absent but SQLite sidecar %s exists or is unreadable", suffix)
			}
		}
		return Metadata{}, nil
	} else if err != nil {
		return Metadata{}, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := regularFile(database + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Metadata{}, fmt.Errorf("invalid SQLite sidecar %s: %w", suffix, err)
		}
	}
	schema, err := normalize(ctx, database)
	if err != nil {
		return Metadata{}, err
	}
	metadata, err := fileMetadata(ctx, database)
	metadata.Schema = schema
	return metadata, err
}

func normalize(ctx context.Context, path string) (schema int, err error) {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	q := u.Query()
	q.Set("mode", "rw") // Never create a missing database or run migrations.
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return 0, err
	}
	db.SetMaxOpenConns(1)
	defer func() { err = errors.Join(err, db.Close()) }()
	connection, err := db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, connection.Close()) }()
	if _, err := connection.ExecContext(ctx, "PRAGMA busy_timeout = 1000"); err != nil {
		return 0, err
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schema); err != nil {
		return 0, err
	}
	rows, err := connection.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return 0, err
	}
	count := 0
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if result != "ok" {
			_ = rows.Close()
			return 0, fmt.Errorf("catalog integrity check: %s", result)
		}
		count++
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil || count != 1 {
		return 0, errors.Join(err, errors.New("catalog integrity check did not return one ok result"))
	}
	rows, err = connection.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return 0, err
	}
	if rows.Next() {
		_ = rows.Close()
		return 0, errors.New("catalog foreign key check failed")
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	var busy, frames, checked int
	if err := connection.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checked); err != nil {
		return 0, err
	}
	if busy != 0 || frames != checked {
		return 0, fmt.Errorf("catalog WAL is busy: busy=%d frames=%d checkpointed=%d", busy, frames, checked)
	}
	return schema, nil
}

func fileMetadata(ctx context.Context, path string) (Metadata, error) {
	if err := regularFile(path); err != nil {
		return Metadata{}, err
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if err == nil && (!info.Mode().IsRegular() || info.Size() != 0) {
			return Metadata{}, fmt.Errorf("catalog has a nonempty or invalid %s sidecar", suffix)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Metadata{}, err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return Metadata{}, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, contextReader{ctx, f})
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{Present: true, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

// Checkpoint publishes a new snapshot directory exclusively. An existing
// directory is never replaced, including an incomplete or foreign snapshot.
func Checkpoint(ctx context.Context, database, snapshot string) (Metadata, error) {
	metadata, err := Inspect(ctx, database)
	if err != nil {
		return Metadata{}, err
	}
	return metadata, save(ctx, database, snapshot, metadata, nil)
}

func save(ctx context.Context, database, snapshot string, metadata Metadata, target *Metadata) error {
	if err := absolutePath(snapshot); err != nil {
		return err
	}
	if _, err := os.Lstat(snapshot); !errors.Is(err, os.ErrNotExist) {
		return errors.New("catalog snapshot destination already exists or is unreadable")
	}
	parent := filepath.Dir(snapshot)
	if err := realDirectory(parent); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(parent, ".catalog-checkpoint-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	if metadata.Present {
		if err := copyFile(ctx, database, filepath.Join(temporary, databaseName), metadata); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(temporary, metadataName), metadata); err != nil {
		return err
	}
	if target != nil {
		if err := writeJSON(filepath.Join(temporary, targetName), target); err != nil {
			return err
		}
	}
	if err := syncDirectory(temporary); err != nil {
		return err
	}
	// The supervisor serializes all catalog operations. The second check also
	// refuses a foreign destination created while the copy was in progress.
	if _, err := os.Lstat(snapshot); !errors.Is(err, os.ErrNotExist) {
		return errors.New("catalog snapshot destination appeared during checkpoint")
	}
	if err := os.Rename(temporary, snapshot); err != nil {
		return err
	}
	return syncDirectory(parent)
}

// Load validates a snapshot's metadata, bytes, schema and SQLite integrity.
func Load(ctx context.Context, snapshot string) (Metadata, error) {
	if err := absolutePath(snapshot); err != nil {
		return Metadata{}, err
	}
	if err := realDirectory(snapshot); err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	if err := readJSON(filepath.Join(snapshot, metadataName), &metadata); err != nil {
		return Metadata{}, err
	}
	if !metadata.Present {
		if metadata != (Metadata{}) {
			return Metadata{}, errors.New("invalid absent catalog metadata")
		}
		if _, err := os.Lstat(filepath.Join(snapshot, databaseName)); !errors.Is(err, os.ErrNotExist) {
			return Metadata{}, errors.New("absent catalog snapshot contains database")
		}
		return metadata, nil
	}
	if metadata.Schema < 0 || metadata.Size <= 0 || len(metadata.SHA256) != 64 {
		return Metadata{}, errors.New("invalid catalog snapshot metadata")
	}
	if decoded, err := hex.DecodeString(metadata.SHA256); err != nil || len(decoded) != sha256.Size {
		return Metadata{}, errors.New("invalid catalog snapshot digest")
	}
	// Validate a private working copy. Opening the immutable snapshot itself
	// could change its journal header or sidecars and invalidate its digest.
	actual, err := fileMetadata(ctx, filepath.Join(snapshot, databaseName))
	actual.Schema = metadata.Schema
	if err != nil || actual != metadata {
		return Metadata{}, errors.Join(err, errors.New("catalog snapshot digest or size mismatch"))
	}
	temporary, err := os.MkdirTemp(filepath.Dir(snapshot), ".catalog-validate-*")
	if err != nil {
		return Metadata{}, err
	}
	defer os.RemoveAll(temporary)
	path := filepath.Join(temporary, databaseName)
	if err := copyFile(ctx, filepath.Join(snapshot, databaseName), path, metadata); err != nil {
		return Metadata{}, err
	}
	schema, err := normalize(ctx, path)
	if err != nil || schema != metadata.Schema {
		return Metadata{}, errors.Join(err, errors.New("catalog snapshot schema or integrity mismatch"))
	}
	return metadata, nil
}

// Restore retains the current catalog durably before replacing it. A retry
// accepts only the exact retained source or exact restored target; another
// catalog or a different target snapshot is never overwritten.
func Restore(ctx context.Context, database, snapshot, retain string, expectedCurrentSchema int) (Metadata, error) {
	return restore(ctx, database, snapshot, retain, expectedCurrentSchema, copyFile)
}

func restore(ctx context.Context, database, snapshot, retain string, expectedCurrentSchema int, copy func(context.Context, string, string, Metadata) error) (Metadata, error) {
	target, err := Load(ctx, snapshot)
	if err != nil {
		return Metadata{}, err
	}
	current, err := Inspect(ctx, database)
	if err != nil {
		return Metadata{}, err
	}
	if err := absolutePath(retain); err != nil {
		return Metadata{}, err
	}
	if _, err := os.Lstat(retain); errors.Is(err, os.ErrNotExist) {
		if current.Schema != expectedCurrentSchema {
			return Metadata{}, errors.New("current catalog schema differs from expected restore source")
		}
		if err := save(ctx, database, retain, current, &target); err != nil {
			return Metadata{}, err
		}
	} else if err != nil {
		return Metadata{}, err
	}
	source, err := Load(ctx, retain)
	if err != nil {
		return Metadata{}, err
	}
	var recordedTarget Metadata
	if err := readJSON(filepath.Join(retain, targetName), &recordedTarget); err != nil {
		return Metadata{}, err
	}
	if recordedTarget != target || source.Schema != expectedCurrentSchema {
		return Metadata{}, errors.New("retained catalog belongs to a different restore")
	}
	// A previous attempt may have renamed the retained directory successfully
	// and then failed its parent fsync. Re-prove its durable publication before
	// either replacing the live catalog or accepting an already-restored one.
	if err := syncDirectory(retain); err != nil {
		return Metadata{}, err
	}
	if err := syncDirectory(filepath.Dir(retain)); err != nil {
		return Metadata{}, err
	}
	if current == target {
		return target, syncDirectory(filepath.Dir(database))
	}
	if current != source {
		return Metadata{}, errors.New("live catalog differs from both retained source and restore target")
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if target.Present {
		temporary, err := os.CreateTemp(filepath.Dir(database), ".catalog-restore-*")
		if err != nil {
			return Metadata{}, err
		}
		name := temporary.Name()
		if err := temporary.Close(); err != nil {
			_ = os.Remove(name)
			return Metadata{}, err
		}
		defer os.Remove(name)
		// copyFile exclusively creates its destination.
		if err := os.Remove(name); err != nil {
			return Metadata{}, err
		}
		if err := copy(ctx, filepath.Join(snapshot, databaseName), name, target); err != nil {
			return Metadata{}, err
		}
		if err := ctx.Err(); err != nil {
			return Metadata{}, err
		}
		if err := clearEmptySidecars(database); err != nil {
			return Metadata{}, err
		}
		if err := os.Rename(name, database); err != nil {
			return Metadata{}, err
		}
	} else {
		if err := clearEmptySidecars(database); err != nil {
			return Metadata{}, err
		}
		if err := os.Remove(database); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Metadata{}, err
		}
	}
	return target, syncDirectory(filepath.Dir(database))
}

func clearEmptySidecars(database string) error {
	for _, suffix := range []string{"-wal", "-journal", "-shm"} {
		info, err := os.Lstat(database + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || (suffix != "-shm" && info.Size() != 0) {
			return fmt.Errorf("refusing to remove nonempty or invalid SQLite sidecar %s", suffix)
		}
		if err := os.Remove(database + suffix); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(ctx context.Context, source, destination string, metadata Metadata) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(output, hash), contextReader{ctx, input})
	if copyErr == nil && (size != metadata.Size || hex.EncodeToString(hash.Sum(nil)) != metadata.SHA256) {
		copyErr = errors.New("catalog changed while copying")
	}
	if copyErr == nil {
		copyErr = output.Sync()
	}
	return errors.Join(copyErr, output.Close())
}

func absolutePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == filepath.Dir(path) {
		return errors.New("catalog path must be clean and absolute, below the filesystem root")
	}
	return nil
}

func regularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("catalog path is not a regular file")
	}
	return nil
}

func realDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("catalog snapshot parent is not a real directory")
	}
	return nil
}

func writeJSON(path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(body, '\n'))
	if writeErr == nil {
		writeErr = f.Sync()
	}
	return errors.Join(writeErr, f.Close())
}

func readJSON(path string, value any) error {
	if err := regularFile(path); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("catalog metadata contains trailing data")
	}
	return nil
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
