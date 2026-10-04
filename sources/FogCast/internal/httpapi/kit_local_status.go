package httpapi

import (
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

// KitLocalRun is the kit-local core currently launching, running, or stopping.
// Phase uses the local-control words idle, launching, running, and stopping.
// CoreID is the installed core id, such as fes.sms.
type KitLocalRun struct {
	Phase  string
	CoreID string
}

// WithKitLocalRun publishes a kit-local core on GET /v1/status while the
// host-session coordinator is idle. A host session still wins.
func WithKitLocalRun(read func() KitLocalRun) Option {
	return func(options *serverOptions) {
		if read != nil {
			options.kitLocal = read
		}
	}
}

// applyKitLocalRun replaces an idle host-session status with the kit-local
// core. Launching and stopping keep those names. A running local core uses
// state local so it is not idle and is not adopted as a host session.
func applyKitLocalRun(status protocol.Status, run KitLocalRun) protocol.Status {
	if status.State != protocol.StateIdle {
		return status
	}
	var state protocol.State
	switch run.Phase {
	case "launching":
		state = protocol.StateLaunching
	case "running":
		state = protocol.StateLocal
	case "stopping":
		state = protocol.StateStopping
	default:
		return status
	}
	overlaid := protocol.Status{State: state}
	if core := strings.TrimSpace(run.CoreID); core != "" {
		overlaid.ObservedCore = &core
		overlaid.ExpectedCore = &core
	}
	return overlaid
}
