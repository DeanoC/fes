package fogcast

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/DeanoC/FogCast/internal/buildinputs"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

type TargetConnection struct {
	State    string `json:"state"`
	Message  string `json:"message,omitempty"`
	Address  string `json:"address,omitempty"`
	TargetID string `json:"target_id,omitempty"`
	BootID   string `json:"boot_id,omitempty"`
	Owner    string `json:"owner,omitempty"`
}

func (s *Service) TargetConnection() TargetConnection {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	c := s.connection
	if c.State == "" {
		c.State = "disconnected"
	}
	return c
}

func (s *Service) cancelTargetLookup() {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	if s.lookupCancel != nil {
		s.lookupCancel()
	}
}

// refreshTargetConnection runs under the existing lifecycle admission. It never
// claims ownership or retries a hardware mutation.
func (s *Service) refreshTargetConnection(ctx context.Context) (protocol.Health, error) {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	name := s.selectedTarget
	s.executionMu.Lock()
	if s.activeTarget != "" && s.activeExecution != ExecutionHostOnly {
		name = s.activeTarget
	}
	s.executionMu.Unlock()
	selected := targetByName(s.targets, name)
	client, ok := s.selectedClientLocked()
	concrete, realClient := client.(*targetclient.Client)
	if !ok {
		s.publishConnection(TargetConnection{State: "disconnected", Message: "Enable and configure a target address."})
		return protocol.Health{}, errors.New("target unavailable")
	}
	// Test adapters and legacy application integrations keep their original surface.
	if !realClient {
		return client.Health(ctx)
	}
	s.connectionMu.Lock()
	previous := s.connection
	if previous.TargetID != selected.TargetID || previous.Address == "" {
		previous = TargetConnection{Address: selected.Address, TargetID: selected.TargetID}
	}
	if time.Now().Before(s.nextLookup) && s.connection.State == "disconnected" {
		s.connectionMu.Unlock()
		return protocol.Health{}, errors.New("target lookup backoff")
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	s.lookupCancel = cancel
	s.connection = previous
	s.connection.State = "connecting"
	s.connectionMu.Unlock()
	defer cancel()
	health, address, err := s.probeTarget(lookupCtx, concrete, selected)
	if err != nil {
		return s.connectionFailed(previous, err)
	}
	if err := lookupCtx.Err(); err != nil {
		return s.connectionFailed(previous, err)
	}
	// Authenticated explicit address may bind an existing agent identity once.
	if selected.TargetID == "" && discovery.ValidID(health.TargetID) && s.configPath != "" {
		targets := append([]TargetConfig(nil), s.targets...)
		for i := range targets {
			if targets[i].Name == selected.Name {
				targets[i].TargetID = health.TargetID
			}
		}
		s.libraryMu.RLock()
		err := writeCanonicalConfig(s.configPath, s.roots, targets, s.selectedTarget)
		s.libraryMu.RUnlock()
		if err != nil {
			return s.connectionFailed(previous, errors.New("Cannot save the target identity in private settings."))
		}
		s.targets = targets
		selected.TargetID = health.TargetID
	}
	// Address-only older agents need not expose the discovery/lease extensions.
	if selected.TargetID == "" {
		status, err := concrete.Status(lookupCtx)
		if err != nil {
			return s.connectionFailed(previous, errors.New("Target status could not be reconciled."))
		}
		base, _ := url.Parse(address)
		// Old peers may omit the lease extension. Its absence does not disable
		// explicit-address health/status inspection.
		ownership, _ := concrete.AdoptEndpoint(lookupCtx, base, false)
		connection := connectionFromStatus(health, status, ownership, address, "")
		s.publishConnected(connection)
		return health, nil
	}
	base, _ := url.Parse(address)
	reboot := previous.BootID != "" && previous.BootID != health.BootID
	hadGrant := concrete.HasKitGrant()
	if reboot {
		s.invalidateTargetSession(concrete)
	}
	ownership, err := concrete.AdoptEndpoint(lookupCtx, base, reboot)
	if err != nil {
		return s.connectionFailed(previous, errors.New("Target ownership could not be reconciled."))
	}
	if !reboot && (previous.Address != address || (hadGrant && !ownership.Owned)) && s.targetReset != nil {
		s.targetReset()
	}
	status, err := concrete.Status(lookupCtx)
	if err != nil {
		return s.connectionFailed(previous, errors.New("Target status could not be reconciled."))
	}
	connection := connectionFromStatus(health, status, ownership, address, selected.TargetID)
	s.executionMu.Lock()
	s.selectedTargetReconciled = status.State == protocol.StateIdle
	s.selectedTargetRepairAllowed = false
	s.executionMu.Unlock()
	s.publishConnected(connection)
	return health, nil
}

func (s *Service) publishConnection(connection TargetConnection) {
	s.connectionMu.Lock()
	s.connection = connection
	s.connectionMu.Unlock()
}
func (s *Service) connectionFailed(previous TargetConnection, err error) (protocol.Health, error) {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	previous.State = "disconnected"
	previous.Message = err.Error()
	s.connection = previous
	delay := time.Second << min(s.lookupFailures, 4)
	if delay > 15*time.Second {
		delay = 15 * time.Second
	}
	s.lookupFailures++
	s.nextLookup = time.Now().Add(delay)
	return protocol.Health{}, err
}

func (s *Service) startTargetMonitor() {
	ctx, cancel := context.WithCancel(context.Background())
	s.monitorCancel = cancel
	s.monitorDone = make(chan struct{})
	go func() {
		defer close(s.monitorDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:

				request, cancel := context.WithTimeout(ctx, 4*time.Second)
				_, _ = s.Health(request)
				cancel()
			}
		}
	}()
}

