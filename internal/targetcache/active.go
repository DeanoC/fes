package targetcache

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast-POC/protocol"
	"golang.org/x/sys/unix"
)

const maxActiveRecordBytes = 4 << 10

type activeRecord struct {
	System  protocol.System          `json:"system"`
	Content protocol.ContentIdentity `json:"content"`
}

type activeStore struct {
	directory string
	name      string
	info      os.FileInfo
	root      *os.Root
}

func (m *Manager) prepareActiveStore() error {
	if m.config.ActiveRecord == "" {
		return nil
	}
	directory := filepath.Dir(m.config.ActiveRecord)
	name := filepath.Base(m.config.ActiveRecord)
	if name == "." || name == string(filepath.Separator) {
		return errors.New("target cache active record name is invalid")
	}
	if err := os.MkdirAll(directory, privateDirectoryMode); err != nil {
		return errors.New("create target cache active record directory failed")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return errors.New("resolve target cache active record directory failed")
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return errors.New("make target cache active record directory absolute failed")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.IsDir() {
		return errors.New("target cache active record parent is not a directory")
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return errors.New("open target cache active record directory failed")
	}
	bound, err := root.Stat(".")
	if err != nil || !sameFileInfo(info, bound) {
		_ = root.Close()
		return errors.New("bind target cache active record directory failed")
	}
	m.activeStore = activeStore{directory: filepath.Clean(resolved), name: name, info: bound, root: root}
	m.loadActiveRecord()
	return nil
}

func (m *Manager) loadActiveRecord() {
	store := m.activeStore
	if store.root == nil || !m.activeStoreIntact() {
		return
	}
	info, err := store.root.Lstat(store.name)
	if os.IsNotExist(err) {
		return
	}
	m.recordPresent = true
	if err != nil || info.Mode() != privateFileMode || info.Size() < 1 || info.Size() > maxActiveRecordBytes {
		return
	}
	file, err := openRegularAt(store.root, store.name, "active-record")
	if err != nil {
		return
	}
	opened, statErr := file.Stat()
	if statErr != nil || !sameStamp(fileStamp{info: info}, fileStamp{info: opened}) {
		_ = file.Close()
		return
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxActiveRecordBytes+1))
	after, afterErr := file.Stat()
	closeErr := file.Close()
	current, currentErr := store.root.Lstat(store.name)
	if readErr != nil || afterErr != nil || closeErr != nil || currentErr != nil ||
		len(data) > maxActiveRecordBytes || int64(len(data)) != info.Size() ||
		!sameStamp(fileStamp{info: info}, fileStamp{info: after}) || !sameStamp(fileStamp{info: info}, fileStamp{info: current}) {
		return
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(data), maxActiveRecordBytes))
	decoder.DisallowUnknownFields()
	var record activeRecord
	if err := decoder.Decode(&record); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return
	}
	if protocol.ValidateSystem(record.System) != nil || protocol.ValidateContentIdentity(record.Content) != nil {
		return
	}
	if _, _, apiErr := m.checkedPath(record.System, record.Content.Key()); apiErr != nil {
		return
	}
	m.pendingActive = &record
}

func (m *Manager) PinForLaunch(system protocol.System, content protocol.ContentIdentity) *protocol.APIError {
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return badRequestAPIError("content identity is invalid")
	}
	id, _, apiErr := m.checkedPath(system, content.Key())
	if apiErr != nil {
		return apiErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.verifiedContentLocked(id, content.Size) {
		return contentNotCachedError()
	}
	pinned := id
	m.inFlight = &pinned
	return nil
}

func (m *Manager) AbortLaunch(system protocol.System, content protocol.ContentIdentity) {
	if protocol.ValidateContentIdentity(content) != nil {
		return
	}
	id, _, apiErr := m.checkedPath(system, content.Key())
	if apiErr != nil {
		return
	}
	m.mu.Lock()
	if m.inFlight != nil && *m.inFlight == id {
		m.inFlight = nil
	}
	m.mu.Unlock()
}

