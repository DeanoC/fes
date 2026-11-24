package tenfoot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// SDL cannot supply a per-device physical keyboard snapshot. This service
// recovers the shared pad/mouse feed while keyboard guest input stays fenced;
// local SDL UI and Stop do not depend on it.
type padOnlyAuthority struct {
	Runtime                                          protocol.RuntimeInputIdentity
	TargetID, BootID, AgentInstance, LeaseGeneration string
}

func (a *App) nativePadOnlyCapabilitiesLocked() bool {
	p := a.session.CorePackage
	if p == nil || p.ActiveInterfaces == nil || p.ABI.ID == "" || strings.TrimSpace(p.ABI.ID) != p.ABI.ID || p.ABI.Major == 0 {
		return false
	}
	seen := make(map[struct {
		id           string
		major, minor uint16
	}]bool, len(p.ActiveInterfaces))
	for _, contract := range p.ActiveInterfaces {
		if contract.ID == "" || strings.TrimSpace(contract.ID) != contract.ID || contract.Major == 0 || contract.ID == "fes.keyboard" || contract.ID == "fes.keyboard.hid" {
			return false
		}
		key := struct {
			id           string
			major, minor uint16
		}{contract.ID, contract.Major, contract.Minor}
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func (a *App) padOnlyAuthorityLocked(now time.Time, native bool) (padOnlyAuthority, error) {
	fresh := func(t time.Time) bool { return !t.IsZero() && !now.Before(t) && now.Sub(t) <= 5*time.Second }
	c := a.health.Connection
	modeAllowed := !native && a.kitLocalKeyboardUnavailable || native && a.delegatedV2Active && !a.nativeKeyboardClosing && !a.kitLocalKeyboardUnavailable && !a.session.CoreKeyboard && !a.session.CoreKeyboardHID && a.nativePadOnlyCapabilitiesLocked()
	if a.playHIDFailClosedLocked() {
		return padOnlyAuthority{}, errors.New("pad recovery denied by kit lease")
	}
	if !a.kitHost || !modeAllowed || a.client == nil || a.client.paired || !a.healthHave || !fresh(a.healthObservedAt) || !fresh(a.sessionObservedAt) || !a.sessionRuntimeInputFreshLocked(now) || a.hostUnreachable || !a.health.Ready || !a.health.TargetReachable || !a.kitLeaseHave || a.kitLease.Unavailable || !localhid.ValidRecoveryIdentity(c.TargetID) || !localhid.ValidRecoveryIdentity(c.BootID) || !fullLocalIdentity(c.LeaseGeneration) || c.LeaseGeneration != strings.ToLower(c.LeaseGeneration) || c.LeaseGeneration == strings.Repeat("0", 64) || a.kitLease.Generation != c.LeaseGeneration || a.session.State != "active" || a.session.TargetID != c.TargetID || a.session.RuntimeInput == nil || a.session.RuntimeInput.Validate() != nil || !protocol.ValidInputAgentInstance(a.session.InputAgentInstance) {
		return padOnlyAuthority{}, errors.New("SDL pad recovery authority unavailable")
	}
	identity := *a.session.RuntimeInput
	if identity.Kind == protocol.RuntimeInputDescribed && (a.session.CorePackage == nil || a.session.CorePackage.PackageID != identity.PackageID || a.session.CorePackage.Generation != identity.Generation) || identity.Kind == protocol.RuntimeInputDevelopment && (!a.session.Development || a.session.CorePackage != nil) {
		return padOnlyAuthority{}, errors.New("SDL pad recovery session changed")
	}
	return padOnlyAuthority{Runtime: identity, TargetID: c.TargetID, BootID: c.BootID, AgentInstance: a.session.InputAgentInstance, LeaseGeneration: c.LeaseGeneration}, nil
}

func (a *App) padOnlyAuthority(native bool) (padOnlyAuthority, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.padOnlyAuthorityLocked(time.Now(), native)
}

type padOnlyRecovery struct {
	app    *App
	feed   *localfeed.Feed
	native bool
}

// configureSDLPadRecovery reserves before SDL begins polling. A disabled or
// paired host never changes its existing input path.
func configureSDLPadRecovery(a *App, enabled bool) (*padOnlyRecovery, error) {
	if !enabled {
		return nil, nil
	}
	if a == nil {
		return nil, errors.New("SDL pad recovery unavailable")
	}
	a.mu.Lock()
	feed, ok := a.localFeed.(*localfeed.Feed)
	allowed := a.kitHost && a.kitLocalKeyboardUnavailable && a.client != nil && !a.client.paired && ok && feed != nil
	a.mu.Unlock()
	if !allowed {
		return nil, errors.New("SDL pad recovery requires the local kit host")
	}
	if err := feed.ReserveV2(); err != nil {
		return nil, err
	}
	return &padOnlyRecovery{app: a, feed: feed}, nil
}

func (s *padOnlyRecovery) recover(ctx context.Context, expected padOnlyAuthority, previous string) (handle localfeed.V2Handle, err error) {
	reply, handle, err := s.feed.RecoverV2(ctx, previous, expected.TargetID, expected.BootID, time.Second)
	if err != nil {
		return handle, err
	}
	defer func() {
		if err != nil {
			s.feed.CloseV2(handle)
		}
	}()
	s.app.mu.Lock()
	defer s.app.mu.Unlock()
	current, err := s.app.padOnlyAuthorityLocked(time.Now(), s.native)
	req := localhid.Request{Version: 2, Op: "recover", Request: reply.Request, AgentInstance: previous, TargetID: expected.TargetID, BootID: expected.BootID}
	if err != nil || ctx.Err() != nil || current != expected || reply.AgentInstance != current.AgentInstance || (reply.Status != "reader-drained" && reply.Status != "startup-retired") || (reply.Status == "startup-retired" && !s.app.managedKitSoftware) || localhid.CorrelateRecoveryReply(reply, req) != nil {
		return handle, errors.New("SDL pad recovery authority changed")
	}
	if err = ctx.Err(); err != nil {
		return handle, err
	}
	if s.native {
		if s.app.nativePadRetiring || s.app.nativePadRetirementEpoch >= ^uint64(0)-1 {
			return handle, errors.New("previous pad source is not retired")
		}
		if err = s.app.nativeKeyboardRetireForPadRecoveryLocked(expected, previous, reply); err != nil {
			return handle, err
		}
		// Mark the source before promotion can expose its first raw pad.
		s.app.nativePadRetirementEpoch++
		s.app.nativePadRetirementBinding = expected
		s.app.nativePadRetiring = true
		s.app.nativePadConnectionFence = &localfeed.PadWriteFence{}
	}
	if s.native {
		err = s.feed.PromoteRecoveredPad(handle, reply, s.app.nativePadRetirementEpoch, s.app.nativePadConnectionFence)
	} else {
		err = s.feed.PromoteRecovered(handle, reply)
	}
	if err != nil {
		if s.native {
			s.app.nativePadRetiring = false
			s.app.nativePadRetirementBinding = padOnlyAuthority{}
			s.app.nativePadConnectionFence.Revoke()
			s.app.nativePadConnectionFence = nil
		}
		return handle, err
	}
	return handle, nil
}
