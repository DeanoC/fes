package fogcast

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type libraryOverlayFile struct {
	AttractIdleSeconds *int     `json:"attract_idle_seconds,omitempty"`
	PreferredRegions   []string `json:"preferred_regions,omitempty"`
}

func libraryOverlayPath(paths Paths) string {
	if strings.TrimSpace(paths.LibrarySettings) != "" {
		return paths.LibrarySettings
	}
	if paths.UserLibrary != "" {
		return filepath.Join(filepath.Dir(paths.UserLibrary), "library-settings.json")
	}
	return ""
}

func loadLibraryOverlay(path string) (LibraryConfig, bool, error) {
	if path == "" {
		return LibraryConfig{}, false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return LibraryConfig{}, false, nil
		}
		return LibraryConfig{}, false, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 16<<10))
	if err != nil {
		return LibraryConfig{}, false, err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return LibraryConfig{}, false, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return LibraryConfig{}, false, errors.New("library settings overlay must contain one JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var raw libraryOverlayFile
	if err := decoder.Decode(&raw); err != nil {
		return LibraryConfig{}, false, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return LibraryConfig{}, false, errors.New("library settings overlay must contain one JSON object")
	}
	next := LibraryConfig{PreferredRegions: raw.PreferredRegions}
	if raw.AttractIdleSeconds != nil {
		next.AttractIdleSeconds = *raw.AttractIdleSeconds
	}
	normalized, err := NormalizeLibraryConfig(next)
	if err != nil {
		return LibraryConfig{}, false, err
	}
	return normalized, true, nil
}

var libraryOverlaySaveHook func()

func saveLibraryOverlay(path string, settings LibraryConfig) error {
	if path == "" {
		return errors.New("library settings overlay path is empty")
	}
	normalized, err := NormalizeLibraryConfig(settings)
	if err != nil {
		return err
	}
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	body, err := json.Marshal(libraryOverlayFile{
		AttractIdleSeconds: &normalized.AttractIdleSeconds,
		PreferredRegions:   normalized.PreferredRegions,
	})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "library-settings-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if libraryOverlayPublishErrorHook != nil {
		if err := libraryOverlayPublishErrorHook(); err != nil {
			return err
		}
	}
	if err := ensurePrivateRegularFile(path); err != nil {
		return err
	}
	if libraryOverlaySaveHook != nil {
		libraryOverlaySaveHook()
	}
	return nil
}

var libraryOverlayPublishErrorHook func() error

func snapshotLibraryOverlay(path string) ([]byte, bool, error) {
	return snapshotPrivateFile(path)
}

func restoreLibraryOverlay(path string, body []byte, existed bool) error {
	return restorePrivateFile(path, body, existed)
}
