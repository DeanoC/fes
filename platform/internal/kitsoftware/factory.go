package kitsoftware

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/DeanoC/FogCast/appliance/software"
)

// RetainFactory installs the authenticated read-only factory before selecting it
// durably. A later base image can then resolve the prior factory by content ID.
func RetainFactory(ctx context.Context, store *software.Store, factory software.Release, path string) error {
	verified, err := software.VerifyDirectory(ctx, path)
	if err != nil {
		return err
	}
	if verified.ID != factory.ID {
		return errors.New("factory software changed before retention")
	}
	if existing, err := store.Read(ctx, factory.ID); err == nil && existing.ID == factory.ID {
		return nil
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := software.WriteBundle(writer, factory.Manifest, os.DirFS(path))
		_ = writer.CloseWithError(err)
		done <- err
	}()
	installed, _, installErr := store.Install(ctx, reader)
	_ = reader.CloseWithError(installErr)
	writeErr := <-done
	if err := errors.Join(installErr, writeErr); err != nil {
		return err
	}
	if installed.ID != factory.ID {
		return errors.New("retained factory identity differs from verified factory")
	}
	return nil
}
