package fogcast

import (
	"context"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type remoteCacheCatalog interface {
	RemoteCassetteCache(context.Context, string, int) (catalog.RemoteCacheUsage, error)
	RemoveRemoteCassetteCache(context.Context, []string) (catalog.RemoteCacheCleanup, error)
}

// RemoteCassetteCache inspects tracked remote-only content without mutation.
func (s *Service) RemoteCassetteCache(ctx context.Context, after string, limit int) (catalog.RemoteCacheUsage, error) {
	store, ok := s.catalog.(remoteCacheCatalog)
	if !ok {
		return catalog.RemoteCacheUsage{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	usage, err := store.RemoteCassetteCache(ctx, after, limit)
	return usage, mapCoreMediaError(err)
}

// RemoveRemoteCassetteCache serializes with remote launch preparation and
// selection while the catalog transaction separately guards other processes.
func (s *Service) RemoveRemoteCassetteCache(ctx context.Context, ids []string) (catalog.RemoteCacheCleanup, error) {
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.RemoteCacheCleanup{}, err
	}
	defer release()
	store, ok := s.catalog.(remoteCacheCatalog)
	if !ok {
		return catalog.RemoteCacheCleanup{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	result, err := store.RemoveRemoteCassetteCache(ctx, ids)
	return result, mapCoreMediaError(err)
}
