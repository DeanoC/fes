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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Generates distinguishable chunks without allocating a complete media object.
type patternMediaReader struct {
	remaining, offset int64
	maxRead           int
}

func (r *patternMediaReader) Read(p []byte) (int, error) {
	if len(p) > CoreMediaChunkBytes {
		return 0, fmt.Errorf("unbounded read: %d", len(p))
	}
	r.maxRead = max(r.maxRead, len(p))
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), r.remaining))
	for i := range p[:n] {
		p[i] = byte((r.offset+int64(i))/CoreMediaChunkBytes + (r.offset+int64(i))%251)
	}
	r.remaining -= int64(n)
	r.offset += int64(n)
	return n, nil
}

func assertMediaTemps(t *testing.T, dir string, count int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != count {
		t.Fatalf("temporary files = %v, %v; want %d", entries, err, count)
	}
}
func mediaDigest(t *testing.T, size int64) string {
	t.Helper()
	hash := sha256.New()
	if _, err := io.CopyBuffer(hash, &patternMediaReader{remaining: size}, make([]byte, CoreMediaChunkBytes)); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func TestCoreMediaStreamPolicyAndRestart(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s, path := mediaTestStore(t)
	if MaxCoreMediaBytes != 32<<20 || CoreMediaChunkBytes != 64<<10 {
		t.Fatal("storage policy changed")
	}
	var media []CoreMedia
	for _, size := range []int64{1, 16385, CoreMediaChunkBytes - 1, CoreMediaChunkBytes, CoreMediaChunkBytes + 1, MaxCoreMediaBytes} {
		input := &patternMediaReader{remaining: size}
		got, added, err := s.ImportCoreMediaStream(ctx, size, input)
		if err != nil || !added || got.Size != size || got.MediaID != mediaDigest(t, size) {
			t.Fatalf("size %d import: %+v %v %v", size, got, added, err)
		}
		media = append(media, got)
		assertMediaTemps(t, temp, 0)
		var inline, count, largest int
		if err := s.db.QueryRowContext(ctx, "SELECT length(data) FROM core_media WHERE media_id = ?", got.MediaID).Scan(&inline); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRowContext(ctx, "SELECT count(*), max(length(data)) FROM core_media_chunks WHERE media_id = ?", got.MediaID).Scan(&count, &largest); err != nil {
			t.Fatal(err)
		}
		if inline != 0 || count != int((size+CoreMediaChunkBytes-1)/CoreMediaChunkBytes) || largest > CoreMediaChunkBytes {
			t.Fatalf("storage inline=%d chunks=%d largest=%d", inline, count, largest)
		}
	}
	for _, size := range []int64{-1, 0, MaxCoreMediaBytes + 1} {
		input := &patternMediaReader{remaining: 1}
		if _, _, err := s.ImportCoreMediaStream(ctx, size, input); !errors.Is(err, ErrInvalidCoreMedia) || input.offset != 0 {
			t.Fatalf("invalid size %d: %v", size, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, want := range media {
		got, err := s.CoreMediaInfo(ctx, want.MediaID)
		if err != nil || got != want {
			t.Fatalf("info: %+v %v", got, err)
		}
		assertMediaTemps(t, temp, 0)
		got, reader, err := s.OpenCoreMedia(ctx, want.MediaID)
		if err != nil || got != want {
			t.Fatalf("open: %+v %v", got, err)
		}
		snapshot := reader.(*coreMediaSnapshot)
		stat, err := snapshot.file.Stat()
		if err != nil || stat.Mode().Perm() != 0600 {
			t.Fatalf("snapshot permissions: %v %v", stat, err)
		}
		assertMediaTemps(t, temp, 1)
		hash := sha256.New()
		n, err := io.CopyBuffer(hash, reader, make([]byte, CoreMediaChunkBytes))
		if err != nil || n != want.Size || fmt.Sprintf("%x", hash.Sum(nil)) != want.MediaID {
			t.Fatalf("snapshot: %d %v", n, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal("idempotent close", err)
		}
		assertMediaTemps(t, temp, 0)
	}
	// Selection validates the large object without requesting an allocating read.
	large := media[len(media)-1]
	entry, err := s.CreateCoreMediaEntry(ctx, "Large", "fes.test", strings.Repeat("a", 64), "blob", large.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, large.MediaID, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", "blob", large.MediaID); err != nil {
		t.Fatal(err)
	}
	assertMediaTemps(t, temp, 0)
}

func TestCoreMediaOpenIsIndependentSnapshot(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s, _ := mediaTestStore(t)
	media, _, err := s.ImportCoreMediaStream(ctx, CoreMediaChunkBytes+1, &patternMediaReader{remaining: CoreMediaChunkBytes + 1})
	if err != nil {
		t.Fatal(err)
	}
	_, reader, err := s.OpenCoreMedia(ctx, media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	// If Open retains a connection or transaction, the single-connection update
	// will time out. Corrupting the database cannot change the returned snapshot.
	s.db.SetMaxOpenConns(1)
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(bounded, "UPDATE core_media_chunks SET data = X'00' WHERE media_id = ?", media.MediaID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	n, err := io.CopyBuffer(hash, reader, make([]byte, CoreMediaChunkBytes))
	if err != nil || n != media.Size || fmt.Sprintf("%x", hash.Sum(nil)) != media.MediaID {
		t.Fatalf("snapshot changed: %d %v", n, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	assertMediaTemps(t, temp, 0)
}

func TestCoreMediaStreamRejectsCorruption(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"missing-first", "DELETE FROM core_media_chunks WHERE chunk_index = 0"},
		{"missing-last", "DELETE FROM core_media_chunks WHERE chunk_index = 2"},
		{"gap", "UPDATE core_media_chunks SET chunk_index = 5 WHERE chunk_index = 1"},
		{"negative", "UPDATE core_media_chunks SET chunk_index = -1 WHERE chunk_index = 0"},
		{"reordered", "UPDATE core_media_chunks SET data = (SELECT data FROM core_media_chunks WHERE chunk_index = 1) WHERE chunk_index = 0"},
		{"oversized", "UPDATE core_media_chunks SET data = zeroblob(8 * 1024 * 1024) WHERE chunk_index = 1"},
		{"text", "UPDATE core_media_chunks SET data = 'invalid text' WHERE chunk_index = 1"},
		{"empty", "UPDATE core_media_chunks SET data = X'' WHERE chunk_index = 1"},
		{"short", "UPDATE core_media_chunks SET data = X'01' WHERE chunk_index = 0"},
		{"extra", "INSERT INTO core_media_chunks SELECT media_id, 3, X'01' FROM core_media_chunks WHERE chunk_index = 2"},
		{"size", "UPDATE core_media SET size = size + 1 WHERE length(data) = 0"},
		{"mixed-legacy", "UPDATE core_media SET data = X'01', size = 1 WHERE length(data) = 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			temp := t.TempDir()
			t.Setenv("TMPDIR", temp)
			s, _ := mediaTestStore(t)
			size := int64(2*CoreMediaChunkBytes + 1)
			media, _, err := s.ImportCoreMediaStream(ctx, size, &patternMediaReader{remaining: size})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.ExecContext(ctx, tc.sql); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CoreMediaInfo(ctx, media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) {
				t.Fatalf("info: %v", err)
			}
			if got, reader, err := s.OpenCoreMedia(ctx, media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) || reader != nil || got != (CoreMedia{}) {
				t.Fatalf("partial open: %+v %v %v", got, reader, err)
			}
			if _, added, err := s.ImportCoreMediaStream(ctx, size, &patternMediaReader{remaining: size}); !errors.Is(err, ErrInvalidCoreMedia) || added {
				t.Fatalf("duplicate repaired corruption: %v %v", added, err)
			}
			if _, err := s.CreateCoreMediaEntry(ctx, "Corrupt", "fes.test", strings.Repeat("a", 64), "blob", media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) {
				t.Fatalf("bind corrupt: %v", err)
			}
			assertMediaTemps(t, temp, 0)
		})
	}
}

type cancelMediaReader struct{ cancel context.CancelFunc }

func (r cancelMediaReader) Read(p []byte) (int, error) { p[0] = 1; r.cancel(); return 1, nil }

func TestCoreMediaStreamFailuresCleanUp(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s, _ := mediaTestStore(t)
	var before int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM core_media").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		size   int64
		reader io.Reader
	}{
		{"nil", 1, nil}, {"truncated", 2, strings.NewReader("a")}, {"excess", 1, strings.NewReader("ab")},
	} {
		if _, _, err := s.ImportCoreMediaStream(ctx, tc.size, tc.reader); !errors.Is(err, ErrInvalidCoreMedia) {
			t.Fatalf("%s: %v", tc.name, err)
		}
		assertMediaTemps(t, temp, 0)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.ImportCoreMediaStream(canceled, 1, strings.NewReader("a")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel: %v", err)
	}
	during, cancelDuring := context.WithCancel(ctx)
	defer cancelDuring()
	if _, _, err := s.ImportCoreMediaStream(during, 1, cancelMediaReader{cancelDuring}); !errors.Is(err, context.Canceled) || errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("during cancel: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER fail_chunks BEFORE INSERT ON core_media_chunks WHEN new.chunk_index = 1 BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ImportCoreMediaStream(ctx, CoreMediaChunkBytes+1, &patternMediaReader{remaining: CoreMediaChunkBytes + 1}); err == nil {
		t.Fatal("expected chunk insertion failure")
	}
	var after, chunks int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM core_media").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM core_media_chunks").Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if after != before || chunks != 0 {
		t.Fatalf("failed import leaked rows: before=%d after=%d chunks=%d", before, after, chunks)
	}
	for _, id := range []string{"bad", strings.Repeat("a", 64)} {
		want := ErrCoreMediaNotFound
		if id == "bad" {
			want = ErrInvalidCoreMedia
		}
		if _, err := s.CoreMediaInfo(ctx, id); !errors.Is(err, want) {
			t.Fatalf("info %q: %v", id, err)
		}
		if _, reader, err := s.OpenCoreMedia(ctx, id); !errors.Is(err, want) || reader != nil {
			t.Fatalf("open %q: %v", id, err)
		}
	}
	assertMediaTemps(t, temp, 0)
}

func TestCoreMediaStreamConcurrentImports(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s, path := mediaTestStore(t)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	const size = 2*CoreMediaChunkBytes + 1
	type result struct {
		media CoreMedia
		added bool
		err   error
	}
	results := make(chan result, 4)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, store := range []*Store{s, other, s, other} {
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			<-start
			media, added, err := store.ImportCoreMediaStream(ctx, size, &patternMediaReader{remaining: size})
			results <- result{media, added, err}
		}(store)
	}
	close(start)
	wg.Wait()
	close(results)
	added := 0
	want := mediaDigest(t, size)
	for r := range results {
		if r.err != nil || r.media.MediaID != want {
			t.Fatalf("concurrent: %+v", r)
		}
		if r.added {
			added++
		}
	}
	if added != 1 {
		t.Fatalf("created %d objects", added)
	}
	var chunks int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM core_media_chunks WHERE media_id = ?", want).Scan(&chunks); err != nil || chunks != 3 {
		t.Fatalf("chunks=%d %v", chunks, err)
	}
	assertMediaTemps(t, temp, 0)
}

type heldMediaReader struct {
	entered, release chan struct{}
	once             sync.Once
	body             io.Reader
}

func (r *heldMediaReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.entered); <-r.release })
	return r.body.Read(p)
}

