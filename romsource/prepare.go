// Package romsource lazily prepares cataloged ROM sources for bounded upload.
package romsource

import (
	"archive/zip"
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type Prepared struct {
	Path    string
	Content protocol.ContentIdentity

	mu       sync.Mutex
	data     []byte
	fileInfo fs.FileInfo
	root     *os.Root
	base     string
	removed  bool
	retained bool
	readers  int

	beforePathUnlink func(*os.Root, string) error
}

// Open returns prepared content. Normal Preparer values use an owned bounded
// snapshot; legacy path-backed values are verified against their held staging
// identity.
func (p *Prepared) Open() (io.ReadCloser, error) {
	if p == nil {
		return nil, errors.New("open prepared ROM staging file")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.removed {
		return nil, errors.New("open prepared ROM staging file")
	}
	if p.retained {
		return nil, ErrCleanupRetained
	}
	if p.data != nil {
		p.readers++
		return &snapshotReadCloser{reader: bytes.NewReader(p.data), owner: p}, nil
	}
	if p.Path == "" {
		return nil, errors.New("open prepared ROM staging file")
	}
	if p.root != nil {
		return p.openFromRoot()
	}
	return p.openFromPath()
}

// NewPreparedSnapshot creates a path-independent prepared value whose content
// is bounded by the protocol maximum and owned by the returned value.
func NewPreparedSnapshot(data []byte, extension string) (*Prepared, error) {
	if len(data) < 1 || int64(len(data)) > protocol.MaxContentBytes {
		return nil, errors.New("prepared ROM snapshot size is invalid")
	}
	content := protocol.ContentIdentity{
		SHA256: fmt.Sprintf("%x", sha256.Sum256(data)),
		Size:   int64(len(data)), Extension: extension,
	}
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return nil, errors.New("prepared ROM snapshot identity is invalid")
	}
	return &Prepared{Content: content, data: append([]byte(nil), data...)}, nil
}

func (p *Prepared) openFromRoot() (*os.File, error) {
	current, err := p.root.Lstat(p.base)
	if err != nil || !validPreparedInfo(current) || (p.fileInfo != nil && !os.SameFile(p.fileInfo, current)) {
		return nil, errors.New("inspect prepared ROM staging file")
	}
	file, err := p.root.Open(p.base)
	if err != nil {
		return nil, errors.New("open prepared ROM staging file")
	}
	if err := validateOpenedPrepared(p.root.Lstat, p.base, file, current, p.fileInfo); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (p *Prepared) openFromPath() (*os.File, error) {
	current, err := os.Lstat(p.Path)
	if err != nil || !validPreparedInfo(current) || (p.fileInfo != nil && !os.SameFile(p.fileInfo, current)) {
		return nil, errors.New("inspect prepared ROM staging file")
	}
	file, err := os.Open(p.Path)
	if err != nil {
		return nil, errors.New("open prepared ROM staging file")
	}
	statPath := func(string) (fs.FileInfo, error) { return os.Lstat(p.Path) }
	if err := validateOpenedPrepared(statPath, p.Path, file, current, p.fileInfo); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateOpenedPrepared(lstat func(string) (fs.FileInfo, error), name string, file *os.File, current, expected fs.FileInfo) error {
	opened, err := file.Stat()
	if err != nil || !validPreparedInfo(opened) || !os.SameFile(current, opened) || (expected != nil && !os.SameFile(expected, opened)) {
		return errors.New("prepared ROM staging file identity changed")
	}
	rechecked, err := lstat(name)
	if err != nil || !validPreparedInfo(rechecked) || !os.SameFile(opened, rechecked) {
		return errors.New("prepared ROM staging file identity changed")
	}
	return nil
}

func validPreparedInfo(info fs.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o600
}

// Remove clears owned snapshot content. Path-backed values are retained with a
// fixed error because Go does not expose an identity-conditioned unlink.
func (p *Prepared) Remove() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.removed {
		return nil
	}
	if p.retained {
		return ErrCleanupRetained
	}
	if p.data != nil {
		p.removed = true
		if p.readers == 0 {
			clear(p.data)
			p.data = nil
		}
		return nil
	}
	if p.Path == "" {
		return nil
	}
	if p.root == nil {
		p.retained = true
		return ErrCleanupRetained
	}
	if p.beforePathUnlink != nil {
		_ = p.beforePathUnlink(p.root, p.base)
	}
	_ = p.root.Close()
	p.root = nil
	p.retained = true
	return ErrCleanupRetained
}

type snapshotReadCloser struct {
	mu     sync.Mutex
	reader *bytes.Reader
	owner  *Prepared
	closed bool
	once   sync.Once
}

func (r *snapshotReadCloser) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fs.ErrClosed
	}
	return r.reader.Read(buffer)
}

