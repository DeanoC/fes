package librarymedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	_ "modernc.org/sqlite"
)

const (
	schemaVersion  = 1
	MaxStillBytes  = 8 << 20
	MaxVideoBytes  = 128 << 20
	RoleCover      = "cover"
	RoleLogo       = "logo"
	RoleMarquee    = "marquee"
	RoleScreenshot = "screenshot"
	RoleBackdrop   = "backdrop"
	RoleVideo      = "video"
	MIMEJPEG       = "image/jpeg"
	MIMEPNG        = "image/png"
	MIMEMP4        = "video/mp4"
	MIMEWebM       = "video/webm"
)

const schemaV1 = `
CREATE TABLE media_objects (
  handle TEXT PRIMARY KEY,
  game_id TEXT NOT NULL,
  role TEXT NOT NULL,
  mime TEXT NOT NULL,
  size INTEGER NOT NULL,
  digest TEXT NOT NULL,
  root_id TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  UNIQUE(root_id, relative_path)
);
CREATE INDEX media_objects_game_role ON media_objects(game_id, role, handle);
PRAGMA user_version = 1;
`

type Root struct {
	ID   string
	Path string
}

type Object struct {
	Handle string
	GameID string
	Role   string
	MIME   string
	Size   int64
	Digest string
	RootID string
	Rel    string
}

type GameMedia struct {
	Cover      string
	Logo       string
	Marquee    string
	Backdrop   string
	Video      string
	Screenshot []string
}

type Opened struct {
	MIME   string
	Size   int64
	Reader io.ReadCloser
	Seeker io.ReadSeeker
}

type Index struct {
	db    *sql.DB
	roots []Root
	byID  map[string]Root
	cache string
	held  map[string]*os.Root
}

func Open(ctx context.Context, path string, cache string, roots []Root) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open library media index: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	connection, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	defer connection.Close()
	for _, statement := range []string{"PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL"} {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := migrate(ctx, connection); err != nil {
		_ = db.Close()
		return nil, err
	}
	if cache != "" {
		if err := os.MkdirAll(cache, 0o700); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	byID := make(map[string]Root, len(roots))
	for _, root := range roots {
		byID[root.ID] = root
	}
	return &Index{db: db, roots: roots, byID: byID, cache: cache, held: map[string]*os.Root{}}, nil
}

func migrate(ctx context.Context, connection *sql.Conn) (err error) {
	if _, err := connection.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	var version int
	if err := connection.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("library media schema version %d is newer than supported version %d", version, schemaVersion)
	}
	if version == 0 {
		if _, err := connection.ExecContext(ctx, schemaV1); err != nil {
			return err
		}
	}
	_, err = connection.ExecContext(ctx, "COMMIT")
	return err
}

func (x *Index) Close() error {
	if x == nil {
		return nil
	}
	for _, root := range x.held {
		_ = root.Close()
	}
	if x.db == nil {
		return nil
	}
	return x.db.Close()
}

func (x *Index) Scan(ctx context.Context, games []catalog.Game) (err error) {
	if _, err = x.db.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_, _ = x.db.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if _, err = x.db.ExecContext(ctx, "DELETE FROM media_objects"); err != nil {
		return err
	}
	byID := make(map[string]catalog.Game, len(games))
	byStem := make(map[string]catalog.Game, len(games))
	for _, game := range games {
		idKey := string(game.System) + "/" + game.ID
		byID[idKey] = game
		stemKey := string(game.System) + "/" + romStem(game.RelativePath)
		if _, taken := byID[stemKey]; taken {
			continue
		}
		if _, taken := byStem[stemKey]; taken {
			continue
		}
		byStem[stemKey] = game
	}
	lookup := mediaLookup{byID: byID, byStem: byStem}
	for _, root := range x.roots {
		if err = x.scanRoot(ctx, root, lookup); err != nil {
			return err
		}
	}
	if _, err = x.db.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	return nil
}

type mediaLookup struct {
	byID   map[string]catalog.Game
	byStem map[string]catalog.Game
}

