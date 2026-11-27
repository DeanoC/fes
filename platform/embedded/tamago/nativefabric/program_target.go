// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"fmt"

	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/mdns"
)

// programTarget observes the single local launcher. Its immutable owner came
// from cold admission before MENU or SD started. A package cannot supply it.
type programTarget struct {
	service *ProgramService
	owner   uint64
	bootID  string
}

// NewTargetService publishes the running native launcher on its controlled
// Ethernet interface. The SD record supplies its host-paired identity only.
func (s *ProgramService) NewTargetService(endpoint agentapi.Endpoint, config agentapi.Provisioning, bootOwner uint64, bootID string, onError func(error)) (*agentapi.Service, error) {
	if s == nil || s.state == nil || bootOwner == 0 || !mdns.ValidID(bootID) {
		return nil, ErrProgramSession
	}
	s.state.operation.Lock()
	defer s.state.operation.Unlock()
	view := s.Status()
	if s.state.network == nil || view.Owner != bootOwner || view.State != ProgramReady || view.Generation != 0 {
		return nil, ErrProgramSession
	}
	target := programTarget{s, bootOwner, bootID}
	return agentapi.NewService(agentapi.ServiceConfig{
		Endpoint: endpoint, Port: config.Port, TargetID: config.TargetID, Token: config.Token,
		Controller: target, KitLease: programKitLease{target}, ReadOnly: true,
		AdvertiseMDNS: true, OnError: onError,
	})
}

// A running C64 with an explicitly selected onboard supplier needs a fresh
// protected observation for each remote read. In-flight local work wins.
func (t programTarget) observe() (ProgramServiceStatus, bool) {
	view := t.service.Status()
	p := t.service.state
	if p.menu == nil || p.menu.c64MACReceipt == nil || view.State != ProgramRunning {
		return view, true
	}
	if !p.operation.TryLock() {
		return view, false
	}
	defer p.operation.Unlock()
	view = t.service.Status()
	if view.State != ProgramRunning {
		return view, true
	}
	if view.Owner != t.owner || view.Identity.PackageID != p.menu.c64NetworkPackageID ||
		view.Identity.CoreID != "fes.c64" || view.Identity.ProgrammingProfile != c64Profile ||
		p.computer == nil || p.closed {
		return view, false
	}
	active, retired := p.computer.port.scopedState()
	if !active || retired {
		return view, false
	}
	if err := p.computer.port.observeRunningC64(); err != nil {
		p.networkFailure(err)
		return t.service.Status(), false
	}
	return view, true
}

func (t programTarget) Health(version string) protocol.Health {
	// Anonymous health is a local readiness read. Protected status and lease
	// observations perform the full fresh, role-bound hardware check.
	v := t.service.Status()
	return protocol.Health{APIVersion: "v1", AgentVersion: version,
		BootID: t.bootID,
		Ready:  v.Owner == t.owner && (v.State == ProgramReady || v.State == ProgramRunning)}
}

func (t programTarget) Status() protocol.Status {
	v, observed := t.observe()
	if !observed {
		return protocol.Status{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeUnavailable, Message: "native C64 observation unavailable"}}
	}
	if v.Owner != t.owner {
		return protocol.Status{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeUnavailable, Message: "native boot ownership changed"}}
	}
	switch v.State {
	case ProgramReady:
		return protocol.Status{State: protocol.StateIdle}
	case ProgramLoading:
		return protocol.Status{State: protocol.StateLaunching}
	case ProgramReturning:
		return protocol.Status{State: protocol.StateStopping}
	case ProgramRunning:
		id := v.Identity
		return protocol.Status{State: protocol.StateActive, ObservedCore: &id.CoreID,
			CorePackage: &protocol.CorePackageStatus{CoreID: id.CoreID, PackageID: id.PackageID,
				Generation: v.Generation, BuildID: id.BuildID, ImageSHA256: id.ProgrammedSHA256,
				ABI: protocol.RuntimeContract{ID: id.ABI, Major: id.Major, Minor: id.Minor}}}
	default:
		return protocol.Status{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeUnavailable, Message: string(v.State)}}
	}
}

// Stop never grants network callers control of the locally held session.
func (t programTarget) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	return t.Status(), &protocol.APIError{Code: protocol.CodeKitLeaseDenied, Message: "native launcher owns this session"}
}

type programKitLease struct{ target programTarget }

func (l programKitLease) Status() kitlease.Status {
	v, observed := l.target.observe()
	result := kitlease.Status{State: "blocked", TargetReachable: true}
	if !observed {
		result.Reason = "native C64 observation unavailable"
		return result
	}
	if v.Owner != l.target.owner {
		result.Reason = "native boot ownership changed"
		return result
	}
	result.Generation = fmt.Sprintf("%s-%016x-%d", l.target.bootID, v.Owner, v.Generation)
	// The local launcher retains lifecycle authority even when MENU is idle.
	// Held in ProgramServiceStatus describes the emulated CPU's reset state.
	switch v.State {
	case ProgramReady, ProgramRunning, ProgramLoading, ProgramReturning:
		result.State = "held"
		result.Owner = "native-tenfoot"
		result.Purpose = "local-launcher"
		result.TargetReady = v.State == ProgramReady || v.State == ProgramRunning
	case ProgramRebootRequired:
		result.State = "recovery-required"
		result.Reason = string(v.State)
	default:
		result.Reason = string(v.State)
	}
	return result
}