func (s *Service) discoveryEnabled() bool {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	_, real := s.targetClients[s.selectedTarget].(*targetclient.Client)
	return real && targetByName(s.targets, s.selectedTarget).TargetID != ""
}

func (s *Service) incompatibleTargetError() error {
	s.targetMu.RLock()
	selected := s.selectedTarget
	s.targetMu.RUnlock()
	s.executionMu.Lock()
	active := s.activeTarget
	s.executionMu.Unlock()
	if active != "" && active != selected {
		return nil
	}
	if s.TargetConnection().State != "version_mismatch" {
		return nil
	}
	return &protocol.APIError{
		Code:    protocol.CodeVersionMismatch,
		Message: "target artifacts do not match this host",
	}
}

// SetTargetReset registers local input teardown at composition time. The callback
// must not call back into Service or send network cleanup requests.
func (s *Service) SetTargetReset(reset func()) {
	s.targetMu.Lock()
	s.targetReset = reset
	s.targetMu.Unlock()
}

func (s *Service) probeTarget(ctx context.Context, client *targetclient.Client, selected TargetConfig) (protocol.Health, string, error) {
	healthCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	health, err := client.Health(healthCtx)
	cancel()
	valid := func(h protocol.Health) bool {
		return h.APIVersion == "v1" && (selected.TargetID == "" || h.TargetID == selected.TargetID)
	}
	if err == nil && valid(health) {
		return health, client.EndpointURL().String(), nil
	}
	if selected.TargetID == "" {
		return protocol.Health{}, "", errors.New("Target unavailable; check its configured address.")
	}
	resolve := s.resolveTarget
	if resolve == nil {
		resolve = discovery.Resolve
	}
	browseCtx, browseCancel := context.WithTimeout(ctx, time.Second)
	candidates, err := resolve(browseCtx, selected.TargetID)
	browseCancel()
	unique := map[string]bool{}
	for _, candidate := range candidates {
		if origin, e := normalizeHTTPOrigin(candidate); e == nil {
			unique[origin] = true
		}
	}
	if err != nil || len(unique) != 1 {
		return protocol.Health{}, "", errors.New("Cannot uniquely find the configured kit; check its network or manual address.")
	}
	var address string
	for candidate := range unique {
		address = candidate
	}
	base, _ := url.Parse(address)
	healthCtx, cancel = context.WithTimeout(ctx, 750*time.Millisecond)
	health, err = client.Peer(base).Health(healthCtx)
	cancel()
	if err != nil || !valid(health) {
		return protocol.Health{}, "", errors.New("Discovered kit failed identity, authentication, or protocol validation.")
	}
	return health, address, nil
}

// The explicit development reboot already holds lifecycle admission and the
// target read lock. Its read-only polling may resolve without reacquiring either.
func (s *Service) developmentRecoveryHealth(client serviceClient, oldBoot string) func(context.Context) (protocol.Health, error) {
	concrete, ok := client.(*targetclient.Client)
	selected := targetByName(s.targets, s.selectedTarget)
	if !ok || selected.TargetID == "" {
		return client.Health
	}
	var retryAfter time.Time
	var failures uint
	return func(ctx context.Context) (protocol.Health, error) {
		if time.Now().Before(retryAfter) {
			return protocol.Health{}, errors.New("recovery lookup backoff")
		}
		health, address, err := s.probeTarget(ctx, concrete, selected)
		if err != nil {
			delay := time.Second << min(failures, 4)
			if delay > 15*time.Second {
				delay = 15 * time.Second
			}
			failures++
			retryAfter = time.Now().Add(delay)
			return protocol.Health{}, err
		}
		failures = 0
		retryAfter = time.Time{}
		if health.BootID != "" && health.BootID != oldBoot {
			base, _ := url.Parse(address)
			s.invalidateTargetSession(concrete)
			if _, err := concrete.AdoptEndpoint(ctx, base, true); err != nil {
				return protocol.Health{}, err
			}
			s.publishConnection(TargetConnection{State: "connecting", Address: address, TargetID: selected.TargetID, BootID: health.BootID})
		}
		return health, nil
	}
}

func (s *Service) invalidateTargetSession(client *targetclient.Client) {
	client.InvalidateKitSession()
	if s.targetReset != nil {
		s.targetReset()
	}
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.activeExecution != ExecutionHostOnly {
		s.activeExecution, s.activeTarget, s.activeGameID, s.activeSystem = "", "", "", ""
	}
	s.activePackageID, s.activePackageGeneration = "", 0
	s.packageRejection = nil
	s.stoppedKitLease = nil
	s.selectedTargetReconciled = false
}

func connectionFromStatus(health protocol.Health, status protocol.Status, ownership targetclient.KitOwnership, address, id string) TargetConnection {
	connection := TargetConnection{State: "ready", Address: address, TargetID: id, BootID: health.BootID}
	if field, _, _ := buildinputs.Check(version.Revision, buildinputs.ExpectedRuntimeCommit(), health.Artifacts); field != "" {
		connection.State = "version_mismatch"
		connection.Message = "Target " + field + " does not match this host."
		return connection
	}
	switch {
	case ownership.State == "blocked" || ownership.State == "revoking" || status.Recovery != "" || status.LastError != nil:
		connection.State = "recovery-required"
		connection.Message = "Target cleanup or recovery needs attention."
	case ownership.State == "held" && !ownership.Owned:
		connection.State = "busy"
		connection.Owner = ownership.Owner
		connection.Message = "The kit is held by another session."
	case status.State == protocol.StateActive:
		connection.State = "active"
	case status.State != protocol.StateIdle || !health.Ready:
		connection.State = "connecting"
	}
	return connection
}
func (s *Service) publishConnected(connection TargetConnection) {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	s.connection = connection
	s.nextLookup = time.Time{}
	s.lookupFailures = 0
}
