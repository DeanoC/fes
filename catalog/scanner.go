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

	"github.com/DeanoC/FogCast-POC/internal/core"
)

const defaultMaxZIPEntries = 4096

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

type Scanner struct {
	Store         *Store
	Registry      core.Registry
	MaxZIPEntries int

	walkDir  func(fs.FS, string, fs.WalkDirFunc) error
	lstat    func(string) (fs.FileInfo, error)
	openFile func(*os.Root, string) (scannerSourceFile, error)
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
	root *os.Root
}

func (s scannerTraversalFS) Open(name string) (fs.File, error) {
	checkedInfo, err := validateTraversalPath(s.root, name)
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
	recheckedInfo, err := validateTraversalPath(s.root, name)
	if err != nil || !os.SameFile(openedInfo, recheckedInfo) {
		return fail(errors.Join(err, errors.New("traversal entry identity changed after opening")))
	}
	return file, nil
}

func validateTraversalPath(root *os.Root, name string) (fs.FileInfo, error) {
	if name == "." {
		return root.Lstat(name)
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
		info, err = root.Lstat(current)
		if err != nil {
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
		if err := ctx.Err(); err != nil {
			return report, err
		}
		spec, ok := s.Registry.Lookup(root.System)
		if !ok {
			return report, fmt.Errorf("scan root %q: system %q is not registered", root.ID, root.System)
		}
		heldRoot, err := openScannerRoot(root.Path)
		if err != nil {
			rootReport, err := s.Store.MarkRootOffline(ctx, root, reasonRootOffline)
			if err != nil {
				return report, err
			}
			report.Roots = append(report.Roots, rootReport)
			continue
		}

		rootReport, scanErr := s.scanRoot(ctx, root, heldRoot, spec.Extensions, maximumZIPEntries)
		closeErr := heldRoot.directory.Close()
		if scanErr != nil || closeErr != nil {
			err := errors.Join(scanErr, closeErr)
			return report, err
		}
		report.Roots = append(report.Roots, rootReport)
	}
	return report, nil
}

func openScannerRoot(path string) (*scannerRoot, error) {
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
	session, err := s.Store.BeginRootScan(ctx, root)
	if err != nil {
		return RootReport{}, err
	}
	defer session.Rollback()

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

	err = walk(scannerTraversalFS{root: heldRoot.directory}, ".", func(fsPath string, entry fs.DirEntry, walkErr error) error {
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
			candidate.State = SourceStateInvalid
			candidate.Reason = sourceFailureReason(err)
			_, err = session.Observe(ctx, candidate)
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
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
		_, err = session.Observe(ctx, candidate)
		return err
	})
	if err != nil {
		if !heldRoot.matchesConfiguredPath(root.Path) {
			return s.rollbackAndMarkOffline(ctx, root, session)
		}
		return RootReport{}, fmt.Errorf("scan root %q: %w", root.ID, err)
	}
	if !heldRoot.matchesConfiguredPath(root.Path) {
		return s.rollbackAndMarkOffline(ctx, root, session)
	}
	return session.Complete(ctx)
}

func (r *scannerRoot) matchesConfiguredPath(path string) bool {
	configuredInfo, err := os.Lstat(path)
	if err != nil || configuredInfo.Mode()&fs.ModeSymlink != 0 || !configuredInfo.IsDir() || !os.SameFile(r.info, configuredInfo) {
		return false
	}
	openedInfo, err := r.directory.Stat(".")
	return err == nil && openedInfo.IsDir() && os.SameFile(r.info, openedInfo)
}

func (s Scanner) rollbackAndMarkOffline(ctx context.Context, root Root, session *ScanSession) (RootReport, error) {
	if err := session.Rollback(); err != nil {
		return RootReport{}, fmt.Errorf("roll back replaced root %q: %w", root.ID, err)
	}
	return s.Store.MarkRootOffline(ctx, root, reasonRootOffline)
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
