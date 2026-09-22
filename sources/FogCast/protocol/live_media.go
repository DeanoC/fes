package protocol

import (
	"path/filepath"
	"strings"
)

// LiveMediaRequest arms or identifies a household core-media object for the
// active session generation. Name is required when a filename is known so
// admission can reject non-.p/.P tapes; media_id alone is size-gated.
type LiveMediaRequest struct {
	MediaID string `json:"media_id"`
	Name    string `json:"name,omitempty"`
}

// LiveMediaCapable is the mid-session ZX81 / fes.simple-computer mailbox path.
// fes.application uses launch hold-reset load_media, not replace_live_media.
func LiveMediaCapable(p *CorePackageStatus) bool {
	if p == nil || p.ABI.ID != "fes.simple-computer" || p.ABI.Major != 1 || p.ABI.Minor != 0 {
		return false
	}
	for _, i := range p.ActiveInterfaces {
		if i.ID == "fes.media.blob" && i.Major == 1 && i.Minor == 0 {
			return true
		}
	}
	return false
}

// AdmitTapeMediaName accepts only ZX81 .p / .P basenames.
func AdmitTapeMediaName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	ext := filepath.Ext(name)
	return ext == ".p" || ext == ".P"
}

// AdmitTapeMediaSize is the fes.media.blob 1.0 mailbox bound.
func AdmitTapeMediaSize(size int64) bool {
	return size >= 1 && size <= MaxDevelopmentMediaBytes
}

func (b DevelopmentMediaBinding) MatchesLive(s Status) bool {
	if !b.Valid() || b.Stream || b.Role == FirmwareRole || s.State != StateActive || !s.Development ||
		s.Recovery != "" || s.LastError != nil || s.CorePackage == nil ||
		s.CorePackage.PackageID != b.PackageID || s.CorePackage.Generation != b.Generation {
		return false
	}
	return LiveMediaCapable(s.CorePackage)
}

func LiveMediaRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "live media requires a .p/.P tape of 1..16384 bytes and a package generation", Phase: "request"}
}

func LiveMediaIdentityError() *APIError {
	return &APIError{Code: CodeBusy, Message: "live media requires the current active simple-computer media-capable package generation", Phase: "admission"}
}

func LiveMediaBusyError() *APIError {
	return &APIError{Code: CodeBusy, Message: "tape loader is busy; retry after LOAD finishes", Phase: "input"}
}
