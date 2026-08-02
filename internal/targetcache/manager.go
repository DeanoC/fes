package targetcache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/protocol"
)

const (
	privateDirectoryMode = 0o700
	stalePartPrefix      = ".fogcast-"
	stalePartSuffix      = ".part"
)

type Config struct {
	Root         string
	ActiveRecord string
	MaxBytes     int64
}

type Resolved struct {
	Root string
	Path string
}

type openFileFunc func(string) (*os.File, error)

type managerOptions struct {
	openFile openFileFunc
}

type Option func(*managerOptions) error

// WithOpenFile replaces the content opener. It exists for deterministic
// verification instrumentation; production callers should use the default.
func WithOpenFile(openFile func(string) (*os.File, error)) Option {
	return func(options *managerOptions) error {
		if openFile == nil {
			return errors.New("target cache file opener cannot be nil")
		}
		options.openFile = openFile
		return nil
	}
}

type inventoryEntry struct {
	path          string
	accountedSize int64
}

type fileStamp struct {
	info os.FileInfo
}

type verificationMemo struct {
	stamp    fileStamp
	verified bool
	size     int64
}

type systemDirectory struct {
	path string
	info os.FileInfo
}

type Manager struct {
	config      Config
	root        string
	rootInfo    os.FileInfo
	extensions  map[protocol.System]map[string]struct{}
	directories map[protocol.System]systemDirectory
	openFile    openFileFunc

	mu      sync.Mutex
	entries map[inventoryKey]inventoryEntry
	memos   map[inventoryKey]verificationMemo
	usage   int64
}

func Open(config Config, registry core.Registry, options ...Option) (*Manager, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	resolvedRoot, rootInfo, err := prepareRoot(config.Root)
	if err != nil {
		return nil, err
	}

	settings := managerOptions{openFile: openRegularNoFollow}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("target cache option cannot be nil")
		}
		if err := option(&settings); err != nil {
			return nil, err
		}
	}

	manager := &Manager{
		config:      config,
		root:        resolvedRoot,
		rootInfo:    rootInfo,
		extensions:  cloneRegisteredExtensions(registry),
		directories: make(map[protocol.System]systemDirectory),
		openFile:    settings.openFile,
		entries:     make(map[inventoryKey]inventoryEntry),
		memos:       make(map[inventoryKey]verificationMemo),
	}
	if len(manager.extensions) == 0 {
		return nil, errors.New("target cache registry has no supported systems")
	}
	if err := manager.inventory(); err != nil {
		return nil, err
	}
	return manager, nil
}

func validateConfig(config Config) error {
	if config.Root == "" || strings.IndexByte(config.Root, 0) >= 0 || !filepath.IsAbs(config.Root) {
		return errors.New("target cache root must be an absolute path without NUL bytes")
	}
	if config.ActiveRecord != "" && (strings.IndexByte(config.ActiveRecord, 0) >= 0 || !filepath.IsAbs(config.ActiveRecord)) {
		return errors.New("target cache active record must be an absolute path without NUL bytes")
	}
	if config.MaxBytes <= 0 {
		return errors.New("target cache maximum size must be positive")
	}
	return nil
}

func prepareRoot(configured string) (string, os.FileInfo, error) {
	if err := os.MkdirAll(configured, privateDirectoryMode); err != nil {
		return "", nil, errors.New("create target cache root failed")
	}
	resolved, err := filepath.EvalSymlinks(configured)
	if err != nil {
		return "", nil, errors.New("resolve target cache root failed")
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", nil, errors.New("make target cache root absolute failed")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.IsDir() {
		return "", nil, errors.New("target cache root is not a directory")
	}
	if err := os.Chmod(resolved, privateDirectoryMode); err != nil {
		return "", nil, errors.New("secure target cache root permissions failed")
	}
	info, err = os.Lstat(resolved)
	if err != nil {
		return "", nil, errors.New("inspect target cache root failed")
	}
	return filepath.Clean(resolved), info, nil
}

func cloneRegisteredExtensions(registry core.Registry) map[protocol.System]map[string]struct{} {
	result := make(map[protocol.System]map[string]struct{}, 2)
	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES} {
		spec, ok := registry.Lookup(system)
		if !ok {
			continue
		}
		allowed := make(map[string]struct{}, len(spec.Extensions))
		for extension := range spec.Extensions {
			allowed[extension] = struct{}{}
		}
		result[system] = allowed
	}
	return result
}

