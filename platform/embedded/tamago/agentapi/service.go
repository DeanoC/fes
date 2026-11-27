// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targethttp"
	"github.com/DeanoC/fes/platform/embedded/tamago/mdns"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/google/uuid"
)

func defaultMDNSNames(targetID string) (instance, host string) {
	short := strings.ReplaceAll(targetID, "-", "")
	if len(short) > 12 {
		short = short[:12]
	}
	return "FES-TamaGo-" + short, "fes-tamago-" + short + ".local"
}

var (
	// ErrInvalidConfig is returned when ServiceConfig or StateControllerConfig fails validation.
	ErrInvalidConfig = errors.New("agentapi: invalid configuration")
	// ErrServiceStarted is returned when Start is called more than once.
	ErrServiceStarted = errors.New("agentapi: service already started")
	// ErrServiceStopped is returned when Start is called after Stop.
	ErrServiceStopped = errors.New("agentapi: service stopped")
)

// Endpoint abstracts a live TamaGo network interface or test *network.Link pair
// capable of serving HTTP over TCP and emitting/tapping raw Ethernet frames for mDNS.
type Endpoint interface {
	network.RawFramePort
	Address() string
	MAC() string
	NewHTTPServer(network.ServerConfig) (*network.HTTPServer, error)
}

type linkEndpoint struct {
	link *network.Link
	mac  string
}

// NewLinkEndpoint adapts a *network.Link and its MAC address into an Endpoint for tests.
func NewLinkEndpoint(link *network.Link, mac string) Endpoint {
	return linkEndpoint{link: link, mac: mac}
}

func (e linkEndpoint) Err() error {
	if e.link == nil {
		return network.ErrControl
	}
	return e.link.Err()
}

func (e linkEndpoint) Address() string {
	if e.link == nil {
		return ""
	}
	return e.link.Prefix().String()
}

func (e linkEndpoint) MAC() string { return e.mac }

func (e linkEndpoint) SendRawFrame(frame []byte) error {
	if e.link == nil {
		return network.ErrControl
	}
	return e.link.SendRawFrame(frame)
}

func (e linkEndpoint) AddTap(fn func([]byte)) func() {
	if e.link == nil {
		return func() {}
	}
	return e.link.AddTap(fn)
}

func (e linkEndpoint) NewHTTPServer(cfg network.ServerConfig) (*network.HTTPServer, error) {
	return network.NewHTTPServer(e.link, cfg)
}

// StateControllerConfig configures a thread-safe StateController.
type StateControllerConfig struct {
	Board   string
	Address string
	BootID  string // Optional boot identity supplied by the platform handoff.
}

// StateController implements targethttp.Controller and targethttp.DiagnosticsProvider
// with mutable thread-safe appliance state.
type StateController struct {
	cfg StateControllerConfig

	mu     sync.Mutex
	state  protocol.State
	coreID string
}

var bootID = sync.OnceValue(uuid.NewString)

// BootID identifies this firmware boot across controller and network service restarts.
func BootID() string { return bootID() }

// NewStateController constructs a StateController initialized to StateIdle.
func NewStateController(cfg StateControllerConfig) *StateController {
	if cfg.BootID == "" {
		cfg.BootID = BootID()
	}
	return &StateController{
		cfg:   cfg,
		state: protocol.StateIdle,
	}
}

// Health reports agent readiness and version information.
func (c *StateController) Health(agentVersion string) protocol.Health {
	return protocol.Health{
		APIVersion:   "v1",
		BootID:       c.cfg.BootID,
		AgentVersion: agentVersion,
		Ready:        true,
	}
}

// Status returns a snapshot of the controller's active state and observed core.
func (c *StateController) Status() protocol.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	var observed *string
	if c.coreID != "" {
		v := c.coreID
		observed = &v
	}
	return protocol.Status{
		State:        c.state,
		ObservedCore: observed,
	}
}

// Stop transitions the controller back to StateIdle and returns the updated status.
func (c *StateController) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	c.mu.Lock()
	c.state = protocol.StateIdle
	c.coreID = ""
	c.mu.Unlock()
	return c.Status(), nil
}

// SetState updates the controller's state and active core identifier.
func (c *StateController) SetState(state protocol.State, coreID string) {
	c.mu.Lock()
	c.state = state
	c.coreID = coreID
	c.mu.Unlock()
}

// Diagnostics returns a structured controller status snapshot for /v1/diagnostics.
func (c *StateController) Diagnostics(context.Context) (any, *protocol.APIError) {
	st := c.Status()
	return struct {
		Board   string          `json:"board,omitempty"`
		Address string          `json:"address,omitempty"`
		Status  protocol.Status `json:"status"`
	}{
		Board:   c.cfg.Board,
		Address: c.cfg.Address,
		Status:  st,
	}, nil
}

