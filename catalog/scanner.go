package catalog

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/protocol"
)

const defaultMaxZIPEntries = 4096

const scanLeaseRetryInterval = 25 * time.Millisecond

const (
	reasonRootOffline       = "root_offline"
	reasonSourceDisappeared = "source_disappeared"
	reasonSourceUnreadable  = "source_unreadable"
	reasonZIPCorrupt        = "zip_corrupt"
	reasonZIPEncrypted      = "zip_encrypted"
	reasonZIPMultipleROMs   = "zip_multiple_roms"
	reasonZIPNested         = "zip_nested"
	reasonZIPNoROM          = "zip_no_rom"
	reasonZIPTooManyEntries = "zip_too_many_entries"
)

var errRemoteRootUnmounted = errors.New("catalog root is a remote UNC path; mount an absolute local path")

// SourceErrorCode maps a catalog source state to the public launch error code
// without exposing scanner diagnostics or host paths.
func SourceErrorCode(game Game) protocol.ErrorCode {
	if game.State == SourceStateInvalid && game.Kind == SourceKindZIP {
		switch game.Reason {
		case reasonSourceDisappeared, reasonSourceUnreadable:
			return protocol.CodeSourceUnavailable
		default:
			return protocol.CodeInvalidArchive
		}
	}
	return protocol.CodeSourceUnavailable
}

type Scanner struct {
	Store         *Store
	Registry      core.Registry
	Platforms     PlatformRegistry
	MaxZIPEntries int
	Debug         func(string)
	admit         func(context.Context) (func(), error)

	walkDir  func(fs.FS, string, fs.WalkDirFunc) error
	lstat    func(string) (fs.FileInfo, error)
	openFile func(*os.Root, string) (scannerSourceFile, error)
}

// SetAdmissionGate conditions catalog mutation without holding the gate while
// the scanner traverses and fingerprints the source tree.
func (s *Scanner) SetAdmissionGate(admit func(context.Context) (func(), error)) {
	s.admit = admit
}

type scannerSourceFile interface {
	io.Reader
	io.ReaderAt
	io.Closer
	Stat() (fs.FileInfo, error)
}

type scannerRoot struct {
	directory *os.Root
	info      fs.FileInfo
}

type scannerTraversalFS struct {
	root        *os.Root
	mu          sync.RWMutex
	directories map[string]fs.FileInfo
}

func newScannerTraversalFS(root *os.Root) *scannerTraversalFS {
	return &scannerTraversalFS{root: root, directories: make(map[string]fs.FileInfo)}
}

func (s *scannerTraversalFS) rememberDirectory(name string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.directories[name] = info
	s.mu.Unlock()
	return nil
}

func (s *scannerTraversalFS) expectedDirectory(name string) fs.FileInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.directories[name]
}

func (s *scannerTraversalFS) Open(name string) (fs.File, error) {
	checkedInfo, err := s.validateTraversalPath(name)
	if err != nil {
		return nil, err
	}
	file, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (fs.File, error) {
		_ = file.Close()
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(checkedInfo, openedInfo) {
		return fail(errors.Join(err, errors.New("traversal entry identity changed while opening")))
	}
	recheckedInfo, err := s.validateTraversalPath(name)
	if err != nil || !os.SameFile(openedInfo, recheckedInfo) {
		return fail(errors.Join(err, errors.New("traversal entry identity changed after opening")))
	}
	return file, nil
}

func (s *scannerTraversalFS) validateTraversalPath(name string) (fs.FileInfo, error) {
	if name == "." {
		info, err := s.root.Lstat(name)
		if err == nil {
			err = s.validateExpectedDirectory(name, info)
		}
		return info, err
	}
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrInvalid}
	}
	current := ""
	components := strings.Split(name, "/")
	var info fs.FileInfo
	for index, component := range components {
		current = pathpkg.Join(current, component)
		var err error
		info, err = s.root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if err := s.validateExpectedDirectory(current, info); err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, &fs.PathError{Op: "lstat", Path: current, Err: errors.New("symbolic link traversal is disabled")}
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, &fs.PathError{Op: "lstat", Path: current, Err: errors.New("traversal component is not a directory")}
		}
	}
	return info, nil
}

