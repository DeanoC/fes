// Package sessiondisplayipc coordinates launcher display transitions between
// the kit agent and launcher on the same machine.
package sessiondisplayipc

import (
	"errors"
	"github.com/DeanoC/FogCast/protocol"
	"strings"
	"unicode"
)

const LauncherSocket = "/run/fogcast/launcher-display.sock"

var ErrUnsupported = errors.New("session display IPC is unsupported on this platform")

type Transition struct {
	PackageID     string                  `json:"package_id"`
	Generation    uint64                  `json:"generation"`
	TargetID      string                  `json:"target_id,omitempty"`
	Display       protocol.SessionDisplay `json:"display"`
	LeaseAdmitted bool                    `json:"lease_admitted,omitempty"`
}

func (t Transition) Valid() bool {
	if len(t.PackageID) != 64 || t.Generation == 0 || !t.Display.Valid() || len(t.TargetID) > 256 || strings.TrimSpace(t.TargetID) != t.TargetID {
		return false
	}
	for _, c := range t.PackageID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	for _, c := range t.TargetID {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

type Outcome string

const (
	Applied  Outcome = "applied"
	Rejected Outcome = "rejected"
	Unknown  Outcome = "unknown"
)

func (o Outcome) valid() bool { return o == Applied || o == Rejected || o == Unknown }
