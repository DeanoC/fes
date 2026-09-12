package protocol

import (
	"io"
	"net/http"
	"strconv"
)

const MaxDevelopmentMediaBytes int64 = 16384
const CorePackageIDHeader = "X-FogCast-Package-ID"
const CoreGenerationHeader = "X-FogCast-Core-Generation"
const HostSessionIDHeader = "X-FogCast-Session-ID"
const HostTargetHeader = "X-FogCast-Target"
const HostTargetIDHeader = "X-FogCast-Target-ID"

// DevelopmentMediaBinding names the already active target package generation.
// It is checked by FogCast; the local runtime load_media wire shape is unchanged.
type DevelopmentMediaBinding struct {
	PackageID  string
	Generation uint64
	Target     string // Host-only binding; not sent to the local runtime.
	TargetID   string
}

func (b DevelopmentMediaBinding) Valid() bool {
	if len(b.PackageID) != 64 || b.Generation == 0 {
		return false
	}
	for _, c := range b.PackageID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (b DevelopmentMediaBinding) Matches(s Status) bool {
	return b.Valid() && s.State == StateActive && s.Development && s.Recovery == "" && s.LastError == nil &&
		s.CorePackage != nil && s.CorePackage.PackageID == b.PackageID && s.CorePackage.Generation == b.Generation && DevelopmentMediaCapable(s.CorePackage)
}
func DevelopmentMediaCapable(p *CorePackageStatus) bool {
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
func (b DevelopmentMediaBinding) SetHeaders(h http.Header) {
	h.Set(CorePackageIDHeader, b.PackageID)
	h.Set(CoreGenerationHeader, strconv.FormatUint(b.Generation, 10))
	if b.Target != "" {
		h.Set(HostTargetHeader, b.Target)
	}
	if b.TargetID != "" {
		h.Set(HostTargetIDHeader, b.TargetID)
	}
}
func DevelopmentMediaHeaders(h http.Header) (DevelopmentMediaBinding, bool) {
	ids, gens := h.Values(CorePackageIDHeader), h.Values(CoreGenerationHeader)
	if len(ids) != 1 || len(gens) != 1 {
		return DevelopmentMediaBinding{}, false
	}
	gen, err := strconv.ParseUint(gens[0], 10, 64)
	b := DevelopmentMediaBinding{PackageID: ids[0], Generation: gen, Target: h.Get(HostTargetHeader), TargetID: h.Get(HostTargetIDHeader)}
	return b, err == nil && b.Valid() && strconv.FormatUint(gen, 10) == gens[0]
}
func DevelopmentMediaRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "development media requires 1..16384 bytes and a package generation", Phase: "request"}
}
func DevelopmentMediaIdentityError() *APIError {
	return &APIError{Code: CodeBusy, Message: "development media requires the current active fes.simple-computer media package generation", Phase: "admission"}
}

// ReadDevelopmentMedia snapshots and validates the entire bounded body before mutation.
func ReadDevelopmentMedia(size int64, body io.Reader) ([]byte, *APIError) {
	if size < 1 || size > MaxDevelopmentMediaBytes || body == nil {
		return nil, DevelopmentMediaRequestError()
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxDevelopmentMediaBytes+1))
	if err != nil || int64(len(data)) != size {
		return nil, DevelopmentMediaRequestError()
	}
	return data, nil
}
