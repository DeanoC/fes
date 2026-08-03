package targetcache

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
	"golang.org/x/sys/unix"
)

const privateFileMode = 0o600

type evictionCandidate struct {
	id       inventoryKey
	key      protocol.ContentKey
	name     string
	modified time.Time
}

type capacityPlan struct {
	victims []inventoryKey
}

func (m *Manager) Put(ctx context.Context, system protocol.System, content protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
	if err := protocol.ValidateContentIdentity(content); err != nil || body == nil {
		return protocol.CacheUploadResponse{}, badRequestAPIError("content upload is invalid")
	}
	id, _, apiErr := m.checkedPath(system, content.Key())
	if apiErr != nil {
		return protocol.CacheUploadResponse{}, apiErr
	}
	if err := ctx.Err(); err != nil {
		return protocol.CacheUploadResponse{}, transferAPIError("content upload was canceled")
	}
	select {
	case m.uploadGate <- struct{}{}:
		defer func() { <-m.uploadGate }()
	case <-ctx.Done():
		return protocol.CacheUploadResponse{}, transferAPIError("content upload was canceled")
	}
	if err := ctx.Err(); err != nil {
		return protocol.CacheUploadResponse{}, transferAPIError("content upload was canceled")
	}

	m.mu.Lock()
	apiErr = m.refreshAccountingLocked()
	m.mu.Unlock()
	if apiErr != nil {
		return protocol.CacheUploadResponse{}, apiErr
	}
	if response, present, apiErr := m.uploadDestinationState(ctx, system, content); apiErr != nil || present {
		return response, apiErr
	}

	m.mu.Lock()
	uploading := id
	m.uploading = &uploading
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.uploading != nil && *m.uploading == id {
			m.uploading = nil
		}
		m.mu.Unlock()
	}()

	if _, apiErr := m.planCapacity(ctx, content.Size); apiErr != nil {
		return protocol.CacheUploadResponse{}, apiErr
	}

	var staged bytes.Buffer
	staged.Grow(int(content.Size))
	hasher := sha256.New()
	if apiErr := streamExact(ctx, &staged, hasher, body, content.Size); apiErr != nil {
		return protocol.CacheUploadResponse{}, apiErr
	}
	if fmt.Sprintf("%x", hasher.Sum(nil)) != content.SHA256 {
		return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeDigestMismatch, Message: "uploaded content digest does not match its identity"}
	}
	plan, apiErr := m.planCapacity(ctx, content.Size)
	if apiErr != nil {
		return protocol.CacheUploadResponse{}, apiErr
	}

	directory, ok := m.directories[system]
	if !ok || !m.directoriesIntact(system) {
		return protocol.CacheUploadResponse{}, internalAPIError("cache directory identity changed")
	}
	partName, err := newUploadPartName()
	if err != nil {
		return protocol.CacheUploadResponse{}, internalAPIError("cache upload cannot create a temporary name")
	}
	part, err := directory.root.OpenFile(partName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFileMode)
	if err != nil {
		return protocol.CacheUploadResponse{}, internalAPIError("cache upload temporary file cannot be created")
	}
	partOpen := true
	partExists := true
	cleanup := func() *protocol.APIError {
		if partOpen {
			_ = part.Close()
			partOpen = false
		}
		if partExists {
			if removeErr := directory.root.Remove(partName); removeErr != nil && !os.IsNotExist(removeErr) {
				m.mu.Lock()
				_ = m.refreshAccountingLocked()
				m.mu.Unlock()
				return internalAPIError("cache upload temporary file cannot be cleaned")
			}
			partExists = false
		}
		return nil
	}
	defer func() { _ = cleanup() }()

	if apiErr := writeStagedUpload(part, staged.Bytes()); apiErr != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		return protocol.CacheUploadResponse{}, apiErr
	}
	if err := part.Chmod(privateFileMode); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		return protocol.CacheUploadResponse{}, internalAPIError("cache upload permissions cannot be secured")
	}
	if err := part.Sync(); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		return protocol.CacheUploadResponse{}, internalAPIError("cache upload cannot be flushed")
	}
	partInfo, err := part.Stat()
	if err != nil || !partInfo.Mode().IsRegular() || partInfo.Size() != content.Size {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		return protocol.CacheUploadResponse{}, internalAPIError("cache upload temporary file cannot be verified")
	}
	if err := part.Close(); err != nil {
		partOpen = false
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		return protocol.CacheUploadResponse{}, internalAPIError("cache upload cannot be closed")
	}
	partOpen = false
	if err := ctx.Err(); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		return protocol.CacheUploadResponse{}, transferAPIError("content upload was canceled")
	}
	if apiErr := m.applyCapacityPlan(plan); apiErr != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		return protocol.CacheUploadResponse{}, apiErr
	}
	if !m.directoriesIntact(system) {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		return protocol.CacheUploadResponse{}, internalAPIError("cache directory identity changed")
	}

	destinationName := content.SHA256 + "." + content.Extension
	if err := exclusiveRename(directory.root, partName, destinationName); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return protocol.CacheUploadResponse{}, cleanupErr
		}
		if errors.Is(err, os.ErrExist) || errors.Is(err, unix.EEXIST) {
			m.mu.Lock()
			refreshErr := m.refreshAccountingLocked()
			m.mu.Unlock()
			if refreshErr != nil {
				return protocol.CacheUploadResponse{}, refreshErr
			}
			if response, present, apiErr := m.uploadDestinationState(ctx, system, content); apiErr != nil || present {
				return response, apiErr
			}
			return protocol.CacheUploadResponse{}, transferAPIError("cache destination is occupied")
		}
		return protocol.CacheUploadResponse{}, internalAPIError("cache upload cannot be published")
	}
	partExists = false

	m.mu.Lock()
	apiErr = m.refreshAccountingLocked()
	if apiErr == nil {
		entry, exists := m.entries[id]
		if !exists {
			apiErr = internalAPIError("published cache entry is unavailable")
		} else {
			info, statErr := directory.root.Lstat(entry.name)
			if statErr != nil || !info.Mode().IsRegular() || info.Size() != content.Size || !os.SameFile(partInfo, info) {
				apiErr = internalAPIError("published cache entry cannot be verified")
			} else {
				m.memos[id] = verificationMemo{stamp: fileStamp{info: info}, verified: true, size: content.Size}
			}
		}
	}
	m.mu.Unlock()
	if apiErr != nil {
		return protocol.CacheUploadResponse{}, apiErr
	}
	return uploadResponse(protocol.CacheUploadCreated, system, content), nil
}