func (m *Manager) inventory() error {
	rootEntries, err := os.ReadDir(m.root)
	if err != nil {
		return errors.New("read target cache root failed")
	}
	presentSystems := make(map[protocol.System]bool, len(m.extensions))
	for _, directoryEntry := range rootEntries {
		path := filepath.Join(m.root, directoryEntry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return errors.New("inspect target cache root entry failed")
		}
		system := protocol.System(directoryEntry.Name())
		if _, registered := m.extensions[system]; registered && info.IsDir() {
			presentSystems[system] = true
			if err := os.Chmod(path, privateDirectoryMode); err != nil {
				return errors.New("secure target cache system directory permissions failed")
			}
			info, err = os.Lstat(path)
			if err != nil {
				return errors.New("inspect target cache system directory failed")
			}
			m.directories[system] = systemDirectory{path: path, info: info}
			continue
		}
		m.usage += knownSize(info)
	}

	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES} {
		allowed, registered := m.extensions[system]
		if !registered {
			continue
		}
		if !presentSystems[system] {
			path := filepath.Join(m.root, string(system))
			if _, exists := m.directories[system]; !exists {
				if err := os.Mkdir(path, privateDirectoryMode); err != nil {
					// An invalid direct entry already occupies the registered name.
					if !os.IsExist(err) {
						return errors.New("create target cache system directory failed")
					}
					continue
				}
				info, err := os.Lstat(path)
				if err != nil {
					return errors.New("inspect created target cache system directory failed")
				}
				m.directories[system] = systemDirectory{path: path, info: info}
			}
		}
		directory, ready := m.directories[system]
		if !ready {
			continue
		}
		if err := m.inventorySystem(system, directory.path, allowed); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) inventorySystem(system protocol.System, directory string, allowed map[string]struct{}) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return errors.New("read target cache system directory failed")
	}
	for _, directoryEntry := range entries {
		path := filepath.Join(directory, directoryEntry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return errors.New("inspect target cache entry failed")
		}
		if info.Mode().IsRegular() && isRecognizedStalePart(directoryEntry.Name()) {
			if err := os.Remove(path); err != nil {
				return errors.New("remove stale target cache part failed")
			}
			continue
		}
		size := knownSize(info)
		m.usage += size
		key, validName := parseInventoryName(directoryEntry.Name(), allowed)
		if !validName || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > protocol.MaxContentBytes {
			continue
		}
		m.entries[makeInventoryKey(system, key)] = inventoryEntry{path: path, accountedSize: size}
	}
	return nil
}

func isRecognizedStalePart(name string) bool {
	return strings.HasPrefix(name, stalePartPrefix) && strings.HasSuffix(name, stalePartSuffix) && len(name) > len(stalePartPrefix)+len(stalePartSuffix)
}

func knownSize(info os.FileInfo) int64 {
	if info == nil || info.Size() <= 0 {
		return 0
	}
	return info.Size()
}

func (m *Manager) Probe(ctx context.Context, system protocol.System, key protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError) {
	id, path, apiErr := m.checkedPath(system, key)
	if apiErr != nil {
		return absentProbe(), apiErr
	}
	if err := ctx.Err(); err != nil {
		return absentProbe(), internalAPIError("cache verification was canceled")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[id]
	if !ok {
		return absentProbe(), nil
	}
	if entry.path != path || !m.directoriesIntact(system) {
		return absentProbe(), internalAPIError("cache directory identity changed")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			m.removeEntry(id, entry)
			return absentProbe(), nil
		}
		return absentProbe(), internalAPIError("cache entry cannot be inspected")
	}
	if !m.reconcileStructuralEntry(id, &entry, info) {
		return absentProbe(), nil
	}
	stamp := fileStamp{info: info}
	if memo, ok := m.memos[id]; ok && sameStamp(memo.stamp, stamp) {
		if !memo.verified {
			return absentProbe(), nil
		}
		return presentProbe(system, key, memo.size), nil
	}
	delete(m.memos, id)

	verified, size, stableStamp, apiErr := m.verify(ctx, id, entry, stamp)
	if apiErr != nil {
		return absentProbe(), apiErr
	}
	if stableStamp.info == nil {
		return absentProbe(), nil
	}
	m.memos[id] = verificationMemo{stamp: stableStamp, verified: verified, size: size}
	if !verified {
		return absentProbe(), nil
	}
	return presentProbe(system, key, size), nil
}