func (s *scannerTraversalFS) validateExpectedDirectory(name string, info fs.FileInfo) error {
	expected := s.expectedDirectory(name)
	if expected != nil && !os.SameFile(expected, info) {
		return &fs.PathError{Op: "lstat", Path: name, Err: errors.New("directory identity changed after parent enumeration")}
	}
	return nil
}

func (s Scanner) Scan(ctx context.Context, roots []Root) (ScanReport, error) {
	if s.Store == nil {
		return ScanReport{}, errors.New("catalog scanner requires a store")
	}
	if s.MaxZIPEntries < 0 || s.MaxZIPEntries > defaultMaxZIPEntries {
		return ScanReport{}, fmt.Errorf("catalog scanner MaxZIPEntries must be between 0 and %d", defaultMaxZIPEntries)
	}
	maximumZIPEntries := s.MaxZIPEntries
	if maximumZIPEntries == 0 {
		maximumZIPEntries = defaultMaxZIPEntries
	}

	report := ScanReport{Roots: make([]RootReport, 0, len(roots))}
	for _, root := range roots {
		if s.Debug != nil {
			s.Debug(fmt.Sprintf("scan root start: %s (%s)", root.ID, root.System))
		}
		if err := ctx.Err(); err != nil {
			return report, err
		}
		extensions, ok := s.extensions(root.System)
		if !ok {
			return report, fmt.Errorf("scan root %q: system %q is not registered", root.ID, root.System)
		}
		releaseLease, err := s.Store.acquireRootScanLease(ctx, root)
		if err != nil {
			return report, err
		}
		heldRoot, err := openScannerRoot(root.Path)
		if err != nil {
			rootReport, scanErr := s.Store.MarkRootOffline(ctx, root, reasonRootOffline)
			leaseErr := releaseLease()
			err := errors.Join(scanErr, leaseErr)
			if err != nil {
				return report, err
			}
			report.Roots = append(report.Roots, rootReport)
			if s.Debug != nil {
				s.Debug(fmt.Sprintf("scan root offline: %s", root.ID))
			}
			continue
		}
		rootReport, scanErr := s.scanRoot(ctx, root, heldRoot, extensions, maximumZIPEntries)
		closeErr := heldRoot.directory.Close()
		leaseErr := releaseLease()
		if scanErr != nil || closeErr != nil || leaseErr != nil {
			err := errors.Join(scanErr, closeErr, leaseErr)
			return report, err
		}
		report.Roots = append(report.Roots, rootReport)
		if s.Debug != nil {
			s.Debug(fmt.Sprintf("scan root complete: %s added=%d updated=%d unchanged=%d invalid=%d missing=%d", root.ID, rootReport.Added, rootReport.Updated, rootReport.Unchanged, rootReport.Invalid, rootReport.Missing))
		}
	}
	return report, nil
}

