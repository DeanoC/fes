// Package storage manages remembered, read-only USB volumes and SMB shares.
// File access is rooted at a selected drive; it never accepts host file paths.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	MaxFileBytes         int64 = 32 << 20
	MaxExtendedFileBytes int64 = 33 << 20
)

type Drive struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	Label       string `json:"label"`
	Username    string `json:"username,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Password    string `json:"password,omitempty"`
	PasswordSet bool   `json:"password_set,omitempty"`
	BIOSFolder  string `json:"bios_folder,omitempty"`
	MediaFolder string `json:"media_folder,omitempty"`
	Saved       bool   `json:"saved"`
}

type Entry struct {
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
}

type Backend interface {
	Discover(context.Context) ([]Drive, error)
	Mount(context.Context, Drive, string) (string, error)
}

type Manager struct {
	ConfigPath          string
	MountRoot           string
	Backend             Backend
	persistSMBPasswords bool
	smbPasswords        map[string]string
	mu                  sync.Mutex
}

func New(configPath string) *Manager {
	return &Manager{ConfigPath: configPath, MountRoot: "/run/fogcast/drives", Backend: systemBackend{}, persistSMBPasswords: true}
}

// NewMemorySMBCredentials keeps SMB passwords only for this manager lifetime.
// Tenfoot uses it because its drive settings live on FAT, which cannot enforce
// file permissions. Existing plaintext passwords are migrated out on read.
func NewMemorySMBCredentials(configPath string) *Manager {
	manager := New(configPath)
	manager.persistSMBPasswords = false
	manager.smbPasswords = map[string]string{}
	return manager
}

func Public(d Drive) Drive {
	d.PasswordSet = d.Password != ""
	d.Password = ""
	return d
}

func normalize(d Drive) (Drive, error) {
	d.Source = strings.TrimSpace(d.Source)
	if strings.ContainsAny(d.Source+d.Username+d.Password+d.Domain, "\x00\r\n") {
		return d, errors.New("drive fields cannot contain line breaks")
	}
	switch d.Kind {
	case "smb":
		d.Source = strings.ReplaceAll(d.Source, `\`, "/")
		if strings.HasPrefix(d.Source, "smb://") {
			d.Source = "//" + strings.TrimPrefix(d.Source, "smb://")
		}
		parts := strings.Split(strings.TrimPrefix(d.Source, "//"), "/")
		if !strings.HasPrefix(d.Source, "//") || len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(parts[0], "@ ,") || parts[1] == "." || parts[1] == ".." {
			return d, errors.New("enter an SMB share as //server/share")
		}
	case "usb":
		if d.Source == "" || strings.ContainsAny(d.Source, "/\\ \t") || strings.HasPrefix(d.Source, "-") {
			return d, errors.New("USB drive requires its filesystem UUID")
		}
		d.Username, d.Domain, d.Password = "", "", ""
	default:
		return d, errors.New("choose USB or SMB storage")
	}
	for _, p := range []string{d.BIOSFolder, d.MediaFolder} {
		if _, err := relative(p); err != nil {
			return d, err
		}
	}
	sum := sha256.Sum256([]byte(d.Kind + "\x00" + d.Source))
	d.ID = hex.EncodeToString(sum[:12])
	if strings.TrimSpace(d.Label) == "" {
		d.Label = d.Source
	}
	d.Saved = true
	d.PasswordSet = false
	return d, nil
}

func relative(p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	if strings.ContainsAny(p, "\\\x00") || filepath.IsAbs(p) {
		return "", errors.New("folder must be relative to the selected drive")
	}
	p = filepath.Clean(p)
	if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return "", errors.New("path leaves the selected drive")
	}
	return p, nil
}

func (m *Manager) read() ([]Drive, error) {
	data, err := os.ReadFile(m.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return []Drive{}, nil
	}
	if err != nil {
		return nil, err
	}
	var drives []Drive
	if err = json.Unmarshal(data, &drives); err != nil {
		return nil, err
	}
	removePersistedPasswords := false
	for i, d := range drives {
		n, err := normalize(d)
		if err != nil || n.ID != d.ID {
			return nil, errors.New("saved drive configuration is invalid")
		}
		if !m.persistSMBPasswords {
			if d.Password != "" && n.Kind == "smb" && m.smbPasswords[n.ID] == "" {
				m.smbPasswords[n.ID] = d.Password
			}
			n.Password = m.smbPasswords[n.ID]
			n.PasswordSet = n.Password != ""
			removePersistedPasswords = removePersistedPasswords || d.Password != ""
		}
		drives[i] = n
	}
	if removePersistedPasswords {
		if err := m.write(drives); err != nil {
			return nil, err
		}
	}
	return drives, nil
}

func (m *Manager) write(drives []Drive) error {
	if m.ConfigPath == "" {
		return errors.New("drive settings path is unavailable")
	}
	dir := filepath.Dir(m.ConfigPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	saved := drives
	if !m.persistSMBPasswords {
		saved = append([]Drive(nil), drives...)
		for i := range saved {
			saved[i].Password = ""
			saved[i].PasswordSet = false
		}
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".drives-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), m.ConfigPath); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (m *Manager) List(ctx context.Context, discover bool) ([]Drive, error) {
	m.mu.Lock()
	drives, err := m.read()
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i, d := range drives {
		seen[d.ID] = true
		drives[i] = Public(d)
	}
	if discover {
		found, err := m.Backend.Discover(ctx)
		if err != nil {
			return drives, err
		}
		for _, d := range found {
			n, err := normalize(d)
			if err != nil {
				return nil, err
			}
			if !seen[n.ID] {
				n.Saved = false
				drives = append(drives, Public(n))
				seen[n.ID] = true
			}
		}
	}
	return drives, nil
}

func (m *Manager) Save(d Drive) (Drive, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, err := m.read()
	if err != nil {
		return Drive{}, err
	}
	normalized, err := normalize(d)
	if err != nil {
		return Drive{}, err
	}
	if d.ID != "" && d.ID != normalized.ID {
		for _, prior := range old {
			if prior.ID == normalized.ID {
				return Drive{}, errors.New("that drive is already saved; edit its existing connection")
			}
		}
	}
	for i, prior := range old {
		if prior.ID == normalized.ID || d.ID != "" && prior.ID == d.ID {
			if d.Password == "" && d.PasswordSet && prior.Kind == normalized.Kind && prior.Source == normalized.Source && prior.Username == normalized.Username && prior.Domain == normalized.Domain {
				normalized.Password = prior.Password
			}
			old[i] = normalized
			err = m.write(old)
			if !m.persistSMBPasswords && err == nil {
				if prior.ID != normalized.ID {
					delete(m.smbPasswords, prior.ID)
				}
				if normalized.Password == "" {
					delete(m.smbPasswords, normalized.ID)
				} else {
					m.smbPasswords[normalized.ID] = normalized.Password
				}
			}
			return Public(normalized), err
		}
	}
	old = append(old, normalized)
	err = m.write(old)
	if !m.persistSMBPasswords && err == nil && normalized.Password != "" {
		m.smbPasswords[normalized.ID] = normalized.Password
	}
	return Public(normalized), err
}

func (m *Manager) Forget(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	drives, err := m.read()
	if err != nil {
		return err
	}
	for i, d := range drives {
		if d.ID == id {
			if err := m.write(append(drives[:i], drives[i+1:]...)); err != nil {
				return err
			}
			delete(m.smbPasswords, id)
			return nil
		}
	}
	return os.ErrNotExist
}

func (m *Manager) drive(id string) (Drive, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	drives, err := m.read()
	if err != nil {
		return Drive{}, err
	}
	for _, d := range drives {
		if d.ID == id {
			return d, nil
		}
	}
	return Drive{}, os.ErrNotExist
}

func (m *Manager) root(ctx context.Context, id string) (*os.Root, error) {
	d, err := m.drive(id)
	if err != nil {
		return nil, err
	}
	path, err := m.Backend.Mount(ctx, d, filepath.Join(m.MountRoot, d.ID))
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(path)
}

func (m *Manager) Entries(ctx context.Context, id, path string) ([]Entry, error) {
	rel, err := relative(path)
	if err != nil {
		return nil, err
	}
	drive, err := m.drive(id)
	if err != nil {
		return nil, err
	}
	if drive.Kind == "smb" {
		return smbEntries(ctx, drive, rel)
	}
	root, err := m.root(ctx, id)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.IsDir() || info.Mode().IsRegular() {
			out = append(out, Entry{Name: e.Name(), Directory: info.IsDir(), Size: info.Size()})
		}
	}
	sortEntries(out)
	return out, nil
}

func (m *Manager) Open(ctx context.Context, id, path string) (io.ReadCloser, int64, error) {
	return m.OpenWithLimit(ctx, id, path, MaxFileBytes)
}

// OpenWithLimit reads a regular file with a caller-selected bound up to the
// extended archive ceiling. Ordinary imports should continue to use Open.
func (m *Manager) OpenWithLimit(ctx context.Context, id, path string, limit int64) (io.ReadCloser, int64, error) {
	if limit < 1 || limit > MaxExtendedFileBytes {
		return nil, 0, fmt.Errorf("file limit must be between 1 and %d bytes", MaxExtendedFileBytes)
	}
	rel, err := relative(path)
	if err != nil {
		return nil, 0, err
	}
	drive, err := m.drive(id)
	if err != nil {
		return nil, 0, err
	}
	if drive.Kind == "smb" {
		return smbOpen(ctx, drive, rel, limit)
	}
	root, err := m.root(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	defer root.Close()
	f, err := root.OpenFile(rel, os.O_RDONLY|nonblock, 0)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit) {
		err = fmt.Errorf("choose a regular file of 1–%d bytes", limit)
	}
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func sortEntries(out []Entry) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Directory != out[j].Directory {
			return out[i].Directory
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
}
