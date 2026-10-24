package protocol

import (
	"encoding/json"
	"errors"
)

// SessionFocus names who receives input while a session display is shown.
type SessionFocus string

const (
	SessionFocusMachine  SessionFocus = "machine"
	SessionFocusLauncher SessionFocus = "launcher"
)

// UnmarshalJSON refuses null and the empty string. A request reads the empty
// focus as an omitted one, so a focus that is present must name an owner.
func (f *SessionFocus) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	if name == "" {
		return errors.New("session focus must not be null or empty")
	}
	*f = SessionFocus(name)
	return nil
}

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
// null field. An omitted focus is the one visibility implies, launcher for a
// shown plane and machine for a closed one, so the hosts and kits that ship
// separately agree on every request but a surround's. A null or empty focus
// is not an omitted one and does not decode.
type SessionDisplayRequest struct {
	Visible *bool        `json:"visible"`
	Focus   SessionFocus `json:"focus,omitempty"`
}

// defaultSessionFocus is the focus a request without one asks for.
func defaultSessionFocus(visible bool) SessionFocus {
	if visible {
		return SessionFocusLauncher
	}
	return SessionFocusMachine
}

// NewSessionDisplayRequest names focus only when it differs from the default.
func NewSessionDisplayRequest(d SessionDisplay) SessionDisplayRequest {
	visible := d.Visible
	r := SessionDisplayRequest{Visible: &visible}
	if d.Focus != defaultSessionFocus(d.Visible) {
		r.Focus = d.Focus
	}
	return r
}

// Display returns the requested state, or false for an omitted visibility,
// an unknown focus or a closed plane with launcher focus.
func (r SessionDisplayRequest) Display() (SessionDisplay, bool) {
	if r.Visible == nil {
		return SessionDisplay{}, false
	}
	d := SessionDisplay{Visible: *r.Visible, Focus: r.Focus}
	if d.Focus == "" {
		d.Focus = defaultSessionFocus(d.Visible)
	}
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
	if !simple && !computer && !applicationMachine(p) {
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

// applicationMachine is a running fes.application machine, such as the Coleco
// native shell. The idle MENU is also fes.application but never runs as a
// session.
func applicationMachine(p *CorePackageStatus) bool {
	return p.ABI == (RuntimeContract{ID: "fes.application", Major: 1})
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
	// A cartridge machine has no removable media; its plane is the whole contract.
	if applicationMachine(s.CorePackage) {
		return true
	}
	// Preserve the existing blob and ZX81 tape paths. Other supported computers
	// use the same plane when their unit-0 interface and limits are observed.
	if liveBlobCapable(s.CorePackage) || liveComputerZx81TapeCapable(s.CorePackage) {
		return true
	}
	unit, ok := MediaUnit(s.CorePackage, 0)
	if !ok {
		return false
	}
	switch unit.Interface {
	case Apple2FloppyInterface(), C64DiskInterface(), AtariStFloppyInterface(), SpectrumTapeInterface():
		return true
	}
	return false
}

func SessionDisplayIdentityError() *APIError {
	return &APIError{Code: CodeBusy, Message: "launcher display requires the current active display-capable package generation", Phase: "admission"}
}

func SessionDisplayRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "launcher display requires an explicit visible boolean, a matching focus and a package generation", Phase: "request"}
}
