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

const (
	activeRecordLaunching       = "launching"
	activeRecordLaunchingDirect = "launching-direct"
	activeRecordDirect          = "direct"
)

type activeRecordEntry struct {
	System  protocol.System          `json:"system"`
	Content protocol.ContentIdentity `json:"content"`
	Direct  bool                     `json:"direct,omitempty"`
}

type activeRecord struct {
	System   protocol.System          `json:"system"`
	Content  protocol.ContentIdentity `json:"content"`
	Phase    string                   `json:"phase,omitempty"`
	Previous *activeRecordEntry       `json:"previous,omitempty"`
}

// LaunchIntent identifies the in-memory attempt that durably recorded a
// launch intent. Its zero value has no authority to restore an older record.
type LaunchIntent struct {
	id uint64
}

// ActiveRecordEntry is one validated durable active-record alternative.
type ActiveRecordEntry struct {
	System  protocol.System
	Content protocol.ContentIdentity
	Direct  bool
}

// ActiveRecords contains the validated identities carried by the durable
// active record. During an interrupted launch, Candidate and Previous are
// alternatives whose observed-core interpretation remains the coordinator's
// responsibility.
type ActiveRecords struct {
	Candidate   ActiveRecordEntry
	Previous    *ActiveRecordEntry
	Interrupted bool
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
	if protocol.ValidateSystem(record.System) != nil {
		return
	}
	if record.Phase != "" && record.Phase != activeRecordLaunching && record.Phase != activeRecordLaunchingDirect && record.Phase != activeRecordDirect {
		return
	}
	switch record.Phase {
	case activeRecordLaunchingDirect:
		if record.Content != (protocol.ContentIdentity{}) {
			return
		}
	case activeRecordDirect:
		if record.Content != (protocol.ContentIdentity{}) || record.Previous != nil {
			return
		}
	case activeRecordLaunching:
		if protocol.ValidateContentIdentity(record.Content) != nil {
			return
		}
	case "":
		if protocol.ValidateContentIdentity(record.Content) != nil || record.Previous != nil {
			return
		}
	}
	if record.Previous != nil && !validActiveRecordEntry(*record.Previous) {
		return
	}
	if record.Phase == activeRecordLaunchingDirect || record.Phase == activeRecordDirect {
		if _, ok := m.extensions[record.System]; !ok {
			return
		}
	} else {
		if _, _, apiErr := m.checkedPath(record.System, record.Content.Key()); apiErr != nil {
			return
		}
	}
	if record.Previous != nil {
		if record.Previous.Direct {
			if _, ok := m.extensions[record.Previous.System]; !ok {
				return
			}
		} else if _, _, apiErr := m.checkedPath(record.Previous.System, record.Previous.Content.Key()); apiErr != nil {
			return
		}
	}
	m.pendingActive = &record
	if record.Phase == activeRecordLaunching || record.Phase == activeRecordLaunchingDirect {
		m.launchIntent = &record
	}
}

// ActiveRecordSystems returns the alternatives from a validated durable
// active record. Interrupted records are not interpreted here because observed
// core names belong to the coordinator's core registry.
func (m *Manager) ActiveRecordSystems(ctx context.Context) (ActiveRecords, bool, *protocol.APIError) {
	m.mu.Lock()
	if !m.recordPresent || m.pendingActive == nil {
		m.mu.Unlock()
		return ActiveRecords{}, false, nil
	}
	record := *m.pendingActive
	m.mu.Unlock()
	if record.Phase == activeRecordLaunching || record.Phase == activeRecordLaunchingDirect {
		records := ActiveRecords{Candidate: ActiveRecordEntry{System: record.System, Content: record.Content, Direct: record.Phase == activeRecordLaunchingDirect}, Interrupted: true}
		if record.Previous != nil {
			records.Previous = &ActiveRecordEntry{System: record.Previous.System, Content: record.Previous.Content, Direct: record.Previous.Direct}
		}
		return records, true, nil
	}
	if record.Phase == activeRecordDirect {
		return ActiveRecords{Candidate: ActiveRecordEntry{System: record.System, Direct: true}}, true, nil
	}
	if _, apiErr := m.Resolve(ctx, record.System, record.Content); apiErr != nil {
		if apiErr.Code == protocol.CodeContentNotCached {
			return ActiveRecords{}, false, nil
		}
		return ActiveRecords{}, false, apiErr
	}
	return ActiveRecords{Candidate: ActiveRecordEntry{System: record.System, Content: record.Content}}, true, nil
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
	if m.inFlight == nil {
		m.inFlight = make(map[inventoryKey]uint64)
	}
	m.inFlight[id]++
	return nil
}