func (s Scanner) beginRootScanAfterContention(ctx context.Context, root Root) (*ScanSession, error) {
	for {
		session, err := s.Store.BeginRootScan(ctx, root)
		if err == nil {
			return session, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isSQLiteLockContention(err) {
			return nil, err
		}
		timer := time.NewTimer(scanLeaseRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func isSQLiteLockContention(err error) bool {
	var coded interface{ Code() int }
	if !errors.As(err, &coded) {
		return false
	}
	switch coded.Code() & 0xff {
	case 5, 6: // SQLITE_BUSY or SQLITE_LOCKED, including their extended codes.
		return true
	default:
		return false
	}
}

func (s *Scanner) SetDebug(debug func(string)) {
	s.Debug = debug
}

func (s *Scanner) extensions(system protocol.System) (map[string]struct{}, bool) {
	if platform, ok := s.Platforms.Lookup(system); ok {
		return platform.Extensions, true
	}
	spec, ok := s.Registry.Lookup(system)
	if !ok {
		return nil, false
	}
	return spec.Extensions, true
}

func isRemoteUNCPath(path string) bool {
	unified := strings.ReplaceAll(strings.TrimSpace(path), `\`, "/")
	if !strings.HasPrefix(unified, "//") || strings.HasPrefix(unified, "///") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(unified, "//"), "/")
	return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
}

func openScannerRoot(path string) (*scannerRoot, error) {
	if isRemoteUNCPath(path) {
		return nil, errRemoteRootUnmounted
	}
	entryInfo, err := os.Lstat(path)
	if err != nil || entryInfo.Mode()&fs.ModeSymlink != 0 || !entryInfo.IsDir() {
		return nil, errors.Join(err, errors.New("catalog root is not a real directory"))
	}
	directory, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	openedInfo, err := directory.Stat(".")
	if err != nil || !openedInfo.IsDir() || !os.SameFile(entryInfo, openedInfo) {
		_ = directory.Close()
		return nil, errors.Join(err, errors.New("catalog root identity changed while opening"))
	}
	return &scannerRoot{directory: directory, info: openedInfo}, nil
}

func (s Scanner) scanRoot(ctx context.Context, root Root, heldRoot *scannerRoot, extensions map[string]struct{}, maximumZIPEntries int) (RootReport, error) {
	walk := s.walkDir
	if walk == nil {
		walk = fs.WalkDir
	}
	lstat := s.lstat
	if lstat == nil {
		lstat = os.Lstat
	}
	openFile := s.openFile
	if openFile == nil {
		openFile = func(root *os.Root, name string) (scannerSourceFile, error) { return root.Open(name) }
	}

	traversal := newScannerTraversalFS(heldRoot.directory)
	skipped, err := s.collectSkippedCompanions(ctx, walk, traversal, heldRoot, extensions)
	if err != nil {
		return RootReport{}, err
	}
	candidates, err := s.collectRootCandidates(ctx, walk, lstat, openFile, traversal, heldRoot, root, extensions, maximumZIPEntries, skipped)
	if err != nil {
		if !heldRoot.matchesConfiguredPath(root.Path) {
			return s.Store.MarkRootOffline(ctx, root, reasonRootOffline)
		}
		return RootReport{}, fmt.Errorf("scan root %q: %w", root.ID, err)
	}
	if !heldRoot.matchesConfiguredPath(root.Path) {
		return s.Store.MarkRootOffline(ctx, root, reasonRootOffline)
	}
	release := func() {}
	if s.admit != nil {
		release, err = s.admit(ctx)
		if err != nil {
			return RootReport{}, err
		}
	}
	defer release()

	session, err := s.beginRootScanAfterContention(ctx, root)
	if err != nil {
		return RootReport{}, err
	}
	defer session.Rollback()
	for _, candidate := range candidates {
		if _, err := session.Observe(ctx, candidate); err != nil {
			return RootReport{}, err
		}
	}
	return session.Complete(ctx)
}

func (s Scanner) collectRootCandidates(
	ctx context.Context,
	walk func(fs.FS, string, fs.WalkDirFunc) error,
	lstat func(string) (fs.FileInfo, error),
	openFile func(*os.Root, string) (scannerSourceFile, error),
	traversal *scannerTraversalFS,
	heldRoot *scannerRoot,
	root Root,
	extensions map[string]struct{},
	maximumZIPEntries int,
	skipped skippedCompanions,
) ([]Candidate, error) {
	var candidates []Candidate
	err := walk(traversal, ".", func(fsPath string, entry fs.DirEntry, walkErr error) error {
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
			return traversal.rememberDirectory(fsPath, entry)
		}
		name := entry.Name()
		if name == ".DS_Store" || strings.HasPrefix(name, "._") {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(name))
		kind := SourceKindRaw
		if extension == ".zip" {
			kind = SourceKindZIP
		} else if _, ok := extensions[extension]; !ok {
			return nil
		}

		relativePath, err := NormalizeRelativePath(fsPath)
		if err != nil {
			return fmt.Errorf("normalize candidate in root %q: %w", root.ID, err)
		}
		sourcePath := filepath.Join(root.Path, filepath.FromSlash(relativePath))
		title := strings.TrimSuffix(name, filepath.Ext(name))
		candidate := Candidate{
			ID: GameID(root.System, root.ID, relativePath, title), Title: title,
			RelativePath: relativePath, System: root.System, Kind: kind,
		}

		info, err := lstat(sourcePath)
		if err != nil {
			if skipped.contains(relativePath, nil) {
				return nil
			}
			candidate.State = SourceStateInvalid
			candidate.Reason = sourceFailureReason(err)
			candidates = append(candidates, candidate)
			return nil
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		if skipped.contains(relativePath, info) {
			return nil
		}
		candidate.State = SourceStateAvailable

		source, openedInfo, err := openVerifiedCandidate(heldRoot.directory, relativePath, info, openFile)
		if err != nil {
			candidate.Fingerprint.SourceSize = info.Size()
			candidate.Fingerprint.ModifiedNS = info.ModTime().UnixNano()
			candidate.State = SourceStateInvalid
			candidate.Reason = sourceFailureReason(err)
		} else {
			candidate.Fingerprint.SourceSize = openedInfo.Size()
			candidate.Fingerprint.ModifiedNS = openedInfo.ModTime().UnixNano()
			if kind == SourceKindZIP {
				classifyZIP(source, openedInfo.Size(), extensions, maximumZIPEntries, &candidate)
			}
			if err := source.Close(); err != nil && candidate.State == SourceStateAvailable {
				candidate.State = SourceStateInvalid
				candidate.Reason = reasonSourceUnreadable
			}
		}
		candidates = append(candidates, candidate)
		return nil
	})
	return candidates, err
}

type skippedCompanions struct {
	paths map[string]struct{}
	infos []os.FileInfo
}

func (s skippedCompanions) contains(relativePath string, info os.FileInfo) bool {
	if _, ok := s.paths[relativePath]; ok {
		return true
	}
	if info == nil {
		return false
	}
	for _, skipped := range s.infos {
		if os.SameFile(skipped, info) {
			return true
		}
	}
	return false
}

func (s Scanner) collectSkippedCompanions(ctx context.Context, walk func(fs.FS, string, fs.WalkDirFunc) error, traversal *scannerTraversalFS, heldRoot *scannerRoot, extensions map[string]struct{}) (skippedCompanions, error) {
	skipped := skippedCompanions{paths: make(map[string]struct{})}
	err := walk(traversal, ".", func(fsPath string, entry fs.DirEntry, walkErr error) error {
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
			return traversal.rememberDirectory(fsPath, entry)
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".cue" && extension != ".gdi" {
			return nil
		}
		if _, ok := extensions[extension]; !ok {
			return nil
		}
		relativePath, err := NormalizeRelativePath(fsPath)
		if err != nil {
			return nil
		}
		source, err := heldRoot.directory.Open(relativePath)
		if err != nil {
			return nil
		}
		names := ReferencedMediaNames(entry.Name(), source)
		_ = source.Close()
		directory := pathpkg.Dir(relativePath)
		for _, name := range names {
			companion := name
			if directory != "." {
				companion = pathpkg.Join(directory, name)
			}
			normalized, err := NormalizeRelativePath(companion)
			if err != nil {
				continue
			}
			skipped.paths[normalized] = struct{}{}
			info, err := heldRoot.directory.Lstat(normalized)
			if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
				continue
			}
			skipped.infos = append(skipped.infos, info)
		}
		return nil
	})
	return skipped, err
}

func (r *scannerRoot) matchesConfiguredPath(path string) bool {
	configuredInfo, err := os.Lstat(path)
	if err != nil || configuredInfo.Mode()&fs.ModeSymlink != 0 || !configuredInfo.IsDir() || !os.SameFile(r.info, configuredInfo) {
		return false
	}
	openedInfo, err := r.directory.Stat(".")
	return err == nil && openedInfo.IsDir() && os.SameFile(r.info, openedInfo)
}

func openVerifiedCandidate(root *os.Root, relativePath string, expected fs.FileInfo, openFile func(*os.Root, string) (scannerSourceFile, error)) (scannerSourceFile, fs.FileInfo, error) {
	directoryPath, base := pathpkg.Split(relativePath)
	directoryPath = strings.TrimSuffix(directoryPath, "/")
	if directoryPath == "" {
		directoryPath = "."
	}

	checkedDirectory, err := validateCandidateDirectory(root, directoryPath)
	if err != nil {
		return nil, nil, err
	}
	candidateRoot := root
	if directoryPath != "." {
		candidateRoot, err = root.OpenRoot(directoryPath)
		if err != nil {
			return nil, nil, err
		}
		defer candidateRoot.Close()
		openedDirectory, err := candidateRoot.Stat(".")
		if err != nil || !os.SameFile(checkedDirectory, openedDirectory) {
			return nil, nil, errors.Join(err, errors.New("candidate directory identity changed"))
		}
	}

	currentInfo, err := root.Lstat(relativePath)
	if err != nil || currentInfo.Mode()&fs.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() || !os.SameFile(expected, currentInfo) {
		return nil, nil, errors.Join(err, errors.New("candidate identity changed before opening"))
	}
	source, err := openFile(candidateRoot, base)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (scannerSourceFile, fs.FileInfo, error) {
		_ = source.Close()
		return nil, nil, err
	}
	openedInfo, err := source.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(expected, openedInfo) {
		return fail(errors.Join(err, errors.New("candidate identity changed while opening")))
	}
	recheckedDirectory, err := validateCandidateDirectory(root, directoryPath)
	if err != nil || !os.SameFile(checkedDirectory, recheckedDirectory) {
		return fail(errors.Join(err, errors.New("candidate directory identity changed after opening")))
	}
	recheckedInfo, err := root.Lstat(relativePath)
	if err != nil || recheckedInfo.Mode()&fs.ModeSymlink != 0 || !recheckedInfo.Mode().IsRegular() || !os.SameFile(openedInfo, recheckedInfo) {
		return fail(errors.Join(err, errors.New("candidate identity changed after opening")))
	}
	return source, openedInfo, nil
}

func validateCandidateDirectory(root *os.Root, directoryPath string) (fs.FileInfo, error) {
	if directoryPath == "." {
		return root.Stat(".")
	}
	current := ""
	var info fs.FileInfo
	for _, component := range strings.Split(directoryPath, "/") {
		current = pathpkg.Join(current, component)
		var err error
		info, err = root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return nil, errors.New("candidate parent is not a real directory")
		}
	}
	return info, nil
}

func sourceFailureReason(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return reasonSourceDisappeared
	}
	return reasonSourceUnreadable
}

func classifyZIP(source io.ReaderAt, sourceSize int64, extensions map[string]struct{}, maximumEntries int, candidate *Candidate) {
	reader, err := zip.NewReader(source, sourceSize)
	if err != nil {
		invalidateZIP(candidate, reasonZIPCorrupt)
		return
	}

	candidate.Fingerprint.ZIPEntryCount = len(reader.File)
	if len(reader.File) > maximumEntries {
		invalidateZIP(candidate, reasonZIPTooManyEntries)
		return
	}

	var selected *zip.File
	for _, member := range reader.File {
		if member.Flags&1 != 0 {
			invalidateZIP(candidate, reasonZIPEncrypted)
			return
		}
		if member.FileInfo().IsDir() || strings.HasSuffix(member.Name, "/") {
			continue
		}
		extension := strings.ToLower(filepath.Ext(member.Name))
		if extension == ".zip" {
			invalidateZIP(candidate, reasonZIPNested)
			return
		}
		if _, ok := extensions[extension]; !ok {
			continue
		}
		if selected != nil {
			invalidateZIP(candidate, reasonZIPMultipleROMs)
			return
		}
		selected = member
	}
	if selected == nil {
		invalidateZIP(candidate, reasonZIPNoROM)
		return
	}
	if selected.UncompressedSize64 > math.MaxInt64 {
		invalidateZIP(candidate, reasonZIPCorrupt)
		return
	}
	candidate.Fingerprint.ZIPMember = selected.Name
	candidate.Fingerprint.ZIPSize = int64(selected.UncompressedSize64)
	candidate.Fingerprint.ZIPCRC32 = selected.CRC32
}

func invalidateZIP(candidate *Candidate, reason string) {
	candidate.State = SourceStateInvalid
	candidate.Reason = reason
}