func writeStagedUpload(destination io.Writer, staged []byte) *protocol.APIError {
	written, err := io.Copy(destination, bytes.NewReader(staged))
	if err != nil {
		if errors.Is(err, unix.ENOSPC) {
			return cacheFullAPIError()
		}
		return internalAPIError("cache upload cannot be written")
	}
	if written != int64(len(staged)) {
		return internalAPIError("cache upload cannot be written")
	}
	return nil
}

func (m *Manager) uploadDestinationState(ctx context.Context, system protocol.System, content protocol.ContentIdentity) (protocol.CacheUploadResponse, bool, *protocol.APIError) {
	resolved, apiErr := m.Resolve(ctx, system, content)
	if apiErr == nil {
		_ = resolved
		return uploadResponse(protocol.CacheUploadPresent, system, content), true, nil
	}
	if ctx.Err() != nil {
		return protocol.CacheUploadResponse{}, false, transferAPIError("content upload was canceled")
	}
	if apiErr.Code != protocol.CodeContentNotCached {
		return protocol.CacheUploadResponse{}, false, apiErr
	}
	directory, ok := m.directories[system]
	if !ok || !m.directoriesIntact(system) {
		return protocol.CacheUploadResponse{}, false, internalAPIError("cache directory identity changed")
	}
	name := content.SHA256 + "." + content.Extension
	if _, err := directory.root.Lstat(name); err == nil {
		return protocol.CacheUploadResponse{}, false, transferAPIError("cache destination is occupied")
	} else if !os.IsNotExist(err) {
		return protocol.CacheUploadResponse{}, false, internalAPIError("cache destination cannot be inspected")
	}
	return protocol.CacheUploadResponse{}, false, nil
}

