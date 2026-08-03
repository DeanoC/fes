package targetcache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/protocol"
	"golang.org/x/sys/unix"
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
	logger   *slog.Logger
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

// WithLogger selects the sink for sanitized cache inventory diagnostics.
func WithLogger(logger *slog.Logger) Option {
	return func(options *managerOptions) error {
		if logger == nil {
			return errors.New("target cache logger cannot be nil")
		}
		options.logger = logger
		return nil
	}
}

type inventoryEntry struct {
	name          string
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
	root *os.Root
}

type Manager struct {
	config      Config
	root        string
	rootInfo    os.FileInfo
	rootHandle  *os.Root
	extensions  map[protocol.System]map[string]struct{}
	directories map[protocol.System]systemDirectory
	openFile    openFileFunc
	logger      *slog.Logger

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

	settings := managerOptions{logger: slog.Default()}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("target cache option cannot be nil")
		}
		if err := option(&settings); err != nil {
			return nil, err
		}
	}

	rootHandle, err := os.OpenRoot(resolvedRoot)
	if err != nil {
		return nil, errors.New("open target cache root failed")
	}
	boundRootInfo, err := rootHandle.Stat(".")
	if err != nil || !sameFileInfo(rootInfo, boundRootInfo) {
		_ = rootHandle.Close()
		return nil, errors.New("bind target cache root failed")
	}
	if err := chmodOpenedRoot(rootHandle, privateDirectoryMode); err != nil {
		_ = rootHandle.Close()
		return nil, errors.New("secure target cache root permissions failed")
	}
	boundRootInfo, err = rootHandle.Stat(".")
	currentRootInfo, currentRootErr := os.Lstat(resolvedRoot)
	if err != nil || currentRootErr != nil || !currentRootInfo.IsDir() || !sameFileInfo(boundRootInfo, currentRootInfo) {
		_ = rootHandle.Close()
		return nil, errors.New("recheck target cache root failed")
	}

	manager := &Manager{
		config:      config,
		root:        resolvedRoot,
		rootInfo:    boundRootInfo,
		rootHandle:  rootHandle,
		extensions:  cloneRegisteredExtensions(registry),
		directories: make(map[protocol.System]systemDirectory),
		openFile:    settings.openFile,
		logger:      settings.logger,
		entries:     make(map[inventoryKey]inventoryEntry),
		memos:       make(map[inventoryKey]verificationMemo),
	}
	if len(manager.extensions) == 0 {
		_ = rootHandle.Close()
		return nil, errors.New("target cache registry has no supported systems")
	}
	if err := manager.inventory(); err != nil {
		manager.closeDirectoryHandles()
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
	rootEntries, err := readDirect(m.rootHandle)
	if err != nil {
		return errors.New("read target cache root failed")
	}
	for _, directoryEntry := range rootEntries {
		name := directoryEntry.Name()
		info, err := m.rootHandle.Lstat(name)
		if err != nil {
			return errors.New("inspect target cache root entry failed")
		}
		system := protocol.System(name)
		if _, registered := m.extensions[system]; registered && info.IsDir() {
			directory, err := m.bindSystemDirectory(system, info)
			if err != nil {
				return err
			}
			m.directories[system] = directory
			continue
		}
		m.usage += knownSize(info)
		if _, registered := m.extensions[system]; registered {
			m.logExcluded(system, "invalid-system-directory", info)
		} else {
			m.logExcluded("", "unfamiliar", info)
		}
	}

	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES} {
		allowed, registered := m.extensions[system]
		if !registered {
			continue
		}
		if _, exists := m.directories[system]; !exists {
			if err := m.rootHandle.Mkdir(string(system), privateDirectoryMode); err != nil {
				// An invalid direct entry already occupies the registered name.
				if !os.IsExist(err) {
					return errors.New("create target cache system directory failed")
				}
				continue
			}
			info, err := m.rootHandle.Lstat(string(system))
			if err != nil {
				return errors.New("inspect created target cache system directory failed")
			}
			directory, err := m.bindSystemDirectory(system, info)
			if err != nil {
				return err
			}
			m.directories[system] = directory
		}
		directory, ready := m.directories[system]
		if !ready {
			continue
		}
		if err := m.inventorySystem(system, directory, allowed); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) bindSystemDirectory(system protocol.System, expected os.FileInfo) (systemDirectory, error) {
	root, err := m.rootHandle.OpenRoot(string(system))
	if err != nil {
		return systemDirectory{}, errors.New("open target cache system directory failed")
	}
	boundInfo, err := root.Stat(".")
	currentInfo, currentErr := m.rootHandle.Lstat(string(system))
	if err != nil || currentErr != nil || !currentInfo.IsDir() || !sameFileInfo(expected, boundInfo) || !sameFileInfo(boundInfo, currentInfo) {
		_ = root.Close()
		return systemDirectory{}, errors.New("bind target cache system directory failed")
	}
	if err := chmodOpenedRoot(root, privateDirectoryMode); err != nil {
		_ = root.Close()
		return systemDirectory{}, errors.New("secure target cache system directory permissions failed")
	}
	boundInfo, err = root.Stat(".")
	currentInfo, currentErr = m.rootHandle.Lstat(string(system))
	if err != nil || currentErr != nil || !currentInfo.IsDir() || !sameFileInfo(boundInfo, currentInfo) {
		_ = root.Close()
		return systemDirectory{}, errors.New("recheck target cache system directory failed")
	}
	return systemDirectory{path: filepath.Join(m.root, string(system)), info: boundInfo, root: root}, nil
}

