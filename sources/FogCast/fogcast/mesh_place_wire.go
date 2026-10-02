package fogcast

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
)

const (
	// placementNodeTTL is how long one node document read is reused
	// for fpga_native eligibility.
	placementNodeTTL = 10 * time.Second
	// placementNodeBackoff is how long a failed read is remembered, so
	// an unreachable kit does not stall every games read.
	placementNodeBackoff = 5 * time.Second
	// placementNodeTimeout bounds one node document read.
	placementNodeTimeout = 2 * time.Second
)

// placementNodeRead is one node document read for placement. identity
// is the kit and endpoint the read used (see placementReadAddress). A
// read for another identity, including another endpoint, is not reused.
// ok is false when the read failed or named another node.
type placementNodeRead struct {
	identity meshTargetIdentity
	abis     []meshcontent.EligibleABI
	packages []string
	ok       bool
	at       time.Time
	// reason is set only by the library cache. Placement leaves it empty.
	// A non-empty reason is the node reason for a read that must not dial.
	reason string
}

// EnableMeshPlacement turns production placement on when configuration
// asked for it. Open records that choice and does not read a node.
// Production mains call this after Open. [mesh] placement defaults off,
// so an unset key keeps today's bind. This does not turn mesh ensure on.
func (s *Service) EnableMeshPlacement() {
	if s == nil {
		return
	}
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	if !s.meshPlacementConfig.Enabled {
		return
	}
	s.meshPlacement = true
	if s.meshHTTP == nil {
		s.meshHTTP = &http.Client{}
	}
}

// placementAsk is the placement request for one launch or one games
// read. An installed ask is used as it is. Otherwise production
// placement builds one from the node inventory and the configured
// override. Nil means the caller is not asking. An empty inventory is
// not a placement decision, so it returns nil and the bind stays.
func (s *Service) placementAsk(ctx context.Context) *MeshPlacementAsk {
	if s == nil {
		return nil
	}
	s.meshMu.Lock()
	ask := s.meshPlacementAsk
	wired := s.meshPlacement
	override := s.meshPlacementConfig.Override
	s.meshMu.Unlock()
	if ask != nil {
		return ask
	}
	if !wired {
		return nil
	}
	candidates := s.placementCandidates(ctx)
	if len(candidates) == 0 {
		return nil
	}
	return &MeshPlacementAsk{Candidates: candidates, OverrideNodeID: override}
}

// placementEntryFunc is the projection placement reads when the launch
// snapshot has none. The installed session's projection wins.
// Production placement projects the host library itself when no
// session is installed, so placement does not need the ensure seam.
func (s *Service) placementEntryFunc() func(string) (meshcontent.Entry, bool) {
	if s == nil {
		return nil
	}
	s.meshMu.Lock()
	entryFn := s.meshExecute.Entry
	wired := s.meshPlacement
	s.meshMu.Unlock()
	if entryFn == nil && wired {
		return s.meshCatalogEntry
	}
	return entryFn
}

// placementCandidates builds the Place input from the node inventory.
// Execute kinds, DisplaySink, InputSource, and the mesh major come from
// each advertisement. An fpga_native row carries the abis of that
// node's content document, read only when the node is an enabled
// configured kit. An advertisement alone is not eligibility. Rows keep
// inventory order (node id order), which is the order Place reads when
// several native_emu nodes can run a title.
func (s *Service) placementCandidates(ctx context.Context) []meshplace.Candidate {
	nodes := s.MeshNodes()
	out := make([]meshplace.Candidate, 0, len(nodes))
	for _, node := range nodes {
		candidate := meshplace.Candidate{
			NodeID:      node.NodeID,
			MeshMajorOK: discovery.MeshMajorCompatible(node.Mesh),
			DisplaySink: node.Capabilities.DisplaySink,
			InputSource: node.Capabilities.InputSource,
		}
		fpga := false
		for _, execute := range node.Capabilities.Execute {
			candidate.Execute = append(candidate.Execute, execute.Kind)
			fpga = fpga || execute.Kind == meshcontent.ExecuteFPGANative
		}
		if fpga {
			candidate.ABIs = s.placementNodeABIs(ctx, node.NodeID)
		}
		out = append(out, candidate)
	}
	return out
}

