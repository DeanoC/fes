package fogcast

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

const meshInventoryWindow = time.Second

// MeshNode is one discovered node advertisement. It is not a lease and it
// does not make a title Ready.
type MeshNode struct {
	NodeID       string                 `json:"node_id"`
	TargetID     string                 `json:"target_id"`
	Mesh         string                 `json:"mesh,omitempty"`
	Cap          string                 `json:"cap,omitempty"`
	Capabilities discovery.Capabilities `json:"capabilities"`
	Address      string                 `json:"address,omitempty"`
	TTLSeconds   *int                   `json:"ttl_seconds,omitempty"`
}

// SilenceReleasesLease reports whether dropping this row frees a kit lease.
func (MeshNode) SilenceReleasesLease() bool { return false }

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
	return s.refreshTargetConnectionWithBackoff(ctx, true)
}

func (s *Service) refreshTargetConnectionWithBackoff(ctx context.Context, respectBackoff bool) (result protocol.Health, resultErr error) {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	name := s.sessionTargetNameLocked()
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
	if respectBackoff && time.Now().Before(s.nextLookup) && s.connection.State == "disconnected" {
		s.connectionMu.Unlock()
		return protocol.Health{}, &targetObservationError{"admission_backoff", errors.New("target lookup backoff")}
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	s.lookupCancel = cancel
	s.connection = previous
	s.connection.State = "connecting"
	s.connectionMu.Unlock()
	defer cancel()
	health, address, err := s.probeTarget(lookupCtx, concrete, selected)
	if err != nil {
		return s.connectionFailed(previous, &targetObservationError{"admission_health", err})
	}
	if err := lookupCtx.Err(); err != nil {
		return s.connectionFailed(previous, &targetObservationError{"admission_health", err})
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
			return s.connectionFailed(previous, &targetObservationError{"admission_status", errors.New("Target status could not be reconciled.")})
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
		return s.connectionFailed(previous, &targetObservationError{"admission_ownership", errors.New("Target ownership could not be reconciled.")})
	}
	if !reboot && (previous.Address != address || (hadGrant && !ownership.Owned)) && s.targetReset != nil {
		s.targetReset()
	}
	status, err := concrete.Status(lookupCtx)
	if err != nil {
		return s.connectionFailed(previous, &targetObservationError{"admission_status", errors.New("Target status could not be reconciled.")})
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
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) && apiErr.Code == protocol.CodeVersionMismatch {
		previous.State = "version_mismatch"
	}
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
				if ctx.Err() != nil {
					return
				}
				if s.discoveryEnabled() || s.meshCollectInstalled() {
					observe, observeCancel := context.WithTimeout(ctx, meshInventoryWindow)
					_, _ = s.ObserveMesh(observe)
					observeCancel()
				}
			}
		}
	}()
}

func (s *Service) discoveryEnabled() bool {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	name := s.sessionTargetNameLocked()
	_, real := s.targetClients[name].(*targetclient.Client)
	return real && targetByName(s.targets, name).TargetID != ""
}

func (s *Service) protocolAdmissionEnabled() bool {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, _ := s.selectedClientLocked()
	_, real := client.(*targetclient.Client)
	// Address-only peers require the same protocol admission as discovered peers.
	return real
}

// Explicit Stop is a recovery request, not a background observation. Ignore only
// the lookup timer; retain the same fresh protocol, identity, ownership and status
// checks and their existing deadlines. This never retries a mutation.
func (s *Service) refreshStopAdmission(ctx context.Context) (protocol.Health, error) {
	if s.discoveryEnabled() {
		return s.refreshTargetConnectionWithBackoff(ctx, false)
	}
	return s.refreshTargetAdmission(ctx)
}

// Address-only mutation admission checks the contract without adding discovery,
// status, or lease reconciliation to the existing execution path.
func (s *Service) refreshTargetAdmission(ctx context.Context) (result protocol.Health, resultErr error) {
	if s.discoveryEnabled() {
		return s.refreshTargetConnection(ctx)
	}
	s.targetMu.RLock()
	client, ok := s.selectedClientLocked()
	s.targetMu.RUnlock()
	if !ok {
		return protocol.Health{}, errors.New("target unavailable")
	}
	health, err := client.Health(ctx)
	if err == nil {
		err = protocol.CheckAPIVersion(health.APIVersion)
	}
	if err != nil {
		return s.connectionFailed(s.TargetConnection(), &targetObservationError{"admission_health", err})
	}
	if connection := s.TargetConnection(); connection.State == "version_mismatch" {
		connection.State, connection.Message = "connecting", ""
		s.publishConnection(connection)
	}
	return health, nil
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
		Message: "target API version is missing or unsupported; expected v1",
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
		return selected.TargetID == "" || h.TargetID == selected.TargetID
	}
	if err == nil && valid(health) {
		if err := protocol.CheckAPIVersion(health.APIVersion); err != nil {
			return protocol.Health{}, "", err
		}
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
	// Resolve parses mesh-protocol version and the capability bag additively.
	// Phase 0 announcements that omit them still return an endpoint. Parsed
	// advertisement TTL does not release the kit lease.
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
	if err := protocol.CheckAPIVersion(health.APIVersion); err != nil {
		return protocol.Health{}, "", err
	}
	return health, address, nil
}