func (m *Manager) CommitLaunch(system protocol.System, content protocol.ContentIdentity) *protocol.APIError {
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return badRequestAPIError("content identity is invalid")
	}
	id, _, apiErr := m.checkedPath(system, content.Key())
	if apiErr != nil {
		return apiErr
	}
	m.mu.Lock()
	ready := m.inFlight != nil && *m.inFlight == id && m.verifiedContentLocked(id, content.Size)
	m.mu.Unlock()
	if !ready {
		return internalAPIError("launch content is not pinned and verified")
	}
	record := activeRecord{System: system, Content: content}
	if err := m.writeActiveRecord(record); err != nil {
		return internalAPIError("active cache record cannot be committed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight == nil || *m.inFlight != id || !m.verifiedContentLocked(id, content.Size) || !m.directoriesIntact(system) {
		return internalAPIError("launch content changed before commit")
	}
	entry := m.entries[id]
	directory := m.directories[system]
	file, err := openRegularAt(directory.root, entry.name, entry.path)
	if err != nil {
		return internalAPIError("launched cache entry cannot be opened for touch")
	}
	before, statErr := file.Stat()
	memo := m.memos[id]
	if statErr != nil || !sameStamp(memo.stamp, fileStamp{info: before}) {
		_ = file.Close()
		return internalAPIError("launched cache entry changed before touch")
	}
	now := m.clock()
	timeval := unix.NsecToTimeval(now.UnixNano())
	touchErr := unix.Futimes(int(file.Fd()), []unix.Timeval{timeval, timeval})
	after, afterErr := file.Stat()
	closeErr := file.Close()
	pathInfo, pathErr := directory.root.Lstat(entry.name)
	if touchErr != nil || afterErr != nil || closeErr != nil || pathErr != nil ||
		!sameFileInfo(after, pathInfo) || !m.directoriesIntact(system) {
		return internalAPIError("launched cache entry cannot be touched")
	}
	m.memos[id] = verificationMemo{stamp: fileStamp{info: pathInfo}, verified: true, size: content.Size}
	active := id
	m.active = &active
	m.inFlight = nil
	m.pendingActive = &record
	m.recordPresent = true
	return nil
}

func (m *Manager) ClearActive() *protocol.APIError {
	if err := m.removeActiveRecord(); err != nil {
		return internalAPIError("active cache record cannot be cleared")
	}
	m.mu.Lock()
	m.active = nil
	m.inFlight = nil
	m.pendingActive = nil
	m.recordPresent = false
	m.mu.Unlock()
	return nil
}

func (m *Manager) ReconcileActive(status protocol.Status) {
	m.mu.Lock()
	recordPresent := m.recordPresent
	var record *activeRecord
	if m.pendingActive != nil {
		copied := *m.pendingActive
		record = &copied
	}
	m.active = nil
	m.inFlight = nil
	m.mu.Unlock()

	if recordPresent && record != nil && status.State == protocol.StateActive && status.System != nil && *status.System == record.System {
		if _, apiErr := m.Resolve(context.Background(), record.System, record.Content); apiErr == nil {
			id := makeInventoryKey(record.System, record.Content.Key())
			m.mu.Lock()
			if m.verifiedContentLocked(id, record.Content.Size) {
				active := id
				m.active = &active
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
		}
	}
	if !recordPresent {
		return
	}
	if err := m.removeActiveRecord(); err != nil {
		m.logger.LogAttrs(context.Background(), slog.LevelWarn, "active cache record could not be reconciled", slog.String("category", "active-record-cleanup"))
		return
	}
	m.mu.Lock()
	m.pendingActive = nil
	m.recordPresent = false
	m.mu.Unlock()
}

func (m *Manager) verifiedContentLocked(id inventoryKey, size int64) bool {
	entry, ok := m.entries[id]
	if !ok || entry.accountedSize != size || !m.directoriesIntact(id.system) {
		return false
	}
	directory := m.directories[id.system]
	info, err := directory.root.Lstat(entry.name)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return false
	}
	memo, ok := m.memos[id]
	return ok && memo.verified && memo.size == size && sameStamp(memo.stamp, fileStamp{info: info})
}

func (m *Manager) writeActiveRecord(record activeRecord) error {
	store := m.activeStore
	if store.root == nil || !m.activeStoreIntact() {
		return errors.New("active record store is unavailable")
	}
	if info, err := store.root.Lstat(store.name); err == nil && !info.Mode().IsRegular() {
		return errors.New("active record destination is not regular")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tempName, err := newActiveTempName()
	if err != nil {
		return err
	}
	file, err := store.root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFileMode)
	if err != nil {
		return err
	}
	tempExists := true
	cleanup := func() {
		_ = file.Close()
		if tempExists {
			_ = store.root.Remove(tempName)
		}
	}
	defer cleanup()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(privateFileMode); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if !m.activeStoreIntact() {
		return errors.New("active record store identity changed")
	}
	if err := store.root.Rename(tempName, store.name); err != nil {
		return err
	}
	tempExists = false
	info, err := store.root.Lstat(store.name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != privateFileMode || info.Size() != int64(len(data)) {
		return errors.New("active record publication cannot be verified")
	}
	return nil
}

func (m *Manager) removeActiveRecord() error {
	store := m.activeStore
	if store.root == nil {
		return nil
	}
	if !m.activeStoreIntact() {
		return errors.New("active record store identity changed")
	}
	info, err := store.root.Lstat(store.name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("active record destination is a directory")
	}
	return store.root.Remove(store.name)
}

func (m *Manager) activeStoreIntact() bool {
	store := m.activeStore
	if store.root == nil {
		return false
	}
	current, err := os.Lstat(store.directory)
	bound, boundErr := store.root.Stat(".")
	return err == nil && boundErr == nil && current.IsDir() && sameFileInfo(store.info, current) && sameFileInfo(store.info, bound)
}

func newActiveTempName() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf(".fogcast-active-%x.tmp", token[:]), nil
}