func TestCoreMediaStreamSnapshotsBeforeWriter(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	held := &heldMediaReader{entered: make(chan struct{}), release: make(chan struct{}), body: strings.NewReader("abc")}
	result := make(chan error, 1)
	go func() { _, _, err := s.ImportCoreMediaStream(ctx, 3, held); result <- err }()
	<-held.entered
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	_, err := s.db.ExecContext(bounded, "UPDATE core_media SET size = size")
	cancel()
	close(held.release)
	importErr := <-result
	if err != nil || importErr != nil {
		t.Fatalf("writer held during body read: %v; import %v", err, importErr)
	}
}

func TestCoreMediaSchemaEightPreservesSeven(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{schemaV1, schemaV2, schemaV3, schemaV4, schemaV5, schemaV6} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateCoreMedia(ctx, conn); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	data := bytes.Repeat([]byte{7}, legacyCoreMediaBytes)
	id := fmt.Sprintf("%x", sha256.Sum256(data))
	if _, err := db.ExecContext(ctx, "INSERT INTO core_media VALUES (?, ?, ?)", id, len(data), data); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, key := range []string{id, "ef9443c2787cd02b6d78d233d015b0bbf3fb21d53d1a5890497cdbb7897f053c"} {
		info, err := s.CoreMediaInfo(ctx, key)
		if err != nil || info.MediaID != key {
			t.Fatalf("legacy info: %+v %v", info, err)
		}
	}
	if got, added, err := s.ImportCoreMediaStream(ctx, int64(len(data)), bytes.NewReader(data)); err != nil || added || got.MediaID != id {
		t.Fatalf("legacy reimport %+v %v %v", got, added, err)
	}
	var length, chunks int
	if err := s.db.QueryRowContext(ctx, "SELECT length(data) FROM core_media WHERE media_id = ?", id).Scan(&length); err != nil || length != len(data) {
		t.Fatalf("legacy rewritten: %d %v", length, err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM core_media_chunks").Scan(&chunks); err != nil || chunks != 0 {
		t.Fatalf("unexpected conversion: %d %v", chunks, err)
	}
	// Legacy corruption does not inherit the expanded policy even with a matching hash.
	oversized := bytes.Repeat([]byte{7}, legacyCoreMediaBytes+1)
	badID := fmt.Sprintf("%x", sha256.Sum256(oversized))
	if _, err := s.db.ExecContext(ctx, "INSERT INTO core_media VALUES (?, ?, ?)", badID, len(oversized), oversized); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CoreMediaInfo(ctx, badID); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("legacy bound expanded: %v", err)
	}
}