func (r *snapshotReadCloser) Close() error {
	r.once.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.reader = nil
		owner := r.owner
		r.owner = nil
		r.mu.Unlock()

		owner.releaseSnapshotReader()
	})
	return nil
}

func (p *Prepared) releaseSnapshotReader() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readers--
	if p.removed && p.readers == 0 && p.data != nil {
		clear(p.data)
		p.data = nil
	}
}

type Preparer struct {
	StagingRoot string
	MaxBytes    int64

	openFile                 func(*os.Root, string) (sourceFile, error)
	openZIPMember            func(*zip.File) (io.ReadCloser, error)
	statStagedFile           func(*os.File) (fs.FileInfo, error)
	chmodStagedFile          func(*os.File, fs.FileMode) error
	beforeStagingFinalVerify func(string, string) error
	beforeStagingRootChmod   func(string) error
	beforeStagingCreate      func(string) error
}

type sourceFile interface {
	io.Reader
	io.ReaderAt
	io.Closer
	Stat() (fs.FileInfo, error)
}

type verifiedSource struct {
	file          sourceFile
	root          *os.Root
	rootInfo      fs.FileInfo
	directoryPath string
	directoryInfo fs.FileInfo
	relativePath  string
	info          fs.FileInfo
}

func (p Preparer) Prepare(ctx context.Context, root catalog.Root, game catalog.Game) (*Prepared, error) {
	if err := ctx.Err(); err != nil {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, err)
	}
	maximum, err := p.maximumBytes()
	if err != nil {
		return nil, preparationError(game.ID, protocol.CodeTransferFailed, nil)
	}
	if game.LibraryID != root.ID || game.System != root.System || game.State != catalog.SourceStateAvailable || !game.RootOnline {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
	}
	relativePath, err := catalog.NormalizeRelativePath(game.RelativePath)
	if err != nil || relativePath != game.RelativePath {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
	}

	source, err := p.openVerifiedSource(root.Path, relativePath, game.Fingerprint)
	if err != nil {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, safeSourceCause(err))
	}
	closed := false
	defer func() {
		if !closed {
			_ = source.close()
		}
	}()

	var prepared *Prepared
	switch game.Kind {
	case catalog.SourceKindRaw:
		prepared, err = p.prepareRaw(ctx, game, source, maximum)
	case catalog.SourceKindZIP:
		prepared, err = p.prepareZIP(ctx, game, source, maximum)
	default:
		err = preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
	}
	closeErr := source.close()
	closed = true
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		cleanupErr := prepared.Remove()
		return nil, errors.Join(
			preparationError(game.ID, protocol.CodeSourceUnavailable, nil),
			safeCleanupCause(cleanupErr),
		)
	}
	return prepared, nil
}

func (p Preparer) maximumBytes() (int64, error) {
	switch {
	case p.MaxBytes < 0:
		return 0, errors.New("negative content limit")
	case p.MaxBytes == 0 || p.MaxBytes > protocol.MaxContentBytes:
		return protocol.MaxContentBytes, nil
	default:
		return p.MaxBytes, nil
	}
}

func (p Preparer) prepareRaw(ctx context.Context, game catalog.Game, source *verifiedSource, maximum int64) (*Prepared, error) {
	if source.info.Size() < 1 || source.info.Size() > maximum {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
	}
	extension, err := normalizedExtension(game.RelativePath)
	if err != nil {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
	}
	return p.stream(ctx, game.ID, source, source.file, source.info.Size(), extension, maximum, protocol.CodeSourceUnavailable)
}