func streamExact(ctx context.Context, destination io.Writer, hasher io.Writer, source io.Reader, declared int64) *protocol.APIError {
	buffer := make([]byte, 64<<10)
	remaining := declared
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return transferAPIError("content upload was canceled")
		}
		limit := int64(len(buffer))
		if remaining < limit {
			limit = remaining
		}
		count, readErr := source.Read(buffer[:limit])
		if count < 0 || int64(count) > limit {
			return transferAPIError("content upload body returned an invalid byte count")
		}
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			if writeErr != nil || written != count {
				return internalAPIError("cache upload cannot be written")
			}
			_, _ = hasher.Write(buffer[:count])
			remaining -= int64(count)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if remaining == 0 {
					break
				}
				return transferAPIError("content upload ended before its declared length")
			}
			return transferAPIError("content upload body cannot be read")
		}
		if count == 0 {
			return transferAPIError("content upload body made no progress")
		}
	}
	if err := ctx.Err(); err != nil {
		return transferAPIError("content upload was canceled")
	}
	var excess [1]byte
	for emptyReads := 0; ; emptyReads++ {
		count, readErr := source.Read(excess[:])
		if count > 0 {
			return transferAPIError("content upload exceeded its declared length")
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return transferAPIError("content upload body cannot be read")
		}
		if emptyReads >= 99 {
			return transferAPIError("content upload body made no progress")
		}
		if err := ctx.Err(); err != nil {
			return transferAPIError("content upload was canceled")
		}
	}
}

func (m *Manager) planCapacity(ctx context.Context, size int64) (capacityPlan, *protocol.APIError) {
	if err := ctx.Err(); err != nil {
		return capacityPlan{}, transferAPIError("content upload was canceled")
	}
	if size > m.config.MaxBytes {
		return capacityPlan{}, cacheFullAPIError()
	}
	m.mu.Lock()
	apiErr := m.refreshAccountingLocked()
	usage := m.usage
	m.mu.Unlock()
	if apiErr != nil {
		return capacityPlan{}, apiErr
	}
	available, err := m.spaceProbe(m.root)
	if err != nil || available < 0 {
		return capacityPlan{}, internalAPIError("cache free space cannot be determined")
	}
	if capacityReady(usage, 0, available, size, m.config.MaxBytes) {
		return capacityPlan{}, nil
	}

	candidates, apiErr := m.evictionCandidates()
	if apiErr != nil {
		return capacityPlan{}, apiErr
	}
	plan := capacityPlan{victims: make([]inventoryKey, 0, len(candidates))}
	var reclaimed int64
	for _, candidate := range candidates {
		response, probeErr := m.Probe(ctx, candidate.id.system, candidate.key)
		if probeErr != nil {
			if ctx.Err() != nil {
				return capacityPlan{}, transferAPIError("content upload was canceled")
			}
			return capacityPlan{}, probeErr
		}
		if !response.Present {
			continue
		}
		m.mu.Lock()
		entry, present := m.entries[candidate.id]
		pinned := m.isPinnedLocked(candidate.id)
		m.mu.Unlock()
		if !present || pinned {
			continue
		}
		plan.victims = append(plan.victims, candidate.id)
		if entry.accountedSize > math.MaxInt64-reclaimed {
			reclaimed = math.MaxInt64
		} else {
			reclaimed += entry.accountedSize
		}
		if capacityReady(usage, reclaimed, available, size, m.config.MaxBytes) {
			return plan, nil
		}
	}
	return capacityPlan{}, cacheFullAPIError()
}

func capacityReady(usage, reclaimed, available, size, maximum int64) bool {
	if size > maximum {
		return false
	}
	remaining := int64(0)
	if reclaimed < usage {
		remaining = usage - reclaimed
	}
	if remaining > maximum-size {
		return false
	}
	if available >= size {
		return true
	}
	return reclaimed >= size-available
}

func (m *Manager) evictionCandidates() ([]evictionCandidate, *protocol.APIError) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if apiErr := m.refreshAccountingLocked(); apiErr != nil {
		return nil, apiErr
	}
	result := make([]evictionCandidate, 0, len(m.entries))
	for id, entry := range m.entries {
		if m.isPinnedLocked(id) {
			continue
		}
		directory := m.directories[id.system]
		info, err := directory.root.Lstat(entry.name)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		result = append(result, evictionCandidate{
			id: id, key: protocol.ContentKey{SHA256: id.digest, Extension: id.extension}, name: entry.name, modified: info.ModTime(),
		})
	}
	sort.Slice(result, func(left, right int) bool {
		if !result[left].modified.Equal(result[right].modified) {
			return result[left].modified.Before(result[right].modified)
		}
		if result[left].name != result[right].name {
			return result[left].name < result[right].name
		}
		return result[left].id.system < result[right].id.system
	})
	return result, nil
}

