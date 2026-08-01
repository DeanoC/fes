package mister

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

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
	if strings.IndexByte(candidate, 0) >= 0 || !filepath.IsAbs(candidate) {
		return fail(protocol.CodeInvalidROMPath, "ROM path must be absolute and contain no NUL byte")
	}
	cleaned := filepath.Clean(candidate)
	root, err := filepath.EvalSymlinks(spec.ROMRoot)
	if err != nil {
		return fail(protocol.CodeInternal, "registered ROM root cannot be resolved")
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return fail(protocol.CodeROMNotFound, "ROM does not exist on the MiSTer SD card")
		}
		return fail(protocol.CodeInvalidROMPath, "ROM path cannot be resolved")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fail(protocol.CodeInvalidROMPath, "ROM path escapes its registered root")
	}
	if _, ok := spec.Extensions[strings.ToLower(filepath.Ext(resolved))]; !ok {
		return fail(protocol.CodeInvalidROMPath, "ROM extension is not allowed for the selected system")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return fail(protocol.CodeROMNotFound, "ROM does not identify a regular file")
	}
	relative = filepath.ToSlash(relative)
	mgl, err := RenderMGL(spec, relative)
	if err != nil {
		return fail(protocol.CodeInternal, "MGL rendering failed")
	}
	return PreparedLaunch{Spec: spec, AbsoluteROM: resolved, RelativeROM: relative, MGL: mgl}, nil
}
