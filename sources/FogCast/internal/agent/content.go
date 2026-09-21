package agent

import (
	"context"
	"io"

	"github.com/DeanoC/FogCast/protocol"
)

type ContentStore interface {
	Probe(context.Context, protocol.System, protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError)
	Put(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError)
}

type ContentController struct {
	coordinator *Coordinator
	store       ContentStore
}

func NewContentController(coordinator *Coordinator, store ContentStore) *ContentController {
	return &ContentController{coordinator: coordinator, store: store}
}

type cacheIndexStore interface {
	CacheIndex() protocol.CacheIndex
}

// CacheIndex is the lease-free ROM cache used/free inventory. It never
// claims the kit lease and never walks the cover store.
func (c *ContentController) CacheIndex() (protocol.CacheIndex, *protocol.APIError) {
	empty := protocol.CacheIndex{Entries: []protocol.CacheIndexEntry{}}
	if c == nil || c.store == nil {
		return empty, nil
	}
	indexer, ok := c.store.(cacheIndexStore)
	if !ok {
		return empty, nil
	}
	index := indexer.CacheIndex()
	if index.Entries == nil {
		index.Entries = []protocol.CacheIndexEntry{}
	}
	return index, nil
}

func (c *ContentController) ProbeContent(ctx context.Context, system protocol.System, key protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError) {
	if err := protocol.ValidateSystem(system); err != nil {
		return protocol.CacheProbeResponse{Present: false}, unsupportedSystemError()
	}
	if err := protocol.ValidateContentKey(key); err != nil {
		return protocol.CacheProbeResponse{Present: false}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "content key is invalid"}
	}
	return c.store.Probe(ctx, system, key)
}

func (c *ContentController) PutContent(ctx context.Context, system protocol.System, content protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
	if err := protocol.ValidateSystem(system); err != nil {
		return protocol.CacheUploadResponse{}, unsupportedSystemError()
	}
	if err := protocol.ValidateContentIdentity(content); err != nil || body == nil {
		return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "content upload is invalid"}
	}
	return c.store.Put(ctx, system, content, body)
}

func unsupportedSystemError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "system is not registered"}
}
