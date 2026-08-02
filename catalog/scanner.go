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

	walkDir func(string, fs.WalkDirFunc) error
	lstat   func(string) (fs.FileInfo, error)
	openRaw func(string) (io.ReadCloser, error)
}

func (s Scanner) Scan(ctx context.Context, roots []Root) (ScanReport, error) {
	if s.Store == nil {
		return ScanReport{}, errors.New("catalog scanner requires a store")
	}
	if s.MaxZIPEntries < 0 {
		return ScanReport{}, errors.New("catalog scanner MaxZIPEntries must not be negative")
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
		if !rootDirectoryOpens(root.Path) {
			rootReport, err := s.Store.MarkRootOffline(ctx, root, reasonRootOffline)
			if err != nil {
				return report, err
			}
			report.Roots = append(report.Roots, rootReport)
			continue
		}

		rootReport, err := s.scanRoot(ctx, root, spec.Extensions, maximumZIPEntries)
		if err != nil {
			return report, err
		}
		report.Roots = append(report.Roots, rootReport)
	}
	return report, nil
}

func rootDirectoryOpens(path string) bool {
	entryInfo, err := os.Lstat(path)
	if err != nil || entryInfo.Mode()&fs.ModeSymlink != 0 || !entryInfo.IsDir() {
		return false
	}
	directory, err := os.Open(path)
	if err != nil {
		return false
	}
	defer directory.Close()
	openedInfo, err := directory.Stat()
	return err == nil && openedInfo.IsDir() && os.SameFile(entryInfo, openedInfo)
}

func (s Scanner) scanRoot(ctx context.Context, root Root, extensions map[string]struct{}, maximumZIPEntries int) (RootReport, error) {
	session, err := s.Store.BeginRootScan(ctx, root)
	if err != nil {
		return RootReport{}, err
	}
	defer session.Rollback()

	walk := s.walkDir
	if walk == nil {
		walk = filepath.WalkDir
	}
	lstat := s.lstat
	if lstat == nil {
		lstat = os.Lstat
	}
	openRaw := s.openRaw
	if openRaw == nil {
		openRaw = func(path string) (io.ReadCloser, error) { return os.Open(path) }
	}

	err = walk(root.Path, func(path string, entry fs.DirEntry, walkErr error) error {
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

		relativePath, err := filepath.Rel(root.Path, path)
		if err != nil {
			return fmt.Errorf("make path relative for root %q: %w", root.ID, err)
		}
		relativePath, err = NormalizeRelativePath(filepath.ToSlash(relativePath))
		if err != nil {
			return fmt.Errorf("normalize candidate in root %q: %w", root.ID, err)
		}
		title := strings.TrimSuffix(name, filepath.Ext(name))
		candidate := Candidate{
			ID: GameID(root.System, root.ID, relativePath, title), Title: title,
			RelativePath: relativePath, System: root.System, Kind: kind,
		}

		info, err := lstat(path)
		if err != nil {
			candidate.State = SourceStateInvalid
			candidate.Reason = sourceFailureReason(err)
			_, err = session.Observe(ctx, candidate)
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		candidate.Fingerprint.SourceSize = info.Size()
		candidate.Fingerprint.ModifiedNS = info.ModTime().UnixNano()
		candidate.State = SourceStateAvailable

		if kind == SourceKindZIP {
			classifyZIP(path, extensions, maximumZIPEntries, &candidate)
		} else {
			reader, err := openRaw(path)
			if err != nil {
				candidate.State = SourceStateInvalid
				candidate.Reason = sourceFailureReason(err)
			} else if err := reader.Close(); err != nil {
				candidate.State = SourceStateInvalid
				candidate.Reason = reasonSourceUnreadable
			}
		}
		_, err = session.Observe(ctx, candidate)
		return err
	})
	if err != nil {
		return RootReport{}, fmt.Errorf("scan root %q: %w", root.ID, err)
	}
	return session.Complete(ctx)
}

func sourceFailureReason(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return reasonSourceDisappeared
	}
	return reasonSourceUnreadable
}

func classifyZIP(path string, extensions map[string]struct{}, maximumEntries int, candidate *Candidate) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		invalidateZIP(candidate, reasonZIPCorrupt)
		return
	}
	defer reader.Close()

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
