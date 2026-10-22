package protocol

// SessionFocus names who receives input while a session display is shown.
type SessionFocus string

const (
	SessionFocusMachine  SessionFocus = "machine"
	SessionFocusLauncher SessionFocus = "launcher"
)

// SessionDisplay is the running machine's launcher plane: whether it is
// composited over the machine picture, and who receives input. A closed
// plane always leaves input with the machine.
type SessionDisplay struct {
	Visible bool
	Focus   SessionFocus
}

var (
	// SessionDisplayClosed shows machine pixels only.
	SessionDisplayClosed = SessionDisplay{Focus: SessionFocusMachine}
	// SessionDisplayShown composites the plane while the machine keeps
	// input, as a surround around the machine picture does.
	SessionDisplayShown = SessionDisplay{Visible: true, Focus: SessionFocusMachine}
	// SessionDisplayFocused composites the plane and gives the launcher input.
	SessionDisplayFocused = SessionDisplay{Visible: true, Focus: SessionFocusLauncher}
)

func (d SessionDisplay) Valid() bool {
	return d == SessionDisplayClosed || d == SessionDisplayShown || d == SessionDisplayFocused
}

// SessionDisplayRequest changes the launcher plane on an existing core
// generation. A pointer distinguishes an explicit close from an omitted or
// null field.
type SessionDisplayRequest struct {
	Visible *bool        `json:"visible"`
	Focus   SessionFocus `json:"focus"`
}

func NewSessionDisplayRequest(d SessionDisplay) SessionDisplayRequest {
	visible := d.Visible
	return SessionDisplayRequest{Visible: &visible, Focus: d.Focus}
}

// Display returns the requested state, or false for an omitted visibility,
// an unknown focus or a closed plane with launcher focus.
func (r SessionDisplayRequest) Display() (SessionDisplay, bool) {
	if r.Visible == nil {
		return SessionDisplay{}, false
	}
	d := SessionDisplay{Visible: *r.Visible, Focus: r.Focus}
	return d, d.Valid()
}

// SessionDisplayCapable uses the interfaces actually identified on the FPGA.
// A package declaration or the name of a machine does not grant this capability.
func SessionDisplayCapable(p *CorePackageStatus) bool {
	if p == nil {
		return false
	}
	simple := p.ABI == (RuntimeContract{ID: "fes.simple-computer", Major: 1})
	computer := p.ABI == (RuntimeContract{ID: "fes.computer", Major: 1})
	if !simple && !computer {
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
	if !b.Valid() || b.Stream || b.Role == FirmwareRole || s.State != StateActive || !s.Development ||
		s.Recovery != "" || s.LastError != nil || s.CorePackage == nil ||
		s.CorePackage.PackageID != b.PackageID || s.CorePackage.Generation != b.Generation {
		return false
	}
	if !SessionDisplayCapable(s.CorePackage) {
		return false
	}
	// The legacy plane rides the blob mailbox. A fes.computer ZX81 tape unit
	// uses the same plane once the running package reports it.
	return liveBlobCapable(s.CorePackage) || liveComputerZx81TapeCapable(s.CorePackage)
}

func SessionDisplayIdentityError() *APIError {
	return &APIError{Code: CodeBusy, Message: "launcher display requires the current active display-capable package generation", Phase: "admission"}
}

func SessionDisplayRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "launcher display requires an explicit visible boolean, a matching focus and a package generation", Phase: "request"}
}
