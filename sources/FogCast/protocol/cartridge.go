package protocol

import "strings"

// CartridgeRequest relaunches the running cartridge machine with another
// cartridge, POST /api/v1/session/cartridge. The cartridge is linked into the
// bitstream, so this is a new load of the same title, never a live insert,
// and the title's own cartridge binding is unchanged.
type CartridgeRequest struct {
	MediaID string `json:"media_id"`
	Name    string `json:"name"`
}

// AdmitCartridgeName accepts the usual cartridge dump extensions.
func AdmitCartridgeName(name string) bool {
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	lower := strings.ToLower(name)
	for _, ext := range []string{".col", ".rom", ".bin"} {
		if strings.HasSuffix(lower, ext) && len(lower) > len(ext) {
			return true
		}
	}
	return false
}

// CartridgeSwapCapable is a running fes.application machine whose cartridge
// is linked into its bitstream, alone or after its BIOS, with the session
// plane its selector is drawn on.
func CartridgeSwapCapable(p *CorePackageStatus) bool {
	if p == nil || !applicationMachine(p) || !SessionDisplayCapable(p) {
		return false
	}
	if p.ROMLinks != nil {
		return p.ROMLink == nil && len(p.ROMLinks.Sources) == 2 && p.ROMLinks.Sources[1].Role == "cartridge"
	}
	return p.ROMLink != nil
}

// MatchesCartridgeSwap admits the request's binding only for the exact
// running cartridge machine generation.
func (b DevelopmentMediaBinding) MatchesCartridgeSwap(s Status) bool {
	return b.MatchesSessionDisplay(s) && CartridgeSwapCapable(s.CorePackage)
}

func CartridgeRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "cartridge swap requires a .col/.rom/.bin cartridge media id and a package generation", Phase: "request"}
}

func CartridgeIdentityError() *APIError {
	return &APIError{Code: CodeBusy, Message: "cartridge swap requires the current active cartridge machine generation", Phase: "admission"}
}
