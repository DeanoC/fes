package kitpackages

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
)

func (s *Store) Select(ctx context.Context, coreID, expectedID, id string) (Selection, error) {
	if !coreIDRE.MatchString(coreID) {
		return Selection{}, errors.New("kit packages: invalid core ID")
	}
	if !packageIDRE.MatchString(id) || (expectedID != "" && !packageIDRE.MatchString(expectedID)) {
		return Selection{}, ErrInvalidPackageID
	}
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return Selection{}, err
	}
	defer unlock()
	selections, err := s.selections(ctx, r)
	if err != nil {
		return Selection{}, err
	}
	inspection, _, err := s.read(ctx, r, id)
	if err != nil {
		return Selection{}, err
	}
	if inspection.Descriptor.Core.ID != coreID {
		return Selection{}, ErrCoreMismatch
	}
	current := ""
	index := -1
	for i, selection := range selections {
		if selection.CoreID == coreID {
			current, index = selection.PackageID, i
			break
		}
	}
	result := Selection{CoreID: coreID, PackageID: id}
	// The same completed change may be retried after a lost HTTP response.
	if current == id {
		return result, nil
	}
	if current != expectedID {
		return Selection{}, ErrSelectionConflict
	}
	if index < 0 {
		selections = append(selections, result)
	} else {
		selections[index] = result
	}
	sort.Slice(selections, func(i, j int) bool { return selections[i].CoreID < selections[j].CoreID })
	data, err := json.Marshal(selectionFile{Version: 1, Selections: selections})
	if err != nil {
		return Selection{}, err
	}
	if len(data) > 1024*1024 {
		return Selection{}, errors.New("kit packages: too many selections")
	}
	if err := atomicSelections(ctx, r, data); err != nil {
		return Selection{}, err
	}
	return result, nil
}

func (s *Store) Selections(ctx context.Context) ([]Selection, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.selections(ctx, r)
}

// SelectedPackages returns the selected launch inventory. Listing reuses a
// completed byte verification while the archive and member fingerprint still
// matches: size, mtime, ctime, device, inode, mode, and directory entries.
// Select, Path, and Read call read, which inspects the payload bytes. Launch
// inspects those bytes again before programming.
func (s *Store) SelectedPackages(ctx context.Context) ([]SelectedPackage, error) {
	r, unlock, err := s.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.selectedPackages(ctx, r)
}

func (s *Store) selectedPackages(ctx context.Context, r *os.Root) ([]SelectedPackage, error) {
	selections, err := readSelections(r)
	if err != nil {
		return nil, err
	}
	result := make([]SelectedPackage, 0, len(selections))
	for _, selection := range selections {
		inspection, err := s.listingInspection(ctx, r, selection.PackageID)
		if err != nil {
			return nil, err
		}
		if inspection.Descriptor.Core.ID != selection.CoreID {
			return nil, ErrCoreMismatch
		}
		result = append(result, SelectedPackage{Selection: selection, Inspection: inspection, Path: filepath.Join(s.root, "packages", selection.PackageID)})
	}
	return result, nil
}

// listingInspection reuses the last successful byte verification when size,
// mtime, ctime, device, inode, mode, and directory entries are unchanged.
// A miss inspects the payload once and records that fingerprint. On Linux a
// same-size rewrite that restores mtime still changes ctime, so the cached
// verification is not reused.
func (s *Store) listingInspection(ctx context.Context, r *os.Root, id string) (corepackage.Inspection, error) {
	if err := ctx.Err(); err != nil {
		return corepackage.Inspection{}, err
	}
	if key, err := packageStatKey(r, id); err == nil {
		if inspection, ok := s.cachedVerified(id, key); ok {
			return inspection, nil
		}
	}
	inspection, _, err := s.read(ctx, r, id)
	return inspection, err
}

func (s *Store) rememberVerified(id, key string, inspection corepackage.Inspection) {
	s.verifyMu.Lock()
	defer s.verifyMu.Unlock()
	if s.verified == nil {
		s.verified = make(map[string]verifiedPackage)
	}
	s.verified[id] = verifiedPackage{key: key, inspection: inspection.Clone()}
}