func (m *Manager) inventorySystem(system protocol.System, directory systemDirectory, allowed map[string]struct{}) error {
	entries, err := readDirect(directory.root)
	if err != nil {
		return errors.New("read target cache system directory failed")
	}
	for _, directoryEntry := range entries {
		name := directoryEntry.Name()
		path := filepath.Join(directory.path, name)
		info, err := directory.root.Lstat(name)
		if err != nil {
			return errors.New("inspect target cache entry failed")
		}
		if info.Mode().IsRegular() && isRecognizedStalePart(name) {
			m.usage += knownSize(info)
			m.logExcluded(system, "stale-part-retained", info)
			continue
		}
		size := knownSize(info)
		m.usage += size
		key, validName := parseInventoryName(name, allowed)
		if !validName {
			m.logExcluded(system, "unfamiliar", info)
			continue
		}
		if !info.Mode().IsRegular() {
			m.logExcluded(system, "invalid-type", info)
			continue
		}
		if info.Size() < 1 || info.Size() > protocol.MaxContentBytes {
			m.logExcluded(system, "invalid-size", info)
			continue
		}
		m.entries[makeInventoryKey(system, key)] = inventoryEntry{name: name, path: path, accountedSize: size}
	}
	return nil
}

func readDirect(root *os.Root) ([]os.DirEntry, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	return entries, nil
}

func chmodOpenedRoot(root *os.Root, mode os.FileMode) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	chmodErr := directory.Chmod(mode)
	closeErr := directory.Close()
	if chmodErr != nil {
		return chmodErr
	}
	return closeErr
}

func (m *Manager) closeDirectoryHandles() {
	for _, directory := range m.directories {
		_ = directory.root.Close()
	}
	_ = m.rootHandle.Close()
}

func isRecognizedStalePart(name string) bool {
	return strings.HasPrefix(name, stalePartPrefix) && strings.HasSuffix(name, stalePartSuffix)
}

func knownSize(info os.FileInfo) int64 {
	if info == nil || info.Size() <= 0 {
		return 0
	}
	return info.Size()
}

func (m *Manager) logExcluded(system protocol.System, category string, info os.FileInfo) {
	attributes := []slog.Attr{
		slog.String("category", category),
		slog.Int64("size", knownSize(info)),
	}
	if system != "" {
		attributes = append(attributes, slog.String("system", string(system)))
	}
	m.logger.LogAttrs(context.Background(), slog.LevelWarn, "cache entry excluded from verified inventory", attributes...)
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
	if err := ctx.Err(); err != nil {
		return absentProbe(), internalAPIError("cache verification was canceled")
	}
	entry, ok := m.entries[id]
	if !ok {
		return absentProbe(), nil
	}
	if entry.path != path || !m.directoriesIntact(system) {
		return absentProbe(), internalAPIError("cache directory identity changed")
	}
	directory := m.directories[system]
	info, err := directory.root.Lstat(entry.name)
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
		if !m.directoriesIntact(system) {
			return absentProbe(), internalAPIError("cache directory identity changed")
		}
		if err := ctx.Err(); err != nil {
			return absentProbe(), internalAPIError("cache verification was canceled")
		}
		if !memo.verified {
			return absentProbe(), nil
		}
		return presentProbe(system, key, memo.size), nil
	}
	delete(m.memos, id)

	verified, size, stableStamp, apiErr := m.verify(ctx, system, id, entry, stamp)
	if apiErr != nil {
		return absentProbe(), apiErr
	}
	if stableStamp.info == nil {
		return absentProbe(), nil
	}
	if !m.directoriesIntact(system) {
		return absentProbe(), internalAPIError("cache directory identity changed")
	}
	m.memos[id] = verificationMemo{stamp: stableStamp, verified: verified, size: size}
	if !verified {
		m.logExcluded(system, "digest-mismatch", stableStamp.info)
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
	if matches, apiErr := m.declaredSizeMatches(system, content.Key(), path, content.Size); apiErr != nil {
		return Resolved{}, apiErr
	} else if !matches {
		return Resolved{}, contentNotCachedError()
	}
	response, apiErr := m.Probe(ctx, system, content.Key())
	if apiErr != nil {
		return Resolved{}, apiErr
	}
	if !response.Present || response.Content == nil || response.Content.Size != content.Size {
		return Resolved{}, contentNotCachedError()
	}
	return Resolved{Root: m.root, Path: path}, nil
}

