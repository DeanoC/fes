package rooms

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

const installedDir = ".installed"
const maxInstallBytes = 64 << 20
const maxInstallEntries = 2048

type roomSelection struct {
	Current  string `json:"current"`
	Previous string `json:"previous,omitempty"`
}

func readSelections(root string) (map[string]roomSelection, error) {
	data, err := readPackFile(os.DirFS(filepath.Join(root, installedDir)), "selected.json", 1<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]roomSelection{}, nil
	}
	if err != nil {
		return nil, err
	}
	var selections map[string]roomSelection
	if err = json.Unmarshal(data, &selections); err != nil {
		return nil, err
	}
	if selections == nil {
		return nil, errors.New("invalid room selections")
	}
	for id, s := range selections {
		if !ValidRoomID(id) || !validRevision(s.Current) || (s.Previous != "" && !validRevision(s.Previous)) {
			return nil, errors.New("invalid room selection identity")
		}
	}
	return selections, nil
}

func validRevision(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == sha256.Size && strings.ToLower(s) == s
}

func writeSelections(root string, selections map[string]roomSelection) error {
	dir := filepath.Join(root, installedDir)
	data, err := json.Marshal(selections)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".selection-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, "selected.json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Install accepts a directory or portable ZIP and creates a bounded, versioned snapshot. Manifest
// and every Lua file are checked before the atomic selection changes. Runtime
// callbacks are checked by the launcher with its actual services on opening.
func Install(root, source string) (Pack, error) {
	if strings.TrimSpace(root) == "" {
		return Pack{}, errors.New("room directory is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Pack{}, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return Pack{}, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return Pack{}, err
	}
	if info.Mode().IsRegular() {
		return installArchive(root, source)
	}
	rel, err := filepath.Rel(source, root)
	if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return Pack{}, errors.New("room source must not contain its installation directory")
	}
	unlock, err := lockInstall(root)
	if err != nil {
		return Pack{}, err
	}
	defer unlock()
	selections, err := readSelections(root)
	if err != nil {
		return Pack{}, err
	}
	stage, err := os.MkdirTemp(filepath.Join(root, installedDir), ".pack-")
	if err != nil {
		return Pack{}, err
	}
	defer os.RemoveAll(stage)
	hash := sha256.New()
	total, count := 0, 0
	vm := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer vm.Close()
	err = filepath.WalkDir(source, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		if rel == "." {
			if !entry.IsDir() {
				return errors.New("room source must be a directory")
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		if count > maxInstallEntries {
			return errors.New("room has too many entries")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("room symlink is not supported: %s", rel)
		}
		dest := filepath.Join(stage, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("room file must be regular: %s", rel)
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, maxAssetBytes+1))
		f.Close()
		if err != nil {
			return err
		}
		total += len(data)
		if len(data) > maxAssetBytes || total > maxInstallBytes {
			return errors.New("room exceeds installation size limit")
		}
		if strings.HasSuffix(rel, ".lua") {
			if len(data) > maxSourceBytes {
				return errors.New("room Lua source too large")
			}
			if _, err := vm.Load(bytes.NewReader(data), filepath.ToSlash(rel)); err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(rel), len(data))
		hash.Write(data)
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0444)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return Pack{}, err
	}
	pack := LoadPackFS(os.DirFS(stage), stage)
	if pack.Err != nil {
		return Pack{}, pack.Err
	}
	main, err := readPackFile(pack.FS, pack.Main, maxSourceBytes)
	if err != nil {
		return Pack{}, err
	}
	if _, err = vm.Load(bytes.NewReader(main), pack.Main); err != nil {
		return Pack{}, err
	}
	if pack.Icon != "" {
		if _, err = readPackFile(pack.FS, pack.Icon, maxAssetBytes); err != nil {
			return Pack{}, err
		}
	}
	revision := hex.EncodeToString(hash.Sum(nil))
	dest := filepath.Join(root, installedDir, revision)
	if _, err = os.Stat(dest); errors.Is(err, fs.ErrNotExist) {
		if err = os.Rename(stage, dest); err != nil {
			return Pack{}, err
		}
	} else if err != nil {
		return Pack{}, err
	}
	old := selections[pack.ID]
	if old.Current != revision {
		selections[pack.ID] = roomSelection{Current: revision, Previous: old.Current}
	}
	if err = writeSelections(root, selections); err != nil {
		return Pack{}, err
	}
	return installedPack(root, pack.ID, revision), nil
}

func installedPack(root, id, revision string) Pack {
	dir := filepath.Join(root, installedDir, revision)
	p := LoadPackFS(os.DirFS(dir), dir)
	p.Revision = revision
	if p.ID != id {
		p.Err = errors.New("installed room id differs from selection")
		p.ID = id
	}
	return p
}

func loadInstalled(root string) ([]Pack, error) {
	selected, err := readSelections(root)
	if err != nil {
		return nil, err
	}
	var packs []Pack
	for id, selection := range selected {
		packs = append(packs, installedPack(root, id, selection.Current))
	}
	sortPacks(packs)
	return packs, nil
}

// Rollback restores the previous installed version. expected, when nonempty,
// prevents a failed live trial from undoing a newer concurrently installed pack.
// A first installation is deselected so embedded/loose rooms can recover.
func Rollback(root, id, expected string) error {
	unlock, err := lockInstall(root)
	if err != nil {
		return err
	}
	defer unlock()
	selections, err := readSelections(root)
	if err != nil {
		return err
	}
	s, ok := selections[id]
	if !ok {
		return fmt.Errorf("room %s has no installed selection", id)
	}
	if expected != "" && s.Current != expected {
		return errors.New("room selection changed; refresh before rollback")
	}
	if s.Previous == "" {
		// The selection before a first install was the loose/embedded room,
		// or no room at all. Deselecting restores exactly that state.
		delete(selections, id)
	} else {
		p := installedPack(root, id, s.Previous)
		if p.Err != nil {
			return p.Err
		}
		selections[id] = roomSelection{Current: s.Previous}
	}
	return writeSelections(root, selections)
}
