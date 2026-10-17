package kitsoftware

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/appliance/software"
)

// ResolveRelease mirrors the actual executable selection. A retained factory
// copy has identical content but different inodes from the base-image factory;
// readiness and operator repair must inspect the path the supervisor starts.
func ResolveRelease(ctx context.Context, id, factoryPath string, factory software.Release, base software.Compatibility, store *software.Store) (string, error) {
	if id == factory.ID {
		verified, err := software.VerifyDirectory(ctx, factoryPath)
		if err != nil {
			return "", err
		}
		if verified.ID != id {
			return "", errors.New("factory software identity changed")
		}
		return factoryPath, verified.Manifest.Compatible(base)
	}
	if store == nil {
		return "", errors.New("durable release storage unavailable")
	}
	return store.Path(ctx, id)
}