func (p Preparer) prepareZIP(ctx context.Context, game catalog.Game, source *verifiedSource, maximum int64) (*Prepared, error) {
	reader, err := zip.NewReader(source.file, source.info.Size())
	if err != nil {
		return nil, preparationError(game.ID, protocol.CodeInvalidArchive, safeArchiveCause(err))
	}
	if len(reader.File) != game.Fingerprint.ZIPEntryCount {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
	}
	var selected *zip.File
	for _, member := range reader.File {
		if member.Name == game.Fingerprint.ZIPMember {
			if selected != nil {
				return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
			}
			selected = member
		}
	}
	if selected == nil {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
	}
	if selected.UncompressedSize64 > uint64(^uint64(0)>>1) ||
		int64(selected.UncompressedSize64) != game.Fingerprint.ZIPSize ||
		selected.CRC32 != game.Fingerprint.ZIPCRC32 {
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
	}
	if selected.FileInfo().IsDir() || selected.Flags&1 != 0 {
		return nil, preparationError(game.ID, protocol.CodeInvalidArchive, nil)
	}
	if game.Fingerprint.ZIPSize < 1 || game.Fingerprint.ZIPSize > maximum {
		return nil, preparationError(game.ID, protocol.CodeInvalidArchive, nil)
	}
	extension, err := normalizedExtension(selected.Name)
	if err != nil {
		return nil, preparationError(game.ID, protocol.CodeInvalidArchive, nil)
	}
	openMember := p.openZIPMember
	if openMember == nil {
		openMember = func(member *zip.File) (io.ReadCloser, error) { return member.Open() }
	}
	member, err := openMember(selected)
	if err != nil {
		return nil, preparationError(game.ID, protocol.CodeInvalidArchive, safeArchiveCause(err))
	}
	prepared, streamErr := p.stream(ctx, game.ID, source, member, game.Fingerprint.ZIPSize, extension, maximum, protocol.CodeInvalidArchive)
	closeErr := member.Close()
	if streamErr != nil {
		return nil, streamErr
	}
	if closeErr != nil {
		cleanupErr := prepared.Remove()
		return nil, errors.Join(
			preparationError(game.ID, protocol.CodeInvalidArchive, safeArchiveCause(closeErr)),
			safeCleanupCause(cleanupErr),
		)
	}
	return prepared, nil
}

func (p Preparer) stream(ctx context.Context, gameID string, source *verifiedSource, reader io.Reader, expectedSize int64, extension string, maximum int64, readCode protocol.ErrorCode) (*Prepared, error) {
	if p.statStagedFile != nil || p.chmodStagedFile != nil || p.beforeStagingFinalVerify != nil || p.beforeStagingRootChmod != nil || p.beforeStagingCreate != nil {
		return p.streamStaged(ctx, gameID, source, reader, expectedSize, extension, maximum, readCode)
	}

	var snapshot bytes.Buffer
	hash := sha256.New()
	limited := io.LimitReader(&contextReader{ctx: ctx, reader: reader}, maximum+1)
	written, copyErr := io.Copy(io.MultiWriter(&snapshot, hash), limited)
	if written > maximum {
		return nil, preparationError(gameID, readCode, nil)
	}
	if copyErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, preparationError(gameID, protocol.CodeSourceUnavailable, ctxErr)
		}
		cause := error(nil)
		if readCode == protocol.CodeInvalidArchive {
			cause = safeArchiveCause(copyErr)
		}
		return nil, preparationError(gameID, readCode, cause)
	}
	if written != expectedSize {
		return nil, preparationError(gameID, readCode, nil)
	}
	if err := source.revalidate(); err != nil {
		return nil, preparationError(gameID, protocol.CodeSourceUnavailable, safeSourceCause(err))
	}
	if err := ctx.Err(); err != nil {
		return nil, preparationError(gameID, protocol.CodeSourceUnavailable, err)
	}
	content := protocol.ContentIdentity{SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Size: written, Extension: extension}
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return nil, preparationError(gameID, readCode, nil)
	}
	return &Prepared{Content: content, data: snapshot.Bytes()}, nil
}