// ServiceConfig configures the combined FogCast target HTTP + optional mDNS responder Service.
type ServiceConfig struct {
	Endpoint             Endpoint
	Port                 uint16
	TLSConfig            *tls.Config
	Token                string
	AllowUnauthenticated bool
	TargetID             string
	AgentVersion         string
	Controller           targethttp.Controller
	Packages             targethttp.PackageStore
	Launcher             targethttp.Launcher
	Diagnostics          targethttp.DiagnosticsProvider
	KitLease             targethttp.KitLeaseProvider
	ReadOnly             bool
	MaxUploadBytes       int64

	AdvertiseMDNS bool
	MDNSInstance  string
	MDNSHost      string
	MDNSInfo      mdns.FogCastInfo
	OnError       func(error)
}

// DiagnosticsSnapshot summarizes HTTP, targethttp agent, and mDNS state for /v1/diagnostics.
type DiagnosticsSnapshot struct {
	TargetID string                  `json:"target_id,omitempty"`
	Addr     string                  `json:"addr"`
	HTTP     HTTPDiagnosticsSnapshot `json:"http"`
	Agent    targethttp.AgentStats   `json:"agent"`
	MDNS     MDNSDiagnosticsSnapshot `json:"mdns"`
}

// HTTPDiagnosticsSnapshot reports lneto HTTPServer listener state.
type HTTPDiagnosticsSnapshot struct {
	Listening   bool   `json:"listening"`
	Addr        string `json:"addr"`
	TLS         bool   `json:"tls"`
	ActiveConns int64  `json:"active_conns"`
	TotalConns  uint64 `json:"total_conns"`
}

// MDNSDiagnosticsSnapshot reports mDNS responder activity.
type MDNSDiagnosticsSnapshot struct {
	Enabled         bool   `json:"enabled"`
	Running         bool   `json:"running"`
	Announcements   uint64 `json:"announcements"`
	Goodbyes        uint64 `json:"goodbyes"`
	QueriesReceived uint64 `json:"queries_received"`
	ResponsesSent   uint64 `json:"responses_sent"`
}

// ServiceStats aggregates raw stats from HTTPServer, targethttp.Handler, and mDNS Responder.
type ServiceStats struct {
	Addr  netip.AddrPort
	HTTP  network.ServerStats
	Agent targethttp.AgentStats
	MDNS  mdns.ResponderStats
}

// Service owns a FogCast targethttp HTTPServer and optional mDNS Responder bound to an Endpoint.
type Service struct {
	cfg       ServiceConfig
	addr      netip.AddrPort
	handler   *targethttp.Handler
	httpSrv   *network.HTTPServer
	responder *mdns.Responder

	mu      sync.Mutex
	started bool
	closed  bool
}

type builtInDiagnostics struct {
	svc *Service
}

func (d builtInDiagnostics) Diagnostics(context.Context) (any, *protocol.APIError) {
	return d.svc.DiagnosticsSnapshot(), nil
}

// NewService validates cfg and constructs an idle Service ready for Start.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Endpoint == nil {
		return nil, fmt.Errorf("%w: missing network endpoint", ErrInvalidConfig)
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("%w: TCP listen port must be non-zero", ErrInvalidConfig)
	}
	if cfg.Controller == nil {
		return nil, fmt.Errorf("%w: missing controller", ErrInvalidConfig)
	}
	if cfg.AdvertiseMDNS && cfg.TLSConfig != nil {
		return nil, fmt.Errorf("%w: mDNS advertisement does not support TLS endpoints", ErrInvalidConfig)
	}
	prefix, err := netip.ParsePrefix(cfg.Endpoint.Address())
	if err != nil || !prefix.IsValid() || !prefix.Addr().Is4() {
		return nil, fmt.Errorf("%w: invalid endpoint IPv4 prefix %q", ErrInvalidConfig, cfg.Endpoint.Address())
	}
	hw, err := net.ParseMAC(cfg.Endpoint.MAC())
	if err != nil || len(hw) != 6 || hw[0]&1 != 0 || hw.String() == "00:00:00:00:00:00" {
		return nil, fmt.Errorf("%w: invalid endpoint MAC %q", ErrInvalidConfig, cfg.Endpoint.MAC())
	}

	targetID := cfg.TargetID
	if targetID == "" {
		targetID = cfg.MDNSInfo.TargetID
	}

	svc := &Service{
		cfg:  cfg,
		addr: netip.AddrPortFrom(prefix.Addr(), cfg.Port),
	}

	diag := cfg.Diagnostics
	if diag == nil {
		diag = builtInDiagnostics{svc: svc}
	}

	handler, err := targethttp.New(targethttp.Config{
		Controller:           cfg.Controller,
		Token:                cfg.Token,
		AllowUnauthenticated: cfg.AllowUnauthenticated,
		TargetID:             targetID,
		AgentVersion:         cfg.AgentVersion,
		Packages:             cfg.Packages,
		Launcher:             cfg.Launcher,
		Diagnostics:          diag,
		KitLease:             cfg.KitLease,
		ReadOnly:             cfg.ReadOnly,
		MaxUploadBytes:       cfg.MaxUploadBytes,
	})
	if err != nil {
		return nil, err
	}
	svc.handler = handler

	httpSrv, err := cfg.Endpoint.NewHTTPServer(network.ServerConfig{
		Port:      cfg.Port,
		Handler:   handler,
		TLSConfig: cfg.TLSConfig,
	})
	if err != nil {
		return nil, err
	}
	svc.httpSrv = httpSrv

	if cfg.AdvertiseMDNS {
		if !mdns.ValidID(targetID) {
			return nil, fmt.Errorf("%w: valid UUID target_id required for mDNS advertisement", ErrInvalidConfig)
		}
		defaultInstance, defaultHost := defaultMDNSNames(targetID)
		instance := cfg.MDNSInstance
		if instance == "" {
			instance = defaultInstance
		}
		host := cfg.MDNSHost
		if host == "" {
			host = defaultHost
		}
		info := cfg.MDNSInfo
		info.TargetID = targetID
		if info.DiscoveryProtocol == "" {
			info.DiscoveryProtocol = mdns.FogCastProtocolVersion
		}
		mdnsSvc, err := mdns.NewFogCastService(instance, host, prefix.Addr(), cfg.Port, info)
		if err != nil {
			return nil, err
		}
		var mac6 [6]byte
		copy(mac6[:], hw)
		responder, err := mdns.NewResponder(mdns.ResponderConfig{
			Port:     cfg.Endpoint,
			LocalMAC: mac6,
			Service:  mdnsSvc,
			OnError:  cfg.OnError,
		})
		if err != nil {
			return nil, err
		}
		svc.responder = responder
	}

	return svc, nil
}