// The explicit development reboot already holds lifecycle admission and the
// target read lock. Its read-only polling may resolve without reacquiring either.
func (s *Service) developmentRecoveryHealth(client serviceClient, oldBoot string) func(context.Context) (protocol.Health, error) {
	selected := targetByName(s.targets, s.sessionTargetNameLocked())
	concrete, ok := client.(*targetclient.Client)
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
	// Forget this client's grant only. Another target's Soft-stop lease
	// stays reachable for a later explicit release.
	s.stoppedKitLeases = dropStoppedKitLease(s.stoppedKitLeases, client.KitLease())
	s.selectedTargetReconciled = false
}

func (s *Service) meshCollectInstalled() bool {
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	return s.collectNodes != nil
}

// MeshNodes returns the last collected advertisement inventory. The slice is
// a copy. An empty inventory is not a lease release.
func (s *Service) MeshNodes() []MeshNode {
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	if len(s.meshNodes) == 0 {
		return []MeshNode{}
	}
	return append([]MeshNode(nil), s.meshNodes...)
}

// ObserveMesh browses node advertisements and replaces the inventory with
// that window. A browse error keeps the previous rows. Replacing the rows,
// including with an empty set when an advertisement goes quiet or omits ttl,
// does not release a kit lease and does not change the Phase 0 target bind.
func (s *Service) ObserveMesh(ctx context.Context) ([]MeshNode, error) {
	s.meshMu.Lock()
	collect := s.collectNodes
	s.meshMu.Unlock()
	if collect == nil {
		collect = discovery.Collect
	}
	observed, err := collect(ctx)
	if err != nil {
		return s.MeshNodes(), err
	}
	nodes := make([]MeshNode, 0, len(observed))
	for _, n := range observed {
		nodes = append(nodes, meshNodeFrom(n))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	s.meshMu.Lock()
	s.meshNodes = nodes
	s.meshMu.Unlock()
	return s.MeshNodes(), nil
}

func meshNodeFrom(n discovery.ObservedNode) MeshNode {
	node := MeshNode{
		NodeID:       n.NodeID,
		TargetID:     n.TargetID,
		Mesh:         n.Mesh,
		Cap:          n.Cap,
		Capabilities: n.Capabilities,
		Address:      n.Address,
	}
	if n.TTLSeconds != nil {
		seconds := *n.TTLSeconds
		node.TTLSeconds = &seconds
	}
	return node
}

// kitLeaseForeign reports a kit lease held by another session. The same
// shell's retained grant is not busy.
func (s *Service) kitLeaseForeign() bool {
	return s.TargetConnection().State == "busy"
}

// launchUsesForeignKit reports an FPGA launch that would use the kit whose
// cached connection is held by another session. Host-only execution does not
// use that kit. A different named target does not use the selected connection.
func (s *Service) launchUsesForeignKit(target, execution string) bool {
	if execution == ExecutionHostOnly || !s.kitLeaseForeign() {
		return false
	}
	conn := s.TargetConnection()
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	name := strings.TrimSpace(target)
	if name == "" {
		name = s.selectedTarget
	}
	cfg := targetByName(s.targets, name)
	if conn.TargetID != "" && cfg.TargetID != "" {
		return conn.TargetID == cfg.TargetID
	}
	if conn.Address != "" && cfg.Address != "" {
		return conn.Address == cfg.Address
	}
	return name == s.selectedTarget
}

func connectionFromStatus(health protocol.Health, status protocol.Status, ownership targetclient.KitOwnership, address, id string) TargetConnection {
	connection := TargetConnection{State: "ready", Address: address, TargetID: id, BootID: health.BootID}
	if err := protocol.CheckAPIVersion(health.APIVersion); err != nil {
		connection.State = "version_mismatch"
		connection.Message = err.Error()
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