func (l mediaLookup) game(platform, key string) (catalog.Game, bool) {
	if game, ok := l.byID[platform+"/"+key]; ok {
		return game, true
	}
	game, ok := l.byStem[platform+"/"+key]
	return game, ok
}

func (x *Index) scanRoot(ctx context.Context, root Root, lookup mediaLookup) error {
	info, err := os.Lstat(root.Path)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return nil
	}
	held, err := os.OpenRoot(root.Path)
	if err != nil {
		return nil
	}
	defer held.Close()
	return fs.WalkDir(held.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := catalog.NormalizeRelativePath(name)
		if err != nil {
			return nil
		}
		platform, key, role, ext, ok := parseMediaPath(relative)
		if !ok {
			return nil
		}
		game, ok := lookup.game(platform, key)
		if !ok {
			return nil
		}
		return x.ingest(ctx, held, root, relative, game, role, ext)
	})
}

func parseMediaPath(relative string) (platform, key, role, ext string, ok bool) {
	parts := strings.Split(relative, "/")
	if len(parts) != 3 {
		return "", "", "", "", false
	}
	platform, key, file := parts[0], parts[1], parts[2]
	ext = strings.ToLower(path.Ext(file))
	role = strings.ToLower(strings.TrimSuffix(file, ext))
	if !validRole(role) || platform == "" || key == "" {
		return "", "", "", "", false
	}
	return platform, key, role, ext, true
}

func validRole(role string) bool {
	switch role {
	case RoleCover, RoleLogo, RoleMarquee, RoleScreenshot, RoleBackdrop, RoleVideo:
		return true
	default:
		return false
	}
}