// Start begins serving FogCast HTTP routes and, when enabled, mDNS service advertisement.
func (s *Service) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("%w: nil service", ErrInvalidConfig)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrServiceStopped
	}
	if s.started {
		s.mu.Unlock()
		return ErrServiceStarted
	}
	s.started = true
	s.mu.Unlock()

	if err := s.httpSrv.Start(ctx); err != nil {
		_ = s.httpSrv.Close()
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
		return err
	}
	if s.responder != nil {
		if err := s.responder.Start(ctx); err != nil {
			_ = s.httpSrv.Close()
			s.mu.Lock()
			s.started = false
			s.mu.Unlock()
			return err
		}
	}
	return nil
}

// Stop emits an mDNS goodbye (if advertising) and gracefully stops the HTTP server.
// Safe to call multiple times or before Start.
func (s *Service) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.closed || !s.started {
		s.closed = true
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	if s.responder != nil {
		s.responder.Stop()
	}
	err := s.httpSrv.Shutdown(ctx)
	if err != nil {
		_ = s.httpSrv.Close()
	}
	return err
}

// OnNewEndpoint builds a fresh server and mDNS responder after this address
// has retired. Pairing, bearer checks and the same local boot authority carry
// forward. No sockets, advertised addresses or connections carry forward.
func (s *Service) OnNewEndpoint(endpoint Endpoint) (*Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		return nil, errors.New("agentapi: previous network endpoint still active")
	}
	cfg := s.cfg
	cfg.Endpoint = endpoint
	return NewService(cfg)
}

// Addr returns the bound IPv4 address and TCP port for this Service.
func (s *Service) Addr() netip.AddrPort {
	if s == nil {
		return netip.AddrPort{}
	}
	return s.addr
}

// Stats returns a live telemetry snapshot across HTTPServer, targethttp, and mDNS.
func (s *Service) Stats() ServiceStats {
	if s == nil {
		return ServiceStats{}
	}
	var mstats mdns.ResponderStats
	if s.responder != nil {
		mstats = s.responder.Stats()
	}
	return ServiceStats{
		Addr:  s.addr,
		HTTP:  s.httpSrv.Stats(),
		Agent: s.handler.Stats(),
		MDNS:  mstats,
	}
}

// DiagnosticsSnapshot formats current ServiceStats into a JSON-encodable struct.
func (s *Service) DiagnosticsSnapshot() DiagnosticsSnapshot {
	if s == nil {
		return DiagnosticsSnapshot{}
	}
	st := s.Stats()
	targetID := s.cfg.TargetID
	if targetID == "" {
		targetID = s.cfg.MDNSInfo.TargetID
	}
	return DiagnosticsSnapshot{
		TargetID: targetID,
		Addr:     st.Addr.String(),
		HTTP: HTTPDiagnosticsSnapshot{
			Listening:   st.HTTP.Listening,
			Addr:        st.HTTP.Addr,
			TLS:         st.HTTP.TLS,
			ActiveConns: st.HTTP.ActiveConns,
			TotalConns:  st.HTTP.TotalConns,
		},
		Agent: st.Agent,
		MDNS: MDNSDiagnosticsSnapshot{
			Enabled:         s.responder != nil,
			Running:         st.MDNS.Running,
			Announcements:   st.MDNS.Announcements,
			Goodbyes:        st.MDNS.Goodbyes,
			QueriesReceived: st.MDNS.QueriesReceived,
			ResponsesSent:   st.MDNS.ResponsesSent,
		},
	}
}