func (p Preparer) streamStaged(ctx context.Context, gameID string, source *verifiedSource, reader io.Reader, expectedSize int64, extension string, maximum int64, readCode protocol.ErrorCode) (prepared *Prepared, resultErr error) {
	staged, err := newStagedFile(p)
	if err != nil {
		cause := error(nil)
		if errors.Is(err, ErrCleanupRetained) {
			cause = ErrCleanupRetained
		}
		return nil, preparationError(gameID, protocol.CodeTransferFailed, cause)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			if cleanupErr := staged.cleanup(); cleanupErr != nil {
				resultErr = errors.Join(resultErr, cleanupErr)
			}
		}
	}()

	hash := sha256.New()
	limited := io.LimitReader(&contextReader{ctx: ctx, reader: reader}, maximum+1)
	written, copyErr := io.Copy(io.MultiWriter(staged.file, hash), limited)
	if written > maximum {
		return nil, preparationError(gameID, readCode, nil)
	}
	if copyErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, preparationError(gameID, protocol.CodeSourceUnavailable, ctxErr)
		}
		cause := error(nil)
		if readCode == protocol.CodeInvalidArchive {
			cause = safeArchiveCause(copyErr)
		}
		return nil, preparationError(gameID, readCode, cause)
	}
	if written != expectedSize {
		return nil, preparationError(gameID, readCode, nil)
	}
	if err := source.revalidate(); err != nil {
		return nil, preparationError(gameID, protocol.CodeSourceUnavailable, safeSourceCause(err))
	}
	if err := ctx.Err(); err != nil {
		return nil, preparationError(gameID, protocol.CodeSourceUnavailable, err)
	}
	if p.beforeStagingFinalVerify != nil {
		if err := p.beforeStagingFinalVerify(staged.root.Name(), staged.path); err != nil {
			return nil, preparationError(gameID, protocol.CodeTransferFailed, nil)
		}
	}
	content := protocol.ContentIdentity{SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Size: written, Extension: extension}
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return nil, preparationError(gameID, readCode, nil)
	}
	if err := staged.file.Sync(); err != nil {
		return nil, preparationError(gameID, protocol.CodeTransferFailed, nil)
	}
	fileInfo, err := staged.verify(true)
	if err != nil {
		return nil, preparationError(gameID, protocol.CodeTransferFailed, nil)
	}
	if err := staged.file.Close(); err != nil {
		staged.closed = true
		return nil, preparationError(gameID, protocol.CodeTransferFailed, nil)
	}
	staged.closed = true
	fileInfo, err = staged.verify(false)
	if err != nil {
		return nil, preparationError(gameID, protocol.CodeTransferFailed, nil)
	}
	succeeded = true
	return &Prepared{Path: staged.path, Content: content, fileInfo: fileInfo, root: staged.root, base: staged.base}, nil
}

func (p Preparer) openVerifiedSource(rootPath, relativePath string, fingerprint catalog.Fingerprint) (*verifiedSource, error) {
	rootInfo, err := os.Lstat(rootPath)
	if err != nil || rootInfo.Mode()&fs.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.Join(err, errors.New("catalog root is unavailable"))
	}
	heldRoot, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	failRoot := func(err error) (*verifiedSource, error) {
		_ = heldRoot.Close()
		return nil, err
	}
	openedRootInfo, err := heldRoot.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedRootInfo) {
		return failRoot(errors.Join(err, errors.New("catalog root identity changed")))
	}

	directoryPath, base := path.Split(relativePath)
	directoryPath = strings.TrimSuffix(directoryPath, "/")
	if directoryPath == "" {
		directoryPath = "."
	}
	directoryInfo, err := validateSourceDirectory(heldRoot, directoryPath)
	if err != nil {
		return failRoot(err)
	}
	parent := heldRoot
	if directoryPath != "." {
		parent, err = heldRoot.OpenRoot(directoryPath)
		if err != nil {
			return failRoot(err)
		}
		defer parent.Close()
		openedDirectory, err := parent.Stat(".")
		if err != nil || !os.SameFile(directoryInfo, openedDirectory) {
			return failRoot(errors.Join(err, errors.New("source directory identity changed")))
		}
	}

	entryInfo, err := heldRoot.Lstat(relativePath)
	if err != nil || entryInfo.Mode()&fs.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() || !matchesFingerprint(entryInfo, fingerprint) {
		return failRoot(errors.Join(err, errors.New("source fingerprint changed before opening")))
	}
	openFile := p.openFile
	if openFile == nil {
		openFile = func(root *os.Root, name string) (sourceFile, error) { return root.Open(name) }
	}
	file, err := openFile(parent, base)
	if err != nil {
		return failRoot(err)
	}
	failFile := func(err error) (*verifiedSource, error) {
		_ = file.Close()
		return failRoot(err)
	}
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(entryInfo, fileInfo) || !matchesFingerprint(fileInfo, fingerprint) {
		return failFile(errors.Join(err, errors.New("source identity changed while opening")))
	}
	recheckedDirectory, err := validateSourceDirectory(heldRoot, directoryPath)
	if err != nil || !os.SameFile(directoryInfo, recheckedDirectory) {
		return failFile(errors.Join(err, errors.New("source directory identity changed after opening")))
	}
	recheckedEntry, err := heldRoot.Lstat(relativePath)
	if err != nil || recheckedEntry.Mode()&fs.ModeSymlink != 0 || !recheckedEntry.Mode().IsRegular() || !os.SameFile(fileInfo, recheckedEntry) || !matchesFingerprint(recheckedEntry, fingerprint) {
		return failFile(errors.Join(err, errors.New("source identity changed after opening")))
	}
	return &verifiedSource{
		file: file, root: heldRoot, rootInfo: rootInfo, directoryPath: directoryPath,
		directoryInfo: directoryInfo, relativePath: relativePath, info: fileInfo,
	}, nil
}