func romStem(relativePath string) string {
	base := path.Base(strings.ReplaceAll(relativePath, `\`, "/"))
	return strings.TrimSuffix(base, path.Ext(base))
}

func (x *Index) ingest(ctx context.Context, held *os.Root, root Root, relative string, game catalog.Game, role, ext string) error {
	info, err := held.Lstat(relative)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil
	}
	file, err := held.Open(relative)
	if err != nil {
		return nil
	}
	defer file.Close()
	if role == RoleVideo {
		mime, size, digest, ok := inspectVideo(file, info.Size(), ext)
		if !ok {
			return nil
		}
		return x.insert(ctx, Object{
			Handle: mediaHandle(root.ID, relative, digest),
			GameID: game.ID, Role: role, MIME: mime, Size: size, Digest: digest,
			RootID: root.ID, Rel: relative,
		})
	}
	mime, data, ok := sanitizeStill(file, info.Size(), ext)
	if !ok {
		return nil
	}
	digest := hex.EncodeToString(sum256(data))
	if x.cache != "" {
		if err := os.WriteFile(path.Join(x.cache, digest), data, 0o600); err != nil {
			return err
		}
	}
	return x.insert(ctx, Object{
		Handle: mediaHandle(root.ID, relative, digest),
		GameID: game.ID, Role: role, MIME: mime, Size: int64(len(data)), Digest: digest,
		RootID: root.ID, Rel: relative,
	})
}

func (x *Index) insert(ctx context.Context, object Object) error {
	_, err := x.db.ExecContext(ctx, `
		INSERT INTO media_objects(handle, game_id, role, mime, size, digest, root_id, relative_path)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(root_id, relative_path) DO UPDATE SET
		  handle=excluded.handle, game_id=excluded.game_id, role=excluded.role,
		  mime=excluded.mime, size=excluded.size, digest=excluded.digest`,
		object.Handle, object.GameID, object.Role, object.MIME, object.Size, object.Digest, object.RootID, object.Rel,
	)
	return err
}

func mediaHandle(rootID, relative, digest string) string {
	sum := sha256.Sum256([]byte(rootID + "\x00" + relative + "\x00" + digest))
	return hex.EncodeToString(sum[:])
}

func sum256(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func sanitizeStill(file *os.File, size int64, ext string) (string, []byte, bool) {
	if size < 1 || size > MaxStillBytes {
		return "", nil, false
	}
	compressed, err := io.ReadAll(io.LimitReader(file, MaxStillBytes+1))
	if err != nil || int64(len(compressed)) != size || int64(len(compressed)) > MaxStillBytes {
		return "", nil, false
	}
	declared := MIMEJPEG
	if ext == ".png" {
		declared = MIMEPNG
	} else if ext != ".jpg" && ext != ".jpeg" {
		return "", nil, false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(compressed))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width*config.Height > 16e6 {
		return "", nil, false
	}
	decoded, err := decodeStill(compressed)
	if err != nil {
		return "", nil, false
	}
	var output bytes.Buffer
	switch {
	case declared == MIMEPNG && format == "png":
		if err := png.Encode(&output, decoded); err != nil {
			return "", nil, false
		}
		return MIMEPNG, output.Bytes(), true
	default:
		if err := jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90}); err != nil {
			return "", nil, false
		}
		return MIMEJPEG, output.Bytes(), true
	}
}

func decodeStill(compressed []byte) (image.Image, error) {
	decoded, _, err := image.Decode(bytes.NewReader(compressed))
	return decoded, err
}

func inspectVideo(file *os.File, size int64, ext string) (string, int64, string, bool) {
	if size < 16 || size > MaxVideoBytes {
		return "", 0, "", false
	}
	var header [16]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		return "", 0, "", false
	}
	mime := ""
	switch ext {
	case ".mp4", ".m4v":
		if string(header[4:8]) != "ftyp" {
			return "", 0, "", false
		}
		mime = MIMEMP4
	case ".webm":
		if header[0] != 0x1A || header[1] != 0x45 || header[2] != 0xDF || header[3] != 0xA3 {
			return "", 0, "", false
		}
		mime = MIMEWebM
	default:
		return "", 0, "", false
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, "", false
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, io.LimitReader(file, size)); err != nil {
		return "", 0, "", false
	}
	return mime, size, hex.EncodeToString(hasher.Sum(nil)), true
}

func (x *Index) GameMedia(ctx context.Context, gameID string) (GameMedia, error) {
	rows, err := x.db.QueryContext(ctx, `
		SELECT handle, role, root_id, relative_path FROM media_objects WHERE game_id = ?`, gameID)
	if err != nil {
		return GameMedia{}, err
	}
	defer rows.Close()
	rootOrder := make(map[string]int, len(x.roots))
	for index, root := range x.roots {
		rootOrder[root.ID] = index
	}
	type candidate struct {
		handle, role, rootID, rel string
		rootIdx, kind             int
	}
	best := map[string]candidate{}
	screenshots := make([]candidate, 0)
	better := func(next, current candidate) bool {
		if next.kind != current.kind {
			return next.kind < current.kind
		}
		if next.rootIdx != current.rootIdx {
			return next.rootIdx < current.rootIdx
		}
		return next.handle < current.handle
	}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.handle, &item.role, &item.rootID, &item.rel); err != nil {
			return GameMedia{}, err
		}
		item.rootIdx = len(x.roots) + 1
		if index, ok := rootOrder[item.rootID]; ok {
			item.rootIdx = index
		}
		item.kind = 1
		parts := strings.Split(item.rel, "/")
		if len(parts) == 3 && parts[1] == gameID {
			item.kind = 0
		}
		if item.role == RoleScreenshot {
			screenshots = append(screenshots, item)
			continue
		}
		current, ok := best[item.role]
		if !ok || better(item, current) {
			best[item.role] = item
		}
	}
	if err := rows.Err(); err != nil {
		return GameMedia{}, err
	}
	for index := 1; index < len(screenshots); index++ {
		for cursor := index; cursor > 0 && better(screenshots[cursor], screenshots[cursor-1]); cursor-- {
			screenshots[cursor], screenshots[cursor-1] = screenshots[cursor-1], screenshots[cursor]
		}
	}
	media := GameMedia{
		Cover:    best[RoleCover].handle,
		Logo:     best[RoleLogo].handle,
		Marquee:  best[RoleMarquee].handle,
		Backdrop: best[RoleBackdrop].handle,
		Video:    best[RoleVideo].handle,
	}
	for _, shot := range screenshots {
		media.Screenshot = append(media.Screenshot, shot.handle)
	}
	return media, nil
}

func (x *Index) CoverHandle(ctx context.Context, gameID string) string {
	media, err := x.GameMedia(ctx, gameID)
	if err != nil {
		return ""
	}
	return media.Cover
}

func (x *Index) GamesWithMedia(ctx context.Context, ids []string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT DISTINCT game_id FROM media_objects`
	args := make([]any, 0)
	if len(ids) > 0 {
		query += ` WHERE game_id IN (` + strings.Repeat("?,", len(ids)-1) + `?)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	query += ` ORDER BY game_id LIMIT ?`
	args = append(args, limit)
	rows, err := x.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func (x *Index) Lookup(ctx context.Context, handle string) (Object, error) {
	if !handlePattern(handle) {
		return Object{}, fs.ErrNotExist
	}
	var object Object
	err := x.db.QueryRowContext(ctx, `
		SELECT handle, game_id, role, mime, size, digest, root_id, relative_path
		FROM media_objects WHERE handle = ?`, handle,
	).Scan(&object.Handle, &object.GameID, &object.Role, &object.MIME, &object.Size, &object.Digest, &object.RootID, &object.Rel)
	if err != nil {
		return Object{}, err
	}
	return object, nil
}

func (x *Index) Open(_ context.Context, handle string) (Opened, error) {
	object, err := x.Lookup(context.Background(), handle)
	if err != nil {
		return Opened{}, err
	}
	if object.Role != RoleVideo && x.cache != "" {
		file, err := os.Open(path.Join(x.cache, object.Digest))
		if err != nil {
			return Opened{}, err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return Opened{}, err
		}
		return Opened{MIME: object.MIME, Size: info.Size(), Reader: file, Seeker: file}, nil
	}
	root, ok := x.byID[object.RootID]
	if !ok {
		return Opened{}, fs.ErrNotExist
	}
	held, err := os.OpenRoot(root.Path)
	if err != nil {
		return Opened{}, err
	}
	info, err := held.Lstat(object.Rel)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 {
		_ = held.Close()
		return Opened{}, fs.ErrNotExist
	}
	file, err := held.Open(object.Rel)
	if err != nil {
		_ = held.Close()
		return Opened{}, err
	}
	if object.Role == RoleVideo {
		if info.Size() != object.Size || info.Size() < 16 || info.Size() > MaxVideoBytes {
			_ = file.Close()
			_ = held.Close()
			return Opened{}, fs.ErrNotExist
		}
		mime, size, digest, ok := inspectVideo(file, info.Size(), strings.ToLower(path.Ext(object.Rel)))
		if !ok || mime != object.MIME || size != object.Size || digest != object.Digest {
			_ = file.Close()
			_ = held.Close()
			return Opened{}, fs.ErrNotExist
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			_ = file.Close()
			_ = held.Close()
			return Opened{}, err
		}
	}
	rf := &rootFile{file: file, root: held}
	return Opened{MIME: object.MIME, Size: info.Size(), Reader: rf, Seeker: rf}, nil
}

type rootFile struct {
	file *os.File
	root *os.Root
}

func (f *rootFile) Read(p []byte) (int, error) { return f.file.Read(p) }

func (f *rootFile) Seek(offset int64, whence int) (int64, error) {
	return f.file.Seek(offset, whence)
}

func (f *rootFile) Close() error {
	err := f.file.Close()
	if f.root != nil {
		return errors.Join(err, f.root.Close())
	}
	return err
}

func Serve(w http.ResponseWriter, r *http.Request, opened Opened) {
	w.Header().Set("Content-Type", opened.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if opened.Seeker != nil {
		http.ServeContent(w, r, "media", time.Time{}, opened.Seeker)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", opened.Size))
	_, _ = io.CopyN(w, opened.Reader, opened.Size)
}

func handlePattern(handle string) bool {
	if len(handle) != 64 {
		return false
	}
	for _, character := range handle {
		if character < '0' || character > 'f' || (character > '9' && character < 'a') {
			return false
		}
	}
	return true
}
