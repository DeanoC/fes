// Package hostapi exposes the local, privacy-safe FogCast application API.
package hostapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
)

// Service is the host operation surface used by the privacy-safe application API.
type Service interface {
	Games(context.Context) ([]catalog.Game, error)
	Search(context.Context, string) ([]catalog.Game, error)
	Game(context.Context, string) (catalog.Game, error)
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
	LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
	// MeshBackendLibrary is the combined library. The host API does not
	// treat a missing projection as an empty title list.
	MeshBackendLibrary(context.Context) ([]fogcast.MeshBackendRow, []fogcast.MeshSkip)
}

type serverOptions struct {
	storage            *storage.Manager
	remoteInput        host.RemoteInputController
	remoteInputFactory RemoteInputFactory
	media              MediaSession
	mediaPreview       http.Handler
	metadata           metadata.Runtime
	metadataState      metadata.ConfigState
}

type ServerOption func(*serverOptions)

func WithRemoteInput(remoteInput host.RemoteInputController) ServerOption {
	return func(options *serverOptions) { options.remoteInput = remoteInput }
}

// RemoteInputFactory builds the input bridge for one named target session. An
// error means remote input is configured but that target's bridge could not be
// built; the input routes report it instead of a disabled host.
type RemoteInputFactory func(target string) (host.RemoteInputController, error)

// WithRemoteInputFactory provides an independent input bridge for each named
// target session. The factory must be concurrency-safe.
func WithRemoteInputFactory(factory RemoteInputFactory) ServerOption {
	return func(options *serverOptions) { options.remoteInputFactory = factory }
}

func WithMediaSession(media MediaSession) ServerOption {
	return func(options *serverOptions) { options.media = media }
}

// WithMediaPreview exposes a browser-safe picture stream owned by the active
// host media session.
func WithMediaPreview(preview http.Handler) ServerOption {
	return func(options *serverOptions) { options.mediaPreview = preview }
}

func WithMetadata(runtime metadata.Runtime, states ...metadata.ConfigState) ServerOption {
	return func(options *serverOptions) {
		options.metadata = runtime
		if len(states) != 0 {
			options.metadataState = states[0]
		} else if runtime != nil {
			options.metadataState = metadata.StateReady
		} else {
			options.metadataState = metadata.StateUnconfigured
		}
	}
}

func requestSessionCoordinator(root *sessionCoordinator, service Service, r *http.Request, target string, foregroundOnly ...bool) *sessionCoordinator {
	if target == "" {
		target = launcherTargetFromContext(r.Context())
	}
	if target == "" {
		if pairedID := fogcast.PairedTargetFromContext(r.Context()); pairedID != "" {
			if resolver, ok := service.(interface{ TargetNameForID(string) string }); ok {
				target = resolver.TargetNameForID(pairedID)
			}
		}
	}
	if target == "" {
		// Unscoped reads and mutations follow the foreground session, falling
		// back to the selected target only when no kit is active.
		if len(foregroundOnly) > 0 && foregroundOnly[0] {
			if resolver, ok := service.(interface{ ForegroundSessionTargetName() string }); ok {
				target = resolver.ForegroundSessionTargetName()
				// A restarted kit host has not observed its one local target
				// yet. Use that coordinator on the first read as well, so
				// adoption does not change session identity or input owner.
				if target == "" {
					if kit, ok := service.(interface{ KitHostMode() bool }); ok && kit.KitHostMode() {
						if selected, ok := service.(interface{ SessionTargetName() string }); ok {
							target = selected.SessionTargetName()
						}
					}
				}
			} else if resolver, ok := service.(interface{ SessionTargetName() string }); ok {
				target = resolver.SessionTargetName()
			} else if resolver, ok := service.(interface{ SelectedTargetConfig() fogcast.TargetConfig }); ok {
				target = resolver.SelectedTargetConfig().Name
			}
		} else if resolver, ok := service.(interface{ SessionTargetName() string }); ok {
			target = resolver.SessionTargetName()
		} else if resolver, ok := service.(interface{ SelectedTargetConfig() fogcast.TargetConfig }); ok {
			target = resolver.SelectedTargetConfig().Name
		}
	}
	return root.forTarget(target)
}

func New(service Service, options ...ServerOption) http.Handler {
	if service == nil {
		panic("hostapi: nil service")
	}
	var config serverOptions
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	mux := http.NewServeMux()
	registerStorage(mux, config.storage)
	registerCoreLibrary(mux, service)
	registerKitSoftwareInventory(mux, service)
	registerCoreData(mux, service)
	registerMeshHostContent(mux, service)
	registerLibraryTitles(mux, service)
	registerLibrarySources(mux, service)
	session := newSessionCoordinator(service, config.remoteInput, config.media)
	session.remoteInputFactory = config.remoteInputFactory
	registerHardware(mux, service, session)
	uiEvents := newUIEventRing(uiEventRingCapacity)
	registerDebugUIRoutes(mux, uiEvents)
	registerSessionRoutes(mux, service, session, config.mediaPreview)
	registerSessionInputRoutes(mux, service, session)
	registerStatusAndCatalogRoutes(mux, service)
	registerPresentationRoutes(mux, service, config)
	return &applicationHandler{browser: noStore(rejectUnexpectedHost(mux)), routes: mux, service: service, remoteInput: config.remoteInput, session: session}
}