func (s *verifiedSource) revalidate() error {
	fileInfo, err := s.file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(s.info, fileInfo) || fileInfo.Size() != s.info.Size() || fileInfo.ModTime().UnixNano() != s.info.ModTime().UnixNano() {
		return errors.Join(err, errors.New("source changed during streaming"))
	}
	rootInfo, err := os.Lstat(s.root.Name())
	if err != nil || rootInfo.Mode()&fs.ModeSymlink != 0 || !rootInfo.IsDir() || !os.SameFile(s.rootInfo, rootInfo) {
		return errors.Join(err, errors.New("catalog root changed during streaming"))
	}
	directoryInfo, err := validateSourceDirectory(s.root, s.directoryPath)
	if err != nil || !os.SameFile(s.directoryInfo, directoryInfo) {
		return errors.Join(err, errors.New("source directory changed during streaming"))
	}
	entryInfo, err := s.root.Lstat(s.relativePath)
	if err != nil || entryInfo.Mode()&fs.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() || !os.SameFile(s.info, entryInfo) || entryInfo.Size() != s.info.Size() || entryInfo.ModTime().UnixNano() != s.info.ModTime().UnixNano() {
		return errors.Join(err, errors.New("source path changed during streaming"))
	}
	return nil
}

func (s *verifiedSource) close() error {
	return errors.Join(s.file.Close(), s.root.Close())
}

func validateSourceDirectory(root *os.Root, directoryPath string) (fs.FileInfo, error) {
	if directoryPath == "." {
		return root.Stat(".")
	}
	current := ""
	var info fs.FileInfo
	for _, component := range strings.Split(directoryPath, "/") {
		current = path.Join(current, component)
		var err error
		info, err = root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return nil, errors.New("source parent is not a real directory")
		}
	}
	return info, nil
}

func matchesFingerprint(info fs.FileInfo, fingerprint catalog.Fingerprint) bool {
	return info.Size() == fingerprint.SourceSize && info.ModTime().UnixNano() == fingerprint.ModifiedNS
}

func normalizedExtension(name string) (string, error) {
	extension := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	if err := protocol.ValidateExtension(extension); err != nil {
		return "", err
	}
	return extension, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type stagedFile struct {
	file     *os.File
	root     *os.Root
	rootInfo fs.FileInfo
	path     string
	base     string
	info     fs.FileInfo
	statFile func(*os.File) (fs.FileInfo, error)
	closed   bool
}

func newStagedFile(preparer Preparer) (*stagedFile, error) {
	rootPath := preparer.StagingRoot
	if rootPath == "" || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		return nil, errors.New("staging root must be a clean absolute path")
	}
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(rootPath)
	if err != nil || rootInfo.Mode()&fs.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.Join(err, errors.New("staging root is not a real directory"))
	}
	heldRoot, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	failRoot := func(err error) (*stagedFile, error) {
		_ = heldRoot.Close()
		return nil, err
	}
	openedRootInfo, err := heldRoot.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedRootInfo) {
		return failRoot(errors.Join(err, errors.New("staging root identity changed")))
	}
	if preparer.beforeStagingRootChmod != nil {
		if err := preparer.beforeStagingRootChmod(rootPath); err != nil {
			return failRoot(err)
		}
	}
	if err := heldRoot.Chmod(".", 0o700); err != nil {
		return failRoot(err)
	}
	chmodRootInfo, err := heldRoot.Stat(".")
	if err != nil || !chmodRootInfo.IsDir() || !os.SameFile(rootInfo, chmodRootInfo) || chmodRootInfo.Mode().Perm() != 0o700 {
		return failRoot(errors.Join(err, errors.New("staging root mode or identity changed")))
	}
	pathRootInfo, err := os.Lstat(rootPath)
	if err != nil || pathRootInfo.Mode()&fs.ModeSymlink != 0 || !pathRootInfo.IsDir() || !os.SameFile(chmodRootInfo, pathRootInfo) || pathRootInfo.Mode().Perm() != 0o700 {
		return failRoot(errors.Join(err, errors.New("staging root path mode or identity changed")))
	}
	if preparer.beforeStagingCreate != nil {
		if err := preparer.beforeStagingCreate(rootPath); err != nil {
			return failRoot(err)
		}
	}
	file, base, err := createTempInRoot(heldRoot)
	if err != nil {
		return failRoot(err)
	}
	statFile := preparer.statStagedFile
	if statFile == nil {
		statFile = func(file *os.File) (fs.FileInfo, error) { return file.Stat() }
	}
	staged := &stagedFile{
		file: file, root: heldRoot, rootInfo: chmodRootInfo, path: file.Name(),
		base: base, statFile: statFile,
	}
	fileInfo, err := file.Stat()
	staged.info = fileInfo
	if err != nil || fileInfo == nil || !fileInfo.Mode().IsRegular() {
		return nil, errors.Join(err, errors.New("staging file is not regular"), staged.cleanup())
	}
	rootEntry, err := heldRoot.Lstat(staged.base)
	if err != nil || !rootEntry.Mode().IsRegular() || !os.SameFile(fileInfo, rootEntry) {
		return nil, errors.Join(err, errors.New("staging file identity changed after creation"), staged.cleanup())
	}
	checkedFileInfo, err := statFile(file)
	if err != nil || checkedFileInfo == nil || !checkedFileInfo.Mode().IsRegular() || !os.SameFile(fileInfo, checkedFileInfo) {
		return nil, errors.Join(err, errors.New("staging file descriptor identity changed after creation"), staged.cleanup())
	}
	chmodFile := preparer.chmodStagedFile
	if chmodFile == nil {
		chmodFile = func(file *os.File, mode fs.FileMode) error { return file.Chmod(mode) }
	}
	if err := chmodFile(file, 0o600); err != nil {
		return nil, errors.Join(err, staged.cleanup())
	}
	if _, err := staged.verify(true); err != nil {
		return nil, errors.Join(err, staged.cleanup())
	}
	return staged, nil
}