func TestCoreMediaStreamRejectsOtherwiseValidMixedRepresentation(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	data := []byte("valid in either representation alone")
	media, _, err := s.ImportCoreMedia(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE core_media SET data = ? WHERE media_id = ?", data, media.MediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CoreMediaInfo(ctx, media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("accepted mixed representation: %v", err)
	}
	if _, reader, err := s.OpenCoreMedia(ctx, media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) || reader != nil {
		t.Fatalf("mixed open: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM core_media_chunks WHERE media_id = ?", media.MediaID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.CoreMediaInfo(ctx, media.MediaID); err != nil || got != media {
		t.Fatalf("legacy alone invalid: %+v %v", got, err)
	}
}

type callbackMediaWriter struct {
	once     sync.Once
	callback func()
	writer   io.Writer
}

func (w *callbackMediaWriter) Write(p []byte) (int, error) {
	w.once.Do(w.callback)
	return w.writer.Write(p)
}

func TestCoreMediaStreamReadUsesOneSnapshot(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	const size = 2*CoreMediaChunkBytes + 1
	media, _, err := s.ImportCoreMediaStream(ctx, size, &patternMediaReader{remaining: size})
	if err != nil {
		t.Fatal(err)
	}
	var writeErr error
	hash := sha256.New()
	dst := &callbackMediaWriter{writer: hash, callback: func() {
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_, writeErr = other.db.ExecContext(bounded, "UPDATE core_media_chunks SET data = X'00' WHERE media_id = ? AND chunk_index = 1", media.MediaID)
	}}
	got, err := s.readCoreMediaSnapshot(ctx, media.MediaID, dst)
	if err != nil || writeErr != nil || got != media || fmt.Sprintf("%x", hash.Sum(nil)) != media.MediaID {
		t.Fatalf("inconsistent snapshot or reserved writer: %+v read=%v write=%v", got, err, writeErr)
	}
	if _, err := s.CoreMediaInfo(ctx, media.MediaID); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("next read missed committed corruption: %v", err)
	}
}

func TestCoreMediaStreamCanceledReadReleasesTransaction(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	media, _, err := s.ImportCoreMediaStream(ctx, 2*CoreMediaChunkBytes, &patternMediaReader{remaining: 2 * CoreMediaChunkBytes})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err = s.readCoreMediaSnapshot(canceled, media.MediaID, &callbackMediaWriter{writer: io.Discard, callback: cancel})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read: %v", err)
	}
	s.db.SetMaxOpenConns(1)
	bounded, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if _, err := s.db.ExecContext(bounded, "UPDATE core_media SET size = size"); err != nil {
		t.Fatalf("read transaction leaked: %v", err)
	}
}

