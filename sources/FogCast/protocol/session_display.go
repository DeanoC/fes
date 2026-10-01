package protocol

// SessionDisplayRequest changes launcher focus on an existing core generation.
// A pointer distinguishes an explicit close from an omitted or null field.
type SessionDisplayRequest struct {
	Visible *bool `json:"visible"`
}

// SessionDisplayCapable uses the interfaces actually identified on the FPGA.
// A package declaration or the name of a machine does not grant this capability.
func SessionDisplayCapable(p *CorePackageStatus) bool {
	if p == nil || p.ABI != (RuntimeContract{ID: "fes.simple-computer", Major: 1}) {
		return false
	}
	ddr, display := false, false
	for _, i := range p.ActiveInterfaces {
		if i.Major != 1 || i.Minor != 0 {
			continue
		}
		switch i.ID {
		case "fes.memory.hps-ddr":
			ddr = true
		case "fes.video.session-display":
			display = true
		}
	}
	return ddr && display
}

func (b DevelopmentMediaBinding) MatchesSessionDisplay(s Status) bool {
	return b.MatchesLive(s) && SessionDisplayCapable(s.CorePackage)
}

func SessionDisplayIdentityError() *APIError {
	return &APIError{Code: CodeBusy, Message: "launcher display requires the current active display-capable package generation", Phase: "admission"}
}

func SessionDisplayRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "launcher display requires an explicit visible boolean and a package generation", Phase: "request"}
}
