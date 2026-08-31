package agent

import (
	"context"
	"io"

	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

type ContentStore interface {
	Probe(context.Context, protocol.System, protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError)
	Put(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError)
	Resolve(context.Context, protocol.System, protocol.ContentIdentity) (targetcache.Resolved, *protocol.APIError)
	PinForLaunch(protocol.System, protocol.ContentIdentity) *protocol.APIError
	RecordLaunchIntent(protocol.System, protocol.ContentIdentity) (targetcache.LaunchIntent, *protocol.APIError)
	RecordDirectLaunchIntent(protocol.System) (targetcache.LaunchIntent, *protocol.APIError)
	AbortLaunch(protocol.System, protocol.ContentIdentity, targetcache.LaunchIntent) *protocol.APIError
	AbortDirectLaunch(protocol.System, targetcache.LaunchIntent) *protocol.APIError
	CommitDirectLaunch(protocol.System, targetcache.LaunchIntent) *protocol.APIError
	CommitLaunch(protocol.System, protocol.ContentIdentity) *protocol.APIError
	ClearActive() *protocol.APIError
	ActiveRecordSystems(context.Context) (targetcache.ActiveRecords, bool, *protocol.APIError)
	ReconcileActive(context.Context, protocol.Status, *targetcache.ActiveRecordEntry) *protocol.APIError
}

type ContentController struct {
	coordinator *Coordinator
	store       ContentStore
}

func NewContentController(coordinator *Coordinator, store ContentStore) *ContentController {
	coordinator.content = store
	return &ContentController{coordinator: coordinator, store: store}
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

func (c *ContentController) LaunchContent(parent context.Context, request protocol.CachedLaunchRequest) (response protocol.CachedLaunchResponse, resultErr *protocol.APIError) {
	response = protocol.CachedLaunchResponse{Content: request.Content}
	if !c.coordinator.begin() {
		response.Status = c.coordinator.Status()
		return response, &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	defer c.coordinator.end()
	response.Status = c.coordinator.Status()

	if err := protocol.ValidateGameID(request.GameID); err != nil {
		return response, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "game ID is invalid"}
	}
	if err := protocol.ValidateSystem(request.System); err != nil {
		return response, unsupportedSystemError()
	}
	spec, ok := c.coordinator.registry.Lookup(request.System)
	if !ok {
		return response, unsupportedSystemError()
	}
	if err := protocol.ValidateContentIdentity(request.Content); err != nil {
		return response, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "content identity is invalid"}
	}

	resolved, apiErr := c.store.Resolve(parent, request.System, request.Content)
	if apiErr != nil {
		response.Status = c.coordinator.Status()
		return response, apiErr
	}
	if apiErr := c.store.PinForLaunch(request.System, request.Content); apiErr != nil {
		response.Status = c.coordinator.Status()
		return response, apiErr
	}
	abort := true
	var launchIntent targetcache.LaunchIntent
	defer func() {
		if abort {
			if apiErr := c.store.AbortLaunch(request.System, request.Content, launchIntent); apiErr != nil && resultErr == nil {
				resultErr = apiErr
			}
		}
	}()

	spec.ROMRoot = resolved.Root
	status, dispatchAttempted, apiErr := c.coordinator.launchWithIntent(parent, request.GameID, spec, resolved.Path, func() *protocol.APIError {
		intent, apiErr := c.store.RecordLaunchIntent(request.System, request.Content)
		if apiErr == nil {
			launchIntent = intent
		}
		return apiErr
	})
	abort = !dispatchAttempted
	response.Status = status
	if apiErr != nil {
		return response, apiErr
	}

	if apiErr := c.store.CommitLaunch(request.System, request.Content); apiErr != nil {
		return response, apiErr
	}
	return response, nil
}

func unsupportedSystemError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "system is not Mega Drive or SNES"}
}