func createTempInRoot(root *os.Root) (*os.File, string, error) {
	for range 10_000 {
		base := ".fogcast-rom-" + strings.ToLower(cryptorand.Text())
		file, err := root.OpenFile(base, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return file, base, err
	}
	return nil, "", fs.ErrExist
}

func (s *stagedFile) verify(fileOpen bool) (fs.FileInfo, error) {
	heldRoot, err := s.root.Stat(".")
	if err != nil || !heldRoot.IsDir() || !os.SameFile(s.rootInfo, heldRoot) || heldRoot.Mode().Perm() != 0o700 {
		return nil, errors.Join(err, errors.New("held staging root mode or identity changed"))
	}
	pathRoot, err := os.Lstat(s.root.Name())
	if err != nil || pathRoot.Mode()&fs.ModeSymlink != 0 || !pathRoot.IsDir() || !os.SameFile(heldRoot, pathRoot) || pathRoot.Mode().Perm() != 0o700 {
		return nil, errors.Join(err, errors.New("staging root path mode or identity changed"))
	}
	if fileOpen {
		descriptorEntry, err := s.statFile(s.file)
		if err != nil || !descriptorEntry.Mode().IsRegular() || !os.SameFile(s.info, descriptorEntry) || descriptorEntry.Mode().Perm() != 0o600 {
			return nil, errors.Join(err, errors.New("staging file descriptor mode or identity changed"))
		}
	}
	rootEntry, err := s.root.Lstat(s.base)
	if err != nil || !rootEntry.Mode().IsRegular() || !os.SameFile(s.info, rootEntry) || rootEntry.Mode().Perm() != 0o600 {
		return nil, errors.Join(err, errors.New("held staging file mode or identity changed"))
	}
	pathEntry, err := os.Lstat(s.path)
	if err != nil || !pathEntry.Mode().IsRegular() || !os.SameFile(s.info, pathEntry) || pathEntry.Mode().Perm() != 0o600 {
		return nil, errors.Join(err, errors.New("staging path mode or identity changed"))
	}
	return pathEntry, nil
}

func (s *stagedFile) cleanup() error {
	if !s.closed {
		_ = s.file.Close()
		s.closed = true
	}
	_ = s.root.Close()
	return ErrCleanupRetained
}

func safeSourceCause(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, fs.ErrNotExist):
		return fs.ErrNotExist
	default:
		return nil
	}
}

func safeArchiveCause(err error) error {
	switch {
	case errors.Is(err, zip.ErrChecksum):
		return zip.ErrChecksum
	case errors.Is(err, zip.ErrFormat):
		return zip.ErrFormat
	default:
		return nil
	}
}

func safeCleanupCause(err error) error {
	if err != nil {
		return ErrCleanupRetained
	}
	return nil
}