func (m *Manager) applyCapacityPlan(plan capacityPlan) *protocol.APIError {
	if len(plan.victims) == 0 {
		return nil
	}
	type verifiedVictim struct {
		id       inventoryKey
		entry    inventoryEntry
		relative string
	}
	victims := make([]verifiedVictim, 0, len(plan.victims))
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range plan.victims {
		if m.isPinnedLocked(id) {
			return cacheFullAPIError()
		}
		entry, ok := m.entries[id]
		if !ok {
			return cacheFullAPIError()
		}
		if !m.directoriesIntact(id.system) {
			return internalAPIError("cache directory identity changed")
		}
		directory := m.directories[id.system]
		info, err := directory.root.Lstat(entry.name)
		if err != nil || !info.Mode().IsRegular() {
			return internalAPIError("cache eviction candidate cannot be inspected")
		}
		memo, ok := m.memos[id]
		if !ok || !memo.verified || !sameStamp(memo.stamp, fileStamp{info: info}) {
			return cacheFullAPIError()
		}
		relative := filepath.Join(string(id.system), entry.name)
		rootInfo, err := m.rootHandle.Lstat(relative)
		if err != nil || !sameStamp(memo.stamp, fileStamp{info: rootInfo}) || !m.directoriesIntact(id.system) {
			return internalAPIError("cache eviction candidate changed")
		}
		victims = append(victims, verifiedVictim{id: id, entry: entry, relative: relative})
	}
	for _, victim := range victims {
		if err := m.rootHandle.Remove(victim.relative); err != nil {
			return internalAPIError("cache eviction could not finish")
		}
		m.removeEntry(victim.id, victim.entry)
	}
	return nil
}

func (m *Manager) refreshAccountingLocked() *protocol.APIError {
	rootEntries, err := readDirect(m.rootHandle)
	if err != nil {
		return internalAPIError("cache accounting cannot read its root")
	}
	var usage int64
	for _, direct := range rootEntries {
		info, statErr := m.rootHandle.Lstat(direct.Name())
		if statErr != nil {
			return internalAPIError("cache accounting cannot inspect a root entry")
		}
		system := protocol.System(direct.Name())
		if _, registered := m.extensions[system]; registered && info.IsDir() {
			bound, ok := m.directories[system]
			if !ok || !sameFileInfo(bound.info, info) || !m.directoriesIntact(system) {
				return internalAPIError("cache directory identity changed")
			}
			continue
		}
		usage = addKnownSize(usage, info)
	}

	entries := make(map[inventoryKey]inventoryEntry)
	for system, allowed := range m.extensions {
		directory, ok := m.directories[system]
		if !ok || !m.directoriesIntact(system) {
			return internalAPIError("cache directory identity changed")
		}
		direct, readErr := readDirect(directory.root)
		if readErr != nil {
			return internalAPIError("cache accounting cannot read a system directory")
		}
		for _, item := range direct {
			info, statErr := directory.root.Lstat(item.Name())
			if statErr != nil {
				return internalAPIError("cache accounting cannot inspect an entry")
			}
			usage = addKnownSize(usage, info)
			key, valid := parseInventoryName(item.Name(), allowed)
			if !valid || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > protocol.MaxContentBytes {
				continue
			}
			id := makeInventoryKey(system, key)
			entries[id] = inventoryEntry{
				name: item.Name(), path: filepath.Join(directory.path, item.Name()), accountedSize: knownSize(info),
			}
		}
	}
	m.entries = entries
	m.usage = usage
	for id := range m.memos {
		if _, ok := entries[id]; !ok {
			delete(m.memos, id)
		}
	}
	return nil
}

func addKnownSize(total int64, info os.FileInfo) int64 {
	size := knownSize(info)
	if size > math.MaxInt64-total {
		return math.MaxInt64
	}
	return total + size
}

func (m *Manager) isPinnedLocked(id inventoryKey) bool {
	return (m.uploading != nil && *m.uploading == id) ||
		(m.active != nil && *m.active == id) ||
		(m.inFlight != nil && *m.inFlight == id)
}

func defaultSpaceProbe(path string) (int64, error) {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return 0, err
	}
	blocks := uint64(stats.Bavail)
	blockSize := uint64(stats.Bsize)
	if blockSize != 0 && blocks > uint64(math.MaxInt64)/blockSize {
		return math.MaxInt64, nil
	}
	return int64(blocks * blockSize), nil
}

func newUploadPartName() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf(".fogcast-upload-%x.part", token[:]), nil
}

func uploadResponse(result protocol.CacheUploadResult, system protocol.System, content protocol.ContentIdentity) protocol.CacheUploadResponse {
	return protocol.CacheUploadResponse{Result: result, System: system, Content: content}
}

func badRequestAPIError(message string) *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Message: message}
}

func transferAPIError(message string) *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeTransferFailed, Message: message}
}

func cacheFullAPIError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeCacheFull, Message: "target cache has insufficient safe capacity"}
}