func TestCoreMediaStreamClosedStoreCleansSnapshot(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s, _ := mediaTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ImportCoreMediaStream(context.Background(), 3, strings.NewReader("abc")); err == nil {
		t.Fatal("closed import succeeded")
	}
	if _, reader, err := s.OpenCoreMedia(context.Background(), strings.Repeat("a", 64)); err == nil || reader != nil {
		t.Fatal("closed open succeeded")
	}
	assertMediaTemps(t, temp, 0)
}

type callbackMediaReader struct {
	callback func()
	reader   io.Reader
	once     sync.Once
}

func (r *callbackMediaReader) Read(p []byte) (int, error) {
	r.once.Do(r.callback)
	return r.reader.Read(p)
}

func TestCoreMediaStreamReportsCleanupFailureAfterCommit(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s, _ := mediaTestStore(t)
	var removeErr error
	input := &callbackMediaReader{reader: strings.NewReader("abc"), callback: func() {
		files, err := os.ReadDir(temp)
		if err != nil {
			removeErr = err
			return
		}
		if len(files) != 1 {
			removeErr = fmt.Errorf("expected one private snapshot, got %d", len(files))
			return
		}
		// The open file remains usable on the remote Linux host, but Close's
		// subsequent unlink fails. No injection hook is needed in production.
		removeErr = os.Remove(filepath.Join(temp, files[0].Name()))
	}}
	media, created, err := s.ImportCoreMediaStream(context.Background(), 3, input)
	if removeErr != nil {
		t.Fatal(removeErr)
	}
	if !errors.Is(err, os.ErrNotExist) || !created || media.Size != 3 {
		t.Fatalf("cleanup failure lost: %+v created=%v err=%v", media, created, err)
	}
	if got, err := s.CoreMediaInfo(context.Background(), media.MediaID); err != nil || got != media {
		t.Fatalf("committed result lost: %+v %v", got, err)
	}
	assertMediaTemps(t, temp, 0)
}

func TestCoreMediaSnapshotCloseReportsFailureAndStillUnlinks(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	s, _ := mediaTestStore(t)
	media, _, err := s.ImportCoreMedia(context.Background(), []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	_, reader, err := s.OpenCoreMedia(context.Background(), media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reader.(*coreMediaSnapshot)
	if err := snapshot.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("close error hidden: %v", err)
	}
	if err := reader.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("repeat close changed result: %v", err)
	}
	assertMediaTemps(t, temp, 0)
}
