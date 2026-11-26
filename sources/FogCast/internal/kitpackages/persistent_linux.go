//go:build linux

package kitpackages

import (
	"context"
	"errors"
	"fmt"

	"github.com/DeanoC/FogCast/internal/appliancedata"
)

// OpenPersistent positively verifies the existing FESDATA3 mount before
// creating any library directory. The expected block device is the one
// appliancedata.Prepare selects, including a labelled USB partition.
// It neither mounts nor formats storage.
func OpenPersistent(root string) (*Store, error) {
	return openPersistent(context.Background(), root, true)
}

// OpenPersistentForHost opens the durable store without repeating the
// agent's recovery. Package access verifies publications on demand.
func OpenPersistentForHost(ctx context.Context, root string) (*Store, error) {
	return openPersistent(ctx, root, false)
}

func openPersistent(ctx context.Context, root string, recoverInstallations bool) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("kit packages: missing open context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkPersistent(root); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s, err := openStoreContext(ctx, root, recoverInstallations)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.persistent = true
	if err := s.CheckPersistent(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) CheckPersistent() error { return checkPersistent(s.root) }

func checkPersistent(root string) error {
	if root != DefaultRoot {
		return errors.New("kit packages: persistent library must use the FESDATA3 core-library root")
	}
	if err := appliancedata.CheckPersistent(root); err != nil {
		return fmt.Errorf("kit packages: %w", err)
	}
	return nil
}
