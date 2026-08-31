package targetcache

import (
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

type inventoryKey struct {
	system    protocol.System
	digest    string
	extension string
}

func makeInventoryKey(system protocol.System, key protocol.ContentKey) inventoryKey {
	return inventoryKey{system: system, digest: key.SHA256, extension: key.Extension}
}

func (m *Manager) checkedPath(system protocol.System, key protocol.ContentKey) (inventoryKey, string, *protocol.APIError) {
	if err := protocol.ValidateContentKey(key); err != nil {
		return inventoryKey{}, "", &protocol.APIError{
			Code:    protocol.CodeBadRequest,
			Message: "content key is invalid",
		}
	}
	allowed, ok := m.extensions[system]
	if !ok {
		return inventoryKey{}, "", &protocol.APIError{
			Code:    protocol.CodeUnsupportedSystem,
			Message: "system is not registered for the target cache",
		}
	}
	if _, ok := allowed["."+key.Extension]; !ok {
		return inventoryKey{}, "", &protocol.APIError{
			Code:    protocol.CodeUnsupportedSystem,
			Message: "content extension is not supported for the selected system",
		}
	}

	id := makeInventoryKey(system, key)
	path := filepath.Join(m.root, string(system), key.SHA256+"."+key.Extension)
	relative, err := filepath.Rel(m.root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return inventoryKey{}, "", internalAPIError("cache destination cannot be constructed safely")
	}
	return id, path, nil
}

func parseInventoryName(name string, allowed map[string]struct{}) (protocol.ContentKey, bool) {
	extension := filepath.Ext(name)
	if extension == "" {
		return protocol.ContentKey{}, false
	}
	if _, ok := allowed[extension]; !ok {
		return protocol.ContentKey{}, false
	}
	key := protocol.ContentKey{
		SHA256:    strings.TrimSuffix(name, extension),
		Extension: strings.TrimPrefix(extension, "."),
	}
	if protocol.ValidateContentKey(key) != nil || name != key.SHA256+"."+key.Extension {
		return protocol.ContentKey{}, false
	}
	return key, true
}

func internalAPIError(message string) *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInternal, Message: message}
}

func absentProbe() protocol.CacheProbeResponse {
	return protocol.CacheProbeResponse{Present: false}
}