// RecordLaunchIntent durably replaces the active record before the runtime can
// change hardware. This lets startup distinguish systems that share one core.
func (m *Manager) RecordLaunchIntent(system protocol.System, content protocol.ContentIdentity) (LaunchIntent, *protocol.APIError) {
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return LaunchIntent{}, badRequestAPIError("content identity is invalid")
	}
	id, _, apiErr := m.checkedPath(system, content.Key())
	if apiErr != nil {
		return LaunchIntent{}, apiErr
	}
	record := activeRecord{System: system, Content: content, Phase: activeRecordLaunching}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight[id] == 0 || !m.verifiedContentLocked(id, content.Size) {
		return LaunchIntent{}, internalAPIError("launch content is not pinned and verified")
	}
	if m.launchIntent != nil {
		if m.launchIntent.System == system && m.launchIntent.Content == content {
			return LaunchIntent{}, nil
		}
		return LaunchIntent{}, internalAPIError("another launch intent is already recorded")
	}
	var previous *activeRecordEntry
	if m.pendingActive != nil {
		entry := activeRecordEntryFromRecord(*m.pendingActive)
		previous = &entry
	}
	record.Previous = previous
	if err := m.writeActiveRecord(record); err != nil {
		return LaunchIntent{}, internalAPIError("launch intent cannot be recorded")
	}
	m.nextIntentID++
	if m.nextIntentID == 0 {
		m.nextIntentID++
	}
	intent := LaunchIntent{id: m.nextIntentID}
	m.launchIntent = &record
	m.intentOwner = intent
	m.pendingActive = &record
	m.recordPresent = true
	return intent, nil
}

// RecordDirectLaunchIntent durably records a path-based launch candidate while
// retaining the prior active identity as an alternative.
func (m *Manager) RecordDirectLaunchIntent(system protocol.System) (LaunchIntent, *protocol.APIError) {
	if protocol.ValidateSystem(system) != nil {
		return LaunchIntent{}, badRequestAPIError("system is invalid")
	}
	if _, ok := m.extensions[system]; !ok {
		return LaunchIntent{}, &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "system is not registered for the target cache"}
	}
	record := activeRecord{System: system, Phase: activeRecordLaunchingDirect}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.launchIntent != nil {
		if m.launchIntent.Phase == activeRecordLaunchingDirect && m.launchIntent.System == system {
			return LaunchIntent{}, nil
		}
		return LaunchIntent{}, internalAPIError("another launch intent is already recorded")
	}
	if m.pendingActive != nil {
		if m.pendingActive.Phase != "" && m.pendingActive.Phase != activeRecordDirect {
			return LaunchIntent{}, internalAPIError("another launch intent is already recorded")
		}
		previous := activeRecordEntryFromRecord(*m.pendingActive)
		record.Previous = &previous
	}
	if err := m.writeActiveRecord(record); err != nil {
		return LaunchIntent{}, internalAPIError("direct launch intent cannot be recorded")
	}
	m.nextIntentID++
	if m.nextIntentID == 0 {
		m.nextIntentID++
	}
	intent := LaunchIntent{id: m.nextIntentID}
	m.launchIntent = &record
	m.intentOwner = intent
	m.pendingActive = &record
	m.recordPresent = true
	return intent, nil
}

func (m *Manager) AbortLaunch(system protocol.System, content protocol.ContentIdentity, intent LaunchIntent) *protocol.APIError {
	if protocol.ValidateContentIdentity(content) != nil {
		return nil
	}
	id, _, apiErr := m.checkedPath(system, content.Key())
	if apiErr != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if intent.id != 0 && m.intentOwner == intent && m.launchIntent != nil && m.launchIntent.System == system && m.launchIntent.Content == content {
		if m.launchIntent.Previous != nil {
			previous := activeRecordFromEntry(*m.launchIntent.Previous)
			if err := m.writeActiveRecord(previous); err != nil {
				return internalAPIError("previous active cache record cannot be restored")
			}
		} else if err := m.removeActiveRecord(); err != nil {
			return internalAPIError("launch intent cannot be cleared")
		}
		if m.launchIntent.Previous != nil {
			previous := activeRecordFromEntry(*m.launchIntent.Previous)
			m.pendingActive = &previous
			m.recordPresent = true
		} else {
			m.pendingActive = nil
			m.recordPresent = false
		}
		m.launchIntent = nil
		m.intentOwner = LaunchIntent{}
	}
	if count := m.inFlight[id]; count > 1 {
		m.inFlight[id] = count - 1
	} else {
		delete(m.inFlight, id)
	}
	return nil
}

