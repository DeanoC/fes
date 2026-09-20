package misterruntime

import (
	"io/fs"
	"os"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

// WithNativeCoreFS supplies the filesystem rooted at the target root. This
// permits isolated fixtures without creating files in the host's /usr paths.
func WithNativeCoreFS(files fs.FS) RuntimeOption {
	return func(r *Runtime) { r.nativeCoreFS = files }
}

func (r *Runtime) nativeCorePresent(system protocol.System) bool {
	path := nativeRBFPath(system)
	if path == "" {
		return false
	}
	files := r.nativeCoreFS
	if files == nil {
		files = os.DirFS("/")
	}
	name := strings.TrimPrefix(path, "/")
	info, err := fs.Stat(files, name)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return false
	}
	file, err := files.Open(name)
	if err != nil {
		return false
	}
	info, err = file.Stat()
	closeErr := file.Close()
	return err == nil && closeErr == nil && info.Mode().IsRegular() && info.Size() > 0
}

func (r *Runtime) nativeCoreAvailability() *protocol.NativeCoreAvailability {
	result := &protocol.NativeCoreAvailability{Version: 1, Systems: []protocol.System{}}
	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemNES, protocol.SystemPong, protocol.SystemSNES} {
		if r.nativeCorePresent(system) {
			result.Systems = append(result.Systems, system)
		}
	}
	return result
}

func missingNativeCoreError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "legacy native core artifact is unavailable on this target", Phase: "admission"}
}