func (m *Manager) declaredSizeMatches(system protocol.System, key protocol.ContentKey, path string, declared int64) (bool, *protocol.APIError) {
	id := makeInventoryKey(system, key)
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[id]
	if !ok {
		return false, nil
	}
	if entry.path != path || !m.directoriesIntact(system) {
		return false, internalAPIError("cache directory identity changed")
	}
	directory := m.directories[system]
	info, err := directory.root.Lstat(entry.name)
	if err != nil {
		if os.IsNotExist(err) {
			m.removeEntry(id, entry)
			return false, nil
		}
		return false, internalAPIError("cache entry cannot be inspected")
	}
	if !m.reconcileStructuralEntry(id, &entry, info) {
		return false, nil
	}
	return info.Size() == declared, nil
}

func contentNotCachedError() *protocol.APIError {
	return &protocol.APIError{
		Code:    protocol.CodeContentNotCached,
		Message: "requested content is not present in the verified cache",
	}
}

func (m *Manager) Usage() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage
}

func (m *Manager) directoriesIntact(system protocol.System) bool {
	root, err := os.Lstat(m.root)
	boundRoot, boundErr := m.rootHandle.Stat(".")
	if err != nil || boundErr != nil || !root.IsDir() || !sameFileInfo(m.rootInfo, root) || !sameFileInfo(m.rootInfo, boundRoot) {
		return false
	}
	directory, ok := m.directories[system]
	if !ok {
		return false
	}
	current, err := m.rootHandle.Lstat(string(system))
	bound, boundErr := directory.root.Stat(".")
	return err == nil && boundErr == nil && current.IsDir() && sameFileInfo(directory.info, current) && sameFileInfo(directory.info, bound)
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

func (m *Manager) verify(ctx context.Context, system protocol.System, id inventoryKey, entry inventoryEntry, before fileStamp) (bool, int64, fileStamp, *protocol.APIError) {
	file, err := m.openEntry(system, entry)
	if err != nil {
		m.reconcileAfterOpenFailure(system, id, entry)
		if _, stillPresent := m.entries[id]; !stillPresent {
			return false, 0, fileStamp{}, nil
		}
		return false, 0, fileStamp{}, internalAPIError("cache entry cannot be opened")
	}
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !sameStamp(before, fileStamp{info: openedInfo}) {
		_ = file.Close()
		m.reconcileAfterOpenFailure(system, id, entry)
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
				m.reconcileAfterOpenFailure(system, id, entry)
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
	directory, ok := m.directories[system]
	if !ok {
		return false, 0, fileStamp{}, internalAPIError("cache directory identity changed")
	}
	afterPathInfo, err := directory.root.Lstat(entry.name)
	if err != nil || !afterPathInfo.Mode().IsRegular() || !sameStamp(before, fileStamp{info: afterOpenInfo}) || !sameStamp(before, fileStamp{info: afterPathInfo}) || size != before.info.Size() {
		m.reconcileAfterOpenFailure(system, id, entry)
		return false, 0, fileStamp{}, nil
	}
	verified := fmt.Sprintf("%x", hasher.Sum(nil)) == id.digest
	return verified, size, fileStamp{info: afterPathInfo}, nil
}

func (m *Manager) reconcileAfterOpenFailure(system protocol.System, id inventoryKey, previous inventoryEntry) {
	directory, ok := m.directories[system]
	if !ok {
		return
	}
	info, err := directory.root.Lstat(previous.name)
	if err != nil {
		if os.IsNotExist(err) {
			m.removeEntry(id, previous)
		}
		return
	}
	entry := previous
	m.reconcileStructuralEntry(id, &entry, info)
}

func (m *Manager) openEntry(system protocol.System, entry inventoryEntry) (*os.File, error) {
	if m.openFile != nil {
		return m.openFile(entry.path)
	}
	directory, ok := m.directories[system]
	if !ok {
		return nil, errors.New("cache system directory is unavailable")
	}
	return openRegularAt(directory.root, entry.name, entry.path)
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

func sameFileInfo(left, right os.FileInfo) bool {
	return left != nil && right != nil && os.SameFile(left, right)
}

func presentProbe(system protocol.System, key protocol.ContentKey, size int64) protocol.CacheProbeResponse {
	returnedSystem := system
	content := protocol.ContentIdentity{SHA256: key.SHA256, Size: size, Extension: key.Extension}
	return protocol.CacheProbeResponse{Present: true, System: &returnedSystem, Content: &content}
}

func openRegularAt(root *os.Root, name, displayPath string) (*os.File, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	fd, openErr := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	closeErr := directory.Close()
	if openErr != nil {
		return nil, openErr
	}
	if closeErr != nil {
		_ = unix.Close(fd)
		return nil, closeErr
	}
	file := os.NewFile(uintptr(fd), displayPath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("cache file descriptor is invalid")
	}
	return file, nil
}
