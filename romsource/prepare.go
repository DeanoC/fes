// Package romsource lazily prepares cataloged ROM sources for bounded upload.
package romsource

import (
	"archive/zip"
	"context"
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

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type Prepared struct {
	Path    string
	Content protocol.ContentIdentity

	mu       sync.Mutex
	fileInfo fs.FileInfo
	root     *os.Root
	base     string
	removed  bool
}

// Remove deletes the staged content. It is safe to call more than once.
func (p *Prepared) Remove() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.removed || p.Path == "" {
		return nil
	}
	if p.root != nil {
		current, err := p.root.Lstat(p.base)
		if errors.Is(err, fs.ErrNotExist) {
			p.removed = true
			_ = p.root.Close()
			p.root = nil
			return nil
		}
		if err != nil {
			return errors.New("inspect prepared ROM staging file")
		}
		if p.fileInfo != nil && !os.SameFile(p.fileInfo, current) {
			return errors.New("prepared ROM staging file identity changed")
		}
		if !current.Mode().IsRegular() {
			return errors.New("prepared ROM staging path is not a regular file")
		}
		if err := p.root.Remove(p.base); err != nil {
			return errors.New("remove prepared ROM staging file")
		}
		p.removed = true
		closeErr := p.root.Close()
		p.root = nil
		if closeErr != nil {
			return errors.New("close prepared ROM staging directory")
		}
		return nil
	}
	current, err := os.Lstat(p.Path)
	if errors.Is(err, fs.ErrNotExist) {
		p.removed = true
		return nil
	}
	if err != nil {
		return errors.New("inspect prepared ROM staging file")
	}
	if p.fileInfo != nil && !os.SameFile(p.fileInfo, current) {
		return errors.New("prepared ROM staging file identity changed")
	}
	if !current.Mode().IsRegular() {
		return errors.New("prepared ROM staging path is not a regular file")
	}
	if err := os.Remove(p.Path); err != nil {
		return errors.New("remove prepared ROM staging file")
	}
	p.removed = true
	return nil
}

type Preparer struct {
	StagingRoot string
	MaxBytes    int64

	openFile      func(*os.Root, string) (sourceFile, error)
	openZIPMember func(*zip.File) (io.ReadCloser, error)
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
		_ = prepared.Remove()
		return nil, preparationError(game.ID, protocol.CodeSourceUnavailable, nil)
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
		_ = prepared.Remove()
		return nil, preparationError(game.ID, protocol.CodeInvalidArchive, safeArchiveCause(closeErr))
	}
	return prepared, nil
}

func (p Preparer) stream(ctx context.Context, gameID string, source *verifiedSource, reader io.Reader, expectedSize int64, extension string, maximum int64, readCode protocol.ErrorCode) (*Prepared, error) {
	staged, err := newStagedFile(p.StagingRoot)
	if err != nil {
		return nil, preparationError(gameID, protocol.CodeTransferFailed, nil)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			staged.cleanup()
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
	content := protocol.ContentIdentity{SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Size: written, Extension: extension}
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return nil, preparationError(gameID, readCode, nil)
	}
	if err := staged.file.Sync(); err != nil {
		return nil, preparationError(gameID, protocol.CodeTransferFailed, nil)
	}
	if err := staged.file.Close(); err != nil {
		staged.closed = true
		return nil, preparationError(gameID, protocol.CodeTransferFailed, nil)
	}
	staged.closed = true
	fileInfo, err := staged.verify()
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
	closed   bool
}

func newStagedFile(rootPath string) (*stagedFile, error) {
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
	if err := os.Chmod(rootPath, 0o700); err != nil {
		return nil, err
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
	file, err := os.CreateTemp(rootPath, ".fogcast-rom-*")
	if err != nil {
		return failRoot(err)
	}
	staged := &stagedFile{file: file, root: heldRoot, rootInfo: rootInfo, path: file.Name(), base: filepath.Base(file.Name())}
	if err := file.Chmod(0o600); err != nil {
		staged.cleanup()
		return nil, err
	}
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() {
		staged.cleanup()
		return nil, errors.Join(err, errors.New("staging file is not regular"))
	}
	staged.info = fileInfo
	if _, err := staged.verify(); err != nil {
		staged.cleanup()
		return nil, err
	}
	return staged, nil
}

func (s *stagedFile) verify() (fs.FileInfo, error) {
	currentRoot, err := os.Lstat(s.root.Name())
	if err != nil || currentRoot.Mode()&fs.ModeSymlink != 0 || !currentRoot.IsDir() || !os.SameFile(s.rootInfo, currentRoot) {
		return nil, errors.Join(err, errors.New("staging root changed"))
	}
	rootEntry, err := s.root.Lstat(s.base)
	if err != nil || !rootEntry.Mode().IsRegular() || !os.SameFile(s.info, rootEntry) {
		return nil, errors.Join(err, errors.New("staging file identity changed"))
	}
	pathEntry, err := os.Lstat(s.path)
	if err != nil || !pathEntry.Mode().IsRegular() || !os.SameFile(s.info, pathEntry) {
		return nil, errors.Join(err, errors.New("staging path identity changed"))
	}
	return pathEntry, nil
}

func (s *stagedFile) cleanup() {
	if !s.closed {
		_ = s.file.Close()
		s.closed = true
	}
	if entry, err := s.root.Lstat(s.base); err == nil && s.info != nil && os.SameFile(s.info, entry) {
		_ = s.root.Remove(s.base)
	}
	if entry, err := os.Lstat(s.path); err == nil && s.info != nil && os.SameFile(s.info, entry) {
		_ = os.Remove(s.path)
	}
	_ = s.root.Close()
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