func (m *Manager) Resolve(ctx context.Context, system protocol.System, content protocol.ContentIdentity) (Resolved, *protocol.APIError) {
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return Resolved{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "content identity is invalid"}
	}
	_, path, apiErr := m.checkedPath(system, content.Key())
	if apiErr != nil {
		return Resolved{}, apiErr
	}
	response, apiErr := m.Probe(ctx, system, content.Key())
	if apiErr != nil {
		return Resolved{}, apiErr
	}
	if !response.Present || response.Content == nil || response.Content.Size != content.Size {
		return Resolved{}, &protocol.APIError{
			Code:    protocol.CodeContentNotCached,
			Message: "requested content is not present in the verified cache",
		}
	}
	return Resolved{Root: m.root, Path: path}, nil
}

func (m *Manager) Usage() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage
}

func (m *Manager) directoriesIntact(system protocol.System) bool {
	root, err := os.Lstat(m.root)
	if err != nil || !root.IsDir() || !os.SameFile(m.rootInfo, root) {
		return false
	}
	directory, ok := m.directories[system]
	if !ok {
		return false
	}
	current, err := os.Lstat(directory.path)
	return err == nil && current.IsDir() && os.SameFile(directory.info, current)
}

func (m *Manager) reconcileStructuralEntry(id inventoryKey, entry *inventoryEntry, info os.FileInfo) bool {
	newSize := knownSize(info)
	if newSize != entry.accountedSize {
		m.usage += newSize - entry.accountedSize
		entry.accountedSize = newSize
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > protocol.MaxContentBytes {
		delete(m.entries, id)
		delete(m.memos, id)
		return false
	}
	m.entries[id] = *entry
	return true
}

func (m *Manager) removeEntry(id inventoryKey, entry inventoryEntry) {
	m.usage -= entry.accountedSize
	if m.usage < 0 {
		m.usage = 0
	}
	delete(m.entries, id)
	delete(m.memos, id)
}

func (m *Manager) verify(ctx context.Context, id inventoryKey, entry inventoryEntry, before fileStamp) (bool, int64, fileStamp, *protocol.APIError) {
	file, err := m.openFile(entry.path)
	if err != nil {
		m.reconcileAfterOpenFailure(id, entry)
		if _, stillPresent := m.entries[id]; !stillPresent {
			return false, 0, fileStamp{}, nil
		}
		return false, 0, fileStamp{}, internalAPIError("cache entry cannot be opened")
	}
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !sameStamp(before, fileStamp{info: openedInfo}) {
		_ = file.Close()
		m.reconcileAfterOpenFailure(id, entry)
		return false, 0, fileStamp{}, nil
	}

	hasher := sha256.New()
	buffer := make([]byte, 64<<10)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return false, 0, fileStamp{}, internalAPIError("cache verification was canceled")
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			size += int64(count)
			if size > protocol.MaxContentBytes {
				_ = file.Close()
				m.reconcileAfterOpenFailure(id, entry)
				return false, 0, fileStamp{}, nil
			}
			_, _ = hasher.Write(buffer[:count])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			_ = file.Close()
			return false, 0, fileStamp{}, internalAPIError("cache entry cannot be read")
		}
	}
	afterOpenInfo, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil {
		return false, 0, fileStamp{}, internalAPIError("cache entry verification could not finish")
	}
	afterPathInfo, err := os.Lstat(entry.path)
	if err != nil || !afterPathInfo.Mode().IsRegular() || !sameStamp(before, fileStamp{info: afterOpenInfo}) || !sameStamp(before, fileStamp{info: afterPathInfo}) || size != before.info.Size() {
		m.reconcileAfterOpenFailure(id, entry)
		return false, 0, fileStamp{}, nil
	}
	verified := fmt.Sprintf("%x", hasher.Sum(nil)) == id.digest
	return verified, size, fileStamp{info: afterPathInfo}, nil
}

func (m *Manager) reconcileAfterOpenFailure(id inventoryKey, previous inventoryEntry) {
	info, err := os.Lstat(previous.path)
	if err != nil {
		if os.IsNotExist(err) {
			m.removeEntry(id, previous)
		}
		return
	}
	entry := previous
	m.reconcileStructuralEntry(id, &entry, info)
}

func sameStamp(left, right fileStamp) bool {
	if left.info == nil || right.info == nil {
		return false
	}
	return left.info.Size() == right.info.Size() &&
		left.info.Mode() == right.info.Mode() &&
		left.info.ModTime().Equal(right.info.ModTime()) &&
		os.SameFile(left.info, right.info)
}

func presentProbe(system protocol.System, key protocol.ContentKey, size int64) protocol.CacheProbeResponse {
	returnedSystem := system
	content := protocol.ContentIdentity{SHA256: key.SHA256, Size: size, Extension: key.Extension}
	return protocol.CacheProbeResponse{Present: true, System: &returnedSystem, Content: &content}
}

func openRegularNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