// AbortDirectLaunch restores the previous cached record when a staged direct
// launch is known not to have reached runtime dispatch.
func (m *Manager) AbortDirectLaunch(system protocol.System, intent LaunchIntent) *protocol.APIError {
	m.mu.Lock()
	defer m.mu.Unlock()
	if intent.id == 0 || m.intentOwner != intent || m.launchIntent == nil || m.launchIntent.Phase != activeRecordLaunchingDirect || m.launchIntent.System != system {
		return nil
	}
	if m.launchIntent.Previous == nil {
		if err := m.removeActiveRecord(); err != nil {
			return internalAPIError("direct launch intent cannot be cleared")
		}
		m.pendingActive = nil
		m.recordPresent = false
		m.launchIntent = nil
		m.intentOwner = LaunchIntent{}
		return nil
	}
	previous := activeRecordFromEntry(*m.launchIntent.Previous)
	if err := m.writeActiveRecord(previous); err != nil {
		return internalAPIError("previous active cache record cannot be restored")
	}
	m.pendingActive = &previous
	m.recordPresent = true
	m.launchIntent = nil
	m.intentOwner = LaunchIntent{}
	return nil
}

// CommitDirectLaunch replaces an interrupted path-based launch intent with a
// durable system-only identity. It intentionally retains no path or content.
func (m *Manager) CommitDirectLaunch(system protocol.System, intent LaunchIntent) *protocol.APIError {
	if protocol.ValidateSystem(system) != nil {
		return badRequestAPIError("system is invalid")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	intentMatches := m.launchIntent != nil && m.launchIntent.Phase == activeRecordLaunchingDirect && m.launchIntent.System == system
	if !intentMatches || (intent.id != 0 && m.intentOwner != intent) {
		return internalAPIError("recorded direct launch intent does not match launched system")
	}
	record := activeRecord{System: system, Phase: activeRecordDirect}
	if err := m.writeActiveRecord(record); err != nil {
		return internalAPIError("direct active record cannot be committed")
	}
	m.active = nil
	m.inFlight = nil
	m.pendingActive = &record
	m.recordPresent = true
	m.launchIntent = nil
	m.intentOwner = LaunchIntent{}
	return nil
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
	ready := m.inFlight[id] > 0 && m.verifiedContentLocked(id, content.Size)
	intentMatches := m.launchIntent != nil && m.launchIntent.System == system && m.launchIntent.Content == content
	intentPending := m.launchIntent != nil
	m.mu.Unlock()
	if !ready {
		return internalAPIError("launch content is not pinned and verified")
	}
	if intentPending && !intentMatches {
		return internalAPIError("recorded launch intent does not match launched content")
	}
	record := activeRecord{System: system, Content: content}
	if err := m.writeActiveRecord(record); err != nil {
		return internalAPIError("active cache record cannot be committed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight[id] == 0 || !m.verifiedContentLocked(id, content.Size) || !m.directoriesIntact(system) {
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
	m.launchIntent = nil
	m.intentOwner = LaunchIntent{}
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
	m.launchIntent = nil
	m.intentOwner = LaunchIntent{}
	m.mu.Unlock()
	return nil
}

func (m *Manager) ReconcileActive(ctx context.Context, status protocol.Status, selected *ActiveRecordEntry) *protocol.APIError {
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

	if recordPresent && record != nil && status.State == protocol.StateActive && status.System != nil {
		selectedRecord, matches := activeRecordForSelection(*record, *status.System, selected)
		if matches {
			if selectedRecord.Phase == activeRecordDirect {
				if record.Phase == activeRecordLaunching || record.Phase == activeRecordLaunchingDirect {
					if err := m.writeActiveRecord(selectedRecord); err != nil {
						return internalAPIError("interrupted direct launch record cannot be committed")
					}
				}
				m.mu.Lock()
				m.pendingActive = &selectedRecord
				m.recordPresent = true
				m.launchIntent = nil
				m.intentOwner = LaunchIntent{}
				m.mu.Unlock()
				return nil
			}
			if _, apiErr := m.Resolve(ctx, selectedRecord.System, selectedRecord.Content); apiErr == nil {
				id := makeInventoryKey(selectedRecord.System, selectedRecord.Content.Key())
				m.mu.Lock()
				verified := m.verifiedContentLocked(id, selectedRecord.Content.Size)
				m.mu.Unlock()
				if verified && (record.Phase == activeRecordLaunching || record.Phase == activeRecordLaunchingDirect) {
					if err := m.writeActiveRecord(selectedRecord); err != nil {
						m.logger.LogAttrs(context.Background(), slog.LevelWarn, "interrupted active cache record could not be committed", slog.String("category", "active-record-reconcile"))
						return internalAPIError("interrupted active cache record cannot be committed")
					}
				}
				if verified {
					m.mu.Lock()
					active := id
					m.active = &active
					m.pendingActive = &selectedRecord
					m.recordPresent = true
					m.launchIntent = nil
					m.intentOwner = LaunchIntent{}
					m.mu.Unlock()
					return nil
				}
			} else if apiErr.Code != protocol.CodeContentNotCached {
				return apiErr
			}
			if record.Phase == activeRecordLaunching || record.Phase == activeRecordLaunchingDirect {
				return internalAPIError("interrupted active cache content cannot be verified")
			}
		}
	}
	if recordPresent && record != nil && (record.Phase == activeRecordLaunching || record.Phase == activeRecordLaunchingDirect) && status.State != protocol.StateIdle {
		return internalAPIError("interrupted active cache record cannot be reconciled")
	}
	if !recordPresent {
		return nil
	}
	if err := m.removeActiveRecord(); err != nil {
		m.logger.LogAttrs(context.Background(), slog.LevelWarn, "active cache record could not be reconciled", slog.String("category", "active-record-cleanup"))
		return internalAPIError("active cache record cannot be reconciled")
	}
	m.mu.Lock()
	m.pendingActive = nil
	m.recordPresent = false
	m.launchIntent = nil
	m.intentOwner = LaunchIntent{}
	m.mu.Unlock()
	return nil
}

func activeRecordForSelection(record activeRecord, system protocol.System, selected *ActiveRecordEntry) (activeRecord, bool) {
	if record.Phase != activeRecordLaunching && record.Phase != activeRecordLaunchingDirect {
		return record, record.System == system
	}
	if selected == nil || selected.System != system {
		return activeRecord{}, false
	}
	if record.Phase == activeRecordLaunchingDirect {
		candidateMatches := selected.Direct && selected.System == record.System
		previousMatches := record.Previous != nil && selected.Direct == record.Previous.Direct && selected.System == record.Previous.System && selected.Content == record.Previous.Content
		if candidateMatches {
			return activeRecord{System: record.System, Phase: activeRecordDirect}, true
		}
		if previousMatches {
			return activeRecordFromEntry(*record.Previous), true
		}
		return activeRecord{}, false
	}
	candidateMatches := !selected.Direct && selected.System == record.System && selected.Content == record.Content
	previousMatches := record.Previous != nil && selected.Direct == record.Previous.Direct && selected.System == record.Previous.System && selected.Content == record.Previous.Content
	if !candidateMatches && !previousMatches {
		return activeRecord{}, false
	}
	if candidateMatches {
		return activeRecord{System: record.System, Content: record.Content}, true
	}
	return activeRecordFromEntry(*record.Previous), true
}

func validActiveRecordEntry(entry activeRecordEntry) bool {
	if protocol.ValidateSystem(entry.System) != nil {
		return false
	}
	if entry.Direct {
		return entry.Content == (protocol.ContentIdentity{})
	}
	return protocol.ValidateContentIdentity(entry.Content) == nil
}

func activeRecordEntryFromRecord(record activeRecord) activeRecordEntry {
	return activeRecordEntry{System: record.System, Content: record.Content, Direct: record.Phase == activeRecordDirect}
}

func activeRecordFromEntry(entry activeRecordEntry) activeRecord {
	record := activeRecord{System: entry.System, Content: entry.Content}
	if entry.Direct {
		record.Content = protocol.ContentIdentity{}
		record.Phase = activeRecordDirect
	}
	return record
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
