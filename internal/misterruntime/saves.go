package misterruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/protocol"
)

// WithSaveRoot enables target-local SNES battery saves. An empty root keeps
// explicit standalone adapter uses volatile; the production agent sets it.
func WithSaveRoot(path string) RuntimeOption {
	return func(r *Runtime) { r.saveRoot = path }
}

func (r *Runtime) prepareSavePath(ctx context.Context, prepared mister.PreparedLaunch) (string, *protocol.APIError) {
	if r.saveRoot == "" || prepared.Spec.System != protocol.SystemSNES {
		return "", nil
	}
	fail := func() (string, *protocol.APIError) {
		return "", &protocol.APIError{Code: protocol.CodeInternal, Message: "SNES save storage is unavailable"}
	}
	if !validRuntimePath(r.saveRoot) || protocol.ValidateGameID(prepared.GameID) != nil || ctx.Err() != nil {
		return fail()
	}
	file, err := os.Open(prepared.AbsoluteROM)
	if err != nil {
		return fail()
	}
	hasher := sha256.New()
	// Match the admitted cartridge bound including an optional copier header.
	const maximumCartridge = (4 << 20) + 512
	count, readErr := io.Copy(hasher, io.LimitReader(&contextReader{ctx: ctx, reader: file}, maximumCartridge+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || count == 0 || count > maximumCartridge {
		return fail()
	}
	if err := os.MkdirAll(r.saveRoot, 0700); err != nil {
		return fail()
	}
	root, err := filepath.EvalSymlinks(r.saveRoot)
	if err != nil {
		return fail()
	}
	gameHash := sha256.Sum256([]byte(prepared.GameID))
	directory := filepath.Join(root, fmt.Sprintf("%x", gameHash))
	if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return fail()
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fail()
	}
	path := filepath.Join(directory, fmt.Sprintf("%x.srm", hasher.Sum(nil)))
	if !validRuntimePath(path) {
		return fail()
	}
	// Test actual writability before hardware mutation without creating save bytes.
	probe, err := os.CreateTemp(directory, ".write-check-")
	if err != nil {
		return fail()
	}
	closeErr = probe.Close()
	removeErr := os.Remove(probe.Name())
	if closeErr != nil || removeErr != nil || ctx.Err() != nil {
		return fail()
	}
	return path, nil
}

// A failed snapshot leaves the SNES session available only for a Stop retry.
func retryableSaveFailure(response Response) bool {
	if response.Error == nil || response.Error.Code != "save_failed" {
		return false
	}
	response.Error = nil
	response.OK = true
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	return validProfileState(response, spec, "running_game")
}