func (s *Store) cachedVerified(id, key string) (corepackage.Inspection, bool) {
	s.verifyMu.Lock()
	defer s.verifyMu.Unlock()
	hit, ok := s.verified[id]
	if !ok || hit.key != key {
		return corepackage.Inspection{}, false
	}
	return hit.inspection.Clone(), true
}

func packageStatKey(r *os.Root, id string) (string, error) {
	var b strings.Builder
	if err := writeStat(&b, r, filepath.Join("archives", id+".fcore")); err != nil {
		return "", err
	}
	dir := filepath.Join("packages", id)
	if err := writeStat(&b, r, dir); err != nil {
		return "", err
	}
	entries, err := directoryEntries(r, dir)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		fmt.Fprintf(&b, "entry %s\n", entry.Name())
		if err := writeStat(&b, r, filepath.Join(dir, entry.Name())); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func writeStat(b *strings.Builder, r *os.Root, name string) error {
	info, err := r.Lstat(name)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "%s size=%d mtime=%d mode=%d dev=%d ino=%d ctime=%d\n",
		name, info.Size(), info.ModTime().UnixNano(), uint64(info.Mode()), devOf(info), inodeOf(info), ctimeOf(info))
	return nil
}

func installedIDs(r *os.Root) ([]string, error) {
	packages, err := directoryEntries(r, "packages")
	if err != nil {
		return nil, err
	}
	archives, err := directoryEntries(r, "archives")
	if err != nil {
		return nil, err
	}
	packageSet := make(map[string]struct{}, len(packages))
	for _, entry := range packages {
		name := entry.Name()
		if stageRE.MatchString(name) {
			return nil, errors.New("kit packages: unexpected staging entry")
		}
		info, err := entry.Info()
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !packageIDRE.MatchString(name) {
			return nil, fmt.Errorf("kit packages: unexpected package entry %q", name)
		}
		packageSet[name] = struct{}{}
	}
	ids := make([]string, 0, len(archives))
	for _, entry := range archives {
		name := entry.Name()
		if archiveTemporaryRE.MatchString(name) {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
				return nil, errors.New("kit packages: invalid archive temporary")
			}
			continue
		}
		if len(name) != 70 || !strings.HasSuffix(name, ".fcore") || !packageIDRE.MatchString(name[:64]) {
			return nil, fmt.Errorf("kit packages: unexpected archive entry %q", name)
		}
		id := name[:64]
		if _, ok := packageSet[id]; !ok {
			return nil, errors.New("kit packages: archive and member inventories differ")
		}
		delete(packageSet, id)
		ids = append(ids, id)
	}
	if len(packageSet) != 0 {
		return nil, errors.New("kit packages: archive and member inventories differ")
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *Store) selections(ctx context.Context, r *os.Root) ([]Selection, error) {
	packages, err := s.selectedPackages(ctx, r)
	if err != nil {
		return nil, err
	}
	result := make([]Selection, 0, len(packages))
	for _, selected := range packages {
		result = append(result, selected.Selection)
	}
	return result, nil
}

func readSelections(r *os.Root) ([]Selection, error) {
	info, err := r.Lstat("selections.json")
	if errors.Is(err, os.ErrNotExist) {
		return []Selection{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > 1024*1024 {
		return nil, errors.New("kit packages: invalid selection file")
	}
	f, err := r.Open("selections.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("kit packages: selection file changed")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 1024*1024+1))
	decoder.DisallowUnknownFields()
	var document selectionFile
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF || document.Version != 1 || document.Selections == nil {
		return nil, errors.New("kit packages: invalid selection document")
	}
	seen := make(map[string]bool)
	for _, selection := range document.Selections {
		if !coreIDRE.MatchString(selection.CoreID) || !packageIDRE.MatchString(selection.PackageID) || seen[selection.CoreID] {
			return nil, errors.New("kit packages: invalid or duplicate selected core")
		}
		seen[selection.CoreID] = true

	}
	sort.Slice(document.Selections, func(i, j int) bool { return document.Selections[i].CoreID < document.Selections[j].CoreID })
	return document.Selections, nil
}

func atomicSelections(ctx context.Context, r *os.Root, data []byte) error {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	name := ".selections-" + hex.EncodeToString(random[:]) + ".tmp"
	f, err := r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer r.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.Rename(name, "selections.json"); err != nil {
		return err
	}
	return syncDirectory(r, ".")
}