// placementNodeABIs is the abis placement passes for nodeID. The node
// must be an enabled configured kit with an address, an agent token,
// and that TargetID, and its content document must name nodeID. The
// document is read at one endpoint: the one discovered inventory origin
// for this node, otherwise the origin AdoptEndpoint reconciled on the
// kit's target client when that differs from the configured address,
// otherwise the configured address (#259). The read keeps the
// configured token and does not rewrite s.targets, the config file, or
// the target client. A failed
// read, another node id, and an unconfigured or disabled node return
// nil, which is not eligibility. A read is reused for placementNodeTTL
// and a failure for placementNodeBackoff. A read the caller canceled is
// not remembered. The read is not a kit-lease mutation.
func (s *Service) placementNodeABIs(ctx context.Context, nodeID string) []meshcontent.EligibleABI {
	abis, _ := s.placementNodeFacts(ctx, nodeID)
	return abis
}

// placementNodeFacts reuses placement's authenticated document and cache for
// both ABI eligibility and the installed package ids in that same document.
func (s *Service) placementNodeFacts(ctx context.Context, nodeID string) ([]meshcontent.EligibleABI, []string) {
	cfg, clientEndpoint, ok := s.targetForPlacementRead(nodeID)
	if !ok {
		return nil, nil
	}
	want := meshTargetIdentityOf(cfg.Name, cfg)
	if !want.dialable() || want.nodeID != nodeID {
		return nil, nil
	}
	want.address = placementReadAddress(want.address, s.discoveredPlacementOrigin(nodeID), clientEndpoint)
	s.meshMu.Lock()
	cached, hit := s.placementNodes[nodeID]
	client := s.meshHTTP
	s.meshMu.Unlock()
	if hit && cached.identity == want {
		ttl := placementNodeTTL
		if !cached.ok {
			ttl = placementNodeBackoff
		}
		if time.Since(cached.at) < ttl {
			return append([]meshcontent.EligibleABI(nil), cached.abis...), append([]string(nil), cached.packages...)
		}
	}
	abis, packages, ok := readPlacementNodeFacts(ctx, want, client)
	if !ok && ctx != nil && ctx.Err() != nil {
		return nil, nil
	}
	s.meshMu.Lock()
	if s.placementNodes == nil {
		s.placementNodes = map[string]placementNodeRead{}
	}
	s.placementNodes[nodeID] = placementNodeRead{identity: want, abis: abis, packages: packages, ok: ok, at: time.Now()}
	s.meshMu.Unlock()
	return append([]meshcontent.EligibleABI(nil), abis...), append([]string(nil), packages...)
}

