package protocol

import (
	"errors"
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

// LiveMediaCapable reports a session that can change a mid-session .p tape.
// The legacy case is fes.simple-computer 1.0 with active fes.media.blob 1.0.
// An active fes.computer 1.0 generation with fes.media.zx81-tape 1.0 uses the
// tape unit instead. fes.application stays on launch-time load_media.
func LiveMediaCapable(p *CorePackageStatus) bool {
	return liveBlobCapable(p) || liveComputerZx81TapeCapable(p)
}

func liveBlobCapable(p *CorePackageStatus) bool {
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

func liveComputerZx81TapeCapable(p *CorePackageStatus) bool {
	return p != nil && ComputerABI(p.ABI.ID, int64(p.ABI.Major), int64(p.ABI.Minor)) &&
		activeInterface(p, Zx81TapeInterface())
}

// ComputerZx81TapeCapable reports an active fes.computer 1.0 generation with
// fes.media.zx81-tape 1.0. Kit HDMI controls still require the session-display
// plane; this predicate only identifies the tape unit.
func ComputerZx81TapeCapable(p *CorePackageStatus) bool {
	return liveComputerZx81TapeCapable(p)
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

// MaxRemovableMediaBytes bounds one image for every unit the running media
// selector changes, including the legacy blob mailbox. A Spectrum tape image
// is bounded before the host converts it to the unit's PZX stream.
const MaxRemovableMediaBytes = max(MaxDevelopmentMediaBytes, Zx81TapeMaxBytes, SpectrumTapeImageMaxBytes,
	Apple2FloppyBytes, C64DiskBytes, AtariStFloppyBytes, AtariStFloppyGeometryMaxBytes)

// AdmitRemovableMediaSize bounds a kit launcher import. The running unit
// still applies its own format and size limits on insert.
func AdmitRemovableMediaSize(size int64) bool {
	return size >= 1 && size <= MaxRemovableMediaBytes
}

// MatchesLive admits the legacy mailbox generation. A fes.computer ZX81 tape
// insert uses the media unit and does not pass this mailbox check.
func (b DevelopmentMediaBinding) MatchesLive(s Status) bool {
	if !b.Valid() || b.Stream || b.Role == FirmwareRole || s.State != StateActive || !s.Development ||
		s.Recovery != "" || s.LastError != nil || s.CorePackage == nil ||
		s.CorePackage.PackageID != b.PackageID || s.CorePackage.Generation != b.Generation {
		return false
	}
	return liveBlobCapable(s.CorePackage)
}

func LiveMediaRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "live media requires a .p/.P tape of 1..16384 bytes and a package generation", Phase: "request", NoChange: true}
}

func LiveMediaIdentityError() *APIError {
	return &APIError{Code: CodeBusy, Message: "live media requires the current active simple-computer media-capable package generation", Phase: "admission", NoChange: true}
}

func LiveMediaBusyError() *APIError {
	return &APIError{Code: CodeBusy, Message: "tape loader is busy; retry after LOAD finishes", Phase: "input", NoChange: true}
}

// LiveMediaInputUncertainError stays retryable but does not claim that the
// current media survived an unstable input exchange.
func LiveMediaInputUncertainError() *APIError {
	return &APIError{Code: CodeBusy, Message: "live media input outcome is uncertain; retry after checking the machine", Phase: "input"}
}

// LiveMediaNoChange reports whether a failed live-media mutation explicitly
// guarantees that the mounted medium was left alone.
func LiveMediaNoChange(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.NoChange
}

// LiveMediaRetryError preserves uncertainty from an earlier failed attempt
// when a later retry returns a definite no-change rejection.
func LiveMediaRetryError(err error, priorUncertain bool) error {
	if !priorUncertain || !LiveMediaNoChange(err) {
		return err
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	copy := *apiErr
	copy.NoChange = false
	return &copy
}
