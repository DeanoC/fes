package mister

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/protocol"
)

const fatRoot = "/media/fat"

func mglROMPath(root, resolved, fallback string) string {
	if root == "" {
		return filepath.ToSlash(fallback)
	}
	path, err := filepath.Rel(root, resolved)
	if err == nil && path != "." && path != ".." {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(fallback)
}

type PreparedLaunch struct {
	Spec        core.Spec
	AbsoluteROM string
	RelativeROM string
	MGL         []byte
}

func PrepareLaunch(spec core.Spec, candidate string) (PreparedLaunch, *protocol.APIError) {
	fail := func(code protocol.ErrorCode, message string) (PreparedLaunch, *protocol.APIError) {
		return PreparedLaunch{}, &protocol.APIError{Code: code, Message: message}
	}
	if spec.ROMless {
		return fail(protocol.CodeUnsupportedSystem, "ROM-less Pong requires the native runtime")
	}
	if strings.IndexByte(candidate, 0) >= 0 || !filepath.IsAbs(candidate) {
		return fail(protocol.CodeInvalidROMPath, "ROM path must be absolute and contain no NUL byte")
	}
	cleaned := filepath.Clean(candidate)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return fail(protocol.CodeROMNotFound, "ROM does not exist on the MiSTer SD card")
		}
		return fail(protocol.CodeInvalidROMPath, "ROM path cannot be resolved")
	}
	root, rootErr := filepath.EvalSymlinks(spec.ROMRoot)
	if rootErr == nil {
		relative, relativeErr := filepath.Rel(root, resolved)
		if relativeErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return fail(protocol.CodeInvalidROMPath, "ROM path escapes its registered root")
		}
	}
	if _, ok := spec.Extensions[strings.ToLower(filepath.Ext(resolved))]; !ok {
		return fail(protocol.CodeInvalidROMPath, "ROM extension is not allowed for the selected system")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return fail(protocol.CodeROMNotFound, "ROM does not identify a regular file")
	}
	relative, _ := filepath.Rel(root, resolved)
	if rootErr != nil {
		relative = filepath.Base(resolved)
	}
	relative = filepath.ToSlash(relative)
	mglRoot := spec.MGLRoot
	if mglRoot == "" {
		mglRoot = spec.ROMRoot
	}
	mgl, err := RenderMGL(spec, mglROMPath(mglRoot, resolved, relative))
	if err != nil {
		return fail(protocol.CodeInternal, "MGL rendering failed")
	}
	return PreparedLaunch{Spec: spec, AbsoluteROM: resolved, RelativeROM: relative, MGL: mgl}, nil
}