// libraryNodeFacts is the package evidence GET /api/v1/library/titles
// may use. It dials the [[targets]] address exactly as configured, the
// enrolled origin, and it does not call placementReadAddress. A
// discovered or reconciled address is not a library endpoint. The
// dial uses a client that refuses redirects and dials directly, so a
// 3xx or HTTP_PROXY cannot forward the agent token. A configured
// hostname is resolved once for this process and later authenticated
// reads dial only that pinned IP, leaving the configured host on the
// URL and the Host header. When the library cache refreshes, the name
// is resolved again. If that answer no longer includes the pinned IP,
// this returns LibraryNodeMovedReason and does not dial. An IP literal
// is dialed as written, on that same direct client. A fresh hit in
// libraryNodes for that same configured identity is reused so a GET
// does not hammer the kit. Hostname lookup and the document read share
// one placementNodeTimeout context. A miss, a failed read, or an
// expired entry is not eligibility. This method does not read or
// write placementNodes. The pin is not persisted (#396).
func (s *Service) libraryNodeFacts(ctx context.Context, nodeID string) ([]meshcontent.EligibleABI, []string, string) {
	if s == nil {
		return nil, nil, ""
	}
	// The reconciled endpoint is ignored. cfg.Address is the enrolled origin.
	cfg, _, ok := s.targetForPlacementRead(nodeID)
	if !ok {
		return nil, nil, ""
	}
	configured := meshTargetIdentityOf(cfg.Name, cfg)
	if !configured.dialable() || configured.nodeID != nodeID {
		return nil, nil, ""
	}
	s.meshMu.Lock()
	cached, hit := s.libraryNodes[nodeID]
	client := s.meshHTTP
	s.meshMu.Unlock()
	if hit && cached.identity == configured {
		ttl := placementNodeTTL
		if !cached.ok {
			ttl = placementNodeBackoff
		}
		if time.Since(cached.at) < ttl {
			return append([]meshcontent.EligibleABI(nil), cached.abis...), append([]string(nil), cached.packages...), cached.reason
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Lookup and the kit read share this deadline. A child timer inside
	// readPlacementNodeFacts cannot extend it. A deadline is not cached.
	readCtx, cancel := context.WithTimeout(ctx, placementNodeTimeout)
	defer cancel()
	endpoint, err := url.Parse(configured.address)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || net.ParseIP(endpoint.Hostname()) != nil {
		// An IP literal, and an address that is not an HTTP origin, has
		// no lookup that can move. The client still dials directly.
		return s.finishLibraryRead(readCtx, nodeID, configured, libraryReadClient(client, nil))
	}
	// Resolve outside meshMu. The pin is recorded before the dial, so a
	// failed read still leaves the first answer pinned. The resolver
	// sees readCtx, so LookupIP stops when the deadline passes.
	pin, moved, lookupErr := s.libraryHostPin(readCtx, endpoint.Hostname())
	if lookupErr != nil || readCtx.Err() != nil {
		if readCtx.Err() != nil {
			return nil, nil, ""
		}
		s.rememberLibraryRead(nodeID, configured, nil, nil, false, "")
		return nil, nil, ""
	}
	if moved {
		s.rememberLibraryRead(nodeID, configured, nil, nil, false, LibraryNodeMovedReason)
		return nil, nil, LibraryNodeMovedReason
	}
	if pin == nil {
		s.rememberLibraryRead(nodeID, configured, nil, nil, false, "")
		return nil, nil, ""
	}
	return s.finishLibraryRead(readCtx, nodeID, configured, libraryReadClient(client, libraryPinnedDial(endpoint.Hostname(), pin)))
}

// finishLibraryRead dials the configured origin with client and caches
// the outcome. A canceled read is not cached. The returned reason is
// empty: a moved hostname never reaches this dial.
func (s *Service) finishLibraryRead(ctx context.Context, nodeID string, configured meshTargetIdentity, client *http.Client) ([]meshcontent.EligibleABI, []string, string) {
	abis, packages, ok := readPlacementNodeFacts(ctx, configured, client)
	if !ok && ctx != nil && ctx.Err() != nil {
		return nil, nil, ""
	}
	s.rememberLibraryRead(nodeID, configured, abis, packages, ok, "")
	return append([]meshcontent.EligibleABI(nil), abis...), append([]string(nil), packages...), ""
}

// rememberLibraryRead stores one library node-document attempt. reason
// is LibraryNodeMovedReason when the attempt must not dial.
func (s *Service) rememberLibraryRead(nodeID string, identity meshTargetIdentity, abis []meshcontent.EligibleABI, packages []string, ok bool, reason string) {
	if s == nil {
		return
	}
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	if s.libraryNodes == nil {
		s.libraryNodes = map[string]placementNodeRead{}
	}
	s.libraryNodes[nodeID] = placementNodeRead{
		identity: identity,
		abis:     append([]meshcontent.EligibleABI(nil), abis...),
		packages: append([]string(nil), packages...),
		ok:       ok,
		at:       time.Now(),
		reason:   reason,
	}
}

// libraryHostPin resolves host for the library pin. The returned IP is
// the one an authenticated dial may use. moved is true when a pin
// already exists and this answer does not include it; the caller must
// not dial. A lookup error is not a move. The first successful answer
// is recorded for the process and is not replaced.
func (s *Service) libraryHostPin(ctx context.Context, host string) (net.IP, bool, error) {
	key := strings.ToLower(host)
	s.meshMu.Lock()
	resolve := s.libraryResolve
	var pinned net.IP
	if s.libraryPins != nil {
		if existing, ok := s.libraryPins[key]; ok {
			pinned = append(net.IP(nil), existing...)
		}
	}
	s.meshMu.Unlock()

	ips, err := lookupLibraryIPs(ctx, resolve, host)
	if err != nil {
		return nil, false, err
	}
	if pinned != nil {
		if !libraryIPIncludes(ips, pinned) {
			return nil, true, nil
		}
		return pinned, false, nil
	}
	if len(ips) == 0 {
		return nil, false, nil
	}
	chosen := append(net.IP(nil), ips[0]...)
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	if s.libraryPins == nil {
		s.libraryPins = map[string]net.IP{}
	}
	if existing, ok := s.libraryPins[key]; ok {
		kept := append(net.IP(nil), existing...)
		if !libraryIPIncludes(ips, kept) {
			return nil, true, nil
		}
		return kept, false, nil
	}
	s.libraryPins[key] = append(net.IP(nil), chosen...)
	return append(net.IP(nil), chosen...), false, nil
}

func lookupLibraryIPs(ctx context.Context, resolve func(context.Context, string) ([]net.IP, error), host string) ([]net.IP, error) {
	if resolve != nil {
		return resolve(ctx, host)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func libraryIPIncludes(ips []net.IP, pin net.IP) bool {
	for _, ip := range ips {
		if ip.Equal(pin) {
			return true
		}
	}
	return false
}

// placementReadAddress is the one address a placement node read dials.
// A unique discovered origin from the current browse window is the
// freshest evidence and wins, so a target client adopted at an earlier
// claim cannot pin the read to an endpoint the kit has since left.
// Otherwise (no row, an ambiguous claim, or rows retained after a browse
// error) the origin AdoptEndpoint reconciled on the target client is
// used, otherwise the configured address. The configured token and
// node-id check apply to every choice.
func placementReadAddress(configured, discovered, reconciled string) string {
	if origin, err := normalizeHTTPOrigin(discovered); err == nil {
		return origin
	}
	if origin, different := differingPlacementOrigin(reconciled, configured); different {
		return origin
	}
	return configured
}

// differingPlacementOrigin is candidate when it normalizes to an HTTP
// origin other than configured. An empty or invalid candidate is not
// used. configured is compared as an origin when it normalizes.
func differingPlacementOrigin(candidate, configured string) (string, bool) {
	origin, err := normalizeHTTPOrigin(candidate)
	if err != nil {
		return "", false
	}
	if configuredOrigin, cfgErr := normalizeHTTPOrigin(configured); cfgErr == nil && configuredOrigin == origin {
		return "", false
	}
	if configured == origin {
		return "", false
	}
	return origin, true
}

// discoveredPlacementOrigin is the inventory origin for nodeID when the
// current browse window's advertisements for that node normalize to
// exactly one HTTP origin. Invalid addresses are ignored, matching
// probeTarget. It returns "" when there is no row, when discovery saw
// that node id at several addresses (AddressConflict, or several rows),
// and when the rows were retained after a browse error. The caller then
// keeps the reconciled or configured address and sends the bearer to no
// contender. meshMu is not held across a read.
func (s *Service) discoveredPlacementOrigin(nodeID string) string {
	if s == nil || nodeID == "" {
		return ""
	}
	s.meshMu.Lock()
	retained := s.meshNodesRetained
	nodes := append([]MeshNode(nil), s.meshNodes...)
	s.meshMu.Unlock()
	if retained {
		return ""
	}
	var origin string
	found := false
	for _, node := range nodes {
		if node.NodeID != nodeID {
			continue
		}
		if node.AddressConflict {
			return ""
		}
		next, err := normalizeHTTPOrigin(node.Address)
		if err != nil {
			continue
		}
		if found && next != origin {
			return ""
		}
		origin = next
		found = true
	}
	if !found {
		return ""
	}
	return origin
}

// libraryReadClient is the HTTP client for one authenticated library
// read. It refuses redirects and dials directly: Proxy is nil, so
// HTTP_PROXY and HTTPS_PROXY cannot receive the bearer. base is not
// modified, and its Transport is not reused. A nil Transport on base
// would otherwise select the process default, which honors those
// variables. dial, when set, is the only connect path (the hostname
// pin). A nil dial connects straight to the URL host, which for an
// IP literal is that address. Keep-alives are off because this
// transport is used for one read.
func libraryReadClient(base *http.Client, dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	if dial == nil {
		dialer := &net.Dialer{}
		dial = dialer.DialContext
	}
	next := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:             nil,
			DisableKeepAlives: true,
			DialContext:       dial,
		},
	}
	if base != nil {
		next.Timeout = base.Timeout
	}
	return next
}

// libraryPinnedDial connects only to pin. The request URL keeps the
// configured hostname, so the Host header does too.
func libraryPinnedDial(host string, pin net.IP) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: placementNodeTimeout}
	pinned := append(net.IP(nil), pin...)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialHost, dialPort, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return nil, splitErr
		}
		if !strings.EqualFold(dialHost, host) {
			return nil, fmt.Errorf("library dial refused for host %s", dialHost)
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(pinned.String(), dialPort))
	}
}

// readPlacementNodeFacts reads one kit's content document at want.address
// with the configured agent token. The document must name want.nodeID.
func readPlacementNodeFacts(ctx context.Context, want meshTargetIdentity, client *http.Client) ([]meshcontent.EligibleABI, []string, bool) {
	endpoint, err := url.Parse(want.address)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{}
	}
	readCtx, cancel := context.WithTimeout(ctx, placementNodeTimeout)
	defer cancel()
	node, err := kitcontent.ReadNode(readCtx, endpoint, want.agent, client)
	if err != nil || node.NodeID != want.nodeID {
		return nil, nil, false
	}
	return node.ABIs, node.Packages, true
}
