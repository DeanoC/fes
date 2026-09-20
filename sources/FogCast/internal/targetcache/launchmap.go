package targetcache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	LaunchMapName     = "launch-map.json"
	launchMapFormat   = 1
	maxLaunchMapBytes = 4 << 20
)

// LaunchMap is a durable game-id → verified cache identity index written beside
// the ROM cache. It is not inventory and is never launched from without Probe.
type LaunchMap struct {
	path string
	mu   sync.Mutex
	file launchMapFile
}

type launchMapFile struct {
	Format  int                       `json:"format"`
	Entries map[string]LaunchMapEntry `json:"entries"`
}

type LaunchMapEntry struct {
	GameID  string                   `json:"game_id"`
	System  protocol.System          `json:"system"`
	Content protocol.ContentIdentity `json:"content"`
}

func OpenLaunchMap(path string) (*LaunchMap, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, errors.New("launch map path must be absolute")
	}
	if filepath.Base(path) != LaunchMapName {
		return nil, errors.New("launch map name is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(path), privateDirectoryMode); err != nil {
		return nil, err
	}
	m := &LaunchMap{path: path, file: launchMapFile{Format: launchMapFormat, Entries: map[string]LaunchMapEntry{}}}
	m.load()
	return m, nil
}

func (m *LaunchMap) load() {
	info, err := os.Lstat(m.path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxLaunchMapBytes {
		return
	}
	data, err := os.ReadFile(m.path)
	if err != nil || len(data) == 0 || len(data) > maxLaunchMapBytes {
		return
	}
	var file launchMapFile
	if json.Unmarshal(data, &file) != nil || file.Format != launchMapFormat {
		return
	}
	if file.Entries == nil {
		file.Entries = map[string]LaunchMapEntry{}
	}
	m.file = file
}

func (m *LaunchMap) Remember(gameID string, system protocol.System, content protocol.ContentIdentity) error {
	if m == nil {
		return nil
	}
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateSystem(system) != nil || protocol.ValidateContentIdentity(content) != nil {
		return errors.New("launch map entry is invalid")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file.Entries == nil {
		m.file.Entries = map[string]LaunchMapEntry{}
	}
	m.file.Format = launchMapFormat
	m.file.Entries[gameID] = LaunchMapEntry{GameID: gameID, System: system, Content: content}
	return m.writeLocked()
}

func (m *LaunchMap) Lookup(gameID string) (LaunchMapEntry, bool) {
	if m == nil {
		return LaunchMapEntry{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.file.Entries[gameID]
	if !ok || entry.GameID != gameID {
		return LaunchMapEntry{}, false
	}
	return entry, true
}

func (m *LaunchMap) Forget(gameID string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.file.Entries[gameID]; !ok {
		return nil
	}
	delete(m.file.Entries, gameID)
	return m.writeLocked()
}

func (m *LaunchMap) writeLocked() error {
	m.file.Format = launchMapFormat
	if m.file.Entries == nil {
		m.file.Entries = map[string]LaunchMapEntry{}
	}
	data, err := json.Marshal(m.file)
	if err != nil {
		return err
	}
	if len(data) > maxLaunchMapBytes {
		return errors.New("launch map exceeds size limit")
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), privateFileMode); err != nil {
		return err
	}
	if err := os.Rename(tmp, m.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
